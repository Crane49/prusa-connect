package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The four verbs the vocabulary moved above operator, written out so a rename
// in the table fails here rather than leaving this a test of four dead strings.
var provisioningVerbs = []string{
	"core.command.flash",
	"core.command.upgrade",
	"core.command.set_token",
	"core.command.set_hostname",
}

// Flashing firmware is a manager action. The permit and the refusal are one
// test, because a permit test without its refusal is half a test: the whole
// point of the action is that holding send_command does not carry it.
//
// user:carol is written here rather than in the shared fixture. A direct
// manager grant on a registered device is what proves the level rather than the
// org role above it, and the fixture's only device#manager names a device the
// directory does not have.
func TestProvisionIsAManagerActionAndNotAnOperatorOne(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-beta#manager@user:carol")))

		cases := []struct {
			name    string
			subject string
			device  string
			action  Action
			want    bool
		}{
			{
				name:    "an org admin flashes firmware",
				subject: "user:root",
				device:  "p-beta",
				action:  ActionConfigure,
				want:    true,
			},
			{
				name:    "a manager of that device flashes firmware",
				subject: "user:carol",
				device:  "p-beta",
				action:  ActionConfigure,
				want:    true,
			},
			{
				name:    "a team operator commands the same device",
				subject: "user:alice",
				device:  "p-beta",
				action:  ActionOperate,
				want:    true,
			},
			{
				name:    "and that operator does not flash it",
				subject: "user:alice",
				device:  "p-beta",
				action:  ActionConfigure,
				want:    false,
			},
			{
				name:    "a service account operator does not flash",
				subject: "service_account:sa-ui",
				device:  "p-alpha",
				action:  ActionConfigure,
				want:    false,
			},
			{
				name:    "a peer node holding viewer does not flash",
				subject: "node:site-b",
				device:  "p-gamma",
				action:  ActionConfigure,
				want:    false,
			},
			{
				name:    "a viewer does not flash",
				subject: "user:dana",
				device:  "p-delta",
				action:  ActionConfigure,
				want:    false,
			},
			{
				name:    "a principal holding nothing does not flash",
				subject: "user:mallory",
				device:  "p-beta",
				action:  ActionConfigure,
				want:    false,
			},
			{
				name:    "a device's own credential does not flash itself",
				subject: "device_credential:sn-alpha",
				device:  "p-alpha",
				action:  ActionConfigure,
				want:    false,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				got, err := store.Can(t.Context(), subjectOf(t, tc.subject), tc.action, deviceOf(t, tc.device))
				require.NoError(t, err)
				require.Equalf(t, tc.want, got, "%s %s on %s", tc.subject, tc.action, tc.device)
			})
		}
	})
}

// The four verbs take provision and nothing else. ActionForVerb is the one
// place a verb becomes an action, so a verb that slipped back to send_command
// would be reachable by every operator in the deployment.
func TestTheFourIrreversibleVerbsTakeProvision(t *testing.T) {
	t.Parallel()

	for _, verb := range provisioningVerbs {
		t.Run(verb, func(t *testing.T) {
			t.Parallel()

			require.Equalf(t, ActionConfigure, ActionForVerb(DeviceKindPrinter, verb),
				"%s reaches a printer under a different action", verb)
		})
	}

	// The other direction: nothing else took provision by accident. A verb that
	// an operator needs for day-to-day work would stop reaching them.
	var carrying []string

	for verb, action := range VerbActions(DeviceKindPrinter) {
		if action == ActionConfigure {
			carrying = append(carrying, verb)
		}
	}

	require.ElementsMatch(t, provisioningVerbs, carrying,
		"the set of verbs taking provision is not the four this action was declared for")
}

// The action set is closed and every member of it has to be checkable, or a
// handler naming one gets a refusal that reads like a missing grant.
func TestProvisionIsInTheClosedActionSet(t *testing.T) {
	t.Parallel()

	require.True(t, ActionConfigure.Valid(), "provision has no mask, so every check of it refuses")
	require.Contains(t, Actions(), ActionConfigure)

	domain, ok := ActionConfigure.domain()
	require.True(t, ok, "provision belongs to no object type")
	require.Equal(t, TypeDevice, domain)

	require.Zero(t, agentMask&maskConfigure,
		"a device's own credential reaches provision, so a compromised printer flashes itself")
	require.Zero(t, levelOperator.deviceMask()&maskConfigure,
		"the operator level reaches provision")
	require.NotZero(t, levelManager.deviceMask()&maskConfigure,
		"the manager level does not reach provision, so nobody below org admin can flash")
}

// The mask a projected row carries is stored as one integer, so the bit an
// action occupies is part of the on-disk format. provision is the highest bit
// this vocabulary declares, which is what says it was appended rather than
// inserted: a bit that moved would reinterpret every row already written.
//
// write_storage is the last one today. The whole set was renumbered once, when
// the class set replaced the action names, and that was safe exactly once:
// nothing had shipped and no tuple existed outside a development database. It
// is not a precedent, and this assertion is what says so.
func TestTheLastDeclaredActionTakesTheHighestBit(t *testing.T) {
	t.Parallel()

	declared := Actions()
	last := declared[len(declared)-1]

	var others actionSet

	for _, action := range declared[:len(declared)-1] {
		others |= action.mask()
	}

	require.Greater(t, last.mask(), others,
		"the last declared action does not occupy the highest bit, so an action's bit moved")
}
