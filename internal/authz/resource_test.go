package authz

import (
	"database/sql"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// TestResourceRefusalWithoutAGrant holds the guard on this surface. The API can
// create, edit and delete a storage backend, an auth provider or an event sink,
// and the principal is threaded through every store method, so a method that
// takes it and enforces nothing looks identical from the call site. mallory
// holds nothing, and the refusal is asserted on both enforcement surfaces
// rather than on the one that is easiest to reach.
func TestResourceRefusalWithoutAGrant(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		mallory := subjectOf(t, "user:mallory")

		for _, action := range resourceActions() {
			// The point check, on every instance the server has.
			for _, instance := range resourceUniverse(t) {
				allowed, err := store.CanResource(t.Context(), mallory, action, instance)
				require.NoError(t, err)
				require.Falsef(t, allowed, "%s on %s", action, instance)

				require.Error(t, store.RequireResource(t.Context(), mallory, action, instance))
			}

			// The kind-level check a create goes through, which names no
			// instance and so cannot be covered by the loop above.
			for _, kind := range ref.ConfigurableKinds() {
				allowed, err := store.Can(t.Context(), mallory, action, kindOf(t, kind))
				require.NoError(t, err)
				require.Falsef(t, allowed, "%s on resource_kind:%s", action, kind)
			}

			// The list filter.
			listed, err := store.ListResources(t.Context(), mallory, action)
			require.NoError(t, err)
			require.Emptyf(t, listed, "%s listed something", action)
		}

		// The event stream filter.
		visible, err := store.Visible(t.Context(), mallory)
		require.NoError(t, err)
		require.Empty(t, visible.Resources)
		require.Empty(t, visible.Devices)
	})
}

// TestResourceGrantsReachTheRightActions is the permit half. Every case names
// the path the grant takes, because a permit that arrives by the wrong path is
// a permit that will not be revoked by deleting the tuple somebody thinks
// granted it.
func TestResourceGrantsReachTheRightActions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		who      string
		kind     ref.Kind
		id       string
		view     bool
		operate  bool
		configre bool
	}{
		{
			name: "a direct manager configures the kind",
			who:  "user:dana", kind: ref.KindStorage, id: "s3-main",
			view: true, operate: true, configre: true,
		},
		{
			name: "an org admin reaches a kind that names the org",
			who:  "user:root", kind: ref.KindSink, id: "webhook",
			view: true, operate: true, configre: true,
		},
		{
			name: "an org admin does not reach a kind that does not",
			who:  "user:root", kind: ref.KindCamera, id: "front",
		},
		{
			name: "an org viewer views a kind that names the org",
			who:  "user:olga", kind: ref.KindSink, id: "webhook",
			view: true,
		},
		{
			name: "a block cuts the org role on that kind",
			who:  "user:olga", kind: ref.KindStorage, id: "local",
		},
		{
			name: "a group operator operates but does not configure",
			who:  "user:alice", kind: ref.KindSink, id: "webhook",
			view: true, operate: true,
		},
		{
			name: "a peer node holds the grant it was given",
			who:  "node:site-b", kind: ref.KindSink, id: "webhook",
			view: true,
		},
		{
			name: "a direct viewer on a kind with no org",
			who:  "user:bob", kind: ref.KindCamera, id: "front",
			view: true,
		},
		{
			name: "a printer credential reaches no configuration",
			who:  "device_credential:sn-alpha", kind: ref.KindStorage, id: "local",
		},
		{
			name: "a service account scoped to printers reaches no configuration",
			who:  "service_account:sa-ui", kind: ref.KindStorage, id: "local",
		},
	}

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, tc.who)
				instance := instanceOf(t, tc.kind, tc.id)

				for _, want := range []struct {
					action Action
					allow  bool
				}{
					{ActionViewResource, tc.view},
					{ActionOperateResource, tc.operate},
					{ActionConfigureResource, tc.configre},
					// The contents actions ride the same two levels, so every
					// case above states them too: read_storage arrives with the
					// viewer column and write_storage with the operator one.
					{ActionReadStorage, tc.view},
					{ActionWriteStorage, tc.operate},
				} {
					allowed, err := store.CanResource(t.Context(), who, want.action, instance)
					require.NoError(t, err)
					require.Equalf(t, want.allow, allowed, "%s %s on %s", tc.who, want.action, instance)
				}
			})
		}
	})
}

