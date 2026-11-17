package natsbind_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
)

// filesName is the extension these tests carry. The binding does not read the
// data, so any JSON stands for it.
const filesName fractal.Identifier = "files"

// errNoEntry is what the check of these tests refuses a grant with.
var errNoEntry = errors.New("the grant carries no files entry")

// offering returns the settings of a node that accepts children and puts data
// in every grant under filesName.
func offering(t *testing.T, id fractal.NodeID, data string) natsbind.Config {
	t.Helper()
	cfg, _ := baseConfig(t, nodeOptions{id: id, accepts: true})
	cfg.Extensions = map[fractal.Identifier]json.RawMessage{filesName: json.RawMessage(data)}
	return cfg
}

// running returns the settings of printer-1 that joins parent, runs filesName
// and refuses a grant without it.
func running(t *testing.T, parent natsbind.Config, dataDir string) natsbind.Config {
	t.Helper()
	cfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: parent.ClientListen, dataDir: dataDir})
	cfg.JoinExtensions = []fractal.Identifier{filesName}
	cfg.CheckExtensions = func(held map[fractal.Identifier]json.RawMessage) error {
		if _, ok := held[filesName]; !ok {
			return errNoEntry
		}
		return nil
	}
	return cfg
}

// extensionOf reads ParentExtension as one comparable value.
func extensionOf(b *natsbind.Binding) string {
	data, ok := b.ParentExtension(filesName)
	if !ok {
		return "absent"
	}
	return string(data)
}

// TestGrantCarriesExtension: the parent puts its data in the grant, and the
// child keeps it.
func TestGrantCarriesExtension(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hq := offering(t, "hq", `{"version":1}`)
	startNode(ctx, t, hq)
	child := startNode(ctx, t, running(t, hq, ""))

	if got := extensionOf(child.binding); got != `{"version":1}` {
		t.Errorf("ParentExtension is %s, want the data of hq", got)
	}
}

// TestGrantWithoutExtensionIsRefused: a grant with no files entry is refused
// before the child saves anything, so it holds no Node Path.
func TestGrantWithoutExtensionIsRefused(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hq, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	startNode(ctx, t, hq)
	child := buildNode(ctx, t, running(t, hq, ""))

	err := child.binding.Join(ctx)
	if !errors.Is(err, join.ErrExtensionRefused) || !errors.Is(err, errNoEntry) {
		t.Fatalf("Join returned %v, want ErrExtensionRefused wrapping the check's error", err)
	}
	if path := child.binding.Path(); path != "" {
		t.Errorf("Path is %q after a refused grant, want none", path)
	}
}

// TestRefusedGrantIsNotRetried: Run with a grant the check refuses brings up
// no connection, says why, and does not return, so the process is not
// restarted to try again (fractal-files.md, Join Data).
func TestRefusedGrantIsNotRetried(t *testing.T) {
	t.Parallel()
	hq, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	startNode(t.Context(), t, hq)
	child := buildNode(t.Context(), t, running(t, hq, ""))

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	err := child.binding.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run returned %v, want it to wait until the context ends", err)
	}
	got := child.binding.Status().Uplink
	want := natsbind.Uplink{
		Address: hq.ClientListen,
		State:   natsbind.UplinkDenied,
		Reason:  "this node refused the grant of the node above: an extension refused the grant: " + errNoEntry.Error(),
		Refused: true,
	}
	if got != want {
		t.Errorf("the uplink is %+v, want %+v", got, want)
	}
}

// TestARefusedRejoinStopsTheRun: a grant the check refuses in a join while
// Run keeps the link takes the connection down and stops the renewals, as a
// refused renewal does (fractal.md, After the Join Exchange).
func TestARefusedRejoinStopsTheRun(t *testing.T) {
	t.Parallel()
	hqCfg := offering(t, "hq", `{"version":1}`)
	hq := startNode(t.Context(), t, hqCfg)
	cfg := running(t, hqCfg, "")
	var refuse atomic.Bool
	check := cfg.CheckExtensions
	cfg.CheckExtensions = func(held map[fractal.Identifier]json.RawMessage) error {
		if refuse.Load() {
			return errNoEntry
		}
		return check(held)
	}
	child := buildNode(t.Context(), t, cfg)
	ran := make(chan error, 1)
	go func() { ran <- child.binding.Run(t.Context()) }()
	waitUntil(t, "hq to see printer-1 connected", func() bool { return len(hq.engine.Connected()) >= 1 })

	refuse.Store(true)
	if err := child.binding.Join(t.Context()); !errors.Is(err, join.ErrExtensionRefused) {
		t.Fatalf("Join returned %v, want ErrExtensionRefused", err)
	}
	waitUntil(t, "hq to see printer-1 lost", func() bool { return len(hq.engine.Lost()) >= 1 })
	time.Sleep(100 * time.Millisecond) // the window in which a Run that does not wait would return
	select {
	case err := <-ran:
		t.Fatalf("Run returned %v, want it to wait until the context ends", err)
	default:
	}
	want := natsbind.Uplink{
		Address: hqCfg.ClientListen,
		State:   natsbind.UplinkDenied,
		Reason:  "this node refused the grant of the node above: an extension refused the grant: " + errNoEntry.Error(),
		Refused: true,
	}
	got := child.binding.Status().Uplink
	got.ConnectedAt = nil // the time of the connection the grant came over
	if got != want {
		t.Errorf("the uplink is %+v, want %+v", got, want)
	}
}

