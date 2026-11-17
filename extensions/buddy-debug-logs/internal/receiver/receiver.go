// Package receiver listens on the UDP ports the operator granted and keeps
// what arrives.
//
// One goroutine reads each port. A datagram is parsed, matched to a printer
// and appended to its stream. Nothing a sender writes can stop the loop: an
// unreadable datagram is kept raw and counted, and a write the disk refuses
// is counted as dropped. A datagram larger than datagram.MaxBytes is
// unreadable.
package receiver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/buddy-debug-logs/internal/datagram"
	"github.com/prusa3d/buddy-debug-logs/internal/sources"
	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

// maxRawBytes bounds what is kept of an unreadable datagram. It is evidence
// that a sender speaks another format, and its first bytes show which.
const maxRawBytes = 512

// Counts is what one stream received.
type Counts struct {
	// Received is every datagram that arrived on the port.
	Received int64 `json:"received"`
	// Unreadable is a datagram in neither format, kept raw.
	Unreadable int64 `json:"unreadable"`
	// Records is every line and every metric point written.
	Records int64 `json:"records"`
	// Dropped is a record the disk refused.
	Dropped int64 `json:"dropped"`
}

// Listener is one port, as the status reports it.
type Listener struct {
	// Granted is whether the operator granted the port. A module installed
	// without the port approval runs without the listener.
	Granted bool   `json:"granted"`
	Network string `json:"network,omitempty"`
	Port    int    `json:"port,omitempty"`
	// Listening is whether the socket is open now.
	Listening bool `json:"listening"`
	// Error is why the socket could not be opened, such as a port another
	// program holds.
	Error string `json:"error,omitempty"`
}

// counters is one stream's counts, written by its reading goroutine and read
// by the status route.
type counters struct {
	received, unreadable, records, dropped atomic.Int64
	lastDrop                               atomic.Pointer[string]
	// lastAt is when the newest datagram arrived, in Unix nanoseconds, and
	// zero before the first one.
	lastAt atomic.Int64
}

// Receiver is every port and what came in on it.
type Receiver struct {
	store *store.Store
	table *sources.Table
	now   func() time.Time

	counts map[store.Stream]*counters

	mu        sync.Mutex
	listeners map[store.Stream]Listener
}

// New builds a receiver that keeps what it reads in held and matches it with
// table.
func New(held *store.Store, table *sources.Table, now func() time.Time) *Receiver {
	r := &Receiver{
		store:     held,
		table:     table,
		now:       now,
		counts:    map[store.Stream]*counters{},
		listeners: map[store.Stream]Listener{},
	}

	for _, stream := range store.Streams() {
		r.counts[stream] = &counters{}
		r.listeners[stream] = Listener{}
	}

	return r
}

// Listen opens the port of one stream and reads it until ctx ends. It returns
// once the socket is open or has failed, and the reading goes on in a
// goroutine that ends with ctx. done is closed when that goroutine has ended.
func (r *Receiver) Listen(ctx context.Context, stream store.Stream, network string, port int) (<-chan struct{}, error) {
	held := Listener{Granted: true, Network: network, Port: port}

	var config net.ListenConfig

	conn, err := config.ListenPacket(ctx, network, net.JoinHostPort("", strconv.Itoa(port)))
	if err != nil {
		held.Error = err.Error()
		r.set(stream, held)

		return nil, fmt.Errorf("listen on %s/%d: %w", network, port, err)
	}

	// Port 0 asks the system for a free port, which a test does. The status
	// reports the port the socket holds.
	if udp, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		held.Port = udp.Port
	}

	held.Listening = true
	r.set(stream, held)

	done := make(chan struct{})

	go func() {
		<-ctx.Done()
		_ = conn.Close() // the read loop below returns on the closed socket
	}()

	go func() {
		defer close(done)

		r.read(stream, conn)

		held.Listening = false
		r.set(stream, held)
	}()

	return done, nil
}

// read handles datagrams until the socket is closed.
func (r *Receiver) read(stream store.Stream, conn net.PacketConn) {
	buffer := make([]byte, datagram.MaxBytes+1)

	for {
		n, from, err := conn.ReadFrom(buffer)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}

			continue
		}

		r.handleSafely(stream, hostOf(from), buffer[:n])
	}
}

