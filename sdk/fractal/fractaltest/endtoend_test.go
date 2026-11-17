package fractaltest_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// TestAnswerCrossesACutLinkOnce cuts the link between hq and brno while the
// printer runs a method that hq called. The answer waits in the queue of that
// link and reaches hq when the link returns. hq processes one answer and the
// method runs once (fractal.md, Method Invocation Result Message).
func TestAnswerCrossesACutLinkOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()

		release := make(chan struct{})
		n.owner.Handle(setNozzleTemp, func(context.Context, []json.RawMessage) (json.RawMessage, error) {
			<-release
			return json.RawMessage(`true`), nil
		})
		n.clock.Set(tInvoked)
		answers := make(chan fractal.RES, 1)
		failures := make(chan error, 1)
		go func() {
			res, err := n.hq.Call(t.Context(), node.CallRequest{
				Target:    printerPath,
				Interface: printerInterface,
				Instance:  printerInstance,
				Method:    setNozzleTemp,
				Params:    []json.RawMessage{json.RawMessage(`250.0`)},
				Expires:   tInvoked + callWindow,
			})
			if err != nil {
				failures <- err
				return
			}
			answers <- res
		}()
		synctest.Wait()

		n.hqLink.Cut(t.Context())
		close(release)
		synctest.Wait()
		if got := n.hqCount.Messages(fractal.KindRES, node.OutcomeProcessed); got != 0 {
			t.Fatalf("hq processed %d answers while its link to brno was down, want 0", got)
		}

		n.hqLink.Restore(t.Context())
		synctest.Wait()
		select {
		case err := <-failures:
			t.Fatalf("the call failed: %v", err)
		case res := <-answers:
			if string(res.Result) != "true" {
				t.Errorf("the result of the call is %s, want true", res.Result)
			}
		default:
			t.Fatal("the call at hq has no answer after the link to brno returned")
		}
		if got := n.hqCount.Messages(fractal.KindRES, node.OutcomeProcessed); got != 1 {
			t.Errorf("hq processed %d answers, want 1", got)
		}
		if got := n.owner.Executions(setNozzleTemp); got != 1 {
			t.Errorf("the method ran %d times, want 1", got)
		}
		if got := n.hq.PendingCalls(); got != 0 {
			t.Errorf("hq still waits for the answer to %d calls, want 0", got)
		}
	})
}

// TestAQueuedInvocationExpires sends a method invocation while the link
// between brno and the printer is down, so the invocation waits in the queue
// of that link. The test sets the clock past the Expires time of the
// invocation and restores the link. The printer must answer with the expiry
// code and must not run the method (fractal.md, Method Invocation Request
// Message, check expiry).
func TestAQueuedInvocationExpires(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()

		n.clock.Set(tInvoked)
		n.brnoLink.Cut(t.Context())
		answers := make(chan fractal.RES, 1)
		failures := make(chan error, 1)
		go func() {
			res, err := n.hq.Call(t.Context(), node.CallRequest{
				Target:    printerPath,
				Interface: printerInterface,
				Instance:  printerInstance,
				Method:    setNozzleTemp,
				Params:    []json.RawMessage{json.RawMessage(`250.0`)},
				Expires:   tInvoked + callWindow,
			})
			if err != nil {
				failures <- err
				return
			}
			answers <- res
		}()
		synctest.Wait()

		n.clock.Set(tInvoked + callWindow + 1)
		n.brnoLink.Restore(t.Context())
		synctest.Wait()

		select {
		case err := <-failures:
			t.Fatalf("the call failed: %v", err)
		case res := <-answers:
			if res.Error == nil || !res.Error.Code.Is(fractal.CodeExpired) {
				t.Errorf("the answer has the error %+v, want the code fractal.CodeExpired", res.Error)
			}
		default:
			t.Fatal("the call at hq has no answer after the link to the printer returned")
		}
		if got := n.owner.Executions(setNozzleTemp); got != 0 {
			t.Errorf("the method ran %d times, want 0: the invocation had expired", got)
		}
	})
}

