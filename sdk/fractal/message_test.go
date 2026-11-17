package fractal_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// message has the methods every message struct in package fractal has, so one
// test table can have messages of every type.
type message interface {
	Kind() fractal.Kind
	Validate() error
}

// roundTrip decodes the JSON in golden into a new T, validates it, encodes it
// again and compares the bytes with golden. It also compares the decoded
// message with want.
func roundTrip[T message](t *testing.T, golden string, want T) {
	t.Helper()
	var got T
	if err := json.Unmarshal([]byte(golden), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(out) != golden {
		t.Fatalf("encode gave\n  %s\nwant\n  %s", out, golden)
	}
	if !jsonEqual(t, got, want) {
		t.Fatalf("decoded %+v, want %+v", got, want)
	}
}

// jsonEqual reports whether two messages encode to the same JSON. The protocol
// defines the JSON and not the Go struct, so two messages are equal when their
// JSON is equal.
func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	ab, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	bb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(ab) == string(bb)
}

// The messages of the Examples section of fractal.md, as the JSON a transport
// binding sends. The examples in fractal.md leave the version field out to
// stay short. It is written here.
const (
	goldenRBR = `{"version":1}`

	goldenREPStartup = `{"version":1,"sender":"hq/brno/printer-1","interface":"FDMPrinter1",` +
		`"instance":"1","timestamp":1751400000000,` +
		`"props":{"nozzleTemp":25.0,"nozzleTempTarget":null}}`

	goldenREPChange = `{"version":1,"sender":"hq/brno/printer-1","interface":"FDMPrinter1",` +
		`"instance":"1","timestamp":1751400010000,"props":{"nozzleTemp":200.0}}`

	goldenDES = `{"version":1,"receiver":"hq/brno/printer-1","interface":"FDMPrinter1",` +
		`"instance":"1","props":{"nozzleTempTarget":220.0}}`

	goldenREPAccepted = `{"version":1,"sender":"hq/brno/printer-1","interface":"FDMPrinter1",` +
		`"instance":"1","timestamp":1751400020000,"props":{"nozzleTempTarget":220.0}}`

	goldenRPC = `{"version":1,"id":"dGVzdA","sender":"hq","receiver":"hq/brno/printer-1",` +
		`"interface":"FDMPrinter1","instance":"1","method":"SetNozzleTemp",` +
		`"params":[250.0],"expires":1751400055000}`

	goldenREPExecuted = `{"version":1,"sender":"hq/brno/printer-1","interface":"FDMPrinter1",` +
		`"instance":"1","timestamp":1751400025000,"props":{"nozzleTempTarget":250.0}}`

	goldenRES = `{"version":1,"id":"dGVzdA","receiver":"hq","result":true}`

	goldenLOS = `{"version":1,"sender":"hq/brno","child":"printer-1"}`

	goldenREPReconnect = `{"version":1,"sender":"hq/brno/printer-1","interface":"FDMPrinter1",` +
		`"instance":"1","timestamp":1751400120000,` +
		`"props":{"nozzleTemp":230.0,"nozzleTempTarget":250.0}}`
)

// The messages of Examples 7 and 8 of fractal.md, with the version field.
const (
	goldenAREP = `{"version":1,"sender":"hq","interface":"Site1","instance":"1",` +
		`"timestamp":1751400180000,"props":{"maintenanceMode":true}}`

	goldenALOS = `{"version":1,"sender":"hq/brno"}`

	goldenARBR = `{"version":1}`
)

