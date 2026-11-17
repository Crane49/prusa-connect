package threemf

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The namespaces of the 3MF core and of its production extension. The
// production extension lets a component or a build item name an object in
// another model file, through a p:path attribute.
const (
	nsCore       = "http://schemas.microsoft.com/3dmanufacturing/core/2015/02"
	nsProduction = "http://schemas.microsoft.com/3dmanufacturing/production/2015/06"
	// nsMaterial is the materials and properties extension. It gives colours
	// and materials and does not change a shape, so a reader that leaves it
	// out still reads every shape right.
	nsMaterial = "http://schemas.microsoft.com/3dmanufacturing/material/2015/02"
	// nsSlic3r is the namespace PrusaSlicer writes its own attributes in,
	// such as the painting of a triangle.
	nsSlic3r = "http://schemas.slic3r.org/3mf/2017/06"
)

// maxXMLDepth bounds how deep elements may nest. A model is six levels deep;
// a file nested a thousand levels is not a model.
const maxXMLDepth = 64

// unitScale is how many millimetres one unit of each 3MF unit is.
//
//nolint:gochecknoglobals // a constant table.
var unitScale = map[string]float64{
	"":           1,
	"millimeter": 1,
	"micron":     mmPerMicron,
	"centimeter": mmPerCentimeter,
	"inch":       mmPerInch,
	"foot":       mmPerFoot,
	"meter":      mmPerMeter,
}

// The units of the 3MF core, in millimetres.
const (
	mmPerMicron     = 0.001
	mmPerCentimeter = 10
	mmPerInch       = 25.4
	mmPerFoot       = 304.8
	mmPerMeter      = 1000
)

// resKey names one object resource: the model file it is in and its id.
type resKey struct {
	path string
	id   int
}

// component places one object inside another.
type component struct {
	ref resKey
	tr  Transform
}

// resource is one object of a model file.
type resource struct {
	key   resKey
	name  string
	mesh  *mesh
	comps []component
}

// triRange is a run of triangles, firstTri to lastTri inclusive, that a
// slicer's own files name as one part of a mesh. lo and hi become the vertices
// those triangles use, and lo stays -1 when no triangle of the run was read.
type triRange struct {
	firstTri, lastTri int
	lo, hi            int
	// paint is what was painted on the triangles of the run.
	paint paint
}

// mesh is what is kept of one mesh: its box, its vertices while the budget
// lasts, and the vertex runs of its parts.
type mesh struct {
	box   Box
	verts []float32
	// tris are the vertex indices of the triangles, three a triangle, kept with
	// the vertices. A slicer judges a part that reaches below the bed by the
	// edges that cross the bed's plane, so the edges are kept too.
	tris   []int32
	nverts int
	ntris  int
	// dropped is true when the vertices were over the budget and only the box
	// is kept.
	dropped bool
	ranges  []triRange
	// paint is what was painted on any triangle of the mesh.
	paint paint
	// cache holds the plane outline and height range of each vertex run, so
	// that instances of the same object do not compute them again.
	cache map[[2]int]*runShape
}

// buildItem is one item of the build.
type buildItem struct {
	ref       resKey
	tr        Transform
	printable *bool
	ordinal   int
}

// modelFile is one parsed model file.
type modelFile struct {
	path     string
	metadata []Metadata
	// unsupported are the extensions the file requires that this package does
	// not read, by namespace.
	unsupported []string
	resources   map[int]*resource
	order       []int
	items       []buildItem
}

// models are every model file the root model reached.
type models struct {
	root  *modelFile
	files map[string]*modelFile
}

func (m *models) resource(key resKey) *resource {
	file := m.files[key.path]
	if file == nil {
		return nil
	}

	return file.resources[key.id]
}

// parser reads one model file. It keeps vertices while the budget given to
// the whole read lasts.
type parser struct {
	limits    Limits
	keptVerts *int
	// ranges are the triangle runs a dialect wants turned into vertex runs,
	// by the id of the mesh object, for the root model only.
	ranges map[int][]triRange
	// refs are the other model files the components and items name.
	refs []string
	// orcaPaint reads painted triangles in the encoding of OrcaSlicer, which
	// differs from that of PrusaSlicer above 16 states.
	orcaPaint bool
}

