package devicestate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/devicestate"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// printerDeclaration is a sound declaration every case starts from, so a case
// asserting a refusal changes one thing. The state words belong to the caller:
// nothing under internal/kernel names one.
func printerDeclaration() devicestate.Declaration {
	return devicestate.Declaration{
		Namespace: extension.CoreNamespace,
		Kind:      ref.KindPrinter,
		Connector: "reference",
		Instances: []string{"bench"},
		States: connectorapi.StateSet{
			States: []connectorapi.DeviceState{
				{Name: "IDLE", DisplayName: "Idle"},
				{Name: "PRINTING", DisplayName: "Printing"},
			},
			Transitions: []connectorapi.Transition{
				{From: "IDLE", To: "PRINTING", Verb: "core.command.start_print"},
				{From: "PRINTING", To: "IDLE"},
			},
		},
	}
}

func TestTheZeroValueDeclaresNothing(t *testing.T) {
	t.Parallel()

	var installed devicestate.Installed

	require.Empty(t, installed.Declarations())

	_, found := installed.Lookup(ref.KindPrinter, "reference")
	require.False(t, found)

	require.False(t, installed.Declares(ref.KindPrinter, "reference", "IDLE"),
		"a kind nothing declared reports a state as declared")
}

func TestASoundDeclarationIsPlaced(t *testing.T) {
	t.Parallel()

	installed, err := devicestate.Place(printerDeclaration())
	require.NoError(t, err)

	placed := installed.Declarations()
	require.Len(t, placed, 1)
	require.Equal(t, ref.KindPrinter, placed[0].Kind)
	require.Equal(t, extension.CoreNamespace, placed[0].Namespace)
	require.Len(t, placed[0].States.States, 2)
	require.Len(t, placed[0].States.Transitions, 2)

	require.True(t, installed.Declares(ref.KindPrinter, "reference", "PRINTING"))
	require.False(t, installed.Declares(ref.KindPrinter, "reference", "PAUSED"),
		"a state the declaration does not name is reported as declared")
	require.False(t, installed.Declares(ref.KindPrinter, "other", "PRINTING"),
		"a state one connector type declared is reported as declared by another")
}

// The host's own validator refuses a bad declaration at install. The cases
// cover the refusals the host owns and the ones it delegates to the state set's
// own check, because both reach a person as one refusal.
func TestTheHostRefusesWhatTheGrammarRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		declarations func() []devicestate.Declaration
		want         error
	}{
		"a state name the grammar refuses": {
			declarations: func() []devicestate.Declaration {
				declaration := printerDeclaration()
				declaration.States.States[0].Name = "idle"

				return []devicestate.Declaration{declaration}
			},
			want: connectorapi.ErrInvalidStateName,
		},
		"a display name a person cannot read": {
			declarations: func() []devicestate.Declaration {
				declaration := printerDeclaration()
				declaration.States.States[0].DisplayName = ""

				return []devicestate.Declaration{declaration}
			},
			want: connectorapi.ErrInvalidStateDisplayName,
		},
		"states and no transition": {
			declarations: func() []devicestate.Declaration {
				declaration := printerDeclaration()
				declaration.States.Transitions = nil

				return []devicestate.Declaration{declaration}
			},
			want: connectorapi.ErrNoTransitions,
		},
		"no state at all": {
			declarations: func() []devicestate.Declaration {
				declaration := printerDeclaration()
				declaration.States = connectorapi.StateSet{}

				return []devicestate.Declaration{declaration}
			},
			want: connectorapi.ErrNoStates,
		},
		"a namespace the host could not derive": {
			declarations: func() []devicestate.Declaration {
				declaration := printerDeclaration()
				declaration.Namespace = ""

				return []devicestate.Declaration{declaration}
			},
			want: devicestate.ErrNoNamespace,
		},
		"a device kind this build does not carry": {
			declarations: func() []devicestate.Declaration {
				declaration := printerDeclaration()
				declaration.Kind = "acme.dev/cnc"

				return []devicestate.Declaration{declaration}
			},
			want: devicestate.ErrUnknownKind,
		},
		"one device kind declared twice by one connector type": {
			declarations: func() []devicestate.Declaration {
				return []devicestate.Declaration{printerDeclaration(), printerDeclaration()}
			},
			want: devicestate.ErrDuplicateKind,
		},
		"no connector type": {
			declarations: func() []devicestate.Declaration {
				declaration := printerDeclaration()
				declaration.Connector = ""

				return []devicestate.Declaration{declaration}
			},
			want: devicestate.ErrNoConnector,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			installed, err := devicestate.Place(c.declarations()...)
			require.Error(t, err, "the placement was accepted and %v was expected", c.want)
			require.ErrorIs(t, err, c.want, "the refusal is %v, and %v was expected", err, c.want)
			require.Empty(t, installed.Declarations(),
				"a refused placement served a declaration, so an install serves a set it refused")
		})
	}
}

