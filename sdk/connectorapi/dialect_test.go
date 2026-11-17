package connectorapi_test

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// aDialect is a complete declaration, so a case below changes one member and
// tests that member alone.
func aDialect() connectorapi.Dialect {
	return connectorapi.Dialect{
		Brands: []string{"Bench Works"},
		Needs:  connectorapi.NeedsSettingsFile,
		Admission: connectorapi.Admission{
			Key:  "mode",
			Open: "none",
			Ask:  "token",
		},
		Models: []connectorapi.DeviceModel{aModel()},
	}
}

// aModel is a complete model row, so a case below empties one field and tests
// that field alone.
func aModel() connectorapi.DeviceModel {
	return connectorapi.DeviceModel{
		Model:       "Bench_One",
		DisplayName: "Bench Works Tool Changer",
		Vendor:      "Bench Works",
		ToolChanger: true,
		Artwork: connectorapi.Artwork{
			MediaType: connectorapi.ArtworkSVG,
			Bytes:     []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		},
	}
}

// TestADeclaredDialectReachesTheRegistration is the whole point of the
// declaration: a host that looked a driver type up reads what its author wrote
// without knowing which dialect it is.
func TestADeclaredDialectReachesTheRegistration(t *testing.T) {
	t.Parallel()

	registration, err := connectorapi.Declare(freshName("declaring-dialect"), testDoc(),
		func(connectorapi.Spec) (connectorapi.Driver, error) { return dialledInto{}, nil },
		aDialect())
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	got := registration.Dialect()
	if got.Needs != connectorapi.NeedsSettingsFile {
		t.Errorf("the declaration needs %q, want %q", got.Needs, connectorapi.NeedsSettingsFile)
	}

	if len(got.Brands) != 1 || got.Brands[0] != "Bench Works" {
		t.Errorf("the declaration names the brands %v", got.Brands)
	}

	if got.Admission != (connectorapi.Admission{Key: "mode", Open: "none", Ask: "token"}) {
		t.Errorf("the declaration spells admission as %+v", got.Admission)
	}

	if len(got.Models) != 1 || !reflect.DeepEqual(got.Models[0], aModel()) {
		t.Errorf("the declaration names the models %+v", got.Models)
	}
}

// TestADriverThatDeclaresNoDialectSaysNothing is the shape every driver
// registered before the declaration existed keeps. It is not an error, and the
// host serves what it can answer on its own.
func TestADriverThatDeclaresNoDialectSaysNothing(t *testing.T) {
	t.Parallel()

	registration := mustDeclare(t, freshName("silent-dialect"))

	if !registration.Dialect().IsZero() {
		t.Errorf("a driver that declared nothing answers %+v", registration.Dialect())
	}
}

// TestAResidentDialectReachesTheRegistration covers a declaration whose only
// member is Resident. IsZero must not report it as empty (ADR-20261103-szc).
func TestAResidentDialectReachesTheRegistration(t *testing.T) {
	t.Parallel()

	registration, err := connectorapi.Declare(freshName("resident-dialect"), testDoc(),
		func(connectorapi.Spec) (connectorapi.Driver, error) { return dialledInto{}, nil },
		connectorapi.Dialect{Resident: true})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	if got := registration.Dialect(); !got.Resident || got.IsZero() {
		t.Errorf("a resident declaration answers %+v, IsZero %v", got, got.IsZero())
	}
}

// TestTheDeclarationIsCopiedAwayFromTheAuthor holds the registry against a
// module that keeps writing to the slice it registered. A registry that shared
// the backing array would answer a different list later in the process.
func TestTheDeclarationIsCopiedAwayFromTheAuthor(t *testing.T) {
	t.Parallel()

	brands := []string{"Bench Works"}

	registration, err := connectorapi.Declare(freshName("mutating-dialect"), testDoc(),
		func(connectorapi.Spec) (connectorapi.Driver, error) { return dialledInto{}, nil },
		connectorapi.Dialect{Brands: brands})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	brands[0] = "Somebody Else"

	if got := registration.Dialect().Brands[0]; got != "Bench Works" {
		t.Errorf("the registration answers %q after the author overwrote its slice", got)
	}
}

