package linkapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/linkapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// fakeLink is the Link of a test module. It returns the status and the error
// the test set, and it records the node id of each Approve, Reject and Revoke
// call.
type fakeLink struct {
	status     linkapi.Status
	err        error
	sawApprove linkapi.NodeID
	sawReject  linkapi.NodeID
	sawRevoke  linkapi.NodeID
}

func (f *fakeLink) Status(context.Context) (linkapi.Status, error) {
	return f.status, f.err
}

func (f *fakeLink) Approve(_ context.Context, params linkapi.NodeParams) error {
	f.sawApprove = params.ID

	return f.err
}

func (f *fakeLink) Reject(_ context.Context, params linkapi.NodeParams) error {
	f.sawReject = params.ID

	return f.err
}

func (f *fakeLink) Revoke(_ context.Context, params linkapi.NodeParams) error {
	f.sawRevoke = params.ID

	return f.err
}

// fakeChanges is the Changes of a test host. It records the Status it received
// and returns the error the test set.
type fakeChanges struct {
	saw linkapi.Status
	err error
}

func (f *fakeChanges) Changed(_ context.Context, params linkapi.ChangedParams) error {
	f.saw = params.Status

	return f.err
}

// call encodes the params as JSON, runs the handler for the method, and returns
// the handler's answer and error. A wirerpc session does the same.
func call(t *testing.T, methods wirerpc.Methods, method wirerpc.Method, params any) (any, error) {
	t.Helper()

	handler, ok := methods[method]
	if !ok {
		t.Fatalf("no handler for %s", method)
	}

	var raw json.RawMessage

	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("encode the params: %v", err)
		}

		raw = encoded
	}

	return handler(t.Context(), wirerpc.Call{Method: method, Params: raw})
}

// codeOf returns the code of the wirerpc error a handler returned.
func codeOf(t *testing.T, err error) int {
	t.Helper()

	coded, ok := errors.AsType[*wirerpc.Error](err)
	if !ok {
		t.Fatalf("the error is not a wirerpc error: %v", err)
	}

	return coded.Code
}

// reading returns a Status with a connected node above, one admitted node below
// and one listener. The tests that send a whole Status use it.
func reading() linkapi.Status {
	seen := time.Date(2026, time.September, 11, 9, 41, 12, 0, time.UTC)
	trip := 0.84

	return linkapi.Status{
		NodeID:    "6f1c0a2d5b8e4711",
		NodePath:  "root.workshop.6f1c0a2d5b8e4711",
		PublicKey: "6c5fd7a0a0a71b1f4c4b5a2ea1d5c84d0d38c6f1b9a9a2b64c8b1f2a3d4e5f60",
		Accepting: true,
		Uplink: &linkapi.Uplink{
			Address:     "nats://above.workshop.example:4222",
			State:       linkapi.UplinkConnected,
			ConnectedAt: &seen,
		},
		Nodes: []linkapi.Node{{
			ID:          "26c70f1f054e7fdb",
			Path:        "root.workshop.6f1c0a2d5b8e4711.26c70f1f054e7fdb",
			Admission:   linkapi.AdmissionAdmitted,
			Connected:   true,
			LastSeen:    &seen,
			RoundTripMS: &trip,
		}},
		Listeners: []linkapi.Listener{{
			Protocol: "nats",
			Address:  "0.0.0.0",
			Port:     4222,
		}},
	}
}

