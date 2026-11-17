package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// A plan names the enabled modules a release would conflict with over a
// file type, before the install is refused for it (ADR-20261027-wvj).
func TestAPlanNamesTheModulesAReleaseConflictsWith(t *testing.T) {
	t.Parallel()

	instance, err := ref.Parse("sink:reader")
	require.NoError(t, err)

	installed := []resource.Declaration{{
		Ref:    instance,
		Source: resource.SourceOverlay,
		Spec: resource.Spec{
			Type: "com.github.aux.acme.reader/reader", Enabled: true, Runtime: install.RuntimeService,
			Module: &install.ModuleEntry{Version: "1.0.0", FileTypes: []string{".3mf"}},
		},
	}}

	release := func(namespace string, fileTypes []string) httpapi.ModuleOffer {
		return httpapi.ModuleOffer{Namespace: namespace, FileTypes: fileTypes}
	}

	conflicts := planConflicts([]httpapi.ModuleOffer{
		release("com.github.aux.acme.other", []string{".3mf", ".stl"}),
	}, installed)
	require.Equal(t, []httpapi.ModuleFileTypeConflict{{
		Module: "com.github.aux.acme.other", With: "com.github.aux.acme.reader",
		Use: resource.UseFileAnalysis, Types: []string{".3mf"},
	}}, conflicts)

	// An update of the module that reads the type is not a conflict with
	// its older release.
	require.Empty(t, planConflicts([]httpapi.ModuleOffer{
		release("com.github.aux.acme.reader", []string{".3mf"}),
	}, installed))

	// A release of another type is not either.
	require.Empty(t, planConflicts([]httpapi.ModuleOffer{
		release("com.github.aux.acme.other", []string{".stl"}),
	}, installed))
}
