package event_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// forwardable is one event as a node ahead of this build would emit it: the
// attributes this build knows, plus two it does not, plus a data member holding
// a key that shares a name with an envelope attribute.
const forwardable = `{"specversion":"1.0","id":"01K3F9M2V8QW0R7YB4T6H0PDC5",` +
	`"source":"urn:prusa:site:01K2A0SPRAGUEFARM07/device/01JB8XN2K7QW9ZR4MV3T6H0PDC",` +
	`"type":"com.prusa.connect.job.finished","time":"2026-08-24T11:42:07.318Z",` +
	`"sequence":"00000000000000914233","epoch":"01K2A0S3P4","hops":0,"evclass":"state",` +
	`"dssematerial":"a signature this build has never heard of",` +
	`"sampledrate":17,"data":{"jobId":"01K3E7T0JOB0000000000000AA","hops":99,"ttl":5}}`

// TestAnUnknownExtensionAttributeSurvivesFourHops is ADR-20260829-t7d rules 12 and 13
// and benchmark 5 of earlier research, as a test.
//
// The failure it exists for is silent. A relay that decodes into Envelope and
// re-marshals produces valid JSON carrying every attribute this binary knows,
// so nothing downstream reports an error. What is gone is every attribute the
// binary predates, which is exactly the coupling federation exists to remove.
func TestAnUnknownExtensionAttributeSurvivesFourHops(t *testing.T) {
	t.Parallel()

	encoded := []byte(forwardable)

	for hop := 1; hop <= 4; hop++ {
		out, err := event.ForwardBytes(encoded, 0)
		if err != nil {
			t.Fatalf("hop %d: ForwardBytes() = %v", hop, err)
		}

		encoded = out
	}

	for _, attribute := range []string{
		`"dssematerial":"a signature this build has never heard of"`,
		`"sampledrate":17`,
		`"data":{"jobId":"01K3E7T0JOB0000000000000AA","hops":99,"ttl":5}`,
	} {
		if !strings.Contains(string(encoded), attribute) {
			t.Errorf("after four hops the encoding no longer holds %s:\n\t%s", attribute, encoded)
		}
	}

	var decoded event.Envelope
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal the forwarded encoding: %v", err)
	}

	if decoded.Hops != 4 {
		t.Errorf("Hops = %d after four hops, want 4", decoded.Hops)
	}

	if decoded.ID != "01K3F9M2V8QW0R7YB4T6H0PDC5" {
		t.Errorf("ID = %q, which a relay must never rewrite", decoded.ID)
	}
}

// TestForwardBytesLeavesDataUntouched holds the one case a naive scan for
// "hops" gets wrong: the payload carries a key of the same name.
func TestForwardBytesLeavesDataUntouched(t *testing.T) {
	t.Parallel()

	out, err := event.ForwardBytes([]byte(forwardable), 0)
	if err != nil {
		t.Fatalf("ForwardBytes() = %v", err)
	}

	if !strings.Contains(string(out), `"hops":99`) {
		t.Errorf("the hops key inside data was rewritten:\n\t%s", out)
	}

	if !strings.Contains(string(out), `"hops":1,`) {
		t.Errorf("the envelope's own hops was not incremented:\n\t%s", out)
	}
}

// TestForwardBytesSpendsRemainingSeconds pairs with
// TestForwardDecrementsHopsAndExpiry: the same arithmetic, on the wire form.
func TestForwardBytesSpendsRemainingSeconds(t *testing.T) {
	t.Parallel()

	const command = `{"specversion":"1.0","id":"01K3F9KZ4H0000000000000CMD",` +
		`"source":"urn:prusa:site:01K2A0SGLOBALHQ001/user/01K1USER0000000000000042",` +
		`"type":"com.prusa.connect.device.pause","hops":2,"evclass":"command","ttl":47}`

	tests := map[string]struct {
		elapsed time.Duration
		want    string
	}{
		"nothing spent":      {elapsed: 0, want: `"ttl":47`},
		"a part of a second": {elapsed: 10 * time.Millisecond, want: `"ttl":46`},
		"three seconds":      {elapsed: 3 * time.Second, want: `"ttl":44`},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, err := event.ForwardBytes([]byte(command), tc.elapsed)
			if err != nil {
				t.Fatalf("ForwardBytes() = %v", err)
			}

			if !strings.Contains(string(out), tc.want) {
				t.Errorf("ForwardBytes() gave %s, which does not hold %s", out, tc.want)
			}

			if !strings.Contains(string(out), `"hops":3`) {
				t.Errorf("ForwardBytes() gave %s, want hops 3", out)
			}
		})
	}
}

