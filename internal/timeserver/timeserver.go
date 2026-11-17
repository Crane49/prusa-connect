// Package timeserver answers SNTP requests (RFC 4330) with this machine's
// clock, so a device on a network with no internet can set its own.
package timeserver

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Port is the UDP port every NTP client asks on.
const Port = 123

// Addr is the address the server listens on: every IPv4 interface, on Port.
//
//nolint:gochecknoglobals // derived from Port once, never written.
var Addr = net.JoinHostPort("", strconv.Itoa(Port))

// The states State reports.
const (
	StateOff     = "off"
	StateServing = "serving"
	StateFailed  = "failed"
)

// The causes of StateFailed that State names, so a page can say what to do.
const (
	// CausePermission is a process with no right to open a port below 1024.
	CausePermission = "permission"
	// CauseInUse is another program holding the port, such as an NTP server.
	CauseInUse = "in_use"
)

const (
	packetSize = 48
	// maxRequest is the read buffer. A longer datagram carries extension
	// fields this server ignores.
	maxRequest   = 512
	modeMask     = 0x7
	versionShift = 3
	modeClient   = 3
	modeServer   = 4
	// stratum is the level of a local clock with no source above it. A client
	// refuses 0 and 16.
	stratum = 10
	// epochOffset is the seconds from 1900, where NTP time starts, to 1970.
	epochOffset = 2208988800
	// nanosPerSecond scales a nanosecond count to the 32-bit fraction.
	nanosPerSecond = 1e9
)

// Server is one SNTP listener that can be switched on and off while the
// process runs. The zero value is not usable; call New.
type Server struct {
	addr   string
	logger *slog.Logger

	mu     sync.Mutex
	conn   net.PacketConn
	done   chan struct{}
	reason string
	cause  string
}

// New returns a server that listens on addr once started.
func New(addr string, logger *slog.Logger) *Server {
	return &Server{addr: addr, logger: logger}
}

// Start opens the socket and answers on it until Stop or until ctx ends. A
// server already serving is left as it is, and one whose context ended is
// opened again. A socket that cannot be opened is reported by State, and the
// error is returned too.
func (s *Server) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn != nil && !closed(s.done) {
		return nil
	}

	// IPv4 only. A dual-stack socket opens beside another program's IPv4
	// socket on some systems and then hears nothing over IPv4.
	conn, err := (&net.ListenConfig{}).ListenPacket(ctx, "udp4", s.addr)
	if err != nil {
		s.reason, s.cause = err.Error(), causeOf(err)
		s.logger.WarnContext(ctx, "the time server could not open its port", "addr", s.addr, "error", err)

		return err
	}

	s.conn, s.done, s.reason, s.cause = conn, make(chan struct{}), "", ""
	s.logger.InfoContext(ctx, "the time server is serving", "addr", conn.LocalAddr().String())

	go s.serve(ctx, conn, s.done)

	return nil
}

// Stop closes the socket and waits for the answering goroutine to end. It
// also clears a failure, so a stopped server reports off.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reason, s.cause = "", ""

	if s.conn == nil {
		return
	}

	_ = s.conn.Close() // the read loop ends on the error this causes
	<-s.done
	s.conn, s.done = nil, nil
}

// State is StateServing, StateFailed with the reason and a cause when one is
// known, or StateOff.
func (s *Server) State() (state, reason, cause string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case s.conn != nil && !closed(s.done):
		return StateServing, "", ""
	case s.reason != "":
		return StateFailed, s.reason, s.cause
	default:
		return StateOff, "", ""
	}
}

// causeOf sorts an error from opening the socket. Any other error has no
// cause, and the reason alone says what happened.
func causeOf(err error) string {
	switch {
	case errors.Is(err, os.ErrPermission):
		return CausePermission
	case errors.Is(err, syscall.EADDRINUSE):
		return CauseInUse
	default:
		return ""
	}
}

// LocalAddr is the address the socket is bound to, or nil while not serving.
func (s *Server) LocalAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return nil
	}

	return s.conn.LocalAddr()
}

// closed reports whether the answering goroutine has ended, which a cancelled
// context causes without a Stop.
func closed(done chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (s *Server) serve(ctx context.Context, conn net.PacketConn, done chan struct{}) {
	defer close(done)

	// A cancelled context closes the socket, which ends the read below.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	request := make([]byte, maxRequest)
	answer := make([]byte, packetSize) // reused, so an answer allocates nothing

	for {
		n, from, err := conn.ReadFrom(request)
		if errors.Is(err, net.ErrClosed) {
			return
		}

		if err != nil {
			continue // one bad datagram, such as an ICMP error on some systems
		}

		if n < packetSize || request[0]&modeMask != modeClient || !answerable(from) {
			continue
		}

		fill(answer, request[:packetSize], time.Now())
		_, _ = conn.WriteTo(answer, from) // a lost answer is asked again
	}
}

// answerable refuses a source no client sends from. A spoofed broadcast or
// multicast source would make every host on the network receive the answer.
func answerable(from net.Addr) bool {
	udp, ok := from.(*net.UDPAddr)
	if !ok || udp.Port == 0 {
		return false
	}

	ip := udp.IP.To4()

	// A subnet's own broadcast address, such as 192.168.1.255, still passes:
	// refusing it needs the netmask of each interface.
	return ip != nil && !ip.IsMulticast() && !ip.IsUnspecified() && !ip.Equal(net.IPv4bcast)
}

// Answer is the reply to one SNTP request at the time now. The request is at
// least 48 bytes.
func Answer(request []byte, now time.Time) []byte {
	out := make([]byte, packetSize)
	fill(out, request, now)

	return out
}

// fill writes the answer to request into out, which is 48 bytes.
func fill(out, request []byte, now time.Time) {
	clear(out)

	version := request[0] >> versionShift & modeMask
	if version == 0 {
		version = 4
	}

	out[0] = version<<versionShift | modeServer
	out[1] = stratum
	out[2] = request[2] // the client's poll interval
	out[3] = 0xEC       // precision, 2^-20 seconds
	copy(out[12:16], "LOCL")
	put(out[16:24], now)
	copy(out[24:32], request[40:48]) // the client's transmit time, which it matches
	put(out[32:40], now)
	put(out[40:48], now)
}

func put(b []byte, t time.Time) {
	// NTP seconds wrap every 136 years by design, so the truncation is the
	// format. The nanoseconds are 0 to 1e9, never negative.
	seconds := uint32(t.Unix() + epochOffset)                         //nolint:gosec // the wrap above
	fraction := uint32(uint64(t.Nanosecond()) << 32 / nanosPerSecond) //nolint:gosec // the range above

	binary.BigEndian.PutUint32(b, seconds)
	binary.BigEndian.PutUint32(b[4:], fraction)
}
