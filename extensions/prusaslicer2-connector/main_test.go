package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/prusa3d/prusaslicer2-connector/internal/link"
)

// Each refusal of the device door reaches the link package as the refusal it
// answers PrusaSlicer with, and keeps the door's error for the log.
func TestADeviceRefusalBecomesTheLinkRefusal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		door, want error
	}{
		{deviceapi.ErrNoDevice, link.ErrNoPrinter},
		{deviceapi.ErrInvalidQuery, link.ErrNoPrinter},
		{deviceapi.ErrDenied, link.ErrDenied},
		{deviceapi.ErrNameTaken, link.ErrNameTaken},
		{deviceapi.ErrNotReady, link.ErrNotReady},
		{deviceapi.ErrPrintRefused, link.ErrPrintRefused},
		{deviceapi.ErrPrintFailed, link.ErrPrintRefused},
	} {
		t.Run(tc.door.Error(), func(t *testing.T) {
			t.Parallel()

			got := deviceRefusal(fmt.Errorf("device.print: %w", tc.door))
			if !errors.Is(got, tc.want) || !errors.Is(got, tc.door) {
				t.Errorf("deviceRefusal = %v, want %v wrapping %v", got, tc.want, tc.door)
			}
		})
	}
}

// An error the link package has no refusal for passes through as it came.
func TestAnUnknownDeviceErrorPassesThrough(t *testing.T) {
	t.Parallel()

	door := fmt.Errorf("device.print: %w", deviceapi.ErrBusy)
	if got := deviceRefusal(door); !errors.Is(got, deviceapi.ErrBusy) || errors.Is(got, link.ErrNotReady) {
		t.Errorf("deviceRefusal = %v, want the error unchanged", got)
	}
}

func TestThePrinterStateIsTheReportedOneInUpperCase(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		device deviceapi.Device
		want   string
	}{
		{"connected", deviceapi.Device{Connected: true, State: map[string]json.RawMessage{
			"/state": json.RawMessage(`"idle"`),
		}}, "IDLE"},
		{"away", deviceapi.Device{Connected: false, State: map[string]json.RawMessage{
			"/state": json.RawMessage(`"IDLE"`),
		}}, "OFFLINE"},
		{"no state", deviceapi.Device{Connected: true}, "UNKNOWN"},
		{"not a string", deviceapi.Device{Connected: true, State: map[string]json.RawMessage{
			"/state": json.RawMessage(`3`),
		}}, "UNKNOWN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := stateOf(tc.device); got != tc.want {
				t.Errorf("stateOf = %q, want %q", got, tc.want)
			}
		})
	}
}

// caller answers every call with the same body or error.
type caller struct {
	body json.RawMessage
	err  error
}

func (c caller) Call(context.Context, wirerpc.Method, any) (json.RawMessage, error) {
	return c.body, c.err
}

// A host that does not let the module ask answers ErrDenied, which leaves the
// print itself as the check. Any other failure stops the print.
func TestMayPrintTellsACheckItMayNotAskFromAnAnswer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		conn    caller
		allowed bool
		wantErr error
	}{
		{"allowed", caller{body: json.RawMessage(`{"allowed":true}`)}, true, nil},
		{"refused", caller{body: json.RawMessage(`{"allowed":false}`)}, false, nil},
		{"not approved", caller{err: &wirerpc.Error{Code: accessapi.CodeDenied, Message: "no"}}, false, link.ErrCannotAsk},
		{"unknown subject", caller{err: &wirerpc.Error{Code: accessapi.CodeUnknownSubject, Message: "no"}},
			false, accessapi.ErrUnknownSubject},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := access{client: accessapi.NewClient(tc.conn)}.MayPrint(t.Context(), "user:alice", "mk4-01")
			if got != tc.allowed {
				t.Errorf("allowed = %v, want %v", got, tc.allowed)
			}

			switch {
			case tc.wantErr == nil && err != nil:
				t.Errorf("err = %v, want none", err)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			case !errors.Is(tc.wantErr, link.ErrCannotAsk) && errors.Is(err, link.ErrCannotAsk):
				t.Errorf("err = %v, which would skip the check", err)
			}
		})
	}
}

