package resourcehost_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// A module is declared here the way ADR-20260827-wmv rule 5 says one is: a type outside
// the core namespace, reached as a supervised child process. install.runtimeOf
// refuses the same names as builtin, so this is the only shape a module has, and
// these cases drive it through the real store, the real supervision plane and a
// real second process.
//
// The artefact is named after the type with the namespace taken off, which is
// what resourcehost.artefactName does and what an author's release workflow
// produces, so the two children service_test.go already builds serve here
// unchanged.
const (
	// moduleType is a module that answers the handshake and reports ready.
	moduleType = "acme/" + pluginType
	// stalledModuleType is a module that launches and never reports ready. It
	// stands in for ADR-20260827-rwq rule 5's migration that does not finish: the
	// process is up, its own store is not migrated, and it has said nothing.
	stalledModuleType = "acme/" + silentType
	// mountedNamespace is a namespace the grammar accepts, so a module declared
	// under it is mounted at /api/v1/modules/<namespace>/. The one-label
	// namespace above is a service instance and not a mount.
	mountedNamespace = "com.github.aux.acme"
	// mountedModuleType is the example module under a mountable namespace.
	mountedModuleType = mountedNamespace + "/" + pluginType
)

// TestAModuleIsDeclaredOutsideTheCoreAndStarts is the arming half of the two
// cases below, and the first thing a module author needs to be true. A type this
// binary does not contain, under a namespace the core does not hold, is declared
// in the operator's file and is a running supervised process.
//
// Without it, a build that refused every namespaced type would pass every
// assertion about a module that is not being routed to.
func TestAModuleIsDeclaredOutsideTheCoreAndStarts(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	module := install(t)
	instance := theServiceSink(t)

	w.declareType(moduleType, module.dir, module.checksum, 1)
	w.waitReady(instance)

	w.waitLog(func(logged string) bool { return strings.Contains(logged, "is serving on stdio as pid") },
		"the module's own process never announced itself")

	status, ok := w.host.Status(instance)
	require.True(t, ok)
	require.True(t, status.Serving, conditionsOf(&status))

	// Routed to, which is the state the two cases below are the negative of.
	require.NoError(t, routeInto(t, w, instance))
}

// TestNothingIsRoutedToAModuleThatHasNotReportedReady is ADR-20260827-rwq rule 5's
// second sentence, on the real plane.
//
// The rule's first sentence puts a module's migration inside the module's own
// process, before it reports ready. That only holds the host's data apart if
// the host also declines to use a module that is still migrating: a half
// migrated store answering a call is the outcome rules 1 to 5 exist to avoid,
// and it is the one Odoo produces by running the migration inside its own
// start-up instead.
//
// The child here launches and says nothing, which is what a migration that has
// not finished looks like from outside. The assertion is that the operation
// never ran, not that it returned an error: a host that routed the call and let
// the child fail it would produce the same error and would have handed a
// request to an unmigrated store.
func TestNothingIsRoutedToAModuleThatHasNotReportedReady(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	stalled := installSilent(t)
	instance := theServiceSink(t)

	w.declareType(stalledModuleType, stalled.dir, stalled.checksum, 1)

	// Before the start budget expires the instance is starting, and after it
	// expires it is retrying. Neither is serving, and a call is refused in both,
	// so the loop covers the whole window rather than one moment in it.
	deadline := time.Now().Add(startBudget)
	refusals := 0

	for time.Now().Before(deadline) {
		err := routeInto(t, w, instance)
		require.Error(t, err, "a call reached a module that has not reported ready")
		require.Contains(t, err.Error(), "RESOURCE_NOT_READY",
			"the refusal does not say the instance is not serving")

		refusals++

		time.Sleep(20 * time.Millisecond)
	}

	require.Positive(t, refusals, "no call was made, so nothing was proved about routing")

	// The timeout the rule needs. A module that never reports ready does not
	// hold the supervision loop: the start budget ends the attempt, the instance
	// says so, and the ladder takes it from there.
	status := w.waitState(instance, condition.StateRetrying())
	require.True(t, says(status, "start budget"), conditionsOf(&status))
	require.False(t, status.Serving, conditionsOf(&status))

	// And it is still refused once it has latched into the ladder, which is the
	// state an operator will actually find it in.
	require.Error(t, routeInto(t, w, instance),
		"a call reached a module whose start had already timed out")
}