// parseModel reads one model file from r.
func (p *parser) parseModel(ctx context.Context, path string, r io.Reader, root bool) (*modelFile, error) {
	out := &modelFile{path: path, resources: map[int]*resource{}}
	dec := xml.NewDecoder(r)
	dec.Strict = true

	st := &parseState{p: p, out: out, root: root, scale: 1, prefixes: map[string]string{}}

	for {
		if st.tokens++; st.tokens%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err //nolint:wrapcheck // the caller's own cancellation.
			}
		}

		tok, err := dec.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			if limit, ok := errors.AsType[*LimitError](err); ok {
				return nil, limit
			}

			return nil, fmt.Errorf("%w: %s: %w", ErrFormat, path, err)
		}

		if err = st.token(tok); err != nil {
			return nil, err
		}
	}

	if !st.sawModel || len(st.stack) != 0 {
		return nil, formatError("%s has no complete model element", path)
	}

	return out, nil
}

// parseState is where a parse is inside the document.
type parseState struct {
	p        *parser
	out      *modelFile
	root     bool
	scale    float64
	prefixes map[string]string
	depth    int
	stack    []xml.Name
	tokens   int
	sawModel bool

	// The element being read, when it is one whose content matters.
	inBuild   bool
	object    *resource
	mesh      *mesh
	ranges    []triRange
	run       int
	metaName  string
	inMeta    bool
	metaValue strings.Builder
}

func (st *parseState) token(tok xml.Token) error {
	switch t := tok.(type) {
	case xml.StartElement:
		st.depth++
		if st.depth > maxXMLDepth {
			return formatError("%s nests deeper than %d", st.out.path, maxXMLDepth)
		}

		st.stack = append(st.stack, t.Name)

		return st.start(t)
	case xml.EndElement:
		// RawToken does not match end tags to start tags, so this does.
		if len(st.stack) == 0 || st.stack[len(st.stack)-1] != t.Name {
			return formatError("%s closes %s, which is not open", st.out.path, t.Name.Local)
		}

		st.stack = st.stack[:len(st.stack)-1]
		st.depth--
		st.end(t)
	case xml.CharData:
		if st.inMeta && st.metaValue.Len() < maxMetadataValue {
			st.metaValue.Write(t)
		}
	}

	return nil
}

// maxMetadataValue bounds one metadata value of the model.
const maxMetadataValue = 64 << 10

// local returns the element's local name, and "" for an element of a namespace
// other than the core one.
func (st *parseState) local(name xml.Name) string {
	if name.Space == "" || st.prefixes[name.Space] == nsCore {
		return name.Local
	}

	return ""
}

func (st *parseState) attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Space == "" && a.Name.Local == name {
			return a.Value
		}
	}

	return ""
}

// pathAttr returns the production extension's p:path of an element.
func (st *parseState) pathAttr(e xml.StartElement) string {
	for _, a := range e.Attr {
		if a.Name.Local == "path" && a.Name.Space != "" && st.prefixes[a.Name.Space] == nsProduction {
			return a.Value
		}
	}

	return ""
}

func (st *parseState) start(e xml.StartElement) error {
	switch st.local(e.Name) {
	case "model":
		return st.startModel(e)
	case "metadata":
		if st.depth == 2 && st.root {
			st.inMeta = true
			st.metaName = st.attr(e, "name")
			st.metaValue.Reset()
		}
	case "object":
		return st.startObject(e)
	case "mesh":
		if st.object != nil {
			st.mesh = &mesh{box: emptyBox()}
			st.ranges = cloneRanges(st.p.ranges[st.object.key.id], st.root)
			st.run = 0
		}
	case "vertex":
		return st.vertex(e)
	case "triangle":
		return st.triangle(e)
	case "component":
		return st.component(e)
	case "build":
		st.inBuild = st.root
	case "item":
		return st.item(e)
	}

	return nil
}

func cloneRanges(ranges []triRange, root bool) []triRange {
	if !root || len(ranges) == 0 {
		return nil
	}

	out := make([]triRange, len(ranges))
	for i, r := range ranges {
		out[i] = triRange{firstTri: r.firstTri, lastTri: r.lastTri, lo: -1, hi: -1}
	}

	return out
}

