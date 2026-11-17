package threemf_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

// FuzzRead runs Read and Rewrite over damaged files. Neither may panic, and a
// file Read accepts must have a project Rewrite can use or refuse cleanly.
func FuzzRead(f *testing.F) {
	for _, path := range []string{fixture296, fixture281, fixture3Alpha} {
		f.Add(readFile(f, path))
	}

	f.Add(zipOf(f, entry{"3D/3dmodel.model", `<model><resources><object id="1"><components>` +
		`<component objectid="1"/></components></object></resources><build><item objectid="1"/></build></model>`}))
	f.Add([]byte("PK\x05\x06"))

	limits := threemf.Limits{
		MaxModelBytes: 1 << 20, MaxMetadataBytes: 1 << 20, MaxThumbnailBytes: 1 << 16,
		MaxVertices: 1 << 12, MaxResources: 256, MaxItems: 256, MaxMeshUses: 1 << 12,
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := threemf.Read(t.Context(), bytes.NewReader(data), int64(len(data)), limits)
		if err != nil {
			return
		}

		for s, setup := range p.Setups {
			for b := range setup.Beds {
				sel := threemf.Selection{Bed: threemf.BedRef{Setup: s, Bed: b}}
				_ = threemf.Rewrite(t.Context(), io.Discard, bytes.NewReader(data), int64(len(data)), p, sel, limits)
			}
		}
	})
}
