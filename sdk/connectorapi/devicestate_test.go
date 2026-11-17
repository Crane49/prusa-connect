package connectorapi_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// shippingStateWords is every device state word a dialect in this repository
// puts in the state tree today.
//
// The grammar is meant to refuse nothing that already travels, so this is the
// list it is held against. The sources are internal/connector/moonraker's
// stateWords table, internal/connector/xbuddy's two dialog constants,
// adapters/events/mqtt's job words and internal/printer/simulator. A driver
// author reading this sees what shape a name takes rather than a closed set: the
// set is the adapter's, not this package's.
var shippingStateWords = []connectorapi.StateName{
	"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED", "ERROR", "ATTENTION",
	"FIN_OK", "FIN_STOPPED", "FIN_ERROR", "JOB_PLANNER", "SINGLE",
}

func TestTheGrammarAcceptsEveryStateWordThatShips(t *testing.T) {
	t.Parallel()

	for _, name := range shippingStateWords {
		if !name.Valid() {
			t.Errorf("%q ships today and the grammar refuses it, so a declaration cannot name it", name)
		}
	}
}

func TestTheGrammarRefusesWhatItSays(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		name  connectorapi.StateName
		valid bool
	}{
		"one letter":          {name: "A", valid: false},
		"two letters":         {name: "OK", valid: true},
		"lower case":          {name: "idle", valid: false},
		"mixed case":          {name: "Idle", valid: false},
		"leading digit":       {name: "1IDLE", valid: false},
		"trailing digit":      {name: "STAGE2", valid: true},
		"leading underscore":  {name: "_IDLE", valid: false},
		"trailing underscore": {name: "IDLE_", valid: false},
		"inner underscore":    {name: "FIN_OK", valid: true},
		"double underscore":   {name: "FIN__OK", valid: true},
		"hyphen":              {name: "FIN-OK", valid: false},
		"dot":                 {name: "core.IDLE", valid: false},
		"space":               {name: "NEEDS ATTENTION", valid: false},
		"empty":               {name: "", valid: false},
		"at the length limit": {
			name:  connectorapi.StateName(strings.Repeat("A", connectorapi.MaxStateNameLen)),
			valid: true,
		},
		"one past the limit": {
			name:  connectorapi.StateName(strings.Repeat("A", connectorapi.MaxStateNameLen+1)),
			valid: false,
		},
		"a non-ascii letter": {name: "PŘIPRAVENO", valid: false},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := c.name.Valid(); got != c.valid {
				t.Errorf("Valid() on %q is %v, and the grammar says %v", c.name, got, c.valid)
			}
		})
	}
}

// printerStates is a sound declaration every case below starts from, so a case
// asserting a refusal changes one thing.
func printerStates() connectorapi.StateSet {
	return connectorapi.StateSet{
		States: []connectorapi.DeviceState{
			{Name: "IDLE", DisplayName: "Idle"},
			{Name: "PRINTING", DisplayName: "Printing"},
		},
		Transitions: []connectorapi.Transition{
			{From: "IDLE", To: "PRINTING", Verb: "core.command.start_print"},
			{From: "PRINTING", To: "IDLE"},
		},
	}
}

func TestASoundStateSetHasNoProblems(t *testing.T) {
	t.Parallel()

	if problems := printerStates().Problems(); len(problems) > 0 {
		t.Fatalf("a sound set reports %v", problems)
	}
}