// TestGoldenREP round-trips every REP of the Examples section.
func TestGoldenREP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		golden string
		want   fractal.REP
	}{
		{
			name: "startup", golden: goldenREPStartup,
			want: fractal.REP{
				Version:   1,
				Sender:    "hq/brno/printer-1",
				Interface: "FDMPrinter1",
				Instance:  "1",
				Timestamp: 1751400000000,
				Props: fractal.Props{
					"nozzleTemp":       json.RawMessage("25.0"),
					"nozzleTempTarget": json.RawMessage("null"),
				},
			},
		},
		{
			name: "state change", golden: goldenREPChange,
			want: fractal.REP{
				Version: 1, Sender: "hq/brno/printer-1", Interface: "FDMPrinter1",
				Instance: "1", Timestamp: 1751400010000,
				Props: fractal.Props{"nozzleTemp": json.RawMessage("200.0")},
			},
		},
		{
			name: "desired properties accepted", golden: goldenREPAccepted,
			want: fractal.REP{
				Version: 1, Sender: "hq/brno/printer-1", Interface: "FDMPrinter1",
				Instance: "1", Timestamp: 1751400020000,
				Props: fractal.Props{"nozzleTempTarget": json.RawMessage("220.0")},
			},
		},
		{
			name: "method executed", golden: goldenREPExecuted,
			want: fractal.REP{
				Version: 1, Sender: "hq/brno/printer-1", Interface: "FDMPrinter1",
				Instance: "1", Timestamp: 1751400025000,
				Props: fractal.Props{"nozzleTempTarget": json.RawMessage("250.0")},
			},
		},
		{
			name: "reconnection", golden: goldenREPReconnect,
			want: fractal.REP{
				Version:   1,
				Sender:    "hq/brno/printer-1",
				Interface: "FDMPrinter1",
				Instance:  "1",
				Timestamp: 1751400120000,
				Props: fractal.Props{
					"nozzleTemp":       json.RawMessage("230.0"),
					"nozzleTempTarget": json.RawMessage("250.0"),
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			roundTrip(t, tc.golden, tc.want)
		})
	}
}

// TestGoldenDES round-trips the DES of the Examples section.
func TestGoldenDES(t *testing.T) {
	t.Parallel()
	roundTrip(t, goldenDES, fractal.DES{
		Version: 1, Receiver: "hq/brno/printer-1", Interface: "FDMPrinter1",
		Instance: "1",
		Props:    fractal.Props{"nozzleTempTarget": json.RawMessage("220.0")},
	})
}

// TestGoldenRPC round-trips the RPC of the Examples section.
func TestGoldenRPC(t *testing.T) {
	t.Parallel()
	roundTrip(t, goldenRPC, fractal.RPC{
		Version: 1, ID: "dGVzdA", Sender: "hq", Receiver: "hq/brno/printer-1",
		Interface: "FDMPrinter1", Instance: "1", Method: "SetNozzleTemp",
		Params: []json.RawMessage{json.RawMessage("250.0")}, Expires: 1751400055000,
	})
}

// TestGoldenRES round-trips the RES of the Examples section.
func TestGoldenRES(t *testing.T) {
	t.Parallel()
	roundTrip(t, goldenRES, fractal.RES{
		Version: 1, ID: "dGVzdA", Receiver: "hq", Result: json.RawMessage("true"),
	})
}

// TestGoldenLOS round-trips the LOS of the Examples section.
func TestGoldenLOS(t *testing.T) {
	t.Parallel()
	roundTrip(t, goldenLOS, fractal.LOS{Version: 1, Sender: "hq/brno", Child: "printer-1"})
}

// TestGoldenRBR round-trips the RBR of the Examples section. Its only field is
// the protocol version.
func TestGoldenRBR(t *testing.T) {
	t.Parallel()
	roundTrip(t, goldenRBR, fractal.RBR{Version: 1})
}

// TestGoldenAREP round-trips the AREP of Example 7. An AREP has the fields of a
// REP (fractal.md, Ancestor Reported Properties Message).
func TestGoldenAREP(t *testing.T) {
	t.Parallel()
	roundTrip(t, goldenAREP, fractal.AREP{
		Version:   1,
		Sender:    "hq",
		Interface: "Site1",
		Instance:  "1",
		Timestamp: 1751400180000,
		Props:     fractal.Props{"maintenanceMode": json.RawMessage("true")},
	})
}

