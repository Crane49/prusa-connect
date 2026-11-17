package event

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// testFailure is a registered code, because the error inside an envelope is a
// fault payload and an unregistered code renders redacted.
var testFailure = fault.Register(fault.DomainPrinter(), "STREAM_TEST_FAILURE", 500, fault.Document(
	"The printer refused a command",
	"Check the printer and send it again.",
	fault.RetryAllowed,
	fault.ShowDetail,
	"device_id",
))

func ttl(seconds uint32) *uint32 { return new(seconds) }

func TestEnvelopeValidate(t *testing.T) {
	t.Parallel()

	valid := Envelope{
		SpecVersion: SpecVersion,
		ID:          "01K2A0S3P4000000000000000A",
		Source:      SourceURN("site", printerA),
		Type:        "com.prusa.connect.job.finished",
		Class:       ClassState,
	}

	tests := map[string]struct {
		mutate func(*Envelope)
		want   error
	}{
		"complete":             {mutate: func(*Envelope) {}, want: nil},
		"wrong spec version":   {mutate: func(e *Envelope) { e.SpecVersion = "0.3" }, want: ErrInvalidEnvelope},
		"no id":                {mutate: func(e *Envelope) { e.ID = "" }, want: ErrInvalidEnvelope},
		"no source":            {mutate: func(e *Envelope) { e.Source = "" }, want: ErrInvalidEnvelope},
		"no type":              {mutate: func(e *Envelope) { e.Type = "" }, want: ErrInvalidEnvelope},
		"unknown class":        {mutate: func(e *Envelope) { e.Class = "guess" }, want: ErrInvalidEnvelope},
		"ttl on a non-command": {mutate: func(e *Envelope) { e.TTL = ttl(30) }, want: ErrInvalidEnvelope},
		"ttl on a command": {mutate: func(e *Envelope) {
			e.Class = ClassCommand
			e.TTL = ttl(30)
		}, want: nil},
		"past the hop limit": {mutate: func(e *Envelope) { e.Hops = MaxHops + 1 }, want: ErrHopLimit},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env := valid
			tc.mutate(&env)

			err := env.Validate()
			if tc.want == nil {
				noError(t, err)

				return
			}

			errorIs(t, err, tc.want)
		})
	}
}

func TestForwardDecrementsHopsAndExpiry(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		ttl     *uint32
		elapsed time.Duration
		wantTTL *uint32
		wantErr error
	}{
		"no expiry to decrement":           {ttl: nil, elapsed: time.Minute, wantTTL: nil},
		"whole seconds":                    {ttl: ttl(60), elapsed: 10 * time.Second, wantTTL: ttl(50)},
		"a partial second still costs one": {ttl: ttl(60), elapsed: time.Millisecond, wantTTL: ttl(59)},
		"instant hop costs nothing":        {ttl: ttl(60), elapsed: 0, wantTTL: ttl(60)},
		"spent in transit":                 {ttl: ttl(5), elapsed: 5 * time.Second, wantErr: ErrExpired},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env := Envelope{
				SpecVersion: SpecVersion,
				ID:          "01K2A0S3P4000000000000000A",
				Source:      SourceURN("site", printerA),
				Type:        "com.prusa.connect.device.pause",
				Class:       ClassCommand,
				Hops:        3,
				TTL:         tc.ttl,
			}

			out, err := env.Forward(tc.elapsed)
			if tc.wantErr != nil {
				errorIs(t, err, tc.wantErr)

				return
			}

			noError(t, err)
			equal(t, uint8(4), out.Hops)

			if tc.wantTTL == nil {
				if out.TTL != nil {
					t.Fatalf("an envelope with no expiry came back carrying %d", *out.TTL)
				}

				return
			}

			if out.TTL == nil {
				t.Fatal("an envelope carrying an expiry came back with none")
			}
			equal(t, *tc.wantTTL, *out.TTL)
			equal(t, *tc.ttl, *env.TTL, "the original envelope is not modified")
		})
	}
}

