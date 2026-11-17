package fractal_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestKindTable tests the direction, the routing and the delivery guarantee of
// every message type against the routing sections and the RPC and RES sections
// of fractal.md.
func TestKindTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		kind     fractal.Kind
		dir      fractal.Direction
		routing  fractal.Routing
		delivery fractal.Delivery
	}{
		{name: "REP", kind: fractal.KindREP, dir: fractal.Up, routing: fractal.Broadcast, delivery: fractal.BestEffort},
		{name: "DES", kind: fractal.KindDES, dir: fractal.Down, routing: fractal.Unicast, delivery: fractal.BestEffort},
		{name: "RPC", kind: fractal.KindRPC, dir: fractal.Down, routing: fractal.Unicast, delivery: fractal.Durable},
		{name: "RES", kind: fractal.KindRES, dir: fractal.Up, routing: fractal.Unicast, delivery: fractal.Durable},
		{name: "LOS", kind: fractal.KindLOS, dir: fractal.Up, routing: fractal.Broadcast, delivery: fractal.BestEffort},
		{
			name:     "RBR",
			kind:     fractal.KindRBR,
			dir:      fractal.Down,
			routing:  fractal.Broadcast,
			delivery: fractal.BestEffort,
		},
		{
			name:     "AREP",
			kind:     fractal.KindAREP,
			dir:      fractal.Down,
			routing:  fractal.Broadcast,
			delivery: fractal.BestEffort,
		},
		{
			name:     "ALOS",
			kind:     fractal.KindALOS,
			dir:      fractal.Down,
			routing:  fractal.Broadcast,
			delivery: fractal.BestEffort,
		},
		{
			name:     "ARBR",
			kind:     fractal.KindARBR,
			dir:      fractal.Up,
			routing:  fractal.Broadcast,
			delivery: fractal.BestEffort,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.kind.String(); got != tc.name {
				t.Errorf("String = %q, want %q", got, tc.name)
			}
			if got := tc.kind.Direction(); got != tc.dir {
				t.Errorf("Direction = %s, want %s", got, tc.dir)
			}
			if got := tc.kind.Routing(); got != tc.routing {
				t.Errorf("Routing = %s, want %s", got, tc.routing)
			}
			if got := tc.kind.Delivery(); got != tc.delivery {
				t.Errorf("Delivery = %s, want %s", got, tc.delivery)
			}
			if !tc.kind.Valid() {
				t.Error("Valid returned false for a message type fractal.md defines")
			}
			got, ok := fractal.ParseKind(tc.name)
			if !ok || got != tc.kind {
				t.Errorf("ParseKind(%q) = %s %v", tc.name, got, ok)
			}
		})
	}
	if got := len(fractal.Kinds()); got != len(tests) {
		t.Fatalf("Kinds returned %d message types, want %d", got, len(tests))
	}
}

// TestKindOfEachMessage tests that the Kind method of each message struct
// returns the Kind of that message.
func TestKindOfEachMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msg  message
		kind fractal.Kind
	}{
		{name: "REP", msg: fractal.REP{}, kind: fractal.KindREP},
		{name: "DES", msg: fractal.DES{}, kind: fractal.KindDES},
		{name: "RPC", msg: fractal.RPC{}, kind: fractal.KindRPC},
		{name: "RES", msg: fractal.RES{}, kind: fractal.KindRES},
		{name: "LOS", msg: fractal.LOS{}, kind: fractal.KindLOS},
		{name: "RBR", msg: fractal.RBR{}, kind: fractal.KindRBR},
		{name: "AREP", msg: fractal.AREP{}, kind: fractal.KindAREP},
		{name: "ALOS", msg: fractal.ALOS{}, kind: fractal.KindALOS},
		{name: "ARBR", msg: fractal.ARBR{}, kind: fractal.KindARBR},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.msg.Kind(); got != tc.kind {
				t.Fatalf("Kind = %s, want %s", got, tc.kind)
			}
		})
	}
}

// TestKindRefusesAnythingElse tests the methods of a Kind that is not a message
// type, ParseKind on a string that is not a message type, and the String
// methods of Direction, Routing and Delivery.
func TestKindRefusesAnythingElse(t *testing.T) {
	t.Parallel()
	unknown := fractal.Kind(0)
	if unknown.Valid() {
		t.Error("Valid returned true for Kind(0)")
	}
	if got := unknown.String(); got != "unknown" {
		t.Errorf("String = %q", got)
	}
	if unknown.Direction() != 0 || unknown.Routing() != 0 || unknown.Delivery() != 0 {
		t.Error("Direction, Routing or Delivery of Kind(0) is not 0")
	}
	if _, ok := fractal.ParseKind("rep"); ok {
		t.Error("ParseKind accepted the lower case name rep")
	}
	if _, ok := fractal.ParseKind("HEARTBEAT"); ok {
		t.Error("ParseKind accepted something that is not a message type")
	}
	if got := fractal.Direction(0).String(); got != "unknown" {
		t.Errorf("Direction.String = %q", got)
	}
	if got := fractal.Routing(0).String(); got != "unknown" {
		t.Errorf("Routing.String = %q", got)
	}
	if got := fractal.Delivery(0).String(); got != "unknown" {
		t.Errorf("Delivery.String = %q", got)
	}
	if fractal.Up.String() != "up" || fractal.Down.String() != "down" {
		t.Error("Direction.String is not up and down")
	}
	if fractal.Unicast.String() != "unicast" || fractal.Broadcast.String() != "broadcast" {
		t.Error("Routing.String is not unicast and broadcast")
	}
	if fractal.Durable.String() != "durable" || fractal.BestEffort.String() != "best effort" {
		t.Error("Delivery.String is not durable and best effort")
	}
}
