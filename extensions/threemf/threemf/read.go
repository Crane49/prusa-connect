package threemf

import (
	"context"
	"encoding/xml"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Read reads the 3MF project in r, which is size bytes long.
//
// It returns ErrFormat for a file that is not a 3MF it can read and ErrLimit
// for one larger than limits allow. A part of the file it cannot read in full,
// such as a damaged settings file, is reported in Project.Issues and the read
// goes on. A zero Limits takes DefaultLimits.
func Read(ctx context.Context, r io.ReaderAt, size int64, limits Limits) (*Project, error) {
	project, _, err := read(ctx, r, size, limits)

	return project, err
}

// read is Read, and it also answers the objects as the dialect grouped them,
// which ReadMesh takes the meshes from.
func read(ctx context.Context, r io.ReaderAt, size int64, limits Limits) (*Project, []objectSpec, error) {
	limits = limits.withDefaults()

	a, err := openArchive(r, size, limits)
	if err != nil {
		return nil, nil, err
	}

	rd := &reading{ctx: ctx, a: a, limits: limits, rels: readRelationships(ctx, a)}

	model := a.entry(rd.rels.model)
	if model == nil {
		return nil, nil, formatError("no model entry %q", rd.rels.model)
	}

	rd.producer = producerOf(sniffApplication(model))
	d := detect(rd.producer, a)
	rd.producer.Dialect = d.name()

	if err = d.prepare(rd); err != nil {
		return nil, nil, err
	}

	rd.orcaPaint = d.name() == DialectOrcaSlicer

	if err = rd.parseModels(model.Name); err != nil {
		return nil, nil, err
	}

	specs, err := d.objects(rd)
	if err != nil {
		return nil, nil, err
	}

	project := &Project{Producer: rd.producer, Metadata: rd.models.root.metadata}
	project.Objects = buildObjects(specs, shapeWork*rd.limits.MaxVertices)

	for o := range specs {
		if specs[o].settings == nil {
			specs[o].settings = none
		}
	}

	resolveParts(rd.rules, specs, project.Objects)

	d.place(rd, project, specs)

	project.Extruders = projectExtruders(project, rd.rules)
	if gcodeOnly(project) {
		rd.issue(IssueGCodeOnly, "")
	}

	project.Thumbnails = rd.thumbnails(d)
	project.Issues = rd.issues

	project.Producer = lateApplication(project.Producer, project.Metadata)

	if project.Producer.FormatVersion == "" {
		project.Producer.FormatVersion = firstNonEmpty(
			metadataValue(project.Metadata, "slic3rpe:Version3mf"),
			metadataValue(project.Metadata, "BambuStudio:3mfVersion"),
		)
	}

	project.ProjectID = rd.projectID

	return project, specs, nil
}

// reading is the state of one Read.
type reading struct {
	ctx      context.Context //nolint:containedctx // lives for one call to Read and no longer.
	a        *archive
	limits   Limits
	rels     relationships
	producer Producer
	models   *models
	issues   []Issue
	// ranges are the triangle runs the dialect wants as vertex runs.
	ranges map[int][]triRange
	// projectID is the project id a dialect found.
	projectID string
	// meshUses counts the meshes the objects are built of.
	meshUses int
	// rules resolve the extruder of each part. A dialect fills them from the
	// settings.
	rules extruderRules
	// orcaPaint reads painted triangles in the encoding of OrcaSlicer.
	orcaPaint bool
}

// useMeshes counts meshes the objects are built of against MaxMeshUses.
func (rd *reading) useMeshes(n int) error {
	rd.meshUses += n
	if rd.meshUses > rd.limits.MaxMeshUses {
		return limitError("MaxMeshUses", int64(rd.limits.MaxMeshUses))
	}

	return nil
}

func (rd *reading) issue(code IssueCode, detail string) {
	for _, existing := range rd.issues {
		if existing.Code == code && existing.Detail == detail {
			return
		}
	}

	rd.issues = append(rd.issues, Issue{Code: code, Detail: detail})
}

// parseModels reads the root model and every model file its components and
// items name.
func (rd *reading) parseModels(root string) error {
	kept := 0
	p := &parser{limits: rd.limits, keptVerts: &kept, ranges: rd.ranges, orcaPaint: rd.orcaPaint}
	rd.models = &models{files: map[string]*modelFile{}}

	queue := []string{root}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]

		if _, done := rd.models.files[path]; done {
			continue
		}

		f := rd.a.entry(path)
		if f == nil {
			rd.issue(IssueUnknownReference, path)

			continue
		}

		body, err := rd.a.openModel(f)
		if err != nil {
			return err
		}

		isRoot := rd.models.root == nil
		p.refs = p.refs[:0]
		file, err := p.parseModel(rd.ctx, path, body, isRoot)
		_ = body.Close() // the entry was read to its end or the parse failed; either way it is done.

		if err != nil {
			return err
		}

		rd.models.files[path] = file

		for _, ns := range file.unsupported {
			rd.issue(IssueUnsupportedExtension, ns)
		}

		if isRoot {
			rd.models.root = file
		}

		queue = append(queue, p.refs...)
	}

	for _, file := range rd.models.files {
		for _, res := range file.resources {
			if res.mesh != nil && res.mesh.dropped {
				rd.issue(IssueOutlineApprox, res.name)
			}
		}
	}

	return nil
}

