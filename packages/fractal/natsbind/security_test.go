package natsbind_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/prusa3d/fractal-link/natsbind"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// TestRevocationCutsTheConnectionItAlreadyHolds checks that Revoke closes the
// connection the node below has open. It is not enough to refuse the node's
// next connection (fractal.md, Admission and Revocation).
func TestRevocationCutsTheConnectionItAlreadyHolds(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)

	if err := p.above.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	waitUntil(t, "the node above to call ChildLost for printer-1", func() bool {
		return len(p.above.engine.Lost()) == 1
	})
	status := p.above.binding.Status()
	if len(status.Nodes) != 1 || status.Nodes[0].Admission != natsbind.AdmissionRevoked {
		t.Fatalf("Status.Nodes is %v, want printer-1 revoked", status.Nodes)
	}
	waitUntil(t, "the node below to lose its connection", func() bool {
		return p.below.binding.Status().Uplink.State != natsbind.UplinkConnected
	})
	if err := p.above.binding.SendBestEffort(ctx, fractalnode.Dispatch{
		Kind: fractal.KindDES, Payload: desPayload(t, "hq/printer-1"), Child: "printer-1",
	}); !errors.Is(err, natsbind.ErrNoLink) {
		t.Errorf("SendBestEffort to a revoked node returned %v, want ErrNoLink", err)
	}
}

// TestARevokedNodeLeavesNothingBehind: a revocation tells the engine, so it
// can delete the shadow of the node, and deletes the method invocations that
// wait for the node. The node never connects again, so nothing would read
// them (fractal-requirements.md: all data of a removed node SHOULD be
// deleted).
func TestARevokedNodeLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	p.link.Cut()
	waitUntil(t, "hq to lose printer-1", func() bool { return len(p.above.engine.Lost()) == 1 })
	send(t, p.above, fractalnode.Dispatch{
		Kind: fractal.KindRPC, Payload: rpcPayload(t, "waits"), Child: "printer-1", ID: "waits",
	})
	if queued, err := natsbind.QueuedFor(p.above.binding, "printer-1"); err != nil || queued != 1 {
		t.Fatalf("the stream of printer-1 holds %d invocations (%v), want the one sent", queued, err)
	}

	if err := p.above.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	queued, err := natsbind.QueuedFor(p.above.binding, "printer-1")
	if err != nil {
		t.Fatalf("read the stream of printer-1: %v", err)
	}
	got := map[string]any{"revoked": p.above.engine.Revoked(), "queued": queued}
	want := map[string]any{"revoked": []fractal.NodeID{"printer-1"}, "queued": uint64(0)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the revocation hq has %v, want %v", got, want)
	}
}

// TestCallsSentDuringARevocationAreDeleted: method invocations sent while a
// revocation runs are either refused or deleted with the others. None waits
// for a node that never connects again.
func TestCallsSentDuringARevocationAreDeleted(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	p.link.Cut()
	waitUntil(t, "hq to lose printer-1", func() bool { return len(p.above.engine.Lost()) == 1 })

	var senders sync.WaitGroup
	for sender := range 64 {
		senders.Go(func() {
			for call := 0; ; call++ {
				id := fractal.RPCID(fmt.Sprintf("call-%d-%d", sender, call))
				if p.above.binding.SendDurable(ctx, fractalnode.Dispatch{
					Kind: fractal.KindRPC, Payload: rpcPayload(t, id), Child: "printer-1", ID: id,
				}) != nil {
					return
				}
			}
		})
	}
	waitUntil(t, "a call to wait for printer-1", func() bool {
		queued, err := natsbind.QueuedFor(p.above.binding, "printer-1")
		return err == nil && queued > 0
	})
	if err := p.above.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	senders.Wait()

	if queued, err := natsbind.QueuedFor(p.above.binding, "printer-1"); err != nil || queued != 0 {
		t.Errorf("after the revocation %d calls wait for printer-1 (%v), want none", queued, err)
	}
}

