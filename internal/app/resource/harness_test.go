package resource_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/hostschema"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// owner is the principal the harness bootstraps the graph for, spelled the way
// the composition root spells one: a bare id is not an identity to the graph,
// because two objects of different types may share one.
const owner = "user:operator"

// stranger is a principal the graph knows nothing about, for the cases that
// have to observe what somebody holding no grant is told.
const stranger = "user:stranger"

// harness is one store over a pair of files in a temporary directory, with a
// plane that records what it was asked to do rather than supervising anything,
// and a real authorization graph over a database of its own.
type harness struct {
	t     *testing.T
	dir   string
	paths resource.Paths
	store *resource.Store
	plane *recordingPlane
	graph *authz.Store
}

func newHarness(t *testing.T, options ...func(*resource.Config)) *harness {
	t.Helper()

	dir := t.TempDir()
	plane := &recordingPlane{status: map[ref.Ref]httpapi.ResourceStatus{}}
	graph := newGraph(t, owner)

	cfg := resource.Config{
		Paths: resource.Paths{
			File:    filepath.Join(dir, "config.yaml"),
			Overlay: filepath.Join(dir, "config.d", "resources.json"),
		},
		Plane: plane,
		Graph: graph,
	}

	for _, option := range options {
		option(&cfg)
	}

	store, err := resource.New(cfg)
	require.NoError(t, err)

	return &harness{t: t, dir: dir, paths: cfg.Paths, store: store, plane: plane, graph: graph}
}

// newGraph is a real graph on a database of this test's own, bootstrapped so
// that first holds manager on every resource kind. It is the real store rather
// than a stand-in because the refusals under test are properties of the
// projection and a stand-in would be asserting against a second copy of them.
func newGraph(t *testing.T, first string) *authz.Store {
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

	subject, err := authz.ParseSubject(first)
	require.NoError(t, err)

	written, err := graph.Bootstrap(t.Context(), "local", subject)
	require.NoError(t, err)
	require.True(t, written, "a fresh graph holds no tuple, so the first grant is written")

	return graph
}

// writeFile puts bytes into the operator's hand-authored file, which is what a
// person with an editor does and what the API never does.
func (h *harness) writeFile(content string) {
	h.t.Helper()
	require.NoError(h.t, os.WriteFile(h.paths.File, []byte(content), 0o600))
}

// writeOverlayByHand edits the machine-owned file from outside the store, which
// is the second writer the concurrency token exists for.
func (h *harness) writeOverlayByHand(content string) {
	h.t.Helper()
	require.NoError(h.t, os.MkdirAll(filepath.Dir(h.paths.Overlay), 0o700))
	require.NoError(h.t, os.WriteFile(h.paths.Overlay, []byte(content), 0o600))
}

func (h *harness) overlayBytes() []byte {
	h.t.Helper()

	content, err := os.ReadFile(h.paths.Overlay)
	if os.IsNotExist(err) {
		return nil
	}

	require.NoError(h.t, err)

	return content
}

// stray reports files in the overlay's directory that are neither the overlay
// nor its backup. A staged write that was abandoned without being discarded
// shows up here.
func (h *harness) stray() []string {
	h.t.Helper()

	entries, err := os.ReadDir(filepath.Dir(h.paths.Overlay))
	if os.IsNotExist(err) {
		return nil
	}

	require.NoError(h.t, err)

	var out []string

	for _, entry := range entries {
		switch entry.Name() {
		case filepath.Base(h.paths.Overlay), filepath.Base(h.paths.Overlay) + ".bak":
		default:
			out = append(out, entry.Name())
		}
	}

	return out
}

func (h *harness) version() string {
	h.t.Helper()

	listing, err := h.store.List(h.t.Context(), owner, "")
	require.NoError(h.t, err)

	return listing.Version
}

func (h *harness) get(instance ref.Ref) (httpapi.ResourceInstance, error) {
	h.t.Helper()

	return h.store.Get(h.t.Context(), owner, instance)
}

// create declares an instance that must not exist yet, which is the only shape
// of write that may create one.
func (h *harness) create(instance ref.Ref, spec httpapi.ResourceSpec) (httpapi.ApplyResult, error) {
	h.t.Helper()

	return h.store.Apply(h.t.Context(), owner, instance, spec, httpapi.Precondition{MustNotExist: true})
}

