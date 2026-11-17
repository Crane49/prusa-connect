package connectorapi

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// The bounds on one declared state. A declaration is refused rather than
// truncated, so these are limits and not the floor RetainedTextFloor states for
// a value a device reports.
const (
	// MinStateNameLen is the shortest legal name. The grammar reads the first
	// byte and the last one as separate positions, and at one byte they are the
	// same byte.
	MinStateNameLen = 2

	// MaxStateNameLen bounds one name. It is the length a DNS-1123 label takes,
	// which is what every other identifier a host places is held to.
	MaxStateNameLen = 63

	// MaxStateDisplayNameLen bounds one display name, counted in characters. A
	// display name sits in a badge beside the state, so the bound is about what
	// a badge holds. Characters and not bytes, because a bound in bytes gives a
	// name written in Japanese a third of the room it gives one written in
	// English, and the same badge holds both.
	MaxStateDisplayNameLen = 63
)

// The refusals a declared state set reports. A host inspects them with
// errors.Is, and each one is a distinct thing a person fixes in the
// declaration.
var (
	// ErrInvalidStateName reports a name the grammar refuses. See StateName.
	ErrInvalidStateName = errors.New("connectorapi: invalid device state name")
	// ErrInvalidStateDisplayName reports a display name that is empty, only
	// space, past MaxStateDisplayNameLen characters, not UTF-8, or carrying a
	// character a reader cannot see.
	ErrInvalidStateDisplayName = errors.New("connectorapi: invalid device state display name")
	// ErrNoStates reports a set declaring no state at all.
	ErrNoStates = errors.New("connectorapi: the state set declares no state")
	// ErrNoTransitions reports a set declaring states and no transition, which
	// is a list nobody can act on (ADR-20260831-c7b rule 6).
	ErrNoTransitions = errors.New("connectorapi: the state set declares no transition")
	// ErrDuplicateState reports one name declared twice.
	ErrDuplicateState = errors.New("connectorapi: the state is declared twice")
	// ErrUnknownState reports a transition naming a state the set does not
	// declare.
	ErrUnknownState = errors.New("connectorapi: the transition names an undeclared state")
	// ErrInvalidTransitionVerb reports a transition whose verb is not a
	// capability key, or is the key of one argument of a verb rather than the
	// verb. A client renders a control from what a transition names, so an
	// argument key there offers a caller something no command path accepts.
	ErrInvalidTransitionVerb = errors.New("connectorapi: invalid transition verb")
	// ErrDuplicateTransition reports one from, to and verb declared twice.
	ErrDuplicateTransition = errors.New("connectorapi: the transition is declared twice")
	// ErrSelfTransition reports a move from a state to itself. It reaches no
	// other state, so it says nothing a state model is for.
	ErrSelfTransition = errors.New("connectorapi: the transition leaves and enters one state")
	// ErrStateNoTransition reports a declared state no transition names. It is
	// a state nothing enters and nothing leaves, which is the per-state form of
	// the list nobody can act on that ADR-20260831-c7b rule 6 refuses.
	ErrStateNoTransition = errors.New("connectorapi: the state is named by no transition")
	// ErrUnguardedState reports a state that says nothing about the verbs it
	// accepts, in a set where another state does. A client reads a missing list
	// as "this state accepts nothing", so a state the author forgot would turn
	// every control off.
	ErrUnguardedState = errors.New("connectorapi: the state does not say which verbs it accepts")
	// ErrInvalidAcceptedVerb reports an accepted verb that is not a capability
	// key, or is the key of one argument of a verb rather than the verb.
	ErrInvalidAcceptedVerb = errors.New("connectorapi: invalid accepted verb")
	// ErrDuplicateAcceptedVerb reports one verb listed twice for one state.
	ErrDuplicateAcceptedVerb = errors.New("connectorapi: the state accepts the verb twice")
	// ErrTransitionNotAccepted reports a transition caused by a verb that the
	// state it leaves does not accept. The two lists would then tell a client
	// two different things about one verb.
	ErrTransitionNotAccepted = errors.New(
		"connectorapi: the transition's verb is not accepted in the state it leaves")
)

