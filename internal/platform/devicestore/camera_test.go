package devicestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
)

// TestACameraRowGoesWithItsDevice holds ADR-20261116-g5x rule 1 on both
// engines: a camera row is written with its device, reads back what it was
// given, and goes when the device does. A camera device with no row is bare.
func TestACameraRowGoesWithItsDevice(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		f := newStore(t, e)

		door := Camera{ID: "door", Type: "mjpeg", Config: json.RawMessage(`{"printer":"p1"}`), Generation: 2}
		require.NoError(t, f.graph.DeclareDevice(t.Context(), authz.DeviceKindCamera,
			authz.DeclaredDevice{ID: "door", Name: "door"},
			func(ctx context.Context, tx *sql.Tx) error { return f.PutCamera(ctx, tx, door) }))
		require.NoError(t, f.graph.DeclareDevice(t.Context(), authz.DeviceKindCamera,
			authz.DeclaredDevice{ID: "shed", Name: "shed"}, nil))

		cameras, err := f.Cameras(t.Context())
		require.NoError(t, err)
		require.Len(t, cameras, 1)
		require.Equal(t, "mjpeg", cameras[0].Type)
		require.Equal(t, int64(2), cameras[0].Generation)
		require.JSONEq(t, `{"printer":"p1"}`, string(cameras[0].Config))

		bare, err := f.BareCameras(t.Context())
		require.NoError(t, err)
		require.Equal(t, []string{"shed"}, bare)

		require.NoError(t, f.RemoveDevice(t.Context(), "door"))

		cameras, err = f.Cameras(t.Context())
		require.NoError(t, err)
		require.Empty(t, cameras, "the camera went with its device")
	})
}
