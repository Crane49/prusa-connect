package fractal_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestNewRPCID tests the form of a generated id: a version 4 UUID in base64url
// without padding, which is RPCIDLength characters. It also tests that
// NewRPCID does not return the same id twice.
func TestNewRPCID(t *testing.T) {
	t.Parallel()
	seen := make(map[fractal.RPCID]bool, 1000)
	for range 1000 {
		id, err := fractal.NewRPCID()
		if err != nil {
			t.Fatalf("NewRPCID: %v", err)
		}
		if len(id) != fractal.RPCIDLength {
			t.Fatalf("id %q is %d characters, want %d", id, len(id), fractal.RPCIDLength)
		}
		if bad := id.Validate(); bad != nil {
			t.Fatalf("Validate refused a generated id: %v", bad)
		}
		if seen[id] {
			t.Fatalf("NewRPCID returned id %q twice", id)
		}
		seen[id] = true
		uuid, ok := id.UUID()
		if !ok {
			t.Fatalf("UUID could not decode the generated id %q", id)
		}
		if uuid[6]&0xF0 != 0x40 || uuid[8]&0xC0 != 0x80 {
			t.Fatalf("id %q is not a version 4 UUID", id)
		}
	}
}

// TestRPCIDGrammar tests which ids Validate accepts: characters of the base64url
// alphabet, no padding, and at most RPCIDLength characters.
func TestRPCIDGrammar(t *testing.T) {
	t.Parallel()
	cases := []nameCase{
		{name: "the example of the specification", in: "dGVzdA", ok: true},
		{name: "a full uuid", in: strings.Repeat("A", 22), ok: true},
		{name: "hyphen and underscore", in: "a-b_c", ok: true},
		{name: "one character", in: "a", ok: true},
		{name: "empty", in: "", is: fractal.ErrEmpty},
		{name: "over twenty two characters", in: strings.Repeat("A", 23), is: fractal.ErrTooLong},
		{name: "padding", in: "dGVzdA==", is: fractal.ErrBadRPCID},
		{name: "standard base64 plus", in: "a+b", is: fractal.ErrBadRPCID},
		{name: "standard base64 slash", in: "a/b", is: fractal.ErrBadRPCID},
		{name: "space", in: "a b", is: fractal.ErrBadRPCID},
	}
	checkNameCases(t, cases, func(s string) error { return fractal.RPCID(s).Validate() })
}

// TestRPCIDUUID tests that UUID returns ok false for an id that does not encode
// 16 bytes, and ok true for a generated id.
func TestRPCIDUUID(t *testing.T) {
	t.Parallel()
	if _, ok := fractal.RPCID("dGVzdA").UUID(); ok {
		t.Error("UUID returned ok for a six character id")
	}
	if _, ok := fractal.RPCID(strings.Repeat("!", 22)).UUID(); ok {
		t.Error("UUID returned ok for an id with characters outside the base64url alphabet")
	}
	id, err := fractal.NewRPCID()
	if err != nil {
		t.Fatalf("NewRPCID: %v", err)
	}
	if _, ok := id.UUID(); !ok {
		t.Error("UUID returned ok false for a generated id")
	}
}

// TestParseRPCIDReturnsTheID tests that ParseRPCID returns a valid string
// unchanged, and returns ErrBadRPCID for a string with a space.
func TestParseRPCIDReturnsTheID(t *testing.T) {
	t.Parallel()
	got, err := fractal.ParseRPCID("dGVzdA")
	if err != nil || got.String() != "dGVzdA" {
		t.Fatalf("ParseRPCID = %q %v", got, err)
	}
	_, err = fractal.ParseRPCID("a b")
	if !errors.Is(err, fractal.ErrBadRPCID) {
		t.Fatalf("ParseRPCID accepted a space: %v", err)
	}
}
