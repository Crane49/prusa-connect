package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// TestOwnershipAloneReachesEveryActionItIsMeantToAndNoOther sweeps the whole
// action set rather than the four the ownerMask comment names. The mask was
// assembled by hand, and a mask assembled by hand is checked against the closed
// set and not against the sentence that produced it.
//
// The expectation is written out from the owner's decision of 2026-08-27 -
// "commands are their prerogative" plus the four irreversible verbs - and not
// derived from ownerMask, so a bit added to the mask fails here.
func TestOwnershipAloneReachesEveryActionItIsMeantToAndNoOther(t *testing.T) {
	t.Parallel()

	// Every action on a device, by hand, for a principal whose only tuple is
	// device#owner. The resource actions are deliberately absent: the loop
	// below asks for each of them separately, on a resource kind, and requires
	// the answer to be no.
	//
	//nolint:exhaustive // the device half of the closed set; the loop covers the other half
	reached := map[Action]bool{
		ActionObserve:   true,
		ActionOperate:   true,
		ActionConfigure: true,
		// Audit sight is a read surface and not a class a command carries.
		// ADR-20260901-atx rule 12 keeps it at manager so that holding a machine does
		// not make somebody the reader of who else touched it.
		ActionViewAudit: false,
		// manage administers the grants on the device, which is not a command,
		// and it is what stops an owner handing ownership on.
		ActionAdminister: false,
		// Reachable from the agent relation alone, which is what keeps a device
		// credential from becoming an operator credential.
		ActionReportTelemetry: false,
		ActionFetchJob:        false,
	}

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")
		alpha := deviceOf(t, "p-alpha")

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-alpha#owner@user:dana")))

		for _, action := range Actions() {
			domain, ok := action.domain()
			require.True(t, ok)

			if domain != TypeDevice {
				// A device grant is not permission to rewrite the server's
				// configuration, and the two masks share no bit.
				allowed, err := store.Can(t.Context(), dana, action, kindOf(t, ref.KindSink))
				require.NoError(t, err)
				require.Falsef(t, allowed, "owning a device reaches %s on a resource kind", action)

				continue
			}

			want, named := reached[action]
			require.Truef(t, named, "%s is a device action nobody decided for an owner", action)

			allowed, err := store.Can(t.Context(), dana, action, alpha)
			require.NoError(t, err)
			require.Equalf(t, want, allowed, "an owner and %s", action)
		}
	})
}

// TestOwnershipIsNotAShapeOnAnythingButADevice holds both guards against the
// same relation on the wrong object type.
//
// The vocabulary carries device#owner and nothing else, and the tuple
// constructor is closed over it, so no such tuple can be written. That was the
// whole guard while conferred matched owner and agent ahead of its object-type
// switch: both answered with a device mask whatever object they were asked
// about, and one added relationSpecs row would have had the escalation rule
// comparing device bits against a resource kind's mask. conferred now decides
// the object type first, so it is its own guard and the vocabulary is the
// second one rather than the only one.
func TestOwnershipIsNotAShapeOnAnythingButADevice(t *testing.T) {
	t.Parallel()

	object, err := NewObject(TypeResourceKind, ref.KindStorage.String())
	require.NoError(t, err)

	subject, err := NewSubject(TypeUser, "mallory")
	require.NoError(t, err)

	_, err = NewTuple(object, RelOwner, subject)
	require.Error(t, err, "owner is writable on a resource kind")

	// The device masks the two relations carry share no bit with what a
	// resource kind's manager holds, so answering one of them for a kind would
	// be handing out an action off that object's surface entirely.
	require.Zero(t, ownerMask&levelManager.resourceMask())
	require.Zero(t, agentMask&levelManager.resourceMask())

	for _, relation := range []Relation{RelOwner, RelAgent} {
		require.Zerof(t, conferred(TypeResourceKind, relation),
			"conferred answers %s on a resource kind with a device mask", relation)
	}
}
