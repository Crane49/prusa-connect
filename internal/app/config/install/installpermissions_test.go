package install_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/authz"
)

// moduleWithPermissions renders a service block whose instance may take the
// named actions as itself.
func moduleWithPermissions(permissions ...string) []byte {
	block := strings.Builder{}

	block.Write(moduleService("com.github.aux.acme.planner/spool-tracker"))

	if len(permissions) > 0 {
		block.WriteString(lines("        permissions:"))

		for _, permission := range permissions {
			block.WriteString(lines("          - " + strconv.Quote(permission)))
		}
	}

	return []byte(block.String())
}

// The ceiling on a module's own work is what the operator wrote, as actions
// the authorization graph holds, in the order they were written.
func TestTheActionsAModuleMayTakeAsItselfAreTheOperators(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", moduleWithPermissions("observe", "operate", "admit_device"))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)

	require.Equal(t, []authz.Action{authz.ActionObserve, authz.ActionOperate, authz.ActionAdmitDevice},
		doc.Instances[0].Service.Permissions)
}

// An instance that declares nothing may do nothing as itself. That is the
// default, and the host holds every call the module makes as itself to it.
func TestAnInstanceThatDeclaresNoPermissionsMayDoNothingAsItself(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", moduleWithPermissions())
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)
	require.Empty(t, doc.Instances[0].Service.Permissions)
}

// A name the authorization graph does not hold, and a name written twice, are
// refused at load rather than at the first call the module makes as itself.
func TestAPermissionTheGraphDoesNotHoldIsRefusedAtLoad(t *testing.T) {
	t.Parallel()

	for name, permissions := range map[string][]string{
		"unknown":   {"observe", "read_everything"},
		"twice":     {"observe", "observe"},
		"blank":     {""},
		"a level":   {"viewer"},
		"a spacing": {"observe telemetry"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", moduleWithPermissions(permissions...))
			require.NoError(t, err)
			require.Empty(t, doc.Instances, "the instance loaded over a permission the graph cannot answer")
			require.Len(t, doc.Failures, 1)
			require.Equal(t, "resources.sink.planner", doc.Failures[0].Key)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
			require.Contains(t, doc.Failures[0].Err.Error(), "service.permissions")
		})
	}
}

// A module that reads files for its own background work, and one that reads
// the audit record, name the actions that unlock those in the same list. Both
// are actions the authorization graph holds, so both load, and the ceiling
// then carries them into every call the module makes as itself.
func TestAModuleMayDeclareReadStorageAndViewAudit(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml",
		moduleWithPermissions("observe", "read_storage", "view_audit"))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)

	require.Equal(t,
		[]authz.Action{authz.ActionObserve, authz.ActionReadStorage, authz.ActionViewAudit},
		doc.Instances[0].Service.Permissions)
}
