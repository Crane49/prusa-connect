package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
)

// commandPrefix is what a sendable verb is spelled under. A core.feature key
// shares the vocabulary and is a statement about a machine rather than a
// request, so no action governs one.
const commandPrefix = "core.command."

// The authorization graph names no printer package and the printer packages
// name no graph, so the two vocabularies meet in the composition root and
// nowhere else. This file is that meeting: it holds the verb every shipped
// profile publishes against the action authz decides for it.
//
// Both directions, which is ADR-20260825-bhe rule 8. A verb the profiles publish and
// the table does not classify is a verb nobody weighed, and it reaches no
// printer, so a client reads it in the capability vocabulary and is refused. A
// verb the table classifies and no profile publishes is a rule about something
// that is not there.
func TestEveryPublishedVerbTakesADecidedAction(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	require.NoError(t, err, "the shipped profiles do not load, so this gate reads nothing")

	published := publishedVerbs(t, registry)
	classified := authz.VerbActions(authz.DeviceKindPrinter)

	var unclassified, unpublished []string

	for _, verb := range published {
		if _, ok := classified[verb]; !ok {
			unclassified = append(unclassified, verb)
		}
	}

	for verb := range classified {
		if !slices.Contains(published, verb) {
			unpublished = append(unpublished, verb)
		}
	}

	slices.Sort(unclassified)
	slices.Sort(unpublished)

	require.Emptyf(t, unclassified,
		"these verbs are in the capability vocabulary and take no action, so every caller is refused "+
			"them: %v. Give each one a class in the printer's verb table, "+
			"internal/authz/generatedverbs.go", unclassified)
	require.Emptyf(t, unpublished,
		"these verbs carry a class in internal/authz/generatedverbs.go and no shipped profile "+
			"publishes them: %v. Remove the row, or add the verb to internal/printer/profile",
		unpublished)
}

// A printer action is what the graph confers on a printer. A verb taking a
// resource action would compile, publish and refuse every caller, because the
// two masks share no bit.
func TestEveryClassifiedVerbTakesAnActionTheGraphConfersOnAPrinter(t *testing.T) {
	t.Parallel()

	printerActions := map[authz.Action]struct{}{}

	for _, doc := range authz.ActionDocs() {
		if doc.Domain == authz.TypeDevice {
			printerActions[doc.Action] = struct{}{}
		}
	}

	require.NotEmpty(t, printerActions, "no action is defined on a printer, so this gate checks nothing")

	for verb, action := range authz.VerbActions(authz.DeviceKindPrinter) {
		_, ok := printerActions[action]
		require.Truef(t, ok, "%s takes %q, which is not an action defined on a printer", verb, action)
	}
}

// publishedVerbs is every core verb the shipped profiles carry, without the
// argument keys hanging off each one.
func publishedVerbs(t *testing.T, registry profile.Registry) []string {
	t.Helper()

	out := make([]string, 0, len(registry.Coverage()))

	for key := range registry.Coverage() {
		if _, isArgument := key.Argument(); isArgument {
			continue
		}

		// Only a command is sendable. A core.feature key states what a
		// machine supports and travels in no request, so there is nothing for
		// an action to govern. A key under somebody else's prefix belongs to
		// whoever declared it (ADR-20260825-bhe rule 13).
		if !strings.HasPrefix(key.String(), commandPrefix) {
			continue
		}

		out = append(out, key.String())
	}

	require.NotEmpty(t, out, "the shipped profiles publish no verb, so this gate checks nothing")

	slices.Sort(out)

	return out
}
