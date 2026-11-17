package natsbind_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/prusa3d/fractal-link/natsbind"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// refusalKind is one answer of a node above that permanently ends the joining
// of the node below.
type refusalKind struct {
	name  string
	reply func(t *testing.T, hq *node) joinwire.Reply
}

// denialFor returns the refusalKind of a denial with one code.
func denialFor(name string, code fractal.Code) refusalKind {
	return refusalKind{name: name, reply: func(*testing.T, *node) joinwire.Reply {
		return joinwire.Deny(fractal.JoinDenial{Version: fractal.Version, Code: code, Reason: "the test denies"})
	}}
}

// grantFor returns a grant from a node above with this identity, for this
// Node Path. Its credentials work nowhere: brno refuses it before it uses
// them.
func grantFor(identity fractal.Identity, path fractal.NodePath) joinwire.Reply {
	return joinwire.Grant(fractal.JoinGrant{
		Version:        fractal.Version,
		NodePath:       path,
		ParentIdentity: identity,
		Credentials:    "never used",
		ExpiresAt:      fractal.TimestampOf(time.Now().Add(time.Hour)),
	}, "nats://127.0.0.1:1", time.Second)
}

// refusalKinds are the denials after which a node MUST NOT retry, and
// ERR_JOIN_UNAUTHORIZED, after which it waits for a new join credential
// (fractal.md, Error Codes). They are also the grants brno, which holds a
// child, refuses by fractal.md, After the Join Exchange, and by
// fractal-files.md, Join Data.
var refusalKinds = []refusalKind{
	denialFor("rejected", fractal.CodeJoinRejected),
	denialFor("collision", fractal.CodeJoinCollision),
	denialFor("revoked", fractal.CodeJoinRevoked),
	denialFor("unauthorized", fractal.CodeJoinUnauthorized),
	{name: "another parent", reply: func(t *testing.T, _ *node) joinwire.Reply {
		return grantFor(otherIdentity(t), "hq/brno")
	}},
	{name: "another path", reply: func(_ *testing.T, hq *node) joinwire.Reply {
		return grantFor(hq.binding.Identity(), "praha/brno")
	}},
	{name: "unsupported version", reply: func(_ *testing.T, hq *node) joinwire.Reply {
		reply := grantFor(hq.binding.Identity(), "hq/brno")
		reply.Version = fractal.Version + 1
		return reply
	}},
	{name: "unusable grant", reply: func(_ *testing.T, hq *node) joinwire.Reply {
		reply := grantFor(hq.binding.Identity(), "hq/brno")
		reply.LeafCredentials = ""
		return reply
	}},
	{name: "refused extension", reply: func(_ *testing.T, hq *node) joinwire.Reply {
		reply := grantFor(hq.binding.Identity(), "hq/brno")
		reply.Extensions = map[string]json.RawMessage{refusedExtension: json.RawMessage(`{}`)}
		return reply
	}},
}

// refusedExtension is an extension brno runs and refuses the join data of.
// hq offers no join data for it, so brno takes the grants of hq.
const refusedExtension = "files"

// refuseExtension is brno's check of the join data: it refuses every entry of
// refusedExtension.
func refuseExtension(data map[fractal.Identifier]json.RawMessage) error {
	if _, ok := data[refusedExtension]; ok {
		return errors.New("brno refuses every entry of this extension")
	}
	return nil
}

// otherIdentity returns the public key of a new NKEY, the identity of a node
// above that brno never joined.
func otherIdentity(t *testing.T) fractal.Identity {
	t.Helper()
	pair, err := nkeys.CreateServer()
	if err != nil {
		t.Fatalf("create an identity: %v", err)
	}
	pub, err := pair.PublicKey()
	if err != nil {
		t.Fatalf("read the public key of an identity: %v", err)
	}
	return fractal.Identity(pub)
}

// The ways a refusal reaches a running node.
const (
	refusedAtStart  = "at start"
	refusedInRenew  = "in a renewal"
	refusedInJoin   = "in a join"
	renewedLifetime = 2 * time.Second
)