// handleSafely handles one datagram, and counts one that made the handling
// panic as unreadable. A sender then cannot end the loop with a datagram that
// meets a defect.
func (r *Receiver) handleSafely(stream store.Stream, ip string, raw []byte) {
	defer func() {
		if recover() != nil {
			r.counts[stream].unreadable.Add(1)
		}
	}()

	r.Handle(stream, ip, raw)
}

// Handle keeps one datagram that arrived on stream's port from the address
// ip.
func (r *Receiver) Handle(stream store.Stream, ip string, raw []byte) {
	held := r.counts[stream]
	held.received.Add(1)

	at := r.now().UTC()
	held.lastAt.Store(at.UnixNano())

	parsed, err := datagram.Parse(raw)
	if err != nil {
		held.unreadable.Add(1)

		r.append(stream, store.Record{At: at, IP: ip, Raw: datagram.Clean(string(raw[:min(len(raw), maxRawBytes)]))})

		return
	}

	kind := store.Syslog
	if parsed.Kind == datagram.KindMetrics {
		kind = store.Metrics
	}

	device := r.table.Resolve(parsed.MAC, ip, kind == store.Syslog)
	base := store.Record{At: at, Device: device, MAC: parsed.MAC, IP: ip, Seq: parsed.Seq, TmUS: parsed.TmUS}

	if kind == store.Syslog {
		severity := int(parsed.Severity)
		record := base
		record.Severity = &severity
		record.Component = parsed.Component
		record.Text = parsed.Text

		r.append(kind, record)

		return
	}

	for _, point := range parsed.Points {
		record := base
		record.Line = point.Line
		record.Name = point.Name
		record.Tags = point.Tags
		record.Fields = point.Fields
		record.PointUS = parsed.TmUS + point.OffsetUS

		r.append(kind, record)
	}
}

// append writes one record and counts it. The record goes to the stream its
// content belongs to, which a printer configured with the ports swapped
// would otherwise mix up.
func (r *Receiver) append(stream store.Stream, record store.Record) {
	held := r.counts[stream]

	if err := r.store.Append(stream, record); err != nil {
		held.dropped.Add(1)

		reason := err.Error()
		held.lastDrop.Store(&reason)

		return
	}

	held.records.Add(1)
}

// Refuse records a port the operator did not grant, or one that cannot be
// used, without opening anything.
func (r *Receiver) Refuse(stream store.Stream, reason string) {
	r.set(stream, Listener{Error: reason})
}

func (r *Receiver) set(stream store.Stream, held Listener) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.listeners[stream] = held
}

// Status is one stream's port and counts.
type Status struct {
	Listener Listener `json:"listener"`
	Counts   Counts   `json:"counts"`
	// LastDrop is why the newest dropped record was dropped.
	LastDrop string `json:"last_drop,omitempty"`
	// LastReceived is when the newest datagram arrived since the module
	// started, and nil before the first one. A port that listens and has
	// received nothing for a while has a printer that stopped sending.
	LastReceived *time.Time `json:"last_received,omitempty"`
}

// Status is every stream's port and counts.
func (r *Receiver) Status() map[store.Stream]Status {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := map[store.Stream]Status{}

	for stream, held := range r.counts {
		status := Status{
			Listener: r.listeners[stream],
			Counts: Counts{
				Received:   held.received.Load(),
				Unreadable: held.unreadable.Load(),
				Records:    held.records.Load(),
				Dropped:    held.dropped.Load(),
			},
		}

		if reason := held.lastDrop.Load(); reason != nil {
			status.LastDrop = *reason
		}

		if at := held.lastAt.Load(); at != 0 {
			last := time.Unix(0, at).UTC()
			status.LastReceived = &last
		}

		out[stream] = status
	}

	return out
}

// hostOf is the address a datagram came from, without its port.
func hostOf(from net.Addr) string {
	if udp, ok := from.(*net.UDPAddr); ok {
		return udp.IP.String()
	}

	host, _, err := net.SplitHostPort(from.String())
	if err != nil {
		return from.String()
	}

	return host
}
