package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/bridge"
	"github.com/prusa3d/fractal-link/files"
	"github.com/prusa3d/fractal-link/internal/testport"
	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// TestOneNodeStartsAndStopsOnItsContext: serve builds the node on the real
// transport, the status shows the Node Path "hq", and serve returns nil after
// its context ends.
func TestOneNodeStartsAndStopsOnItsContext(t *testing.T) {
	if testing.Short() {
		t.Skip("this test starts a NATS server")
	}

	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	record := settingsAbove(t, t.TempDir(), testport.Free(t), testport.Free(t))
	state := newLink()

	done := make(chan error, 1)

	cfg, err := ParseConfig([]byte(record))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	settings := make(chan Config, 1)
	settings <- cfg

	go func() {
		done <- serve(ctx, state, &tree{}, session{
			fleet:   noMachines{},
			changes: &reports{},
		}, slog.New(slog.DiscardHandler), settings)
	}()

	awaitPath(t, state, "hq")
	cancel()

	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	case <-time.After(crossWait):
		t.Fatal("serve did not return after its context ended")
	}
}

// awaitPath waits until the status has the Node Path want.
func awaitPath(t *testing.T, state *link, want fractal.NodePath) {
	t.Helper()

	deadline := time.Now().Add(crossWait)
	for time.Now().Before(deadline) {
		held, err := state.Status(t.Context())
		if err != nil {
			t.Fatalf("read the link: %v", err)
		}

		if string(held.NodePath) == string(want) {
			return
		}

		time.Sleep(crossPoll)
	}

	t.Fatalf("the status never had the Node Path %s", want)
}

// TestSettingsThisBuildCannotRunOnStopTheNode: ParseConfig refuses an empty
// data_dir. It accepts an identity that is not an NKEY seed, and build then
// returns an error, so the module stops and does not run with no node.
func TestSettingsThisBuildCannotRunOnStopTheNode(t *testing.T) {
	t.Parallel()

	record := `{"node_id":"hq","data_dir":"","dedup_window":"1m"}`
	if _, err := ParseConfig([]byte(record)); !errors.Is(err, ErrNoDataDir) {
		t.Fatalf("ParseConfig returned %v, want ErrNoDataDir", err)
	}

	held, err := ParseConfig(fmt.Appendf(nil,
		`{"node_id":"hq","data_dir":%q,"dedup_window":"1m","identity":"not a seed"}`, t.TempDir()))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	if _, err = build(t.Context(), held, newLink(), &tree{}, session{fleet: noMachines{}},
		slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("build accepted an identity that is not an NKEY seed")
	}
}

// TestWhoACallFromAboveRunsAs: with command_principal "self" the host checks
// the permissions of this module, so principalOf returns the empty principal.
// Only "node" and "self" reach principalOf.
func TestWhoACallFromAboveRunsAs(t *testing.T) {
	t.Parallel()

	if held := principalOf(Config{CommandPrincipal: PrincipalSelf}, nil)(); held != "" {
		t.Errorf("with self the principal is %q, want the empty one", held)
	}

	// A value the host would deny every command for is refused at the start,
	// so principalOf never sees one.
	if _, err := ParseConfig([]byte(
		`{"node_id":"hq","data_dir":"/d","dedup_window":"5m","command_principal":"user:shift"}`,
	)); !errors.Is(err, ErrBadCommandPrincipal) {
		t.Errorf("ParseConfig returned %v, want ErrBadCommandPrincipal", err)
	}
}

// TestTheEngineHolderIsSafeBeforeTheNodeExists: build makes the transport
// before the node. So every method of engine must work while it has no node,
// and must pass the call to the node once it has one.
func TestTheEngineHolderIsSafeBeforeTheNodeExists(t *testing.T) {
	t.Parallel()

	holder := &engine{}

	holder.Deliver(t.Context(), fractalnode.Inbound{Kind: fractal.KindREP})
	holder.ChildConnected(t.Context(), "workshop")
	holder.ChildLost(t.Context(), "workshop")
	holder.ParentConnected(t.Context())
	holder.ParentLost(t.Context())

	if err := holder.SetPath(t.Context(), "hq"); err != nil {
		t.Fatalf("SetPath with no node returned %v, want nil", err)
	}

	node := standaloneNode(t)
	holder.node.Store(node)

	holder.Deliver(t.Context(), fractalnode.Inbound{Kind: fractal.KindREP, Payload: []byte(`{}`)})
	holder.ChildConnected(t.Context(), "workshop")
	holder.ChildLost(t.Context(), "workshop")
	holder.ParentConnected(t.Context())
	holder.ParentLost(t.Context())

	if err := holder.SetPath(t.Context(), "hq/workshop"); err != nil {
		t.Fatalf("SetPath with a node returned %v, want nil", err)
	}

	if node.Path() != "hq/workshop" {
		t.Fatalf("the Node Path is %s, want hq/workshop", node.Path())
	}

	if err := holder.SetPath(t.Context(), "not a path/"); err == nil {
		t.Fatal("SetPath accepted a Node Path that is not valid")
	}
}