func TestForwardRefusesPastTheHopLimit(t *testing.T) {
	t.Parallel()

	env := Envelope{Hops: MaxHops}

	_, err := env.Forward(0)
	errorIs(t, err, ErrHopLimit)
}

func TestEnvelopeAttributeNames(t *testing.T) {
	t.Parallel()

	env := Envelope{
		SpecVersion: SpecVersion,
		ID:          "01K2A0S3P4000000000000000A",
		Source:      SourceURN("01K2A0SPRAGUEFARM07", mustOrigin(KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0PDC")),
		Type:        "com.prusa.connect.job.finished",
		Time:        time.Date(2026, time.August, 24, 11, 42, 7, 0, time.UTC),
		Subject:     "01JB8XN2K7QW9ZR4MV3T6H0PDC",
		Sequence:    FormatSequence(914233),
		Epoch:       "01K2A0S3P4",
		Hops:        3,
		Class:       ClassState,
		Data:        json.RawMessage(`{"result":"finished"}`),
	}

	encoded, err := json.Marshal(env)
	noError(t, err)

	var wire map[string]any
	noError(t, json.Unmarshal(encoded, &wire))

	equalAny(t, "1.0", wire["specversion"])
	equalAny(t, "urn:prusa:site:01K2A0SPRAGUEFARM07/device/01JB8XN2K7QW9ZR4MV3T6H0PDC", wire["source"])
	equalAny(t, "00000000000000914233", wire["sequence"])
	equalAny(t, "state", wire["evclass"])
	equalAny(t, 3.0, wire["hops"])
	noKey(t, wire, "datacontenttype", "JSON is the assumed content type, so the bytes are not spent")
	noKey(t, wire, "ttl", "only a command carries an expiry")
	noKey(t, wire, "expirytime", "expiry is relative and hop-decremented, never a wall clock")
}

func TestEnvelopeCarriesFaultPayload(t *testing.T) {
	t.Parallel()

	failure := testFailure.New("nozzle temperature never reached the target").
		With(fault.StringParam("device_id", "01JB8XN2K7QW9ZR4MV3T6H0PDC")).
		WithTrace("4bf92f3577b34da6a3ce929d0e0e4736")

	env, err := Envelope{
		SpecVersion: SpecVersion,
		ID:          "01K2A0S3P4000000000000000A",
		Source:      SourceURN("site", printerA),
		Type:        "com.prusa.connect.device.command.failed",
		Class:       ClassState,
	}.WithError(failure)
	noError(t, err)
	noError(t, env.Validate())

	var payload struct {
		Error struct {
			Code      string `json:"code"`
			Title     string `json:"title"`
			Detail    string `json:"detail"`
			Retryable bool   `json:"retryable"`
			TraceID   string `json:"trace_id"`
		} `json:"error"`
	}

	noError(t, json.Unmarshal(env.Data, &payload))
	equal(t, "core.printer/STREAM_TEST_FAILURE", payload.Error.Code)
	equal(t, "The printer refused a command", payload.Error.Title)
	equal(t, "nozzle temperature never reached the target", payload.Error.Detail)
	isTrue(t, payload.Error.Retryable)
	empty(t, payload.Error.TraceID, "the envelope carries the trace id as an attribute of its own")
}

func TestFormatSequenceSortsLexicographically(t *testing.T) {
	t.Parallel()

	nine := FormatSequence(9)
	ten := FormatSequence(10)

	equal(t, 20, len(nine))
	isTrue(t, nine < ten, "%q must sort before %q", nine, ten)

	parsed, err := ParseSequence(ten)
	noError(t, err)
	equal(t, uint64(10), parsed)

	_, err = ParseSequence("not a sequence")
	errorIs(t, err, ErrInvalidEnvelope)
}

func TestClassDurability(t *testing.T) {
	t.Parallel()

	isFalse(t, ClassTelemetry.Durable())

	for _, class := range []Class{ClassState, ClassCommand, ClassAudit} {
		isTrue(t, class.Durable(), "class %s", class)
		isTrue(t, class.Valid())
	}

	isFalse(t, Class("guess").Valid())
}
