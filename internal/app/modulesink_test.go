package app

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// noEvents is the warning the sink feed writes for a sink it cannot feed.
const noEvents = "a declared sink is receiving no events"

// TestAModuleUnderTheSinkKindIsNotReportedAsASinkWithNoEvents is how every
// module is installed: an instance of the sink kind with runtime: service. A
// module reads this server's events by subscribing to the event API itself, so
// the feed leaves it alone and says nothing about it at start.
func TestAModuleUnderTheSinkKindIsNotReportedAsASinkWithNoEvents(t *testing.T) {
	t.Parallel()

	installed := installModule(t)

	feed := newFeedPlane(t, nil)
	feed.declare(t, lines(
		"resources:",
		"  sink:",
		"    planner:",
		"      type: "+moduleType,
		"      enabled: true",
		"      runtime: service",
		"      service:",
		"        path: "+slashed(installed.dir),
		"        checksum: sha256:"+installed.checksum,
		"        contract_major: 1",
	))

	feed.waitServing(t, "planner")

	attached, refused := map[ref.Ref]*sinkStream{}, map[ref.Ref]string{}

	incomplete := feed.feed.reconcile(t.Context(), feed.host, attached, refused)

	require.NotContains(t, feed.logs.String(), noEvents)
	require.Empty(t, attached, "a module was put on the bus as a sink")
	require.Empty(t, refused)
	require.False(t, incomplete, "a module was counted as a sink still to attach, so the feed retries it for ever")
}

// TestASinkTypeThatBuildsNoProcessorIsStillReported is the misconfiguration the
// warning exists for: a type registered under the sink kind whose instance does
// not receive events. It is not a module, so nothing else would say it.
func TestASinkTypeThatBuildsNoProcessorIsStillReported(t *testing.T) {
	t.Parallel()

	silent, err := registry.Declare(ref.KindSink, "silent", extension.Doc{
		Summary:     "A sink type that builds something which is not an event processor.",
		Description: "It exists so the sink feed has a misdeclared sink to refuse.",
		Version:     "1.0.0",
		Contract:    registry.Contract,
		Stability:   extension.StabilityExperimental,
	}, func(context.Context, host.Handle, registry.Spec) (closesOnly, error) { return closesOnly{}, nil })
	require.NoError(t, err)

	feed := newFeedPlane(t, []registry.Type{silent})
	feed.declare(t, lines(
		"resources:",
		"  sink:",
		"    quiet:",
		"      type: silent",
	))

	feed.waitServing(t, "quiet")

	attached, refused := map[ref.Ref]*sinkStream{}, map[ref.Ref]string{}

	incomplete := feed.feed.reconcile(t.Context(), feed.host, attached, refused)

	require.Contains(t, feed.logs.String(), noEvents)
	require.Contains(t, feed.logs.String(), "sink:quiet")
	require.Empty(t, attached)
	require.True(t, incomplete)
}

// closesOnly satisfies no contract in sdk/eventsapi.
type closesOnly struct{}

func (closesOnly) Close() error { return nil }

// feedPlane is the supervision plane with a sink feed whose reconcile a case
// calls itself, so the case reads one pass and not a timer.
type feedPlane struct {
	host  *resourcehost.Host
	store *resource.Store
	feed  *sinkFeed
	logs  *lockedBuffer
	dir   string
}

func newFeedPlane(t *testing.T, more []registry.Type) *feedPlane {
	t.Helper()

	shipped, err := builtin.Registry()
	require.NoError(t, err)

	types, err := registry.NewFrom(append(shipped.All(), more...)...)
	require.NoError(t, err)

	hub, err := stream.New(stream.Options{Site: "local", Authorizer: visibility{graph: newGraph(t)}})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	supervision, err := resourcehost.New(resourcehost.Config{Registry: types, Logger: discard()})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	stopped := make(chan error, 1)

	go func() { stopped <- supervision.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-stopped)
	})

	dir := t.TempDir()

	store, err := resource.New(resource.Config{
		Paths: resource.Paths{
			File:    filepath.Join(dir, "config.yaml"),
			Overlay: filepath.Join(dir, "config.d", "resources.json"),
		},
		Plane:  supervision,
		Graph:  newGraph(t),
		Logger: discard(),
	})
	require.NoError(t, err)

	logs := &lockedBuffer{}

	return &feedPlane{
		host: supervision, store: store, feed: newSinkFeed(hub, slog.New(slog.NewTextHandler(logs, nil))),
		logs: logs, dir: dir,
	}
}

func (p *feedPlane) declare(t *testing.T, content string) {
	t.Helper()

	write(t, filepath.Join(p.dir, "config.yaml"), content)
	require.NoError(t, p.store.Reload(t.Context()))
}

func (p *feedPlane) waitServing(t *testing.T, name string) {
	t.Helper()

	instance, err := ref.New(ref.KindSink, name)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		status, ok := p.host.Status(instance)

		return ok && status.Serving
	}, 30*time.Second, 20*time.Millisecond, "%s never started", instance)
}