// TestAReadingCrossesWholeAndComesBackTheSame sends a Status through the status
// handler, encodes the answer as JSON and decodes it again. Every field the
// test set has the same value afterwards.
func TestAReadingCrossesWholeAndComesBackTheSame(t *testing.T) {
	t.Parallel()

	module := &fakeLink{status: reading()}

	answer, err := call(t, linkapi.Handlers(module), linkapi.MethodStatus, linkapi.StatusParams{})
	if err != nil {
		t.Fatalf("read the status: %v", err)
	}

	encoded, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("encode the answer: %v", err)
	}

	var back linkapi.Status
	if err = json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("read the answer: %v", err)
	}

	want := reading()

	switch {
	case back.NodeID != want.NodeID:
		t.Errorf("node id is %q and should be %q", back.NodeID, want.NodeID)
	case back.NodePath != want.NodePath:
		t.Errorf("node path is %q and should be %q", back.NodePath, want.NodePath)
	case back.PublicKey != want.PublicKey:
		t.Errorf("public key is %q and should be %q", back.PublicKey, want.PublicKey)
	case !back.Accepting:
		t.Error("accepting is false and should be true")
	case back.Uplink == nil:
		t.Fatal("the decoded status has no uplink")
	case back.Uplink.State != linkapi.UplinkConnected:
		t.Errorf("uplink state is %q and should be %q", back.Uplink.State, linkapi.UplinkConnected)
	case back.Uplink.ConnectedAt == nil || !back.Uplink.ConnectedAt.Equal(*want.Uplink.ConnectedAt):
		t.Error("uplink connected_at is missing or has another value")
	case len(back.Nodes) != 1:
		t.Fatalf("the decoded status has %d nodes and one was sent", len(back.Nodes))
	case back.Nodes[0].Admission != linkapi.AdmissionAdmitted:
		t.Errorf("admission is %q and should be %q", back.Nodes[0].Admission, linkapi.AdmissionAdmitted)
	case back.Nodes[0].RoundTripMS == nil || *back.Nodes[0].RoundTripMS != 0.84:
		t.Error("round_trip_ms is missing or has another value")
	case len(back.Listeners) != 1 || back.Listeners[0].Port != 4222:
		t.Error("the listener is missing or has another port")
	}
}

// TestAMeasurementNobodyTookIsAbsent encodes a node that has sent nothing. The
// JSON has no last_seen and no round_trip_ms member, because a zero there would
// read as a measurement. An unset uplink and node path are absent too.
func TestAMeasurementNobodyTookIsAbsent(t *testing.T) {
	t.Parallel()

	status := linkapi.Status{Nodes: []linkapi.Node{{
		ID:        "26c70f1f054e7fdb",
		Admission: linkapi.AdmissionPending,
	}}}

	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("encode the reading: %v", err)
	}

	for _, absent := range []string{"last_seen", "round_trip_ms", "uplink", "node_path"} {
		if strings.Contains(string(encoded), absent) {
			t.Errorf("%s is in the JSON and the test did not set it: %s", absent, encoded)
		}
	}
}

// TestTheClosedSetsAnswerBothWays checks Valid for UplinkState and Admission:
// every listed value is valid, and a value outside the set is not.
func TestTheClosedSetsAnswerBothWays(t *testing.T) {
	t.Parallel()

	for _, state := range linkapi.UplinkStates() {
		if !state.Valid() {
			t.Errorf("%q is in the set and is not valid", state)
		}
	}

	for _, admission := range linkapi.Admissions() {
		if !admission.Valid() {
			t.Errorf("%q is in the set and is not valid", admission)
		}
	}

	if linkapi.UplinkState("linked").Valid() {
		t.Error("a state outside the set is valid")
	}

	if linkapi.Admission("denied").Valid() {
		t.Error("an admission outside the set is valid")
	}
}

// TestAReadingOutsideTheBoundsIsRefused breaks each limit and each closed set
// in turn. Validate returns ErrInvalidReading every time.
func TestAReadingOutsideTheBoundsIsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status linkapi.Status
	}{
		{"a node id over the bound", linkapi.Status{
			NodeID: linkapi.NodeID(strings.Repeat("a", linkapi.MaxNodeIDBytes+1)),
		}},
		{"a node path over the bound", linkapi.Status{
			NodePath: linkapi.NodePath(strings.Repeat("a", linkapi.MaxNodePathBytes+1)),
		}},
		{"a key over the bound", linkapi.Status{
			PublicKey: strings.Repeat("a", linkapi.MaxKeyBytes+1),
		}},
		{"a state outside the set", linkapi.Status{
			Uplink: &linkapi.Uplink{State: "linked"},
		}},
		{"an uplink address over the bound", linkapi.Status{
			Uplink: &linkapi.Uplink{
				State:   linkapi.UplinkConnected,
				Address: strings.Repeat("a", linkapi.MaxAddressBytes+1),
			},
		}},
		{"a reason over the bound", linkapi.Status{
			Uplink: &linkapi.Uplink{
				State:  linkapi.UplinkDenied,
				Reason: strings.Repeat("a", linkapi.MaxReasonBytes+1),
			},
		}},
		{"a node with no id", linkapi.Status{
			Nodes: []linkapi.Node{{Admission: linkapi.AdmissionPending}},
		}},
		{"an admission outside the set", linkapi.Status{
			Nodes: []linkapi.Node{{ID: "a", Admission: "denied"}},
		}},
		{"a listener address over the bound", linkapi.Status{
			Listeners: []linkapi.Listener{{
				Protocol: "nats",
				Address:  strings.Repeat("a", linkapi.MaxAddressBytes+1),
			}},
		}},
	}

	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			if err := one.status.Validate(); !errors.Is(err, linkapi.ErrInvalidReading) {
				t.Fatalf("Validate did not return ErrInvalidReading: %v", err)
			}
		})
	}

	if err := reading().Validate(); err != nil {
		t.Fatalf("Validate refused a status inside every limit: %v", err)
	}
}

