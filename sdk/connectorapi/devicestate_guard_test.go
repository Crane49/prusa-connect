package connectorapi_test

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// guardedStates is printerStates with the verbs each state accepts, so a case
// asserting a refusal changes one thing.
func guardedStates() connectorapi.StateSet {
	set := printerStates()
	set.States[0].Accepts = []connectorapi.Capability{"core.command.start_print", "core.command.beep"}
	set.States[1].Accepts = []connectorapi.Capability{}

	return set
}

func TestAGuardedSetHasNoProblems(t *testing.T) {
	t.Parallel()

	set := guardedStates()

	if problems := set.Problems(); len(problems) > 0 {
		t.Fatalf("a sound guarded set reports %v", problems)
	}

	if !set.Guarded() {
		t.Error("a set that lists the verbs of every state reports that it lists none")
	}
}

func TestTheGuardIsRefusedWhereItCannotBeRead(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		edit func(*connectorapi.StateSet)
		want error
	}{
		"a state that says nothing beside one that does": {
			edit: func(set *connectorapi.StateSet) { set.States[1].Accepts = nil },
			want: connectorapi.ErrUnguardedState,
		},
		"an argument key in place of a verb": {
			edit: func(set *connectorapi.StateSet) {
				set.States[1].Accepts = []connectorapi.Capability{"core.command.start_print#path"}
			},
			want: connectorapi.ErrInvalidAcceptedVerb,
		},
		"a key the grammar refuses": {
			edit: func(set *connectorapi.StateSet) {
				set.States[1].Accepts = []connectorapi.Capability{"Start Print"}
			},
			want: connectorapi.ErrInvalidAcceptedVerb,
		},
		"one verb listed twice": {
			edit: func(set *connectorapi.StateSet) {
				set.States[1].Accepts = []connectorapi.Capability{"core.command.beep", "core.command.beep"}
			},
			want: connectorapi.ErrDuplicateAcceptedVerb,
		},
		"a move caused by a verb its state does not accept": {
			edit: func(set *connectorapi.StateSet) {
				set.States[0].Accepts = []connectorapi.Capability{"core.command.beep"}
			},
			want: connectorapi.ErrTransitionNotAccepted,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			set := guardedStates()
			c.edit(&set)

			problems := set.Problems()
			if !errors.Is(errors.Join(problems...), c.want) {
				t.Errorf("the set reports %v, and %v was expected", problems, c.want)
			}
		})
	}
}

func TestAcceptedReadsTheGuard(t *testing.T) {
	t.Parallel()

	guarded := guardedStates()

	if !guarded.Accepted("IDLE", "core.command.start_print") {
		t.Error("a verb the state lists is reported as refused")
	}

	if guarded.Accepted("PRINTING", "core.command.start_print") {
		t.Error("a verb the state does not list is reported as accepted")
	}

	if guarded.Accepted("PAUSED", "core.command.start_print") {
		t.Error("a state the set does not declare is reported as accepting a verb")
	}

	unguarded := printerStates()
	if !unguarded.Accepted("PRINTING", "core.command.beep") {
		t.Error("a set that says nothing about verbs refuses one")
	}
}

func TestCloneCopiesTheAcceptedVerbs(t *testing.T) {
	t.Parallel()

	original := guardedStates()
	copied := original.Clone()

	copied.States[0].Accepts[0] = "core.command.home"

	if original.States[0].Accepts[0] == "core.command.home" {
		t.Error("editing a clone's accepted verbs changed the set it was cloned from")
	}

	if copied.States[1].Accepts == nil {
		t.Error("the clone turned a state that accepts nothing into one that says nothing")
	}
}

func TestADialectWithARefusedStateSetIsRefused(t *testing.T) {
	t.Parallel()

	set := guardedStates()
	set.States[1].Accepts = nil

	err := connectorapi.ValidateDialect(connectorapi.Dialect{States: set})
	if !errors.Is(err, connectorapi.ErrInvalidDialect) || !errors.Is(err, connectorapi.ErrUnguardedState) {
		t.Errorf("a dialect carrying a refused state set reports %v", err)
	}

	if sound := connectorapi.ValidateDialect(connectorapi.Dialect{States: guardedStates()}); sound != nil {
		t.Errorf("a dialect carrying a sound state set is refused: %v", sound)
	}
}
