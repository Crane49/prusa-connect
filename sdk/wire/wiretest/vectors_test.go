package wiretest_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wire/wiretest"
)

// errorOf is the Go sentinel each published reason names. An implementation in
// another language reads the reason and never this table.
func errorOf(t *testing.T, reason wiretest.Reason) error {
	t.Helper()

	switch reason {
	case wiretest.ReasonStreamZero:
		return wire.ErrStreamZero
	case wiretest.ReasonUnknownKind:
		return wire.ErrUnknownKind
	case wiretest.ReasonPayloadTooLarge:
		return wire.ErrPayloadTooLarge
	case wiretest.ReasonChunkTooLarge:
		return wire.ErrChunkTooLarge
	case wiretest.ReasonPayloadNotEmpty:
		return wire.ErrPayloadNotEmpty
	case wiretest.ReasonCreditLength:
		return wire.ErrCreditLength
	case wiretest.ReasonCreditZero:
		return wire.ErrCreditZero
	case wiretest.ReasonResetTooLarge:
		return wire.ErrResetTooLarge
	case wiretest.ReasonTruncated:
		return io.ErrUnexpectedEOF
	case wiretest.ReasonHandshakeLine:
		return wire.ErrHandshakeLine
	case wiretest.ReasonHandshakeRange:
		return wire.ErrHandshakeRange
	case wiretest.ReasonHandshakeAddress:
		return wire.ErrHandshakeAddress
	case wiretest.ReasonProtocolMajor:
		return wire.ErrProtocolMajor
	case wiretest.ReasonNoCommonMinor:
		return wire.ErrNoCommonMinor
	}

	t.Fatalf("reason %q names no error", reason)

	return nil
}

// TestTheWriterProducesTheBytesTheVectorStates is the encode direction. The
// bytes in the vector are written by hand, so a change to the header layout
// fails here rather than agreeing with itself.
func TestTheWriterProducesTheBytesTheVectorStates(t *testing.T) {
	t.Parallel()

	for _, vector := range wiretest.Frames() {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()

			want, err := vector.Bytes()
			if err != nil {
				t.Fatalf("build the vector: %v", err)
			}

			payload, err := vector.Payload()
			if err != nil {
				t.Fatalf("build the payload: %v", err)
			}

			var out bytes.Buffer

			frame := wire.Frame{Stream: vector.Stream, Kind: vector.Kind, Payload: payload}

			writer := wire.NewWriter(&out, 0)
			if written := writer.Write(frame); written != nil {
				t.Fatalf("write the frame: %v", written)
			}

			if !bytes.Equal(out.Bytes(), want) {
				t.Fatalf("the writer produced %d bytes and the vector states %d; first difference at byte %d",
					out.Len(), len(want), firstDifference(out.Bytes(), want))
			}
		})
	}
}

// TestTheReaderParsesTheBytesTheVectorStates is the decode direction.
func TestTheReaderParsesTheBytesTheVectorStates(t *testing.T) {
	t.Parallel()

	for _, vector := range wiretest.Frames() {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()

			raw, err := vector.Bytes()
			if err != nil {
				t.Fatalf("build the vector: %v", err)
			}

			payload, err := vector.Payload()
			if err != nil {
				t.Fatalf("build the payload: %v", err)
			}

			reader := wire.NewReader(bytes.NewReader(raw), 0)

			frame, err := reader.Next()
			if err != nil {
				t.Fatalf("read the frame: %v", err)
			}

			if frame.Stream != vector.Stream {
				t.Errorf("stream %d, and the vector states %d", frame.Stream, vector.Stream)
			}

			if frame.Kind != vector.Kind {
				t.Errorf("kind %s, and the vector states %s", frame.Kind, vector.Kind)
			}

			if !bytes.Equal(frame.Payload, payload) {
				t.Errorf("payload of %d bytes, and the vector states %d", len(frame.Payload), len(payload))
			}

			if _, after := reader.Next(); !errors.Is(after, io.EOF) {
				t.Errorf("after one frame the reader answered %v, and the vector holds one frame", after)
			}
		})
	}
}

