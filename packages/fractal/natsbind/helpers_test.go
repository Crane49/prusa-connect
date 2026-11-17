package natsbind_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/internal/testport"
	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
)

// The intervals and timeouts of the tests in this package. They are short
// because every NATS server runs in this process on the loopback interface.
const (
	testPoll        = 20 * time.Millisecond
	testWait        = 30 * time.Second
	testAckWait     = time.Second
	testDedupWindow = time.Minute
	testHeartbeat   = 100 * time.Millisecond
	testMaxBeat     = 200 * time.Millisecond
)

// node is one Binding under test. engine is the recorder that receives the
// messages and the connect and loss calls of the Binding.
type node struct {
	binding *natsbind.Binding
	engine  *fractaltest.Recorder
	client  string
	leaf    string
}

// nodeOptions has the configuration values a test sets for one node.
type nodeOptions struct {
	id        fractal.NodeID
	parent    string
	accepts   bool
	advertise string
	askFirst  bool
	mode      natsbind.Mode
	dataDir   string
}

// baseConfig returns a Config with free ports, a temporary data directory and
// the short test intervals. It also returns the leafnode port.
func baseConfig(t testing.TB, opts nodeOptions) (natsbind.Config, int) {
	t.Helper()
	clientPort, leafPort := testport.Free(t), testport.Free(t)
	dir := opts.dataDir
	if dir == "" {
		dir = t.TempDir()
	}
	cfg := natsbind.Config{
		NodeID:               opts.id,
		ParentAddr:           opts.parent,
		AcceptsChildren:      opts.accepts,
		AdvertisedLeafRemote: opts.advertise,
		AskFirst:             opts.askFirst,
		Verification:         opts.mode,
		ClientListen:         fmt.Sprintf("127.0.0.1:%d", clientPort),
		LeafListen:           fmt.Sprintf("127.0.0.1:%d", leafPort),
		DataDir:              dir,
		DedupWindow:          testDedupWindow,
		AckWait:              testAckWait,
		HeartbeatInterval:    testHeartbeat,
		MaxHeartbeatInterval: testMaxBeat,
		PollInterval:         testPoll,
	}
	if opts.accepts && cfg.AdvertisedLeafRemote == "" {
		cfg.AdvertisedLeafRemote = fmt.Sprintf("nats://127.0.0.1:%d", leafPort)
	}
	return cfg, leafPort
}

// buildNode calls natsbind.New and does not start the node. It closes the
// Binding when the test ends.
func buildNode(ctx context.Context, t testing.TB, cfg natsbind.Config) *node {
	t.Helper()
	engine := fractaltest.NewRecorder(true)
	binding, err := natsbind.New(ctx, cfg, engine)
	if err != nil {
		t.Fatalf("build %s: %v", cfg.NodeID, err)
	}
	t.Cleanup(func() {
		if closeErr := binding.Close(); closeErr != nil {
			t.Errorf("stop %s: %v", cfg.NodeID, closeErr)
		}
	})
	return &node{binding: binding, engine: engine, client: cfg.ClientListen, leaf: cfg.LeafListen}
}

// startNode builds one node, calls Join, which does nothing on a root node,
// and calls Start.
func startNode(ctx context.Context, t testing.TB, cfg natsbind.Config) *node {
	t.Helper()
	built := buildNode(ctx, t, cfg)
	if joinErr := built.binding.Join(ctx); joinErr != nil {
		t.Fatalf("join %s: %v", cfg.NodeID, joinErr)
	}
	if startErr := built.binding.Start(ctx); startErr != nil {
		t.Fatalf("start %s: %v", cfg.NodeID, startErr)
	}
	return built
}

// proxy is a TCP relay in front of one leafnode listener. A test calls Cut to
// disconnect a node and keep it disconnected. Closing the connection alone is
// not enough, because the NATS server connects again at once.
type proxy struct {
	target string

	mu       sync.Mutex
	listener net.Listener
	conns    []net.Conn
	addr     string
	open     bool
	accepted int
	live     int
	refused  int
	closed   bool
	held     chan struct{}
	renames  [][2]string
	wg       sync.WaitGroup
}

// newProxy starts a relay on a free port in front of target. It refuses every
// connection until the test calls Open.
func newProxy(t testing.TB, target string) *proxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start the relay: %v", err)
	}
	p := &proxy{target: target, listener: listener, addr: listener.Addr().String()}
	p.wg.Add(1)
	go p.accept()
	t.Cleanup(p.stop)
	return p
}

// Addr returns the address the relay listens on.
func (p *proxy) Addr() string { return p.addr }

// Retarget relays the connections accepted from now on to target.
func (p *proxy) Retarget(target string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.target = target
}

// accept accepts connections. It relays a connection that arrives while the
// relay is open and closes every other one.
func (p *proxy) accept() {
	defer p.wg.Done()
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		allowed := p.open
		if allowed {
			p.conns = append(p.conns, conn)
			p.accepted++
		} else {
			p.refused++
		}
		p.mu.Unlock()
		if !allowed {
			_ = conn.Close() // The relay is not open, so it refuses the connection.
			continue
		}
		p.wg.Go(func() {
			p.relay(conn)
		})
	}
}

