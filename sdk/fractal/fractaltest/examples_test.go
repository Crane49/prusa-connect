package fractaltest_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// TestExample1Startup follows the first worked example of fractal.md. hq
// starts and asks for a full state report. The printer's report then updates
// the shadow of brno and the shadow of hq.
func TestExample1Startup(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startBelow(t)
		synctest.Wait()
		hqTrace, brnoTrace := n.hqLink.Watch(), n.brnoLink.Watch()
		n.startRoot(t)
		synctest.Wait()

		wantKinds(t, hqTrace.Down(), fractal.KindRBR)
		wantKinds(t, brnoTrace.Down(), fractal.KindRBR)
		wantKinds(t, brnoTrace.Up(), fractal.KindREP)
		wantKinds(t, hqTrace.Up(), fractal.KindREP)
		wantREP(t, brnoTrace.Up()[0], fractal.REP{
			Version:   fractal.Version,
			Sender:    printerPath,
			Interface: printerInterface,
			Instance:  printerInstance,
			Timestamp: tStartup,
			Props: fractal.Props{
				nozzleTemp:       json.RawMessage(`25.0`),
				nozzleTempTarget: json.RawMessage(`null`),
			},
		})
		if string(hqTrace.Up()[0].Payload) != string(brnoTrace.Up()[0].Payload) {
			t.Error("the report brno sent to hq differs from the report it received, " +
				"and a node must forward a report unchanged")
		}
		for _, snap := range []shadow.Snapshot{shadowOf(t, n.hq), shadowOf(t, n.brno)} {
			wantShadow(t, snap, shadow.StatusOK, tStartup, map[fractal.PropertyName]string{
				nozzleTemp:       "25.0",
				nozzleTempTarget: "null",
			})
		}
	})
}

// TestExample2StateChange follows the second worked example. The nozzle heats
// up and the printer reports only nozzleTemp. In each shadow, nozzleTempTarget
// keeps the value it had, because the report does not name it.
func TestExample2StateChange(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()
		hqTrace := n.hqLink.Watch()

		n.clock.Set(tHeating)
		n.owner.Set(nozzleTemp, json.RawMessage(`200.0`))
		report(t, n, fractal.Props{nozzleTemp: json.RawMessage(`200.0`)}, tHeating)
		synctest.Wait()

		wantKinds(t, hqTrace.Up(), fractal.KindREP)
		wantREP(t, hqTrace.Up()[0], fractal.REP{
			Version:   fractal.Version,
			Sender:    printerPath,
			Interface: printerInterface,
			Instance:  printerInstance,
			Timestamp: tHeating,
			Props:     fractal.Props{nozzleTemp: json.RawMessage(`200.0`)},
		})
		for _, snap := range []shadow.Snapshot{shadowOf(t, n.hq), shadowOf(t, n.brno)} {
			wantShadow(t, snap, shadow.StatusOK, tHeating, map[fractal.PropertyName]string{
				nozzleTemp:       "200.0",
				nozzleTempTarget: "null",
			})
		}
	})
}