// TestAModuleReportingAnUnreadableReadingIsRefusedAtItsOwnDoor: the status
// handler validates the Status before it sends it. For an invalid Status it
// answers CodeInvalidReading and sends no Status.
func TestAModuleReportingAnUnreadableReadingIsRefusedAtItsOwnDoor(t *testing.T) {
	t.Parallel()

	module := &fakeLink{status: linkapi.Status{
		Nodes: []linkapi.Node{{ID: "26c70f1f054e7fdb", Admission: "denied"}},
	}}

	_, err := call(t, linkapi.Handlers(module), linkapi.MethodStatus, linkapi.StatusParams{})
	if err == nil {
		t.Fatal("the handler sent a status with an admission outside the closed set")
	}

	if got := codeOf(t, err); got != linkapi.CodeInvalidReading {
		t.Fatalf("the error code is %d and should be %d", got, linkapi.CodeInvalidReading)
	}
}

// TestADecisionNamesOneNodeAndAnUnknownOneIsRefused checks Approve and Revoke.
// The node id in the call reaches the module. When the module returns
// ErrNoNode, the handler answers CodeNoNode.
func TestADecisionNamesOneNodeAndAnUnknownOneIsRefused(t *testing.T) {
	t.Parallel()

	module := &fakeLink{}
	methods := linkapi.Handlers(module)

	if _, err := call(t, methods, linkapi.MethodApprove,
		linkapi.NodeParams{ID: "26c70f1f054e7fdb"}); err != nil {
		t.Fatalf("approve one node: %v", err)
	}

	if module.sawApprove != "26c70f1f054e7fdb" {
		t.Errorf("the module was asked to approve %q", module.sawApprove)
	}

	if _, err := call(t, methods, linkapi.MethodRevoke,
		linkapi.NodeParams{ID: "26c70f1f054e7fdb"}); err != nil {
		t.Fatalf("revoke one node: %v", err)
	}

	if module.sawRevoke != "26c70f1f054e7fdb" {
		t.Errorf("the module was asked to revoke %q", module.sawRevoke)
	}

	refusing := linkapi.Handlers(&fakeLink{err: linkapi.ErrNoNode})

	for _, method := range []wirerpc.Method{
		linkapi.MethodApprove, linkapi.MethodReject, linkapi.MethodRevoke,
	} {
		_, err := call(t, refusing, method, linkapi.NodeParams{ID: "0000000000000000"})
		if err == nil {
			t.Fatalf("%s accepted a node id the link does not know", method)
		}

		if got := codeOf(t, err); got != linkapi.CodeNoNode {
			t.Fatalf("%s answered code %d and should answer %d", method, got, linkapi.CodeNoNode)
		}
	}
}

// TestRejectReachesTheModuleAndTheClientNamesIt runs the reject call in both
// directions. The handler passes the node id to the module, and Client.Reject
// names MethodReject on the session.
func TestRejectReachesTheModuleAndTheClientNamesIt(t *testing.T) {
	t.Parallel()

	module := &fakeLink{}

	if _, err := call(t, linkapi.Handlers(module), linkapi.MethodReject,
		linkapi.NodeParams{ID: "26c70f1f054e7fdb"}); err != nil {
		t.Fatalf("reject one node: %v", err)
	}

	if module.sawReject != "26c70f1f054e7fdb" {
		t.Errorf("the module was asked to reject %q", module.sawReject)
	}

	conn := &stubCaller{answer: json.RawMessage(`{}`)}

	if err := linkapi.NewClient(conn).Reject(t.Context(),
		linkapi.NodeParams{ID: "26c70f1f054e7fdb"}); err != nil {
		t.Fatalf("the client rejected one node: %v", err)
	}

	if conn.saw != linkapi.MethodReject {
		t.Errorf("the client called %s and should call %s", conn.saw, linkapi.MethodReject)
	}
}

