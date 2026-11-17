package cameraapi_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// doc is a complete descriptor, so a case that expects a refusal is refused for
// the reason it names and not for a missing field.
func doc() extension.Doc {
	return extension.Doc{
		Summary:     "A camera for a test.",
		Description: "It answers a fixed frame and streams nothing.",
		Version:     "1.0.0",
		Contract:    cameraapi.Contract,
		Stability:   extension.StabilityStable,
	}
}

// fixed is a camera that answers one frame and declares whatever it was given.
type fixed struct {
	formats []cameraapi.Format
	shot    cameraapi.Snapshot
}

func (f *fixed) Formats() []cameraapi.Format { return slices.Clone(f.formats) }

func (f *fixed) Snapshot(context.Context) (cameraapi.Snapshot, error) { return f.shot, nil }

func (f *fixed) Open(context.Context, cameraapi.Format) (cameraapi.Stream, error) {
	return cameraapi.Stream{}, cameraapi.ErrFormatNotSupported
}

func (f *fixed) Close() error { return nil }

func jpegShot() cameraapi.Snapshot {
	return cameraapi.Snapshot{
		Bytes:     []byte{0xFF, 0xD8, 0xFF},
		MediaType: cameraapi.FormatJPEG.MediaType(),
		Taken:     time.Unix(1, 0).UTC(),
	}
}

// TestTheFormatVocabularyIsClosed is an earlier design decision's whole basis. A driver that
// could name a format of its own would be naming one no host knows how to
// serve, so a set is a negotiation vocabulary only while both sides read the
// same closed list.
func TestTheFormatVocabularyIsClosed(t *testing.T) {
	t.Parallel()

	for _, format := range cameraapi.Formats() {
		if !format.Known() {
			t.Errorf("Formats() names %q and Known() refuses it", format)
		}
	}

	for _, outside := range []cameraapi.Format{"", "avif", "JPEG", "h265", "jpeg "} {
		if outside.Known() {
			t.Errorf("Known() accepts %q, which is outside the vocabulary", outside)
		}
	}
}

// TestAServableFormatCarriesItsMediaType holds the half of a format that
// decides what a host writes on the wire. A format the host can serve says what
// it is; one it cannot says nothing, rather than a type a browser would guess
// at.
func TestAServableFormatCarriesItsMediaType(t *testing.T) {
	t.Parallel()

	cases := map[cameraapi.Format]string{
		cameraapi.FormatJPEG:       "image/jpeg",
		cameraapi.FormatMJPEG:      "multipart/x-mixed-replace; boundary=" + cameraapi.MJPEGBoundary,
		cameraapi.FormatH264AnnexB: "",
		cameraapi.FormatH264AVCC:   "",
		cameraapi.FormatYUYV:       "",
	}

	for format, want := range cases {
		if got := format.MediaType(); got != want {
			t.Errorf("Format(%q).MediaType() = %q, want %q", format, got, want)
		}
	}
}

// TestASnapshotThatIsNotAStillImageIsRefused is the check that stands between a
// device on somebody's network and this server's own origin. A driver chooses
// the media type, and a host that took its word would serve whatever a
// compromised camera decided to answer with.
func TestASnapshotThatIsNotAStillImageIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]cameraapi.Snapshot{
		"no bytes": {MediaType: "image/jpeg"},
		"html":     {Bytes: []byte("<script>"), MediaType: "text/html"},
		"svg, which a browser executes": {
			Bytes:     []byte("<svg onload=alert(1)>"),
			MediaType: "image/svg+xml",
		},
		"no media type at all": {Bytes: []byte{0xFF, 0xD8, 0xFF}},
	}

	for name, shot := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := shot.Validate(); !errors.Is(err, cameraapi.ErrInvalidSnapshot) {
				t.Fatalf("Validate() = %v, want ErrInvalidSnapshot", err)
			}
		})
	}

	if err := jpegShot().Validate(); err != nil {
		t.Fatalf("a still image was refused: %v", err)
	}
}

// TestAnOversizedMediaTypeIsQuotedInBounds keeps a refusal from becoming the
// payload. A driver that returned a megabyte of nonsense as its media type
// would otherwise write a megabyte into every log line that reported it.
func TestAnOversizedMediaTypeIsQuotedInBounds(t *testing.T) {
	t.Parallel()

	shot := cameraapi.Snapshot{Bytes: []byte{0xFF}, MediaType: strings.Repeat("x", 4096)}

	err := shot.Validate()
	if err == nil {
		t.Fatal("an unusable media type was accepted")
	}

	if len(err.Error()) > 256 {
		t.Fatalf("the refusal is %d bytes long, so a driver chooses how big a log line is", len(err.Error()))
	}
}

