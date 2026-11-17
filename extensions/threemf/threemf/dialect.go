package threemf

import (
	"context"
	"fmt"
	"io"
)

// dialect reads what one slicer adds to the 3MF core. A new slicer is a new
// type implementing it, added to dialects. ADR-20261026-w6a.
//
// The steps run in this order for every Read:
//
//  1. prepare reads the slicer's own files, before the model is parsed. It may
//     set reading.ranges to ask for the vertex runs of mesh parts.
//  2. objects groups the build items into objects and their parts.
//  3. place builds the printer setups and their beds, and finds the bed of
//     every instance.
//
// rewrite writes a copy of the file for a Selection, changing as few entries
// as it can.
type dialect interface {
	name() Dialect
	// matches reports whether the dialect reads files from this producer, or
	// with these entries.
	matches(p Producer, a *archive) bool
	prepare(rd *reading) error
	objects(rd *reading) ([]objectSpec, error)
	place(rd *reading, p *Project, specs []objectSpec)
	// thumbnail says which bed a picture shows, and false when it shows the
	// whole project or is not one of the slicer's pictures.
	thumbnail(path string) (BedRef, bool)
	rewrite(ctx context.Context, rw *rewriting) error
}

// dialects are tried in this order, and generic takes what none of them
// matches.
func dialects() []dialect {
	return []dialect{&prusa3{}, &prusa2{}, &orca{}}
}

func detect(p Producer, a *archive) dialect {
	for _, d := range dialects() {
		if d.matches(p, a) {
			return d
		}
	}

	return &generic{}
}

// generic reads a file from a program this package has no dialect for. Each
// object a build item names is an object, each mesh it is made of is a model
// part, and every instance stands on the one bed of the one setup. A program
// with a dialect of its own, such as one that stores plates, is read this way
// until it has one.
type generic struct{}

func (*generic) name() Dialect                   { return DialectGeneric }
func (*generic) matches(Producer, *archive) bool { return false }
func (*generic) prepare(*reading) error          { return nil }

func (*generic) objects(rd *reading) ([]objectSpec, error) {
	return groupByObject(rd, func(*resource, component) PartType { return PartModel })
}

func (*generic) place(_ *reading, p *Project, _ []objectSpec) {
	bed := Bed{Index: 0}

	for o := range p.Objects {
		for i := range p.Objects[o].Instances {
			inst := &p.Objects[o].Instances[i]
			inst.Bed = &BedRef{}
			inst.Placement = PlacementUnknown
			bed.Instances = append(bed.Instances, InstanceRef{Object: o, Instance: i})
		}
	}

	p.Setups = []Setup{{Index: 0, Beds: []Bed{bed}}}
}

func (*generic) thumbnail(string) (BedRef, bool) { return BedRef{}, false }

func (*generic) rewrite(context.Context, *rewriting) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, DialectGeneric)
}

// groupByObject makes one object of every resource the build items name, in
// the order the build first names them. Its parts are its components, each
// typed by partType, or the object itself when it is a mesh.
func groupByObject(rd *reading, partType func(*resource, component) PartType) ([]objectSpec, error) {
	root := rd.models.root
	index := map[resKey]int{}

	var specs []objectSpec

	for _, item := range root.items {
		res := rd.models.resource(item.ref)
		if res == nil {
			rd.issue(IssueUnknownReference, "build item")

			continue
		}

		o, seen := index[item.ref]
		if !seen {
			parts, err := partsOf(rd, res, partType)
			if err != nil {
				return nil, err
			}

			o = len(specs)
			index[item.ref] = o
			specs = append(specs, objectSpec{name: res.name, resourceID: res.key.id, parts: parts})
		}

		specs[o].items = append(specs[o].items, item)
	}

	return specs, nil
}

// partsOf lists the parts of an object: itself when it holds a mesh, and one
// part for every component.
func partsOf(rd *reading, res *resource, partType func(*resource, component) PartType) ([]partSpec, error) {
	var parts []partSpec

	if res.mesh != nil {
		parts = append(parts, partSpec{
			part:   Part{Name: res.name, Type: PartModel},
			pieces: []piece{{mesh: res.mesh, lo: 0, hi: -1, tlast: -1, tr: Identity}},
			id:     res.key.id,
		})
	}

	if err := rd.useMeshes(len(parts)); err != nil {
		return nil, err
	}

	for _, c := range res.comps {
		pieces, err := rd.models.leaves(c.ref, c.tr, 1, rd.limits, nil)
		if err != nil {
			return nil, err
		}

		if err = rd.useMeshes(len(pieces)); err != nil {
			return nil, err
		}

		name := ""
		if target := rd.models.resource(c.ref); target != nil {
			name = target.name
		} else {
			rd.issue(IssueUnknownReference, "component")
		}

		parts = append(parts, partSpec{part: Part{Name: name, Type: partType(res, c)}, pieces: pieces, id: c.ref.id})
	}

	return parts, nil
}

// thumbnails reads the pictures: the ones the package names, the one every
// PrusaSlicer writes, and the ones a dialect knows. Each is read once, and a
// picture that cannot be read is an issue.
func (rd *reading) thumbnails(d dialect) []Thumbnail {
	names := thumbnailNames(rd.a, rd.rels)

	var out []Thumbnail

	for _, name := range names {
		if len(out) >= rd.limits.MaxThumbnails {
			break
		}

		thumb, err := readThumbnail(rd.ctx, rd.a, name)
		if err != nil {
			rd.issue(IssueThumbnailUnreadable, name)

			continue
		}

		if ref, ok := d.thumbnail(name); ok {
			thumb.Bed = &ref
		}

		out = append(out, thumb)
	}

	return out
}

// Thumbnails reads only the pictures of the 3MF file in r, without reading the
// model. It is the cheap read for a listing that shows a picture per file.
// The best picture comes first.
func Thumbnails(ctx context.Context, r io.ReaderAt, size int64, limits Limits) ([]Thumbnail, error) {
	limits = limits.withDefaults()

	a, err := openArchive(r, size, limits)
	if err != nil {
		return nil, err
	}

	var out []Thumbnail

	for _, name := range thumbnailNames(a, readRelationships(ctx, a)) {
		if len(out) >= limits.MaxThumbnails {
			break
		}

		if err = ctx.Err(); err != nil {
			return out, err //nolint:wrapcheck // the caller's own cancellation.
		}

		var thumb Thumbnail
		thumb, err = readThumbnail(ctx, a, name)
		if err == nil {
			out = append(out, thumb)
		}
	}

	return out, nil
}
