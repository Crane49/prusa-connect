// Package devicestate is the host's half of a declared device state set: the
// shape of a declaration, the registry the host places one in, and the refusals
// a bad declaration reports.
//
// It holds no state name and no display name. Every value in this vocabulary
// comes from the adapter that declared it, so two installs of one build answer
// differently and neither answer is in this tree.
//
// The vocabulary is therefore per-install. Nothing here is a table a generator
// could write, so no file under docs/vocabularies carries a row and no content
// version moves when an adapter declares a state.
//
// A declaration is data an adapter's package holds and the host places at
// install. Nothing here calls into a running module.
package devicestate

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// What a served device-state set declares about itself.
//
// There is no content version. The content is per-install, so a version would
// describe a set this build does not carry. The format version is per-build and
// moves when the shape of an entry changes, so a consumer can tell a reshaped
// answer from a differently populated one.
const (
	VocabularyName   = "device-states"
	VocabularyFormat = "1.0"
)

// The refusals a placement reports. A person reads one at install time.
var (
	// ErrNoNamespace reports a declaration the host could not attribute to an
	// adapter. The host derives the namespace and a caller never passes it in,
	// so an empty one means the derivation failed.
	ErrNoNamespace = errors.New("devicestate: the declaration carries no namespace")
	// ErrUnknownKind reports a declaration naming a kind this build does not
	// carry. The check is the one validDeviceRow in internal/authz makes on a
	// device row: the kind is known. The tree holds no narrower rule for which
	// kinds count as devices, so a declaration on any known kind is accepted.
	ErrUnknownKind = errors.New("devicestate: the declaration names a kind this build does not carry")
	// ErrDuplicateKind reports two declarations for one device kind by one
	// connector type. Two dialects may each declare the printer kind, because a
	// state name is read inside the dialect that declared it (ADR-20260831-c7b rule 4).
	// One dialect declaring it twice is a conflict and not a replacement.
	ErrDuplicateKind = errors.New("devicestate: the device kind is declared twice by one connector type")
	// ErrNoConnector reports a declaration that names no connector type, so a
	// client could not tell which devices it describes.
	ErrNoConnector = errors.New("devicestate: the declaration names no connector type")
	// ErrRefusedStateSet reports a state set connectorapi.Problems refuses. It
	// wraps every problem, so the person fixing the declaration reads all of
	// them at once.
	ErrRefusedStateSet = errors.New("devicestate: the declared state set is refused")
)

// Declaration is one adapter's state set for one device kind, as the host
// installed it.
type Declaration struct {
	// Namespace is the declaring adapter, derived by the host.
	Namespace extension.Namespace
	// Kind is the device kind the set belongs to.
	//
	// It is a ref.Kind because every device kind in the directory is one.
	// When an installed module may declare a kind of its own, this field
	// widens to a qualified id.
	Kind ref.Kind
	// Connector is the connector type that declared the set, qualified the way
	// the registry spells it.
	Connector connectorapi.TypeName
	// Instances is the connector instances of that type this deployment
	// mounted. A device record names its instance under driver, so this
	// is what a client joins a device onto its state set by.
	Instances []string
	// States is the declared states and the moves between them.
	States connectorapi.StateSet
}

// key is what makes a declaration distinct: one per device kind per connector
// type.
type key struct {
	kind      ref.Kind
	connector connectorapi.TypeName
}

// Installed is the state sets the host placed.
//
// The zero value declares nothing, which is what a deployment with no adapter
// declaration answers. It is read-only once built: Place copies its input, so
// an adapter that keeps the slice it declared cannot change what the host
// serves.
type Installed struct {
	declarations []Declaration
}

// Place validates every declaration and returns the set to serve. One bad
// declaration refuses the whole placement: an install that served a subset
// would leave an operator with a fleet page that does not say which adapter was
// dropped.
//
// Every refused declaration is reported, not only the first, so an operator with
// several misdeclaring adapters fixes them all from one refusal.
//
// Declarations come back ordered by device kind and then by connector type, so
// the served answer does not depend on the order the host installed them in.
func Place(declarations ...Declaration) (Installed, error) {
	placed := make([]Declaration, 0, len(declarations))
	seen := make(map[key]bool, len(declarations))

	var refusals []error

	for _, declaration := range declarations {
		if err := declaration.validate(seen); err != nil {
			refusals = append(refusals, err)

			continue
		}

		seen[declaration.key()] = true
		placed = append(placed, declaration.clone())
	}

	if len(refusals) > 0 {
		return Installed{}, errors.Join(refusals...)
	}

	slices.SortFunc(placed, func(a, b Declaration) int {
		if byKind := strings.Compare(a.Kind.String(), b.Kind.String()); byKind != 0 {
			return byKind
		}

		return strings.Compare(a.Connector.String(), b.Connector.String())
	})

	return Installed{declarations: placed}, nil
}

// Declarations returns a copy of what the host placed, ordered by device kind.
// The copy keeps a caller from editing the served set.
func (i Installed) Declarations() []Declaration {
	out := make([]Declaration, 0, len(i.declarations))
	for _, declaration := range i.declarations {
		out = append(out, declaration.clone())
	}

	return out
}

// Lookup returns the declaration for one device kind by one connector type.
func (i Installed) Lookup(kind ref.Kind, connector connectorapi.TypeName) (Declaration, bool) {
	for _, declaration := range i.declarations {
		if declaration.key() == (key{kind: kind, connector: connector}) {
			return declaration.clone(), true
		}
	}

	return Declaration{}, false
}

// Declares reports whether kind declares name. A kind with no declaration
// declares no state, so the answer is false: otherwise the caller would accept
// a value nobody named.
//
// It reads the placed set directly. Lookup would copy a whole state set to
// answer one bool.
func (i Installed) Declares(kind ref.Kind, connector connectorapi.TypeName, name connectorapi.StateName) bool {
	for _, declaration := range i.declarations {
		if declaration.key() == (key{kind: kind, connector: connector}) {
			return declaration.States.Has(name)
		}
	}

	return false
}

// validate checks one declaration. seen holds the kinds placed before it, so
// the second declaration of a kind by one connector type is the one refused.
func (d Declaration) validate(seen map[key]bool) error {
	if d.Namespace.IsZero() {
		return fmt.Errorf("%w: it declares %q", ErrNoNamespace, d.Kind)
	}

	if d.Connector == "" {
		return fmt.Errorf("%w: %s declares %q", ErrNoConnector, d.Namespace, d.Kind)
	}

	if !d.Kind.Valid() {
		return fmt.Errorf("%w: %q, declared by %s", ErrUnknownKind, d.Kind, d.Namespace)
	}

	if seen[d.key()] {
		return fmt.Errorf("%w: %q, declared again by %s for %s", ErrDuplicateKind, d.Kind, d.Namespace, d.Connector)
	}

	if problems := d.States.Problems(); len(problems) > 0 {
		return fmt.Errorf("%w: %s declares %q: %w",
			ErrRefusedStateSet, d.Namespace, d.Kind, errors.Join(problems...))
	}

	return nil
}

// clone copies the slices a declaration carries, so the placed set and the
// declaring package do not share one backing array.
func (d Declaration) clone() Declaration {
	out := d
	out.Instances = slices.Clone(d.Instances)
	out.States = d.States.Clone()

	return out
}

func (d Declaration) key() key { return key{kind: d.Kind, connector: d.Connector} }
