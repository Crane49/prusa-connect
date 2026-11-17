package resource_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Two enabled modules never claim one file type for one use
// (ADR-20261027-wvj rules 3 and 19).

// claiming is a module that reads and draws the file types given.
func claiming(t *testing.T, name string, enabled bool, fileTypes, viewTypes []string) resource.Declaration {
	t.Helper()

	declared := module(t, name, "1.0.0", enabled)
	declared.Spec.Module.FileTypes = fileTypes
	declared.Spec.Module.ViewTypes = viewTypes

	return declared
}

func TestSwitchingOnAModuleThatReadsATypeAnotherReadsIsRefused(t *testing.T) {
	t.Parallel()

	a := claiming(t, "a", true, []string{".3mf", ".stl"}, nil)
	off := claiming(t, "b", false, []string{".3mf"}, nil)
	on := claiming(t, "b", true, []string{".3mf"}, nil)

	before := []resource.Declaration{a, off}

	// Installed switched off, it claims nothing yet.
	require.NoError(t, resource.CheckChange([]resource.Declaration{a}, before, off.Ref))

	err := resource.CheckChange(before, resource.Replaced(before, on), on.Ref)
	require.True(t, fault.HasCode(err, httpfault.CodeModuleFileTypeConflict()),
		"want MODULE_FILE_TYPE_CONFLICT, got %v", err)
	require.ErrorContains(t, err, "com.github.aux.acme.a and com.github.aux.acme.b both read .3mf files")
}

func TestTwoModulesOfOtherTypesOrOtherUsesDoNotConflict(t *testing.T) {
	t.Parallel()

	a := claiming(t, "a", true, []string{".3mf"}, []string{".3mf"})
	b := claiming(t, "b", true, []string{".stl"}, []string{".stl"})
	c := claiming(t, "c", true, nil, nil)

	require.Empty(t, resource.FileTypeConflicts([]resource.Declaration{a, b, c}))

	// One reads .3mf, the other draws it: two uses, no conflict.
	reader := claiming(t, "d", true, []string{".gcode"}, nil)
	viewer := claiming(t, "e", true, nil, []string{".gcode"})
	require.Empty(t, resource.FileTypeConflicts([]resource.Declaration{reader, viewer}))
}

func TestAViewForEveryFileConflictsWithAnyView(t *testing.T) {
	t.Parallel()

	every := claiming(t, "a", true, nil, []string{resource.EveryFileType})
	one := claiming(t, "b", true, nil, []string{".3mf", ".stl"})

	conflicts := resource.FileTypeConflicts([]resource.Declaration{every, one})
	require.Len(t, conflicts, 1)
	require.Equal(t, resource.UseFileView, conflicts[0].First.Use)
	require.Equal(t, []string{".3mf", ".stl"}, conflicts[0].Types)
}

func TestAConflictThatWasThereDoesNotRefuseAnotherChange(t *testing.T) {
	t.Parallel()

	a := claiming(t, "a", true, []string{".3mf"}, nil)
	b := claiming(t, "b", true, []string{".3mf"}, nil)
	c := module(t, "c", "1.0.0", false)
	cOn := module(t, "c", "1.0.0", true)

	before := []resource.Declaration{a, b, c}
	require.NoError(t, resource.CheckChange(before, resource.Replaced(before, cOn), cOn.Ref))
}
