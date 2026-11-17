package httpapi

// The dialects half of the device-model vocabulary.
//
// The model catalogue says what each machine is called and who makes it. It does
// not say how a person gets one of them talking to this server, and that is the
// question a page asking "what are you adding?" has to answer: a Prusa machine
// reads a settings file off a USB stick and dials in, and a Klipper machine sits
// at an address this server dials. The two ask a person for completely different
// things, so a page that offers one form for both is wrong for one of them.
//
// It is served with the models and is not a vocabulary of its own, as the
// runtimes are served with the resource kinds: the two are read together, and a
// row here has meaning only beside the models that name it.
//
// Two sources, joined. Which dialects this binary linked comes from the
// connector registry, the same read `get-developer-connectors` makes, so the
// answer is about the process that is running. What a dialect is for, the brands
// and what it asks a person for, comes from each driver's own declaration in
// sdk/connectorapi. A table of dialect names in this package would be a table an
// out-of-tree driver cannot add a row to.

import (
	"sort"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// connectorEntry is one dialect, as a page offering a choice of brand reads it.
type connectorEntry struct {
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Connector string `json:"connector" doc:"The dialect's bare name, as an operator writes it under type in core.connectors and as the connector member of a model row names it. The driver member of a device record carries the name of an instance of this dialect, not this name."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Qualified string `json:"qualified,omitempty" doc:"The namespace-qualified name, which is unique by construction. Absent for a dialect this build does not carry, because nothing registered it to derive a namespace from."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Kind string `json:"kind" doc:"What type of device this dialect speaks to, from the same closed set the kind member of a device record is drawn from."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Brands []string `json:"brands" doc:"The names the machines this dialect speaks to are sold under, from the driver's own declaration. A brand name is the same word in every language, so it is served as it is written and never translated. It is empty for a dialect that declares none and for one this build does not have."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Summary string `json:"summary" doc:"One sentence on the dialect, from the driver's own descriptor. Empty for a dialect this build does not carry."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Available bool `json:"available" doc:"Whether this binary carries the dialect. A row that is false names machines this build knows about and cannot talk to, which is what a page draws greyed out rather than leaving out: the model rows still name it, and a reader looking for their machine needs to be told why it is not on offer."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Dials bool `json:"dials" doc:"Whether this server dials the machine. A dialect that dials takes an address, declared through put-connector-endpoint. It is read from the driver itself, so it is false for a dialect this build does not have, where there is no driver to ask."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Arrives bool `json:"arrives" doc:"Whether the machine dials this server. A dialect can do both, and one this build carries that does neither could not be built."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Needs string `json:"needs" enum:"settings_file,address,none,unknown" doc:"What a person supplies before a machine of this kind can arrive. settings_file is a file written onto the machine, which then dials in. address is somewhere this server dials. none is a machine that arrives with nothing declared here. unknown is a dialect this build does not have, so nothing could be asked and a page draws no form for it."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Models []string `json:"models" doc:"The models in this set that are reached through this dialect, by the machine's own spelling. It is empty for a dialect that talks to machines the catalogue does not name, which is not a fault: a driver serves a machine with no profile from its own baseline."`
}

// connectorEntries is the projection onto the wire.
//
// The list is the union of two sets: what the registry carries, and what the
// model catalogue names. The catalogue alone would hide a dialect somebody
// composed in that declared no model. A model always names a dialect this build
// has, because its row is the core's own xBuddy row or one a registered driver
// declared. A dialect the catalogue names and the registry lacks is therefore a
// fault, and the union still shows it.
//
// The kind is written here and not in the profile package, because the closed
// set of kinds is the registry's and internal/printer may not reach it.
func connectorEntries(models profile.Catalogue) []connectorEntry {
	// The registry is drained once by whatever wired the connectors, and draining
	// it again returns the same registrations and not an empty list, so
	// reading it here neither seals anything early nor sees a different table
	// than the host mounted from.
	var registrations []connectorapi.Registration
	if types, err := connectorapi.NewRegistry(); err == nil {
		registrations = types.Types()
	}

	return connectorEntriesFrom(models, registrations...)
}

// connectorEntriesFrom is connectorEntries over the registrations it is handed,
// so a test can serve a declared dialect without registering it for the whole
// process.
func connectorEntriesFrom(models profile.Catalogue, registrations ...connectorapi.Registration) []connectorEntry {
	held := map[string]*connectorEntry{}
	order := []string{}

	at := func(name string) *connectorEntry {
		if entry, ok := held[name]; ok {
			return entry
		}

		// What is true before any driver is asked: only the name and the kind.
		// A dialect this build does not have keeps these values, so its needs
		// word is unknown.
		entry := &connectorEntry{
			Connector: name,
			Kind:      ref.KindPrinter.String(),
			Brands:    []string{},
			Needs:     connectorapi.NeedsUnknown.String(),
			Models:    []string{},
		}
		held[name] = entry
		order = append(order, name)

		return entry
	}

	for _, registration := range registrations {
		describeDialect(at(registration.Name().String()), registration)
	}

	for _, doc := range models.Docs() {
		entry := at(doc.Connector)
		entry.Models = append(entry.Models, doc.Model)
	}

	out := make([]connectorEntry, 0, len(order))
	for _, name := range order {
		out = append(out, *held[name])
	}

	// By name, so two builds carrying the same dialects list them in the same
	// order whichever order the registry happened to drain in.
	sort.Slice(out, func(a, b int) bool { return out[a].Connector < out[b].Connector })

	return out
}

// describeDialect fills in what only a registered driver can answer: what it
// declared about itself, which way it is reached, and its summary.
//
// It builds one probe instance, which is the read get-developer-connectors
// already makes for the same question. A factory is handed no host services, so
// by contract it cannot reach a device before the host is ready, and the probe is
// never handed Arrivals and never serves a request.
//
// A driver that will not build is left unavailable and not dropped. A dialect
// missing from this list would read as a build that does not have it at all,
// which is a different problem with a different fix.
func describeDialect(entry *connectorEntry, registration connectorapi.Registration) {
	entry.Qualified = registration.Qualified().String()
	entry.Summary = registration.Descriptor().Doc().Summary

	// The declaration is read before the probe, so a driver that will not build
	// still answers with what its author wrote down.
	declared := registration.Dialect()
	if len(declared.Brands) > 0 {
		entry.Brands = declared.Brands
	}

	if declared.Needs != "" {
		entry.Needs = declared.Needs.String()
	}

	driver, err := registration.Build(connectorapi.Spec{
		Ref:  connectorapi.Ref(registration.Name()),
		Type: registration.Qualified(),
	})
	if err != nil {
		return
	}

	inbound, outbound, err := connectorapi.Directions(driver)
	if err != nil {
		return
	}

	entry.Available = true
	entry.Arrives = inbound != nil
	entry.Dials = outbound != nil

	if declared.Needs != "" {
		return
	}

	// A driver that declared no word still gets an answer, from the direction
	// it is reached in: a machine this server dials needs an address, and a
	// machine that dials in does not.
	entry.Needs = connectorapi.NeedsNothing.String()
	if entry.Dials {
		entry.Needs = connectorapi.NeedsAddress.String()
	}
}