// TestGoldenALOS round-trips the ALOS of Example 8. Its sender is the node that
// lost its parent (fractal.md, Ancestor Connection Loss Message).
func TestGoldenALOS(t *testing.T) {
	t.Parallel()
	roundTrip(t, goldenALOS, fractal.ALOS{Version: 1, Sender: "hq/brno"})
}

// TestGoldenARBR round-trips an ARBR. Its only field is the protocol version
// (fractal.md, Ancestor Full State Report Request Message).
func TestGoldenARBR(t *testing.T) {
	t.Parallel()
	roundTrip(t, goldenARBR, fractal.ARBR{Version: 1})
}

// TestRPCParamsOmittedWhenEmpty tests the rule of fractal.md: an RPC for a
// method with no parameters has no params field.
func TestRPCParamsOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	call := fractal.RPC{
		Version: 1, ID: "dGVzdA", Sender: "hq", Receiver: "hq/brno/printer-1",
		Interface: "FDMPrinter1", Instance: "1", Method: "Home", Expires: 1751400055000,
	}
	want := `{"version":1,"id":"dGVzdA","sender":"hq","receiver":"hq/brno/printer-1",` +
		`"interface":"FDMPrinter1","instance":"1","method":"Home","expires":1751400055000}`
	roundTrip(t, want, call)

	call.Params = []json.RawMessage{}
	out, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(out) != want {
		t.Fatalf("an RPC with an empty Params slice was encoded with a params field: %s", out)
	}
}

// TestRESResultAbsentOnFailure tests the rule of fractal.md: the RES for a
// failed call has an error and no result, and the RES for a successful call
// has no error.
func TestRESResultAbsentOnFailure(t *testing.T) {
	t.Parallel()
	failed := `{"version":1,"id":"dGVzdA","receiver":"hq",` +
		`"error":{"message":"nozzle is busy","code":8208}}`
	roundTrip(t, failed, fractal.RES{
		Version: 1, ID: "dGVzdA", Receiver: "hq",
		Error: &fractal.Error{Message: "nozzle is busy", Code: fractal.CodeDenied},
	})

	noMessage := `{"version":1,"id":"dGVzdA","receiver":"hq","error":{"code":16}}`
	roundTrip(t, noMessage, fractal.RES{
		Version: 1, ID: "dGVzdA", Receiver: "hq",
		Error: &fractal.Error{Code: fractal.CodeInternal},
	})

	noReturn := `{"version":1,"id":"dGVzdA","receiver":"hq"}`
	roundTrip(t, noReturn, fractal.RES{Version: 1, ID: "dGVzdA", Receiver: "hq"})

	both := fractal.RES{
		Version: 1, ID: "dGVzdA", Receiver: "hq", Result: json.RawMessage("true"),
		Error: &fractal.Error{Code: fractal.CodeInternal},
	}
	if err := both.Validate(); !errors.Is(err, fractal.ErrResultWithError) {
		t.Fatalf("Validate of a RES with both a result and an error = %v, want ErrResultWithError", err)
	}
	if both.Failed() != true {
		t.Fatal("Failed returned false for a RES with an error")
	}
}