// TestARepeatedInvocationRunsOnce stops the printer's acknowledgement of a
// method invocation from reaching the transport, so the transport delivers the
// invocation again. The printer must send the answer it stored and must not
// run the method a second time (fractal.md, Method Invocation Request Message,
// check dedup).
func TestARepeatedInvocationRunsOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()
		n.printerAck.swallow.Store(true)

		n.clock.Set(tInvoked)
		res := call(t, n, `250.0`, tInvoked+callWindow)
		if res.Failed() {
			t.Fatalf("the call failed: %v", res.Error)
		}
		synctest.Wait()

		n.brnoLink.Cut(t.Context())
		n.brnoLink.Restore(t.Context())
		synctest.Wait()

		if got := n.owner.Executions(setNozzleTemp); got != 1 {
			t.Errorf("the method ran %d times, want 1", got)
		}
		if got := n.brnoLink.Downward().Redelivered; got < 1 {
			t.Errorf("the link delivered the invocation again %d times, want at least 1", got)
		}
		// The printer sent the stored answer a second time. The link did not
		// deliver it, because it had seen the id of that answer inside the
		// deduplication window. So hq receives one answer, however often the
		// transport delivers the invocation (fractal-nats.md, Idempotent RES on
		// redelivery).
		if got := n.brnoLink.Upward().Deduplicated; got < 1 {
			t.Errorf("the link deduplicated %d repeated answers, want at least 1", got)
		}
		if got := n.hqCount.Messages(fractal.KindRES, node.OutcomeProcessed); got != 1 {
			t.Errorf("hq processed %d answers, want 1", got)
		}
	})
}

// TestASubtreeGoesStaleAtEveryAncestor cuts the link between hq and brno. hq
// must mark as stale the interface of the printer, a node below brno, in its
// shadow. The shadow of brno stays OK, because brno still has its link to the
// printer (fractal.md, Connection Loss Message).
func TestASubtreeGoesStaleAtEveryAncestor(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()
		wantShadow(t, shadowOf(t, n.hq), shadow.StatusOK, tStartup, nil)

		n.hqLink.Cut(t.Context())
		synctest.Wait()

		wantShadow(t, shadowOf(t, n.hq), shadow.StatusStale, tStartup, map[fractal.PropertyName]string{
			nozzleTemp: "25.0",
		})
		wantShadow(t, shadowOf(t, n.brno), shadow.StatusOK, tStartup, nil)
	})
}

// TestAReconnectResynchronisesTheShadow changes nozzleTemp on the printer while
// the link between hq and brno is down, so hq misses the report. When the link
// returns, hq asks for a full state report. The shadow of hq must then have
// the new value and the status OK (fractal.md, Synchronization of the Shadow -
// Reaction to Connection Loss and Reconnection).
func TestAReconnectResynchronisesTheShadow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()

		n.clock.Set(tLost)
		n.hqLink.Cut(t.Context())
		synctest.Wait()
		n.clock.Set(tHeating)
		n.owner.Set(nozzleTemp, json.RawMessage(`200.0`))
		report(t, n, fractal.Props{nozzleTemp: json.RawMessage(`200.0`)}, tHeating)
		synctest.Wait()
		wantShadow(t, shadowOf(t, n.hq), shadow.StatusStale, tStartup, map[fractal.PropertyName]string{
			nozzleTemp: "25.0",
		})

		n.clock.Set(tReconnect)
		n.hqLink.Restore(t.Context())
		synctest.Wait()

		wantShadow(t, shadowOf(t, n.hq), shadow.StatusOK, tReconnect, map[fractal.PropertyName]string{
			nozzleTemp:       "200.0",
			nozzleTempTarget: "null",
		})
	})
}