// TestTheContentsActionsAreNotTheDeclarationActions is the boundary the two
// actions exist for, stated in both directions on one instance. A viewer reads
// what is inside and may not write it; an operator writes what is inside and
// still may not rewrite the instance's declaration. If read_storage or
// write_storage ever became reachable only at manager, or reached
// configure_resource with it, this is what would say so.
func TestTheContentsActionsAreNotTheDeclarationActions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		who       string
		read      bool
		write     bool
		configure bool
	}{
		{name: "an org viewer reads the contents and does not write them", who: "user:olga", read: true},
		{
			name: "a group operator writes the contents and does not rewrite the instance",
			who:  "user:alice", read: true, write: true,
		},
		{
			name: "a manager holds both and the declaration as well",
			who:  "user:root", read: true, write: true, configure: true,
		},
		{name: "a principal with no grant holds neither", who: "user:mallory"},
	}

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		instance := instanceOf(t, ref.KindSink, "webhook")

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, tc.who)

				for _, want := range []struct {
					action Action
					allow  bool
				}{
					{ActionReadStorage, tc.read},
					{ActionWriteStorage, tc.write},
					{ActionConfigureResource, tc.configure},
				} {
					allowed, err := store.CanResource(t.Context(), who, want.action, instance)
					require.NoError(t, err)
					require.Equalf(t, want.allow, allowed, "%s %s on %s", tc.who, want.action, instance)
				}
			})
		}
	})
}

// TestResourceCheckMatchesTheGraphWalk holds the resource projection to a walk
// of the raw tuples, the same way the printer projection is held. The oracle
// shares no table with the implementation, so a mask widened in vocabulary.go
// cannot widen the thing that judges it.
func TestResourceCheckMatchesTheGraphWalk(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		reference := newOracle(t, store)

		for _, name := range fixturePrincipals {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, name)

				for _, action := range resourceActions() {
					for _, instance := range resourceUniverse(t) {
						got, err := store.CanResource(t.Context(), who, action, instance)
						require.NoError(t, err)
						require.Equalf(t, reference.canResource(who, action, instance), got,
							"%s %s on %s", name, action, instance)
					}
				}
			})
		}
	})
}

// TestResourceListMatchesThePointCheck is an earlier design decision on this surface: for
// every principal and every resource action, the set the list returns equals
// the set the point check permits. A list that fetched and then filtered would
// pass one and fail the other.
func TestResourceListMatchesThePointCheck(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, name := range fixturePrincipals {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, name)

				for _, action := range resourceActions() {
					permitted := []string{}

					for _, instance := range resourceUniverse(t) {
						allowed, err := store.CanResource(t.Context(), who, action, instance)
						require.NoError(t, err)

						if allowed {
							permitted = append(permitted, instance.String())
						}
					}

					listed, err := store.ListResources(t.Context(), who, action)
					require.NoError(t, err)

					names := refStrings(listed)
					slices.Sort(permitted)
					slices.Sort(names)
					require.Equalf(t, permitted, names, "%s %s", name, action)
				}
			})
		}
	})
}

// TestStreamVisibilityMatchesTheOtherSurfaces is the third surface. What a
// subscription may attach to has to be exactly what the list returns and what
// the point check permits, or a permission is enforced on one transport and not
// the next, which is this project's most repeated defect.
func TestStreamVisibilityMatchesTheOtherSurfaces(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, name := range fixturePrincipals {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, name)

				visible, err := store.Visible(t.Context(), who)
				require.NoError(t, err)
				require.Equal(t, store.Epoch(), visible.Epoch)

				listed, err := store.ListResources(t.Context(), who, ActionViewResource)
				require.NoError(t, err)
				require.Equal(t, refStrings(listed), visibleStrings(visible.Resources))

				permitted := []string{}

				for _, id := range directoryIDs() {
					allowed, canErr := store.Can(t.Context(), who, ActionObserve, deviceOf(t, id))
					require.NoError(t, canErr)

					if allowed {
						permitted = append(permitted, id)
					}
				}

				got := deviceIDs(visible.Devices)
				slices.Sort(permitted)
				slices.Sort(got)
				require.Equal(t, permitted, got)
			})
		}
	})
}

// TestAbsenceIsIndistinguishableFromNoAccess is the property a refusal exists
// to keep. One reference, two worlds: in the first the instance is not there
// and the caller holds every grant, in the second it is there and the caller
// holds none. If the two refusals differ in one byte, a caller can enumerate
// the configuration it may not read.
func TestAbsenceIsIndistinguishableFromNoAccess(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		ghost := instanceOf(t, ref.KindStorage, "ghost")

		absent := seedFixture(t, e)
		// dana manages storage and the instance is not registered.
		absentErr := absent.RequireResource(
			t.Context(), subjectOf(t, "user:dana"), ActionViewResource, ghost)

		present := seedFixture(t, e)
		require.NoError(t, present.RegisterResources(t.Context(), ghost))
		// mallory holds nothing and the instance is registered.
		deniedErr := present.RequireResource(
			t.Context(), subjectOf(t, "user:mallory"), ActionViewResource, ghost)

		require.Error(t, absentErr)
		require.Error(t, deniedErr)
		requireCode(t, absentErr, CodeAccessDenied())
		requireCode(t, deniedErr, CodeAccessDenied())

		require.Equal(t, payloadJSON(t, absentErr), payloadJSON(t, deniedErr),
			"the refusal for an absent instance and the refusal for a forbidden one differ")
		require.Equal(t, absentErr.Error(), deniedErr.Error())
	})
}