// TestDecodeIgnoresUnknownFields tests the rule of fractal-nats.md: a receiver
// ignores a field it does not recognize. A sender of a later protocol version
// can then add a field.
func TestDecodeIgnoresUnknownFields(t *testing.T) {
	t.Parallel()
	in := `{"version":1,"sender":"hq/brno/printer-1","interface":"FDMPrinter1",` +
		`"instance":"1","timestamp":1751400000000,"props":{"nozzleTemp":25.0},` +
		`"quality":"high","trace":{"span":"abc"}}`
	var rep fractal.REP
	if err := json.Unmarshal([]byte(in), &rep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := rep.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if rep.Timestamp != 1751400000000 {
		t.Fatalf("timestamp = %d", rep.Timestamp)
	}
}

// TestEveryMessageCarriesVersion tests the rule of fractal.md: every message
// has a version field, and a node drops a message of a version it does not
// support. Validate returns ErrUnsupportedVersion for such a message.
func TestEveryMessageCarriesVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		good message
		bad  message
	}{
		{
			name: "REP",
			good: fractal.REP{Version: 1, Sender: "hq", Interface: "I", Instance: "1"},
			bad:  fractal.REP{Version: 2, Sender: "hq", Interface: "I", Instance: "1"},
		},
		{
			name: "DES",
			good: fractal.DES{Version: 1, Receiver: "hq", Interface: "I", Instance: "1"},
			bad:  fractal.DES{Version: 0, Receiver: "hq", Interface: "I", Instance: "1"},
		},
		{
			name: "RPC",
			good: fractal.RPC{
				Version:   1,
				ID:        "a",
				Sender:    "hq",
				Receiver:  "hq/b",
				Interface: "I",
				Instance:  "1",
				Method:    "M",
			},
			bad: fractal.RPC{
				Version:   9,
				ID:        "a",
				Sender:    "hq",
				Receiver:  "hq/b",
				Interface: "I",
				Instance:  "1",
				Method:    "M",
			},
		},
		{
			name: "RES",
			good: fractal.RES{Version: 1, ID: "a", Receiver: "hq"},
			bad:  fractal.RES{Version: 9, ID: "a", Receiver: "hq"},
		},
		{
			name: "LOS",
			good: fractal.LOS{Version: 1, Sender: "hq", Child: "b"},
			bad:  fractal.LOS{Version: 9, Sender: "hq", Child: "b"},
		},
		{
			name: "RBR",
			good: fractal.RBR{Version: 1},
			bad:  fractal.RBR{Version: 9},
		},
		{
			name: "AREP",
			good: fractal.AREP{Version: 1, Sender: "hq", Interface: "I", Instance: "1"},
			bad:  fractal.AREP{Version: 2, Sender: "hq", Interface: "I", Instance: "1"},
		},
		{
			name: "ALOS",
			good: fractal.ALOS{Version: 1, Sender: "hq"},
			bad:  fractal.ALOS{Version: 9, Sender: "hq"},
		},
		{
			name: "ARBR",
			good: fractal.ARBR{Version: 1},
			bad:  fractal.ARBR{Version: 9},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.good.Validate(); err != nil {
				t.Fatalf("Validate refused a message of version 1: %v", err)
			}
			err := tc.bad.Validate()
			if !errors.Is(err, fractal.ErrUnsupportedVersion) {
				t.Fatalf("Validate of an unsupported version = %v, want ErrUnsupportedVersion", err)
			}
			if !tc.good.Kind().Valid() {
				t.Fatal("Kind returned a value that is not a message type")
			}
		})
	}
}

