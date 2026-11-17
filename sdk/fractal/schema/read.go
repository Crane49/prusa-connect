package schema

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
)

// ErrNotFinite reports that RawFloat cannot encode a float, because JSON cannot
// express a not-a-number value or an infinity.
var ErrNotFinite = errors.New("float is not a finite number")

// IsNull reports whether raw is JSON null. Call it before a Read function on
// the value of a nullable property, because every Read function returns an
// error for null.
func IsNull(raw json.RawMessage) bool {
	form, _ := classify(raw)
	return form == formNull
}

// ReadString decodes raw as a JSON string. It returns ErrWrongType when raw is
// any other JSON value, null included, and ErrMalformed when raw is not valid
// JSON.
func ReadString(raw json.RawMessage) (string, error) {
	form, b := classify(raw)
	if form != formString {
		return "", typeErr(form)
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return "", ErrMalformed
	}
	return s, nil
}

// ReadBool decodes raw as JSON true or false. It returns ErrWrongType when raw
// is any other JSON value.
func ReadBool(raw json.RawMessage) (bool, error) {
	form, b := classify(raw)
	if form != formBool {
		return false, typeErr(form)
	}
	return b[0] == 't', nil
}

// ReadInt decodes raw as an integer. It returns ErrWrongType for a number with a
// fraction or an exponent, and ErrIntRange for an integer outside the signed
// 64-bit range.
func ReadInt(raw json.RawMessage) (int64, error) {
	form, b := classify(raw)
	if form != formInteger {
		return 0, typeErr(form)
	}
	v, ok := parseInt64(b)
	if !ok {
		return 0, ErrIntRange
	}
	return v, nil
}

// ReadFloat decodes raw as a float. It accepts a number with no fraction too,
// so 1 and 1.0 both give 1. It returns ErrFloatRange for a number too large for
// a float64.
func ReadFloat(raw json.RawMessage) (float64, error) {
	form, b := classify(raw)
	if form != formInteger && form != formNumber {
		return 0, typeErr(form)
	}
	v, ok := parseFloat64(b)
	if !ok {
		return 0, ErrFloatRange
	}
	return v, nil
}

// RawString encodes a string as a JSON value.
func RawString(s string) json.RawMessage {
	return strconv.AppendQuote(make([]byte, 0, len(s)+quoteOverhead), s)
}

// quoteOverhead is the two quotes a JSON string adds around its characters.
const quoteOverhead = 2

// RawBool encodes a bool as a JSON value.
func RawBool(v bool) json.RawMessage {
	if v {
		return json.RawMessage("true")
	}
	return json.RawMessage("false")
}

// RawInt encodes an integer as a JSON value.
func RawInt(v int64) json.RawMessage {
	return strconv.AppendInt(make([]byte, 0, maxInt64Digits+1), v, decimalBase)
}

// RawFloat encodes a float as a JSON value. It returns ErrNotFinite for a
// not-a-number value and for an infinity, which JSON cannot express.
func RawFloat(v float64) (json.RawMessage, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, ErrNotFinite
	}
	return strconv.AppendFloat(make([]byte, 0, floatDigits), v, 'g', -1, floatBits), nil
}

// floatDigits is the buffer size for the longest text strconv writes for a
// float64.
const floatDigits = 24

// RawNull returns the JSON null value. A node sends it for a nullable property
// it has no value for.
func RawNull() json.RawMessage { return json.RawMessage("null") }
