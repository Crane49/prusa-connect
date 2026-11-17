package resourcehost_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/hostschema"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// testDoc is a complete descriptor, so a case is testing the registration rule
// rather than the documentation rule.
func testDoc() extension.Doc {
	return extension.Doc{
		Summary:     "A resource type used only by this package's tests.",
		Description: "It builds a value that does nothing and exists to exercise the registry.",
		Version:     "1.0.0",
		Contract:    registry.Contract,
		Stability:   extension.StabilityExperimental,
	}
}

// countedDoc is testDoc with the one setting the cases write, so a block they
// declare is a block the type reads.
func countedDoc() extension.Doc {
	doc := testDoc()
	doc.Options = []extension.Option{
		{Key: "url", Type: extension.OptionURL, Summary: "Where the cases write a value they can read back."},
	}

	return doc
}

// settle bounds an assertion that something must not happen, which is the one
// shape that has nowhere to wait: there is no event for a construction that was
// never made. A loaded machine only makes it more conservative.
const settle = 2 * time.Second

// wedged is the outer bound on an event-driven wait, and the only clock left
// in this package's waits.
//
// It is not a budget the work competes with. A wait ends on the event it is
// waiting for, so it costs what the plane takes to publish that event and no
// more: a machine under load publishes the same events later and the case
// still passes. Nothing here legitimately takes longer than one start budget,
// which is the window a child that never speaks holds before its launch is
// abandoned, so reaching this means the transition is not coming at all.
//
// It exists so a case waiting on something a broken host will never publish
// says what it was waiting for and what it last saw, instead of the package
// timing out with a stack dump naming every goroutine in the suite.
const wedged = 24 * startBudget

// broadcast is the wake-up half of an event-driven wait. A producer calls
// changed once it has published something; a waiter takes wait() before it
// looks, so an event raised between the look and the wait closes the channel
// the waiter is already holding rather than being missed.
type broadcast struct {
	mu sync.Mutex
	ch chan struct{}
}

func newBroadcast() *broadcast { return &broadcast{ch: make(chan struct{})} }

// wait returns the channel closed by the next event.
func (b *broadcast) wait() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.ch
}

// changed wakes every waiter. It never blocks, because the supervisor calls it
// from whichever goroutine moved an instance and a status hook that blocks
// stops the loop it is reporting on.
func (b *broadcast) changed() {
	b.mu.Lock()
	woken := b.ch
	b.ch = make(chan struct{})
	b.mu.Unlock()

	close(woken)
}

// owner is the principal these tests act as, spelled the way the composition
// root spells one: the graph keys on a type and an id, never on a bare id.
const owner = "user:operator"

// newGraph is a real authorization graph on a database of this test's own,
// bootstrapped so that owner holds manager on every resource kind. The resource
// store takes no other kind of graph, because a store deciding access with
// anything else is the shape an earlier design decision exists to make impossible.
func newGraph(t *testing.T) *authz.Store {
	t.Helper()

	db, err := authz.OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "authz.db"))
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	graph, err := authz.Open(db, authz.DialectSQLite)
	require.NoError(t, err)
	migrator, err := hostschema.Migrator(db, sqlstore.EngineSQLite, "0.0.0-test")
	require.NoError(t, err)
	require.NoError(t, migrator.Migrate(t.Context()))
	require.NoError(t, graph.Reconcile(t.Context()))

	subject, err := authz.ParseSubject(owner)
	require.NoError(t, err)

	_, err = graph.Bootstrap(t.Context(), "local", subject)
	require.NoError(t, err)

	return graph
}

// counted is a resource that records every construction and every close, which
// is how a test tells a reconcile that restarted an instance from one that did
// nothing.
type counted struct {
	mu         sync.Mutex
	builds     []registry.Spec
	closes     atomic.Int64
	failNext   atomic.Bool
	buildCount atomic.Int64

	// moved is the host's event source, so a case waiting for a second
	// construction is woken by the construction rather than by a ticker.
	moved *broadcast
}

func (c *counted) build(_ context.Context, _ host.Handle, spec registry.Spec) (*target, error) {
	defer c.moved.changed()

	if c.failNext.Load() {
		return nil, errRefused
	}

	c.mu.Lock()
	c.builds = append(c.builds, spec)
	c.mu.Unlock()

	c.buildCount.Add(1)

	return &target{owner: c}, nil
}

