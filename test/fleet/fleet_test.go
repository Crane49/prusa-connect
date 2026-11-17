package fleet

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The fleet sizes. A design requirement is 1 to 400 printers, and this runs every
// model at once rather than four hundred of one, because the thing under test
// here is the driver's session map and the resolver's per-unit epoch table
// holding up while thirteen vocabularies are in flight together.
//
// The full fleet is the whole four hundred, spread over the models. It cannot
// be more: the driver keeps that many registrations, every one here is approved
// by an operator, and one an operator approved is never dropped to make room,
// so the four hundred and first is refused.
const (
	fastFleetPerModel = 4
	fullFleet         = 400
)

// fleetBudget bounds the whole run. It is virtual time.
const fleetBudget = time.Minute

// TestTheWholeFleetIsAdmitted runs every model at once against one driver and
// requires every printer to register, be admitted on the capability set its own
// model resolves to, and answer a command.
//
// It always runs. The set decides how many printers per model, so the fast lane
// still proves the fleet path works and the full lane proves it at scale.
func TestTheWholeFleetIsAdmitted(t *testing.T) {
	t.Parallel()

	cells := matrix(t, setFast)

	expected := len(cells) * fastFleetPerModel
	if selectedSet(t) == setFull {
		expected = fullFleet
	}

	synctest.Test(t, func(t *testing.T) {
		rig := newFleetRig(t)
		defer rig.stop(t)

		ctx, cancel := context.WithTimeout(t.Context(), fleetBudget)
		defer cancel()

		printers := rig.start(ctx, t, cells, expected)

		digests := map[string]string{}

		for range expected {
			select {
			case session := <-rig.host.sessions:
				identity := session.Identity()
				resolution := session.Resolution()

				if held, seen := digests[identity.Model]; seen && held != resolution.Digest {
					t.Errorf("two %s printers resolved to different capability sets, %s and %s",
						identity.Model, held, resolution.Digest)
				}

				digests[identity.Model] = resolution.Digest
				rig.sessions = append(rig.sessions, session)
				<-rig.host.spies
			case <-ctx.Done():
				t.Fatalf("%d of %d printers were admitted before the budget ran out",
					len(rig.sessions), expected)
			}
		}

		if len(digests) != len(cells) {
			t.Errorf("%d models were admitted, want %d", len(digests), len(cells))
		}

		answered := 0

		for _, session := range rig.sessions {
			resolved, _ := session.Capabilities()

			result, err := session.Do(ctx, connectorapi.Command{Verb: firstArgumentFreeVerb(t,
				Cell{Model: session.Identity().Model}, resolved)})
			if err != nil {
				t.Fatalf("%s: %v", session.Identity().Serial, err)
			}

			if !result.OK {
				t.Fatalf("%s answered a declared verb with a refusal: %s", session.Identity().Serial, result.Raw)
			}

			answered++
		}

		if answered != expected {
			t.Fatalf("%d of %d printers answered", answered, expected)
		}

		t.Logf("observed: %d printers across %d models registered, were admitted and answered a command",
			expected, len(cells))

		for _, printer := range printers {
			if printer.Sessions() != 1 {
				t.Errorf("%s held %d websocket sessions, want 1", printer.Serial(), printer.Sessions())
			}
		}
	})
}

// fleetRig is one driver serving many printers of many models.
type fleetRig struct {
	driver   *xbuddy.Driver
	host     *arrivals
	server   *http.Server
	listener *pipeListener
	sessions []*capability.Session
	running  []chan error
}

func newFleetRig(t *testing.T) *fleetRig {
	t.Helper()

	profiles, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the profiles: %v", err)
	}

	quiet := slog.New(slog.DiscardHandler)

	host := &arrivals{
		profiles: profiles,
		coverage: profiles.Coverage(),
		resolver: capability.NewResolver(profiles, quiet),
		sessions: make(chan *capability.Session, fullFleet),
		spies:    make(chan *driverSpy, fullFleet),
	}

	driver := xbuddy.NewDriver(xbuddy.ModeToken, queueDepth, 0, "", quiet)
	host.baseline = driver.Baseline()

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	listener := newPipeListener()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: handshakeBudget}

	go func() { _ = server.Serve(listener) }()

	return &fleetRig{driver: driver, host: host, server: server, listener: listener}
}

// start registers and runs total printers spread over the cells' models, the
// first models taking one more when it does not divide.
func (f *fleetRig) start(ctx context.Context, t *testing.T, cells []Cell, total int) []*simulator.Printer {
	t.Helper()

	printers := make([]*simulator.Printer, 0, total)

	for at, cell := range cells {
		perModel := total / len(cells)
		if at < total%len(cells) {
			perModel++
		}

		for i := range perModel {
			printer, err := simulator.New(simulator.Config{
				Model:    cell.Model,
				Firmware: cell.Firmware,
				Serial:   fmt.Sprintf("SIM-%s-%04d", cell.Model, i),
				BaseURL:  "http://xbuddy-host",
				Dial:     f.listener.dial,
			})
			if err != nil {
				t.Fatalf("new printer for %s: %v", cell, err)
			}

			// Mode token waits for an operator, so each printer polls on its
			// own goroutine while this approves the code it was given.
			registered := make(chan error, 1)
			go func() { registered <- printer.Register(ctx) }()

			f.approve(ctx, t, printer.Fingerprint())

			if err = <-registered; err != nil {
				t.Fatalf("%s: register: %v", cell, err)
			}

			done := make(chan error, 1)
			f.running = append(f.running, done)

			go func() { done <- printer.Run(ctx) }()

			printers = append(printers, printer)
		}
	}

	return printers
}

// approve lets one printer's registration through, which is what an operator
// does at /api/v1/registrations. A whole fleet registers at once here, so the
// code approved is the one this printer is polling on and not the first waiting.
func (f *fleetRig) approve(ctx context.Context, t *testing.T, fingerprint string) {
	t.Helper()

	const attempts = 1000

	for range attempts {
		for _, waiting := range f.driver.Pending() {
			if waiting.Fingerprint != fingerprint {
				continue
			}

			if _, err := f.driver.Approve(ctx, waiting.Code, "user:tester"); err != nil {
				t.Fatalf("approve %s: %v", waiting.Code, err)
			}

			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatalf("no registration was waiting for the printer %s", fingerprint)
}

// stop closes every session the host was handed, including those the test
// never took because it failed first. Nothing else ends them: the driver runs
// with no grace, so a session outlives its printer until the host closes it,
// and one left open is what testing/synctest reports as a deadlock in place of
// the failure. The wait is what makes the drain complete, since a printer that
// connected just before the test failed may still be on its way to Arrived.
func (f *fleetRig) stop(t *testing.T) {
	t.Helper()

	synctest.Wait()

	for len(f.host.sessions) > 0 {
		f.sessions = append(f.sessions, <-f.host.sessions)
	}

	for _, session := range f.sessions {
		if err := session.Close(); err != nil {
			t.Errorf("close a session: %v", err)
		}
	}

	for _, done := range f.running {
		<-done
	}

	_ = f.server.Close()
	_ = f.listener.Close()
}
