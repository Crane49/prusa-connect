package event

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	generationSeparator = ";"
	cursorSeparator     = ":"
	positionSeparator   = ","
	offsetSeparator     = "="

	idFields = 3
	// decimal is the base every offset on this wire is written in.
	decimal = 10
)

// ErrInvalidCursor reports text that is not a rendered Cursor or event id.
var ErrInvalidCursor = errors.New("invalid cursor")

// Position is one event's place in one origin's stream. Offsets are per
// origin.
type Position struct {
	Origin Origin `json:"origin"`
	Offset uint64 `json:"offset"`
}

// Generation is a reader's places inside one numbering generation. An epoch
// scopes its offsets: when the process that assigns them recreates its log,
// the offsets restart under a new epoch. A generation with no position is a
// reader that knows the epoch and has no place in it.
type Generation struct {
	Epoch     string     `json:"epoch"`
	Positions []Position `json:"positions,omitempty"`
}

// Cursor is a reader's place in every stream it follows, grouped by
// generation. A reader that follows several nodes has one generation per
// node, so one node restarting invalidates only that node's positions.
//
// It renders as "epoch:kind/id=offset,kind/id=offset;epoch:kind/id=offset".
type Cursor struct {
	Generations []Generation `json:"generations,omitempty"`
}

// NewGeneration returns the canonical form: positions sorted by origin, and an
// origin at offset zero dropped.
func NewGeneration(epoch string, positions []Position) Generation {
	kept := make([]Position, 0, len(positions))

	for _, position := range positions {
		if position.Offset > 0 && !position.Origin.IsZero() {
			kept = append(kept, position)
		}
	}

	slices.SortFunc(kept, comparePositions)

	return Generation{Epoch: epoch, Positions: kept}
}

// NewCursor returns the canonical form: generations sorted by epoch, and a
// generation with no epoch dropped.
func NewCursor(generations ...Generation) Cursor {
	kept := make([]Generation, 0, len(generations))

	for _, generation := range generations {
		if generation.Epoch != "" {
			kept = append(kept, generation)
		}
	}

	slices.SortFunc(kept, func(a, b Generation) int { return strings.Compare(a.Epoch, b.Epoch) })

	return Cursor{Generations: kept}
}

// ParseCursor reads the form String renders. It refuses a cursor that names
// one epoch or one origin twice.
func ParseCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, fmt.Errorf("%w: %q names no generation", ErrInvalidCursor, s)
	}

	var (
		cursor Cursor
		seen   = map[string]struct{}{}
	)

	for field := range strings.SplitSeq(s, generationSeparator) {
		generation, err := parseGeneration(field)
		if err != nil {
			return Cursor{}, err
		}

		if _, duplicate := seen[generation.Epoch]; duplicate {
			return Cursor{}, fmt.Errorf("%w: epoch %q named twice", ErrInvalidCursor, echo(generation.Epoch))
		}

		seen[generation.Epoch] = struct{}{}
		cursor.Generations = append(cursor.Generations, generation)
	}

	return cursor, nil
}

// ParseID reads the id of one event into the cursor that resumes from it. The
// cursor is complete only when the stream follows one origin.
func ParseID(s string) (Cursor, error) {
	fields := strings.Split(s, cursorSeparator)
	if len(fields) != idFields || fields[0] == "" {
		return Cursor{}, fmt.Errorf("%w: %q is not epoch:kind/id:offset", ErrInvalidCursor, echo(s))
	}

	origin, err := ParsePath(fields[1])
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: %w", ErrInvalidCursor, err)
	}

	offset, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: offset %q: %w", ErrInvalidCursor, echo(fields[2]), err)
	}

	return NewCursor(NewGeneration(fields[0], []Position{{Origin: origin, Offset: offset}})), nil
}

// ID renders this place as the id of an event frame. It is empty when the
// offset, the origin or the epoch is zero. Telemetry has no offset, because it
// is not replayed.
func (p Position) ID(epoch string) string {
	if p.Offset == 0 || p.Origin.IsZero() || epoch == "" {
		return ""
	}

	kind, id := p.Origin.split()

	return epoch + cursorSeparator +
		kind + pathSeparator + id +
		cursorSeparator + strconv.FormatUint(p.Offset, 10)
}

