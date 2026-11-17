package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// decisionCase is one point check and the answer the schema owes it. Every rule
// appears twice, once permitting and once refusing: the refusal is where a leak
// would be.
type decisionCase struct {
	name    string
	subject string
	action  Action
	device  string
	want    bool
}

var decisionCases = []decisionCase{
	// A direct per-printer grant, and the printers it does not reach.
	{
		name:    "direct grant views its printer",
		subject: "user:dana",
		action:  ActionObserve,
		device:  "p-delta",
		want:    true,
	},
	{
		name:    "direct grant reaches no other printer",
		subject: "user:dana",
		action:  ActionObserve,
		device:  "p-alpha",
		want:    false,
	},
	{
		name:    "a viewer does not command",
		subject: "user:dana",
		action:  ActionOperate,
		device:  "p-delta",
		want:    false,
	},

	// A grant held by a team, through the printer group it was written on.
	{
		name:    "team operator commands the group's printer",
		subject: "user:alice",
		action:  ActionOperate,
		device:  "p-beta",
		want:    true,
	},
	{name: "a non-member does not", subject: "user:bob", action: ActionOperate, device: "p-beta", want: false},
	{name: "team operator uploads", subject: "user:alice", action: ActionOperate, device: "p-beta", want: true},
	{
		name:    "team operator cancels a job",
		subject: "user:alice",
		action:  ActionOperate,
		device:  "p-beta",
		want:    true,
	},
	{
		name:    "team operator does not manage",
		subject: "user:alice",
		action:  ActionAdminister,
		device:  "p-beta",
		want:    false,
	},

	// A nested printer group inherits its parent's grants.
	{
		name:    "grant on the parent group reaches the child's printer",
		subject: "user:alice",
		action:  ActionOperate,
		device:  "p-alpha",
		want:    true,
	},
	{
		name:    "the child group's printer stays out of reach otherwise",
		subject: "user:dana",
		action:  ActionOperate,
		device:  "p-alpha",
		want:    false,
	},

	// A team nested inside another team inherits its grants.
	{
		name:    "nested team views the outer team's printer",
		subject: "user:alice",
		action:  ActionObserve,
		device:  "p-gamma",
		want:    true,
	},
	{
		name:    "nested team inherits the level, not more",
		subject: "user:alice",
		action:  ActionOperate,
		device:  "p-gamma",
		want:    false,
	},
	{
		name:    "outer team member views it too",
		subject: "user:bob",
		action:  ActionObserve,
		device:  "p-gamma",
		want:    true,
	},

	// A printer in a group the principal cannot see.
	{
		name:    "an unrelated printer group is invisible",
		subject: "user:bob",
		action:  ActionObserve,
		device:  "p-alpha",
		want:    false,
	},
	{
		name:    "and its parent group too",
		subject: "user:bob",
		action:  ActionObserve,
		device:  "p-beta",
		want:    false,
	},

	// A service account scoped to three printers and nothing else.
	{
		name:    "service account commands its first printer",
		subject: "service_account:sa-ui",
		action:  ActionOperate,
		device:  "p-alpha",
		want:    true,
	},
	{
		name:    "service account commands its second printer",
		subject: "service_account:sa-ui",
		action:  ActionOperate,
		device:  "p-beta",
		want:    true,
	},
	{
		name:    "service account commands its third printer",
		subject: "service_account:sa-ui",
		action:  ActionOperate,
		device:  "p-delta",
		want:    true,
	},
	{
		name:    "service account reaches nothing else",
		subject: "service_account:sa-ui",
		action:  ActionObserve,
		device:  "p-gamma",
		want:    false,
	},
	{
		name:    "service account gets a level, not a blanket",
		subject: "service_account:sa-ui",
		action:  ActionAdminister,
		device:  "p-alpha",
		want:    false,
	},

	// An org role, and the block that overrides it on one printer.
	{
		name:    "org viewer sees an unblocked printer",
		subject: "user:olga",
		action:  ActionObserve,
		device:  "p-beta",
		want:    true,
	},
	{
		name:    "a block beats the org role",
		subject: "user:olga",
		action:  ActionObserve,
		device:  "p-gamma",
		want:    false,
	},
	{
		name:    "org viewer does not command",
		subject: "user:olga",
		action:  ActionOperate,
		device:  "p-beta",
		want:    false,
	},
	{
		name:    "org admin manages every printer",
		subject: "user:root",
		action:  ActionAdminister,
		device:  "p-gamma",
		want:    true,
	},
	{
		name: "org admin is not everyone", subject: "user:alice",
		action: ActionAdminister, device: "p-gamma", want: false,
	},

	// A printer's own credential: self-scoped, and no role.
	{
		name:    "the agent reports for its own printer",
		subject: "device_credential:sn-alpha",
		action:  ActionReportTelemetry,
		device:  "p-alpha",
		want:    true,
	},
	{
		name:    "a stolen agent credential cannot report for another printer",
		subject: "device_credential:sn-alpha",
		action:  ActionReportTelemetry,
		device:  "p-beta",
		want:    false,
	},
	{
		name:    "the agent fetches its own job",
		subject: "device_credential:sn-alpha",
		action:  ActionFetchJob,
		device:  "p-alpha",
		want:    true,
	},
	{
		name:    "an agent credential is not an operator credential",
		subject: "device_credential:sn-alpha",
		action:  ActionOperate,
		device:  "p-alpha",
		want:    false,
	},
	{
		name:    "the agent views its own telemetry",
		subject: "device_credential:sn-alpha",
		action:  ActionObserve,
		device:  "p-alpha",
		want:    true,
	},
	{
		name:    "a device with no agent tuple reaches nothing",
		subject: "device_credential:sn-gamma",
		action:  ActionObserve,
		device:  "p-gamma",
		want:    false,
	},
	{
		name:    "an operator does not report telemetry",
		subject: "user:alice",
		action:  ActionReportTelemetry,
		device:  "p-beta",
		want:    false,
	},
	{
		name:    "an org admin does not fetch a job",
		subject: "user:root",
		action:  ActionFetchJob,
		device:  "p-alpha",
		want:    false,
	},

	// A peer node is a principal like any other.
	{
		name:    "a node views what it was granted",
		subject: "node:site-b",
		action:  ActionObserve,
		device:  "p-gamma",
		want:    true,
	},
	{
		name:    "a node reaches nothing else",
		subject: "node:site-b",
		action:  ActionObserve,
		device:  "p-beta",
		want:    false,
	},

	// No tuple, no access.
	{
		name:    "a principal with no grant sees nothing",
		subject: "user:mallory",
		action:  ActionObserve,
		device:  "p-alpha",
		want:    false,
	},
	{
		name:    "and cannot command either",
		subject: "user:mallory",
		action:  ActionOperate,
		device:  "p-alpha",
		want:    false,
	},

	// A grant on a printer the directory does not know fails closed.
	{
		name:    "an unregistered printer is unreachable",
		subject: "user:dana",
		action:  ActionAdminister,
		device:  "p-hidden",
		want:    false,
	},
	{name: "even for the org admin", subject: "user:root", action: ActionAdminister, device: "p-hidden", want: false},
}

