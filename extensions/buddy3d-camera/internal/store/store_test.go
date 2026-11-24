package store_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/prusa3d/buddy3d-camera/internal/store"
)

const fingerprint = "19aa6bb40cb11836dabbb2db33625305"

// The first request binds the fingerprint, a restart keeps it, and the token
// stops being shown once it is bound.
func TestTheFirstRequestBindsTheFingerprint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	cameras, err := store.Open(dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	paired, err := cameras.Pair("bed-cam", "printer-1", "")
	if err != nil {
		t.Fatal(err)
	}

	if len(paired.Token) != 20 || cameras.List()[0].Token != paired.Token {
		t.Fatalf("paired %+v, listed %+v", paired, cameras.List())
	}

	if _, err = cameras.Authenticate(paired.Token, fingerprint); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = reopened.Authenticate(paired.Token, "another-fingerprint-0000"); !errors.Is(err, store.ErrFingerprint) {
		t.Fatalf("another fingerprint after a restart: %v", err)
	}

	if entry := reopened.List()[0]; !entry.Bound || entry.Token != "" {
		t.Fatalf("a bound camera is listed as %+v", entry)
	}
}

// A token no camera used within the pairing window is refused, as the cloud
// refuses it.
func TestAnUnusedTokenExpires(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	cameras, err := store.Open(t.TempDir(), clock)
	if err != nil {
		t.Fatal(err)
	}

	paired, err := cameras.Pair("bed-cam", "printer-1", "")
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(store.PairingWindow + time.Second)

	if _, err = cameras.Authenticate(paired.Token, fingerprint); !errors.Is(err, store.ErrWindowClosed) {
		t.Fatalf("got %v", err)
	}
}

// A camera that already holds a token is paired with it, and no two cameras
// share one.
func TestATokenTheCameraHoldsIsTaken(t *testing.T) {
	t.Parallel()

	cameras, err := store.Open(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = cameras.Pair("bed-cam", "printer-1", "0123456789abcdefghij"); err != nil {
		t.Fatal(err)
	}

	if _, err = cameras.Pair("door-cam", "printer-1", "0123456789abcdefghij"); !errors.Is(err, store.ErrTokenTaken) {
		t.Fatalf("a shared token: %v", err)
	}

	if _, err = cameras.Pair("door-cam", "printer-1", "short"); !errors.Is(err, store.ErrBadToken) {
		t.Fatalf("a short token: %v", err)
	}
}

// A camera's interval is kept on disk and across pairing it again, and one
// the camera does not take is refused.
func TestACamerasIntervalIsKept(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	cameras, err := store.Open(dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = cameras.Pair("bed-cam", "printer-1", ""); err != nil {
		t.Fatal(err)
	}

	if err = cameras.SetSettings(
		"bed-cam",
		store.Settings{SnapshotInterval: 5},
	); !errors.Is(
		err,
		store.ErrBadInterval,
	) {
		t.Fatalf("an interval under the firmware's 10 s: %v", err)
	}

	if err = cameras.SetSettings(
		"bed-cam",
		store.Settings{SnapshotInterval: 256},
	); !errors.Is(
		err,
		store.ErrBadInterval,
	) {
		t.Fatalf("256: %v", err)
	}

	if err = cameras.SetSettings("bed-cam", store.Settings{Stream: "hls"}); !errors.Is(err, store.ErrBadStream) {
		t.Fatalf("a stream the camera does not run: %v", err)
	}

	if err = cameras.SetSettings("bed-cam", store.Settings{SnapshotInterval: 10, Stream: "rtsp"}); err != nil {
		t.Fatal(err)
	}

	if _, err = cameras.Pair("bed-cam", "printer-1", ""); err != nil {
		t.Fatal(err)
	}

	again, err := store.Open(dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	if got := again.Settings("bed-cam"); got != (store.Settings{SnapshotInterval: 10, Stream: "rtsp"}) {
		t.Fatalf("after pairing again and reopening the settings are %+v", got)
	}
}

// The camera reports its features on every session, and a restart of the
// module keeps them, so its controls show before it reports them again.
func TestACamerasFeaturesAreKept(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	cameras, err := store.Open(dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = cameras.Pair("bed-cam", "printer-1", ""); err != nil {
		t.Fatal(err)
	}

	if err = cameras.SetFeatures("bed-cam", []string{"RotationZ", "IrMode"}); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	if got := reopened.List()[0].Features; !slices.Equal(got, []string{"RotationZ", "IrMode"}) {
		t.Fatalf("features after a restart: %q", got)
	}

	if err = cameras.SetFeatures("no-such-cam", nil); !errors.Is(err, store.ErrNoSuchCamera) {
		t.Fatalf("a camera nobody paired: %v", err)
	}
}