func (h *harness) update(instance ref.Ref, spec httpapi.ResourceSpec, version string) (httpapi.ApplyResult, error) {
	h.t.Helper()

	return h.store.Apply(h.t.Context(), owner, instance, spec,
		httpapi.Precondition{IfMatch: []string{version}})
}

func mustRef(t *testing.T, kind ref.Kind, name string) ref.Ref {
	t.Helper()

	instance, err := ref.New(kind, name)
	require.NoError(t, err)

	return instance
}

func spec(typeName, config string) httpapi.ResourceSpec {
	return httpapi.ResourceSpec{
		Type:    typeName,
		Enabled: true,
		Config:  httpapi.ResourceConfig(config),
	}
}

// recordingPlane stands in for the supervision plane. It counts what it was
// asked to do, which is how a test can tell a reconcile that changed nothing
// from one that restarted an instance.
type recordingPlane struct {
	mu        sync.Mutex
	plans     []resource.Plan
	steps     []resource.Step
	restarted []ref.Ref
	status    map[ref.Ref]httpapi.ResourceStatus
	// documented are the instances a case declares as modules that shipped a
	// document, which the listing renders an address for.
	documented map[ref.Ref]bool
	// drawn are the instances a case declares as modules whose bundle carries
	// a ui block, which the listing publishes beside the instance.
	drawn    map[ref.Ref]httpapi.ResourceUI
	failWith error
	// history and log are what the plane answers for an instance, and asked is
	// the start of the last history it was asked for.
	history map[ref.Ref]httpapi.ResourceMemoryHistory
	log     map[ref.Ref]httpapi.ResourceLog
	asked   time.Time
}

func (p *recordingPlane) MemoryHistory(instance ref.Ref, since time.Time) (httpapi.ResourceMemoryHistory, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.asked = since
	history, ok := p.history[instance]

	return history, ok
}

func (p *recordingPlane) Log(instance ref.Ref) (httpapi.ResourceLog, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	lines, ok := p.log[instance]

	return lines, ok
}

func (p *recordingPlane) Reconcile(_ context.Context, plan resource.Plan) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.plans = append(p.plans, plan)
	p.steps = append(p.steps, plan.Steps...)

	for _, step := range plan.Steps {
		if step.Outcome == resource.OutcomeStop {
			delete(p.status, step.Ref)

			continue
		}

		p.status[step.Ref] = httpapi.ResourceStatus{
			State:              condition.StateReady().String(),
			Serving:            true,
			ObservedGeneration: step.Desired.Generation,
		}
	}

	return p.failWith
}

func (p *recordingPlane) Status(instance ref.Ref) (httpapi.ResourceStatus, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	status, ok := p.status[instance]

	return status, ok
}

// Documented reports the instances a case declared as modules shipping a
// document.
func (p *recordingPlane) Documented(instance ref.Ref) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.documented[instance]
}

// UI reports the browser half a case declared for an instance, the way the
// real plane reports one only while the instance is running.
func (p *recordingPlane) UI(instance ref.Ref) (httpapi.ResourceUI, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	surface, drawn := p.drawn[instance]

	return surface, drawn
}

// Restart records the ask and reports what the real plane reports: false for a
// name it is supervising nothing under, which here is a name no plan started.
func (p *recordingPlane) Database(ref.Ref) (wirerpc.DatabaseState, bool) {
	return wirerpc.DatabaseState{}, false
}

func (p *recordingPlane) Restart(instance ref.Ref) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, running := p.status[instance]; !running {
		return false
	}

	p.restarted = append(p.restarted, instance)

	return true
}

// restarts returns the instances the plane was asked to restart, in order.
func (p *recordingPlane) restarts() []ref.Ref {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]ref.Ref(nil), p.restarted...)
}

// outcomes returns every step the plane has been asked to carry out, as
// "outcome ref" strings, which is what a test asserts against.
func (p *recordingPlane) outcomes() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]string, 0, len(p.steps))
	for _, step := range p.steps {
		out = append(out, step.Outcome.String()+" "+step.Ref.String())
	}

	return out
}

func (p *recordingPlane) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.plans = nil
	p.steps = nil
}

func (p *recordingPlane) planCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.plans)
}

// parses reports whether the bytes are a whole JSON document, which is what a
// reader racing a write must always see.
func parses(content []byte) bool {
	if len(content) == 0 {
		return true
	}

	var into map[string]json.RawMessage

	return json.Unmarshal(content, &into) == nil
}

// sinceAsked is the start of the last history the plane was asked for.
func (p *recordingPlane) sinceAsked() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.asked
}