// One bad declaration refuses the placement. An operator reading a fleet page
// cannot see which adapter was skipped, so the install fails instead.
func TestARefusedDeclarationRefusesThePlacement(t *testing.T) {
	t.Parallel()

	sound := printerDeclaration()

	refused := printerDeclaration()
	refused.Kind = ref.KindCamera
	refused.States.States[0].Name = "idle"

	_, err := devicestate.Place(sound, refused)
	require.ErrorIs(t, err, devicestate.ErrRefusedStateSet)
}

// Two dialects drive printers, and each declares its own words for them. Both
// are placed, because a state name is read inside the dialect that declared it
// (ADR-20260831-c7b rule 4).
func TestTwoConnectorTypesMayEachDeclareOneKind(t *testing.T) {
	t.Parallel()

	other := printerDeclaration()
	other.Connector = "another"
	other.Instances = []string{"shop", "garage"}

	installed, err := devicestate.Place(printerDeclaration(), other)
	require.NoError(t, err)

	placed := installed.Declarations()
	require.Len(t, placed, 2)
	require.Equal(t, connectorapi.TypeName("another"), placed[0].Connector, "the answer is not ordered by type")
	require.Equal(t, connectorapi.TypeName("reference"), placed[1].Connector)

	found, ok := installed.Lookup(ref.KindPrinter, "another")
	require.True(t, ok)
	require.Equal(t, []string{"shop", "garage"}, found.Instances)
}

func TestPlacementOrdersTheAnswerByKind(t *testing.T) {
	t.Parallel()

	camera := printerDeclaration()
	camera.Kind = ref.KindCamera

	installed, err := devicestate.Place(printerDeclaration(), camera)
	require.NoError(t, err)

	placed := installed.Declarations()
	require.Len(t, placed, 2)
	require.Equal(t, ref.KindCamera, placed[0].Kind, "the answer follows the install order")
	require.Equal(t, ref.KindPrinter, placed[1].Kind)
}

// An adapter cannot edit what the host serves after the install placed it.
func TestThePlacedSetIsNotTheDeclaredSlice(t *testing.T) {
	t.Parallel()

	declaration := printerDeclaration()

	installed, err := devicestate.Place(declaration)
	require.NoError(t, err)

	declaration.States.States[0].Name = "CHANGED"

	served := installed.Declarations()
	require.Equal(t, connectorapi.StateName("IDLE"), served[0].States.States[0].Name)

	served[0].States.States[0].Name = "ALSO_CHANGED"
	require.Equal(t, connectorapi.StateName("IDLE"),
		installed.Declarations()[0].States.States[0].Name,
		"editing the answer changed the placed set")
}

// An operator with several misdeclaring adapters reads every refusal at once.
// Reporting only the first would send them through the install once per
// adapter.
func TestEveryRefusedDeclarationIsNamed(t *testing.T) {
	t.Parallel()

	printer := printerDeclaration()
	printer.States.States[0].Name = "printer_bad"

	camera := printerDeclaration()
	camera.Kind = ref.KindCamera
	camera.States.States[0].DisplayName = ""

	_, err := devicestate.Place(printer, camera)
	require.Error(t, err)

	require.Contains(t, err.Error(), "printer_bad",
		"the printer declaration's own refusal is missing")
	require.Contains(t, err.Error(), ref.KindCamera.String(),
		"the second refused declaration is not named, so an operator fixes one and finds the other")
}