// TestTheDeclaredModelsAreCopiedAwayFromTheAuthor is the same hold over the
// model rows, which the host reads once at start and serves for the life of
// the process.
func TestTheDeclaredModelsAreCopiedAwayFromTheAuthor(t *testing.T) {
	t.Parallel()

	models := []connectorapi.DeviceModel{aModel()}

	registration, err := connectorapi.Declare(freshName("mutating-models"), testDoc(),
		func(connectorapi.Spec) (connectorapi.Driver, error) { return dialledInto{}, nil },
		connectorapi.Dialect{Models: models})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	models[0].Model = "Somebody_Else"
	models[0].Artwork.Bytes[1] = 'X'

	got := registration.Dialect().Models[0]
	if got.Model != "Bench_One" {
		t.Errorf("the registration answers %q after the author overwrote its slice", got.Model)
	}

	if !bytes.Equal(got.Artwork.Bytes, aModel().Artwork.Bytes) {
		t.Errorf("the registration serves %q after the author overwrote the drawing", got.Artwork.Bytes)
	}
}

// TestAnUnservableDeclarationIsRefused is every way a declaration is wrong,
// each refused at the author's own build rather than at somebody's deployment.
func TestAnUnservableDeclarationIsRefused(t *testing.T) {
	t.Parallel()

	for name, broken := range map[string]connectorapi.Dialect{
		"an empty brand":            {Brands: []string{"Bench Works", " "}},
		"a needs word nobody knows": {Needs: connectorapi.Needs("usb-stick")},
		"the host's own word":       {Needs: connectorapi.NeedsUnknown},
		"half the admission words": {
			Admission: connectorapi.Admission{Key: "mode", Open: "none"},
		},
		"a model with no spelling":     {Models: []connectorapi.DeviceModel{withModel(func(m *connectorapi.DeviceModel) { m.Model = " " })}},
		"a model with no display name": {Models: []connectorapi.DeviceModel{withModel(func(m *connectorapi.DeviceModel) { m.DisplayName = "" })}},
		"a model with no vendor":       {Models: []connectorapi.DeviceModel{withModel(func(m *connectorapi.DeviceModel) { m.Vendor = "" })}},
		"a spelling with nothing to match": {
			Models: []connectorapi.DeviceModel{withModel(func(m *connectorapi.DeviceModel) { m.Model = "--" })},
		},
		"a spelling longer than a path takes": {
			Models: []connectorapi.DeviceModel{withModel(func(m *connectorapi.DeviceModel) {
				m.Model = strings.Repeat("a", connectorapi.MaxModelBytes+1)
			})},
		},
		"a drawing of a type a page cannot draw": {Models: []connectorapi.DeviceModel{withArtwork("image/gif", []byte("GIF89a"))}},
		"a drawing that is not what it claims":   {Models: []connectorapi.DeviceModel{withArtwork(connectorapi.ArtworkPNG, []byte("<svg/>"))}},
		"a picture claiming to be an SVG": {Models: []connectorapi.DeviceModel{
			withArtwork(connectorapi.ArtworkSVG, []byte("\x89PNG\r\n\x1a\n")),
		}},
		"a RIFF file that is not a WebP": {Models: []connectorapi.DeviceModel{
			withArtwork(connectorapi.ArtworkWebP, []byte("RIFF\x24\x00\x00\x00WAVEfmt ")),
		}},
		"a WebP too short to carry its type": {Models: []connectorapi.DeviceModel{
			withArtwork(connectorapi.ArtworkWebP, []byte("RIFF")),
		}},
		"a drawing with no bytes": {Models: []connectorapi.DeviceModel{withArtwork(connectorapi.ArtworkSVG, nil)}},
		"a drawing too large to serve": {Models: []connectorapi.DeviceModel{withArtwork(connectorapi.ArtworkSVG,
			append([]byte("<svg>"), make([]byte, connectorapi.MaxArtworkBytes)...))}},
		"two models answering to one spelling": {Models: []connectorapi.DeviceModel{
			aModel(),
			withModel(func(m *connectorapi.DeviceModel) { m.Model = "bench-one" }),
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := connectorapi.ValidateDialect(broken); !errors.Is(err, connectorapi.ErrInvalidDialect) {
				t.Fatalf("%+v was accepted: %v", broken, err)
			}
		})
	}
}