// TestARevocationBeforeTheLinkStands: an operator revokes printer-9 after its
// join request passed the checks and before hq built its link. hq MUST deny
// the request and keep the node revoked (fractal.md, Admission and
// Revocation).
func TestARevocationBeforeTheLinkStands(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	hq := startNode(ctx, t, cfg)
	user, err := nkeys.CreateUser()
	if err != nil {
		t.Fatalf("create a key: %v", err)
	}
	signed, err := joinwire.Sign(joinwire.Request{Version: fractal.Version, NodeID: "printer-9"}, user)
	if err != nil {
		t.Fatalf("sign the join request: %v", err)
	}

	reply, err := natsbind.RevokeBeforeTheReply(ctx, hq.binding, signed)
	if err != nil {
		t.Fatalf("join and revoke printer-9: %v", err)
	}
	admissions := map[fractal.NodeID]natsbind.Admission{}
	for _, below := range hq.binding.Status().Nodes {
		admissions[below.ID] = below.Admission
	}
	got := map[string]any{"code": reply.Code, "admissions": admissions}
	want := map[string]any{
		"code":       fractal.CodeJoinRevoked,
		"admissions": map[fractal.NodeID]natsbind.Admission{"printer-9": natsbind.AdmissionRevoked},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the revocation hq has %v, want %v", got, want)
	}
}

// TestRevokingANodeNobodyAdmittedIsRefused checks that Revoke returns
// ErrUnknownNode for a Node ID that never joined.
func TestRevokingANodeNobodyAdmittedIsRefused(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	if err := p.above.binding.Revoke(ctx, "printer-9"); !errors.Is(err, natsbind.ErrUnknownNode) {
		t.Errorf("Revoke returned %v, want ErrUnknownNode", err)
	}
}

// TestSiblingsSeeNothingOfEachOther checks that the account of each link
// isolates it. Two nodes join hq. A state report from printer-1 reaches hq and
// not printer-2, and a method invocation for printer-1 reaches only printer-1
// (fractal-nats.md, Per-link account isolation).
func TestSiblingsSeeNothingOfEachOther(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	relay := newProxy(t, fmt.Sprintf("127.0.0.1:%d", aboveLeaf))
	aboveCfg.AdvertisedLeafRemote = "nats://" + relay.Addr()
	relay.Open()
	hq := startNode(ctx, t, aboveCfg)

	oneCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
	one := startNode(ctx, t, oneCfg)
	twoCfg, _ := baseConfig(t, nodeOptions{id: "printer-2", parent: aboveCfg.ClientListen})
	two := startNode(ctx, t, twoCfg)
	waitUntil(t, "hq to see both nodes as connected", func() bool { return len(hq.engine.Connected()) == 2 })

	send(t, one, fractalnode.Dispatch{Kind: fractal.KindREP, Payload: repPayload(t, "hq/printer-1", 1)})
	awaitKind(t, hq, fractal.KindREP, 1)
	if got := hq.engine.Messages()[0].From; got != "printer-1" {
		t.Errorf("the state report arrived on the link to %q, want printer-1", got)
	}
	if got := countKind(two, fractal.KindREP); got != 0 {
		t.Errorf("printer-2 received %d state reports of printer-1, want 0", got)
	}

	send(t, hq, fractalnode.Dispatch{
		Kind: fractal.KindRPC, Payload: rpcPayload(t, "one"), Child: "printer-1", ID: "one",
	})
	awaitKind(t, one, fractal.KindRPC, 1)
	if got := countKind(two, fractal.KindRPC); got != 0 {
		t.Errorf("printer-2 received %d method invocations for printer-1, want 0", got)
	}

	// printer-2 has no link to printer-1, because printer-1 did not join it.
	// So it cannot send to printer-1 even with the right Node ID.
	err := two.binding.SendBestEffort(ctx, fractalnode.Dispatch{
		Kind: fractal.KindDES, Payload: desPayload(t, "hq/printer-1"), Child: "printer-1",
	})
	if !errors.Is(err, natsbind.ErrNoLink) {
		t.Errorf("SendBestEffort from printer-2 to printer-1 returned %v, want ErrNoLink", err)
	}
}