// TestARefusedNodeNeverAsksAgain: brno, with printer-1 below it, is
// refused permanently when it starts, in a renewal and in a Join from outside
// Run. It serves printer-1 on, a later Join asks the node above nothing, and
// after a restart it asks nothing either (fractal.md, Error Codes: "MUST NOT
// retry. The denial stands until an operator acts"). A relay in front of the
// leafnode port of hq counts the dials: brno closes its connection to hq and
// dials it no more, also after the restart.
func TestARefusedNodeNeverAsksAgain(t *testing.T) {
	t.Parallel()
	for _, kind := range refusalKinds {
		for _, way := range []string{refusedAtStart, refusedInRenew, refusedInJoin} {
			t.Run(kind.name+" "+way, func(t *testing.T) {
				t.Parallel()
				refusePermanently(t, kind, way)
			})
		}
	}
}

func refusePermanently(t *testing.T, kind refusalKind, way string) {
	ctx := t.Context()
	hqCfg, hqLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	// brno asks again within milliseconds, faster than the default rate allows.
	hqCfg.JoinsPerSecond = 1000
	if way == refusedInRenew {
		hqCfg.CredentialLifetime = renewedLifetime
	}
	toHQ := newProxy(t, fmt.Sprintf("127.0.0.1:%d", hqLeaf))
	toHQ.Open()
	hqCfg.AdvertisedLeafRemote = "nats://" + toHQ.Addr()
	hq := startNode(ctx, t, hqCfg)
	asks := newProxy(t, hqCfg.ClientListen)
	asks.Open()
	brnoCfg, _ := baseConfig(t, nodeOptions{id: "brno", parent: asks.Addr(), accepts: true, dataDir: t.TempDir()})
	brnoCfg.JoinExtensions = []fractal.Identifier{refusedExtension}
	brnoCfg.CheckExtensions = refuseExtension
	brno := buildNode(ctx, t, brnoCfg)
	stopBrno := runInBackground(ctx, t, brno)
	printerCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: brnoCfg.ClientListen})
	printer := startNode(ctx, t, printerCfg)
	waitUntil(t, "printer-1 to link to brno", func() bool { return len(brno.engine.Connected()) == 1 })
	fake := newFakeParent(t, kind.reply(t, hq))

	asks.Retarget(fake.Addr())
	switch way {
	case refusedAtStart:
		if err := stopBrno(); err != nil {
			t.Fatalf("the first Run of brno: %v", err)
		}
		brno = buildNode(ctx, t, brnoCfg)
		stopBrno = runInBackground(ctx, t, brno)
	case refusedInJoin:
		if err := brno.binding.Join(ctx); err == nil {
			t.Fatal("Join took the answer of the fake node above")
		}
	}
	waitUntil(t, "brno to be refused", func() bool {
		return brno.binding.Status().Uplink.State == natsbind.UplinkDenied
	})
	waitUntil(t, "brno's leafnode connection to hq to close", func() bool { return toHQ.Live() == 0 })
	dialed := toHQ.Accepted()
	waitUntil(t, "printer-1 to link to brno", func() bool { return len(brno.engine.Connected()) >= 1 })
	send(t, printer, fractalnode.Dispatch{Kind: fractal.KindREP, Payload: repPayload(t, "hq/brno/printer-1", 1)})
	awaitKind(t, brno, fractal.KindREP, 1)
	refused := uplinkOf(brno)
	joinErr := brno.binding.Join(ctx)
	runErr := stopBrno()

	again := buildNode(ctx, t, brnoCfg)
	stopAgain := runInBackground(ctx, t, again)
	// Run joins before it starts the NATS server, so a request would have
	// been sent before printer-1 can link.
	waitUntil(t, "printer-1 to link to brno again", func() bool { return len(again.engine.Connected()) == 1 })
	// A dial proves itself only by happening, so the test watches three of
	// the NATS server's leafnode redial intervals for one.
	<-time.After(3 * server.DEFAULT_LEAF_NODE_RECONNECT)
	got := map[string]any{
		"asked": fake.Asked(), "join refused": joinErr != nil, "run": runErr,
		"uplink after the restart": uplinkOf(again), "dials to hq": toHQ.Accepted() - dialed,
	}
	want := map[string]any{
		"asked": int32(1), "join refused": true, "run": nil,
		"uplink after the restart": refused, "dials to hq": 0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the refusal brno has %v, want %v", got, want)
	}
	if err := stopAgain(); err != nil {
		t.Errorf("Run after the restart: %v", err)
	}
}