// TestForwardBytesRefusesWhatEnvelopeForwardRefuses holds the two paths to the
// same two answers. A relay that splices bytes must refuse a spent command and
// an exhausted hop count exactly as the decode path does, or the wire form and
// the struct form disagree about what may travel.
func TestForwardBytesRefusesWhatEnvelopeForwardRefuses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		encoded string
		elapsed time.Duration
		want    error
	}{
		"the hop limit": {
			encoded: `{"specversion":"1.0","id":"a","source":"b","type":"c","hops":32,"evclass":"state"}`,
			want:    event.ErrHopLimit,
		},
		"a command whose time ran out": {
			encoded: `{"specversion":"1.0","id":"a","source":"b","type":"c","hops":0,` +
				`"evclass":"command","ttl":4}`,
			elapsed: 4 * time.Second,
			want:    event.ErrExpired,
		},
		"no hops attribute": {
			encoded: `{"specversion":"1.0","id":"a","source":"b","type":"c","evclass":"state"}`,
			want:    event.ErrInvalidEnvelope,
		},
		"hops is not a number": {
			encoded: `{"specversion":"1.0","id":"a","hops":"2","evclass":"state"}`,
			want:    event.ErrInvalidEnvelope,
		},
		"not an object": {
			encoded: `["hops",1]`,
			want:    event.ErrInvalidEnvelope,
		},
		"truncated": {
			encoded: `{"specversion":"1.0","hops":`,
			want:    event.ErrInvalidEnvelope,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := event.ForwardBytes([]byte(tc.encoded), tc.elapsed); !errors.Is(err, tc.want) {
				t.Errorf("ForwardBytes() = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestForwardBytesAgreesWithEnvelopeForward walks an envelope this build minted
// through both paths and compares the two attributes a relay owns. They are one
// rule with two implementations, and the wire one is the one that ships.
func TestForwardBytesAgreesWithEnvelopeForward(t *testing.T) {
	t.Parallel()

	left := uint32(30)
	env := event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          "01K3F9KZ4H0000000000000CMD",
		Source:      "urn:prusa:site:01K2A0SGLOBALHQ001/user/01K1USER0000000000000042",
		Type:        "com.prusa.connect.device.pause",
		Hops:        7,
		Class:       event.ClassCommand,
		TTL:         &left,
	}

	const elapsed = 2 * time.Second

	want, err := env.Forward(elapsed)
	if err != nil {
		t.Fatalf("Envelope.Forward() = %v", err)
	}

	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal the envelope: %v", err)
	}

	spliced, err := event.ForwardBytes(encoded, elapsed)
	if err != nil {
		t.Fatalf("ForwardBytes() = %v", err)
	}

	var got event.Envelope
	if fail := json.Unmarshal(spliced, &got); fail != nil {
		t.Fatalf("unmarshal the spliced encoding: %v", fail)
	}

	if got.Hops != want.Hops {
		t.Errorf("Hops = %d spliced, %d decoded", got.Hops, want.Hops)
	}

	if got.TTL == nil || want.TTL == nil || *got.TTL != *want.TTL {
		t.Fatalf("TTL = %v spliced, %v decoded", got.TTL, want.TTL)
	}
}

// TestForwardBytesCarriesANullTTL holds the one shape a peer may emit that this
// build does not: ttl present and null. It is absent by another spelling, so
// forwarding neither rewrites it nor refuses the event.
func TestForwardBytesCarriesANullTTL(t *testing.T) {
	t.Parallel()

	const withNull = `{"specversion":"1.0","id":"a","source":"b","type":"c",` +
		`"hops":1,"evclass":"state","ttl":null}`

	out, err := event.ForwardBytes([]byte(withNull), time.Minute)
	if err != nil {
		t.Fatalf("ForwardBytes() = %v", err)
	}

	if !strings.Contains(string(out), `"ttl":null`) {
		t.Errorf("ForwardBytes() gave %s, want the null ttl carried through", out)
	}

	if !strings.Contains(string(out), `"hops":2`) {
		t.Errorf("ForwardBytes() gave %s, want hops 2", out)
	}
}