func TestCan(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, tc := range decisionCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				got, err := store.Can(t.Context(), subjectOf(t, tc.subject), tc.action, deviceOf(t, tc.device))
				require.NoError(t, err)
				require.Equalf(t, tc.want, got, "%s %s on %s", tc.subject, tc.action, tc.device)
			})
		}
	})
}

func TestRequire(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		t.Run("permits what Can permits", func(t *testing.T) {
			t.Parallel()

			require.NoError(t, store.Require(
				t.Context(), subjectOf(t, "user:alice"), ActionOperate, deviceOf(t, "p-beta"),
			))
		})

		t.Run("refuses with a documented code", func(t *testing.T) {
			t.Parallel()

			err := store.Require(t.Context(), subjectOf(t, "user:bob"), ActionOperate, deviceOf(t, "p-beta"))
			require.Error(t, err)
			requireCode(t, err, CodeAccessDenied())
		})
	})
}

func TestCanRejectsWhatIsNotCheckable(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		t.Run("a userset is not a caller", func(t *testing.T) {
			t.Parallel()

			userset, err := NewUserset(TypeGroup, "maintenance", RelMember)
			require.NoError(t, err)

			_, err = store.Can(t.Context(), userset, ActionObserve, deviceOf(t, "p-alpha"))
			requireCode(t, err, CodeInvalidSubject())
		})

		t.Run("an action outside the vocabulary", func(t *testing.T) {
			t.Parallel()

			_, err := store.Can(
				t.Context(),
				subjectOf(t, "user:root"),
				Action("delete_everything"),
				deviceOf(t, "p-alpha"),
			)
			requireCode(t, err, CodeUnknownAction())
		})

		t.Run("a type that holds grants rather than making requests", func(t *testing.T) {
			t.Parallel()

			for _, text := range []string{"org:acme", "device_group:floor-one", "device:p-beta", "group:maintenance"} {
				_, err := store.Can(t.Context(), subjectOf(t, text), ActionObserve, deviceOf(t, "p-alpha"))
				requireCode(t, err, CodeInvalidSubject())
			}
		})

		t.Run("an object type with no actions", func(t *testing.T) {
			t.Parallel()

			object, err := NewObject(TypeDeviceGroup, "floor-one")
			require.NoError(t, err)

			_, err = store.Can(t.Context(), subjectOf(t, "user:root"), ActionObserve, object)
			requireCode(t, err, CodeUnsupportedObject())
		})
	})
}