// TestARevocationReachesTheShadow: the engine passes a revocation from the
// transport to the node, which forgets the state of the server below.
func TestARevocationReachesTheShadow(t *testing.T) {
	t.Parallel()

	node := standaloneNode(t)
	store := node.Shadow()
	holder := &engine{}
	holder.node.Store(node)
	if err := holder.SetPath(t.Context(), "hq"); err != nil {
		t.Fatalf("set the path: %v", err)
	}
	store.ApplyREP(fractal.REP{
		Version: fractal.Version, Sender: "hq/workshop", Interface: "Printer1", Instance: "1",
		Timestamp: 1, Props: fractal.Props{"temp": json.RawMessage(`1.0`)},
	})
	before := store.Len()

	holder.ChildRevoked(t.Context(), "workshop")

	if got := []int{before, store.Len()}; !slices.Equal(got, []int{1, 0}) {
		t.Errorf("the shadow held %d interfaces before and %d after the revocation, want 1 and 0", got[0], got[1])
	}
}

// standaloneNode returns a Fractal node with an empty shadow that keeps every
// interface, and a transport that sends nothing, for a test that calls the
// node directly.
func standaloneNode(t *testing.T) *fractalnode.Node {
	t.Helper()

	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
	if err != nil {
		t.Fatalf("build the shadow: %v", err)
	}

	node, err := fractalnode.New(fractalnode.Config{Binding: silent{}, Shadow: store})
	if err != nil {
		t.Fatalf("build the node: %v", err)
	}

	t.Cleanup(func() { _ = node.Close() })

	return node
}

// silent is a transport that accepts every message and sends none.
type silent struct{}

func (silent) SendBestEffort(context.Context, fractalnode.Dispatch) error { return nil }

func (silent) SendDurable(context.Context, fractalnode.Dispatch) error { return nil }

// TestTheBridgeRegistersThroughTheEngine: registrar.Register returns the handle
// of the node, and returns an error when the same instance is registered
// twice.
func TestTheBridgeRegistersThroughTheEngine(t *testing.T) {
	t.Parallel()

	node := standaloneNode(t)
	through := registrar{node: node}

	handle, err := through.Register(fractalnode.Instance{
		Owner:    quiet{},
		Name:     "Printer1",
		Instance: "1",
	})
	if err != nil {
		t.Fatalf("register one interface instance: %v", err)
	}

	if handle == nil {
		t.Fatal("Register returned a nil handle")
	}

	if _, err = through.Register(fractalnode.Instance{
		Owner: quiet{}, Name: "Printer1", Instance: "1",
	}); err == nil {
		t.Fatal("Register accepted the same interface instance twice")
	}
}

// quiet is an owner that returns empty answers, for a test that only registers
// an instance.
type quiet struct{}

func (quiet) Snapshot(context.Context) (fractal.Props, fractal.Timestamp, error) {
	return fractal.Props{}, 0, nil
}

func (quiet) Desire(context.Context, fractal.Props) (fractal.Props, error) {
	//nolint:nilnil // an owner that accepts and reports nothing.
	return nil, nil
}

func (quiet) Invoke(context.Context, fractalnode.Call) (json.RawMessage, error) { return nil, nil }

// TestAModuleWithNoEventStreamStillRuns: with no event stream, watch waits for
// ctx to end and returns nil. The bridge then learns of changes only from the
// device listing, which is slower and is not an error.
func TestAModuleWithNoEventStreamStillRuns(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)

	go func() { done <- watch(ctx, nil, nil) }()

	cancel()

	if err := <-done; err != nil {
		t.Fatalf("watch with no event stream returned %v, want nil", err)
	}
}

// TestTheTransportConfigurationCarriesWhatItIsGiven: bindingConfig puts the
// identity from the settings into the transport configuration. Its OnChange
// callback stores the new status and reports it to the host once.
func TestTheTransportConfigurationCarriesWhatItIsGiven(t *testing.T) {
	t.Parallel()

	signer, seed, err := natsbind.GenerateSigner()
	if err != nil {
		t.Fatalf("generate an identity: %v", err)
	}

	held, err := ParseConfig(fmt.Appendf(nil,
		`{"node_id":"hq","data_dir":%q,"dedup_window":"1m","identity":%q}`, t.TempDir(), seed))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	state := newLink()
	told := &reports{}

	cfg, err := bindingConfig(held, state, session{changes: told}, slog.New(slog.DiscardHandler), newCounters())
	if err != nil {
		t.Fatalf("build the transport configuration: %v", err)
	}

	wrote, err := signer.PublicKey()
	if err != nil {
		t.Fatalf("read the public key of the generated identity: %v", err)
	}

	reached, err := cfg.Identity.PublicKey()
	if err != nil {
		t.Fatalf("read the public key in the transport configuration: %v", err)
	}

	if reached != wrote {
		t.Fatal("the transport configuration has another identity than the settings")
	}

	cfg.OnChange(natsbind.Status{NodeID: "hq", NodePath: "hq", Accepting: true})

	reading, err := state.Status(t.Context())
	if err != nil {
		t.Fatalf("read the link: %v", err)
	}

	if reading.NodePath != "hq" || reading.Admits != "inherit" {
		t.Fatalf("the status after OnChange is %+v, want the path hq and admits inherit", reading)
	}

	if len(told.seen) != 1 {
		t.Fatalf("the host received %d reports, want 1", len(told.seen))
	}
}