// TestADecisionNamingNoNodeIsRefusedBeforeTheModuleSeesIt: the approve handler
// answers CodeNoNode for an empty node id and does not call the module.
func TestADecisionNamingNoNodeIsRefusedBeforeTheModuleSeesIt(t *testing.T) {
	t.Parallel()

	module := &fakeLink{}

	_, err := call(t, linkapi.Handlers(module), linkapi.MethodApprove, linkapi.NodeParams{})
	if err == nil {
		t.Fatal("the handler accepted an approve call with an empty node id")
	}

	if got := codeOf(t, err); got != linkapi.CodeNoNode {
		t.Fatalf("the error code is %d and should be %d", got, linkapi.CodeNoNode)
	}

	if module.sawApprove != "" {
		t.Error("the handler called the module for an empty node id")
	}
}

// TestAModuleFailureOfItsOwnDoesNotCross: when the module returns an error this
// contract has no code for, the host receives CodeInternal and the fixed text
// ModuleFailed. The module's own text can name an address or a file path, so
// the handler does not send it.
func TestAModuleFailureOfItsOwnDoesNotCross(t *testing.T) {
	t.Parallel()

	module := &fakeLink{err: errors.New("dial 10.0.0.4:4222: connection refused")}

	_, err := call(t, linkapi.Handlers(module), linkapi.MethodStatus, linkapi.StatusParams{})
	if err == nil {
		t.Fatal("the handler returned no error when the module failed")
	}

	coded, ok := errors.AsType[*wirerpc.Error](err)
	if !ok {
		t.Fatalf("the error is not a wirerpc error: %v", err)
	}

	if coded.Code != wirerpc.CodeInternal {
		t.Errorf("the error code is %d and should be %d", coded.Code, wirerpc.CodeInternal)
	}

	if coded.Message != linkapi.ModuleFailed {
		t.Errorf("the message is %q and should be %q", coded.Message, linkapi.ModuleFailed)
	}
}

// TestADecisionWithNoParamsObjectIsInvalidParams: the handler answers
// CodeInvalidParams for a call with no params object and does not call the
// module.
func TestADecisionWithNoParamsObjectIsInvalidParams(t *testing.T) {
	t.Parallel()

	methods := linkapi.Handlers(&fakeLink{})

	for _, method := range []wirerpc.Method{
		linkapi.MethodApprove, linkapi.MethodReject, linkapi.MethodRevoke,
	} {
		_, err := call(t, methods, method, nil)
		if err == nil {
			t.Fatalf("%s accepted a call with no params object", method)
		}

		if got := codeOf(t, err); got != wirerpc.CodeInvalidParams {
			t.Fatalf("%s answered code %d and should answer %d",
				method, got, wirerpc.CodeInvalidParams)
		}
	}
}

// TestAModuleThatServesNoLinkAnswersNothing: Handlers and ChangedHandlers
// return an empty method set for nil, so the caller receives CodeMethodNotFound
// and no handler uses a nil pointer.
func TestAModuleThatServesNoLinkAnswersNothing(t *testing.T) {
	t.Parallel()

	if got := len(linkapi.Handlers(nil)); got != 0 {
		t.Fatalf("Handlers(nil) returned %d methods", got)
	}

	if got := len(linkapi.ChangedHandlers(nil)); got != 0 {
		t.Fatalf("ChangedHandlers(nil) returned %d methods", got)
	}
}

// TestAReportReachesTheHostWhole sends a Status to the changed handler. The
// host's Changes receives the same Status.
func TestAReportReachesTheHostWhole(t *testing.T) {
	t.Parallel()

	host := &fakeChanges{}

	_, err := call(t, linkapi.ChangedHandlers(host), linkapi.MethodChanged,
		linkapi.ChangedParams{Status: reading()})
	if err != nil {
		t.Fatalf("report a change: %v", err)
	}

	if host.saw.NodeID != "6f1c0a2d5b8e4711" {
		t.Errorf("the host received node id %q", host.saw.NodeID)
	}

	if len(host.saw.Nodes) != 1 {
		t.Errorf("the host received %d nodes and one was sent", len(host.saw.Nodes))
	}
}

