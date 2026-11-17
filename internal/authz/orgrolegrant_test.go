package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// What a caller has to know before it grants an action on the org: which role
// carries the action, and whether the principal it is granting to may hold that
// role there.

func TestTheSmallestOrgRoleThatReachesAnAction(t *testing.T) {
	t.Parallel()

	//nolint:exhaustive // the two actions no role reaches have a case of their own below.
	smallest := map[Action]Relation{
		ActionObserve:           RelViewer,
		ActionOperate:           RelOperator,
		ActionConfigure:         RelAdmin,
		ActionAdminister:        RelAdmin,
		ActionViewAudit:         RelAdmin,
		ActionViewResource:      RelViewer,
		ActionReadStorage:       RelViewer,
		ActionOperateResource:   RelOperator,
		ActionWriteStorage:      RelOperator,
		ActionConfigureResource: RelAdmin,
		ActionAdmitDevice:       RelAdmin,
		ActionDevelopDevice:     RelAdmin,
	}

	for action, want := range smallest {
		held, conferred := OrgRoleConferring(action)
		require.True(t, conferred, "%s is reached by an org role", action)
		require.Equal(t, want, held, "the smallest org role that reaches %s", action)
	}
}

func TestNoOrgRoleReachesADeviceCredentialsOwnActions(t *testing.T) {
	t.Parallel()

	for _, action := range []Action{ActionReportTelemetry, ActionFetchJob} {
		_, conferred := OrgRoleConferring(action)
		require.False(t, conferred, "%s comes from a device credential and from no role", action)
	}
}

func TestAnActionOutsideTheClosedSetReachesNoOrgRole(t *testing.T) {
	t.Parallel()

	_, conferred := OrgRoleConferring(Action("read_everything"))
	require.False(t, conferred)
}

func TestAModuleHoldsOnlyTheViewerRoleOnTheOrg(t *testing.T) {
	t.Parallel()

	require.True(t, Grantable(TypeOrg, RelViewer, TypeSink))
	require.False(t, Grantable(TypeOrg, RelOperator, TypeSink))
	require.False(t, Grantable(TypeOrg, RelAdmin, TypeSink))

	// A person holds all three, which is what makes the sink's stop at viewer a
	// rule about the principal and not about the org.
	require.True(t, Grantable(TypeOrg, RelViewer, TypeUser))
	require.True(t, Grantable(TypeOrg, RelOperator, TypeUser))
	require.True(t, Grantable(TypeOrg, RelAdmin, TypeUser))
}

// TestWhatEachOrgRoleConfers pins what a role hands over, because a caller that
// writes one to answer a request for a single action hands the rest of the row
// over with it.
func TestWhatEachOrgRoleConfers(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		[]Action{ActionObserve, ActionViewResource, ActionReadStorage},
		OrgRoleConfers(RelViewer))

	require.Equal(t,
		[]Action{
			ActionObserve, ActionOperate,
			ActionViewResource, ActionOperateResource, ActionReadStorage, ActionWriteStorage,
		},
		OrgRoleConfers(RelOperator))

	require.Equal(t,
		[]Action{
			ActionObserve, ActionOperate, ActionConfigure, ActionAdminister, ActionViewAudit,
			ActionViewResource, ActionOperateResource, ActionConfigureResource,
			ActionReadStorage, ActionWriteStorage, ActionAdmitDevice, ActionDevelopDevice,
		},
		OrgRoleConfers(RelAdmin))

	require.Nil(t, OrgRoleConfers(RelManager), "manager is not a role on the org")
	require.Nil(t, OrgRoleConfers(RelOrg), "the org edge is structure and not a role")
}

func TestGrantableRefusesARelationTheObjectDoesNotCarry(t *testing.T) {
	t.Parallel()

	require.False(t, Grantable(TypeOrg, RelOwner, TypeUser))
	require.False(t, Grantable(TypeOrg, Relation("keeper"), TypeUser))
	require.True(t, Grantable(TypeDevice, RelOperator, TypeSink))
}
