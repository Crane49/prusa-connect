package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
)

// seen returns a fixed time, so a test can compare the time in a status.
func seen() time.Time {
	return time.Date(2026, time.September, 21, 9, 41, 12, 0, time.UTC)
}

// TestTheReadingSaysWhatTheConnectionIsDoing: reading converts every state of
// the connection to the server above into the linkapi state of the same name.
// A refused join keeps the denial code and the reason.
func TestTheReadingSaysWhatTheConnectionIsDoing(t *testing.T) {
	t.Parallel()

	when := seen()

	for name, tc := range map[string]struct {
		uplink natsbind.Uplink
		want   *linkapi.Uplink
	}{
		"nothing to join": {
			uplink: natsbind.Uplink{State: natsbind.UplinkNotConfigured},
			want:   nil,
		},
		"asked and waiting": {
			uplink: natsbind.Uplink{Address: "nats://hq:4222", State: natsbind.UplinkJoining},
			want:   &linkapi.Uplink{Address: "nats://hq:4222", State: linkapi.UplinkJoining},
		},
		"joined and up": {
			uplink: natsbind.Uplink{
				Address: "nats://hq:4222", State: natsbind.UplinkConnected, ConnectedAt: &when,
			},
			want: &linkapi.Uplink{
				Address: "nats://hq:4222", State: linkapi.UplinkConnected, ConnectedAt: &when,
			},
		},
		"was up and is not now": {
			uplink: natsbind.Uplink{
				Address: "nats://hq:4222", State: natsbind.UplinkLost, ConnectedAt: &when,
			},
			want: &linkapi.Uplink{
				Address: "nats://hq:4222", State: linkapi.UplinkLost, ConnectedAt: &when,
			},
		},
		"refused": {
			uplink: natsbind.Uplink{
				Address: "nats://hq:4222",
				State:   natsbind.UplinkDenied,
				Reason:  "this node has been revoked",
				Code:    fractal.CodeJoinRevoked,
			},
			want: &linkapi.Uplink{
				Address:   "nats://hq:4222",
				State:     linkapi.UplinkDenied,
				Reason:    "this node has been revoked",
				Code:      int(fractal.CodeJoinRevoked),
				FaultCode: JoinDenied.Code().String(),
			},
		},
		"this node refused the grant": {
			uplink: natsbind.Uplink{
				Address: "nats://hq:4222",
				State:   natsbind.UplinkDenied,
				Reason:  "this node refused the grant of the node above: the grant names an unsupported protocol version",
				Refused: true,
			},
			want: &linkapi.Uplink{
				Address:   "nats://hq:4222",
				State:     linkapi.UplinkDenied,
				Reason:    "this node refused the grant of the node above: the grant names an unsupported protocol version",
				FaultCode: GrantRefused.Code().String(),
			},
		},
		"the saved refusal cannot be read": {
			uplink: natsbind.Uplink{
				Address:    "nats://hq:4222",
				State:      natsbind.UplinkDenied,
				Reason:     "the saved refusal cannot be read: unexpected end of JSON input",
				Unreadable: true,
			},
			want: &linkapi.Uplink{
				Address:   "nats://hq:4222",
				State:     linkapi.UplinkDenied,
				Reason:    "the saved refusal cannot be read: unexpected end of JSON input",
				FaultCode: RefusalUnreadable.Code().String(),
			},
		},
		"refused and the server above said nothing": {
			uplink: natsbind.Uplink{
				Address: "nats://hq:4222",
				State:   natsbind.UplinkDenied,
				Code:    fractal.CodeJoinRevoked,
			},
			want: &linkapi.Uplink{
				Address:   "nats://hq:4222",
				State:     linkapi.UplinkDenied,
				Reason:    JoinDenied.New("").Title(),
				Code:      int(fractal.CodeJoinRevoked),
				FaultCode: JoinDenied.Code().String(),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			held := reading(natsbind.Status{Uplink: tc.uplink}, "")

			if tc.want == nil {
				if held.Uplink != nil {
					t.Fatalf("the status has an uplink and the settings name no server above: %+v", held.Uplink)
				}

				return
			}

			if held.Uplink == nil {
				t.Fatal("the status has no uplink")
			}

			if *held.Uplink != *tc.want {
				t.Fatalf("the uplink is %+v, want %+v", *held.Uplink, *tc.want)
			}

			if err := held.Validate(); err != nil {
				t.Fatalf("linkapi.Status.Validate refused the status: %v", err)
			}
		})
	}
}

