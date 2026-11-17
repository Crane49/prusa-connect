package profile

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// Catalogue is the device models this process serves: the rows ModelDocs
// returns and the rows the drivers in this build declared.
//
// The zero value holds the core's rows alone, which is a build whose drivers
// declare no model. It is read-only once placed. Place copies the rows, and the
// drawing bytes stay shared with the registration that declared them, which
// hands out a copy of its own and never writes it. A reader writes neither.
type Catalogue struct {
	declared []ModelDoc
	refused  []Refusal
}

// Refusal is a declared row Place left out, and what it collided with. Either
// Profile is set, with Code as the field below says, or Drivers is.
type Refusal struct {
	// Model is the spelling as the driver declared it.
	Model string
	// Connector is the bare name of the dialect that declared it.
	Connector string
	// DeclaredBy is the qualified name of the driver that declared it.
	DeclaredBy string
	// Profile is the Prusa model the spelling collides with. The core names
	// that machine, so the driver's row is the one left out.
	Profile string
	// Code is the device type code of Profile when the spelling matched it
	// only with the dots left out. A spelling that is the code as written is
	// that model to the profile lookup, and Code stays empty.
	Code string
	// Drivers are the qualified names of the other drivers whose declared
	// spelling folds to this one. None of them is placed.
	Drivers []string
}

// CollidesWith names what already answers to the spelling, for a person
// reading the log or the served vocabulary.
func (r Refusal) CollidesWith() string {
	switch {
	case r.Code != "":
		return fmt.Sprintf("the device type code %q of the Prusa model %q", r.Code, r.Profile)
	case r.Profile != "":
		return fmt.Sprintf("the Prusa model %q", r.Profile)
	case len(r.Drivers) == 1:
		return "the driver " + r.Drivers[0]
	default:
		return "the drivers " + strings.Join(r.Drivers, ", ")
	}
}

// Remedy is what the person who composed the build does about the refusal.
func (r Refusal) Remedy() string {
	switch {
	case r.Code != "":
		return "a page would read the spelling as that code, so have the driver report another " +
			"spelling for the model"
	case r.Profile != "":
		return "the core names this machine already, so have the driver's author drop the model, " +
			"or leave the driver out of the build"
	default:
		return "rename the model in all but one of the drivers, or leave all but one of them out " +
			"of the build"
	}
}

// Place checks the declared rows against the core's and against one another,
// and returns the catalogue to serve. A refused row is left out, the rest are
// served, and the catalogue keeps the refusals to serve beside them. A bad
// declaration refuses the row and never the start of the server, and the
// refusal is named where the rows are read.
//
// A declared spelling collides with the core when the profile lookup resolves
// it, by name, by device type code or by an alias, because the host then
// treats a device reporting it as that profiled model. It collides when it
// folds to a core row's model, which matters in a build that carries fewer
// profiles than rows. And it collides when it folds to the device type code of
// a profile the build loads, with the dots left out, because a page folds the
// code a device reports that way.
// Only the declared row goes, and the Prusa row stays.
//
// Two declared spellings collide when they fold to one. Both go: keeping one
// would let the order of init calls pick which driver a page shows.
func Place(declared ...ModelDoc) (Catalogue, error) {
	registry, err := Builtin()
	if err != nil {
		return Catalogue{}, fmt.Errorf("read the built-in profiles: %w", err)
	}

	// The rows are read as well as the profiles: a build for one printer loads
	// one profile and still serves every row.
	names := map[string]string{}
	for _, doc := range ModelDocs() {
		names[normaliseModel(doc.Model)] = doc.Model
	}

	// The lookup keeps a code's dots, so the codes are folded here.
	codes := map[string]Refusal{}

	for _, entry := range registry.Profiles {
		if code := normaliseModel(entry.DeviceTypeID); code != "" {
			codes[code] = Refusal{Profile: entry.Model, Code: entry.DeviceTypeID}
		}
	}

	var refused []Refusal

	byKey := map[string][]ModelDoc{}

	var keys []string

	for _, doc := range declared {
		if refusal, ok := collidesWithCore(registry, names, codes, doc.Model); ok {
			refusal.Model, refusal.Connector, refusal.DeclaredBy = doc.Model, doc.Connector, doc.DeclaredBy
			refused = append(refused, refusal)

			continue
		}

		key := normaliseModel(doc.Model)
		if _, seen := byKey[key]; !seen {
			keys = append(keys, key)
		}

		byKey[key] = append(byKey[key], doc)
	}

	placed := make([]ModelDoc, 0, len(declared))

	for _, key := range keys {
		docs := byKey[key]
		if len(docs) == 1 {
			placed = append(placed, docs[0])

			continue
		}

		for _, doc := range docs {
			refused = append(refused, Refusal{
				Model: doc.Model, Connector: doc.Connector, DeclaredBy: doc.DeclaredBy,
				Drivers: othersThan(doc, docs),
			})
		}
	}

	return Catalogue{declared: placed, refused: refused}, nil
}

// Refused returns the declared rows Place left out, so a reader of the served
// vocabulary learns why a model it expected has no row.
func (c Catalogue) Refused() []Refusal {
	out := make([]Refusal, 0, len(c.refused))
	for _, refusal := range c.refused {
		refusal.Drivers = slices.Clone(refusal.Drivers)
		out = append(out, refusal)
	}

	return out
}

// collidesWithCore reports the Prusa model a declared spelling collides with:
// one the profile lookup resolves it to, a core row it folds to, or the device
// type code of a loaded profile it folds to.
func collidesWithCore(
	registry Registry, names map[string]string, codes map[string]Refusal, spelling string,
) (Refusal, bool) {
	if model, ok := registry.Canonical(spelling); ok {
		return Refusal{Profile: model}, true
	}

	key := normaliseModel(spelling)

	if model, ok := names[key]; ok {
		return Refusal{Profile: model}, true
	}

	refusal, ok := codes[key]

	return refusal, ok
}

// othersThan names the drivers in docs other than the one that declared doc.
func othersThan(doc ModelDoc, docs []ModelDoc) []string {
	out := make([]string, 0, len(docs)-1)

	for _, other := range docs {
		if other.DeclaredBy != doc.DeclaredBy {
			out = append(out, other.DeclaredBy)
		}
	}

	return out
}

// Artwork returns the drawing a driver declared for a model, by its exact
// spelling. The core's rows carry none, because the web app draws those itself.
func (c Catalogue) Artwork(model string) (connectorapi.Artwork, bool) {
	for _, doc := range c.declared {
		if doc.Model == model && !doc.Artwork.IsZero() {
			return doc.Artwork, true
		}
	}

	return connectorapi.Artwork{}, false
}

// Docs returns every row this process serves, ordered by the machine's own
// spelling, so the core's rows and the declared ones read as one list.
func (c Catalogue) Docs() []ModelDoc {
	out := ModelDocs()
	out = append(out, c.declared...)

	sort.Slice(out, func(a, b int) bool { return out[a].Model < out[b].Model })

	return out
}