func (st *parseState) startModel(e xml.StartElement) error {
	st.sawModel = true

	for _, a := range e.Attr {
		if a.Name.Space == "xmlns" {
			st.prefixes[a.Name.Local] = a.Value
		}
	}

	for prefix := range strings.FieldsSeq(st.attr(e, "requiredextensions")) {
		switch ns := st.prefixes[prefix]; ns {
		case nsCore, nsProduction, nsMaterial:
		case "":
			st.out.unsupported = append(st.out.unsupported, prefix)
		default:
			st.out.unsupported = append(st.out.unsupported, ns)
		}
	}

	scale, ok := unitScale[st.attr(e, "unit")]
	if !ok {
		return formatError("%s has unknown unit %q", st.out.path, st.attr(e, "unit"))
	}

	st.scale = scale

	return nil
}

func (st *parseState) startObject(e xml.StartElement) error {
	id, err := strconv.Atoi(st.attr(e, "id"))
	if err != nil || id < 0 {
		return formatError("%s has an object with id %q", st.out.path, st.attr(e, "id"))
	}

	if _, dup := st.out.resources[id]; dup {
		return formatError("%s declares object %d twice", st.out.path, id)
	}

	if len(st.out.resources) >= st.p.limits.MaxResources {
		return limitError("MaxResources", int64(st.p.limits.MaxResources))
	}

	st.object = &resource{key: resKey{path: st.out.path, id: id}, name: st.attr(e, "name")}
	st.out.resources[id] = st.object
	st.out.order = append(st.out.order, id)

	return nil
}

func (st *parseState) vertex(e xml.StartElement) error {
	if st.mesh == nil {
		return nil
	}

	var xyz [3]float64

	for i, name := range [3]string{"x", "y", "z"} {
		value, err := strconv.ParseFloat(st.attr(e, name), 64)
		if err != nil || !finite(value) {
			return formatError("%s object %d has a vertex that is not a number", st.out.path, st.object.key.id)
		}

		xyz[i] = value * st.scale
	}

	m := st.mesh
	m.nverts++
	m.box = m.box.add(xyz[0], xyz[1], xyz[2])

	if m.dropped {
		return nil
	}

	if !st.keep() {
		return nil
	}

	m.verts = append(m.verts, float32(xyz[0]), float32(xyz[1]), float32(xyz[2]))

	return nil
}

// keep counts one more vertex or triangle of the current mesh against
// MaxVertices. Past the limit, the mesh drops what it kept and keeps its box.
func (st *parseState) keep() bool {
	m := st.mesh
	if m.dropped {
		return false
	}

	if *st.p.keptVerts >= st.p.limits.MaxVertices {
		*st.p.keptVerts -= len(m.verts)/3 + len(m.tris)/3 //nolint:mnd // three numbers a vertex or a triangle.
		m.verts, m.tris, m.dropped = nil, nil, true

		return false
	}

	*st.p.keptVerts++

	return true
}

func (st *parseState) triangle(e xml.StartElement) error {
	if st.mesh == nil {
		return nil
	}

	index := st.mesh.ntris
	st.mesh.ntris++

	var (
		v    [3]int
		kept [3]int32
	)

	for i, name := range [3]string{"v1", "v2", "v3"} {
		// The core specification allows fewer than 2^31 vertices.
		value, err := strconv.ParseInt(st.attr(e, name), 10, 32)
		if err != nil || value < 0 {
			return formatError("%s object %d has a triangle with a bad vertex", st.out.path, st.object.key.id)
		}

		v[i], kept[i] = int(value), int32(value)
	}

	if st.keep() {
		st.mesh.tris = append(st.mesh.tris, kept[:]...)
	}

	// A triangle with no multi-material painting prints with its part's own
	// extruder, which is state 0.
	painted := st.paintOf(e)
	if painted.colors.empty() {
		painted.colors.add(0)
	}

	st.mesh.paint.merge(painted)

	if len(st.ranges) == 0 {
		return nil
	}

	// The runs are consecutive, in triangle order, so the run a triangle
	// belongs to is found by walking forward. A run that overlaps the one
	// before it is skipped.
	for st.run < len(st.ranges) && st.ranges[st.run].lastTri < index {
		st.run++
	}

	if st.run == len(st.ranges) || st.ranges[st.run].firstTri > index {
		return nil
	}

	r := &st.ranges[st.run]

	for _, vertex := range v {
		if r.lo < 0 || vertex < r.lo {
			r.lo = vertex
		}

		r.hi = max(r.hi, vertex)
	}

	r.paint.merge(painted)

	return nil
}

