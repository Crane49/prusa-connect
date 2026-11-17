package node_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// reading returns the property values every test in this file reports.
func reading() fractal.Props {
	return fractal.Props{tempProp: json.RawMessage(`25.0`)}
}

// lastREP returns the timestamp of the last REP the node sent.
func lastREP(t *testing.T, h *harness) fractal.Timestamp {
	t.Helper()
	reports := h.binding.OfKind(fractal.KindREP)
	if len(reports) == 0 {
		t.Fatal("the node sent no state report")
	}
	var m fractal.REP
	if err := json.Unmarshal(reports[len(reports)-1].dispatch.Payload, &m); err != nil {
		t.Fatalf("read the report: %v", err)
	}
	return m.Timestamp
}

// TestAReportNeverGoesBackInTime checks that the timestamp of a REP is never
// earlier than the timestamp of the last REP the node sent for that interface.
// The node above discards a report whose timestamp is lower than the one in its
// shadow (fractal.md, Reported Properties Message), so it would lose a reading
// that is a few milliseconds earlier than the last report.
func TestAReportNeverGoesBackInTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		if err := h.iface.Report(t.Context(), reading(), now+10); err != nil {
			t.Fatalf("report: %v", err)
		}
		if got := lastREP(t, h); got != now+10 {
			t.Fatalf("the first report has timestamp %d, want %d", got, now+10)
		}
		if err := h.iface.Report(t.Context(), reading(), now+5); err != nil {
			t.Fatalf("report: %v", err)
		}
		if got := lastREP(t, h); got != now+10 {
			t.Errorf("a report earlier than the last one has timestamp %d, want %d", got, now+10)
		}
		if err := h.iface.Report(t.Context(), reading(), now+20); err != nil {
			t.Fatalf("report: %v", err)
		}
		if got := lastREP(t, h); got != now+20 {
			t.Errorf("a report later than the last one has timestamp %d, want %d", got, now+20)
		}
	})
}

// TestAFullReportCarriesTheTimeOfTheReading checks that a full state report
// has the timestamp Owner.Snapshot returned, and that a later Report with that
// same timestamp keeps it.
func TestAFullReportCarriesTheTimeOfTheReading(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		h.owner.mu.Lock()
		h.owner.snapshotAt = now - 100
		h.owner.mu.Unlock()
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRBR,
			Payload: encode(t, fractal.RBR{Version: fractal.Version}),
		})
		synctest.Wait()
		if got := lastREP(t, h); got != now-100 {
			t.Errorf("the full report has timestamp %d, want the time Snapshot returned, %d", got, now-100)
		}
		// The owner reports the same reading again with its own timestamp. The
		// node must not raise that timestamp.
		if err := h.iface.Report(t.Context(), reading(), now-100); err != nil {
			t.Fatalf("report: %v", err)
		}
		if got := lastREP(t, h); got != now-100 {
			t.Errorf("the report has timestamp %d, want %d", got, now-100)
		}
	})
}

// TestAFullReportTakesTheClockWhenTheOwnerHasNoTime checks that a full state
// report has the node's current time when Owner.Snapshot returns a zero time.
func TestAFullReportTakesTheClockWhenTheOwnerHasNoTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRBR,
			Payload: encode(t, fractal.RBR{Version: fractal.Version}),
		})
		synctest.Wait()
		if got := lastREP(t, h); got != now {
			t.Errorf("the full report has timestamp %d, want the node's current time %d", got, now)
		}
	})
}

// TestAFullReportStopsWhenEveryReadSeesAChange checks an owner that reports
// a change during every Snapshot, each of which takes a second. The node stops
// reading at its timeout and logs that it sent no full report.
func TestAFullReportStopsWhenEveryReadSeesAChange(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		owner := &busyOwner{}
		handle, err := h.node.Register(node.Instance{
			Definition: definition(t), Owner: owner, Name: ifaceName, Instance: "2",
		})
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		owner.handle = handle
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRBR,
			Payload: encode(t, fractal.RBR{Version: fractal.Version}),
		})
		time.Sleep(time.Minute)
		synctest.Wait()

		if got, want := h.log.Levels(unsentFullReportLog), []slog.Level{slog.LevelError}; !slices.Equal(got, want) {
			t.Errorf("the node logged %q at %v, want %v", unsentFullReportLog, got, want)
		}
	})
}

