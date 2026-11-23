package resourcehost

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// The host does not start an instance whose service block lacks a header
// the module marked required. The start check refuses it with the fault
// that names the header and tells the operator to grant it, and a grant of
// the header lets the same check pass (ADR-20261118-nnm rule 2).
func TestAnInstanceWithoutARequiredHeaderDoesNotStart(t *testing.T) {
	t.Parallel()

	declared := declaredAs(t, ref.KindSink, "slicer", "com.github.acme.slicer/slicer", install.RuntimeService)
	declared.Spec.Module = &install.ModuleEntry{Requires: []install.Requirement{{Header: "Print-After-Upload"}}}

	for name, granted := range map[string][]string{"no grant": nil, "another header": {"Overwrite"}} {
		declared.Spec.Service = &install.Service{RequestHeaders: granted}
		host := supervisingDeclarations(declared)

		serves, err := host.servesTo(declared)
		require.NoError(t, err, name)

		err = host.dependenciesMet(t.Context(), declared, serviceOf(declared), serves)
		require.Error(t, err, name)
		require.ErrorIs(t, err, headerNotGranted.New(""), name)
		require.ErrorContains(t, err, "requires the request header Print-After-Upload", name)

		var dependency dependencyError
		require.ErrorAs(t, err, &dependency, name)
	}

	declared.Spec.Service = &install.Service{RequestHeaders: []string{"Print-After-Upload"}}
	host := supervisingDeclarations(declared)

	serves, err := host.servesTo(declared)
	require.NoError(t, err)
	require.NoError(t, host.dependenciesMet(t.Context(), declared, serviceOf(declared), serves))
}
