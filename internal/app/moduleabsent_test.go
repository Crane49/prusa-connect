package app

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
)

// askingManifest is the two-halves module asking to put questions about
// people it is not serving (ADR-20260929-f4n rule 1).
func askingManifest() string {
	return strings.Replace(bothHalvesManifest(), "permissions:\n", "check_absent_people: true\npermissions:\n", 1)
}

// declaredSpools installs the module and reads back what the overlay holds.
func declaredSpools(t *testing.T, extra ...string) (resource.Declaration, string) {
	t.Helper()

	configPath, core := installConfig(t)
	release := aRelease(t, askingManifest(), true)

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

// An approved install writes the permission into the declaration, and the
// command says so, because no grant line would (ADR-20260929-f4n rules 3 and 5).
func TestAnApprovedInstallGrantsCheckAbsentPeople(t *testing.T) {
	t.Parallel()

	declared, said := declaredSpools(t, "-approve")

	require.True(t, declared.Spec.Service.CheckAbsentPeople)
	require.Contains(t, said, "may ask     about people it is not serving (check_absent_people)")
}

// An install without approve writes nothing and lists the permission among the
// requests, so the operator reads it before the module can ask (ADR-20260929-f4n rule 4).
func TestAnInstallWithoutApproveOnlyListsCheckAbsentPeople(t *testing.T) {
	t.Parallel()

	declared, said := declaredSpools(t)

	require.False(t, declared.Spec.Service.CheckAbsentPeople)
	require.Contains(t, said, "asks for    permission check_absent_people")
	require.NotContains(t, said, "may ask")
}

// The account list is what says whether a person is still here. A deleted
// person answers false even though the graph may keep their grants, and a
// principal that is not one person answers false too (ADR-20260929-f4n rules 9 and 11).
func TestTheAccountsSayWhoIsAPerson(t *testing.T) {
	t.Parallel()

	store := local.NewMemoryStore()
	accounts := local.New(store, nil)

	_, err := accounts.CreateFirst(t.Context(), "operator", local.NewAccount{
		Username: "owner@example.com", Password: []byte("correct horse battery"),
	})
	require.NoError(t, err)

	bob, err := accounts.Create(t.Context(), local.NewAccount{
		Username: "bob@example.com", Password: []byte("correct horse battery"),
	})
	require.NoError(t, err)

	people := accountPeople{accounts: accounts}

	for principal, want := range map[string]bool{
		"user:operator":          true,
		"user:" + bob.ID:         true,
		"user:nobody":            false,
		"service_account:backup": false,
		"group:staff#member":     false,
		"not a principal at all": false,
	} {
		held, holdsErr := people.holds(t.Context(), principal)
		require.NoError(t, holdsErr, principal)
		require.Equal(t, want, held, principal)
	}

	require.NoError(t, accounts.Delete(t.Context(), bob.ID))

	held, err := people.holds(t.Context(), "user:"+bob.ID)
	require.NoError(t, err)
	require.False(t, held, "a deleted person is still held")
}

// In open mode every request is served as one principal, so the accounts no
// longer say who is a person. A list nobody bound keeps nobody either
// (ADR-20260929-f4n rule 10).
func TestNoListOfPeopleIsKept(t *testing.T) {
	t.Parallel()

	accounts := local.New(local.NewMemoryStore(), nil)

	open := &accountPeople{accounts: accounts, mode: newSetupMode(resource.SetupOpen)}
	require.False(t, open.kept())

	required := &accountPeople{accounts: accounts, mode: newSetupMode(resource.SetupRequired)}
	require.True(t, required.kept())

	unbound := &modulePeople{}
	require.False(t, unbound.Kept())

	unbound.bind(required)
	require.True(t, unbound.Kept())
}

// The composition as serve builds it: a supervision plane, the graph, and the
// identity layer of a server with local accounts. A module with the permission
// asks about a real account holding a grant and hears yes, and about the same
// account once deleted and hears no. It fails when the plane never learns the
// accounts (ADR-20260929-f4n rules 6, 10 and 11).
func TestTheComposedServerAnswersAboutARealAccount(t *testing.T) {
	t.Parallel()

	host := testHost(t)

	metrics, err := newObserved()
	require.NoError(t, err)

	hub, err := stream.New(stream.Options{Site: "local", Authorizer: visibility{graph: host.graph}, Logger: discard()})
	require.NoError(t, err)
	t.Cleanup(hub.Close)

	supervision, err := startPlane(
		t.Context(),
		discard(),
		hub,
		metrics,
		t.TempDir(),
		false,
		false,
		install.SwitchOff{},
		nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = supervision.shutdown(context.WithoutCancel(t.Context()), discard(), time.Second) })

	supervision.accessors.bind(host.graph)

	who, err := identityFor(t.Context(), config.Default(), host, supervision,
		newSetupMode(resource.SetupRequired), discard())
	require.NoError(t, err)

	_, err = who.accounts.Setup(t.Context(), httpapi.NewAccount{
		Username: "owner@example.com", Password: "correct horse battery",
	})
	require.NoError(t, err)

	bob, err := who.accounts.Create(t.Context(), httpapi.NewAccount{
		Username: "bob@example.com", Password: "correct horse battery",
	})
	require.NoError(t, err)

	require.NoError(t, host.graph.Write(t.Context(),
		tuple(t, authz.TypeOrg, "local", authz.RelViewer, "user:"+bob.ID)))

	instance, err := ref.New(ref.KindSink, "queue")
	require.NoError(t, err)

	access := supervision.host.AccessFor(resourcehost.Module{
		Instance:          instance,
		Permissions:       []authz.Action{authz.ActionReadStorage},
		CheckAbsentPeople: true,
	})

	question := accessapi.CheckParams{
		Subject: "user:" + bob.ID, Action: "read_storage", ObjectType: "resource_kind", ObjectID: "storage",
	}

	decision, err := access.Check(t.Context(), question)
	require.NoError(t, err)
	require.True(t, decision.Allowed, "a real account with the grant was answered no")

	require.NoError(t, who.accounts.Delete(t.Context(), bob.ID))

	decision, err = access.Check(t.Context(), question)
	require.NoError(t, err)
	require.False(t, decision.Allowed, "a deleted account whose grant stayed was answered yes")
}

// serve builds the identity layer through identityFor, and nothing else in the
// package calls buildIdentity, so the plane always learns the accounts. The
// composed test above covers identityFor. This gate covers the path serve
// takes to it (ADR-20260929-f4n rule 11).
func TestServeBuildsItsIdentityThroughIdentityFor(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	calls := map[string][]string{}
	read := 0

	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		parsed, parseErr := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		require.NoError(t, parseErr)

		read++

		for _, decl := range parsed.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Body == nil {
				continue
			}

			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if call, isCall := node.(*ast.CallExpr); isCall {
					if ident, isIdent := call.Fun.(*ast.Ident); isIdent {
						calls[ident.Name] = append(calls[ident.Name], fn.Name.Name)
					}
				}

				return true
			})
		}
	}

	require.Greater(t, read, 50, "the gate read too few files to prove anything")
	require.Equal(t, []string{"identityFor"}, calls["buildIdentity"],
		"buildIdentity is called outside identityFor, so the plane can miss the accounts")
	require.Equal(t, []string{"serveOver"}, calls["identityFor"],
		"serveOver no longer builds its identity through identityFor")
}