// TestAJoinThatWaitsTakesTheDenial: a Join that waits while another Join is
// denied permanently returns that denial at once. It sends no request and
// does not wait for its context to end (fractal.md, Error Codes: "MUST NOT
// retry").
func TestAJoinThatWaitsTakesTheDenial(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fake := newFakeParent(t, joinwire.Deny(fractal.JoinDenial{
		Version: fractal.Version, Code: fractal.CodeJoinRevoked, Reason: "the test revokes",
	}))
	asked, answer := make(chan struct{}), make(chan struct{})
	hold := func() {
		close(asked)
		<-answer
	}
	fake.onAsk.Store(&hold)
	cfg, _ := baseConfig(t, nodeOptions{id: "brno", parent: fake.Addr(), dataDir: t.TempDir()})
	brno := buildNode(ctx, t, cfg)

	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- brno.binding.Join(ctx) }()
	<-asked
	waitCtx, cancel := context.WithTimeout(ctx, testWait)
	defer cancel()
	go func() { second <- brno.binding.Join(waitCtx) }()
	awaitJoins(t, 2)
	close(answer)

	firstErr, secondErr := <-first, <-second
	got := map[string]any{
		"first": codeOf(firstErr), "second": codeOf(secondErr), "second in time": waitCtx.Err() == nil,
		"asked": fake.Asked(),
	}
	want := map[string]any{
		"first": fractal.CodeJoinRevoked, "second": fractal.CodeJoinRevoked, "second in time": true,
		"asked": int32(1),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the two joins of brno ended with %v, want %v", got, want)
	}
}

// awaitJoins waits until want goroutines that the calling test started run
// join.Joiner.Join. Neither the binding nor the joiner shows a Join that waits
// for its turn, so the test reads it from the goroutine stacks.
func awaitJoins(t *testing.T, want int) {
	t.Helper()
	waitUntil(t, fmt.Sprintf("%d joins to run", want), func() bool {
		buffer := make([]byte, 1<<22)
		running := 0
		for stack := range strings.SplitSeq(string(buffer[:runtime.Stack(buffer, true)]), "\n\n") {
			if strings.Contains(stack, "join.(*Joiner).Join(") && strings.Contains(stack, t.Name()) {
				running++
			}
		}
		return running >= want
	})
}

// codeOf returns the denial code of a join error, or 0 for another error.
func codeOf(err error) fractal.Code {
	if denial, ok := errors.AsType[*join.DenialError](err); ok {
		return denial.Denial.Code
	}
	return 0
}

// TestAPermanentRefusalIsCleared: a node denied with ERR_JOIN_UNAUTHORIZED asks
// again once it starts with a new join credential (fractal.md, Error Codes:
// "SHOULD retry once a valid join credential has been obtained"). It also asks
// again once an operator deleted refusal.json from its data directory.
func TestAPermanentRefusalIsCleared(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fake := newFakeParent(t, denialFor("unauthorized", fractal.CodeJoinUnauthorized).reply(t, nil))
	cfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: fake.Addr(), dataDir: t.TempDir()})
	cfg.JoinCredential = "first"
	asked := func(credential fractal.JoinCredential) int32 {
		cfg.JoinCredential = credential
		waiting := newLogWatch(refusedLog)
		cfg.Logger = slog.New(waiting)
		n := buildNode(ctx, t, cfg)
		stop := runInBackground(ctx, t, n)
		awaitLog(t, waiting)
		if err := stop(); err != nil {
			t.Errorf("Run with the join credential %q: %v", credential, err)
		}
		return fake.Asked()
	}

	got := []int32{asked("first"), asked("first"), asked("second"), asked("second")}
	if err := os.Remove(filepath.Join(cfg.DataDir, "refusal.json")); err != nil {
		t.Fatalf("clear the refusal: %v", err)
	}
	got = append(got, asked("second"))
	if want := []int32{1, 1, 2, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("the requests after each start are %v, want %v", got, want)
	}
}