// layerRange is one layer range of an object: settings that apply between
// two heights.
type layerRange struct {
	minZ, maxZ float64
	values     map[string]string
	get        getter
}

// gcodeOnly reports whether a project has no object and G-code for at least
// one bed. OrcaSlicer and BambuStudio write such a file when they export a
// sliced plate as .gcode.3mf.
func gcodeOnly(p *Project) bool {
	if len(p.Objects) > 0 {
		return false
	}

	for _, setup := range p.Setups {
		for _, bed := range setup.Beds {
			if bed.GCode != "" {
				return true
			}
		}
	}

	return false
}

// objectSpec is one object as a dialect grouped it.
type objectSpec struct {
	name       string
	resourceID int
	parts      []partSpec
	items      []buildItem
	// printable overrides the items' own printable flags, by item position in
	// items. A dialect that keeps the flag elsewhere fills it.
	printable []bool
	// shapes are what each item covers, filled by buildObjects.
	shapes []shape
	// settings are the object's own settings. Read fills a nil one with
	// none.
	settings getter
	// ranges are the object's layer ranges.
	ranges []layerRange
	// support is the support the object's settings ask for.
	support supportPlan
	// box is the box of the object's model parts in its own coordinates,
	// computed only when an instance needs it.
	box *Box
}

// buildObjects turns the dialect's grouping into Objects with their instances
// placed nowhere yet.
func buildObjects(specs []objectSpec, budget int) []Object {
	out := make([]Object, len(specs))

	for o, spec := range specs {
		object := Object{Index: o, ResourceID: spec.resourceID, Name: spec.name}
		for _, part := range spec.parts {
			object.Parts = append(object.Parts, part.part)
		}

		object.Instances = make([]Instance, len(spec.items))

		specs[o].shapes = make([]shape, len(spec.items))

		for i, item := range spec.items {
			var sh shape
			if budget > 0 {
				sh = shapeOf(spec.parts, item.tr, &budget)
			} else {
				sh = boxShape(&specs[o], item.tr)
			}

			specs[o].shapes[i] = sh
			printable := item.printable == nil || *item.printable

			if spec.printable != nil {
				printable = spec.printable[i]
			}

			object.Instances[i] = Instance{
				Index:         i,
				Item:          item.ordinal,
				Transform:     item.tr,
				Printable:     printable,
				Placement:     PlacementOutside,
				Bounds:        sh.box,
				Outline:       sh.outline,
				OutlineApprox: sh.approx,
			}
		}

		out[o] = object
	}

	return out
}

// sniffApplication reads the Application metadata from the top of the root
// model. A slicer writes metadata before the resources, so the read stops at
// the first resource and costs a few kilobytes.
func sniffApplication(f interface{ Open() (io.ReadCloser, error) }) string {
	body, openErr := f.Open()
	if openErr != nil {
		return ""
	}

	defer func() { _ = body.Close() }() // nothing was written.

	const sniffBytes = 256 << 10

	dec := xml.NewDecoder(io.LimitReader(body, sniffBytes))

	for {
		tok, err := dec.RawToken()
		if err != nil {
			return ""
		}

		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		switch {
		case start.Name.Local == "resources" || start.Name.Local == "build":
			return ""
		case start.Name.Local == "metadata" && isApplication(start):
			return metadataText(dec)
		}
	}
}

func isApplication(e xml.StartElement) bool {
	return slices.ContainsFunc(e.Attr, func(attr xml.Attr) bool {
		return attr.Name.Local == "name" && attr.Value == "Application"
	})
}

// maxApplicationBytes bounds the Application value.
const maxApplicationBytes = 1024

// metadataText reads the text of the element just started, up to its end.
func metadataText(dec *xml.Decoder) string {
	var value strings.Builder

	for {
		tok, err := dec.RawToken()
		if err != nil {
			return ""
		}

		switch t := tok.(type) {
		case xml.CharData:
			if value.Len() < maxApplicationBytes {
				value.Write(t)
			}
		case xml.EndElement:
			return strings.TrimSpace(value.String())
		}
	}
}