// Offset returns the reader's place in one origin's stream within this
// generation, and whether the generation names that origin at all.
func (g Generation) Offset(origin Origin) (uint64, bool) {
	for _, position := range g.Positions {
		if position.Origin == origin {
			return position.Offset, true
		}
	}

	return 0, false
}

// IsZero reports whether the generation names nothing.
func (g Generation) IsZero() bool { return g.Epoch == "" }

// String renders the generation, empty for the zero value.
func (g Generation) String() string {
	if g.IsZero() {
		return ""
	}

	return string(g.appendTo(nil))
}

// Generation returns the reader's places in one numbering generation, and
// whether the cursor names that epoch.
func (c Cursor) Generation(epoch string) (Generation, bool) {
	for _, generation := range c.Generations {
		if generation.Epoch == epoch {
			return generation, true
		}
	}

	return Generation{}, false
}

// String renders the cursor, empty for the zero value.
func (c Cursor) String() string {
	if c.IsZero() {
		return ""
	}

	return string(c.appendTo(nil))
}

// AppendText appends the rendering String returns to b and returns the extended
// slice. It implements encoding.TextAppender.
func (c Cursor) AppendText(b []byte) ([]byte, error) { return c.appendTo(b), nil }

// IsZero reports whether the cursor names no stream, as a reader connecting
// for the first time sends.
func (c Cursor) IsZero() bool { return len(c.Generations) == 0 }

// Places reports how many streams this cursor names a place in, across every
// generation. A cursor with one place is equal to that event's id.
func (c Cursor) Places() int {
	places := 0
	for _, generation := range c.Generations {
		places += len(generation.Positions)
	}

	return places
}

func (c Cursor) appendTo(dst []byte) []byte {
	for i, generation := range c.Generations {
		if i > 0 {
			dst = append(dst, generationSeparator...)
		}

		dst = generation.appendTo(dst)
	}

	return dst
}

func (g Generation) appendTo(dst []byte) []byte {
	dst = append(dst, g.Epoch...)

	for i, position := range g.Positions {
		if i == 0 {
			dst = append(dst, cursorSeparator...)
		} else {
			dst = append(dst, positionSeparator...)
		}

		kind, id := position.Origin.split()

		dst = append(dst, kind...)
		dst = append(dst, pathSeparator...)
		dst = append(dst, id...)
		dst = append(dst, offsetSeparator...)
		dst = strconv.AppendUint(dst, position.Offset, decimal)
	}

	return dst
}

func parseGeneration(s string) (Generation, error) {
	epoch, rest, found := strings.Cut(s, cursorSeparator)
	if epoch == "" {
		return Generation{}, fmt.Errorf("%w: %q names no epoch", ErrInvalidCursor, echo(s))
	}

	if !found {
		return Generation{Epoch: epoch}, nil
	}

	if rest == "" {
		return Generation{}, fmt.Errorf("%w: %q ends in %q and names no position",
			ErrInvalidCursor, echo(s), cursorSeparator)
	}

	generation := Generation{Epoch: epoch}
	seen := map[Origin]struct{}{}

	for field := range strings.SplitSeq(rest, positionSeparator) {
		path, offset, split := strings.Cut(field, offsetSeparator)
		if !split {
			return Generation{}, fmt.Errorf("%w: %q is not kind/id%soffset",
				ErrInvalidCursor, echo(field), offsetSeparator)
		}

		origin, err := ParsePath(path)
		if err != nil {
			return Generation{}, fmt.Errorf("%w: %w", ErrInvalidCursor, err)
		}

		n, err := strconv.ParseUint(offset, 10, 64)
		if err != nil {
			return Generation{}, fmt.Errorf("%w: offset %q: %w", ErrInvalidCursor, echo(offset), err)
		}

		if _, duplicate := seen[origin]; duplicate {
			return Generation{}, fmt.Errorf("%w: origin %q named twice", ErrInvalidCursor, origin)
		}

		seen[origin] = struct{}{}
		generation.Positions = append(generation.Positions, Position{Origin: origin, Offset: n})
	}

	return generation, nil
}

// comparePositions orders by the canonical rendering: by kind, then by id.
func comparePositions(a, b Position) int {
	return strings.Compare(a.Origin.name, b.Origin.name)
}