// StateName is one state a device kind reports it can be in: two to
// MaxStateNameLen bytes of A to Z, digits and underscores, starting with a
// letter and ending with a letter or a digit.
//
//	IDLE
//	PRINTING
//	FIN_OK
//
// The grammar is the spelling the value already travels in. A device state
// reaches a client as the text a driver put in the state tree, and every
// dialect in this tree spells one this way, so a declaration refuses nothing
// that ships (earlier research and recommendation 11).
//
// A name carries no namespace. It is read inside the device kind that declared
// it, which is itself qualified by the declaring adapter's namespace, so two
// adapters spelling one name mean two different states and neither can capture
// the other (ADR-20260831-c7b rule 4).
type StateName string

// String returns the name as text.
func (s StateName) String() string { return string(s) }

// Valid reports whether the name is one the grammar accepts.
func (s StateName) Valid() bool {
	if len(s) < MinStateNameLen || len(s) > MaxStateNameLen {
		return false
	}

	for i := range len(s) {
		c := s[i]

		switch {
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		case c == '_' && i > 0 && i < len(s)-1:
		default:
			return false
		}
	}

	return true
}

// DeviceState is one declared state with the words a program shows for it.
type DeviceState struct {
	// Name is the value the device reports and a client branches on.
	Name StateName `json:"state"`
	// DisplayName is what a program shows for a state it has no icon for. It
	// is the same cost ADR-20260826-5yj rule 13 already accepted for a kind a build
	// does not carry.
	DisplayName string `json:"display_name"`
	// Deprecated marks a state the adapter is going to stop reporting, and is
	// nil for one that is staying. The set is per install, so no gate here can
	// count the window: the marker is what a client reads to stop branching on
	// the state before it goes.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty"`
	// Accepts is every verb a device in this state accepts, which is the guard
	// of ADR-20260831-c7b rule 7 written as a list the host can read and serve. It is
	// a hint for a client deciding which controls to offer, and the device's
	// own answer to a command stays the decision.
	//
	// A set either says this for every state or for none. Nil in a set that
	// says nothing means "not declared". In a set that says it for one state,
	// nil is refused and a state that accepts nothing is written as an empty,
	// non-nil list. A verb listed here and absent from a device's capability
	// set is one that device cannot carry out in any state.
	Accepts []Capability `json:"accepts,omitempty"`
}

// Transition is one move between two declared states and the verb that causes
// it (ADR-20260831-c7b rule 6).
//
// It says which verb moves a device and never whether a caller may send it. Who
// may act is the authorization graph's answer, and what the device will accept
// in the state it is in is the guard's, which is declared separately and is not
// this type.
type Transition struct {
	// From is the state the device leaves.
	From StateName `json:"from"`
	// To is the state the device enters.
	To StateName `json:"to"`
	// Verb is the capability key that causes the move, and is empty for a move
	// the device makes on its own.
	//
	// A print starts because a caller sent start_print and finishes because the
	// file ran out. ADR-20260831-c7b rule 6 asks every transition to name a verb, so
	// every state a machine reaches by itself would need an invented one.
	Verb Capability `json:"verb,omitempty"`
}

// Caused reports whether a caller's verb causes this move rather than the
// device making it on its own.
func (t Transition) Caused() bool { return t.Verb != "" }

// StateSet is the state model of one device kind: every state the kind reports,
// and every move between two of them.
//
// An adapter declares it as data and the host places it at install. It is not
// read from a running module, so it is a table an adapter's package holds and
// never a call the host makes into a driver (ADR-20260831-c7b rule 3).
//
// The zero value declares nothing, and Problems refuses it. A device kind with
// no declared state set is one this mechanism says nothing about, which is
// every device kind until its adapter declares one.
type StateSet struct {
	// States is every state the kind reports, in the order a person reads them.
	States []DeviceState `json:"states"`
	// Transitions is every move between two declared states.
	Transitions []Transition `json:"transitions"`
}

