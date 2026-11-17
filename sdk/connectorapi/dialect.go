package connectorapi

// A Dialect is what a driver type declares about its machines for the pages
// that add and show them. Every part of it is optional.

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrInvalidDialect reports a declaration the host could not serve as written.
var ErrInvalidDialect = errors.New("connectorapi: invalid dialect declaration")

// Needs is what a person supplies before a machine of one dialect can arrive.
// The set is closed. A page draws one form per word.
type Needs string

const (
	// NeedsSettingsFile is a machine that reads this server's address from a
	// file somebody copies onto it, and then calls in.
	NeedsSettingsFile Needs = "settings_file"
	// NeedsAddress is a machine this server calls, at an address somebody
	// declares.
	NeedsAddress Needs = "address"
	// NeedsNothing is a machine that arrives on its own. Something outside this
	// server told it where to call.
	NeedsNothing Needs = "none"
	// NeedsUnknown is the host's answer for a dialect that is not in this
	// build. A declaration must not use it.
	NeedsUnknown Needs = "unknown"
)

// NeedsWords returns every word the API may serve, in the order the reference
// lists them.
func NeedsWords() []Needs {
	return []Needs{NeedsSettingsFile, NeedsAddress, NeedsNothing, NeedsUnknown}
}

// Valid reports whether n is a member of the closed set.
func (n Needs) Valid() bool { return slices.Contains(NeedsWords(), n) }

// String returns the word as configuration and the API write it.
func (n Needs) String() string { return string(n) }

// Admission is how one dialect writes the two choices the first run offers:
// admit every machine that calls in, or keep each one waiting for an operator.
// The host writes the chosen value under the driver's own settings key.
// Declare all three fields or none.
type Admission struct {
	// Key is the settings key the driver reads the choice under.
	Key string `json:"key"`
	// Open is the value that admits every machine that calls in.
	Open string `json:"open"`
	// Ask is the value that keeps a machine waiting until an operator approves
	// it.
	Ask string `json:"ask"`
}

// IsZero reports whether the dialect declared no admission words. The host
// then leaves the settings as an operator wrote them.
func (a Admission) IsZero() bool { return a == Admission{} }

// Dialect is one driver type's declaration about the machines it talks to.
// Every field is optional.
type Dialect struct {
	// Brands is the names the machines are sold under. They are served as
	// written and never translated.
	Brands []string `json:"brands,omitempty"`
	// Needs is what a person supplies before a machine can arrive.
	Needs Needs `json:"needs,omitempty"`
	// Admission is how this dialect writes the first run's two choices.
	Admission Admission `json:"admission,omitzero"`
	// Models is every device model of this dialect that a page should be able
	// to name. The host serves them to any caller before sign-in, so declare
	// nothing that belongs to one deployment.
	Models []DeviceModel `json:"models,omitempty"`
	// States is every state a machine of this dialect reports at /state, the
	// moves between them, and the verbs each state accepts. The host serves it
	// in the device-state vocabulary.
	States StateSet `json:"states,omitzero"`
	// Resident says that every machine of this dialect is the machine the
	// server runs on. The host refuses to retire such a device. A driver that
	// reaches printers over a network leaves it false.
	Resident bool `json:"resident,omitempty"`
}

// MaxModelBytes is the longest spelling a declared model may have.
const MaxModelBytes = 128

// DeviceModel is one machine of a dialect, as a page names and draws it. The
// host fills in the connector from the registration's name.
type DeviceModel struct {
	// Model is the machine's own spelling, as the driver reports it in the
	// device record. A page matches a device to this row by it.
	Model string `json:"model"`
	// DisplayName is what the machine is sold as.
	DisplayName string `json:"display_name"`
	// Vendor is who makes it.
	Vendor string `json:"vendor"`
	// ToolChanger marks a machine whose numbered slots are separate toolheads,
	// so a page draws a row per tool. It adds no capability.
	ToolChanger bool `json:"tool_changer,omitempty"`
	// Artwork is a drawing of the machine, for a page that has none of its own.
	Artwork Artwork `json:"artwork,omitzero"`
}

// MaxArtworkBytes is the largest size of one drawing. The host keeps every
// drawing in memory.
const MaxArtworkBytes = 256 << 10

// The media types a drawing may have. The host serves a drawing with a policy
// that runs no script inside it.
const (
	ArtworkSVG  = "image/svg+xml"
	ArtworkPNG  = "image/png"
	ArtworkWebP = "image/webp"
)

// Artwork is a drawing of a machine. The zero value is no drawing. A driver
// embeds the file in its own module, usually with go:embed.
type Artwork struct {
	// MediaType is ArtworkSVG, ArtworkPNG or ArtworkWebP.
	MediaType string `json:"media_type,omitempty"`
	// Bytes is the whole file.
	Bytes []byte `json:"bytes,omitempty"`
}

// IsZero reports whether there is no drawing.
func (a Artwork) IsZero() bool { return a.MediaType == "" && len(a.Bytes) == 0 }

// IsZero reports whether the declaration says nothing at all.
func (d Dialect) IsZero() bool {
	return len(d.Brands) == 0 && d.Needs == "" && d.Admission.IsZero() && len(d.Models) == 0 &&
		d.States.IsZero() && !d.Resident
}