// TestAnUnknownIdentityWaitsForAnOperator checks Config.AskFirst between two
// real nodes. hq denies the first join request with CodeAwaitingApproval and
// lists the node as pending. The node sends the request again, and hq grants
// the request that arrives after an operator approved.
func TestAnUnknownIdentityWaitsForAnOperator(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true, askFirst: true})
	relay := newProxy(t, fmt.Sprintf("127.0.0.1:%d", aboveLeaf))
	aboveCfg.AdvertisedLeafRemote = "nats://" + relay.Addr()
	relay.Open()
	hq := startNode(ctx, t, aboveCfg)

	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
	below := buildNode(ctx, t, belowCfg)
	// Join retries until the context ends, so the first call gets a context
	// with a timeout.
	tryCtx, giveUp := context.WithTimeout(ctx, 2*time.Second)
	defer giveUp()
	if err := below.binding.Join(tryCtx); err == nil {
		t.Fatal("Join succeeded for a node no operator approved")
	}
	waitUntil(t, "hq to list printer-1 as pending", func() bool {
		for _, n := range hq.binding.Status().Nodes {
			if n.ID == "printer-1" && n.Admission == natsbind.AdmissionPending {
				return true
			}
		}
		return false
	})
	if err := hq.binding.Approve(ctx, "printer-1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := below.binding.Join(ctx); err != nil {
		t.Fatalf("Join after the approval: %v", err)
	}
	if got := below.binding.Path(); got != "hq/printer-1" {
		t.Errorf("Path is %q, want hq/printer-1", got)
	}
}

// TestADeniedNodeLogsTheDenial: a node logs each denial with its code
// (fractal.md, The Join Exchange: "A denied node SHOULD log the denial"). A
// wait for an operator is a warning, and a denial that allows no retry is an
// error.
func TestADeniedNodeLogsTheDenial(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true, askFirst: true})
	aboveCfg.AdvertisedLeafRemote = fmt.Sprintf("nats://127.0.0.1:%d", aboveLeaf)
	hq := startNode(ctx, t, aboveCfg)

	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
	denials := &denialLog{}
	belowCfg.Logger = slog.New(denials)
	below := buildNode(ctx, t, belowCfg)

	tryCtx, giveUp := context.WithCancel(ctx)
	waiting := make(chan error, 1)
	go func() { waiting <- below.binding.Join(tryCtx) }()
	waitUntil(t, "hq to deny printer-1 until an operator approves it", func() bool {
		return below.binding.Status().Uplink.Code == joinwire.CodeAwaitingApproval
	})
	giveUp()
	if err := <-waiting; err == nil {
		t.Fatal("Join succeeded for a node no operator approved")
	}
	if err := hq.binding.Approve(ctx, "printer-1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := below.binding.Join(ctx); err != nil {
		t.Fatalf("Join after the approval: %v", err)
	}
	if err := hq.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := below.binding.Join(ctx); err == nil {
		t.Fatal("Join succeeded after the revocation")
	}

	want := map[fractal.Code]slog.Level{}
	want[joinwire.CodeAwaitingApproval] = slog.LevelWarn
	want[fractal.CodeJoinRevoked] = slog.LevelError
	got := denials.Levels()
	// The retries while printer-1 waits can reach hq's bound on join
	// requests, which hq then denies as busy.
	if _, busy := got[fractal.CodeJoinUnavailable]; busy {
		want[fractal.CodeJoinUnavailable] = slog.LevelWarn
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the node logged the denials at %v, want %v", got, want)
	}
}

// denialLog is a slog.Handler that keeps the level of the denials a node
// logs, by their code.
type denialLog struct {
	mu     sync.Mutex
	levels map[fractal.Code]slog.Level
}

func (d *denialLog) Enabled(context.Context, slog.Level) bool { return true }