// TestTheLongestSpellingIsAccepted is the other side of the bound, so the
// refusal above is the length and not something else about the spelling.
func TestTheLongestSpellingIsAccepted(t *testing.T) {
	t.Parallel()

	longest := withModel(func(m *connectorapi.DeviceModel) {
		m.Model = strings.Repeat("a", connectorapi.MaxModelBytes)
	})

	err := connectorapi.ValidateDialect(connectorapi.Dialect{Models: []connectorapi.DeviceModel{longest}})
	if err != nil {
		t.Fatalf("a spelling of %d bytes was refused: %v", connectorapi.MaxModelBytes, err)
	}
}

// TestARefusedModelIsNamedByItsRow is the author reading the panic. A model
// with no spelling has nothing to name it by but where it sits in the list.
func TestARefusedModelIsNamedByItsRow(t *testing.T) {
	t.Parallel()

	err := connectorapi.ValidateDialect(connectorapi.Dialect{Models: []connectorapi.DeviceModel{
		aModel(),
		withModel(func(m *connectorapi.DeviceModel) { m.Model = "" }),
	}})
	if err == nil || !strings.Contains(err.Error(), "Models[1]") {
		t.Fatalf("the refusal does not say which row: %v", err)
	}
}

// TestADriverDeclaringTwoDialectsIsRefused is why the parameter is one or none.
// Two declarations for one type are two answers to the same question, and the
// host would have to pick one.
func TestADriverDeclaringTwoDialectsIsRefused(t *testing.T) {
	t.Parallel()

	_, err := connectorapi.Declare(freshName("two-dialects"), testDoc(),
		func(connectorapi.Spec) (connectorapi.Driver, error) { return dialledInto{}, nil },
		aDialect(), aDialect())
	if !errors.Is(err, connectorapi.ErrInvalidDialect) {
		t.Fatalf("two declarations were accepted: %v", err)
	}
}

// TestEveryNeedsWordIsValid holds the closed set against itself, so a word
// added to the list and not to Valid cannot reach a page that has no form
// for it.
func TestEveryNeedsWordIsValid(t *testing.T) {
	t.Parallel()

	words := connectorapi.NeedsWords()
	if len(words) != 4 {
		t.Fatalf("the closed set holds %d words: %v", len(words), words)
	}

	for _, word := range words {
		if !word.Valid() {
			t.Errorf("%q is in the list and Valid refuses it", word)
		}
	}

	if connectorapi.Needs("address-or-file").Valid() {
		t.Error("a word outside the list is accepted")
	}
}

// withModel is aModel with one change, so a refusal case names only what is
// wrong with it.
func withModel(change func(*connectorapi.DeviceModel)) connectorapi.DeviceModel {
	model := aModel()
	change(&model)

	return model
}

// withArtwork is aModel with another drawing.
func withArtwork(mediaType string, file []byte) connectorapi.DeviceModel {
	return withModel(func(m *connectorapi.DeviceModel) {
		m.Artwork = connectorapi.Artwork{MediaType: mediaType, Bytes: file}
	})
}

// TestEveryDrawingTypeIsAccepted holds the three types a page draws, each by
// the bytes that open a real file of that type.
func TestEveryDrawingTypeIsAccepted(t *testing.T) {
	t.Parallel()

	for mediaType, file := range map[string][]byte{
		connectorapi.ArtworkSVG:  []byte(`<?xml version="1.0"?><svg/>`),
		connectorapi.ArtworkPNG:  []byte("\x89PNG\r\n\x1a\n...."),
		connectorapi.ArtworkWebP: []byte("RIFF\x00\x00\x00\x00WEBPVP8 "),
	} {
		dialect := connectorapi.Dialect{Models: []connectorapi.DeviceModel{withArtwork(mediaType, file)}}
		if err := connectorapi.ValidateDialect(dialect); err != nil {
			t.Errorf("a %s drawing was refused: %v", mediaType, err)
		}
	}
}

// TestFoldModelKeepsOnlyLettersAndDigits holds the comparison a driver's rows
// and the host's profile lookup share. A digit is kept, so two models that
// differ by one stay apart.
func TestFoldModelKeepsOnlyLettersAndDigits(t *testing.T) {
	t.Parallel()

	for spelling, want := range map[string]string{
		"CoreOne_INDX": "coreoneindx",
		"core one":     "coreone",
		"MK4S":         "mk4s",
		"MK4":          "mk4",
		"--":           "",
	} {
		if got := connectorapi.FoldModel(spelling); got != want {
			t.Errorf("FoldModel(%q) = %q, want %q", spelling, got, want)
		}
	}
}