// TestAReportOutsideTheBoundsIsRefusedByTheHost: the changed handler validates
// the Status on the host, because a module written without this package may
// not have checked. The host's Changes does not receive an invalid Status.
func TestAReportOutsideTheBoundsIsRefusedByTheHost(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name  string
		nodes []linkapi.Node
	}{
		{name: "an admission outside the closed set", nodes: []linkapi.Node{{ID: "a", Admission: "denied"}}},
		{name: "a node with no id", nodes: []linkapi.Node{{Admission: linkapi.AdmissionAdmitted}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			host := &fakeChanges{}

			// The Status carries a node id, so the assertion below fails when
			// the handler passes the Status on.
			_, err := call(t, linkapi.ChangedHandlers(host), linkapi.MethodChanged,
				linkapi.ChangedParams{Status: linkapi.Status{NodeID: "hq", Nodes: c.nodes}})
			if err == nil {
				t.Fatal("the handler accepted an invalid status")
			}

			if got := codeOf(t, err); got != linkapi.CodeInvalidReading {
				t.Fatalf("the error code is %d and should be %d", got, linkapi.CodeInvalidReading)
			}

			if host.saw.NodeID != "" {
				t.Errorf("the handler passed the status of %q to the host", host.saw.NodeID)
			}
		})
	}
}

// stubCaller is a Caller with no process behind it. It returns the answer and
// the error the test set, and it records the method and the params of the call.
type stubCaller struct {
	answer json.RawMessage
	err    error
	saw    wirerpc.Method
	params any
}

func (s *stubCaller) Call(
	_ context.Context, method wirerpc.Method, params any,
) (json.RawMessage, error) {
	s.saw, s.params = method, params

	return s.answer, s.err
}

// TestTheClientMapsACodedRefusalBackToItsSentinel: for each error code of this
// contract, the client returns an error that errors.Is matches to the code's
// error variable. The error text keeps the message the other side sent.
func TestTheClientMapsACodedRefusalBackToItsSentinel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		code     int
		sentinel error
	}{
		{linkapi.CodeNoNode, linkapi.ErrNoNode},
		{linkapi.CodeNotReady, linkapi.ErrNotReady},
		{linkapi.CodeBusy, linkapi.ErrBusy},
		{linkapi.CodeInvalidReading, linkapi.ErrInvalidReading},
	}

	for _, one := range cases {
		t.Run(one.sentinel.Error(), func(t *testing.T) {
			t.Parallel()

			conn := &stubCaller{err: &wirerpc.Error{Code: one.code, Message: "the link said so"}}

			err := linkapi.NewClient(conn).Approve(t.Context(),
				linkapi.NodeParams{ID: "26c70f1f054e7fdb"})
			if !errors.Is(err, one.sentinel) {
				t.Fatalf("the error is %v and should match %v", err, one.sentinel)
			}

			if !strings.Contains(err.Error(), "the link said so") {
				t.Errorf("the error text lost the message the other side sent: %v", err)
			}
		})
	}
}

// TestTheClientRefusesAReadingTheModuleShouldNotHaveSent: Client.Status
// validates the answer, so the host gets ErrInvalidReading and never an invalid
// Status.
func TestTheClientRefusesAReadingTheModuleShouldNotHaveSent(t *testing.T) {
	t.Parallel()

	conn := &stubCaller{answer: json.RawMessage(
		`{"nodes":[{"id":"26c70f1f054e7fdb","admission":"denied"}]}`)}

	_, err := linkapi.NewClient(conn).Status(t.Context())
	if !errors.Is(err, linkapi.ErrInvalidReading) {
		t.Fatalf("the client accepted a status with an admission outside the closed set: %v", err)
	}
}

// TestAClientWithNoSessionSaysSo: a Client and a Reporter created with a nil
// Caller return an error and do not use the nil pointer.
func TestAClientWithNoSessionSaysSo(t *testing.T) {
	t.Parallel()

	if _, err := linkapi.NewClient(nil).Status(t.Context()); err == nil {
		t.Fatal("a client with a nil Caller returned a status and no error")
	}

	if err := linkapi.NewReporter(nil).Changed(t.Context(),
		linkapi.ChangedParams{}); err == nil {
		t.Fatal("a reporter with a nil Caller returned no error")
	}
}