// TestACameraThatDeclaresNothingIsRefusedAtBuild holds the refusal where an
// operator can act on it. A camera that can emit nothing has no served path, so
// the first symptom would otherwise be a viewer refused for a reason the
// configuration does not explain.
func TestACameraThatDeclaresNothingIsRefusedAtBuild(t *testing.T) {
	t.Parallel()

	cases := map[string][]cameraapi.Format{
		"an empty set":       {},
		"a nil set":          nil,
		"an unknown format":  {"avif"},
		"one known, one not": {cameraapi.FormatJPEG, "h265"},
	}

	for name, formats := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			registration, err := cameraapi.Declare("fixed", doc(),
				func(cameraapi.ProviderSpec) (cameraapi.Camera, error) {
					return &fixed{formats: formats, shot: jpegShot()}, nil
				})
			if err != nil {
				t.Fatal(err)
			}

			if _, err = registration.Build(cameraapi.ProviderSpec{Type: "fixed"}); !errors.Is(
				err, cameraapi.ErrFormatNotSupported) {
				t.Fatalf("Build() = %v, want ErrFormatNotSupported", err)
			}
		})
	}
}

// TestARegisteredCameraIsBuiltFromItsSpec is the ordinary path, and the one
// that proves the refusals above are refusals and not a broken registry.
func TestARegisteredCameraIsBuiltFromItsSpec(t *testing.T) {
	t.Parallel()

	var seen cameraapi.ProviderSpec

	registration, err := cameraapi.Declare("fixed", doc(),
		func(spec cameraapi.ProviderSpec) (cameraapi.Camera, error) {
			seen = spec

			return &fixed{formats: []cameraapi.Format{cameraapi.FormatJPEG}, shot: jpegShot()}, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	registry, err := cameraapi.NewRegistryFrom(registration)
	if err != nil {
		t.Fatal(err)
	}

	camera, err := registry.BuildProvider(cameraapi.ProviderSpec{
		ID: "workshop", Type: "fixed", Printer: "mk4-017",
		Config: map[string]any{"url": "http://camera.invalid/snap.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = camera.Close() }()

	if seen.Printer != "mk4-017" {
		t.Errorf("the driver was given Printer %q, and the host read %q from the declaration",
			seen.Printer, "mk4-017")
	}

	if seen.ID != "workshop" {
		t.Errorf("the driver was given ID %q, want workshop", seen.ID)
	}

	shot, err := camera.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if err = shot.Validate(); err != nil {
		t.Fatal(err)
	}
}

// TestAnUnknownCameraTypeNamesWhatThisBuildHas is what an operator sees after a
// typo. A refusal that only says no leaves them reading source to find the
// name.
func TestAnUnknownCameraTypeNamesWhatThisBuildHas(t *testing.T) {
	t.Parallel()

	registration, err := cameraapi.Declare("fixed", doc(),
		func(cameraapi.ProviderSpec) (cameraapi.Camera, error) {
			return &fixed{formats: []cameraapi.Format{cameraapi.FormatJPEG}, shot: jpegShot()}, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	registry, err := cameraapi.NewRegistryFrom(registration)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = registry.Lookup("fixxed"); !errors.Is(err, cameraapi.ErrUnknownProvider) {
		t.Fatalf("Lookup() = %v, want ErrUnknownProvider", err)
	}

	if registry.Registered("fixxed") {
		t.Error("Registered() answers true for a name nothing registered")
	}

	if !registry.Registered("fixed") {
		t.Error("Registered() answers false for the name that was registered")
	}
}

// The contract's carrier types are the standard library's, which is what makes
// a camera driver compilable by anyone. It is asserted at compilation rather
// than in a case, because an import list is what internal/arch already holds and
// this is the other half: a Stream is an io.ReadCloser and a Snapshot is bytes,
// so no codec type can appear in a driver's signature.
var (
	_ io.ReadCloser = cameraapi.Stream{}.Body
	_ []byte        = cameraapi.Snapshot{}.Bytes
	_ time.Time     = cameraapi.Snapshot{}.Taken
)