// TestARefusalReplacesItsFileWhole: brno writes refusal.json to a new file and
// renames it into place, so after a power loss the file holds the whole
// refusal or the old content. Just before the node above answers, the test
// makes refusal.json a second name of a witness file. A write into the
// existing file would change the witness.
func TestARefusalReplacesItsFileWhole(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fake := newFakeParent(t, denialFor("rejected", fractal.CodeJoinRejected).reply(t, nil))
	cfg, _ := baseConfig(t, nodeOptions{id: "brno", parent: fake.Addr(), dataDir: t.TempDir()})
	witness := filepath.Join(cfg.DataDir, "witness")
	if err := os.WriteFile(witness, []byte("untouched"), 0o600); err != nil {
		t.Fatalf("write the witness: %v", err)
	}
	linked := make(chan error, 1)
	link := func() {
		select {
		case linked <- os.Link(witness, filepath.Join(cfg.DataDir, "refusal.json")):
		default:
		}
	}
	fake.onAsk.Store(&link)
	refused := newLogWatch(refusedLog)
	cfg.Logger = slog.New(refused)
	brno := buildNode(ctx, t, cfg)
	stop := runInBackground(ctx, t, brno)
	awaitLog(t, refused)
	before := uplinkOf(brno)
	runErr := stop()
	content, readErr := os.ReadFile(witness)

	got := map[string]any{
		"link": <-linked, "run": runErr, "witness": string(content), "read": readErr,
		"uplink after a restart": uplinkOf(buildNode(ctx, t, cfg)),
	}
	want := map[string]any{
		"link": nil, "run": nil, "witness": "untouched", "read": nil,
		"uplink after a restart": before,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the refusal brno has %v, want %v", got, want)
	}
}

// TestAnUnreadableRefusalStillRefuses: a refusal.json that is empty or torn,
// as a power loss can leave it, or that cannot be read at all, keeps brno
// refused. brno logs the error, asks the node above nothing and serves
// printer-1 (fractal.md, Error Codes: "The denial stands until an operator
// acts").
func TestAnUnreadableRefusalStillRefuses(t *testing.T) {
	t.Parallel()
	for name, spoil := range map[string]func(path string) error{
		"empty":       func(path string) error { return os.WriteFile(path, nil, 0o600) },
		"torn":        func(path string) error { return os.WriteFile(path, []byte(`{"code":`), 0o600) },
		"a directory": func(path string) error { return os.Mkdir(path, 0o700) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			hqCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
			startNode(ctx, t, hqCfg)
			asks := newProxy(t, hqCfg.ClientListen)
			asks.Open()
			cfg, _ := baseConfig(t, nodeOptions{id: "brno", parent: asks.Addr(), accepts: true, dataDir: t.TempDir()})
			brno := buildNode(ctx, t, cfg)
			stop := runInBackground(ctx, t, brno)
			printerCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: cfg.ClientListen})
			startNode(ctx, t, printerCfg)
			waitUntil(t, "printer-1 to link to brno", func() bool { return len(brno.engine.Connected()) == 1 })
			if err := stop(); err != nil {
				t.Fatalf("the first Run of brno: %v", err)
			}
			fake := newFakeParent(t, denialFor("rejected", fractal.CodeJoinRejected).reply(t, nil))
			asks.Retarget(fake.Addr())
			if err := spoil(filepath.Join(cfg.DataDir, "refusal.json")); err != nil {
				t.Fatalf("spoil the refusal: %v", err)
			}

			unreadable, refused := newLogWatch(unreadableRefusalLog), newLogWatch(refusedLog)
			cfg.Logger = slog.New(slog.NewMultiHandler(unreadable, refused))
			brno = buildNode(ctx, t, cfg)
			stop = runInBackground(ctx, t, brno)
			awaitLog(t, unreadable)
			awaitLog(t, refused)
			waitUntil(t, "printer-1 to link to brno again", func() bool { return len(brno.engine.Connected()) == 1 })

			uplink := brno.binding.Status().Uplink
			got := map[string]any{
				"asked": fake.Asked(), "state": uplink.State, "unreadable": uplink.Unreadable, "run": stop(),
			}
			want := map[string]any{"asked": int32(0), "state": natsbind.UplinkDenied, "unreadable": true, "run": nil}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("brno has %v, want %v", got, want)
			}
		})
	}
}

