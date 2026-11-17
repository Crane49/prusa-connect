package xbuddy_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// transferHeaderLength is the nine bytes of kind and file id every chunk frame
// carries in front of the file's own bytes. It is spelled here because the
// ceiling bounds the file's bytes and the meter below measures the whole frame.
const transferHeaderLength = 9

// The ceiling is what stops a board that dies of a large frame, so the property
// under test is a measurement of the wire and not of the configuration: no
// binary message the printer receives carries more of the file than the ceiling
// allows, whatever chunk size the printer asked for.
//
// The digest is measured alongside it, because a ceiling that bounded frames by
// dropping bytes would satisfy the bound and deliver a different file.
func TestTheChunkCeilingBoundsEveryFrameTheDeviceReceives(t *testing.T) {
	t.Parallel()

	// 4096 is the chunk this printer asks for, so a ceiling above it is bounded
	// by the request instead and the largest frame stops growing there.
	const requestedChunk = 4096

	cases := []struct {
		name    string
		ceiling int
	}{
		{name: "the old default, four times under the chunk", ceiling: 1024},
		{name: "twice the old default", ceiling: 2048},
		{name: "just under the chunk the printer asks for", ceiling: 3000},
		{name: "the measured default, the chunk the printer asks for", ceiling: 4096},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				meter := &frameMeter{}
				rig := startMetered(t, "MK4", testCase.ceiling, meter)
				defer rig.stop(t)

				rig.connect(t)

				data := sampleFile(40 << 10)

				if err := uploadsOf(t, rig.spy.inner).Send(rig.ctx, connectorapi.Upload{
					Path: "usb/ceiling.bgcode", Content: bytesContent{data: data},
				}); err != nil {
					t.Fatalf("send: %v", err)
				}

				landed, ok := rig.printer.LastTransfer()
				if !ok || !landed.Finished {
					t.Fatalf("the transfer did not finish: %+v", landed)
				}

				if landed.Digest != digestOf(data) {
					t.Errorf("the printer received a different file: digest %s, want %s",
						landed.Digest, digestOf(data))
				}

				frames, largest := meter.observed()
				t.Logf("observed: %d binary frames, the largest carrying %d bytes of the file",
					frames, largest-transferHeaderLength)

				if frames == 0 {
					t.Fatal("no binary frame was observed, so the ceiling was never exercised")
				}

				if carried := largest - transferHeaderLength; carried > testCase.ceiling {
					t.Errorf("a frame carried %d bytes of the file, and the ceiling is %d",
						carried, testCase.ceiling)
				}

				// The bound has to bite as well as hold: a run whose largest
				// frame fell short of the ceiling would pass the check above
				// without proving the setting reached the wire.
				want := min(testCase.ceiling, requestedChunk)
				if carried := largest - transferHeaderLength; carried != want {
					t.Errorf("the largest frame carried %d bytes of the file, want %d", carried, want)
				}
			})
		})
	}
}

// firmwareRecordCeiling is the plaintext a Buddy board will read in one TLS
// record: MBEDTLS_SSL_IN_CONTENT_LEN in
// prusa-firmware-buddy/include/mbedtls/cipher_config_ece.h. It is the number the
// default ceiling is chosen against, so it is spelled here rather than derived
// from the default, which would make this test agree with any default at all.
const firmwareRecordCeiling = 1024

