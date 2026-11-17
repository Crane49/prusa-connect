package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
)

// listingManifest is the two-halves module asking to list the people
// (ADR-20261021-x2b).
func listingManifest() string {
	return strings.Replace(bothHalvesManifest(), "permissions:\n", "list_people: true\npermissions:\n", 1)
}

// declaredLister installs the listing module and reads back what the overlay
// holds.
func declaredLister(t *testing.T, extra ...string) (resource.Declaration, string) {
	t.Helper()

	configPath, core := installConfig(t)
	release := aRelease(t, listingManifest(), true)

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

// An approved install writes list_people into the declaration and the ask it
// approved, and the command says so, because no grant line would.
func TestAnApprovedInstallGrantsListPeople(t *testing.T) {
	t.Parallel()

	declared, said := declaredLister(t, "-approve")

	require.True(t, declared.Spec.Service.ListPeople)
	require.NotNil(t, declared.Spec.Module.Asked)
	require.True(t, declared.Spec.Module.Asked.ListPeople)
	require.Contains(t, said, "may list    the people this server holds, with their names (list_people)")
}

// An install without approve writes nothing and lists the permission among the
// requests, so the operator reads it before the module can list anybody.
func TestAnInstallWithoutApproveOnlyListsListPeople(t *testing.T) {
	t.Parallel()

	declared, said := declaredLister(t)

	require.False(t, declared.Spec.Service.ListPeople)
	require.Contains(t, said, "asks for    permission list_people")
	require.NotContains(t, said, "may list")
}

// An install over HTTP says it gave list_people with approve, and lists it
// among the requests without.
func TestAnInstallOverHTTPSaysItGaveListPeople(t *testing.T) {
	t.Parallel()

	for _, approve := range []bool{true, false} {
		configPath, core := installConfig(t)

		host, closeHost, err := openGraph(t.Context(), core, "", quiet())
		require.NoError(t, err)

		installer := newModuleInstaller(core, configPath, host.graph, nil, nil, host.graph, discard())
		release := aRelease(t, listingManifest(), true)

		held, err := installer.Install(t.Context(), "user:"+core.Principal, httpapi.ModuleRelease{
			Module: theModulePath, Version: "1.0.0", From: release, Approve: approve,
		})
		closeHost()
		require.NoError(t, err)

		if approve {
			require.ElementsMatch(t, []string{"permission observe", "permission list_people"}, held.Approved)

			continue
		}

		require.Empty(t, held.Approved)
		require.Contains(t, held.Requests, "permission list_people")
	}
}

// A release asking to list the people when the operator was not shown that
// is refused, and an update that adds it says so.
func TestListPeopleIsHeldToWhatTheOperatorWasShown(t *testing.T) {
	t.Parallel()

	asked := install.ModuleAsks{ListPeople: true}

	require.Equal(t, []string{"permission list_people"}, beyondAsks(asked, install.ModuleAsks{}, theNamespace))
	require.Empty(t, beyondAsks(asked, install.ModuleAsks{ListPeople: true}, theNamespace))

	requests := resource.RequestsOf(asked)
	require.True(t, requests.ListPeople)
	require.True(t, resource.AsksOf(requests).ListPeople)
}

// The listing names each account by its display name, or by its username when
// it has none, and carries nothing else of the account.
func TestTheAccountsListThePeopleByName(t *testing.T) {
	t.Parallel()

	accounts := local.New(local.NewMemoryStore(), nil)

	_, err := accounts.CreateFirst(t.Context(), "operator", local.NewAccount{
		Username: "owner@example.com", DisplayName: "Olga Owner", Password: []byte("correct horse battery"),
	})
	require.NoError(t, err)

	bob, err := accounts.Create(t.Context(), local.NewAccount{
		Username: "bob@example.com", Password: []byte("correct horse battery"),
	})
	require.NoError(t, err)

	people := &modulePeople{}
	listed, err := people.List(t.Context())
	require.NoError(t, err)
	require.Empty(t, listed, "a list nobody bound lists somebody")

	people.bind(&accountPeople{accounts: accounts})

	listed, err = people.List(t.Context())
	require.NoError(t, err)
	require.ElementsMatch(t, []accessapi.Person{
		{Subject: "user:operator", Name: "Olga Owner", Kind: accessapi.KindUser},
		{Subject: "user:" + bob.ID, Name: "bob@example.com", Kind: accessapi.KindUser},
	}, listed)

	people.bind(&accountPeople{accounts: accounts, mode: newSetupMode(resource.SetupOpen)})

	listed, err = people.List(t.Context())
	require.NoError(t, err)
	require.Empty(t, listed, "a server in open mode lists its accounts")
}