// Clone returns a copy that shares no backing array with s. An adapter
// declaring a set returns one, and a host placing one keeps one, so neither can
// edit what the other holds.
func (s StateSet) Clone() StateSet {
	out := StateSet{
		States:      slices.Clone(s.States),
		Transitions: slices.Clone(s.Transitions),
	}

	for i := range out.States {
		if out.States[i].Accepts != nil {
			out.States[i].Accepts = slices.Clone(out.States[i].Accepts)
		}
	}

	return out
}

// IsZero reports whether the set declares nothing at all.
func (s StateSet) IsZero() bool { return len(s.States) == 0 && len(s.Transitions) == 0 }

// Guarded reports whether the set says which verbs each state accepts.
func (s StateSet) Guarded() bool {
	return slices.ContainsFunc(s.States, func(state DeviceState) bool { return state.Accepts != nil })
}

// Accepted reports whether a device in the named state accepts verb. A set that
// is not guarded says nothing, so every verb is accepted in every state it
// declares. A state the set does not declare accepts nothing.
func (s StateSet) Accepted(name StateName, verb Capability) bool {
	for _, state := range s.States {
		if state.Name != name {
			continue
		}

		return !s.Guarded() || slices.Contains(state.Accepts, verb)
	}

	return false
}

// Has reports whether the set declares name.
func (s StateSet) Has(name StateName) bool {
	for _, state := range s.States {
		if state.Name == name {
			return true
		}
	}

	return false
}

// Problems reports every way the set cannot be placed: a name the grammar
// refuses, a display name a person cannot read, a state declared twice, a
// transition naming a state that is not declared, a move from a state to
// itself, a state no transition reaches, a set with no transition at all, and a
// list of accepted verbs that is missing, malformed or disagrees with a
// transition.
//
// It is the check a host runs at install and an adapter author runs in their own
// tests, so both read one answer (ADR-20260825-bhe rule 11). Every problem is reported
// rather than the first, because an author fixing a declaration fixes the whole
// of it once.
func (s StateSet) Problems() []error {
	var problems []error

	if len(s.States) == 0 {
		problems = append(problems, ErrNoStates)
	}

	// Each state is judged once, however many times it is declared, so a name
	// declared twice reports the duplicate and not two copies of everything
	// else that is wrong with it. declared holds the names a transition may
	// name, and an unspellable name is not one: a transition over it is refused
	// for naming an undeclared state, which is what it is.
	seen := make(map[StateName]bool, len(s.States))
	declared := make(map[StateName]bool, len(s.States))

	for _, state := range s.States {
		if seen[state.Name] {
			problems = append(problems, fmt.Errorf("%w: %q", ErrDuplicateState, state.Name))

			continue
		}

		seen[state.Name] = true

		problems = append(problems, state.problems()...)

		if state.Name.Valid() {
			declared[state.Name] = true
		}
	}

	if len(s.States) > 0 && len(s.Transitions) == 0 {
		problems = append(problems, ErrNoTransitions)
	}

	problems = append(problems, s.guardProblems()...)

	return append(problems, s.transitionProblems(declared)...)
}

// guardProblems judges the verbs each state accepts: every state says it when
// one does, each verb is a verb, and a verb that causes a move is accepted in
// the state the move leaves.
func (s StateSet) guardProblems() []error {
	if !s.Guarded() {
		return nil
	}

	var problems []error

	for _, state := range s.States {
		if state.Accepts == nil {
			problems = append(problems, fmt.Errorf("%w: %q", ErrUnguardedState, state.Name))

			continue
		}

		listed := make(map[Capability]bool, len(state.Accepts))

		for _, verb := range state.Accepts {
			if _, isArgument := verb.Argument(); isArgument || !verb.Valid() {
				problems = append(problems, fmt.Errorf("%w: %q, accepted in %q",
					ErrInvalidAcceptedVerb, verb, state.Name))
			}

			if listed[verb] {
				problems = append(problems, fmt.Errorf("%w: %q, in %q",
					ErrDuplicateAcceptedVerb, verb, state.Name))
			}

			listed[verb] = true
		}
	}

	for _, transition := range s.Transitions {
		if !transition.Caused() || !s.Has(transition.From) {
			continue
		}

		if !s.Accepted(transition.From, transition.Verb) {
			problems = append(problems, fmt.Errorf("%w: %s leaves %q, which does not accept it",
				ErrTransitionNotAccepted, transition.Verb, transition.From))
		}
	}

	return problems
}