// lateApplication fills the producer from the Application metadata when the
// model wrote it after the resources, where sniffApplication does not look.
// Cura writes its metadata after the build. The dialect stays as detected: no
// program this package has a dialect for writes it there.
func lateApplication(p Producer, metadata []Metadata) Producer {
	if p.Application != "" {
		return p
	}

	app := strings.TrimSpace(metadataValue(metadata, "Application"))
	if app == "" {
		return p
	}

	late := producerOf(app)
	p.Application = app

	if p.Name == "" {
		p.Name, p.Version = late.Name, late.Version
	}

	return p
}

// producerOf splits an Application value into a name and a version. The
// version follows a dash, as in "PrusaSlicer-2.9.6" or
// "PrusaSlicer-3.0.0-alpha12+UNKNOWN", or a space or an underscore when it is
// dotted, as in "Ultimaker Cura 5.2.1" or "Creality_Print V5.1.4.10249
// Alpha", where a V before it and any word after it are left out.
func producerOf(app string) Producer {
	out := Producer{Application: app, Name: app}

	for i := 1; i < len(app); i++ {
		rest := app[i:]

		switch app[i-1] {
		case '-':
			if !isDigit(rest[0]) {
				continue
			}
		case ' ', '_':
			if len(rest) > 1 && (rest[0] == 'V' || rest[0] == 'v') && isDigit(rest[1]) {
				rest = rest[1:]
			}

			if !dotted(rest) {
				continue
			}

			rest, _, _ = strings.Cut(rest, " ")
		default:
			continue
		}

		out.Name = strings.TrimSpace(app[:i-1])
		out.Version, _, _ = strings.Cut(rest, "+")

		break
	}

	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// dotted reports whether s starts with a version such as 5.2: digits, a dot
// and a digit.
func dotted(s string) bool {
	digits := 0
	for digits < len(s) && isDigit(s[digits]) {
		digits++
	}

	return digits > 0 && digits+1 < len(s) && s[digits] == '.' && isDigit(s[digits+1])
}

// The versions the dialects are told apart by.
const (
	// ps3Major is the first major version of PrusaSlicer that writes the
	// project file of PrusaSlicer 3.0.
	ps3Major = 3
	// versionParts is how many numbers of a version are compared.
	versionParts = 2
)

// versionAtLeast compares a version such as "2.8.1" or "3.0.0-alpha12" with
// major.minor. A version whose parts are not numbers is not at least
// anything.
func versionAtLeast(version string, major, minor int) bool {
	core, _, _ := strings.Cut(version, "-")
	parts := strings.Split(core, ".")

	got := [versionParts]int{}

	for i := range min(len(parts), versionParts) {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return false
		}

		got[i] = n
	}

	if got[0] != major {
		return got[0] > major
	}

	return got[1] >= minor
}

func metadataValue(entries []Metadata, name string) string {
	for _, m := range entries {
		if m.Name == name {
			return m.Value
		}
	}

	return ""
}

// semverLess reports whether version a comes before b, by the rules of
// semantic versioning that PrusaSlicer's Semver follows: major, minor and
// patch as numbers, then a version with a pre-release such as -alpha1 before
// the same version without one. Build data after a plus is ignored. A version
// that is not a number is not less than anything.
func semverLess(a, b string) bool {
	ca, pa, oka := splitSemver(a)
	cb, pb, okb := splitSemver(b)

	if !oka || !okb {
		return false
	}

	for i := range ca {
		if ca[i] != cb[i] {
			return ca[i] < cb[i]
		}
	}

	switch {
	case pa == pb:
		return false
	case pa == "":
		return false
	case pb == "":
		return true
	}

	return prereleaseLess(pa, pb)
}

const semverParts = 3

func splitSemver(v string) ([semverParts]int, string, bool) {
	var out [semverParts]int

	v, _, _ = strings.Cut(strings.TrimSpace(v), "+")
	core, pre, _ := strings.Cut(v, "-")

	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > semverParts {
		return out, "", false
	}

	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, "", false
		}

		out[i] = n
	}

	return out, pre, true
}

// prereleaseLess compares two pre-release strings field by field: numbers as
// numbers, below words, and words as text.
func prereleaseLess(a, b string) bool {
	fa, fb := strings.Split(a, "."), strings.Split(b, ".")

	for i := range min(len(fa), len(fb)) {
		na, errA := strconv.Atoi(fa[i])
		nb, errB := strconv.Atoi(fb[i])

		switch {
		case errA == nil && errB == nil:
			if na != nb {
				return na < nb
			}
		case errA == nil:
			return true
		case errB == nil:
			return false
		case fa[i] != fb[i]:
			return fa[i] < fb[i]
		}
	}

	return len(fa) < len(fb)
}
