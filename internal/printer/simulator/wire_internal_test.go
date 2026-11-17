package simulator

import (
	"errors"
	"testing"
)

// The nine-byte header is frozen, so the reader is checked against the shapes
// a hostile or a truncated frame takes rather than only against the good one.
//
// It is deliberately stricter than a printer where the behaviour is unknown.
// An id that is not eight hex digits and a
// type letter the framing does not define are both refused rather than guessed
// at, so a driver that emits one fails here instead of in the field.
func TestTheFramingRefusesEveryMalformedHeader(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		frame string
		want  error
	}{
		{name: "empty", frame: "", want: ErrShortFrame},
		{name: "header one byte short", frame: "J0000001", want: ErrShortFrame},
		{name: "an undefined type letter", frame: "X00000001{}", want: ErrUnknownFrameType},
		{name: "a lower-case type letter", frame: "j00000001{}", want: ErrUnknownFrameType},
		{name: "an id that is not hex", frame: "J0000000g{}", want: nil},
		{name: "an id with a sign", frame: "J-0000001{}", want: nil},
		{name: "an id in decimal padding", frame: "J 0000001{}", want: nil},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeFrame([]byte(testCase.frame))
			if err == nil {
				t.Fatalf("decoded %q, want a refusal", testCase.frame)
			}

			if testCase.want != nil && !errors.Is(err, testCase.want) {
				t.Fatalf("decoding %q gave %v, want %v", testCase.frame, err, testCase.want)
			}
		})
	}
}

// The header is one type letter and eight hex digits of a uint32, so the whole
// id range has to survive the trip, including the top bit that a signed parse
// would lose.
func TestTheFramingReadsTheWholeIDRange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		frame string
		kind  byte
		id    uint32
		body  string
	}{
		{frame: "J00000000{}", kind: frameJSON, id: 0, body: "{}"},
		{frame: "J00000001{}", kind: frameJSON, id: 1, body: "{}"},
		{frame: "GdeadbeefG28", kind: frameGCode, id: 0xdeadbeef, body: "G28"},
		{frame: "Fffffffff M112", kind: frameForced, id: 0xffffffff, body: " M112"},
		{frame: "D00000abchello", kind: frameDebug, id: 0xabc, body: "hello"},
		{frame: "T0000031500", kind: frameTransfer, id: 0x315, body: "00"},
		{frame: "JABCDEF12{}", kind: frameJSON, id: 0xabcdef12, body: "{}"},
	}

	for _, testCase := range cases {
		t.Run(testCase.frame, func(t *testing.T) {
			t.Parallel()

			decoded, err := decodeFrame([]byte(testCase.frame))
			if err != nil {
				t.Fatalf("decode %q: %v", testCase.frame, err)
			}

			if decoded.kind != testCase.kind {
				t.Errorf("type is %q, want %q", string(decoded.kind), string(testCase.kind))
			}

			if decoded.id != testCase.id {
				t.Errorf("id is %d, want %d", decoded.id, testCase.id)
			}

			if string(decoded.payload) != testCase.body {
				t.Errorf("payload is %q, want %q", decoded.payload, testCase.body)
			}
		})
	}
}

// A firmware-side path is storage-prefixed with a leading slash, and the
// mountpoint is what decides which device a command lands on.
func TestTheMountpointIsReadOutOfTheFirmwarePath(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"/usb/model.gcode":     "/usb",
		"/usb/sub/model.gcode": "/usb",
		"/usb":                 "/usb",
		"/":                    "/",
		"usb/model.gcode":      "",
		"":                     "",
	}

	for path, want := range cases {
		if got := mountpointOf(path); got != want {
			t.Errorf("mountpointOf(%q) is %q, want %q", path, got, want)
		}
	}
}