// ValidateDialect refuses a declaration the host cannot serve. Register panics
// on a refusal.
func ValidateDialect(d Dialect) error {
	for _, brand := range d.Brands {
		if strings.TrimSpace(brand) == "" {
			return fmt.Errorf("%w: a brand name is empty", ErrInvalidDialect)
		}
	}

	if d.Needs == NeedsUnknown {
		return fmt.Errorf("%w: %q is the host's answer for a dialect it does not have, "+
			"and a registered driver can always say what it needs", ErrInvalidDialect, d.Needs)
	}

	if d.Needs != "" && !d.Needs.Valid() {
		return fmt.Errorf("%w: it needs %q, and the words are %v",
			ErrInvalidDialect, d.Needs, NeedsWords())
	}

	if err := validateAdmission(d.Admission); err != nil {
		return err
	}

	if !d.States.IsZero() {
		if problems := d.States.Problems(); len(problems) > 0 {
			return fmt.Errorf("%w: the state set: %w", ErrInvalidDialect, errors.Join(problems...))
		}
	}

	return validateModels(d.Models)
}

// validateModels refuses a row a page could not name or join a device onto,
// and two rows that answer to one spelling.
func validateModels(models []DeviceModel) error {
	seen := make(map[string]string, len(models))

	for i, model := range models {
		for _, field := range [...]struct{ name, value string }{
			{"model", model.Model},
			{"display name", model.DisplayName},
			{"vendor", model.Vendor},
		} {
			if strings.TrimSpace(field.value) == "" {
				return fmt.Errorf("%w: Models[%d] (%q) has an empty %s",
					ErrInvalidDialect, i, model.Model, field.name)
			}
		}

		if len(model.Model) > MaxModelBytes {
			return fmt.Errorf("%w: Models[%d] is spelled in %d bytes, and a model takes at most %d",
				ErrInvalidDialect, i, len(model.Model), MaxModelBytes)
		}

		key := FoldModel(model.Model)
		if key == "" {
			return fmt.Errorf("%w: Models[%d] (%q) has no ASCII letter or digit to match a device by",
				ErrInvalidDialect, i, model.Model)
		}

		if other, ok := seen[key]; ok {
			return fmt.Errorf("%w: Models[%d] (%q) answers to the same spelling as %q",
				ErrInvalidDialect, i, model.Model, other)
		}

		if err := validateArtwork(model.Artwork); err != nil {
			return fmt.Errorf("%w: the drawing of Models[%d] (%q) %w", ErrInvalidDialect, i, model.Model, err)
		}

		seen[key] = model.Model
	}

	return nil
}

// validateArtwork refuses a drawing whose bytes do not match its media type.
func validateArtwork(art Artwork) error {
	if art.IsZero() {
		return nil
	}

	if len(art.Bytes) == 0 {
		return errors.New("is empty")
	}

	if len(art.Bytes) > MaxArtworkBytes {
		return fmt.Errorf("is %d bytes, over the %d a drawing may have", len(art.Bytes), MaxArtworkBytes)
	}

	switch art.MediaType {
	case ArtworkSVG:
		if !bytes.Contains(art.Bytes, []byte("<svg")) {
			return errors.New("is declared as SVG and holds no <svg element")
		}
	case ArtworkPNG:
		if !bytes.HasPrefix(art.Bytes, []byte("\x89PNG\r\n\x1a\n")) {
			return errors.New("is declared as PNG and does not start with the PNG signature")
		}
	case ArtworkWebP:
		if len(art.Bytes) < 12 || string(art.Bytes[:4]) != "RIFF" || string(art.Bytes[8:12]) != "WEBP" {
			return errors.New("is declared as WebP and does not start with the WebP signature")
		}
	default:
		return fmt.Errorf("has the media type %q, and a drawing is %s, %s or %s",
			art.MediaType, ArtworkSVG, ArtworkPNG, ArtworkWebP)
	}

	return nil
}

// FoldModel reduces a model spelling to its ASCII letters and digits,
// lowercased. The host treats two model names with the same fold as one
// machine. Do not fold a device type code: "1.3.10" and "13.1.0" would match.
func FoldModel(spelling string) string {
	var out strings.Builder

	out.Grow(len(spelling))

	for i := range len(spelling) {
		c := spelling[i]

		switch {
		case c >= 'A' && c <= 'Z':
			out.WriteByte(c - 'A' + 'a')
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			out.WriteByte(c)
		}
	}

	return out.String()
}

// validateAdmission accepts all three words or none.
func validateAdmission(a Admission) error {
	if a.IsZero() {
		return nil
	}

	if a.Key == "" || a.Open == "" || a.Ask == "" {
		return fmt.Errorf("%w: the admission words are key=%q open=%q ask=%q, and a dialect "+
			"declares all three or none", ErrInvalidDialect, a.Key, a.Open, a.Ask)
	}

	return nil
}

// clone copies every slice, so the registry keeps nothing the registering
// module can still write to.
func (d Dialect) clone() Dialect {
	out := d
	out.Brands = slices.Clone(d.Brands)
	out.Models = slices.Clone(d.Models)
	out.States = d.States.Clone()

	for i := range out.Models {
		out.Models[i].Artwork.Bytes = slices.Clone(out.Models[i].Artwork.Bytes)
	}

	return out
}
