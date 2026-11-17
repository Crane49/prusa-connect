package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
)

// demoOverAnInstall is what a demo script does on a fresh directory. It runs
// `module add` against the operator's file with the extra flags given, and
// then writes the file the demo starts its core on, with its own directory.
// It answers what the install printed, the path of the demo's file and the
// path of the operator's file.
func demoOverAnInstall(t *testing.T, extra ...string) (string, string, string) {
	t.Helper()

	root := t.TempDir()
	state := filepath.Join(root, "state")
	operatorPath := filepath.Join(root, "config.yaml")

	// The overlay is the demo's own, so the install declares the module where
	// the demo reads it. The file names no graph, which is how a script writes it.
	operator := strings.Join([]string{
		"core:",
		"  overlay: '" + filepath.ToSlash(filepath.Join(state, "resources.json")) + "'",
		"  modules: '" + filepath.ToSlash(filepath.Join(root, "modules")) + "'",
		"  marketplace:",
		"    enabled: false",
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(operatorPath, []byte(operator), 0o600))

	release := aRelease(t, bothHalvesManifest(), true)
	said := addModuleFrom(t, operatorPath, theModulePath+"@1.0.0", release, extra...)

	opts := DemoOptions{APIAddr: "127.0.0.1:8080"}.withDefaults()
	document, err := demoDocument([]byte(operator), demoConfig(opts, state, nil, true))
	require.NoError(t, err)

	path := filepath.Join(state, "config.yaml")
	require.NoError(t, os.MkdirAll(state, 0o700))
	require.NoError(t, os.WriteFile(path, document, 0o600))

	return said, path, operatorPath
}

// TestTheDemoGrantsWhatAnApprovedInstallReported holds the grant an approved
// install reported. The demo keeps its graph in its own directory, so the
// install wrote the grant into another database. The module has to hold it in
// the graph the demo reads, or it sees no printer.
func TestTheDemoGrantsWhatAnApprovedInstallReported(t *testing.T) {
	t.Parallel()

	said, path, _ := demoOverAnInstall(t, "-approve")
	require.Contains(t, said, "granted     "+theSinkGrant)

	demoCore, err := config.Load(path)
	require.NoError(t, err)

	require.NotContains(t, grantsOnTheOrg(t, demoCore), theSinkGrant,
		"the demo's graph already holds the grant, so this case no longer shows two graphs")

	var out bytes.Buffer
	require.NoError(t, demoModuleGrants(t.Context(), &out, path))
	require.Contains(t, out.String(), theSinkGrant)

	require.Contains(t, grantsOnTheOrg(t, demoCore), theSinkGrant,
		"the install reported a grant the demo's graph does not hold")

	// A second start writes nothing and says nothing.
	out.Reset()
	require.NoError(t, demoModuleGrants(t.Context(), &out, path))
	require.Empty(t, out.String())
}

// TestTheDemoGrantsNothingAnInstallDidNotApprove holds the other direction. A
// module installed without -approve recorded no permission, and the demo
// grants it nothing.
func TestTheDemoGrantsNothingAnInstallDidNotApprove(t *testing.T) {
	t.Parallel()

	said, path, _ := demoOverAnInstall(t)
	require.NotContains(t, said, "granted     ")

	var out bytes.Buffer
	require.NoError(t, demoModuleGrants(t.Context(), &out, path))
	require.Empty(t, out.String())

	demoCore, err := config.Load(path)
	require.NoError(t, err)
	require.NotContains(t, grantsOnTheOrg(t, demoCore), theSinkGrant)
}

// TestTheDemoTakesBackWhatARemovalWithdrew holds the grant a removal took back.
// `module remove` against the operator's file withdraws from the graph that
// file names, so the demo's graph keeps the grant until the demo reconciles it,
// and a later install under the same name would inherit it. A grant for a
// person is not a module's, and it stays.
func TestTheDemoTakesBackWhatARemovalWithdrew(t *testing.T) {
	t.Parallel()

	_, path, operatorPath := demoOverAnInstall(t, "-approve")

	demoCore, err := config.Load(path)
	require.NoError(t, err)

	require.NoError(t, demoModuleGrants(t.Context(), io.Discard, path))
	require.Contains(t, grantsOnTheOrg(t, demoCore), theSinkGrant)

	visitor := writeAVisitorGrant(t, demoCore)

	var removed bytes.Buffer
	require.NoError(t, Module(t.Context(), &removed,
		[]string{removeVerb, "-config", operatorPath, theModulePath}))
	require.Contains(t, grantsOnTheOrg(t, demoCore), theSinkGrant,
		"the removal took the grant from the demo's graph, so this case no longer shows two graphs")

	var out bytes.Buffer
	require.NoError(t, demoModuleGrants(t.Context(), &out, path))
	require.Contains(t, out.String(), "Withdrew "+theSinkGrant)

	held := grantsOnTheOrg(t, demoCore)
	require.NotContains(t, held, theSinkGrant, "the demo kept a grant no declaration records")
	require.Contains(t, held, visitor, "the demo took back a grant that is not a module's")
}

// writeAVisitorGrant writes the viewer role on the organization for a person
// into the deployment's graph, and answers it as grantsOnTheOrg spells it.
func writeAVisitorGrant(t *testing.T, core config.Core) string {
	t.Helper()

	authority, closeGraph, err := openModuleGrants(t.Context(), core)
	require.NoError(t, err)

	defer closeGraph()

	org, err := authz.NewObject(authz.TypeOrg, bootstrapOrg)
	require.NoError(t, err)

	person, err := authz.NewSubject(authz.TypeUser, "visitor")
	require.NoError(t, err)

	tuple, err := authz.NewTuple(org, authz.RelViewer, person)
	require.NoError(t, err)

	_, err = authority.graph.WriteGrant(t.Context(), authority.who, tuple)
	require.NoError(t, err)

	return tuple.String()
}