// TestExample3Desired follows the third worked example. hq asks the printer to
// set nozzleTempTarget, brno passes the request down, and the printer accepts
// it and reports the new value.
func TestExample3Desired(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()
		n.clock.Set(tHeating)
		n.owner.Set(nozzleTemp, json.RawMessage(`200.0`))
		report(t, n, fractal.Props{nozzleTemp: json.RawMessage(`200.0`)}, tHeating)
		synctest.Wait()

		hqTrace, brnoTrace := n.hqLink.Watch(), n.brnoLink.Watch()
		n.clock.Set(tDesired)
		err := n.hq.Desire(t.Context(), node.DesireRequest{
			Target:    printerPath,
			Interface: printerInterface,
			Instance:  printerInstance,
			Props:     fractal.Props{nozzleTempTarget: json.RawMessage(`220.0`)},
		})
		if err != nil {
			t.Fatalf("hq could not send the requested property change: %v", err)
		}
		synctest.Wait()

		wantKinds(t, hqTrace.Down(), fractal.KindDES)
		wantKinds(t, brnoTrace.Down(), fractal.KindDES)
		if string(hqTrace.Down()[0].Payload) != string(brnoTrace.Down()[0].Payload) {
			t.Error("the request brno sent to the printer differs from the request it received, " +
				"and a node must pass a request down unchanged")
		}
		wantKinds(t, brnoTrace.Up(), fractal.KindREP)
		wantREP(t, brnoTrace.Up()[0], fractal.REP{
			Version:   fractal.Version,
			Sender:    printerPath,
			Interface: printerInterface,
			Instance:  printerInstance,
			Timestamp: tDesired,
			Props:     fractal.Props{nozzleTempTarget: json.RawMessage(`220.0`)},
		})
		for _, snap := range []shadow.Snapshot{shadowOf(t, n.hq), shadowOf(t, n.brno)} {
			wantShadow(t, snap, shadow.StatusOK, tDesired, map[fractal.PropertyName]string{
				nozzleTemp:       "200.0",
				nozzleTempTarget: "220.0",
			})
		}
	})
}

// TestExample4Invocation follows the fourth worked example. hq calls
// SetNozzleTemp on the printer and the printer runs it. The printer then sends
// up a state report and the answer to the call.
func TestExample4Invocation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()
		hqTrace, brnoTrace := n.hqLink.Watch(), n.brnoLink.Watch()
		n.clock.Set(tInvoked)

		res := call(t, n, `250.0`, tInvoked+callWindow)
		if res.Failed() {
			t.Fatalf("the call failed: %v", res.Error)
		}
		if string(res.Result) != "true" {
			t.Errorf("the result of the call is %s, want true", res.Result)
		}
		if res.Receiver != hqPath {
			t.Errorf("the receiver of the answer is %s, want the caller %s", res.Receiver, hqPath)
		}
		synctest.Wait()

		wantKinds(t, hqTrace.Down(), fractal.KindRPC)
		wantKinds(t, brnoTrace.Down(), fractal.KindRPC)
		wantKinds(t, brnoTrace.Up(), fractal.KindREP, fractal.KindRES)
		wantREP(t, brnoTrace.Up()[0], fractal.REP{
			Version:   fractal.Version,
			Sender:    printerPath,
			Interface: printerInterface,
			Instance:  printerInstance,
			Timestamp: tInvoked,
			Props:     fractal.Props{nozzleTempTarget: json.RawMessage(`250.0`)},
		})
		if got := n.owner.Executions(setNozzleTemp); got != 1 {
			t.Errorf("the method ran %d times, want once", got)
		}
		for _, snap := range []shadow.Snapshot{shadowOf(t, n.hq), shadowOf(t, n.brno)} {
			wantShadow(t, snap, shadow.StatusOK, tInvoked, map[fractal.PropertyName]string{
				nozzleTempTarget: "250.0",
			})
		}
	})
}

// TestExample5ConnectionLoss follows the fifth worked example. The link
// between brno and the printer goes down. brno marks the printer's interface
// in its shadow as stale, which means the values may be out of date. It sends
// a loss message to hq, and hq marks the same interface in its own shadow.
func TestExample5ConnectionLoss(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()
		n.clock.Set(tInvoked)
		report(t, n, fractal.Props{nozzleTempTarget: json.RawMessage(`250.0`)}, tInvoked)
		synctest.Wait()

		hqTrace := n.hqLink.Watch()
		n.clock.Set(tLost)
		n.brnoLink.Cut(t.Context())
		synctest.Wait()

		wantKinds(t, hqTrace.Up(), fractal.KindLOS)
		var los fractal.LOS
		decode(t, hqTrace.Up()[0].Payload, &los)
		if los.Sender != brnoPath || los.Child != printerID {
			t.Errorf("the loss message has the sender %s and the child %s, want %s and %s",
				los.Sender, los.Child, brnoPath, printerID)
		}
		for _, snap := range []shadow.Snapshot{shadowOf(t, n.hq), shadowOf(t, n.brno)} {
			wantShadow(t, snap, shadow.StatusStale, tInvoked, map[fractal.PropertyName]string{
				nozzleTempTarget: "250.0",
			})
		}
	})
}