// unsentFullReportLog is the message a node logs when it stops reading the
// state for a full report.
const unsentFullReportLog = "the full report is not sent: a change was reported during every read of the state"

// busyOwner reports a change during every Snapshot, and each Snapshot takes a
// second.
type busyOwner struct {
	testOwner

	handle *node.Interface
	reads  int
}

func (o *busyOwner) Snapshot(ctx context.Context) (fractal.Props, fractal.Timestamp, error) {
	o.reads++
	props := fractal.Props{tempProp: json.RawMessage(strconv.Itoa(o.reads) + ".0")}
	if err := o.handle.Report(ctx, props, now+fractal.Timestamp(o.reads)); err != nil {
		return nil, 0, err
	}
	time.Sleep(time.Second)
	return props, 0, nil
}

// flushingOwner changes its value while the node reads its full state. Each
// of the first flushes Snapshots reads the value the owner holds. Before it
// returns, the owner reports one degree more, as a report of a change in
// another goroutine does.
type flushingOwner struct {
	testOwner

	handle  *node.Interface
	flushes int
	degrees int
}

func (o *flushingOwner) Snapshot(ctx context.Context) (fractal.Props, fractal.Timestamp, error) {
	read := o.props()
	if o.flushes == 0 {
		return read, 0, nil
	}
	o.flushes--
	o.degrees++
	if err := o.handle.Report(ctx, o.props(), now+fractal.Timestamp(o.degrees)); err != nil {
		return nil, 0, err
	}
	return read, 0, nil
}

func (o *flushingOwner) props() fractal.Props {
	return fractal.Props{tempProp: json.RawMessage(strconv.Itoa(o.degrees) + ".0")}
}

// TestAFullReportNeverPassesANewerChange checks changes reported while the
// node reads the full state, four times in a row. The node above keeps the
// last value it gets, so the last report the node sends carries the newest
// value (fractal-requirements.md: the change MUST eventually be reflected).
func TestAFullReportNeverPassesANewerChange(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		owner := &flushingOwner{flushes: 4, degrees: 25}
		handle, err := h.node.Register(node.Instance{
			Definition: definition(t), Owner: owner, Name: ifaceName, Instance: "2",
		})
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		owner.handle = handle
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRBR,
			Payload: encode(t, fractal.RBR{Version: fractal.Version}),
		})
		synctest.Wait()

		var last fractal.REP
		for _, sent := range h.binding.OfKind(fractal.KindREP) {
			var m fractal.REP
			if decodeErr := json.Unmarshal(sent.dispatch.Payload, &m); decodeErr != nil {
				t.Fatalf("read a report: %v", decodeErr)
			}
			if m.Instance == "2" {
				last = m
			}
		}
		if got := string(last.Props[tempProp]); got != "29.0" {
			t.Errorf("the last report carries %s, want the newest 29.0", got)
		}
	})
}

// slowOwner reports a change during its first Snapshot, and the clock of the
// node moves on while that read runs. It returns no time of its own.
type slowOwner struct {
	testOwner

	handle *node.Interface
	clock  *fixedClock
	reads  int
}

func (o *slowOwner) Snapshot(ctx context.Context) (fractal.Props, fractal.Timestamp, error) {
	o.reads++
	if o.reads == 1 {
		o.clock.Set(now + 1000)
		if err := o.handle.Report(ctx, reading(), now+500); err != nil {
			return nil, 0, err
		}
	}
	return reading(), 0, nil
}

// TestAFullReportCarriesTheTimeOfTheReadItSends checks a full report that
// reads the state twice, because a change was reported during the first
// read. It has the time of the second read, the one it sends.
func TestAFullReportCarriesTheTimeOfTheReadItSends(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		owner := &slowOwner{clock: h.clock}
		handle, err := h.node.Register(node.Instance{
			Definition: definition(t), Owner: owner, Name: ifaceName, Instance: "2",
		})
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		owner.handle = handle
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRBR,
			Payload: encode(t, fractal.RBR{Version: fractal.Version}),
		})
		synctest.Wait()

		if got := lastREP(t, h); got != now+1000 {
			t.Errorf("the full report has timestamp %d, want the time of its second read, %d", got, now+1000)
		}
	})
}
