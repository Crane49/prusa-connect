package event

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// hopsKey and ttlKey are the only two attributes a relay may change. Everything
// else is copied byte for byte.
const (
	hopsKey = "hops"
	ttlKey  = "ttl"
)

// span is one attribute's value: the half-open byte range it occupies in the
// encoding that arrived, and the text that replaces it.
type span struct {
	start       int
	end         int
	replacement string
}

// ForwardBytes returns the encoded envelope as the next hop should see it: the
// bytes that arrived, with hops incremented and any remaining time reduced by
// elapsed. Every other byte and the attribute order stay as the origin wrote
// them, so unknown attributes and signatures survive. Use it to forward. Do
// not re-encode an Envelope.
func ForwardBytes(encoded []byte, elapsed time.Duration) ([]byte, error) {
	spans, err := forwardSpans(encoded, elapsed)
	if err != nil {
		return nil, err
	}

	out := make([]byte, 0, len(encoded)+len(spans))
	cursor := 0

	for _, s := range spans {
		out = append(out, encoded[cursor:s.start]...)
		out = append(out, s.replacement...)
		cursor = s.end
	}

	return append(out, encoded[cursor:]...), nil
}

// forwardSpans finds the two mutable attributes at the top level of the
// encoding and computes their next values. It does not descend into data.
func forwardSpans(encoded []byte, elapsed time.Duration) ([]span, error) {
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.UseNumber()

	if err := expectObject(dec); err != nil {
		return nil, err
	}

	var (
		spans []span
		found bool
	)

	for dec.More() {
		name, err := attributeName(dec)
		if err != nil {
			return nil, err
		}

		var (
			at   *span
			fail error
		)

		switch name {
		case hopsKey:
			at, fail = nextHops(dec)
			found = true
		case ttlKey:
			at, fail = nextTTL(dec, elapsed)
		default:
			fail = skipValue(dec)
		}

		if fail != nil {
			return nil, fail
		}

		if at != nil {
			spans = append(spans, *at)
		}
	}

	if !found {
		return nil, fmt.Errorf("%w: no %s attribute", ErrInvalidEnvelope, hopsKey)
	}

	return spans, nil
}

// nextHops reads the hop count and returns the span that replaces it.
func nextHops(dec *json.Decoder) (*span, error) {
	at, literal, err := readNumber(dec, hopsKey)
	if err != nil {
		return nil, err
	}

	count, err := literal.Int64()
	if err != nil || count < 0 {
		return nil, fmt.Errorf("%w: %s %q is not a count", ErrInvalidEnvelope, hopsKey, literal.String())
	}

	if count >= MaxHops {
		return nil, fmt.Errorf("%w: %d hops", ErrHopLimit, count)
	}

	at.replacement = strconv.FormatInt(count+1, 10)

	return at, nil
}

// nextTTL reads a command's remaining seconds and returns the span that
// replaces it, and nil for a null value.
func nextTTL(dec *json.Decoder, elapsed time.Duration) (*span, error) {
	at, literal, err := readNumber(dec, ttlKey)
	if err != nil || at == nil {
		return nil, err
	}

	left, err := literal.Int64()
	if err != nil || left < 0 {
		return nil, fmt.Errorf("%w: %s %q is not a duration", ErrInvalidEnvelope, ttlKey, literal.String())
	}

	spent := int64(secondsSpent(elapsed))
	if spent >= left {
		return nil, fmt.Errorf("%w: %d seconds left, %d spent", ErrExpired, left, spent)
	}

	at.replacement = strconv.FormatInt(left-spent, 10)

	return at, nil
}

// readNumber returns the span the value occupies and its literal text, and a
// nil span for a null.
func readNumber(dec *json.Decoder, name string) (*span, json.Number, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, "", fmt.Errorf("%w: read %s: %w", ErrInvalidEnvelope, name, err)
	}

	if tok == nil {
		return nil, "", nil
	}

	literal, ok := tok.(json.Number)
	if !ok {
		return nil, "", fmt.Errorf("%w: %s is %T, want a number", ErrInvalidEnvelope, name, tok)
	}

	end := int(dec.InputOffset())

	return &span{start: end - len(literal.String()), end: end}, literal, nil
}

// attributeName reads the next key of the object being walked.
func attributeName(dec *json.Decoder) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", fmt.Errorf("%w: read an attribute name: %w", ErrInvalidEnvelope, err)
	}

	name, ok := tok.(string)
	if !ok {
		return "", fmt.Errorf("%w: attribute name is %T, want a string", ErrInvalidEnvelope, tok)
	}

	return name, nil
}

// skipValue consumes one whole value, however deeply nested. A key named hops
// inside data is not the envelope's hop count.
func skipValue(dec *json.Decoder) error {
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("%w: read an attribute value: %w", ErrInvalidEnvelope, err)
	}

	return nil
}

// expectObject consumes the opening brace and refuses anything else.
func expectObject(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("%w: read the opening brace: %w", ErrInvalidEnvelope, err)
	}

	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("%w: encoding starts with %v, want an object", ErrInvalidEnvelope, tok)
	}

	return nil
}
