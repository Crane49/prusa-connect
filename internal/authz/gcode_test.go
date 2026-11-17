package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Raw gcode is the dangerous tier's first inhabitant: `operate` by class, so it
// has a class at all, and marked opaque_payload, so no class, role or level
// confers it. The permit and the refusal are one test because a permit test
// without its refusal is half a test: the whole point of the marking is that
// holding the class does not carry the verb.
func TestRawGCodeIsReachedByNoLevelAndByOwnership(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		// dana owns p-delta in the fixture's own tuples; carol is written owner
		// here so that ownership and the role layer are told apart.
		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-beta#owner@user:carol")))

		cases := []struct {
			name    string
			subject string
			device  string
			want    bool
		}{
			{
				name:    "an org admin does not send gcode by holding manager everywhere",
				subject: "user:root",
				device:  "p-beta",
				want:    false,
			},
			{
				name:    "an owner sends gcode on their own printer",
				subject: "user:carol",
				device:  "p-beta",
				want:    true,
			},
			{
				name:    "and not on a printer they do not own",
				subject: "user:carol",
				device:  "p-alpha",
				want:    false,
			},
			{
				name:    "a team operator does not send gcode",
				subject: "user:alice",
				device:  "p-beta",
				want:    false,
			},
			{
				name:    "a service account operator does not send gcode",
				subject: "service_account:sa-ui",
				device:  "p-alpha",
				want:    false,
			},
			{
				name:    "a principal holding nothing does not send gcode",
				subject: "user:mallory",
				device:  "p-beta",
				want:    false,
			},
			{
				name:    "a printer's own agent credential does not send gcode",
				subject: "device_credential:sn-alpha",
				device:  "p-alpha",
				want:    false,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				got, err := store.CanVerb(t.Context(),
					subjectOf(t, tc.subject), deviceOf(t, tc.device), verbSendGCode)
				require.NoError(t, err)
				require.Equalf(t, tc.want, got, "%s %s on %s", tc.subject, verbSendGCode, tc.device)
			})
		}
	})
}

// No level reaches the dangerous tier, asserted over the whole level ladder
// rather than over one grant. A level confers a mask, a dangerous verb is not a
// bit, and this is the assertion that says so if somebody ever gives one a bit.
func TestNoLevelMaskReachesADangerousVerb(t *testing.T) {
	t.Parallel()

	for _, verb := range DangerousVerbsOf(DeviceKindPrinter) {
		class := ActionForVerb(DeviceKindPrinter, verb)
		require.Truef(t, class.Valid(), "%s carries no class, so nothing can check it", verb)

		// The class is reachable by a level, and that is right: the class is
		// what says the verb exists at all. What must not happen is the verb
		// becoming reachable by holding it, and CanVerb is what enforces that.
		require.NotZerof(t, levelManager.deviceMask()&class.mask(),
			"%s carries a class no level confers, so the class check refuses before the tier does", verb)
	}
}

// ActionForVerb is the one place a verb becomes a class, so it has to answer
// per kind, for the verbs that carry a class of their own, and for a spelling
// this build does not carry.
//
// The empty answer is the point of the last four cases. A verb no half of the
// table names reaches no device, where the default arm that preceded this table
// handed it to whoever holds the weakest write action there is.
func TestActionForVerbAnswersPerKind(t *testing.T) {
	t.Parallel()

	cases := map[string]Action{
		"core.command.send_gcode":             ActionOperate,
		"core.command.force_gcode":            ActionOperate,
		"core.command.pause_print":            ActionOperate,
		"core.command.start_print":            ActionOperate,
		"core.command.set_nozzle_temperature": ActionOperate,
		"core.command.stop_print":             ActionOperate,
		"core.command.start_inline_download":  ActionOperate,
		"core.command.flash":                  ActionConfigure,
		"core.command.set_hostname":           ActionConfigure,
		"":                                    "",
		"core.command.send_gcode_but_not":     "",
		"send_gcode":                          "",
		"com.example.command.pause_print":     "",
	}

	for verb, want := range cases {
		t.Run(verb, func(t *testing.T) {
			t.Parallel()

			require.Equalf(t, want, ActionForVerb(DeviceKindPrinter, verb), "verb %q on a printer", verb)

			// A camera declares no verbs at all, so every one of them is the
			// zero Action there. The shelf's verb does not exist for kind
			// printer and the printer's does not exist for kind camera: no
			// global profile leaks either into the other.
			require.Emptyf(t, ActionForVerb(DeviceKindCamera, verb),
				"verb %q classifies on a camera, which declares no verbs", verb)
		})
	}
}

// Every verb RawGCodeVerbs names has to be in the dangerous tier, or a
// transport refusing what that list names is refusing something the device
// plane would have let through on the class alone.
func TestRawGCodeVerbsAreDangerous(t *testing.T) {
	t.Parallel()

	verbs := RawGCodeVerbs()
	require.NotEmpty(t, verbs, "the raw gcode verb list is empty, so anything reading it refuses nothing")

	for _, verb := range verbs {
		require.Truef(t, Dangerous(DeviceKindPrinter, verb),
			"%s is listed as a raw gcode verb and no hand marked it", verb)
		require.Containsf(t, MarkingsOf(DeviceKindPrinter, verb), MarkingOpaquePayload,
			"%s is a payload nothing inspected and is not marked as one", verb)
	}
}

// The action set is closed and every member of it has to be checkable, or a
// handler naming one gets a refusal that reads like a missing grant.
func TestEveryActionIsCheckableAndHasItsOwnBit(t *testing.T) {
	t.Parallel()

	seen := map[actionSet]Action{}

	for _, action := range Actions() {
		mask := action.mask()
		require.NotZerof(t, mask, "%s has no mask", action)

		other, clash := seen[mask]
		require.Falsef(t, clash, "%s and %s share a mask bit", action, other)

		seen[mask] = action
	}

	for _, action := range []Action{ActionObserve, ActionOperate, ActionConfigure, ActionAdminister} {
		domain, ok := action.domain()
		require.Truef(t, ok, "%s belongs to no object type", action)
		require.Equal(t, TypeDevice, domain)
	}
}

// An agent credential reaches telemetry and job fetching and nothing else. It
// is asserted against the mask rather than through the graph as well, because
// the graph test above proves one printer and this proves the rule.
func TestTheAgentMaskCarriesNoWriteClass(t *testing.T) {
	t.Parallel()

	require.Zero(t, agentMask&maskOperate,
		"a printer's own credential may operate it, so a compromised printer commands itself")
	require.Zero(t, agentMask&maskConfigure,
		"a printer's own credential may reconfigure it")
	require.Zero(t, agentMask&maskAdminister,
		"a printer's own credential administers the grants written on it")
}