func (d *denialLog) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "the node above denied this node" {
		return nil
	}
	var code fractal.Code
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "code" {
			code, _ = a.Value.Any().(fractal.Code)
		}
		return true
	})
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.levels == nil {
		d.levels = map[fractal.Code]slog.Level{}
	}
	d.levels[code] = r.Level
	return nil
}

func (d *denialLog) WithAttrs([]slog.Attr) slog.Handler { return d }

func (d *denialLog) WithGroup(string) slog.Handler { return d }

// Levels returns the level of the last denial logged for each code.
func (d *denialLog) Levels() map[fractal.Code]slog.Level {
	d.mu.Lock()
	defer d.mu.Unlock()
	return maps.Clone(d.levels)
}

// TestTheOperatorApprovesTheIdentityThatAskedFirst checks the case an
// attacker can try: two machines ask hq for the Node ID printer-1, each with
// its own key. hq keeps the key that asked first, shows that key to the
// operator, names it in the log line of the approval, and denies the machine
// that asked later even after the approval (fractal.md, Uniqueness).
func TestTheOperatorApprovesTheIdentityThatAskedFirst(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true, askFirst: true})
	aboveCfg.AdvertisedLeafRemote = fmt.Sprintf("nats://127.0.0.1:%d", aboveLeaf)
	written := &bytes.Buffer{}
	aboveCfg.Logger = slog.New(slog.NewTextHandler(written, nil))
	hq := startNode(ctx, t, aboveCfg)

	earlyCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
	early := buildNode(ctx, t, earlyCfg)
	lateCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
	late := buildNode(ctx, t, lateCfg)

	tryJoin := func(n *node) error {
		tryCtx, giveUp := context.WithTimeout(ctx, 2*time.Second)
		defer giveUp()
		return n.binding.Join(tryCtx)
	}
	if err := tryJoin(early); err == nil {
		t.Fatal("the machine that asked first joined before the operator approved it")
	}
	if err := tryJoin(late); err == nil {
		t.Fatal("the machine that asked later joined before the operator approved it")
	}

	wanted := early.binding.Status().Identity
	waiting := hq.binding.Status().Nodes
	if len(waiting) != 1 {
		t.Fatalf("hq lists %d machines, want one", len(waiting))
	}
	if waiting[0].Identity != wanted {
		t.Fatalf("the operator sees the key %q, want %q from the machine that asked first",
			waiting[0].Identity, wanted)
	}

	if err := hq.binding.Approve(ctx, "printer-1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !strings.Contains(written.String(), string(wanted)) {
		t.Errorf("the log of the approval does not name the key %q", wanted)
	}
	if err := late.binding.Join(ctx); err == nil {
		t.Fatal("the machine that asked later joined under the approval of another key")
	}
	if err := early.binding.Join(ctx); err != nil {
		t.Fatalf("the machine the operator approved was refused: %v", err)
	}
}

// TestAJoinRequestWithNoTypSaysWhy checks the denial a server on an earlier
// release receives: its join request names no typ, and the reason tells the
// operator of that server what to do, because the code is not retried.
func TestAJoinRequestWithNoTypSaysWhy(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	aboveCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	startNode(ctx, t, aboveCfg)

	user, err := nkeys.CreateUser()
	if err != nil {
		t.Fatalf("create a key: %v", err)
	}
	pub, err := user.PublicKey()
	if err != nil {
		t.Fatalf("read the public key: %v", err)
	}
	signed, err := joinwire.Sign(joinwire.Request{Version: fractal.Version, NodeID: "printer-1"}, user)
	if err != nil {
		t.Fatalf("sign the join request: %v", err)
	}
	header, err := json.Marshal(joinwire.Header{Alg: joinwire.AlgEdDSA, NKey: pub})
	if err != nil {
		t.Fatalf("encode the header: %v", err)
	}
	parts := bytes.SplitN(signed, []byte("."), 2)
	untyped := append([]byte(base64.RawURLEncoding.EncodeToString(header)+"."), parts[1]...)

	conn, err := nats.Connect("nats://" + aboveCfg.ClientListen)
	if err != nil {
		t.Fatalf("connect to hq: %v", err)
	}
	defer conn.Close()
	msg, err := conn.RequestWithContext(ctx, joinwire.SubjectJoin, untyped)
	if err != nil {
		t.Fatalf("send the join request: %v", err)
	}
	reply, err := joinwire.DecodeReply(msg.Data)
	if err != nil {
		t.Fatalf("decode the answer: %v", err)
	}

	want := joinwire.Reply{
		Version: fractal.Version,
		Code:    fractal.CodeJoinRejected,
		Reason:  "the join request does not name typ fractal-join+jws; update the server that asks to join",
	}
	if !reflect.DeepEqual(reply, want) {
		t.Errorf("hq answered %+v, want %+v", reply, want)
	}
}