// The default has to keep one write inside the record buffer the firmware
// declares, and this is the check that fails when somebody raises it.
//
// The measurement is the whole websocket message, header included, because that
// is what one write hands the TLS connection and Go emits one record per write.
// Bounding the file's own bytes is not enough: the ceiling names the payload,
// and the nine-byte transfer header and the websocket header ride on top of it.
//
// It takes the default rather than naming a number, so a change to the constant
// is what this measures. Nothing here terminates TLS - the rig is a pipe - and
// that is the point: the bug this guards against was found on a deployment that
// does, and reproducing it needs only the size of the write, not the record.
func TestTheDefaultCeilingKeepsOneWriteInsideTheFirmwaresRecordBuffer(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		meter := &frameMeter{}
		rig := startMeteredAtTheDefault(t, "MK4", meter)
		defer rig.stop(t)

		rig.connect(t)

		data := sampleFile(40 << 10)

		if err := uploadsOf(t, rig.spy.inner).Send(rig.ctx, connectorapi.Upload{
			Path: "usb/default-ceiling.bgcode", Content: bytesContent{data: data},
		}); err != nil {
			t.Fatalf("send: %v", err)
		}

		landed, ok := rig.printer.LastTransfer()
		if !ok || !landed.Finished || landed.Digest != digestOf(data) {
			t.Fatalf("the transfer did not deliver the file: %+v", landed)
		}

		frames, payload := meter.observed()
		widest := meter.widestWrite()

		t.Logf("observed: %d binary frames, the largest carrying %d bytes of the file "+
			"and handing %d bytes to the connection", frames, payload-transferHeaderLength, widest)

		if frames == 0 {
			t.Fatal("no binary frame was observed, so nothing was measured")
		}

		if widest > firmwareRecordCeiling {
			t.Errorf("one write handed the connection %d bytes, and a Buddy board reads at most %d "+
				"in a record. Raising chunk_write_ceiling breaks every transfer to a printer whose "+
				"TLS this server terminates. See the constant's comment and earlier research.",
				widest, firmwareRecordCeiling)
		}
	})
}

