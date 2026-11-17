package timeserver

import (
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestASourceNoClientSendsFromIsNotAnswered(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		from net.Addr
		want bool
	}{
		"a host":             {&net.UDPAddr{IP: net.IPv4(10, 0, 10, 56), Port: 123}, true},
		"a host ending .255": {&net.UDPAddr{IP: net.IPv4(10, 0, 1, 255), Port: 40000}, true},
		"port zero":          {&net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 0}, false},
		"limited broadcast":  {&net.UDPAddr{IP: net.IPv4bcast, Port: 123}, false},
		"multicast":          {&net.UDPAddr{IP: net.IPv4(224, 0, 0, 1), Port: 123}, false},
		"unspecified":        {&net.UDPAddr{IP: net.IPv4zero, Port: 123}, false},
		"not a UDP address":  {&net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 123}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := answerable(tc.from); got != tc.want {
				t.Fatalf("answerable(%v) = %v, want %v", tc.from, got, tc.want)
			}
		})
	}
}

// scriptedConn hands the read loop one datagram per source, then reports
// itself closed, and records where answers went.
type scriptedConn struct {
	net.PacketConn

	from []net.Addr
	sent []net.Addr
}

func (c *scriptedConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if len(c.from) == 0 {
		return 0, nil, net.ErrClosed
	}

	from := c.from[0]
	c.from = c.from[1:]
	b[0] = 4<<3 | modeClient

	return packetSize, from, nil
}

func (c *scriptedConn) WriteTo(_ []byte, to net.Addr) (int, error) {
	c.sent = append(c.sent, to)

	return packetSize, nil
}

func (c *scriptedConn) Close() error { return nil }

func TestTheReadLoopAnswersOnlyAnAnswerableSource(t *testing.T) {
	t.Parallel()

	host := &net.UDPAddr{IP: net.IPv4(10, 0, 10, 56), Port: 123}
	conn := &scriptedConn{from: []net.Addr{
		&net.UDPAddr{IP: net.IPv4bcast, Port: 123},
		host,
		&net.UDPAddr{IP: net.IPv4(224, 0, 0, 1), Port: 123},
	}}

	done := make(chan struct{})
	(&Server{}).serve(t.Context(), conn, done)

	if len(conn.sent) != 1 || conn.sent[0] != net.Addr(host) {
		t.Fatalf("answers went to %v, want only %v", conn.sent, host)
	}
}

func TestAnAnswerInTheReadLoopAllocatesNothing(t *testing.T) {
	request, out := make([]byte, packetSize), make([]byte, packetSize)
	request[0] = 4<<versionShift | modeClient
	now := time.Now()

	if allocs := testing.AllocsPerRun(100, func() { fill(out, request, now) }); allocs != 0 {
		t.Fatalf("fill allocates %v times per answer, want 0", allocs)
	}
}

func TestAFailureToOpenThePortIsSortedByItsCause(t *testing.T) {
	t.Parallel()

	bind := func(errno syscall.Errno) error {
		return &net.OpError{Op: "listen", Net: "udp4", Err: os.NewSyscallError("bind", errno)}
	}

	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"no right to the port":      {bind(syscall.EACCES), CausePermission},
		"refused by the system":     {bind(syscall.EPERM), CausePermission},
		"another program holds it":  {bind(syscall.EADDRINUSE), CauseInUse},
		"an address the host lacks": {bind(syscall.EADDRNOTAVAIL), ""},
		"not a system error":        {errors.New("no route"), ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := causeOf(tc.err); got != tc.want {
				t.Fatalf("causeOf(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
