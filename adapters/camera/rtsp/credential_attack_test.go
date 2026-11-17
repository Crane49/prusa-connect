package rtsp_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/camera/rtsp"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
)

// TestTheSnapshotCredentialNeverReachesAStringEither is the other URL an
// operator writes a password into.
//
// Config.URL had its userinfo lifted out into the closure, because "a string
// field holding one is printed in full by every %v that ever reaches the
// struct". Config.SnapshotURL is the same field, of the same type, in the same
// struct, and it is taken verbatim: checkSnapshotURL checks the scheme and the
// host and does not touch parsed.User, so a credential written there is still
// sitting in the struct that %+v walks, and in stills.endpoint after it.
//
// http://user:pass@host/snapshot.jpg is how every camera that answers a still
// image over HTTP documents its own endpoint, so it is the spelling an operator
// reaches for.
func TestTheSnapshotCredentialNeverReachesAStringEither(t *testing.T) {
	t.Parallel()

	const secret = "hunter2-do-not-print"

	camera, err := rtsp.New(cameraapi.ProviderSpec{
		ID: "watch", Type: rtsp.Type, Printer: "p1",
		Config: map[string]any{
			"url":          "rtsp://192.168.1.50/live",
			"snapshot_url": "http://operator:" + secret + "@192.168.1.50/snapshot.jpg",
		},
	})
	require.NoError(t, err)

	require.NotContains(t, fmt.Sprintf("%+v", camera), secret,
		"the snapshot URL's password is in the struct a supervisor error and a panic dump both print")
	require.NotContains(t, fmt.Sprintf("%#v", camera), secret)

	// And a refusal from an address nothing answers must not carry it either,
	// which is what the URL half of this already promises.
	_, err = camera.Snapshot(t.Context())
	if err != nil {
		require.NotContains(t, err.Error(), secret)
	}
}