// TestTheReadingSaysWhatEveryServerBelowMayDo: reading converts the admission
// of each server below into the linkapi admission of the same name. An
// admission it does not know becomes pending.
func TestTheReadingSaysWhatEveryServerBelowMayDo(t *testing.T) {
	t.Parallel()

	when := seen()

	held := reading(natsbind.Status{
		NodeID:    "workshop",
		NodePath:  "hq/workshop",
		Identity:  "UAJWORKSHOP",
		Accepting: true,
		Nodes: []natsbind.Below{
			{ID: "bench", Path: "hq/workshop/bench", Admission: natsbind.AdmissionPending},
			{
				ID: "cell", Path: "hq/workshop/cell", Admission: natsbind.AdmissionAdmitted,
				Identity: "UAJCELL", Connected: true, LastHeartbeat: &when,
			},
			{ID: "old", Path: "hq/workshop/old", Admission: natsbind.AdmissionRevoked},
			{ID: "odd", Path: "hq/workshop/odd", Admission: natsbind.Admission("something else")},
		},
		Listeners: []natsbind.Listener{
			{Protocol: "nats", Address: "0.0.0.0", Port: 4222},
			{Protocol: "nats-leaf", Address: "0.0.0.0", Port: 5222},
		},
	}, linkapi.AdmitsExplicit)

	if held.NodeID != "workshop" || held.NodePath != "hq/workshop" ||
		held.PublicKey != "UAJWORKSHOP" || !held.Accepting {
		t.Fatalf("the node id, the node path, the public key or Accepting is wrong: %+v", held)
	}

	want := []linkapi.Admission{
		linkapi.AdmissionPending,
		linkapi.AdmissionAdmitted,
		linkapi.AdmissionRevoked,
		linkapi.AdmissionPending,
	}

	if len(held.Nodes) != len(want) {
		t.Fatalf("the status has %d servers below, want %d", len(held.Nodes), len(want))
	}

	for i, admission := range want {
		if held.Nodes[i].Admission != admission {
			t.Errorf("the admission of %s is %q, want %q", held.Nodes[i].ID, held.Nodes[i].Admission, admission)
		}
	}

	if held.Nodes[1].LastSeen == nil || !held.Nodes[1].Connected {
		t.Error("the server cell is connected and sent a heartbeat, " +
			"and the status has no LastSeen or Connected is false")
	}

	if held.Nodes[1].RoundTripMS != nil {
		t.Error("the status has a round-trip time, and a Fractal heartbeat goes one way so none can be measured")
	}

	if len(held.Listeners) != 2 {
		t.Errorf("the status has %d listeners, want 2", len(held.Listeners))
	}

	if err := held.Validate(); err != nil {
		t.Fatalf("linkapi.Status.Validate refused the status: %v", err)
	}
}

// TestHowThisServerDecidesAboutOneAskingToJoin: admitsOf returns "explicit" for
// a server with ask_first, "inherit" for a server that admits every server
// that proves its key, and the empty string for a server that accepts none.
func TestHowThisServerDecidesAboutOneAskingToJoin(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status natsbind.Status
		want   linkapi.Admits
	}{
		"takes nobody":         {status: natsbind.Status{}, want: ""},
		"takes whoever proves": {status: natsbind.Status{Accepting: true}, want: linkapi.AdmitsInherit},
		"holds for a person": {
			status: natsbind.Status{Accepting: true, AsksFirst: true},
			want:   linkapi.AdmitsExplicit,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if held := admitsOf(tc.status); held != tc.want {
				t.Fatalf("admitsOf returned %q, want %q", held, tc.want)
			}
		})
	}
}

// TestAReportOfANewReadingIsSentOnce: reportEvery sends the whole status to the
// host once, and does nothing when there is no reporter.
func TestAReportOfANewReadingIsSentOnce(t *testing.T) {
	t.Parallel()

	told := &reports{}

	reportEvery(t.Context(), told, linkapi.Status{NodeID: "workshop"}, time.Second)
	reportEvery(t.Context(), nil, linkapi.Status{NodeID: "workshop"}, time.Second)

	if len(told.seen) != 1 || told.seen[0].Status.NodeID != "workshop" {
		t.Fatalf("the host received %+v, want one report for workshop", told.seen)
	}
}

// reports is a host that records every link.changed report.
type reports struct {
	seen []linkapi.ChangedParams
}

func (r *reports) Changed(_ context.Context, params linkapi.ChangedParams) error {
	r.seen = append(r.seen, params)

	return nil
}

// TestAModuleWaitingForItsSettingsServesNothing: before plugin.configure
// arrives, Status returns the empty status. When ctx ends while serve still
// waits for the settings, serve returns nil.
func TestAModuleWaitingForItsSettingsServesNothing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	state := newLink()

	done := make(chan error, 1)

	go func() {
		done <- serve(ctx, state, &tree{}, session{}, slog.New(slog.DiscardHandler), make(chan Config))
	}()

	held, err := state.Status(t.Context())
	if err != nil {
		t.Fatalf("read the link: %v", err)
	}

	if held.NodePath != "" || held.Accepting {
		t.Fatalf("the status before the settings arrive is not empty: %+v", held)
	}

	cancel()

	if err = <-done; err != nil {
		t.Fatalf("serve returned an error while it waited for the settings: %v", err)
	}
}