// TestUnregisteredInstanceIsInvisible is the fail-closed direction of the
// directory: a grant on the kind reaches nothing that the server does not
// actually have.
func TestUnregisteredInstanceIsInvisible(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		orphan := instanceOf(t, ref.KindSink, "orphan")

		// root manages the sink kind, which is what makes this a directory
		// question rather than a grant question.
		allowed, err := store.Can(t.Context(), root, ActionConfigureResource, kindOf(t, ref.KindSink))
		require.NoError(t, err)
		require.True(t, allowed)

		allowed, err = store.CanResource(t.Context(), root, ActionConfigureResource, orphan)
		require.NoError(t, err)
		require.False(t, allowed, "an unregistered instance is reachable")

		listed, err := store.ListResources(t.Context(), root, ActionConfigureResource)
		require.NoError(t, err)
		require.NotContains(t, refStrings(listed), orphan.String())

		// Registering it makes it visible, so the refusal above was the
		// directory and not something else.
		require.NoError(t, store.RegisterResources(t.Context(), orphan))

		allowed, err = store.CanResource(t.Context(), root, ActionConfigureResource, orphan)
		require.NoError(t, err)
		require.True(t, allowed)

		// Unregistering it takes it away again on both surfaces at once.
		require.NoError(t, store.RemoveResource(t.Context(), orphan))

		allowed, err = store.CanResource(t.Context(), root, ActionConfigureResource, orphan)
		require.NoError(t, err)
		require.False(t, allowed)

		visible, err := store.Visible(t.Context(), root)
		require.NoError(t, err)
		require.NotContains(t, visibleStrings(visible.Resources), orphan.String())
	})
}

// TestTheTwoSurfacesDoNotReachEachOther keeps the printer graph and the
// configuration graph disjoint. A fleet-wide printer grant is not permission to
// rewrite the server's storage settings, and a storage manager is not an
// operator of anything that prints.
func TestTheTwoSurfacesDoNotReachEachOther(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		// root is an org admin, so he reaches every printer at manager level.
		root := subjectOf(t, "user:root")

		allowed, err := store.Can(t.Context(), root, ActionAdminister, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.True(t, allowed)

		// dana manages the storage kind and no printer above viewer.
		dana := subjectOf(t, "user:dana")

		allowed, err = store.CanResource(
			t.Context(), dana, ActionConfigureResource, instanceOf(t, ref.KindStorage, "local"))
		require.NoError(t, err)
		require.True(t, allowed)

		allowed, err = store.Can(t.Context(), dana, ActionAdminister, deviceOf(t, "p-delta"))
		require.NoError(t, err)
		require.False(t, allowed, "a storage manager became a printer manager")

		// Asking one domain's verb of the other's object is a caller bug, and
		// it is refused rather than answered false: the two masks share no bit,
		// so a miss would read like a denial.
		_, err = store.Can(t.Context(), root, ActionConfigureResource, deviceOf(t, "p-alpha"))
		requireCode(t, err, CodeUnsupportedObject())

		_, err = store.Can(t.Context(), root, ActionAdminister, kindOf(t, ref.KindStorage))
		requireCode(t, err, CodeUnsupportedObject())

		_, err = store.ListDevices(t.Context(), root, ActionViewResource, Page{})
		requireCode(t, err, CodeUnsupportedObject())

		_, err = store.ListResources(t.Context(), root, ActionObserve)
		requireCode(t, err, CodeUnsupportedObject())
	})
}

