package threemf

import (
	"bytes"
	"context"
	"io"
	"strings"
)

// The relationship types a 3MF package uses, from the 3MF core specification
// and the Open Packaging Conventions it builds on.
const (
	relModel     = "http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"
	relThumbnail = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/thumbnail"
	rootRels     = "_rels/.rels"
	defaultModel = "3D/3dmodel.model"
)

// maxRelationships bounds how many relationships are kept.
const maxRelationships = 256

// relationships are what _rels/.rels points at.
type relationships struct {
	// model is the entry of the root model.
	model string
	// thumbnails are the entries the package names as its pictures.
	thumbnails []string
}

// readRelationships reads _rels/.rels. A package without one, or with one this
// package cannot read, falls back to the name every 3MF writer uses.
func readRelationships(ctx context.Context, a *archive) relationships {
	out := relationships{model: defaultModel}

	f := a.entry(rootRels)
	if f == nil {
		return out
	}

	data, err := readAll(ctx, f, a.limits.MaxMetadataBytes, "MaxMetadataBytes")
	if err != nil {
		return out
	}

	// The relationships are read with the tag scanner the rewrite uses, so a
	// caller that wants only the pictures does not link an XML decoder.
	var rels [][2]string

	err = scanTags(ctx, io.Discard, bytes.NewReader(data), func(t *tag) {
		if !t.closing && t.local() == "Relationship" && len(rels) < maxRelationships {
			rels = append(rels, [2]string{t.attrs.get("Target"), t.attrs.get("Type")})
		}
	})
	if err != nil {
		return out
	}

	found := false

	for _, rel := range rels {
		name, ok := resolve("", rel[0])
		if !ok || a.entry(name) == nil {
			continue
		}

		name = a.entry(name).Name

		switch strings.TrimSpace(rel[1]) {
		case relModel:
			if !found {
				out.model, found = name, true
			}
		case relThumbnail:
			out.thumbnails = append(out.thumbnails, name)
		}
	}

	return out
}