func (c *counted) specs() []registry.Spec {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]registry.Spec, len(c.builds))
	copy(out, c.builds)

	return out
}

var errRefused = errors.New("this implementation refuses to start")

type target struct{ owner *counted }

func (t *target) Close() error {
	t.owner.closes.Add(1)

	return nil
}

// wired is a store and a running host over a pair of files.
type wired struct {
	t     *testing.T
	paths resource.Paths
	store *resource.Store
	host  *resourcehost.Host
	graph *authz.Store
	built *counted
	alt   *counted
	logs  *recorder

	// moved is every event this host produces, on one channel: a status the
	// supervision plane published, a line it logged, a resource it constructed.
	// Every wait in this package blocks on it, so a case ends on the transition
	// it is waiting for instead of on a clock.
	moved *broadcast

	cancel  context.CancelFunc
	running *sync.WaitGroup
}

// stop ends the host and waits for every instance it supervises. The cleanup
// does the same, and both are safe: cancelling twice is a no-op and the second
// wait returns at once.
func (w *wired) stop() {
	w.cancel()
	w.running.Wait()
}

// recorder collects what the host logged, so a test can read back what a
// resource said. An out-of-process resource says it on its own standard error,
// which the host mirrors here and nowhere else.
type recorder struct {
	mu    sync.Mutex
	lines bytes.Buffer

	// moved is the host's event source. A child announces itself on its own
	// standard error, which arrives here and nowhere else, so this is the only
	// transition a case waiting for that banner can be woken by.
	moved *broadcast
}

func (r *recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	written, err := r.lines.Write(p)
	r.mu.Unlock()

	r.moved.changed()

	return written, err
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.lines.String()
}

func newWired(t *testing.T) *wired {
	t.Helper()

	return newWiredBeside(t, nil)
}

// newWiredBeside is newWired for a deployment that mounted the given patterns
// beside the API, which a module's root mount is held away from.
func newWiredBeside(t *testing.T, beside []string) *wired {
	t.Helper()

	return newWiredWith(t, beside, nil)
}