// TestAPrinterIsNeverAResource covers the seam the two closed sets meet at.
// ref.Kind now names a printer and a node so the event bus can address them,
// and a grant written on either as though it were a configuration surface must
// reach nothing at all.
func TestAPrinterIsNeverAResource(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		mallory := subjectOf(t, "user:mallory")

		// The tuple is writable: printer is a legal DNS-1123 id and the shape
		// is in the vocabulary. It compiles to no projected row, because the
		// projection is compiled over the configurable kinds and nothing else.
		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "resource_kind:device#manager@user:mallory"),
			mustTuple(t, "resource_kind:node#manager@user:mallory"),
		))

		for _, kind := range []ref.Kind{ref.KindPrinter, ref.KindNode} {
			allowed, err := store.Can(t.Context(), mallory, ActionConfigureResource, kindOf(t, kind))
			require.NoError(t, err)
			require.Falsef(t, allowed, "a grant on resource_kind:%s reached something", kind)
		}

		// It reaches no printer either, which is the confusion the two sets
		// sharing the name printer could otherwise cause.
		allowed, err := store.Can(t.Context(), mallory, ActionAdminister, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.False(t, allowed)

		// And a ref naming an intrinsic kind is refused by the resource
		// surface outright rather than answered.
		instance, err := ref.New(ref.KindPrinter, "p-alpha")
		require.NoError(t, err)

		_, err = store.CanResource(t.Context(), mallory, ActionViewResource, instance)
		requireCode(t, err, CodeUnknownType())

		require.Error(t, store.RegisterResources(t.Context(), instance))
	})
}

// TestADeviceCannotHoldAResourceGrant is the vocabulary refusing the shape
// rather than the projection dropping it. A stolen printer credential must not
// be grantable on the configuration surface at all.
func TestADeviceCannotHoldAResourceGrant(t *testing.T) {
	t.Parallel()

	object, err := NewObject(TypeResourceKind, "storage")
	require.NoError(t, err)

	subject, err := NewSubject(TypeDeviceCredential, "sn-alpha")
	require.NoError(t, err)

	_, err = NewTuple(object, RelManager, subject)
	requireCode(t, err, CodeInvalidSubject())
}

// TestResourceListOrderIsCollationIndependent holds the order to a property of
// the query rather than of the database the server was installed on. The ids
// differ only in a hyphen, which is where a Postgres locale and SQLite's byte
// order disagree.
func TestResourceListOrderIsCollationIndependent(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.RegisterResources(t.Context(),
			instanceOf(t, ref.KindSink, "ab"),
			instanceOf(t, ref.KindSink, "a-b"),
			instanceOf(t, ref.KindSink, "a1b"),
		))

		listed, err := store.ListResources(t.Context(), subjectOf(t, "user:root"), ActionViewResource)
		require.NoError(t, err)

		require.Equal(t, []string{
			"sink:a-b", "sink:a1b", "sink:ab", "sink:webhook",
			"storage:local", "storage:s3-main",
		}, refStrings(listed), "the order is the database's rather than the query's")
	})
}

// TestVisibleReadsTheEpochBeforeTheRows pins the ordering the revocation half of
// E10 rests on. A stamp taken after the rows would describe a graph newer than
// the one the answer came from, and the hub would never notice the change.
func TestVisibleReadsTheEpochBeforeTheRows(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")

		before, err := store.Visible(t.Context(), dana)
		require.NoError(t, err)
		require.NotEmpty(t, before.Resources)

		require.NoError(t, store.Delete(t.Context(),
			mustTuple(t, "resource_kind:storage#manager@user:dana")))

		after, err := store.Visible(t.Context(), dana)
		require.NoError(t, err)
		require.Greater(t, after.Epoch, before.Epoch,
			"a revoked grant did not move the generation a subscription is stamped with")
		require.Empty(t, after.Resources, "the revoked grant still names an origin")
	})
}

// TestVisibleStampsTheEpochItReadBefore is the ordering, observed rather than
// asserted. A stamp taken after the rows describes a graph newer than the
// answer it carries, and a subscription holding it would never re-authorize:
// the hub compares the two and sees agreement that is not there.
//
// The interleave is forced rather than raced. One connection is allowed, the
// test holds it, and the pool's wait counter says when Visible has reached its
// first query, which is after the point the epoch is read and before the point
// a stamp taken afterwards would be read. The graph is then moved underneath it.
func TestVisibleStampsTheEpochItReadBefore(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")

		// Two, not one: database/sql can hold a connection open for the
		// statement it is closing over, and one would deadlock the setup.
		store.db.SetMaxOpenConns(2)

		before := store.Epoch()

		held := make([]*sql.Conn, 0, 2)

		defer func() {
			for _, conn := range held {
				_ = conn.Close() // returning a connection reports nothing worth acting on
			}
		}()

		for range 2 {
			conn, err := store.db.Conn(t.Context())
			require.NoError(t, err)

			held = append(held, conn)
		}

		waited := store.db.Stats().WaitCount

		answer := make(chan Visibility, 1)

		go func() {
			visible, err := store.Visible(t.Context(), root)
			if err == nil {
				answer <- visible
			}

			close(answer)
		}()

		// Visible has reached a query and is waiting for a connection, so
		// whatever it reads from now on is read after this line.
		require.Eventually(t, func() bool {
			return store.db.Stats().WaitCount > waited
		}, time.Second, time.Millisecond, "Visible never reached its first query")

		// Move the graph underneath it, on the connection the test still holds.
		_, err := held[0].ExecContext(t.Context(),
			store.q(upsertResourceSQL), ref.KindSink.String(), "late")
		require.NoError(t, err)

		store.epoch.Store(before + 1)

		for _, conn := range held {
			require.NoError(t, conn.Close())
		}

		held = held[:0]

		visible, ok := <-answer
		require.True(t, ok, "Visible failed")

		require.Contains(t, visibleStrings(visible.Resources), "sink:late",
			"the rows were read before the change, so the interleave did not happen")
		require.Equal(t, before, visible.Epoch,
			"the answer is stamped with a generation newer than the rows it carries")
	})
}