// TestAModuleThatHasNotReportedReadyIsNotOnTheKindsRoutingTable is the same rule
// where a caller actually looks. Nothing routes work by asking Status: a feed
// takes Supervising for its kind and calls into what it finds, which is what
// internal/app's sink feed does, so an instance that is listed there is an
// instance work reaches.
//
// The instance is listed, because a declared module that is starting is a
// declared module and must not vanish from the plane. What it must not do is
// accept the call, and that is the assertion.
func TestAModuleThatHasNotReportedReadyIsNotOnTheKindsRoutingTable(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	stalled := installSilent(t)
	instance := theServiceSink(t)

	w.declareType(stalledModuleType, stalled.dir, stalled.checksum, 1)

	// The child is launched and the handshake is outstanding, which is the
	// moment this case is about. Asserting before the start begins would pass
	// against a plane that routes to anything with a target, because there is no
	// target yet.
	w.waitState(instance, condition.StateStarting())

	supervising := w.host.Supervising(ref.KindSink)
	require.Contains(t, supervising, instance, "a declared module is missing from its kind's plane")

	var ran atomic.Bool

	err := supervisor.CallVoid(t.Context(), supervising[instance], "publish",
		func(context.Context, any) error {
			ran.Store(true)

			return nil
		})

	require.Error(t, err, "a caller reading the plane reached a module that is not serving")
	require.False(t, ran.Load(), "the operation ran against a module that has not reported ready")
}

// routeInto makes one call into an instance the way every caller of this plane
// does, and reports what the host answered. The closure records that it ran, so
// a refusal that happened after the call reached the module would be caught
// rather than read as the refusal this rule asks for.
func routeInto(t *testing.T, w *wired, instance ref.Ref) error {
	t.Helper()

	supervised, ok := w.host.Instance(instance)
	require.True(t, ok, "the host is supervising nothing under %s", instance)

	var ran atomic.Bool

	err := supervisor.CallVoid(t.Context(), supervised, "publish", func(context.Context, any) error {
		ran.Store(true)

		return nil
	})

	if err != nil {
		require.False(t, ran.Load(), "the call was refused after it had already reached the module")
	}

	return err
}

// TestAModuleDocumentIsRebasedAndPublishedByThePlane is ADR-20260909-c7x rule 9 on the
// real plane: the example module ships a fragment on plugin.info, the host
// rebases it under the namespace the type produced, the mount hands it over,
// and the listing's plane says the instance is documented.
func TestAModuleDocumentIsRebasedAndPublishedByThePlane(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	module := install(t)
	instance := theServiceSink(t)

	w.declareType(mountedModuleType, module.dir, module.checksum, 1)
	w.waitReady(instance)

	w.waitLog(func(logged string) bool { return strings.Contains(logged, "OpenAPI document is served") },
		"the host never said it was serving the module's document")

	surface, claimed := w.host.Module(mountedNamespace)
	require.True(t, claimed, "the namespace the type produced is not mounted")
	require.NotEmpty(t, surface.OpenAPI, "the mount carries no document for a module that shipped one")
	require.Contains(t, string(surface.OpenAPI), `"/api/v1/modules/`+mountedNamespace+`/spools"`,
		"the document was not rebased under the mount")
	require.NotContains(t, string(surface.OpenAPI), `"/spools"`,
		"a relative path survived the rebase")

	require.True(t, w.host.Documented(instance), "the plane does not report the instance as documented")

	unknown, err := ref.Parse("sink:nobody")
	require.NoError(t, err)
	require.False(t, w.host.Documented(unknown), "an instance the plane is not supervising reads as documented")
}