// TestExample6Reconnection follows the sixth worked example. The link to the
// printer comes back and brno asks for a full state report. The shadow of brno
// and the shadow of hq then have the new values and the status OK.
func TestExample6Reconnection(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()
		n.clock.Set(tLost)
		n.brnoLink.Cut(t.Context())
		synctest.Wait()

		brnoTrace := n.brnoLink.Watch()
		n.clock.Set(tReconnect)
		n.owner.Set(nozzleTemp, json.RawMessage(`230.0`))
		n.owner.Set(nozzleTempTarget, json.RawMessage(`250.0`))
		n.brnoLink.Restore(t.Context())
		synctest.Wait()

		wantKinds(t, brnoTrace.Down(), fractal.KindRBR)
		wantKinds(t, brnoTrace.Up(), fractal.KindREP)
		wantREP(t, brnoTrace.Up()[0], fractal.REP{
			Version:   fractal.Version,
			Sender:    printerPath,
			Interface: printerInterface,
			Instance:  printerInstance,
			Timestamp: tReconnect,
			Props: fractal.Props{
				nozzleTemp:       json.RawMessage(`230.0`),
				nozzleTempTarget: json.RawMessage(`250.0`),
			},
		})
		for _, snap := range []shadow.Snapshot{shadowOf(t, n.hq), shadowOf(t, n.brno)} {
			wantShadow(t, snap, shadow.StatusOK, tReconnect, map[fractal.PropertyName]string{
				nozzleTemp:       "230.0",
				nozzleTempTarget: "250.0",
			})
		}
	})
}

// TestExample7ExposedStateChange follows the seventh worked example. hq puts
// the site into maintenance. Site1 is exposed downward only, so hq sends no
// REP and one AREP. brno keeps nothing of the nodes above and passes the AREP
// on unchanged, and the printer stores it.
func TestExample7ExposedStateChange(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()
		hqTrace, brnoTrace := n.hqLink.Watch(), n.brnoLink.Watch()

		n.clock.Set(tSite)
		n.site.Set(maintenanceMode, json.RawMessage(`true`))
		if err := n.siteIf.Report(
			t.Context(),
			fractal.Props{maintenanceMode: json.RawMessage(`true`)},
			tSite,
		); err != nil {
			t.Fatalf("hq could not report the site: %v", err)
		}
		synctest.Wait()

		wantKinds(t, hqTrace.Up())
		wantKinds(t, hqTrace.Down(), fractal.KindAREP)
		wantKinds(t, brnoTrace.Down(), fractal.KindAREP)
		if string(brnoTrace.Down()[0].Payload) != string(hqTrace.Down()[0].Payload) {
			t.Error("the AREP brno passed on differs from the one it received, and a node must pass it on unchanged")
		}
		if got := n.brno.Ancestors().Len(); got != 0 {
			t.Errorf("brno keeps %d interfaces of the nodes above, want none", got)
		}
		wantShadow(t, ancestorOf(t, n.printer), shadow.StatusOK, tSite, map[fractal.PropertyName]string{
			maintenanceMode: "true",
		})
	})
}

// wantKinds checks that the link delivered, in one direction, messages of
// exactly these types in this order.
func wantKinds(t *testing.T, got []fractaltest.Received, want ...fractal.Kind) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the link delivered %d messages, want %d: it delivered %s", len(got), len(want), kindsOf(got))
	}
	for i, k := range want {
		if got[i].Kind != k {
			t.Errorf("message %d on the link is a %s, want a %s", i, got[i].Kind, k)
		}
	}
}