// TestAShadowViewIsBuiltOverWhateverTheNodeKeeps: a ShadowView of an empty
// shadow counts no interface.
func TestAShadowViewIsBuiltOverWhateverTheNodeKeeps(t *testing.T) {
	t.Parallel()

	store, err := shadow.New(shadow.Config{})
	if err != nil {
		t.Fatalf("build the shadow: %v", err)
	}

	if counted := bridge.NewShadowView(store).Count(); counted.Interfaces != 0 {
		t.Fatalf("an empty shadow counts %+v, want zero interfaces", counted)
	}
}

// TestTheEngineDropsTheCopiesFromAbove: the copies of files from above go on
// a new Node Path and on ERR_JOIN_REVOKED, and stay on the same path
// (ADR-20261023-7r3 rule 10).
func TestTheEngineDropsTheCopiesFromAbove(t *testing.T) {
	t.Parallel()

	cache, err := files.OpenCache(t.TempDir(), 1<<20, nil)
	if err != nil {
		t.Fatalf("open the cache: %v", err)
	}

	holder := &engine{log: slog.New(slog.DiscardHandler)}
	holder.cache.Store(cache)

	data := []byte("G28")
	id := fractalfiles.Sum(data)
	keep := func() {
		putErr := cache.Put(id, func(sink files.Sink) (files.Retention, error) {
			_, writeErr := sink.Write(data)

			return files.Retention{Unlimited: true}, writeErr
		})
		if putErr != nil {
			t.Fatalf("keep a copy: %v", putErr)
		}
	}

	held := map[string]bool{}

	if err = holder.SetPath(t.Context(), "hq/workshop"); err != nil {
		t.Fatalf("SetPath: %v", err)
	}

	keep()

	if err = holder.SetPath(t.Context(), "hq/workshop"); err != nil {
		t.Fatalf("SetPath: %v", err)
	}

	held["the same path"] = cache.Path(id) != ""

	if err = holder.SetPath(t.Context(), "farm/workshop"); err != nil {
		t.Fatalf("SetPath: %v", err)
	}

	held["a new path"] = cache.Path(id) != ""

	keep()
	holder.ForgetAncestors(t.Context())

	held["a revocation"] = cache.Path(id) != ""

	if want := map[string]bool{
		"the same path": true,
		"a new path":    false,
		"a revocation":  false,
	}; !maps.Equal(
		held,
		want,
	) {
		t.Errorf("the copy was held after %v, want %v", held, want)
	}
}

// TestTheEngineTakesANewPathPastACopyItCannotDelete: a copy that cannot be
// deleted, as on Windows while a reader holds it open, does not stop the node
// from taking its granted path.
func TestTheEngineTakesANewPathPastACopyItCannotDelete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache, err := files.OpenCache(dir, 1<<20, nil)
	if err != nil {
		t.Fatalf("open the cache: %v", err)
	}

	holder := &engine{log: slog.New(slog.DiscardHandler)}
	holder.cache.Store(cache)

	node := standaloneNode(t)
	holder.node.Store(node)

	if err = holder.SetPath(t.Context(), "hq/workshop"); err != nil {
		t.Fatalf("SetPath: %v", err)
	}

	data := []byte("G28")
	id := fractalfiles.Sum(data)

	if err = cache.Put(id, func(sink files.Sink) (files.Retention, error) {
		_, writeErr := sink.Write(data)

		return files.Retention{Unlimited: true}, writeErr
	}); err != nil {
		t.Fatalf("keep a copy: %v", err)
	}

	copyPath := filepath.Join(dir, cache.Path(id))
	if err = os.Remove(copyPath); err != nil {
		t.Fatalf("remove the copy: %v", err)
	}

	if err = os.MkdirAll(filepath.Join(copyPath, "inside"), 0o750); err != nil {
		t.Fatalf("block the copy: %v", err)
	}

	setErr := holder.SetPath(t.Context(), "farm/workshop")

	got := map[string]any{"error": setErr, "path": node.Path()}
	if want := map[string]any{"error": nil, "path": fractal.NodePath("farm/workshop")}; !maps.Equal(got, want) {
		t.Errorf("SetPath past a blocked copy gave %v, want %v", got, want)
	}
}
