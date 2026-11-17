package resourcehost

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// declaredAs is one instance as the host holds it.
func declaredAs(t *testing.T, kind ref.Kind, name, typeName string, runtime install.Runtime) resource.Declaration {
	t.Helper()

	instance, err := ref.New(kind, name)
	require.NoError(t, err)

	return resource.Declaration{
		Ref:  instance,
		Spec: resource.Spec{Type: typeName, Runtime: runtime},
	}
}

// supervisingDeclarations is a host running these instances.
func supervisingDeclarations(declarations ...resource.Declaration) *Host {
	host := &Host{running: map[ref.Ref]*instance{}}

	for _, declared := range declarations {
		host.running[declared.Ref] = &instance{declaration: declared}
	}

	return host
}

// A sink and a module of another kind under one name read the authorization
// graph as one principal, so the second of them is refused rather than started
// beside the first.
func TestOneInstanceNameIsHeldAcrossTheKindsThatReadTheGraph(t *testing.T) {
	t.Parallel()

	sink := declaredAs(t, ref.KindSink, "audit", "core.sink.webhook", install.RuntimeBuiltin)
	link := declaredAs(t, ref.KindLink, "audit", "acme.example/fractal", install.RuntimeService)

	t.Run("a module after a sink", func(t *testing.T) {
		t.Parallel()

		host := supervisingDeclarations(sink)

		err := host.claimNamespace(link)
		require.ErrorIs(t, err, ErrNameClaimed)
		require.Contains(t, err.Error(), "sink:audit")
		require.Contains(t, err.Error(), "link:audit")
	})

	t.Run("a sink after a module", func(t *testing.T) {
		t.Parallel()

		host := supervisingDeclarations(link)

		require.ErrorIs(t, host.claimNamespace(sink), ErrNameClaimed)
	})

	t.Run("two modules of different kinds", func(t *testing.T) {
		t.Parallel()

		driver := declaredAs(t, ref.KindDriver, "audit", "acme.example/driver", install.RuntimeService)
		host := supervisingDeclarations(link)

		require.ErrorIs(t, host.claimNamespace(driver), ErrNameClaimed)
	})
}

// A name the host is not already running under is taken, and so is the
// instance's own name at a restart, which is the same instance and not a
// second one.
func TestAnInstanceNameNothingElseHoldsIsTaken(t *testing.T) {
	t.Parallel()

	link := declaredAs(t, ref.KindLink, "audit", "acme.example/fractal", install.RuntimeService)

	t.Run("nothing is running", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, supervisingDeclarations().claimNamespace(link))
	})

	t.Run("the same instance again", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, supervisingDeclarations(link).claimNamespace(link))
	})

	t.Run("another name of the same kind", func(t *testing.T) {
		t.Parallel()

		other := declaredAs(t, ref.KindLink, "spare", "other.example/fractal", install.RuntimeService)
		require.NoError(t, supervisingDeclarations(other).claimNamespace(link))
	})
}

// A kind that reads the graph as nothing does not hold a name. A camera is
// configuration this server acts on, not a principal grants are written for,
// so a camera named audit is no reason to refuse a sink of that name.
func TestAKindThatHoldsNoPrincipalHoldsNoName(t *testing.T) {
	t.Parallel()

	camera := declaredAs(t, ref.KindCamera, "audit", "core.camera.rtsp", install.RuntimeBuiltin)
	sink := declaredAs(t, ref.KindSink, "audit", "core.sink.webhook", install.RuntimeBuiltin)

	require.NoError(t, supervisingDeclarations(camera).claimNamespace(sink))
	require.NoError(t, supervisingDeclarations(sink).claimNamespace(camera))
}

// The host lets go of what a link reported when that link starts again.
// servesTo is the method set one start builds, so this is the wiring and not
// the records: an instance that is starting has told this host nothing, and
// the one line that says so is easy to drop.
func TestAStartClearsWhatTheLinkReportedBefore(t *testing.T) {
	t.Parallel()

	link := declaredAs(t, ref.KindLink, "fractal", "acme.example/fractal", install.RuntimeService)
	host := supervisingDeclarations(link)

	require.Empty(t, host.flights.nodes.report(link.Ref, []string{"hq"}, host.supervised()))
	require.True(t, host.flights.nodes.holds(link.Ref, "hq"))

	_, err := host.servesTo(link)
	require.NoError(t, err)

	require.False(t, host.flights.nodes.holds(link.Ref, "hq"),
		"a link that has just started still answers for a server it reported before")
}

// An instance the host has stopped keeps no records, so what a deployment ever
// ran does not pile up for the life of the process.
func TestTheRecordsOfAStoppedLinkAreDropped(t *testing.T) {
	t.Parallel()

	gone := declaredAs(t, ref.KindLink, "fractal", "acme.example/fractal", install.RuntimeService)
	live := declaredAs(t, ref.KindLink, "spare", "other.example/fractal", install.RuntimeService)

	host := supervisingDeclarations(gone, live)
	require.Empty(t, host.flights.nodes.report(gone.Ref, []string{"hq"}, host.supervised()))

	// The first link is uninstalled, and the second one reports.
	delete(host.running, gone.Ref)
	require.Empty(t, host.flights.nodes.report(live.Ref, []string{"depot"}, host.supervised()))

	require.Empty(t, host.flights.nodes.held[gone.Ref])
	require.NotContains(t, host.flights.nodes.by, "hq")
}
