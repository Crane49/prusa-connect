package joinwire_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestAGrantSurvivesTheWire checks that a grant, the join data of its
// extensions included, has the same fields after EncodeReply and DecodeReply.
func TestAGrantSurvivesTheWire(t *testing.T) {
	t.Parallel()
	sent := fractal.JoinGrant{
		Version:        fractal.Version,
		NodePath:       "hq/brno/printer-1",
		ParentIdentity: "UPARENT",
		Credentials:    "a-user-jwt",
		ExpiresAt:      fractal.TimestampOf(time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)),
		Extensions:     map[fractal.Identifier]json.RawMessage{"files": json.RawMessage(`{"version":1}`)},
	}
	payload, err := joinwire.EncodeReply(joinwire.Grant(sent, "nats://hq.example:7422", 10*time.Second))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	read, err := joinwire.DecodeReply(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	grant, extra, err := read.ToGrant()
	if err != nil {
		t.Fatalf("read the grant: %v", err)
	}
	if !reflect.DeepEqual(grant, sent) {
		t.Errorf("the grant is %+v, want the one that was sent, %+v", grant, sent)
	}
	if want := (joinwire.Extra{LeafRemote: "nats://hq.example:7422", MaxHeartbeatIntervalMS: 10000}); extra != want {
		t.Errorf("the binding members are %+v, want %+v", extra, want)
	}
	if got := extra.HeartbeatInterval(); got != 10*time.Second {
		t.Errorf("the granted pacing is %s, want 10s", got)
	}
}

// TestADenialSurvivesTheWire checks that a denial has the same code and reason
// after EncodeReply and DecodeReply.
func TestADenialSurvivesTheWire(t *testing.T) {
	t.Parallel()
	payload, err := joinwire.EncodeReply(joinwire.Deny(fractal.JoinDenial{
		Version: fractal.Version,
		Code:    fractal.CodeJoinCollision,
		Reason:  "that node id is held",
	}))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	read, err := joinwire.DecodeReply(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	denial := read.ToDenial()
	if denial.Code != fractal.CodeJoinCollision || denial.Reason != "that node id is held" {
		t.Errorf("the denial is %+v, want the one that was sent", denial)
	}
	if _, _, grantErr := read.ToGrant(); !errors.Is(grantErr, joinwire.ErrDenied) {
		t.Errorf("a denial was read as a grant: %v", grantErr)
	}
}

// TestAFieldNobodyKnowsIsIgnored checks that DecodeReply ignores a JSON member
// it does not know.
func TestAFieldNobodyKnowsIsIgnored(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"version":1,"granted":true,"nodePath":"hq/printer-1",` +
		`"parentIdentityKey":"UPARENT","leafCredentials":"jwt","credentialsExpireAt":1,` +
		`"leafRemote":"nats://hq:7422","somethingNew":{"a":1}}`)
	read, err := joinwire.DecodeReply(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, _, grantErr := read.ToGrant(); grantErr != nil {
		t.Errorf("a field nobody knows refused the grant: %v", grantErr)
	}
}

// TestAGrantWithNoLeafnodeAddressIsRefused checks that ToGrant returns
// ErrNoLeafRemote for a grant with no leafnode address.
func TestAGrantWithNoLeafnodeAddressIsRefused(t *testing.T) {
	t.Parallel()
	reply := joinwire.Reply{Version: fractal.Version, Granted: true, NodePath: "hq/printer-1"}
	if _, _, err := reply.ToGrant(); !errors.Is(err, joinwire.ErrNoLeafRemote) {
		t.Errorf("the refusal is %v, want a missing leafnode address", err)
	}
}

// TestTheRefinementIsAJoinDenialCode checks that CodeAwaitingApproval is a
// sub-code of fractal.CodeJoinUnauthorized. A node that does not know the
// sub-code then applies the rule of fractal.CodeJoinUnauthorized.
func TestTheRefinementIsAJoinDenialCode(t *testing.T) {
	t.Parallel()
	if !joinwire.CodeAwaitingApproval.Is(fractal.CodeJoinUnauthorized) {
		t.Fatalf("%s does not refine %s", joinwire.CodeAwaitingApproval, fractal.CodeJoinUnauthorized)
	}
	if !fractal.IsJoinDenialCode(joinwire.CodeAwaitingApproval) {
		t.Error("the refinement is not a code a denial may carry")
	}
	if got := joinwire.CodeAwaitingApproval.Refinement(); got != 1 {
		t.Errorf("the refinement is %d, want 1", got)
	}
}