func TestProblemsReportsEachRefusalOnItsOwn(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		set  func() connectorapi.StateSet
		want error
	}{
		"no state at all": {
			set:  func() connectorapi.StateSet { return connectorapi.StateSet{} },
			want: connectorapi.ErrNoStates,
		},
		"a name the grammar refuses": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.States[0].Name = "idle"
				set.Transitions[0].From = "idle"
				set.Transitions[1].To = "idle"

				return set
			},
			want: connectorapi.ErrInvalidStateName,
		},
		"no display name": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.States[0].DisplayName = ""

				return set
			},
			want: connectorapi.ErrInvalidStateDisplayName,
		},
		"a display name carrying a newline": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.States[0].DisplayName = "Idle\nPrinting"

				return set
			},
			want: connectorapi.ErrInvalidStateDisplayName,
		},
		"one state declared twice": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.States = append(set.States, connectorapi.DeviceState{Name: "IDLE", DisplayName: "Idle again"})

				return set
			},
			want: connectorapi.ErrDuplicateState,
		},
		"states and no transition": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.Transitions = nil

				return set
			},
			want: connectorapi.ErrNoTransitions,
		},
		"a transition leaving an undeclared state": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.Transitions[0].From = "PAUSED"

				return set
			},
			want: connectorapi.ErrUnknownState,
		},
		"a transition entering an undeclared state": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.Transitions[0].To = "FINISHED"

				return set
			},
			want: connectorapi.ErrUnknownState,
		},
		"a verb the capability grammar refuses": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.Transitions[0].Verb = "NOT A CAPABILITY KEY"

				return set
			},
			want: connectorapi.ErrInvalidTransitionVerb,
		},
		"one transition declared twice": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.Transitions = append(set.Transitions, set.Transitions[0])

				return set
			},
			want: connectorapi.ErrDuplicateTransition,
		},
		"a move from a state to itself": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.Transitions = append(set.Transitions,
					connectorapi.Transition{From: "IDLE", To: "IDLE"})

				return set
			},
			want: connectorapi.ErrSelfTransition,
		},
		"a set whose every move is a self-transition": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.Transitions = []connectorapi.Transition{
					{From: "IDLE", To: "IDLE"},
					{From: "PRINTING", To: "PRINTING"},
				}

				return set
			},
			want: connectorapi.ErrStateNoTransition,
		},
		"a display name of one space": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.States[0].DisplayName = " "

				return set
			},
			want: connectorapi.ErrInvalidStateDisplayName,
		},
		"a display name of one no-break space": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.States[0].DisplayName = "\u00a0"

				return set
			},
			want: connectorapi.ErrInvalidStateDisplayName,
		},
		"a display name carrying a right-to-left override": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.States[0].DisplayName = "\u202eIdle"

				return set
			},
			want: connectorapi.ErrInvalidStateDisplayName,
		},
		"a transition over a name the grammar refuses": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.States = append(set.States,
					connectorapi.DeviceState{Name: "idle", DisplayName: "Idle"})
				set.Transitions = append(set.Transitions,
					connectorapi.Transition{From: "idle", To: "PRINTING"})

				return set
			},
			want: connectorapi.ErrUnknownState,
		},
		"a verb key naming an argument of a verb": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.Transitions[0].Verb = "core.command.start_print#path"

				return set
			},
			want: connectorapi.ErrInvalidTransitionVerb,
		},
		"a display name past the limit in characters": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.States[0].DisplayName = strings.Repeat("a", connectorapi.MaxStateDisplayNameLen+1)

				return set
			},
			want: connectorapi.ErrInvalidStateDisplayName,
		},
		"a state no transition names": {
			set: func() connectorapi.StateSet {
				set := printerStates()
				set.States = append(set.States,
					connectorapi.DeviceState{Name: "FINISHED", DisplayName: "Finished"})

				return set
			},
			want: connectorapi.ErrStateNoTransition,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			problems := c.set().Problems()
			if len(problems) == 0 {
				t.Fatalf("the set was accepted and %v was expected", c.want)
			}

			if !errors.Is(errors.Join(problems...), c.want) {
				t.Errorf("the set reports %v, and %v was expected", problems, c.want)
			}
		})
	}
}