// TestASavedMembershipTheCheckRefusesIsNotUsed: a node that saved a grant
// without the extension and now runs it holds no Node Path at start, so it
// cannot bring up a link on that grant, and says why (fractal-files.md, Join
// Data).
func TestASavedMembershipTheCheckRefusesIsNotUsed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	hqCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	startNode(t.Context(), t, hqCfg)
	plain, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: hqCfg.ClientListen, dataDir: dir})
	first := startNode(t.Context(), t, plain)
	if err := first.binding.Close(); err != nil {
		t.Fatalf("close printer-1: %v", err)
	}

	again := buildNode(t.Context(), t, running(t, hqCfg, dir))
	want := natsbind.Uplink{
		Address: hqCfg.ClientListen,
		State:   natsbind.UplinkDenied,
		Reason: "this node refused the grant of the node above: the saved grant: " +
			"an extension refused the grant: " + errNoEntry.Error(),
		Refused: true,
	}
	if path, got := again.binding.Path(), again.binding.Status().Uplink; path != "" || got != want {
		t.Errorf("after the restart the path is %q and the uplink %+v, want no path and %+v", path, got, want)
	}
}

// TestRevocationDropsExtension: after ERR_JOIN_REVOKED the child holds no
// data of its former parent, and a restart does not bring it back.
func TestRevocationDropsExtension(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dir := t.TempDir()
	hqCfg := offering(t, "hq", `{"version":1}`)
	hq := startNode(ctx, t, hqCfg)
	childCfg := running(t, hqCfg, dir)
	child := startNode(ctx, t, childCfg)

	if err := hq.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	var denial *join.DenialError
	if err := child.binding.Join(ctx); !errors.As(err, &denial) || denial.Denial.Code != fractal.CodeJoinRevoked {
		t.Fatalf("Join after the revocation returned %v, want ERR_JOIN_REVOKED", err)
	}
	if got := extensionOf(child.binding); got != "absent" {
		t.Errorf("ParentExtension is %s after the revocation, want absent", got)
	}
	if err := child.binding.Close(); err != nil {
		t.Fatalf("close printer-1: %v", err)
	}

	again := buildNode(context.WithoutCancel(ctx), t, childCfg)
	if got := extensionOf(again.binding); got != "absent" {
		t.Errorf("ParentExtension is %s after a restart, want absent", got)
	}
}

// TestNewParentReplacesExtension: a child that joins another parent under a
// new Node Path keeps the data of the new parent and none of the former one.
func TestNewParentReplacesExtension(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dir := t.TempDir()
	hq := offering(t, "hq", `{"from":"hq"}`)
	startNode(ctx, t, hq)
	first := startNode(ctx, t, running(t, hq, dir))
	if err := first.binding.Close(); err != nil {
		t.Fatalf("close printer-1: %v", err)
	}

	school := offering(t, "school", `{"from":"school"}`)
	startNode(ctx, t, school)
	moved := startNode(ctx, t, running(t, school, dir))

	got := struct {
		path fractal.NodePath
		data string
	}{moved.binding.Path(), extensionOf(moved.binding)}
	want := struct {
		path fractal.NodePath
		data string
	}{"school/printer-1", `{"from":"school"}`}
	if got != want {
		t.Errorf("after the move the child holds %+v, want %+v", got, want)
	}
}

// TestChildByIdentity: a key names an admitted child until it is revoked.
func TestChildByIdentity(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	key := p.below.binding.Identity()

	type found struct {
		id fractal.NodeID
		ok bool
	}
	lookup := func(id fractal.Identity) found {
		nodeID, ok := p.above.binding.ChildByIdentity(id)
		return found{nodeID, ok}
	}

	if got := lookup(key); got != (found{"printer-1", true}) {
		t.Errorf("ChildByIdentity of printer-1 is %+v, want printer-1", got)
	}
	if got := lookup(p.above.binding.Identity()); got != (found{}) {
		t.Errorf("ChildByIdentity of hq's own key is %+v, want none", got)
	}
	if err := p.above.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if got := lookup(key); got != (found{}) {
		t.Errorf("ChildByIdentity of a revoked child is %+v, want none", got)
	}
}

// TestSignerSignsAsIdentity: Signer signs with the key Identity names.
func TestSignerSignsAsIdentity(t *testing.T) {
	t.Parallel()
	cfg, _ := baseConfig(t, nodeOptions{id: "hq"})
	n := buildNode(t.Context(), t, cfg)

	key, err := n.binding.Signer().PublicKey()
	if err != nil {
		t.Fatalf("read the public key: %v", err)
	}
	if fractal.Identity(key) != n.binding.Identity() {
		t.Errorf("Signer holds %s and Identity is %s", key, n.binding.Identity())
	}
}