// TestAForgedSenderIsDropped sends, over the printer's link, a state report
// whose sender is the Node Path of another node. brno must drop the report: it
// does not update its shadow and sends nothing to hq (fractal.md, Sender
// Validation).
func TestAForgedSenderIsDropped(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()
		trace := n.hqLink.Watch()

		forged, err := json.Marshal(fractal.REP{
			Version:   fractal.Version,
			Sender:    "hq/brno/other-printer",
			Interface: printerInterface,
			Instance:  printerInstance,
			Timestamp: tHeating,
			Props:     fractal.Props{nozzleTemp: json.RawMessage(`999.0`)},
		})
		if err != nil {
			t.Fatalf("encode the forged report: %v", err)
		}
		transport := n.fabric.Transport(printerPath)
		if sendErr := transport.SendBestEffort(t.Context(), node.Dispatch{
			Kind:    fractal.KindREP,
			Payload: forged,
		}); sendErr != nil {
			t.Fatalf("send the forged report: %v", sendErr)
		}
		synctest.Wait()

		if got := n.brnoCount.Drops(fractal.KindREP, node.DropSenderInvalid); got != 1 {
			t.Errorf("brno dropped %d reports with the reason DropSenderInvalid, want 1", got)
		}
		if got := len(trace.Up()); got != 0 {
			t.Errorf("brno sent %d messages up to hq, want 0", got)
		}
	})
}

// TestASiblingCannotBeAddressed checks that Call and Desire on brno return
// node.ErrNotBelow for a target that is not below brno. It also checks that hq
// refuses, with the not-found code, a target whose path goes through a node
// that does not exist below hq (fractal.md, Message Routing (Unicast)).
func TestASiblingCannotBeAddressed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		target fractal.NodePath
	}{
		{"a sibling", "hq/praha"},
		{"a node below a sibling", "hq/praha/printer-9"},
		{"the node above", hqPath},
		{"itself", brnoPath},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				n := newNetwork(t)
				n.startAll(t)
				synctest.Wait()

				_, err := n.brno.Call(t.Context(), node.CallRequest{
					Target:    c.target,
					Interface: printerInterface,
					Instance:  printerInstance,
					Method:    setNozzleTemp,
					Expires:   tInvoked + callWindow,
				})
				if !errors.Is(err, node.ErrNotBelow) {
					t.Errorf("Call with the target %s returned %v, want node.ErrNotBelow", c.target, err)
				}
				err = n.brno.Desire(t.Context(), node.DesireRequest{
					Target:    c.target,
					Interface: printerInterface,
					Instance:  printerInstance,
					Props:     fractal.Props{nozzleTempTarget: json.RawMessage(`1.0`)},
				})
				if !errors.Is(err, node.ErrNotBelow) {
					t.Errorf("Desire with the target %s returned %v, want node.ErrNotBelow", c.target, err)
				}
			})
		})
	}
	t.Run("a path through a node that does not exist below hq", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			n := newNetwork(t)
			n.startAll(t)
			synctest.Wait()

			err := n.hq.Desire(t.Context(), node.DesireRequest{
				Target:    "hq/praha/printer-9",
				Interface: printerInterface,
				Instance:  printerInstance,
				Props:     fractal.Props{nozzleTempTarget: json.RawMessage(`1.0`)},
			})
			var carried *fractal.Error
			if !errors.As(err, &carried) || !carried.Code.Is(fractal.CodeNodeNotFound) {
				t.Errorf("Desire returned %v, want a fractal.Error with the code CodeNodeNotFound", err)
			}
		})
	})
}

// TestARevokedNodeForgetsItsAncestors has the printer forget its ancestor
// shadow, as a node does when it learns that its admission was revoked. A
// later AREP from hq then creates nothing (fractal.md, Admission and
// Revocation).
func TestARevokedNodeForgetsItsAncestors(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		n.startAll(t)
		synctest.Wait()
		reportSite := func(at fractal.Timestamp) {
			t.Helper()
			n.clock.Set(at)
			props := fractal.Props{maintenanceMode: json.RawMessage(`true`)}
			if err := n.siteIf.Report(t.Context(), props, at); err != nil {
				t.Fatalf("hq could not report the site: %v", err)
			}
			synctest.Wait()
		}
		reportSite(tSite)
		wantShadow(t, ancestorOf(t, n.printer), shadow.StatusOK, tSite, nil)

		n.printer.ForgetAncestors(t.Context())
		reportSite(tAncestorLost)

		if got := n.printer.Ancestors().Len(); got != 0 {
			t.Errorf("the printer keeps %d interfaces of the nodes above after it forgot them, want none", got)
		}
	})
}