// relay copies bytes in both directions between one accepted connection and
// the target. While the relay holds, it passes on only what the node below
// sends up to its CONNECT, until the hold is released or the connection
// closes. It renames subjects in what the node below sends.
func (p *proxy) relay(client net.Conn) {
	p.mu.Lock()
	p.live++
	target := p.target
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.live--
		p.mu.Unlock()
	}()
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		_ = client.Close() // The target cannot be reached, so the relay closes the connection.
		return
	}
	p.mu.Lock()
	p.conns = append(p.conns, upstream)
	held, renames := p.held, p.renames
	p.mu.Unlock()
	downDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		defer close(downDone)
		_, _ = io.Copy(client, upstream) // A copy error means the connection closed, which the test expects.
		_ = client.Close()
		_ = upstream.Close()
	})
	wg.Go(func() {
		upward := io.Reader(client)
		switch {
		case held != nil:
			upward = holdAfterConnect(client, upstream, held, downDone)
		case len(renames) > 0:
			renameFrames(bufio.NewReader(client), upstream, renames)
		}
		_, _ = io.Copy(upstream, upward) // A copy error means the connection closed, which the test expects.
		_ = upstream.Close()
		_ = client.Close()
	})
	wg.Wait()
}

// holdAfterConnect passes src on to dst up to and including the CONNECT of
// the node below. The reader it returns gives the rest once held is closed,
// so the node above accepts the connection and hears nothing more until then.
// It returns an empty reader when closed is closed first.
func holdAfterConnect(src io.Reader, dst io.Writer, held, closed <-chan struct{}) io.Reader {
	buffered := bufio.NewReader(src)
	if !passConnect(buffered, dst) {
		return strings.NewReader("")
	}
	select {
	case <-held:
		return buffered
	case <-closed:
		return strings.NewReader("")
	}
}

// passConnect copies what src sends up to and including the S2 frame that
// holds the CONNECT to dst. It reports whether it found it.
func passConnect(src *bufio.Reader, dst io.Writer) bool {
	return copyFrames(src, dst, func(_, body []byte) bool { return !bytes.Contains(body, []byte("CONNECT ")) })
}

// renameFrames copies src to dst until either side closes. In each frame
// that is not compressed it replaces each subject with another of the same
// length and puts the new checksum in front of the data.
func renameFrames(src *bufio.Reader, dst io.Writer, renames [][2]string) {
	const uncompressedChunk, checksumLen = 0x01, 4
	castagnoli := crc32.MakeTable(crc32.Castagnoli)
	copyFrames(src, dst, func(head, body []byte) bool {
		if head[0] != uncompressedChunk || len(body) < checksumLen {
			return true
		}
		data := body[checksumLen:]
		for _, r := range renames {
			copy(data, bytes.ReplaceAll(data, []byte(r[0]), []byte(r[1])))
		}
		sum := crc32.Checksum(data, castagnoli)
		binary.LittleEndian.PutUint32(body, (sum>>15|sum<<17)+0xa282ead8)
		return true
	})
}

// copyFrames copies the INFO line by which the node below agrees on
// compression and the S2 stream header from src to dst. It then reads one S2
// frame at a time, lets each change it and copies it. A frame starts with its
// type and its length in three bytes, least significant first; data that is
// not compressed follows a masked CRC-32C of it. copyFrames stops after the
// frame for which each returns false, and then returns true. It returns false
// on an error. On the loopback interface the frames are not compressed, so
// the commands of the node below show in them.
func copyFrames(src *bufio.Reader, dst io.Writer, each func(head, body []byte) bool) bool {
	const streamHeaderLen, frameHeaderLen = 10, 4
	line, err := src.ReadString('\n')
	if err != nil || pass(dst, []byte(line)) != nil {
		return false
	}
	header := make([]byte, streamHeaderLen)
	if _, err = io.ReadFull(src, header); err != nil || pass(dst, header) != nil {
		return false
	}
	for {
		head := make([]byte, frameHeaderLen)
		if _, err = io.ReadFull(src, head); err != nil {
			return false
		}
		body := make([]byte, int(head[1])|int(head[2])<<8|int(head[3])<<16)
		if _, err = io.ReadFull(src, body); err != nil {
			return false
		}
		more := each(head, body)
		if pass(dst, head) != nil || pass(dst, body) != nil {
			return false
		}
		if !more {
			return true
		}
	}
}

// pass writes all of b to dst.
func pass(dst io.Writer, b []byte) error {
	_, err := dst.Write(b)
	return err
}

// Rename makes the relay replace each subject from with to, which has the
// same length, in what each new connection from below sends.
func (p *proxy) Rename(from, to string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.renames = append(p.renames, [2]string{from, to})
}

// HoldAfterConnect makes the relay pass on only the CONNECT of each new
// connection from below until the returned function is called.
func (p *proxy) HoldAfterConnect() (release func()) {
	held := make(chan struct{})
	p.mu.Lock()
	p.held = held
	p.mu.Unlock()
	return sync.OnceFunc(func() { close(held) })
}

// Open makes the relay accept connections.
func (p *proxy) Open() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.open = true
}

// Accepted returns the number of connections the relay passed on.
func (p *proxy) Accepted() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.accepted
}

// Live returns the number of relayed connections that are still open.
func (p *proxy) Live() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.live
}

// Refused returns the number of connections the relay refused while it was
// not open.
func (p *proxy) Refused() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refused
}

// Cut closes every open connection of the relay and refuses new ones.
func (p *proxy) Cut() {
	p.mu.Lock()
	p.open = false
	conns := p.conns
	p.conns = nil
	p.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close() // The test wants the connection closed, so a Close error does not matter.
	}
}

// stop closes the listener and every connection, and waits for the
// goroutines.
func (p *proxy) stop() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	listener, conns := p.listener, p.conns
	p.conns = nil
	p.mu.Unlock()
	_ = listener.Close() // The test is over.
	for _, conn := range conns {
		_ = conn.Close() // The test is over.
	}
	p.wg.Wait()
}

// waitUntil polls done until it returns true. After testWait it fails the test
// and names the condition in what.
func waitUntil(t testing.TB, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(testWait)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(testPoll)
	}
	t.Fatalf("waited for %s and it did not happen", what)
}