// TestApprovingWhereNobodyWaitsIsRefused checks that Approve returns an error
// on a node where Config.AskFirst is not set.
func TestApprovingWhereNobodyWaitsIsRefused(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	if err := p.above.binding.Approve(ctx, "printer-1"); err == nil {
		t.Error("Approve returned nil on a node where Config.AskFirst is not set")
	}
}

// TestTheCredentialsAreRenewedBeforeTheyExpire checks that Run sends the join
// request again before the connection credentials expire, that the leafnode
// connection opens again with the new credentials, and that a method
// invocation arrives after that (fractal-nats.md, Provisioning the Link).
func TestTheCredentialsAreRenewedBeforeTheyExpire(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	aboveCfg.CredentialLifetime = 4 * time.Second
	relay := newProxy(t, fmt.Sprintf("127.0.0.1:%d", aboveLeaf))
	aboveCfg.AdvertisedLeafRemote = "nats://" + relay.Addr()
	relay.Open()
	hq := startNode(ctx, t, aboveCfg)

	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
	below := buildNode(ctx, t, belowCfg)
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- below.binding.Run(runCtx) }()

	waitUntil(t, "hq to see printer-1 as connected", func() bool { return len(hq.engine.Connected()) >= 1 })

	// After twice the lifetime the first credentials have expired. If a
	// message still arrives then, the node renewed its credentials.
	past := time.Now().Add(2 * aboveCfg.CredentialLifetime)
	waitUntil(t, "the first credentials to expire", func() bool { return time.Now().After(past) })
	// A renewal closes and opens the leafnode connection, so one check of the
	// state could see it as lost. The test waits until it is connected again.
	waitUntil(t, "the node below to be connected with renewed credentials", func() bool {
		return below.binding.Status().Uplink.State == natsbind.UplinkConnected
	})
	send(t, hq, fractalnode.Dispatch{
		Kind: fractal.KindRPC, Payload: rpcPayload(t, "after"), Child: "printer-1", ID: "after",
	})
	awaitKind(t, below, fractal.KindRPC, 1)
	stop()
	<-done
}

// heldKeys is a key source whose first lookup waits until the test releases
// it, so the join request that asked stays in evaluation.
type heldKeys struct {
	asked   chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *heldKeys) Key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	h.once.Do(func() {
		close(h.asked)
		select {
		case <-h.release:
		case <-ctx.Done():
		}
	})
	return nil, fmt.Errorf("kid %q: %w", kid, joinwire.ErrUnknownKey)
}

