package linkapi_test

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The codes of a made-up module, registered here so the tests have a namespace
// of their own. A registration states what went wrong, what the operator does
// about it, whether sending the same request again can work, and the values the
// code names.
//
//nolint:gochecknoglobals // a registered code is a public identifier; a const cannot hold one.
var (
	testDomain = mustTestDomain()

	testNodeUnknown = fault.Register(testDomain, "NODE_UNKNOWN", 404, fault.Document(
		"This link holds no server under that id",
		"Take the id from the list of servers below this one.",
		fault.RetryNever,
		fault.ShowDetail,
		"id",
	))

	testDecisionRefused = fault.Register(testDomain, "DECISION_REFUSED", 409, fault.Document(
		"This link could not record the decision about that server",
		"Read the link's log for the reason, then send the decision again.",
		fault.RetryAllowed,
		fault.ShowDetail,
	))
)

func mustTestDomain() fault.Domain {
	built, err := fault.NewDomain("com.example.linkapitest")
	if err != nil {
		panic(err.Error())
	}

	return built
}

// TestAModuleCodeReachesTheHostBesideItsSentence: a module that refuses with a
// registered code sends the code, the English title and the sentence for this
// occurrence. The host reads all three, so a page keys its own translation by
// the code and never by the sentence.
func TestAModuleCodeReachesTheHostBesideItsSentence(t *testing.T) {
	t.Parallel()

	raised := testDecisionRefused.New("the record store would not take the decision")
	module := &fakeLink{err: linkapi.Faulted(raised)}

	_, err := call(t, linkapi.Handlers(module), linkapi.MethodApprove,
		linkapi.NodeParams{ID: "26c70f1f054e7fdb"})
	if err == nil {
		t.Fatal("the handler returned no error when the module refused")
	}

	if got := codeOf(t, err); got != linkapi.CodeFaulted {
		t.Fatalf("the error code is %d and should be %d", got, linkapi.CodeFaulted)
	}

	payload, carried := faultAcrossTheWire(t, err)
	if !carried {
		t.Fatal("the refusal carries no fault")
	}

	if payload.Code != testDecisionRefused.Code() {
		t.Errorf("the code is %s, want %s", payload.Code, testDecisionRefused.Code())
	}

	if payload.Title != raised.Title() {
		t.Errorf("the title is %q, want %q", payload.Title, raised.Title())
	}

	if payload.Detail != "the record store would not take the decision" {
		t.Errorf("the detail is %q, and it is not the sentence the module wrote", payload.Detail)
	}

	if !payload.Retryable {
		t.Error("the code is registered as worth retrying and the payload says it is not")
	}
}

// TestACodedRefusalKeepsBothReadings: a module that refuses a node it does not
// know sends the code of this contract and its own fault. The host matches
// ErrNoNode, so it answers the request the way it always has, and it also reads
// the module's code for the page.
func TestACodedRefusalKeepsBothReadings(t *testing.T) {
	t.Parallel()

	raised := testNodeUnknown.New("this link holds no server under that id").
		With(fault.StringParam("id", "0000000000000000")).Wrap(linkapi.ErrNoNode)

	module := &fakeLink{err: linkapi.Faulted(raised)}

	_, err := call(t, linkapi.Handlers(module), linkapi.MethodRevoke,
		linkapi.NodeParams{ID: "0000000000000000"})
	if err == nil {
		t.Fatal("the handler returned no error for a node the link does not know")
	}

	if got := codeOf(t, err); got != linkapi.CodeNoNode {
		t.Fatalf("the error code is %d and should be %d", got, linkapi.CodeNoNode)
	}

	payload, carried := faultAcrossTheWire(t, err)
	if !carried || payload.Code != testNodeUnknown.Code() {
		t.Fatalf("the refusal carries %v, want a fault with %s", payload.Code, testNodeUnknown.Code())
	}

	value, named := payload.Params.Get("id")
	if !named || value.String() != "0000000000000000" {
		t.Errorf("the params name %v, and the code names the id it is about", payload.Params)
	}
}

// TestARefusalWithNoFaultCarriesNone: an error the module raised without a
// registered code reaches the host as it always did, and FaultOf finds nothing.
func TestARefusalWithNoFaultCarriesNone(t *testing.T) {
	t.Parallel()

	module := &fakeLink{err: linkapi.ErrBusy}

	_, err := call(t, linkapi.Handlers(module), linkapi.MethodApprove,
		linkapi.NodeParams{ID: "26c70f1f054e7fdb"})
	if err == nil {
		t.Fatal("the handler returned no error when the module refused")
	}

	if got := codeOf(t, err); got != linkapi.CodeBusy {
		t.Fatalf("the error code is %d and should be %d", got, linkapi.CodeBusy)
	}

	if _, carried := faultAcrossTheWire(t, err); carried {
		t.Error("the refusal carries a fault and the module raised none")
	}
}

// TestFaultedIsFoundThroughWrapping: a caller can ask whether a refusal carries
// a code at all, without reading the code.
func TestFaultedIsFoundThroughWrapping(t *testing.T) {
	t.Parallel()

	refused := linkapi.Faulted(testDecisionRefused.New("the record store said no"))

	if !errors.Is(refused, linkapi.ErrFaulted) {
		t.Error("errors.Is did not find ErrFaulted on a refusal that carries a fault")
	}

	if errors.Is(linkapi.ErrBusy, linkapi.ErrFaulted) {
		t.Error("errors.Is found ErrFaulted on a refusal that carries none")
	}

	if linkapi.Faulted(nil) != nil {
		t.Error("Faulted(nil) is not nil")
	}
}

// faultAcrossTheWire reads the fault out of the error a handler returned, the
// way the client on the other side reads it: through the wirerpc error the
// handler answers with.
func faultAcrossTheWire(t *testing.T, err error) (fault.Payload, bool) {
	t.Helper()

	coded, ok := errors.AsType[*wirerpc.Error](err)
	if !ok {
		t.Fatalf("the error is not a wirerpc error: %v", err)
	}

	client := &stubCaller{err: coded}

	refused := linkapi.NewClient(client).Approve(t.Context(), linkapi.NodeParams{ID: "26c70f1f054e7fdb"})
	if refused == nil {
		t.Fatal("the client returned no error for a refusal")
	}

	return linkapi.FaultOf(refused)
}
