package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
)

// valueForm is the kind of JSON value in a raw message. A number with no
// fraction and no exponent has its own form, because "int" accepts only that.
type valueForm uint8

const (
	// formInvalid means the bytes are not one complete JSON value.
	formInvalid valueForm = iota
	formNull
	formBool
	// formInteger is a number with no fraction and no exponent.
	formInteger
	// formNumber is a number with a fraction or an exponent.
	formNumber
	formString
	// formOther is an object or an array.
	formOther
)

// maxInt64Digits is the number of digits of the largest signed 64-bit integer.
// parseInt64 refuses a longer number, so its sum cannot overflow.
const maxInt64Digits = 19

// hexDigits is the number of hex digits after \u in a JSON string escape.
const hexDigits = 4

// decimalBase is the base of every JSON number.
const decimalBase = 10

// firstPrintable is the lowest character a JSON string may have without an
// escape.
const firstPrintable = 0x20

// floatBits is the size in bits of the floats this package reads and writes.
const floatBits = 64

// classify returns the form of the one JSON value in raw and its bytes without
// surrounding whitespace. Anything after the value makes it formInvalid.
func classify(raw json.RawMessage) (valueForm, []byte) {
	b := trimJSONSpace(raw)
	if len(b) == 0 {
		return formInvalid, b
	}
	switch b[0] {
	case 'n':
		if string(b) == "null" {
			return formNull, b
		}
	case 't':
		if string(b) == "true" {
			return formBool, b
		}
	case 'f':
		if string(b) == "false" {
			return formBool, b
		}
	case '"':
		if n, ok := scanString(b); ok && n == len(b) {
			return formString, b
		}
	case '{', '[':
		if json.Valid(b) {
			return formOther, b
		}
	default:
		if n, integer, ok := scanNumber(b); ok && n == len(b) {
			if integer {
				return formInteger, b
			}
			return formNumber, b
		}
	}
	return formInvalid, b
}

// trimJSONSpace removes JSON whitespace (space, tab, newline, carriage return)
// from both ends of b.
func trimJSONSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && jsonSpace(b[i]) {
		i++
	}
	for j > i && jsonSpace(b[j-1]) {
		j--
	}
	return b[i:j]
}

// jsonSpace reports whether c is whitespace between JSON tokens.
func jsonSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// asciiDigit reports whether c is an ASCII digit.
func asciiDigit(c byte) bool { return c >= '0' && c <= '9' }

// hexDigit reports whether c may appear in a \u escape.
func hexDigit(c byte) bool {
	return asciiDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// scanString reads the JSON string that starts at the quote b[0] and returns
// the index after the closing quote. It does not check for valid UTF-8.
func scanString(b []byte) (n int, ok bool) {
	for i := 1; i < len(b); {
		switch c := b[i]; {
		case c == '"':
			return i + 1, true
		case c == '\\':
			next, good := scanEscape(b, i)
			if !good {
				return 0, false
			}
			i = next
		case c < firstPrintable:
			return 0, false
		default:
			i++
		}
	}
	return 0, false
}

// scanEscape reads the escape sequence that starts at the backslash b[i] and
// returns the index after it.
func scanEscape(b []byte, i int) (n int, ok bool) {
	i++
	if i >= len(b) {
		return 0, false
	}
	switch b[i] {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return i + 1, true
	case 'u':
		if i+hexDigits >= len(b) {
			return 0, false
		}
		for j := 1; j <= hexDigits; j++ {
			if !hexDigit(b[i+j]) {
				return 0, false
			}
		}
		return i + hexDigits + 1, true
	}
	return 0, false
}

// scanNumber reads the JSON number at the start of b and returns the index
// after it. integer is true when it has neither a fraction nor an exponent.
func scanNumber(b []byte) (n int, integer, ok bool) {
	i, ok := scanIntegerPart(b)
	if !ok {
		return 0, false, false
	}
	integer = true
	if next, has, good := scanFraction(b, i); has {
		if !good {
			return 0, false, false
		}
		i, integer = next, false
	}
	if next, has, good := scanExponent(b, i); has {
		if !good {
			return 0, false, false
		}
		i, integer = next, false
	}
	return i, integer, true
}

// scanIntegerPart reads the minus sign and the digits before the decimal point.
// In JSON a number that starts with 0 has no more digits before the point.
func scanIntegerPart(b []byte) (n int, ok bool) {
	i := 0
	if i < len(b) && b[i] == '-' {
		i++
	}
	start := i
	if i < len(b) && b[i] == '0' {
		i++
	} else {
		for i < len(b) && asciiDigit(b[i]) {
			i++
		}
	}
	if i == start {
		return 0, false
	}
	return i, true
}

// scanFraction reads the decimal point and the digits after it. has is false
// when b[i] is not a decimal point. good is false when no digit follows.
func scanFraction(b []byte, i int) (n int, has, good bool) {
	if i >= len(b) || b[i] != '.' {
		return i, false, true
	}
	i++
	digits := i
	for i < len(b) && asciiDigit(b[i]) {
		i++
	}
	return i, true, i > digits
}

// scanExponent reads the exponent: "e" or "E", an optional sign, and digits.
// has is false when b[i] is not "e" or "E". good is false with no digit.
func scanExponent(b []byte, i int) (n int, has, good bool) {
	if i >= len(b) || b[i] != 'e' && b[i] != 'E' {
		return i, false, true
	}
	i++
	if i < len(b) && (b[i] == '+' || b[i] == '-') {
		i++
	}
	digits := i
	for i < len(b) && asciiDigit(b[i]) {
		i++
	}
	return i, true, i > digits
}

// parseInt64 converts an integer that scanNumber has accepted. ok is false when
// the value is outside the signed 64-bit range.
func parseInt64(b []byte) (v int64, ok bool) {
	neg := b[0] == '-'
	digits := b
	if neg {
		digits = b[1:]
	}
	if len(digits) > maxInt64Digits {
		return 0, false
	}
	var u uint64
	for _, c := range digits {
		u = u*decimalBase + uint64(c-'0')
	}
	switch {
	case neg && u == uint64(math.MaxInt64)+1:
		return math.MinInt64, true
	case u > uint64(math.MaxInt64):
		return 0, false
	case neg:
		return -int64(u), true
	}
	return int64(u), true
}

// parseFloat64 converts a number that scanNumber has accepted. ok is false when
// the number is too large for a float64. A number too small converts to zero.
func parseFloat64(b []byte) (v float64, ok bool) {
	v, err := strconv.ParseFloat(string(b), floatBits)
	if err == nil {
		return v, true
	}
	if errors.Is(err, strconv.ErrRange) && !math.IsInf(v, 0) {
		return v, true
	}
	return 0, false
}

// plainString returns the bytes between the quotes of a JSON string that has no
// escape. ok is false when the string has a backslash.
func plainString(b []byte) (s []byte, ok bool) {
	inner := b[1 : len(b)-1]
	if bytes.IndexByte(inner, '\\') >= 0 {
		return nil, false
	}
	return inner, true
}
