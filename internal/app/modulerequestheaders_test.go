package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// headerManifest is the two-halves module asking for one request header
// (ADR-20261118-nnm).
func headerManifest() string {
	return strings.Replace(bothHalvesManifest(), "permissions:\n", strings.Join([]string{
		"routes:",
		"  headers:",
		"    - name: print-after-upload",
		"      reason: the slicer says whether to print the file",
		"permissions:",
		"",
	}, "\n"), 1)
}

// declaredHeaders installs the module asking for a header and reads back what
// the overlay holds.
func declaredHeaders(t *testing.T, extra ...string) (resource.Declaration, string) {
	t.Helper()

	configPath, core := installConfig(t)
	release := aRelease(t, headerManifest(), true)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, extra...)

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Failures)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found)

	return declared, said
}

// An approved install writes each header into the service block, in its
// canonical form, and the ask keeps its reason. The command names each one.
func TestAnApprovedInstallGrantsItsRequestHeaders(t *testing.T) {
	t.Parallel()

	declared, said := declaredHeaders(t, "-approve")

	require.Equal(t, []string{"Print-After-Upload"}, declared.Spec.Service.RequestHeaders)
	require.Equal(t,
		[]install.RequestHeader{{Name: "Print-After-Upload", Reason: "the slicer says whether to print the file"}},
		declared.Spec.Module.Asked.RequestHeaders)
	require.Contains(t, said,
		"header      Print-After-Upload, passed to the module (the slicer says whether to print the file)")
}

// An install without approve passes no header, and lists each one among the
// requests with its reason.
func TestAnInstallWithoutApproveOnlyListsItsRequestHeaders(t *testing.T) {
	t.Parallel()

	declared, said := declaredHeaders(t)

	require.Empty(t, declared.Spec.Service.RequestHeaders)
	require.Contains(t, said, "asks for    header Print-After-Upload (the slicer says whether to print the file)")
}

// A release asking for a header the operator was not shown is refused, a
// changed reason is not a new request, and the request survives the API's
// spelling.
func TestARequestHeaderIsHeldToWhatTheOperatorWasShown(t *testing.T) {
	t.Parallel()

	asked := install.ModuleAsks{RequestHeaders: []install.RequestHeader{{Name: "Overwrite", Reason: "replaces"}}}
	reworded := install.ModuleAsks{RequestHeaders: []install.RequestHeader{{Name: "Overwrite", Reason: "other"}}}

	require.Equal(t, []string{"header Overwrite (replaces)"}, beyondAsks(asked, install.ModuleAsks{}, theNamespace))
	require.Empty(t, beyondAsks(asked, asked, theNamespace))
	require.Empty(t, beyondAsks(asked, reworded, theNamespace))
	require.Equal(t, asked, resource.AsksOf(resource.RequestsOf(asked)))
}

// An approved update keeps a header the operator took away out, and lists one
// the release before did not ask for as new. The function is the same for any
// name, so the names stand for three headers on the list, which holds one
// today.
func TestAnUpdateKeepsARequestHeaderTheOperatorTookAwayOut(t *testing.T) {
	t.Parallel()

	kept := install.RequestHeader{Name: "Print-After-Upload", Reason: "prints"}
	removed := install.RequestHeader{Name: "Overwrite", Reason: "replaces"}
	fresh := install.RequestHeader{Name: "Depth", Reason: "lists a folder"}

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared := resource.Declaration{Ref: instance, Spec: resource.Spec{
		Service: &install.Service{RequestHeaders: []string{kept.Name}},
		Module: &install.ModuleEntry{Asked: &install.ModuleAsks{
			RequestHeaders: []install.RequestHeader{kept, removed},
		}},
	}}

	given, added, err := decidedAsks(moduleRequest{approve: true},
		install.ModuleAsks{RequestHeaders: []install.RequestHeader{kept, removed, fresh}}, declared, theNamespace, nil)
	require.NoError(t, err)
	require.Equal(t, []install.RequestHeader{kept, fresh}, given.RequestHeaders)
	require.Equal(t, []string{headerRequest(fresh)}, added)
}

// requiredHeaderManifest is the two-halves module asking for one request
// header it cannot work without.
func requiredHeaderManifest() string {
	return strings.Replace(headerManifest(), "      reason: the slicer says whether to print the file\n",
		"      reason: the slicer says whether to print the file\n      required: true\n", 1)
}

// A header the module marks required is recorded as a requirement of the
// instance, with or without approve, so the host holds every start to the
// grant. The request line says it is required.
func TestARequiredHeaderIsRecordedAsARequirement(t *testing.T) {
	t.Parallel()

	for _, extra := range [][]string{{"-approve"}, nil} {
		configPath, core := installConfig(t)
		said := addModuleFrom(t, configPath, theModulePath+"@1.0.0",
			aRelease(t, requiredHeaderManifest(), true), extra...)

		doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
		require.NoError(t, err)

		instance, err := ref.New(ref.KindSink, "spools")
		require.NoError(t, err)

		declared, found := doc.Find(instance)
		require.True(t, found)
		require.Contains(t, declared.Spec.Module.Requires, install.Requirement{Header: "Print-After-Upload"})

		if extra == nil {
			require.Contains(t, said,
				"header Print-After-Upload, required (the slicer says whether to print the file)")
		}
	}
}

// An approved update of a module that marks a header required is refused
// when the operator took that header away, naming it, instead of installing
// a release that cannot start.
func TestAnUpdateThatWouldDropARequiredHeaderIsRefused(t *testing.T) {
	t.Parallel()

	required := install.RequestHeader{Name: "Print-After-Upload", Reason: "prints", Required: true}

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared := resource.Declaration{Ref: instance, Spec: resource.Spec{
		Service: &install.Service{},
		Module:  &install.ModuleEntry{Asked: &install.ModuleAsks{RequestHeaders: []install.RequestHeader{required}}},
	}}

	_, _, err = decidedAsks(moduleRequest{approve: true},
		install.ModuleAsks{RequestHeaders: []install.RequestHeader{required}}, declared, theNamespace, nil)
	require.ErrorIs(t, err, ErrUsage)
	require.ErrorContains(t, err, "requires the request header Print-After-Upload")
}