// recorder answers every call with body and keeps the params it was sent.
type recorder struct {
	body   json.RawMessage
	params []any
}

func (r *recorder) Call(_ context.Context, _ wirerpc.Method, params any) (json.RawMessage, error) {
	r.params = append(r.params, params)

	return r.body, nil
}

// A print asks the host to keep both files, so a printer that holds a file of
// the name gets the new one under a free name in one call.
func TestAPrintAsksTheHostToKeepBothFiles(t *testing.T) {
	t.Parallel()

	conn := &recorder{body: json.RawMessage(`{"transfer_id":"t1","path":"/usb/a[1].gcode"}`)}

	err := fleet{devices: deviceapi.NewClient(conn)}.
		Print(t.Context(), "user:alice", "mk4-01", "", "1-x/a.gcode", "/usb/a.gcode")
	if err != nil {
		t.Fatal(err)
	}

	if len(conn.params) != 1 {
		t.Fatalf("%d calls, want 1", len(conn.params))
	}

	sent, ok := conn.params[0].(deviceapi.PrintParams)
	if !ok || !sent.KeepBoth || sent.Storage != deviceapi.ModuleDataStorage || sent.Path != "/usb/a.gcode" {
		t.Fatalf("sent %#v", conn.params[0])
	}
}

// An Upload lands the file under a free name and has the host delete it from
// the print folder once it landed, and starts nothing.
func TestAnUploadAsksTheHostToLandTheFileAndGiveItAway(t *testing.T) {
	t.Parallel()

	conn := &recorder{body: json.RawMessage(`{"transfer_id":"t1","path":"/usb/a[1].gcode"}`)}

	err := fleet{devices: deviceapi.NewClient(conn)}.
		Send(t.Context(), "user:alice", "mk4-01", "", "1-x/a.gcode", "/usb/a.gcode")
	if err != nil {
		t.Fatal(err)
	}

	if len(conn.params) != 1 {
		t.Fatalf("%d calls, want 1", len(conn.params))
	}

	sent, ok := conn.params[0].(deviceapi.SendParams)
	if !ok || !sent.KeepBoth || !sent.DeleteSource || sent.Storage != deviceapi.ModuleDataStorage ||
		sent.Path != "/usb/a.gcode" {
		t.Fatalf("sent %#v", conn.params[0])
	}
}

// A file saved in a storage of this server is sent from there and stays.
func TestAKeptUploadLeavesTheFileInItsStorage(t *testing.T) {
	t.Parallel()

	conn := &recorder{body: json.RawMessage(`{"transfer_id":"t1","path":"/usb/a.gcode"}`)}

	err := fleet{devices: deviceapi.NewClient(conn)}.
		Send(t.Context(), "user:alice", "mk4-01", "local", "prints/a.gcode", "/usb/a.gcode")
	if err != nil {
		t.Fatal(err)
	}

	sent, ok := conn.params[0].(deviceapi.SendParams)
	if !ok || sent.DeleteSource || sent.Storage != "local" || sent.Source != "prints/a.gcode" {
		t.Fatalf("sent %#v", conn.params[0])
	}
}

// A config block written for 0.1.0 names storage, which 0.1.1 no longer
// reads. The host refuses a key the settings do not name, so the module
// still names it and ignores it, and an upgrade starts.
func TestASettingsBlockFrom010StillStartsTheModule(t *testing.T) {
	t.Parallel()

	_, err := wirerpc.ReadSettings[Settings](json.RawMessage(`{"config": {"storage": "shelf"}}`))
	if err != nil {
		t.Fatalf("read the 0.1.0 block: %v", err)
	}
}