// unreadableRefusalLog is the error a node logs when it cannot read
// refusal.json.
const unreadableRefusalLog = "the saved refusal cannot be read; this node stays refused until an operator deletes it"

// refusedLog is the message a node logs when it asks the node above nothing
// more.
const refusedLog = "this node asks the node above nothing more"

// uplinkOf returns the Status.Uplink of a node without the time it last
// connected.
func uplinkOf(n *node) natsbind.Uplink {
	uplink := n.binding.Status().Uplink
	uplink.ConnectedAt = nil
	return uplink
}

// runInBackground calls Run on a node. The function it returns stops Run and
// returns its error, or nil when Run ended because it was stopped.
func runInBackground(ctx context.Context, t *testing.T, n *node) func() error {
	t.Helper()
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- n.binding.Run(runCtx) }()
	stopped := sync.OnceValue(func() error {
		select {
		case err := <-done:
			cancel()
			return fmt.Errorf("Run returned before it was stopped: %w", err)
		default:
		}
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	})
	t.Cleanup(func() { _ = stopped() })
	return stopped
}

// fakeParent is a NATS server that answers every join request with one reply
// and counts the requests. A test that set onAsk runs it before each answer.
type fakeParent struct {
	addr  string
	asked atomic.Int32
	onAsk atomic.Pointer[func()]
}

func newFakeParent(t *testing.T, reply joinwire.Reply) *fakeParent {
	t.Helper()
	srv, err := server.NewServer(&server.Options{
		Host: "127.0.0.1", Port: server.RANDOM_PORT, NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatalf("build the fake node above: %v", err)
	}
	go srv.Start()
	t.Cleanup(func() {
		srv.Shutdown()
		srv.WaitForShutdown()
	})
	if !srv.ReadyForConnections(testWait) {
		t.Fatal("the fake node above did not start")
	}
	payload, err := joinwire.EncodeReply(reply)
	if err != nil {
		t.Fatalf("encode the answer of the fake node above: %v", err)
	}
	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect to the fake node above: %v", err)
	}
	t.Cleanup(conn.Close)
	f := &fakeParent{addr: srv.Addr().String()}
	if _, subErr := conn.Subscribe(joinwire.SubjectJoin, func(msg *nats.Msg) {
		f.asked.Add(1)
		if onAsk := f.onAsk.Load(); onAsk != nil {
			(*onAsk)()
		}
		_ = msg.Respond(payload) // The node below counts as asked whether or not the answer reaches it.
	}); subErr != nil {
		t.Fatalf("subscribe the fake node above: %v", subErr)
	}
	if flushErr := conn.Flush(); flushErr != nil {
		t.Fatalf("flush the fake node above: %v", flushErr)
	}
	return f
}

// Addr returns the client address of the fake node above.
func (f *fakeParent) Addr() string { return f.addr }

// Asked returns the number of join requests the fake node above answered.
func (f *fakeParent) Asked() int32 { return f.asked.Load() }

// logWatch is a slog.Handler that closes seen when a record with one message
// arrives.
type logWatch struct {
	msg  string
	once sync.Once
	seen chan struct{}
}

func newLogWatch(msg string) *logWatch { return &logWatch{msg: msg, seen: make(chan struct{})} }

func (w *logWatch) Enabled(context.Context, slog.Level) bool { return true }

func (w *logWatch) Handle(_ context.Context, r slog.Record) error {
	if r.Message == w.msg {
		w.once.Do(func() { close(w.seen) })
	}
	return nil
}

func (w *logWatch) WithAttrs([]slog.Attr) slog.Handler { return w }

func (w *logWatch) WithGroup(string) slog.Handler { return w }

// awaitLog waits until a logWatch saw its message.
func awaitLog(t *testing.T, w *logWatch) {
	t.Helper()
	select {
	case <-w.seen:
	case <-time.After(testWait):
		t.Fatalf("waited for the log message %q and it did not come", w.msg)
	}
}
