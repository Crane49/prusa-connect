package event

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// maxOriginPart is the DNS-1123 label limit. It keeps an id from making
	// an oversized cursor or Last-Event-ID header.
	maxOriginPart = 63
	// maxEchoLen bounds how much of a rejected value an error may quote, so a
	// malformed id cannot become an oversized log line.
	maxEchoLen = 32

	// del is the one control character above the printable range.
	del = 0x7f

	// originSeparators are the bytes with a meaning in a source URN, a cursor
	// or an event id. A kind or an id must not contain one.
	originSeparators = ":,=/;"

	kindSeparator = ":"
	pathSeparator = "/"
	ellipsis      = "..."
)

// ErrInvalidOrigin reports a kind or an id that cannot be rendered into a
// source URN, a cursor or an event id.
var ErrInvalidOrigin = errors.New("invalid event origin")

// Kind is the resource kind half of an origin, as it appears in the source
// URN, the subject and the cursor. This package checks its shape only. The
// core owns the set of kinds.
type Kind string

// KindDevice is the kind of every device event, whatever the machine is. A
// printer and a camera both render as "device:<id>".
const KindDevice Kind = "device"

// Valid reports whether k can be rendered into an origin.
func (k Kind) Valid() bool { return validOriginPart(string(k)) == nil }

// String returns the kind as it appears on the wire.
func (k Kind) String() string { return string(k) }

// Origin is what an event came from: a resource kind and the instance id within
// it. Every device is KindDevice. A storage adapter, a driver and a node each
// have the kind of their resource.
//
// Build one with NewOrigin or a Parse function, which validate it. The zero
// value names nothing.
type Origin struct {
	name string
}

// NewOrigin validates both halves and returns the origin they name.
func NewOrigin(kind Kind, id string) (Origin, error) {
	if err := validOriginPart(string(kind)); err != nil {
		return Origin{}, fmt.Errorf("%w: kind %w", ErrInvalidOrigin, err)
	}

	if err := validOriginPart(id); err != nil {
		return Origin{}, fmt.Errorf("%w: id %w", ErrInvalidOrigin, err)
	}

	return Origin{name: string(kind) + kindSeparator + id}, nil
}

// ParseOrigin reads the form String renders, "kind:id".
func ParseOrigin(s string) (Origin, error) {
	kind, id, found := strings.Cut(s, kindSeparator)
	if !found {
		return Origin{}, fmt.Errorf("%w: %q has no %q separator", ErrInvalidOrigin, echo(s), kindSeparator)
	}

	if err := validOriginPart(kind); err != nil {
		return Origin{}, fmt.Errorf("%w: kind %w", ErrInvalidOrigin, err)
	}

	if err := validOriginPart(id); err != nil {
		return Origin{}, fmt.Errorf("%w: id %w", ErrInvalidOrigin, err)
	}

	return Origin{name: s}, nil
}

// ParsePath reads the form Path renders, "kind/id".
func ParsePath(s string) (Origin, error) {
	kind, id, found := strings.Cut(s, pathSeparator)
	if !found {
		return Origin{}, fmt.Errorf("%w: %q has no %q separator", ErrInvalidOrigin, echo(s), pathSeparator)
	}

	return NewOrigin(Kind(kind), id)
}

// Kind returns the resource kind.
func (o Origin) Kind() Kind {
	kind, _ := o.split()

	return Kind(kind)
}

// ID returns the instance id within that kind.
func (o Origin) ID() string {
	_, id := o.split()

	return id
}

// IsZero reports whether o was never constructed.
func (o Origin) IsZero() bool { return o.name == "" }

// String renders "kind:id", the form an envelope's subject and an authorization
// key take.
func (o Origin) String() string { return o.name }

// Path renders "kind/id", the form that goes inside a source URN and a cursor.
func (o Origin) Path() string {
	if o.IsZero() {
		return ""
	}

	kind, id := o.split()

	return kind + pathSeparator + id
}

// MarshalText renders the String form, so an Origin survives JSON despite
// having no exported fields.
func (o Origin) MarshalText() ([]byte, error) { return []byte(o.name), nil }

// UnmarshalText parses the String form.
func (o *Origin) UnmarshalText(text []byte) error {
	parsed, err := ParseOrigin(string(text))
	if err != nil {
		return err
	}

	*o = parsed

	return nil
}

// split returns the two halves without copying either.
func (o Origin) split() (kind, id string) {
	kind, id, _ = strings.Cut(o.name, kindSeparator)

	return kind, id
}

// validOriginPart rejects anything that would not survive the round trip
// through a source URN, a cursor or an event id.
func validOriginPart(s string) error {
	if s == "" {
		return errors.New("is empty")
	}

	if len(s) > maxOriginPart {
		return fmt.Errorf("%q is %d bytes, the limit is %d", echo(s), len(s), maxOriginPart)
	}

	for i := range len(s) {
		c := s[i]

		switch {
		case c <= ' ', c == del:
			return fmt.Errorf("%q contains a control character or a space", echo(s))
		case strings.IndexByte(originSeparators, c) >= 0:
			return fmt.Errorf("%q contains %q, which separates fields on the wire", echo(s), string(c))
		}
	}

	return nil
}

// echo bounds a rejected value before it reaches an error string. The cut is
// pulled back to a rune boundary so the quoted form stays readable.
func echo(s string) string {
	if len(s) <= maxEchoLen {
		return s
	}

	cut := s[:maxEchoLen]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}

	return cut + ellipsis
}
