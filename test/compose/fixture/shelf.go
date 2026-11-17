package composefixture

import (
	"context"
	"errors"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// The device kind half of the fixture: a device class this repository does not
// compile, with verbs of its own, declared from outside the core module.
//
// This is what ADR-20260901-b2z rule 1 asks for. Before that record a driver's own verb
// was legal, registrable and conformance-passing, and undispatchable: the
// authorization vocabulary held a static table of the core's 59 verbs and
// dispatch refused any verb the table did not classify, so ten commands an
// adapter declared were dead on arrival. Nothing here edits a file under
// internal/, and the verbs are in the running server's verb table.
const (
	// ShelfType is the driver type an operator writes under connectors:. The
	// devices it admits are registered under the kind declared below, which is
	// what makes their verbs classify against the shelf's table rather than
	// against the printer's.
	ShelfType = "filament-shelf"

	// ShelfKind is the bare device-kind name. The host qualifies it with the
	// namespace it derives from this module's path, and nothing here chooses
	// that half.
	ShelfKind = "filament-shelf"

	// The verbs. They are under this module's own namespace, which the host
	// checks: a kind declares verbs in its own namespace and in no other.
	verbSetSlotAttention = "com.example.aux.composefixture.command.set_slot_attention"
	verbCalibrateScale   = "com.example.aux.composefixture.command.calibrate_scale"
	verbFactoryReset     = "com.example.aux.composefixture.command.factory_reset"
	verbWriteLabel       = "com.example.aux.composefixture.command.write_label"
)

// ErrNoShelf refuses a shelf driver that is asked to dial: this fixture proves
// declaration and classification, and it has no wire.
var ErrNoShelf = errors.New("composefixture: the shelf fixture dials nothing")

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() {
	connectorapi.Register(ShelfType, shelfDoc(), newShelf)
	connectorapi.RegisterKind(shelfDeclaration(), shelfKindDoc())
}

// shelfDeclaration is the kind and its verbs.
//
// set_slot_attention is `operate`: it is the machine's normal duty.
// calibrate_scale is `configure`: it changes what the machine is.
// factory_reset is `configure` and the adapter marks it irreversible, which is
// raising its own bar and is always allowed.
//
// write_label carries no marking at all, and it is the interesting one. Its
// argument is a free string with no supported set, so the host computes
// opaque_payload for it whatever this declaration says. That is the one check a
// lying declaration cannot route around, and this verb is here so that
// something exercises it.
func shelfDeclaration() connectorapi.KindDeclaration {
	return connectorapi.KindDeclaration{
		Kind:        ShelfKind,
		DisplayName: "Filament shelf",
		DriverTypes: []string{ShelfType},
		Verbs: map[string]connectorapi.VerbEntry{
			verbSetSlotAttention: {Class: connectorapi.ClassOperate},
			verbCalibrateScale:   {Class: connectorapi.ClassConfigure},
			verbFactoryReset: {
				Class:     connectorapi.ClassConfigure,
				Dangerous: []connectorapi.Marking{connectorapi.MarkingIrreversible},
			},
			verbWriteLabel: {Class: connectorapi.ClassOperate},
		},
		Capabilities: shelfBaseline(),
	}
}

// shelfBaseline is what the shelf can do, in the descriptor shape the host
// reads the markings out of.
func shelfBaseline() connectorapi.Set {
	set := connectorapi.Set{
		verbSetSlotAttention: {Flag: true},
		verbCalibrateScale:   {Flag: true},
		verbFactoryReset:     {Flag: true},
		verbWriteLabel:       {Flag: true},
	}

	// A bounded argument: one of two values the host can check.
	set[connectorapi.ArgumentKey(verbSetSlotAttention, "state")] = connectorapi.Value{
		Flag: true, Kind: connectorapi.KindString, Supported: []string{"on", "off"},
	}

	// A bounded number: the limit is a magnitude, so it bounds both sides.
	set[connectorapi.ArgumentKey(verbCalibrateScale, "grams")] = connectorapi.Value{
		Flag: true, Kind: connectorapi.KindNumber, Limit: 5000, HasLimit: true,
	}

	// An unbounded string. The declaration marks it nothing; the host marks it
	// opaque_payload.
	set[connectorapi.ArgumentKey(verbWriteLabel, "text")] = connectorapi.Value{
		Flag: true, Kind: connectorapi.KindString,
	}

	return set
}

func shelfKindDoc() extension.Doc {
	return extension.Doc{
		Summary: "A filament shelf: slots that hold spools, weighed and labelled.",
		Description: "A device class from outside the core module, declared to prove that an " +
			"adapter's own verbs classify, dispatch and are grantable with no edit to core source.",
		Version:   "1.0.0",
		Contract:  connectorapi.Contract,
		Stability: extension.StabilityExperimental,
	}
}

func shelfDoc() extension.Doc {
	return extension.Doc{
		Summary:     "The driver that speaks to a filament shelf.",
		Description: "It dials nothing. The declaration beside it is what this fixture is for.",
		Version:     "1.0.0",
		Contract:    connectorapi.Contract,
		Stability:   extension.StabilityExperimental,
	}
}

// shelf is the driver. It has no wire: what this fixture proves is the
// declaration, and a driver that dialled would need a machine to dial.
type shelf struct{}

func newShelf(connectorapi.Spec) (connectorapi.Driver, error) { return shelf{}, nil }

// Baseline is what this driver can do at all.
func (shelf) Baseline() connectorapi.Set { return shelfBaseline() }

// Dial refuses: this fixture has no wire.
func (shelf) Dial(context.Context, connectorapi.Endpoint, connectorapi.Arrivals) error {
	return ErrNoShelf
}

// Close releases nothing.
func (shelf) Close() error { return nil }
