package xbuddy

import (
	"net"
	"net/http"
	"runtime/metrics"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The fleet the ping interval is chosen for. Four hundred printers is the size
// this product is built to hold on one host, and the keepalive is the only
// traffic an idle one of them makes.
const (
	fleetSize   = 400
	fleetWindow = 3 * time.Second
	// fleetInterval is turned down from the shipping 54s so a window short
	// enough to run measures tens of thousands of exchanges rather than none.
	// What is measured is the cost of one exchange, which the interval does not
	// change; the interval is what multiplies it.
	fleetInterval = 25 * time.Millisecond
	fleetSettle   = time.Second
)

// The CPU classes that price the keepalive. Total is the capacity the process
// had, not what it used, so busy is total less idle.
const (
	cpuTotalMetric = "/cpu/classes/total:cpu-seconds"
	cpuIdleMetric  = "/cpu/classes/idle:cpu-seconds"
)

// counted is a listener whose connections report how many bytes crossed them.
type counted struct {
	net.Listener

	bytes *atomic.Int64
}

func (c counted) Accept() (net.Conn, error) {
	conn, err := c.Listener.Accept()
	if err != nil {
		return nil, err //nolint:wrapcheck // a listener's error is the caller's own.
	}

	return countedConn{Conn: conn, bytes: c.bytes}, nil
}

type countedConn struct {
	net.Conn

	bytes *atomic.Int64
}

func (c countedConn) Read(p []byte) (int, error) {
	read, err := c.Conn.Read(p)
	c.bytes.Add(int64(read))

	return read, err //nolint:wrapcheck // a connection's error is the caller's own.
}

func (c countedConn) Write(p []byte) (int, error) {
	written, err := c.Conn.Write(p)
	c.bytes.Add(int64(written))

	return written, err //nolint:wrapcheck // a connection's error is the caller's own.
}

// What one printer's keepalive costs, so that shortening the interval can be
// priced rather than guessed.
//
// It measures one ping and the pong that answers it, over real sockets, with a
// fleet's worth of sessions running: bytes across the connection and CPU the
// process spent. The interval is what multiplies that cost, so the numbers here
// price every interval an operator might write.
//
//	go test -run '^$' -bench BenchmarkOneKeepaliveExchange -benchtime 1x ./internal/connector/xbuddy/
func BenchmarkOneKeepaliveExchange(b *testing.B) {
	for range b.N {
		measureFleet(b)
	}
}

//nolint:funlen // one measurement: stand a fleet up, let it settle, take two samples.
func measureFleet(b *testing.B) {
	b.Helper()

	bytes := &atomic.Int64{}

	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("listening: %v", err)
	}

	var (
		upgrader websocket.Upgrader
		mu       sync.Mutex
		sessions []*Session
	)

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, upgradeErr := upgrader.Upgrade(w, r, nil)
			if upgradeErr != nil {
				return
			}

			session := newSession(settings{
				identity:   connectorapi.Identity{Vendor: Vendor, Model: "MK4"},
				caps:       baseline(),
				storage:    defaultStorage,
				queueDepth: 1,
				live: Liveness{
					ReadDeadline: 10 * fleetInterval,
					PingInterval: fleetInterval,
					PingTimeout:  fleetInterval,
				},
			})

			mu.Lock()
			sessions = append(sessions, session)
			mu.Unlock()

			if !session.attach(r.Context(), conn) {
				_ = conn.Close()
			}
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() { _ = server.Serve(counted{Listener: base, bytes: bytes}) }()

	clients := dialFleet(b, base.Addr().String())

	defer func() {
		for _, client := range clients {
			_ = client.Close()
		}

		mu.Lock()
		for _, session := range sessions {
			_ = session.Close()
		}
		mu.Unlock()

		_ = server.Close()
	}()

	// The handshakes are kilobytes each and are not what an idle fleet costs.
	time.Sleep(fleetSettle)

	before, beforeCPU := bytes.Load(), processCPU(b)

	time.Sleep(fleetWindow)

	after, afterCPU := bytes.Load(), processCPU(b)

	exchanges := float64(fleetSize) * float64(fleetWindow) / float64(fleetInterval)

	b.ReportMetric(float64(after-before)/exchanges, "wire-bytes/exchange")
	b.ReportMetric((afterCPU-beforeCPU)*1e9/exchanges, "cpu-ns/exchange")
	b.ReportMetric(exchanges/fleetWindow.Seconds(), "exchanges/s")
	b.ReportMetric(float64(len(clients)), "printers")
}

// dialFleet opens one connection per printer, each answering pings the way a
// printer with nothing to say does.
func dialFleet(b *testing.B, addr string) []*websocket.Conn {
	b.Helper()

	clients := make([]*websocket.Conn, 0, fleetSize)

	for range fleetSize {
		client, resp, err := websocket.DefaultDialer.Dial("ws://"+addr, nil)
		if err != nil {
			b.Fatalf("dialling printer %d of %d: %v", len(clients)+1, fleetSize, err)
		}

		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}

		clients = append(clients, client)

		go func() {
			for {
				if _, _, readErr := client.ReadMessage(); readErr != nil {
					return
				}
			}
		}()
	}

	return clients
}

// processCPU is the CPU seconds this process has actually spent, from the
// runtime rather than from a platform call.
//
// It carries the whole benchmark: the four hundred client goroutines answering
// pings are in this process too, so the figure is both halves of every exchange
// and is therefore an upper bound on what a host serving real printers pays.
func processCPU(b *testing.B) float64 {
	b.Helper()

	sample := []metrics.Sample{{Name: cpuTotalMetric}, {Name: cpuIdleMetric}}
	metrics.Read(sample)

	for _, taken := range sample {
		if taken.Value.Kind() != metrics.KindFloat64 {
			b.Fatalf("%s is not readable on this build, so the CPU cost is not measured", taken.Name)
		}
	}

	return sample[0].Value.Float64() - sample[1].Value.Float64()
}