// newWiredWith is newWiredBeside for a case that needs one more thing on the
// host's configuration, such as the record store whose methods a module may
// require.
func newWiredWith(t *testing.T, beside []string, more func(*resourcehost.Config)) *wired {
	t.Helper()

	moved := newBroadcast()

	built := &counted{moved: moved}
	alt := &counted{moved: moved}

	sinkType, err := registry.Declare(ref.KindSink, "counted", countedDoc(), built.build)
	require.NoError(t, err)

	altType, err := registry.Declare(ref.KindSink, "other", testDoc(), alt.build)
	require.NoError(t, err)

	storageType, err := registry.Declare(ref.KindStorage, "counted", countedDoc(), built.build)
	require.NoError(t, err)

	reg, err := registry.NewFrom(sinkType, altType, storageType)
	require.NoError(t, err)

	logs := &recorder{moved: moved}

	cfg := resourcehost.Config{
		Registry: reg,
		Policy:   promptPolicy,
		Logger:   slog.New(slog.NewTextHandler(logs, nil)),
		// The plane already publishes every status change to whoever asked for
		// one; this is that subscription, and it is what every wait below is
		// woken by. It must not block, and it does not: it closes a channel.
		OnStatus: func(supervisor.Status) { moved.changed() },
		Beside:   func() []string { return beside },
		Reaper:   testReaper(),
	}

	if more != nil {
		more(&cfg)
	}

	supervision, err := resourcehost.New(cfg)
	require.NoError(t, err)

	dir := t.TempDir()
	paths := resource.Paths{
		File:    filepath.Join(dir, "config.yaml"),
		Overlay: filepath.Join(dir, "config.d", "resources.json"),
	}

	graph := newGraph(t)

	// The store reports a plane refusal on its own logger, and a case that
	// watches a claim be refused reads it off the same recorder the host's
	// lines land on.
	store, err := resource.New(resource.Config{
		Paths: paths, Plane: supervision, Graph: graph, Logger: slog.New(slog.NewTextHandler(logs, nil)),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())

	var running sync.WaitGroup

	// The error is checked after the wait rather than inside the goroutine: an
	// assertion off the test's own goroutine is not one the framework can act on.
	var runErr error

	running.Go(func() { runErr = supervision.Run(ctx) })

	t.Cleanup(func() {
		cancel()
		running.Wait()
		require.NoError(t, runErr)
	})

	return &wired{
		t: t, paths: paths, store: store, host: supervision, graph: graph, built: built, alt: alt, logs: logs,
		moved: moved, cancel: cancel, running: &running,
	}
}

// promptPolicy removes the start scatter and the stability window, which exist
// so a deployment does not dial every endpoint on one millisecond and are only
// latency in a test.
func promptPolicy(kind ref.Kind, _ resource.Declaration) supervisor.Policy {
	policy := supervisor.DefaultPolicy()
	policy.Backoff.Spread = 0
	policy.ReadyFor = time.Millisecond
	// Nothing here starts slowly, and one case starts something that never
	// finishes starting. Thirty seconds of waiting for it is a slow suite.
	policy.StartTimeout = startBudget

	if kind == ref.KindSink {
		policy.Queue = supervisor.QueuePolicy{Depth: 8, Drop: supervisor.DropOldest}
	}

	return policy
}

func (w *wired) writeFile(content string) {
	w.t.Helper()
	require.NoError(w.t, os.WriteFile(w.paths.File, []byte(content), 0o600))
}

// await blocks until want reports true, waking on every event this host
// produces rather than on a tick.
//
// Nothing here polls. The wait ends on the transition and costs what the
// transition costs, so a loaded machine makes the case slower and never makes
// it fail; the only clock is the wedged bound, which is far outside anything
// the work takes and is there so a stuck case reports rather than hangs.
//
// describe is called when the wait gives up, and not before. A message
// argument handed to require.Eventually is evaluated at the call site instead,
// so it reports the status as it was before the wait started rather than the
// one that failed.
func (w *wired) await(want func() bool, what string, describe func() string) {
	w.t.Helper()

	giveUp := time.NewTimer(wedged)
	defer giveUp.Stop()

	for {
		woken := w.moved.wait()

		if want() {
			return
		}

		select {
		case <-woken:
		case <-giveUp.C:
			w.t.Fatalf("%s, in %s. Last seen: %s", what, wedged, describe())
		case <-w.t.Context().Done():
			w.t.Fatalf("%s, and the test ended first. Last seen: %s", what, describe())
		}
	}
}

// waitReady blocks until the supervision plane reports the instance serving.
func (w *wired) waitReady(instance ref.Ref) {
	w.t.Helper()
	w.waitState(instance, condition.StateReady())
}

// waitBuilds blocks until a resource has been constructed want times, woken by
// the construction itself.
func (w *wired) waitBuilds(by *counted, want int64, what string) {
	w.t.Helper()

	w.await(func() bool { return by.buildCount.Load() == want }, what,
		func() string { return fmt.Sprintf("%d constructions", by.buildCount.Load()) })
}

// waitLog blocks until the host's log satisfies want. A child announces itself
// on its own standard error, so this is the only transition that says a second
// process reached the point it announces.
func (w *wired) waitLog(want func(string) bool, what string) {
	w.t.Helper()

	w.await(func() bool { return want(w.logs.String()) }, what,
		func() string { return "\n" + w.logs.String() })
}

// TestReloadingAnUnchangedConfigurationConstructsNothingAgain is the proof that
// matters most for an earlier design decision: it counts constructions of the real resource
// through the real supervisor, not calls to a stub.
func TestReloadingAnUnchangedConfigurationConstructsNothingAgain(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	w.writeFile(`
resources:
  sink:
    webhook:
      type: counted
      config:
        url: https://a.example
`)

	require.NoError(t, w.store.Reload(t.Context()))

	instance := theSink(t)
	w.waitReady(instance)
	require.Equal(t, int64(1), w.built.buildCount.Load())

	overlay, err := os.ReadFile(w.paths.Overlay)
	require.NoError(t, err)

	for range 5 {
		require.NoError(t, w.store.Reload(t.Context()))
	}

	require.Equal(t, int64(1), w.built.buildCount.Load(), "an unchanged configuration must not rebuild anything")
	require.Zero(t, w.built.closes.Load(), "nor close it")

	after, err := os.ReadFile(w.paths.Overlay)
	require.NoError(t, err)
	require.Equal(t, string(overlay), string(after), "nor write to the overlay")

	status, ok := w.host.Status(instance)
	require.True(t, ok)
	require.Equal(t, condition.StateReady().String(), status.State)
	require.Equal(t, int64(1), status.ObservedGeneration)
}

// TestASettingsChangeRestartsTheSameInstance is an earlier design decision's second outcome.
// The instance keeps its identity and its supervisor; only the resource inside
// it is rebuilt, and it is rebuilt from the declaration now in force.
func TestASettingsChangeRestartsTheSameInstance(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theSink(t)

	_, err := w.store.Apply(t.Context(), owner, instance,
		httpapi.ResourceSpec{Type: "counted", Enabled: true, Config: httpapi.ResourceConfig(`{"url":"a"}`)},
		httpapi.Precondition{MustNotExist: true})
	require.NoError(t, err)

	w.waitReady(instance)

	supervised, ok := w.host.Instance(instance)
	require.True(t, ok)

	uid := supervised.UID()
	require.False(t, uid.IsZero())

	version := w.store.Version()

	_, err = w.store.Apply(t.Context(), owner, instance,
		httpapi.ResourceSpec{Type: "counted", Enabled: true, Config: httpapi.ResourceConfig(`{"url":"b"}`)},
		httpapi.Precondition{IfMatch: []string{version}})
	require.NoError(t, err)

	w.waitBuilds(w.built, 2, "the settings change never rebuilt the resource")

	again, ok := w.host.Instance(instance)
	require.True(t, ok)
	require.Same(t, supervised, again, "a settings change must not replace the supervisor")
	require.Equal(t, uid, again.UID(), "nor the identity")

	specs := w.built.specs()
	require.Len(t, specs, 2)
	require.JSONEq(t, `{"url":"a"}`, string(specs[0].Config))
	require.JSONEq(t, `{"url":"b"}`, string(specs[1].Config),
		"the rebuild must read the declaration in force, not the one it was constructed with")

	w.waitReady(instance)
	require.Equal(t, int64(1), w.built.closes.Load(), "the previous incarnation is closed exactly once")
}

// TestATypeChangeInTheOperatorsFileReplacesTheInstance is an earlier design decision's third
// outcome. The API refuses this edit; the file is the only way it can arrive.
func TestATypeChangeInTheOperatorsFileReplacesTheInstance(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	w.writeFile("resources:\n  sink:\n    webhook:\n      type: counted\n")
	require.NoError(t, w.store.Reload(t.Context()))

	instance := theSink(t)
	w.waitReady(instance)

	first, ok := w.host.Instance(instance)
	require.True(t, ok)

	w.writeFile("resources:\n  sink:\n    webhook:\n      type: other\n")
	require.NoError(t, w.store.Reload(t.Context()))

	w.waitReady(instance)

	second, ok := w.host.Instance(instance)
	require.True(t, ok)
	require.NotSame(t, first, second, "a type change is a different instance, not an update")
	require.Equal(t, "other", second.Type())
	require.Equal(t, int64(1), w.alt.buildCount.Load())
	require.Equal(t, int64(1), w.built.closes.Load())
}

// TestRequiredIsAppliedWithoutRebuilding is an earlier design decision's first outcome: host
// policy moves on a live instance, and the resource never notices.
func TestRequiredIsAppliedWithoutRebuilding(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theSink(t)

	_, err := w.store.Apply(t.Context(), owner, instance,
		httpapi.ResourceSpec{Type: "counted", Enabled: true},
		httpapi.Precondition{MustNotExist: true})
	require.NoError(t, err)

	w.waitReady(instance)

	supervised, ok := w.host.Instance(instance)
	require.True(t, ok)
	require.False(t, supervised.Policy().Required)

	_, err = w.store.Apply(t.Context(), owner, instance,
		httpapi.ResourceSpec{Type: "counted", Enabled: true, Required: true},
		httpapi.Precondition{IfMatch: []string{w.store.Version()}})
	require.NoError(t, err)

	require.True(t, supervised.Policy().Required)
	require.Equal(t, int64(1), w.built.buildCount.Load(), "required must not rebuild the resource")
	require.Zero(t, w.built.closes.Load())
}

// TestDisablingStopsTheInstanceAndLeavesItDeclared keeps the operator's switch
// separate from the declaration: the block stays in the file, nothing runs.
func TestDisablingStopsTheInstanceAndLeavesItDeclared(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theSink(t)

	_, err := w.store.Apply(t.Context(), owner, instance,
		httpapi.ResourceSpec{Type: "counted", Enabled: true},
		httpapi.Precondition{MustNotExist: true})
	require.NoError(t, err)

	w.waitReady(instance)

	_, err = w.store.Apply(t.Context(), owner, instance,
		httpapi.ResourceSpec{Type: "counted", Enabled: false},
		httpapi.Precondition{IfMatch: []string{w.store.Version()}})
	require.NoError(t, err)

	_, ok := w.host.Status(instance)
	require.False(t, ok, "a disabled instance is not supervised")
	require.Equal(t, int64(1), w.built.closes.Load())

	found, err := w.store.Get(t.Context(), owner, instance)
	require.NoError(t, err)
	require.Equal(t, condition.StateDisabled().String(), found.Status.State)
	require.False(t, found.Enabled)
}

// TestAStartThatFailsIsReportedOnTheStatusAxis records the choice the store
// makes: the declaration was accepted and written, and the failure to start is
// the instance's status rather than the write's answer.
func TestAStartThatFailsIsReportedOnTheStatusAxis(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	w.built.failNext.Store(true)

	instance := theSink(t)

	_, err := w.store.Apply(t.Context(), owner, instance,
		httpapi.ResourceSpec{Type: "counted", Enabled: true},
		httpapi.Precondition{MustNotExist: true})
	require.NoError(t, err)

	w.waitState(instance, condition.StateRetrying())

	found, err := w.store.Get(t.Context(), owner, instance)
	require.NoError(t, err)
	require.Equal(t, condition.StateRetrying().String(), found.Status.State)
	require.False(t, found.Status.Serving)
}

// TestReconcileConvergesFromAHostThatDisagrees is what makes reconcile
// declarative rather than a log of events: a plan that names a restart against
// an instance the host is not supervising starts it, instead of latching it
// down with nothing left to retry it.
func TestReconcileConvergesFromAHostThatDisagrees(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theSink(t)

	declared := resource.Declaration{
		Ref:        instance,
		Spec:       resource.Spec{Type: "counted", Enabled: true},
		Source:     resource.SourceOverlay,
		Generation: 3,
	}

	// The cases run in order against one host, because each leaves it empty for
	// the next: that empty host is the state being converged from.
	for _, outcome := range []resource.Outcome{resource.OutcomeRestart, resource.OutcomeInPlace} {
		require.NoError(t, w.host.Reconcile(t.Context(),
			resource.Plan{Steps: []resource.Step{{Outcome: outcome, Ref: instance, Desired: declared}}}),
			"%s against a host that is not supervising the instance", outcome)

		w.waitReady(instance)

		status, ok := w.host.Status(instance)
		require.True(t, ok, "%s", outcome)
		require.Equal(t, int64(3), status.ObservedGeneration, "%s", outcome)

		require.NoError(t, w.host.Reconcile(t.Context(),
			resource.Plan{Steps: []resource.Step{{Outcome: resource.OutcomeStop, Ref: instance}}}))
	}
}

// TestReconcileWithoutRunIsRefused is what stops a plan being accepted by
// something that will never carry it out.
func TestReconcileWithoutRunIsRefused(t *testing.T) {
	t.Parallel()

	reg, err := registry.NewFrom()
	require.NoError(t, err)

	supervision, err := resourcehost.New(resourcehost.Config{Registry: reg})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	err = supervision.Reconcile(ctx, resource.Plan{Steps: []resource.Step{{Outcome: resource.OutcomeNone}}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestNewRefusesAHostWithNoRegistry(t *testing.T) {
	t.Parallel()

	_, err := resourcehost.New(resourcehost.Config{})
	require.ErrorIs(t, err, registry.ErrUnknownType)
}

// theSink is the instance these tests declare. It is one name in one place, so
// a test reads as what it is proving rather than as string handling.
func theSink(t *testing.T) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindSink, "webhook")
	require.NoError(t, err)

	return instance
}

// TestARestartRebuildsTheResourceThroughTheSupervisor is operate_resource at
// the end of its wire. The store decides, the host asks the supervisor, and the
// implementation is constructed again, so this counts real constructions of a
// real resource rather than calls to a stub.
//
// The declaration does not move. That is what separates a restart from a write:
// the same instance, the same identity, the same generation, built again.
func TestARestartRebuildsTheResourceThroughTheSupervisor(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theSink(t)

	_, err := w.store.Apply(t.Context(), owner, instance,
		httpapi.ResourceSpec{Type: "counted", Enabled: true, Config: httpapi.ResourceConfig(`{"url":"a"}`)},
		httpapi.Precondition{MustNotExist: true})
	require.NoError(t, err)

	w.waitReady(instance)
	require.Equal(t, int64(1), w.built.buildCount.Load())

	before, err := w.store.Get(t.Context(), owner, instance)
	require.NoError(t, err)

	require.NoError(t, w.store.Restart(t.Context(), owner, instance))

	w.waitBuilds(w.built, 2, "the supervisor never built the resource again")

	w.waitReady(instance)

	require.Equal(t, int64(1), w.built.closes.Load(), "the predecessor was not closed before the successor was built")

	after, err := w.store.Get(t.Context(), owner, instance)
	require.NoError(t, err)

	require.Equal(t, before.Version, after.Version, "a restart moved the version a write has to quote")
	require.Equal(t, before.UID, after.UID, "a restart minted a new identity")
	require.Equal(t, before.Generation, after.Generation, "a restart moved the declaration's generation")
	require.JSONEq(t, string(before.Config), string(after.Config), "a restart changed the settings")
}

// TestAStatusReadIsSafeBesideAReconcile holds the ownership rule Status has to
// obey: the declaration and the generation are the host's own bookkeeping,
// written by the goroutine Run drives, so a reader that wants either takes the
// host's lock for it.
//
// The readiness watcher is that reader in the running binary. It polls Status
// on the listener's goroutine while a configuration change moves an instance
// through the reconcile plan, which is two goroutines over one instance record
// and exactly what this arranges.
func TestAStatusReadIsSafeBesideAReconcile(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theSink(t)

	_, err := w.store.Apply(t.Context(), owner, instance,
		httpapi.ResourceSpec{Type: "counted", Enabled: true, Config: httpapi.ResourceConfig(`{"url":"a"}`)},
		httpapi.Precondition{MustNotExist: true})
	require.NoError(t, err)

	w.waitReady(instance)

	reconciled := make(chan struct{})

	var reading sync.WaitGroup

	reading.Go(func() {
		for {
			select {
			case <-reconciled:
				return
			default:
			}

			w.host.Status(instance)
		}
	})

	// Each settings change is a restart step, and a restart step is where the
	// host writes the generation the reader is projecting.
	for round := range 20 {
		version := w.store.Version()

		_, err = w.store.Apply(t.Context(), owner, instance,
			httpapi.ResourceSpec{
				Type:    "counted",
				Enabled: true,
				Config:  httpapi.ResourceConfig(fmt.Sprintf(`{"url":"%d"}`, round)),
			},
			httpapi.Precondition{IfMatch: []string{version}})
		require.NoError(t, err)
	}

	close(reconciled)
	reading.Wait()

	status, ok := w.host.Status(instance)
	require.True(t, ok)
	require.Equal(t, int64(21), status.ObservedGeneration,
		"the host reports the generation of the last declaration it acted on")
}

// The plane reports what it is supervising, and a name it holds nothing under
// is false rather than a silent success. It is what the store turns into
// RESOURCE_NOT_RUNNING.
func TestRestartingANameTheHostIsNotSupervisingReportsFalse(t *testing.T) {
	t.Parallel()

	w := newWired(t)

	require.False(t, w.host.Restart(theSink(t)))
}
