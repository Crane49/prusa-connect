package datagram

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Point is one line of a metrics datagram.
//
// The firmware writes it in src/common/metric_handlers.cpp,
// textprotocol_append_point: the name, then for a custom metric its own text
// (tags and fields), or one field v=, then " %i\n" with the offset. The
// value of v is "%f" for a float, "%ii" for an integer, a quoted string, or
// T for an event. A value the firmware could not record is error="...".
type Point struct {
	// Line is the point as the printer sent it, without the newline.
	Line string
	Name string
	Tags map[string]string
	// Fields holds each field's value: a float64, an int64, a string, or a
	// bool for an event. A number that is not finite, such as nan, is a
	// string.
	Fields map[string]any
	// OffsetUS is the point's time in microseconds after the datagram's tm.
	// It is negative for a point recorded before the datagram was started.
	OffsetUS int64
}

// metrics reads a metrics datagram's header and its points. A point that
// cannot be read is kept with its line alone, so nothing the printer sent is
// lost. A datagram with more than MaxPoints points, or a point with more than
// MaxFields fields, is refused whole.
func metrics(held Datagram, message string) (Datagram, error) {
	head, body, _ := strings.Cut(message, " ")

	held.Kind = KindMetrics

	for pair := range strings.SplitSeq(head, ",") {
		name, value, found := strings.Cut(pair, "=")
		if !found {
			return Datagram{}, fmt.Errorf("%w: metrics header %q", ErrUnreadable, head)
		}

		switch name {
		case "msg":
			held.Seq, _ = strconv.ParseUint(value, 10, 64)
		case "tm":
			held.TmUS, _ = strconv.ParseInt(value, 10, 64)
		}
	}

	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimRight(line, "\r\x00")
		if strings.TrimSpace(line) == "" {
			continue
		}

		if len(held.Points) == MaxPoints {
			return Datagram{}, fmt.Errorf("%w: more than %d points", ErrUnreadable, MaxPoints)
		}

		point := ParsePoint(line)
		if len(point.Fields) > MaxFields || len(point.Tags) > MaxFields {
			return Datagram{}, fmt.Errorf("%w: a point with more than %d fields", ErrUnreadable, MaxFields)
		}

		held.Points = append(held.Points, point)
	}

	return held, nil
}

// pointParts is a point's series, its fields and its offset, which spaces
// split.
const pointParts = 3

// ParsePoint reads one line-protocol point. It never fails: a line it cannot
// split keeps Line and the name it could read. Line and Name go through Clean.
func ParsePoint(line string) Point {
	point := Point{Line: cut(Clean(line), MaxTextBytes)}

	parts := splitOutsideQuotes(line, ' ')
	if len(parts) < pointParts-1 {
		point.Name = point.Line

		return point
	}

	if offset, err := strconv.ParseInt(parts[len(parts)-1], 10, 64); err == nil && len(parts) >= pointParts {
		point.OffsetUS = offset
		parts = parts[:len(parts)-1]
	}

	// A series of separators alone, such as ",", splits into nothing.
	series := splitOutsideQuotes(parts[0], ',')
	if len(series) == 0 {
		point.Name = point.Line

		return point
	}

	point.Name = cut(Clean(series[0]), MaxTextBytes)

	for _, tag := range series[1:] {
		if key, value, found := strings.Cut(tag, "="); found {
			if point.Tags == nil {
				point.Tags = map[string]string{}
			}

			point.Tags[key] = value
		}
	}

	point.Fields = map[string]any{}

	for _, field := range splitOutsideQuotes(strings.Join(parts[1:], " "), ',') {
		key, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}

		point.Fields[key] = fieldValue(value)
	}

	return point
}

// fieldValue reads one field value in the shapes the firmware writes.
func fieldValue(value string) any {
	switch {
	case len(value) >= len(`""`) && value[0] == '"' && value[len(value)-1] == '"':
		return unescape(value[1 : len(value)-1])
	case value == "T" || value == "t" || value == "true":
		return true
	case value == "F" || value == "f" || value == "false":
		return false
	case strings.HasSuffix(value, "i"):
		if n, err := strconv.ParseInt(strings.TrimSuffix(value, "i"), 10, 64); err == nil {
			return n
		}
	}

	// The firmware prints nan for a sensor that failed. A value that is not
	// finite is kept as its text, because JSON has no number for it and the
	// text is the evidence.
	if f, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return f
	}

	return value
}

// unescape undoes the firmware's escaping of a string value, which puts a
// backslash before a quote and before a backslash.
func unescape(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}

	var out strings.Builder

	escaped := false

	for _, r := range value {
		if r == '\\' && !escaped {
			escaped = true

			continue
		}

		escaped = false

		out.WriteRune(r)
	}

	return out.String()
}

// splitOutsideQuotes splits text at sep where sep is not inside a quoted
// value. A backslash escapes the next character.
func splitOutsideQuotes(text string, sep rune) []string {
	var (
		out     []string
		start   int
		quoted  bool
		escaped bool
	)

	for i, r := range text {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			quoted = !quoted
		case r == sep && !quoted:
			if i > start {
				out = append(out, text[start:i])
			}

			start = i + 1
		}
	}

	if start < len(text) {
		out = append(out, text[start:])
	}

	return out
}