// refStrings renders instances the way a tuple and an origin render one, so a
// TestSyncResourcesMakesTheDirectoryExactlyTheDeclaredSet is the reconcile a
// start makes, in one transaction. What was registered and is no longer
// declared goes, what is declared and missing arrives, and everything declared
// on both sides stays.
func TestSyncResourcesMakesTheDirectoryExactlyTheDeclaredSet(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		kept := instanceOf(t, ref.KindSink, "kept")
		departed := instanceOf(t, ref.KindSink, "departed")
		arriving := instanceOf(t, ref.KindStorage, "arriving")

		require.NoError(t, store.RegisterResources(t.Context(), kept, departed))

		before := store.Epoch()
		require.NoError(t, store.SyncResources(t.Context(), kept, arriving))

		registered, err := store.Resources(t.Context())
		require.NoError(t, err)
		require.ElementsMatch(t, refStrings([]ref.Ref{kept, arriving}), refStrings(registered))

		require.Equal(t, before+1, store.Epoch(),
			"a reconcile that removed one row and added another moved the epoch twice")
	})
}

// TestSyncResourcesWithNothingDeclaredEmptiesTheDirectory is the deployment
// whose last resource block was deleted. An empty set is a set, and a row left
// behind is an instance that exists to the graph and to nothing else.
func TestSyncResourcesWithNothingDeclaredEmptiesTheDirectory(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.RegisterResources(t.Context(),
			instanceOf(t, ref.KindSink, "one"), instanceOf(t, ref.KindSink, "two")))

		require.NoError(t, store.SyncResources(t.Context()))

		registered, err := store.Resources(t.Context())
		require.NoError(t, err)
		require.Empty(t, registered)
	})
}

// TestSyncResourcesThatChangesNothingDoesNotMoveTheEpoch is the ordinary
// restart. Every start reconciles, and a start with nothing to do must not
// invalidate a cached decision anywhere.
func TestSyncResourcesThatChangesNothingDoesNotMoveTheEpoch(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		declared := []ref.Ref{instanceOf(t, ref.KindSink, "steady")}
		require.NoError(t, store.RegisterResources(t.Context(), declared...))

		before := store.Epoch()
		require.NoError(t, store.SyncResources(t.Context(), declared...))
		require.Equal(t, before, store.Epoch())
	})
}

// comparison failure reads as the reference it is about.
// named renders one column of a listing, so the two halves of the answer Visible
// gives and the raw instance list are read by one mechanism rather than three.
func named[T any](rows []T, of func(T) string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, of(row))
	}

	return out
}

// deviceIDs is the device half of the answer Visible gives, without the
// generation each sight began at.
func deviceIDs(devices []VisibleDevice) []string {
	return named(devices, func(device VisibleDevice) string { return device.ID })
}

// visibleStrings is the instance half, read the way refStrings reads a raw list.
func visibleStrings(instances []VisibleResource) []string {
	return named(instances, func(instance VisibleResource) string { return instance.Ref.String() })
}

func refStrings(instances []ref.Ref) []string {
	return named(instances, ref.Ref.String)
}

// payloadJSON is what a transport would put on the wire for this error, which
// is the thing two refusals have to be identical in. It renders the canonical
// payload rather than the Go error, because that is what reaches a caller.
func payloadJSON(tb testing.TB, err error) string {
	tb.Helper()

	var carried *fault.Fault

	require.ErrorAsf(tb, err, &carried, "error carries no fault: %v", err)

	encoded, marshalErr := json.Marshal(carried.Payload())
	require.NoError(tb, marshalErr)

	return string(encoded)
}