// TestMessageValidationRefusesEveryName tests that Validate of each message
// checks each name field. Every case has one invalid field, which the case
// name gives.
func TestMessageValidationRefusesEveryName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msg  message
	}{
		{name: "REP sender", msg: fractal.REP{Version: 1, Sender: "hq//a", Interface: "I", Instance: "1"}},
		{name: "REP interface", msg: fractal.REP{Version: 1, Sender: "hq", Interface: "1I", Instance: "1"}},
		{name: "REP instance", msg: fractal.REP{Version: 1, Sender: "hq", Interface: "I", Instance: "-1"}},
		{
			name: "REP property",
			msg: fractal.REP{Version: 1, Sender: "hq", Interface: "I", Instance: "1",
				Props: fractal.Props{"bad name": json.RawMessage("1")}},
		},
		{name: "DES receiver", msg: fractal.DES{Version: 1, Receiver: "/hq", Interface: "I", Instance: "1"}},
		{name: "DES interface", msg: fractal.DES{Version: 1, Receiver: "hq", Interface: "", Instance: "1"}},
		{
			name: "DES property",
			msg: fractal.DES{Version: 1, Receiver: "hq", Interface: "I", Instance: "1",
				Props: fractal.Props{"nozzle-temp": json.RawMessage("1")}},
		},
		{
			name: "RPC id",
			msg: fractal.RPC{
				Version:   1,
				ID:        "not a base64url id!",
				Sender:    "hq",
				Receiver:  "hq/b",
				Interface: "I",
				Instance:  "1",
				Method:    "M",
			},
		},
		{
			name: "RPC sender",
			msg: fractal.RPC{
				Version:   1,
				ID:        "a",
				Sender:    "hq/",
				Receiver:  "hq/b",
				Interface: "I",
				Instance:  "1",
				Method:    "M",
			},
		},
		{
			name: "RPC receiver",
			msg: fractal.RPC{
				Version:   1,
				ID:        "a",
				Sender:    "hq",
				Receiver:  "",
				Interface: "I",
				Instance:  "1",
				Method:    "M",
			},
		},
		{
			name: "RPC method",
			msg: fractal.RPC{
				Version:   1,
				ID:        "a",
				Sender:    "hq",
				Receiver:  "hq/b",
				Interface: "I",
				Instance:  "1",
				Method:    "2M",
			},
		},
		{name: "RES id", msg: fractal.RES{Version: 1, ID: "", Receiver: "hq"}},
		{name: "RES receiver", msg: fractal.RES{Version: 1, ID: "a", Receiver: "hq//b"}},
		{
			name: "RES reserved code",
			msg:  fractal.RES{Version: 1, ID: "a", Receiver: "hq", Error: &fractal.Error{Code: 0x6000}},
		},
		{name: "LOS sender", msg: fractal.LOS{Version: 1, Sender: "", Child: "b"}},
		{name: "LOS child", msg: fractal.LOS{Version: 1, Sender: "hq", Child: "-b"}},
		{name: "AREP sender", msg: fractal.AREP{Version: 1, Sender: "hq/", Interface: "I", Instance: "1"}},
		{name: "AREP instance", msg: fractal.AREP{Version: 1, Sender: "hq", Interface: "I", Instance: "-1"}},
		{name: "ALOS sender", msg: fractal.ALOS{Version: 1, Sender: "/hq"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.msg.Validate(); err == nil {
				t.Fatal("Validate accepted the message with the invalid field")
			}
		})
	}
}

// TestRPCExpired tests the expiry check, which fractal.md makes the first step
// a node takes with an RPC. An RPC is expired only after its Expires time.
func TestRPCExpired(t *testing.T) {
	t.Parallel()
	call := fractal.RPC{Expires: 1751400055000}
	if call.Expired(1751400055000) {
		t.Error("Expired returned true at exactly the Expires time")
	}
	if call.Expired(1751400054999) {
		t.Error("Expired returned true one millisecond before the Expires time")
	}
	if !call.Expired(1751400055001) {
		t.Error("Expired returned false one millisecond after the Expires time")
	}
}

// TestLOSAffected tests the rule of fractal.md: the path of the node that
// disconnected is the sender field, a slash and the child field.
func TestLOSAffected(t *testing.T) {
	t.Parallel()
	got, err := fractal.LOS{Version: 1, Sender: "hq/brno", Child: "printer-1"}.Affected()
	if err != nil || got != "hq/brno/printer-1" {
		t.Fatalf("Affected = %q %v", got, err)
	}
}

// TestErrorRendersTheCode tests the string the Error method of an *Error
// returns, with a message and without one.
func TestErrorRendersTheCode(t *testing.T) {
	t.Parallel()
	withMessage := &fractal.Error{Code: fractal.CodeDenied, Message: "nozzle is busy"}
	if got := withMessage.Error(); got != "ERR_DENIED: nozzle is busy" {
		t.Fatalf("Error = %q", got)
	}
	bare := &fractal.Error{Code: fractal.CodeExpired}
	if got := bare.Error(); got != "ERR_EXPIRED" {
		t.Fatalf("Error = %q", got)
	}
}