// transitionProblems judges the transitions against the names the set legally
// declares, and then judges every one of those names against the transitions.
func (s StateSet) transitionProblems(declared map[StateName]bool) []error {
	var problems []error

	written := make(map[Transition]bool, len(s.Transitions))
	named := make(map[StateName]bool, len(declared))

	for _, transition := range s.Transitions {
		problems = append(problems, transition.problems(declared)...)

		if written[transition] {
			problems = append(problems, fmt.Errorf("%w: %s to %s on %s",
				ErrDuplicateTransition, transition.From, transition.To, transition.verbText()))
		}

		written[transition] = true

		// A move from a state to itself names that state and reaches no other,
		// so it is not what keeps the state off the orphan list below.
		if transition.From != transition.To {
			named[transition.From] = true
			named[transition.To] = true
		}
	}

	if len(s.Transitions) == 0 {
		return problems
	}

	for name := range declared {
		if !named[name] {
			problems = append(problems, fmt.Errorf("%w: %q", ErrStateNoTransition, name))
		}
	}

	slices.SortFunc(problems, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })

	return problems
}

// problems reports what is wrong with one state.
func (d DeviceState) problems() []error {
	var problems []error

	if !d.Name.Valid() {
		problems = append(problems, fmt.Errorf("%w: %q", ErrInvalidStateName, d.Name))
	}

	if err := validDisplayName(d.DisplayName); err != nil {
		problems = append(problems, fmt.Errorf("%w, declared for the state %q", err, d.Name))
	}

	return problems
}

// problems reports what is wrong with one transition. declared holds the names
// the set carries, so a transition is refused for naming a state that is not
// one of them.
func (t Transition) problems(declared map[StateName]bool) []error {
	var problems []error

	if t.From == t.To {
		problems = append(problems, fmt.Errorf("%w: %q, on %s", ErrSelfTransition, t.From, t.verbText()))
	}

	if !declared[t.From] {
		problems = append(problems, fmt.Errorf("%w: a transition leaves %q", ErrUnknownState, t.From))
	}

	if !declared[t.To] {
		problems = append(problems, fmt.Errorf("%w: a transition enters %q", ErrUnknownState, t.To))
	}

	if t.Caused() && !t.Verb.Valid() {
		problems = append(problems, fmt.Errorf("%w: %q", ErrInvalidTransitionVerb, t.Verb))
	}

	if _, isArgument := t.Verb.Argument(); isArgument {
		problems = append(problems, fmt.Errorf("%w: %q names an argument of a verb and not a verb",
			ErrInvalidTransitionVerb, t.Verb))
	}

	return problems
}

// verbText names the verb for a person reading a refusal, and says so when the
// move has none.
func (t Transition) verbText() string {
	if !t.Caused() {
		return "no verb"
	}

	return t.Verb.String()
}

// validDisplayName holds a display name to something a person can read in a
// badge: present, bounded, UTF-8, and free of the characters that would render
// as nothing or reorder the line around it.
//
// U+202E is the one worth naming. It reverses the rendering of everything after
// it, so a display name carrying one rearranges the row it sits in and not only
// itself. It is in category Cf and not Cc, so unicode.IsControl does not see it.
func validDisplayName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: it is empty, or it is only space", ErrInvalidStateDisplayName)
	}

	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: it is not UTF-8", ErrInvalidStateDisplayName)
	}

	if count := utf8.RuneCountInString(name); count > MaxStateDisplayNameLen {
		return fmt.Errorf("%w: %d characters, and the limit is %d",
			ErrInvalidStateDisplayName, count, MaxStateDisplayNameLen)
	}

	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) ||
			unicode.Is(unicode.Zp, r) {
			return fmt.Errorf("%w: it carries %U, which a reader cannot see",
				ErrInvalidStateDisplayName, r)
		}
	}

	return nil
}