// TestRevocation is the other half of every permit above: a grant that is taken
// away has to stop working on both surfaces, in the same instant.
func TestRevocation(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		t.Run("a revoked direct grant", func(t *testing.T) {
			t.Parallel()

			store := seedFixture(t, e)
			dana := subjectOf(t, "user:dana")

			allowed, err := store.Can(t.Context(), dana, ActionObserve, deviceOf(t, "p-delta"))
			require.NoError(t, err)
			require.True(t, allowed, "the grant has to work before it is revoked")

			require.NoError(t, store.Delete(t.Context(), mustTuple(t, "device:p-delta#viewer@user:dana")))

			allowed, err = store.Can(t.Context(), dana, ActionObserve, deviceOf(t, "p-delta"))
			require.NoError(t, err)
			require.False(t, allowed)

			rows, err := store.ListDevices(t.Context(), dana, ActionObserve, Page{})
			require.NoError(t, err)
			require.Empty(t, rows, "the list has to lose it in the same instant")
		})

		t.Run("a revoked team membership", func(t *testing.T) {
			t.Parallel()

			store := seedFixture(t, e)
			alice := subjectOf(t, "user:alice")

			require.NoError(t, store.Delete(t.Context(), mustTuple(t, "group:maintenance#member@user:alice")))

			for _, id := range []string{"p-alpha", "p-beta", "p-gamma"} {
				allowed, err := store.Can(t.Context(), alice, ActionObserve, deviceOf(t, id))
				require.NoError(t, err)
				require.Falsef(t, allowed, "%s stayed visible after the membership was revoked", id)
			}
		})

		t.Run("a revoked block restores the org role", func(t *testing.T) {
			t.Parallel()

			store := seedFixture(t, e)
			olga := subjectOf(t, "user:olga")

			require.NoError(t, store.Delete(t.Context(), mustTuple(t, "device:p-gamma#blocked@user:olga")))

			allowed, err := store.Can(t.Context(), olga, ActionObserve, deviceOf(t, "p-gamma"))
			require.NoError(t, err)
			require.True(t, allowed)
		})

		t.Run("a printer removed from the directory", func(t *testing.T) {
			t.Parallel()

			store := seedFixture(t, e)
			root := subjectOf(t, "user:root")

			require.NoError(t, store.RemoveDevice(t.Context(), "p-beta"))

			allowed, err := store.Can(t.Context(), root, ActionAdminister, deviceOf(t, "p-beta"))
			require.NoError(t, err)
			require.False(t, allowed)
		})
	})
}

// TestEpoch covers the counter a cache or a live subscription is stamped with:
// every write moves it, so a stale decision is detectable. It also holds the
// cached value the stream hub reads to the row it mirrors, because a cache that
// silently stops tracking is the shape of stale authorization nobody notices.
func TestEpoch(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		before := store.Epoch()
		require.NotZero(t, before, "the cached epoch is seeded by Migrate, not left at zero")

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-delta#viewer@user:mallory")))

		after := store.Epoch()
		require.Greater(t, after, before)

		stored, err := store.readEpoch(t.Context(), store.db)
		require.NoError(t, err)
		require.Equal(t, stored, after, "the cached epoch and its row disagree")
	})
}

// TestOrgTenancyThroughAGroup covers the path a printer takes when its tenancy
// is written on its group rather than on the device: the org admin who is
// supposed to reach the whole org has to reach it, and the admin of another org
// still must not.
func TestOrgTenancyThroughAGroup(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-grouped", Name: "Grouped"},
			DeviceRow{ID: "p-nested", Name: "Nested"},
			DeviceRow{ID: "p-elsewhere", Name: "Elsewhere"},
		))
		require.NoError(t, store.Write(t.Context(), mustTuples(t,
			// Tenancy on the group only: neither printer names an org.
			"device:p-grouped#group@device_group:floor-nine",
			"device_group:floor-nine#org@org:acme",
			"device:p-nested#group@device_group:cell-nine",
			"device_group:cell-nine#parent@device_group:floor-nine",
			// A second org, so reaching "any org" is not enough to pass.
			"device:p-elsewhere#group@device_group:floor-ten",
			"device_group:floor-ten#org@org:other",
			"org:acme#admin@user:root",
			"org:other#admin@user:otherroot",
		)...))

		cases := []struct {
			name    string
			subject string
			device  string
			want    bool
		}{
			{
				name:    "the org admin reaches a printer through its group's org",
				subject: "user:root",
				device:  "p-grouped",
				want:    true,
			},
			{name: "and through a nested group's org", subject: "user:root", device: "p-nested", want: true},
			{name: "but not into another org", subject: "user:root", device: "p-elsewhere"},
			{
				name:    "the other org's admin reaches its own",
				subject: "user:otherroot",
				device:  "p-elsewhere",
				want:    true,
			},
			{name: "and not the first org's", subject: "user:otherroot", device: "p-grouped"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				allowed, err := store.Can(
					t.Context(),
					subjectOf(t, tc.subject),
					ActionAdminister,
					deviceOf(t, tc.device),
				)
				require.NoError(t, err)
				require.Equal(t, tc.want, allowed)

				listed := listedIDs(t, store, subjectOf(t, tc.subject), ActionAdminister)
				require.Equal(t, tc.want, listed[tc.device], "the list has to agree with the check")
			})
		}
	})
}
