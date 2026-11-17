package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// The connector a registration test needs: token admission and no
// auto-approval, so a printer that dials waits for somebody to decide. It is
// the shipping dialect with the two settings a farm runs it under.
const decidedAdmission = `
  connectors:
    xbuddy:
      type: xbuddy
      config:
        mode: token
        auto_register: false`

// registrationRow is one row of /api/v1/registrations.
type registrationRow struct {
	Code        string `json:"code"`
	Connector   string `json:"connector"`
	Fingerprint string `json:"fingerprint"`
	Serial      string `json:"serial"`
	Model       string `json:"model"`
	Approved    bool   `json:"approved"`
}

// TestAnApprovedPrinterIsStillApprovedAfterAPowerCut is finding CL-02 written
// as the sequence that found it.
//
// A registration table lived in the dialect's memory. A restart forgot every
// registered printer, so an approved machine came back presenting a token
// nothing had issued any more, was refused on its own wire, and a whole farm
// had to be approved again by hand.
//
// The binary is killed rather than stopped, because a clean shutdown is not the
// case that matters: what is on disk when the power goes is what a restart has
// to be able to read.
func TestAnApprovedPrinterIsStillApprovedAfterAPowerCut(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := startServerIn(t, dir, decidedAdmission, "")

	const (
		model  = "MK4"
		serial = "SIM-REGISTRATION-RESTART"
	)

	printer, err := simulator.New(simulator.Config{
		Model: model, Serial: serial, BaseURL: first.baseURL,
	})
	if err != nil {
		t.Fatalf("new %s: %v", model, err)
	}

	// The printer registers and then polls. Nothing has approved it, so the
	// handshake does not finish until the operator below decides.
	handshake := make(chan error, 1)
	registering, stopRegistering := context.WithCancel(context.WithoutCancel(t.Context()))

	t.Cleanup(stopRegistering)

	go func() { handshake <- printer.Register(registering) }()

	var waiting registrationRow

	eventually(t, "the printer appeared on /api/v1/registrations", func() bool {
		rows := first.registrations(t)
		if len(rows) != 1 {
			return false
		}

		waiting = rows[0]

		return waiting.Code != ""
	})

	if waiting.Serial != serial {
		t.Fatalf("the waiting registration reports serial %q, and the printer sent %q",
			waiting.Serial, serial)
	}

	approved := first.do(t, http.MethodPost,
		"/api/v1/registrations/"+waiting.Code+"/approve", "", nil)
	if approved.status != http.StatusOK {
		t.Fatalf("approving %s answered %d: %s", waiting.Code, approved.status, approved.body)
	}

	select {
	case err = <-handshake:
		if err != nil {
			t.Fatalf("the approved printer's handshake failed: %v", err)
		}
	case <-time.After(settleBudget):
		t.Fatalf("the printer's handshake did not finish within %s of the approval", settleBudget)
	}

	token := printer.Token()
	if token == "" {
		t.Fatal("the handshake finished and the printer holds no token")
	}

	// It dials, and it is on the fleet.
	first.runPrinter(t, printer)

	if _, found := first.findPrinter(t, serial); !found {
		t.Fatalf("the approved printer is not on /api/v1/devices:\n%s", first.stderr.String())
	}

	first.kill(t)
	stopRegistering()

	// The power comes back: the same directory, so the same database, and a
	// process that has never heard of this printer except through what is
	// stored.
	second := startServerIn(t, dir, decidedAdmission, "")

	if rows := second.registrations(t); len(rows) != 0 {
		t.Fatalf("an approved registration is waiting for a decision again: %+v", rows)
	}

	returning, err := simulator.New(simulator.Config{
		Model: model, Serial: serial, BaseURL: second.baseURL,
	})
	if err != nil {
		t.Fatalf("new %s: %v", model, err)
	}

	// The printer presents the token it was issued before the power cut. It is
	// supplied out of band because that is what the machine does: the firmware
	// keeps its token and dials, and it registers again only when refused.
	returning.SetToken(token)

	second.runPrinter(t, returning)

	view, found := second.findPrinter(t, serial)
	if !found {
		t.Fatalf("the printer approved before the restart was not admitted after it:\n%s",
			second.stderr.String())
	}

	t.Logf("observed: after a power cut, %s dialled with the token it already held and was "+
		"admitted as %s with no second approval", serial, view.ID)
}

// TestARejectedRegistrationIsGoneAfterARestart is the other half of the same
// row: a decision to turn a machine away has to survive too, or a rejected
// printer comes back approved.
func TestARejectedRegistrationIsGoneAfterARestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := startServerIn(t, dir, decidedAdmission, "")

	printer, err := simulator.New(simulator.Config{
		Model: "MK4", Serial: "SIM-REGISTRATION-REJECTED", BaseURL: first.baseURL,
	})
	if err != nil {
		t.Fatalf("new printer: %v", err)
	}

	registering, stopRegistering := context.WithCancel(context.WithoutCancel(t.Context()))
	defer stopRegistering()

	go func() { _ = printer.Register(registering) }()

	var waiting registrationRow

	eventually(t, "the printer appeared on /api/v1/registrations", func() bool {
		rows := first.registrations(t)
		if len(rows) != 1 {
			return false
		}

		waiting = rows[0]

		return true
	})

	rejected := first.do(t, http.MethodDelete, "/api/v1/registrations/"+waiting.Code, "", nil)
	if rejected.status != http.StatusNoContent {
		t.Fatalf("rejecting %s answered %d: %s", waiting.Code, rejected.status, rejected.body)
	}

	stopRegistering()
	first.kill(t)

	second := startServerIn(t, dir, decidedAdmission, "")

	if rows := second.registrations(t); len(rows) != 0 {
		t.Fatalf("a rejected registration came back after a restart: %+v", rows)
	}

	t.Log("observed: a rejected registration is not in the table the next process reads")
}

// registrations reads the operator's listing of what is waiting.
func (s *server) registrations(t *testing.T) []registrationRow {
	t.Helper()

	listed := s.get(t, "/api/v1/registrations")
	if listed.status != http.StatusOK {
		t.Fatalf("/api/v1/registrations answered %d: %s", listed.status, listed.body)
	}

	var body struct {
		Items []registrationRow `json:"items"`
	}

	if err := json.Unmarshal(listed.body, &body); err != nil {
		t.Fatalf("parse the registration listing: %v", err)
	}

	return body.Items
}

// runPrinter puts an admitted printer on the wire and waits for it to appear on
// the fleet, stopping it when the test ends.
func (s *server) runPrinter(t *testing.T, printer *simulator.Printer) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))

	running := make(chan error, 1)
	go func() { running <- printer.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		<-running
	})

	eventually(t, "the printer appeared on /api/v1/devices", func() bool {
		view, found := s.findPrinter(t, printer.Serial())

		return found && view.Connected
	})
}