// A ceiling the wire cannot carry is refused where the operator can still act on
// it, which is at construction and not on the first transfer.
func TestAChunkCeilingOutOfRangeIsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		config string
	}{
		{name: "negative", config: `{"chunk_write_ceiling":-1}`},
		{name: "above the largest chunk this wire carries", config: `{"chunk_write_ceiling":1048577}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, err := xbuddy.New(connectorapi.Spec{
				Ref: "printer-1", Type: xbuddy.Type, Config: json.RawMessage(testCase.config),
			}); err == nil {
				t.Errorf("%s was accepted", testCase.config)
			}
		})
	}

	t.Run("absent takes the default", func(t *testing.T) {
		t.Parallel()

		if _, err := xbuddy.New(connectorapi.Spec{
			Ref: "printer-1", Type: xbuddy.Type, Config: json.RawMessage(`{}`),
		}); err != nil {
			t.Errorf("a config naming no ceiling was refused: %v", err)
		}
	})
}

// The device counts what it has taken and says so in TRANSFER_INFO, and that
// number is the only one that reflects the printer's own side. Sent is what this
// host put on the wire, which a printer that reset mid-transfer has not all
// taken.
func TestTheDevicesOwnProgressReachesTheHost(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		session := rig.connect(t)

		gate := make(chan struct{})
		content := &gatedContent{data: sampleFile(40 << 10), after: 4096, gate: gate}
		reports := &progressLog{}

		sent := make(chan error, 1)

		go func() {
			sent <- uploadsOf(t, rig.spy.inner).Send(rig.ctx, connectorapi.Upload{
				Path:     "usb/gated.bgcode",
				Content:  content,
				Progress: reports.record,
			})
		}()

		// The printer drops its connection on the first chunk of a transfer and
		// resumes on the next (simulator.ErrTransferReset). Until it is back, what
		// holds the transfer is its reconnect timer, which virtual time does not
		// advance across synctest.Wait, and the resumed chunks would race the
		// SEND_TRANSFER_INFO answer below.
		rig.awaitAdoption(t, 2)

		// Everything is blocked on the gate, so the printer has taken every byte
		// this host has written and its answer is a settled number.
		synctest.Wait()

		landed, ok := rig.printer.LastTransfer()
		if !ok || landed.Received == 0 {
			t.Fatalf("the printer has taken nothing to report on: %+v", landed)
		}

		if _, err := session.Do(rig.ctx, connectorapi.Command{Verb: xbuddy.VerbSendTransferInfo}); err != nil {
			t.Fatalf("SEND_TRANSFER_INFO: %v", err)
		}

		known, ok := reports.newestKnown()
		if !ok {
			t.Fatal("no progress report carried a figure from the device")
		}

		t.Logf("observed: the device reported %d of %d taken, and this host had written %d",
			known.Received, known.Total, known.Sent)

		if known.Received != landed.Received {
			t.Errorf("the host was told the device has taken %d bytes, and it has taken %d",
				known.Received, landed.Received)
		}

		if known.Total != content.Size() {
			t.Errorf("the report names a total of %d, want %d", known.Total, content.Size())
		}

		if known.Ref == "" {
			t.Error("the report names no transfer")
		}

		close(gate)

		if err := <-sent; err != nil {
			t.Fatalf("send: %v", err)
		}

		if final := reports.last(); final.Sent != content.Size() {
			t.Errorf("the last report says %d bytes were sent, and the file is %d",
				final.Sent, content.Size())
		}
	})
}

// TestAHostHearsProgressInsideOneRange is the shape of a real Buddy board,
// which asks for the whole file in one range. A report only at the end of a
// range left the host at 0 % until the file had arrived (CL-218).
func TestAHostHearsProgressInsideOneRange(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)

		gate := make(chan struct{})
		content := &gatedContent{data: sampleFile(200 << 10), after: 150 << 10, gate: gate}
		reports := &progressLog{}

		sent := make(chan error, 1)

		go func() {
			sent <- uploadsOf(t, rig.spy.inner).Send(rig.ctx, connectorapi.Upload{
				Path:     "usb/one-range.bgcode",
				Content:  content,
				Progress: reports.record,
			})
		}()

		rig.awaitAdoption(t, 2)
		synctest.Wait()

		reports.mu.Lock()
		midway := slices.ContainsFunc(reports.reports, func(report connectorapi.Progress) bool {
			return report.Sent > 0 && report.Sent < content.Size()
		})
		reports.mu.Unlock()

		if !midway {
			t.Errorf("the host heard nothing between the start and the end of the range: %+v", reports.last())
		}

		close(gate)

		if err := <-sent; err != nil {
			t.Fatalf("send: %v", err)
		}
	})
}

// A printer that starts a file over is served the bytes it had again. Counted
// each time, they passed the file's length, and a CORE One's transfer was drawn
// at 1036 % (CL-296).
func TestABlockAskedForAgainIsCountedOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(func(cfg *simulator.Config) {
			cfg.RestartsTransfers = true
		}))
		defer rig.stop(t)

		rig.connect(t)

		data := sampleFile(40 << 10)
		reports := &progressLog{}

		if err := uploadsOf(t, rig.spy.inner).Send(rig.ctx, connectorapi.Upload{
			Path: "usb/again.bgcode", Content: bytesContent{data: data}, Progress: reports.record,
		}); err != nil {
			t.Fatalf("send: %v", err)
		}

		landed, ok := rig.printer.LastTransfer()
		if !ok || !landed.Finished || landed.Digest != digestOf(data) {
			t.Fatalf("the transfer did not deliver the file: %+v", landed)
		}

		reports.mu.Lock()
		defer reports.mu.Unlock()

		for _, report := range reports.reports {
			if report.Sent > report.Total {
				t.Fatalf("a report says %d bytes were sent of a %d byte file", report.Sent, report.Total)
			}
		}

		if final := reports.reports[len(reports.reports)-1]; final.Sent != int64(len(data)) {
			t.Errorf("the last report says %d bytes were sent, and the file is %d", final.Sent, len(data))
		}
	})
}

// The callback is optional, and the field is the zero value of an Upload that
// sets only a path and a content. A driver that called it unguarded would take
// every host that has not asked for reports down with it.
func TestATransferWithNoProgressCallbackStillCompletes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)

		data := sampleFile(40 << 10)

		if err := uploadsOf(t, rig.spy.inner).Send(rig.ctx, connectorapi.Upload{
			Path: "usb/quiet.bgcode", Content: bytesContent{data: data},
		}); err != nil {
			t.Fatalf("send: %v", err)
		}

		landed, ok := rig.printer.LastTransfer()
		if !ok || !landed.Finished || landed.Digest != digestOf(data) {
			t.Fatalf("a transfer with no progress callback did not deliver the file: %+v", landed)
		}
	})
}

// progressLog is what a host does with the callback, and it is what the contract
// says it may do: write its own state with no lock, because the driver never
// enters it twice at once. The mutex here is for the test goroutine reading the
// log, not for the callback.
type progressLog struct {
	mu      sync.Mutex
	reports []connectorapi.Progress
}

func (l *progressLog) record(report connectorapi.Progress) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.reports = append(l.reports, report)
}

// newestKnown is the last report carrying a figure the device gave.
func (l *progressLog) newestKnown() (connectorapi.Progress, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, v := range slices.Backward(l.reports) {
		if v.Known {
			return v, true
		}
	}

	return connectorapi.Progress{}, false
}

func (l *progressLog) last() connectorapi.Progress {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.reports) == 0 {
		return connectorapi.Progress{}
	}

	return l.reports[len(l.reports)-1]
}

// gatedContent is a file that stops giving bytes once a set number has been
// read, until the test lets the rest through. It is what holds a transfer still
// long enough to ask the printer what it makes of it.
type gatedContent struct {
	data  []byte
	after int64
	gate  chan struct{}
	read  atomic.Int64
}

func (c *gatedContent) Size() int64 { return int64(len(c.data)) }

func (c *gatedContent) Open(ctx context.Context, offset int64) (io.ReadCloser, error) {
	if offset >= int64(len(c.data)) {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}

	return &gatedReader{ctx: ctx, content: c, inner: bytes.NewReader(c.data[offset:])}, nil
}

type gatedReader struct {
	ctx     context.Context //nolint:containedctx // Read carries no context and the gate has to be cancellable.
	content *gatedContent
	inner   *bytes.Reader
}

func (r *gatedReader) Read(p []byte) (int, error) {
	done := r.content.read.Load()

	if done >= r.content.after {
		select {
		case <-r.content.gate:
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		}

		read, err := r.inner.Read(p)
		r.content.read.Add(int64(read))

		return read, err
	}

	room := min(int64(len(p)), r.content.after-done)

	read, err := r.inner.Read(p[:room])
	r.content.read.Add(int64(read))

	return read, err
}

func (r *gatedReader) Close() error { return nil }

// frameMeter is the largest binary websocket message the host wrote, measured
// off the wire rather than taken from the driver's own accounting.
//
// Two sizes, because two different things are bounded. payload is the file's own
// bytes, which the ceiling names. wire is the whole message including the
// websocket header, which is what one write hands the TLS connection and so what
// ends up in one TLS record.
type frameMeter struct {
	mu          sync.Mutex
	frames      int
	largest     int
	largestWire int
}

func (m *frameMeter) saw(payload, wire int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.frames++

	if payload > m.largest {
		m.largest = payload
	}

	if wire > m.largestWire {
		m.largestWire = wire
	}
}

func (m *frameMeter) observed() (frames, largest int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.frames, m.largest
}

func (m *frameMeter) widestWrite() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.largestWire
}

// meteredListener hands the server a connection that reports every binary
// message written down it.
type meteredListener struct {
	*pipeListener

	meter *frameMeter
}

func (l *meteredListener) Accept() (net.Conn, error) {
	conn, err := l.pipeListener.Accept()
	if err != nil {
		return nil, err
	}

	return &meteredConn{Conn: conn, scanner: &frameScanner{meter: l.meter}}, nil
}

// meteredConn counts what the host writes. Each connection scans its own stream,
// because this printer drops the connection mid-transfer and a scanner shared
// across the two would resynchronise on the bytes of a frame it never saw the
// end of.
type meteredConn struct {
	net.Conn

	scanner *frameScanner
}

func (c *meteredConn) Write(p []byte) (int, error) {
	written, err := c.Conn.Write(p)
	c.scanner.wrote(p[:written])

	return written, err
}

// frameScanner reads websocket messages out of one server-to-printer byte
// stream.
//
// It measures whole messages and not frames: gorilla splits a message larger
// than its write buffer into a frame and its continuations, and what the printer
// receives is the message they reassemble into.
type frameScanner struct {
	meter    *frameMeter
	buffer   []byte
	upgraded bool
	// binary is whether the message being assembled is a binary one, and pending
	// is how much of it has arrived.
	binary  bool
	pending int
	// wire is pending plus every frame header the message travelled under, so
	// it is the byte count one write handed the connection.
	wire int
}

func (s *frameScanner) wrote(p []byte) {
	s.buffer = append(s.buffer, p...)

	if !s.upgraded {
		end := bytes.Index(s.buffer, []byte("\r\n\r\n"))
		if end < 0 {
			return
		}

		s.buffer = s.buffer[end+4:]
		s.upgraded = true
	}

	for s.takeFrame() {
	}
}

// takeFrame consumes one frame and reports whether another may follow.
func (s *frameScanner) takeFrame() bool {
	const (
		opcodeContinuation = 0x0
		opcodeBinary       = 0x2

		twoByteLength   = 126
		eightByteLength = 127
	)

	if len(s.buffer) < 2 {
		return false
	}

	final := s.buffer[0]&0x80 != 0
	opcode := s.buffer[0] & 0x0f
	masked := s.buffer[1]&0x80 != 0
	length := int(s.buffer[1] & 0x7f)
	header := 2

	switch length {
	case twoByteLength:
		if len(s.buffer) < 4 {
			return false
		}

		length = int(binary.BigEndian.Uint16(s.buffer[2:4]))
		header = 4
	case eightByteLength:
		if len(s.buffer) < 10 {
			return false
		}

		length = int(binary.BigEndian.Uint64(s.buffer[2:10]))
		header = 10
	}

	if masked {
		header += 4
	}

	if len(s.buffer) < header+length {
		return false
	}

	s.buffer = s.buffer[header+length:]

	switch opcode {
	case opcodeBinary:
		s.binary, s.pending, s.wire = true, length, header+length
	case opcodeContinuation:
		s.pending += length
		s.wire += header + length
	default:
		return true
	}

	if final {
		if s.binary {
			s.meter.saw(s.pending, s.wire)
		}

		s.binary, s.pending, s.wire = false, 0, 0
	}

	return true
}

// startMeteredAtTheDefault is the rig with no ceiling configured, so the driver
// takes its own default and the test measures that rather than a number it
// chose. The simulated board is given room for far more than the default, so
// what bounds the frames is the driver and not the harness.
func startMeteredAtTheDefault(t *testing.T, model string, meter *frameMeter) *rig {
	t.Helper()

	return startMeteredWith(t, model, 0, maxChunkForATolerantBoard, meter)
}

// maxChunkForATolerantBoard is what the simulated board accepts where the test
// is measuring the driver's own bound. It is the wire's own maximum, so nothing
// in the harness can be mistaken for the thing under test.
const maxChunkForATolerantBoard = 1 << 20

// startMetered is the rig with a chunk ceiling of its own and a meter under the
// server's side of every connection.
//
// The simulated board takes exactly the ceiling and hangs up on anything larger,
// the way the hardware the number came from does. So the transfer completing is
// itself proof the ceiling held, and the meter says by how much.
func startMetered(t *testing.T, model string, ceiling int, meter *frameMeter) *rig {
	t.Helper()

	return startMeteredWith(t, model, ceiling, ceiling+transferHeaderLength, meter)
}

// startMeteredWith is the two above, with the driver's ceiling and the board's
// tolerance named separately.
func startMeteredWith(t *testing.T, model string, ceiling, boardChunk int, meter *frameMeter) *rig {
	t.Helper()

	profiles, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the profiles: %v", err)
	}

	quiet := slog.New(slog.DiscardHandler)

	host := &arrivals{
		profiles: profiles,
		resolver: capability.NewResolver(profiles, quiet),
		sessions: make(chan *capability.Session, 4),
		spies:    make(chan *driverSpy, 4),
	}

	driver := xbuddy.NewDriver(xbuddy.ModeToken, 8, 0, "", quiet,
		xbuddy.WithChunkWriteCeiling(ceiling))
	host.baseline = driver.Baseline()

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	listener := newPipeListener()
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() { _ = server.Serve(&meteredListener{pipeListener: listener, meter: meter}) }()

	printer, err := simulator.New(simulator.Config{
		Model: model, BaseURL: "http://xbuddy-host", Dial: listener.dial,
		MaxChunkBytes: boardChunk,
	})
	if err != nil {
		t.Fatalf("new printer: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())

	return &rig{
		driver: driver, host: host, printer: printer,
		ctx: ctx, cancel: cancel, server: server, listener: listener,
	}
}