// wantKindsInAnyOrder checks that a trace holds exactly the message types
// want, in any order.
func wantKindsInAnyOrder(t *testing.T, got []fractaltest.Received, want ...fractal.Kind) {
	t.Helper()
	gotKinds := make([]fractal.Kind, len(got))
	for i, r := range got {
		gotKinds[i] = r.Kind
	}
	slices.Sort(gotKinds)
	want = slices.Sorted(slices.Values(want))
	if !slices.Equal(gotKinds, want) {
		t.Errorf("the link delivered %s, want %v in any order", kindsOf(got), want)
	}
}

// kindsOf joins the message types of a trace into one string for a failure
// message.
func kindsOf(got []fractaltest.Received) string {
	names := make([]string, len(got))
	for i, r := range got {
		names[i] = r.Kind.String()
	}
	return strings.Join(names, " ")
}

// wantREP decodes a state report and checks every field of it.
func wantREP(t *testing.T, got fractaltest.Received, want fractal.REP) {
	t.Helper()
	var m fractal.REP
	decode(t, got.Payload, &m)
	if m.Version != want.Version || m.Sender != want.Sender ||
		m.Interface != want.Interface || m.Instance != want.Instance || m.Timestamp != want.Timestamp {
		t.Errorf("the report is %+v, want %+v", m, want)
	}
	if len(m.Props) != len(want.Props) {
		t.Errorf("the report has %d properties, want %d", len(m.Props), len(want.Props))
	}
	for name, value := range want.Props {
		if string(m.Props[name]) != string(value) {
			t.Errorf("the report has %s = %s, want %s", name, m.Props[name], value)
		}
	}
}

// decode decodes the JSON payload of one message from a trace.
func decode(t *testing.T, payload []byte, into any) {
	t.Helper()
	if err := json.Unmarshal(payload, into); err != nil {
		t.Fatalf("decode the payload %s: %v", payload, err)
	}
}

// TestExample8LossOfAnAncestor follows the eighth worked example and the
// repair that ends it. The link between hq and brno is cut, so brno sends an
// ALOS and Site1 of hq goes STALE at the printer with the value and time it
// had. When the link comes back, brno sends an ARBR, hq answers it after its
// window with a full AREP, and the printer's copy is OK again.
func TestExample8LossOfAnAncestor(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()
		n.clock.Set(tSite)
		n.site.Set(maintenanceMode, json.RawMessage(`true`))
		if err := n.siteIf.Report(
			t.Context(),
			fractal.Props{maintenanceMode: json.RawMessage(`true`)},
			tSite,
		); err != nil {
			t.Fatalf("hq could not report the site: %v", err)
		}
		// The ARBR brno sent at the start holds back its next one this long.
		time.Sleep(node.DefaultARBRMinInterval)
		synctest.Wait()
		hqTrace, brnoTrace := n.hqLink.Watch(), n.brnoLink.Watch()

		n.clock.Set(tAncestorLost)
		n.hqLink.Cut(t.Context())
		synctest.Wait()

		wantKinds(t, brnoTrace.Down(), fractal.KindALOS)
		var loss fractal.ALOS
		decode(t, brnoTrace.Down()[0].Payload, &loss)
		if want := (fractal.ALOS{Version: fractal.Version, Sender: brnoPath}); loss != want {
			t.Errorf("brno sent %+v, want %+v", loss, want)
		}
		wantShadow(t, ancestorOf(t, n.printer), shadow.StatusStale, tSite, map[fractal.PropertyName]string{
			maintenanceMode: "true",
		})

		// A link that comes back also brings hq's RBR and brno's REP that
		// answers it. Two goroutines of brno send the ARBR and the REP, and
		// the specification orders neither before the other.
		n.hqLink.Restore(t.Context())
		synctest.Wait()
		wantKindsInAnyOrder(t, hqTrace.Up(), fractal.KindARBR, fractal.KindREP)
		time.Sleep(node.DefaultARBRWindow)
		synctest.Wait()

		wantKinds(t, hqTrace.Down(), fractal.KindRBR, fractal.KindAREP)
		wantShadow(t, ancestorOf(t, n.printer), shadow.StatusOK, tAncestorLost, map[fractal.PropertyName]string{
			maintenanceMode: "true",
		})
	})
}