// TestTheReporterChecksItsOwnReadingFirst: Reporter.Changed validates the
// Status and makes no call for one the host would refuse.
func TestTheReporterChecksItsOwnReadingFirst(t *testing.T) {
	t.Parallel()

	conn := &stubCaller{}

	err := linkapi.NewReporter(conn).Changed(t.Context(), linkapi.ChangedParams{
		Status: linkapi.Status{Nodes: []linkapi.Node{{ID: "a", Admission: "denied"}}},
	})
	if !errors.Is(err, linkapi.ErrInvalidReading) {
		t.Fatalf("the reporter did not return ErrInvalidReading: %v", err)
	}

	if conn.saw != "" {
		t.Errorf("the reporter called %s with an invalid status", conn.saw)
	}
}

// TestTheClientNamesTheMethodEachCallMakes: Client.Status calls MethodStatus
// and Client.Revoke calls MethodRevoke.
func TestTheClientNamesTheMethodEachCallMakes(t *testing.T) {
	t.Parallel()

	conn := &stubCaller{answer: json.RawMessage(`{}`)}
	client := linkapi.NewClient(conn)

	if _, err := client.Status(t.Context()); err != nil {
		t.Fatalf("read the status: %v", err)
	}

	if conn.saw != linkapi.MethodStatus {
		t.Errorf("the client called %s and should call %s", conn.saw, linkapi.MethodStatus)
	}

	if err := client.Revoke(t.Context(), linkapi.NodeParams{ID: "26c70f1f054e7fdb"}); err != nil {
		t.Fatalf("revoke one node: %v", err)
	}

	if conn.saw != linkapi.MethodRevoke {
		t.Errorf("the client called %s and should call %s", conn.saw, linkapi.MethodRevoke)
	}
}

// TestTheAdmitPolicyIsAClosedSetToo checks Admits the same way: every listed
// policy is valid, and Status.Validate refuses a policy outside the set.
func TestTheAdmitPolicyIsAClosedSetToo(t *testing.T) {
	t.Parallel()

	for _, policy := range linkapi.AdmitsPolicies() {
		if !policy.Valid() {
			t.Errorf("%q is in the set and is not valid", policy)
		}

		if policy.String() != string(policy) {
			t.Errorf("String of %q returned %q", policy, policy.String())
		}
	}

	if linkapi.Admits("by_hand").Valid() {
		t.Error("a policy outside the set is valid")
	}

	status := linkapi.Status{Accepting: true, Admits: "by_hand"}
	if err := status.Validate(); !errors.Is(err, linkapi.ErrInvalidReading) {
		t.Fatalf("Validate accepted a policy outside the set: %v", err)
	}
}

// TestEveryNameRendersAsItself: String returns the same text the wire has, for
// a node id, a node path, an uplink state and an admission.
func TestEveryNameRendersAsItself(t *testing.T) {
	t.Parallel()

	if got := linkapi.NodeID("26c70f1f054e7fdb").String(); got != "26c70f1f054e7fdb" {
		t.Errorf("a node id renders as %q", got)
	}

	if got := linkapi.NodePath("root.workshop").String(); got != "root.workshop" {
		t.Errorf("a node path renders as %q", got)
	}

	if got := linkapi.UplinkLost.String(); got != "lost" {
		t.Errorf("an uplink state renders as %q", got)
	}

	if got := linkapi.AdmissionRevoked.String(); got != "revoked" {
		t.Errorf("an admission renders as %q", got)
	}
}

// TestAReadingRequestNamesNothing: StatusParams has no members, and its
// Validate method returns nil.
func TestAReadingRequestNamesNothing(t *testing.T) {
	t.Parallel()

	if err := (linkapi.StatusParams{}).Validate(); err != nil {
		t.Fatalf("Validate refused an empty StatusParams: %v", err)
	}
}

// TestAParamsObjectThisMethodCannotReadIsInvalidParams: the approve, revoke and
// changed handlers answer CodeInvalidParams for params that are not a JSON
// object.
func TestAParamsObjectThisMethodCannotReadIsInvalidParams(t *testing.T) {
	t.Parallel()

	doors := map[wirerpc.Method]wirerpc.Methods{
		linkapi.MethodApprove: linkapi.Handlers(&fakeLink{}),
		linkapi.MethodReject:  linkapi.Handlers(&fakeLink{}),
		linkapi.MethodRevoke:  linkapi.Handlers(&fakeLink{}),
		linkapi.MethodChanged: linkapi.ChangedHandlers(&fakeChanges{}),
	}

	for method, methods := range doors {
		handler := methods[method]

		_, err := handler(t.Context(), wirerpc.Call{
			Method: method,
			Params: json.RawMessage(`"not an object"`),
		})
		if err == nil {
			t.Fatalf("%s accepted params that are not a JSON object", method)
		}

		if got := codeOf(t, err); got != wirerpc.CodeInvalidParams {
			t.Fatalf("%s answered code %d and should answer %d",
				method, got, wirerpc.CodeInvalidParams)
		}
	}
}