// TestARefusedFrameIsRefusedForTheStatedReason is the half that stops the
// codec accepting everything.
func TestARefusedFrameIsRefusedForTheStatedReason(t *testing.T) {
	t.Parallel()

	for _, vector := range wiretest.Refusals() {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()

			raw, err := vector.Bytes()
			if err != nil {
				t.Fatalf("build the vector: %v", err)
			}

			reader := wire.NewReader(bytes.NewReader(raw), vector.MaxPayloadBytes)

			_, err = reader.Next()
			if err == nil {
				t.Fatalf("the reader accepted %d bytes the vector refuses", len(raw))
			}

			if want := errorOf(t, vector.Reason); !errors.Is(err, want) {
				t.Fatalf("the reader answered %v, and the vector states %s (%v)", err, vector.Reason, want)
			}
		})
	}
}

// TestARefusedFrameStopsTheReader holds the rule that a refusal is final. The
// bytes after a header nobody accepted belong to a frame nobody parsed.
func TestARefusedFrameStopsTheReader(t *testing.T) {
	t.Parallel()

	// A refused frame, then a frame that would parse on its own.
	refused, err := wiretest.Refusals()[0].Bytes()
	if err != nil {
		t.Fatalf("build the refusal: %v", err)
	}

	good, err := wiretest.Frames()[0].Bytes()
	if err != nil {
		t.Fatalf("build the frame: %v", err)
	}

	reader := wire.NewReader(bytes.NewReader(append(refused, good...)), 0)

	if _, refusal := reader.Next(); refusal == nil {
		t.Fatal("the reader accepted the refused frame")
	}

	if _, after := reader.Next(); !errors.Is(after, wire.ErrDesynchronised) {
		t.Fatalf("after a refusal the reader answered %v, and it must not read on", after)
	}
}

// TestAHandshakeLineIsReadAsTheVectorStates covers both directions: a line the
// host accepts, and one it refuses before the child has run anything.
func TestAHandshakeLineIsReadAsTheVectorStates(t *testing.T) {
	t.Parallel()

	for _, vector := range wiretest.Handshakes() {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()

			got, err := wire.ParseHandshake(vector.Line + "\n")

			if !vector.Accepted {
				if err == nil {
					t.Fatalf("the host accepted %q, and the vector refuses it", vector.Line)
				}

				if want := errorOf(t, vector.Reason); !errors.Is(err, want) {
					t.Fatalf("the host answered %v, and the vector states %s (%v)", err, vector.Reason, want)
				}

				return
			}

			if err != nil {
				t.Fatalf("the host refused %q: %v", vector.Line, err)
			}

			if got != vector.Handshake {
				t.Fatalf("the host read %+v, and the vector states %+v", got, vector.Handshake)
			}
		})
	}
}

// TestAnAcceptedHandshakeRendersBackToALine holds the rendering against the
// same vectors, so a child written against this package produces a line a host
// reads.
func TestAnAcceptedHandshakeRendersBackToALine(t *testing.T) {
	t.Parallel()

	for _, vector := range wiretest.Handshakes() {
		if !vector.Accepted {
			continue
		}

		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()

			line, err := vector.Handshake.Line()
			if err != nil {
				t.Fatalf("render the handshake: %v", err)
			}

			got, err := wire.ParseHandshake(line)
			if err != nil {
				t.Fatalf("read back the rendered line %q: %v", line, err)
			}

			if got != vector.Handshake {
				t.Fatalf("the rendered line read back as %+v, and the vector states %+v", got, vector.Handshake)
			}
		})
	}
}