// paintOf reads the painting of one triangle: the attributes PrusaSlicer
// writes in its own namespace and the ones OrcaSlicer writes in none.
func (st *parseState) paintOf(e xml.StartElement) paint {
	var out paint

	if len(e.Attr) <= len([3]string{"v1", "v2", "v3"}) {
		return out
	}

	for _, a := range e.Attr {
		kind := paintKindOf(a.Name, st.prefixes)
		if kind == paintNone || a.Value == "" {
			continue
		}

		var states stateSet
		if !decodePaint(a.Value, st.p.orcaPaint, &states) {
			continue
		}

		switch kind {
		case paintColors:
			out.colors.merge(states)
		case paintSupports:
			out.supports = out.supports || states.painted()
			out.enforcers = out.enforcers || states.has(paintEnforcer)
		case paintSeams:
			out.seams = out.seams || states.painted()
		case paintNone:
		}
	}

	return out
}

func (st *parseState) component(e xml.StartElement) error {
	if st.object == nil {
		return nil
	}

	key, err := st.reference(e)
	if err != nil {
		return err
	}

	tr, ok := parseTransform(st.attr(e, "transform"))
	if !ok {
		return formatError("%s object %d has a component with a bad transform", st.out.path, st.object.key.id)
	}

	tr = scaleTranslation(tr, st.scale)
	st.object.comps = append(st.object.comps, component{ref: key, tr: tr})

	return nil
}

// scaleTranslation converts the move of a transform to millimetres. The
// linear part has no unit.
func scaleTranslation(tr Transform, scale float64) Transform {
	tr[9] *= scale
	tr[10] *= scale
	tr[11] *= scale

	return tr
}

// reference reads the objectid and p:path of a component or an item.
func (st *parseState) reference(e xml.StartElement) (resKey, error) {
	id, err := strconv.Atoi(st.attr(e, "objectid"))
	if err != nil || id < 0 {
		return resKey{}, formatError("%s has a reference to object %q", st.out.path, st.attr(e, "objectid"))
	}

	key := resKey{path: st.out.path, id: id}

	if target := st.pathAttr(e); target != "" {
		// The production extension: "Any consumer of a 3MF package that
		// contains path attributes in components in a non-root model file
		// MUST generate an error for that package."
		if !st.root {
			return resKey{}, formatError("%s, which is not the root model, names another file %q", st.out.path, target)
		}

		name, ok := resolve(st.out.path, target)
		if !ok {
			return resKey{}, formatError("%s names an unsafe path %q", st.out.path, target)
		}

		key.path = name
		if name != st.out.path {
			st.p.refs = append(st.p.refs, name)
		}
	}

	return key, nil
}

func (st *parseState) item(e xml.StartElement) error {
	if !st.inBuild {
		return nil
	}

	if len(st.out.items) >= st.p.limits.MaxItems {
		return limitError("MaxItems", int64(st.p.limits.MaxItems))
	}

	key, err := st.reference(e)
	if err != nil {
		return err
	}

	tr, ok := parseTransform(st.attr(e, "transform"))
	if !ok {
		return formatError("%s has a build item with a bad transform", st.out.path)
	}

	item := buildItem{ref: key, tr: scaleTranslation(tr, st.scale), ordinal: len(st.out.items)}

	switch st.attr(e, "printable") {
	case "0", "false":
		item.printable = new(false)
	case "1", "true":
		item.printable = new(true)
	}

	st.out.items = append(st.out.items, item)

	return nil
}

func (st *parseState) end(e xml.EndElement) {
	switch st.local(e.Name) {
	case "metadata":
		if st.inMeta {
			st.out.metadata = append(st.out.metadata, Metadata{Name: st.metaName, Value: st.metaValue.String()})
			st.inMeta = false
		}
	case "mesh":
		if st.object != nil && st.mesh != nil {
			st.mesh.ranges = st.ranges
			st.object.mesh = st.mesh
		}

		st.mesh, st.ranges = nil, nil
	case "object":
		st.object = nil
	case "build":
		st.inBuild = false
	}
}
