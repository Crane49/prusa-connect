package authz

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The generated half is the whole of what a printer's verb may become, so every
// entry has to carry a class a device defines. An entry naming a resource action
// would compile and would refuse every caller, because the two masks share no
// bit. An entry naming observe, view_audit or administer would be a verb on a
// rung no verb may stand on (ADR-20260901-b2z rule 4).
func TestEveryClassifiedVerbTakesADeviceClass(t *testing.T) {
	t.Parallel()

	seen := map[string]Action{}

	for _, entry := range generatedVerbs {
		t.Run(entry.verb, func(t *testing.T) {
			t.Parallel()

			require.Truef(t, strings.HasPrefix(entry.verb, verbPrefix),
				"%s is classified here and is not a verb this vocabulary owns", entry.verb)
			require.Truef(t, entry.class.Valid(),
				"%s takes %q, which carries no mask, so every command under it is refused",
				entry.verb, entry.class)

			domain, ok := entry.class.domain()
			require.Truef(t, ok, "%s takes %q, which belongs to no object type", entry.verb, entry.class)
			require.Equalf(t, TypeDevice, domain,
				"%s takes %q, which is defined on %s and not on a device", entry.verb, entry.class, domain)

			require.NotContainsf(t,
				[]Action{ActionObserve, ActionViewAudit, ActionAdminister}, entry.class,
				"%s carries %q, which no verb may carry: answering any verb writes to the transport, "+
					"and administering grants is not a command", entry.verb, entry.class)

			for _, marking := range entry.markings {
				require.Truef(t, marking.Valid(), "%s carries the marking %q", entry.verb, marking)
			}
		})
	}

	for _, entry := range generatedVerbs {
		other, clash := seen[entry.verb]
		require.Falsef(t, clash, "%s is classified twice, as %q and %q", entry.verb, other, entry.class)
		seen[entry.verb] = entry.class
	}

	require.Len(t, VerbActions(DeviceKindPrinter), len(generatedVerbs),
		"the published mapping and the table disagree about how many verbs are classified")
}

// A verb the table does not name reaches no printer. This is the half that
// replaces the default arm, and it is asserted through the graph rather than on
// the returned value alone: what matters is that a caller holding every printer
// action there is still cannot send it.
func TestAVerbOutsideTheTableIsRefusedRatherThanDowngraded(t *testing.T) {
	t.Parallel()

	unclassified := []string{
		"",
		"core.command.stop_print_but_not",
		"stop_print",
		"com.example.command.stop_print",
	}

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, verb := range unclassified {
			t.Run(verb, func(t *testing.T) {
				t.Parallel()

				action := ActionForVerb(DeviceKindPrinter, verb)
				require.Falsef(t, action.Valid(), "verb %q was classified as %q", verb, action)

				// user:root is an org admin, so it holds every action the
				// printer role layer confers. The refusal is the verb's and not
				// the caller's.
				_, err := store.Can(t.Context(), subjectOf(t, "user:root"), action, deviceOf(t, "p-beta"))
				require.Errorf(t, err, "verb %q reached a decision", verb)
				require.Truef(t, fault.HasCode(err, unknownAction.Code()),
					"verb %q was refused with %v and not as an action outside the vocabulary", verb, err)
			})
		}
	})
}

// A caller who guessed a spelling is told which verb this vocabulary names, and
// a caller who guessed a word that is no verb at all is told nothing.
//
// The suggestion is the caller's own string folded against this table. It reads
// no printer and no grant, so a case that reaches a real verb and a case that
// reaches none are the same function of the same two inputs.
func TestASpellingOfAVerbResolvesToTheVerbAndAWordThatIsNoVerbDoesNot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		spelling string
		want     string
	}{
		{"the shipping server's name", "SET_NOZZLE_TEMPERATURE", "core.command.set_nozzle_temperature"},
		{"the verb itself", "core.command.set_nozzle_temperature", "core.command.set_nozzle_temperature"},
		{"the suffix alone", "set_nozzle_temperature", "core.command.set_nozzle_temperature"},
		{"the prefix shouted too", "CORE.COMMAND.STOP_PRINT", "core.command.stop_print"},
		{"a host-minted verb", "SEND_GCODE", "core.command.send_gcode"},
		{"a word no verb answers to", "MELT_THE_BED", ""},
		{"another vendor's namespace", "com.example.command.stop_print", ""},
		{"a near miss", "STOP_PRINTING", ""},
		{"nothing at all", "", ""},
	}

	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			got, named := CanonicalVerb(one.spelling)

			require.Equalf(t, one.want != "", named,
				"CanonicalVerb(%q) reported named=%v", one.spelling, named)
			require.Equalf(t, one.want, got, "CanonicalVerb(%q) answered %q", one.spelling, got)

			if named {
				require.Truef(t, ActionForVerb(DeviceKindPrinter, got).Valid(),
					"%q is suggested and takes no action, so a caller who sends it is refused again", got)
			}
		})
	}
}

// The suggestion is only worth carrying if it is the caller's verb and not some
// other one, so the fold has to be injective over the whole table. Two verbs
// folding together would send one of their callers to the wrong command.
func TestNoTwoVerbsShareASpelling(t *testing.T) {
	t.Parallel()

	for verb := range VerbActions(DeviceKindPrinter) {
		shouted := strings.ToUpper(strings.TrimPrefix(verb, verbPrefix))

		got, named := CanonicalVerb(shouted)
		require.Truef(t, named, "%s is spelled %s and nothing answers to it", verb, shouted)
		require.Equalf(t, verb, got, "%s and %s both answer to %s", verb, got, shouted)
	}
}

// The classification is a promise about what a caller must hold, so the actions
// it names have to be the published ones. An action here that Actions() does not
// carry would be published nowhere and checked everywhere.
func TestEveryActionAVerbTakesIsPublished(t *testing.T) {
	t.Parallel()

	published := map[Action]struct{}{}
	for _, action := range Actions() {
		published[action] = struct{}{}
	}

	for verb, action := range VerbActions(DeviceKindPrinter) {
		_, ok := published[action]
		require.Truef(t, ok, "%s takes %q, which the published action set does not carry", verb, action)
	}
}