// TestNegotiationSettlesOnTheVersionTheVectorStates covers the version rule in
// both directions.
func TestNegotiationSettlesOnTheVersionTheVectorStates(t *testing.T) {
	t.Parallel()

	for _, vector := range wiretest.Negotiations() {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()

			minor, err := wire.Negotiate(vector.Host, vector.Child)

			if !vector.Accepted {
				if err == nil {
					t.Fatalf("the host settled on minor %d, and the vector refuses the pair", minor)
				}

				if want := errorOf(t, vector.Reason); !errors.Is(err, want) {
					t.Fatalf("the host answered %v, and the vector states %s (%v)", err, vector.Reason, want)
				}

				return
			}

			if err != nil {
				t.Fatalf("the host refused a pair the vector accepts: %v", err)
			}

			if minor != vector.Minor {
				t.Fatalf("the host settled on minor %d, and the vector states %d", minor, vector.Minor)
			}
		})
	}
}

// TestEveryKindAndEveryReasonHasAVector is the floor. A vector set that lost
// its cases would pass every test above and prove nothing, which is how seven
// gates in this repository came to report green over nothing.
func TestEveryKindAndEveryReasonHasAVector(t *testing.T) {
	t.Parallel()

	kinds := map[wire.Kind]int{}
	for _, vector := range wiretest.Frames() {
		kinds[vector.Kind]++
	}

	for _, doc := range wire.Kinds() {
		if kinds[doc.Kind] == 0 {
			t.Errorf("kind %s appears in no frame vector", doc.Name)
		}
	}

	used := map[wiretest.Reason]int{}

	for _, vector := range wiretest.Refusals() {
		used[vector.Reason]++
	}

	for _, vector := range wiretest.Handshakes() {
		if !vector.Accepted {
			used[vector.Reason]++
		}
	}

	for _, vector := range wiretest.Negotiations() {
		if !vector.Accepted {
			used[vector.Reason]++
		}
	}

	known := map[wiretest.Reason]bool{}

	for _, reason := range wiretest.Reasons() {
		known[reason] = true

		if used[reason] == 0 {
			t.Errorf("reason %s is published and no vector uses it", reason)
		}
	}

	for reason := range used {
		if !known[reason] {
			t.Errorf("reason %s is used by a vector and is not published", reason)
		}
	}
}

// TestTheVectorSetIsNotEmpty is the second half of the floor. Every count is
// the number the set held when the gate was written, and a set that shrinks
// fails until somebody edits the number in the same change.
func TestTheVectorSetIsNotEmpty(t *testing.T) {
	t.Parallel()

	for _, count := range []struct {
		what  string
		got   int
		floor int
	}{
		{what: "frames", got: len(wiretest.Frames()), floor: 13},
		{what: "refusals", got: len(wiretest.Refusals()), floor: 13},
		{what: "handshakes", got: len(wiretest.Handshakes()), floor: 8},
		{what: "negotiations", got: len(wiretest.Negotiations()), floor: 5},
	} {
		if count.got < count.floor {
			t.Errorf("%d %s vectors, and this gate expects at least %d", count.got, count.what, count.floor)
		}
	}
}

// TestEveryVectorHasANameAndADoc keeps the published artefact readable. A
// vector with no name cannot be reported and one with no doc tells a foreign
// implementer nothing about what it holds them to.
func TestEveryVectorHasANameAndADoc(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	check := func(kind, name, doc string) {
		if name == "" {
			t.Errorf("a %s vector has no name", kind)

			return
		}

		if doc == "" {
			t.Errorf("%s vector %s has no doc", kind, name)
		}

		if seen[kind+"/"+name] {
			t.Errorf("%s vector %s is named twice", kind, name)
		}

		seen[kind+"/"+name] = true
	}

	for _, vector := range wiretest.Frames() {
		check("frame", vector.Name, vector.Doc)
	}

	for _, vector := range wiretest.Refusals() {
		check("refusal", vector.Name, vector.Doc)
	}

	for _, vector := range wiretest.Handshakes() {
		check("handshake", vector.Name, vector.Doc)
	}

	for _, vector := range wiretest.Negotiations() {
		check("negotiation", vector.Name, vector.Doc)
	}
}

// firstDifference reports where two byte strings part, for a failure message
// that names a byte rather than printing a megabyte.
func firstDifference(got, want []byte) int {
	for i := range min(len(got), len(want)) {
		if got[i] != want[i] {
			return i
		}
	}

	return min(len(got), len(want))
}