// TestAJoinBeyondTheBoundIsDenied: a node above that checks one join request
// at a time has one in evaluation. It denies the next with
// ERR_JOIN_UNAVAILABLE and does not leave it unanswered (fractal-nats.md,
// Bootstrap Account and Join Endpoint).
func TestAJoinBeyondTheBoundIsDenied(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	keys := &heldKeys{asked: make(chan struct{}), release: make(chan struct{})}
	defer close(keys.release)
	hqCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true, mode: natsbind.ModeJWT})
	hqCfg.KeySource = keys
	hqCfg.MaxJoinsInFlight = 1
	startNode(ctx, t, hqCfg)

	// The credential is never verified: the key lookup it asks for holds it.
	unchecked := fractal.JoinCredential(base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"k"}`)) +
		".e30.e30")
	joinCtx, stop := context.WithCancel(ctx)
	defer stop()
	for _, id := range []fractal.NodeID{"printer-1", "printer-2"} {
		cfg, _ := baseConfig(t, nodeOptions{id: id, parent: hqCfg.ClientListen})
		cfg.JoinCredential = unchecked
		n := buildNode(ctx, t, cfg)
		go func() { _ = n.binding.Join(joinCtx) }()
		if id == "printer-1" {
			<-keys.asked
			continue
		}
		waitUntil(t, "hq to deny printer-2 as busy", func() bool {
			return n.binding.Status().Uplink.Code == fractal.CodeJoinUnavailable
		})
	}
}

// TestTwoGrantsForOneNodeBuildOneLink: two join requests of one node that
// hq evaluates at the same time both get a grant. hq builds one link for that
// node, so each report from it arrives once (fractal.md, Uniqueness).
func TestTwoGrantsForOneNodeBuildOneLink(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	key, err := rsa.GenerateKey(rand.Reader, joinwire.MinModulusBits)
	if err != nil {
		t.Fatalf("make a signing key: %v", err)
	}
	keys := &gatedKeys{key: &key.PublicKey, asked: make(chan struct{}, 2), open: make(chan struct{})}
	hqCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true, mode: natsbind.ModeJWT})
	hqCfg.KeySource = keys
	hqCfg.JoinsPerSecond = 1000
	hq := startNode(ctx, t, hqCfg)
	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: hqCfg.ClientListen})
	belowCfg.JoinCredential = joinToken(t, key, "printer-1", "hq")
	below := buildNode(ctx, t, belowCfg)

	var joins sync.WaitGroup
	joined := make(chan error, 2)
	for range 2 {
		joins.Go(func() { joined <- below.binding.Join(ctx) })
	}
	<-keys.asked
	<-keys.asked
	close(keys.open)
	joins.Wait()
	close(joined)
	for joinErr := range joined {
		if joinErr != nil {
			t.Fatalf("Join: %v", joinErr)
		}
	}
	if startErr := below.binding.Start(ctx); startErr != nil {
		t.Fatalf("start printer-1: %v", startErr)
	}
	waitUntil(t, "hq to see printer-1 as connected", func() bool { return len(hq.engine.Connected()) >= 1 })
	for at := range fractal.Timestamp(2) {
		send(t, below, fractalnode.Dispatch{Kind: fractal.KindREP, Payload: repPayload(t, "hq/printer-1", at+1)})
		awaitKind(t, hq, fractal.KindREP, int(at)+1)
	}
}

// gatedKeys is a key source that holds every lookup until the test opens it,
// so the join requests that asked stay in evaluation together.
type gatedKeys struct {
	key   *rsa.PublicKey
	asked chan struct{}
	open  chan struct{}
}

func (g *gatedKeys) Key(ctx context.Context, _ string) (*rsa.PublicKey, error) {
	g.asked <- struct{}{}
	select {
	case <-g.open:
		return g.key, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// joinToken signs a join credential for one Node ID under one parent Node
// Path, valid for an hour (fractal-nats.md, JWT Mode).
func joinToken(t *testing.T, key *rsa.PrivateKey, id fractal.NodeID, parent fractal.NodePath) fractal.JoinCredential {
	t.Helper()
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","kid":"k"}`))
	claims := fmt.Sprintf(`{"nodeId":%q,"parentPath":%q,"exp":%d}`, id, parent, time.Now().Add(time.Hour).Unix())
	input := header + "." + enc.EncodeToString([]byte(claims))
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign the join credential: %v", err)
	}
	return fractal.JoinCredential(input + "." + enc.EncodeToString(sig))
}