// TestATransitionTheDeviceMakesAloneIsAccepted is the case the printer's own
// state model needs, and Transition.Verb says why. A declaration that invented
// a verb for it would be describing a caller that does not exist.
func TestATransitionTheDeviceMakesAloneIsAccepted(t *testing.T) {
	t.Parallel()

	set := printerStates()

	if set.Transitions[1].Caused() {
		t.Fatal("a transition with no verb reports that a caller causes it")
	}

	if problems := set.Problems(); len(problems) > 0 {
		t.Errorf("a transition the device makes alone is refused: %v", problems)
	}
}

func TestCloneSharesNoBackingArray(t *testing.T) {
	t.Parallel()

	original := printerStates()
	copied := original.Clone()

	copied.States[0].Name = "CHANGED"
	copied.Transitions[0].Verb = "core.command.beep"

	if original.States[0].Name == "CHANGED" || original.Transitions[0].Verb == "core.command.beep" {
		t.Error("editing a clone changed the set it was cloned from")
	}
}

func TestHasAnswersOnlyForADeclaredState(t *testing.T) {
	t.Parallel()

	set := printerStates()

	if !set.Has("IDLE") {
		t.Error("a declared state is reported as undeclared")
	}

	if set.Has("PAUSED") {
		t.Error("a state nothing declared is reported as declared")
	}
}

// TestOneStateIsJudgedOnce holds the shape of a refusal a person reads. A name
// declared twice is one problem, and not the duplicate plus a second copy of
// everything else the state gets wrong.
func TestOneStateIsJudgedOnce(t *testing.T) {
	t.Parallel()

	set := printerStates()
	set.States = append(set.States,
		connectorapi.DeviceState{Name: "PAUSED", DisplayName: "Paused"},
		connectorapi.DeviceState{Name: "PAUSED", DisplayName: "Paused"})

	problems := set.Problems()

	duplicates, orphans := 0, 0

	for _, problem := range problems {
		switch {
		case errors.Is(problem, connectorapi.ErrDuplicateState):
			duplicates++
		case errors.Is(problem, connectorapi.ErrStateNoTransition):
			orphans++
		}
	}

	if duplicates != 1 || orphans != 1 {
		t.Errorf("PAUSED declared twice reports %d duplicates and %d orphans, and one of each is right:\n%v",
			duplicates, orphans, problems)
	}
}

// TestTheProblemListIsStable is what keeps a refusal readable across two runs.
// The orphan check reads a map, so the order it finds names in is not the order
// it reports them in.
func TestTheProblemListIsStable(t *testing.T) {
	t.Parallel()

	set := printerStates()
	set.States = append(set.States,
		connectorapi.DeviceState{Name: "FINISHED", DisplayName: "Finished"},
		connectorapi.DeviceState{Name: "PAUSED", DisplayName: "Paused"},
		connectorapi.DeviceState{Name: "ERROR", DisplayName: "Error"})

	first := messages(set.Problems())

	for range 20 {
		if got := messages(set.Problems()); !slices.Equal(got, first) {
			t.Fatalf("two runs report the problems in different orders:\n%v\n%v", first, got)
		}
	}
}

func messages(problems []error) []string {
	out := make([]string, 0, len(problems))
	for _, problem := range problems {
		out = append(out, problem.Error())
	}

	return out
}

// TestADisplayNameIsBoundedInCharactersAndNotBytes is the localisation case. A
// bound in bytes gives a Japanese display name a third of the room an English
// one gets, and the badge that renders both is one width.
func TestADisplayNameIsBoundedInCharactersAndNotBytes(t *testing.T) {
	t.Parallel()

	set := printerStates()
	set.States[0].DisplayName = strings.Repeat("印", connectorapi.MaxStateDisplayNameLen)

	if problems := set.Problems(); len(problems) > 0 {
		t.Errorf("%d characters of Japanese is refused, and the limit is %d characters: %v",
			connectorapi.MaxStateDisplayNameLen, connectorapi.MaxStateDisplayNameLen, problems)
	}
}