// TestAHostFailureOfItsOwnDoesNotCrossToAModule: when the host's Changes
// returns an error this contract has no code for, the module receives the
// fixed text HostFailed. The host's own text can name a file path, so the
// handler does not send it.
func TestAHostFailureOfItsOwnDoesNotCrossToAModule(t *testing.T) {
	t.Parallel()

	host := &fakeChanges{err: errors.New("write to /var/lib/pcl/events: no space left on device")}

	_, err := call(t, linkapi.ChangedHandlers(host), linkapi.MethodChanged,
		linkapi.ChangedParams{Status: reading()})
	if err == nil {
		t.Fatal("the handler returned no error when the host failed")
	}

	coded, ok := errors.AsType[*wirerpc.Error](err)
	if !ok {
		t.Fatalf("the error is not a wirerpc error: %v", err)
	}

	if coded.Message != linkapi.HostFailed {
		t.Errorf("the message is %q and should be %q", coded.Message, linkapi.HostFailed)
	}
}

// TestAHostThatIsNotReadyIsACodedRefusal: when the host's Changes returns
// ErrNotReady, the module receives CodeNotReady and can try again later.
func TestAHostThatIsNotReadyIsACodedRefusal(t *testing.T) {
	t.Parallel()

	host := &fakeChanges{err: linkapi.ErrNotReady}

	_, err := call(t, linkapi.ChangedHandlers(host), linkapi.MethodChanged,
		linkapi.ChangedParams{Status: reading()})
	if err == nil {
		t.Fatal("the handler returned no error for a host that is not ready")
	}

	if got := codeOf(t, err); got != linkapi.CodeNotReady {
		t.Fatalf("the error code is %d and should be %d", got, linkapi.CodeNotReady)
	}
}

// TestAnAnswerTheClientCannotReadIsNamedAsSuch: Client.Status returns an error
// for an answer that is not JSON, and not an empty Status.
func TestAnAnswerTheClientCannotReadIsNamedAsSuch(t *testing.T) {
	t.Parallel()

	conn := &stubCaller{answer: json.RawMessage(`not a document`)}

	if _, err := linkapi.NewClient(conn).Status(t.Context()); err == nil {
		t.Fatal("the client returned no error for an answer that is not JSON")
	}
}

// TestADecisionNamingNoNodeSpendsNoRoundTrip: Client.Revoke validates the node
// id first. For an empty id it returns ErrNoNode and makes no call.
func TestADecisionNamingNoNodeSpendsNoRoundTrip(t *testing.T) {
	t.Parallel()

	conn := &stubCaller{}

	if err := linkapi.NewClient(conn).Revoke(t.Context(), linkapi.NodeParams{}); !errors.Is(
		err, linkapi.ErrNoNode,
	) {
		t.Fatalf("Revoke with an empty node id did not return ErrNoNode: %v", err)
	}

	if conn.saw != "" {
		t.Errorf("the client called %s with an empty node id", conn.saw)
	}
}

// TestTheReporterSendsTheWholeReading: Reporter.Changed calls MethodChanged
// with the whole Status it received.
func TestTheReporterSendsTheWholeReading(t *testing.T) {
	t.Parallel()

	conn := &stubCaller{}

	if err := linkapi.NewReporter(conn).Changed(t.Context(),
		linkapi.ChangedParams{Status: reading()}); err != nil {
		t.Fatalf("report a change: %v", err)
	}

	if conn.saw != linkapi.MethodChanged {
		t.Errorf("the reporter called %s", conn.saw)
	}

	sent, ok := conn.params.(linkapi.ChangedParams)
	if !ok {
		t.Fatalf("the reporter sent a %T", conn.params)
	}

	if len(sent.Status.Nodes) != 1 || sent.Status.NodeID != "6f1c0a2d5b8e4711" {
		t.Error("the reporter sent another status than the one the test passed")
	}
}
