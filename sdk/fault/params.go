package fault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ParamKind is the JSON type of a ParamValue.
type ParamKind uint8

// The scalar kinds of a param.
const (
	// KindString is a text value.
	KindString ParamKind = iota + 1
	// KindInt is a whole number, kept out of float64 so large ids survive.
	KindInt
	// KindFloat is a fractional number.
	KindFloat
	// KindBool is a boolean.
	KindBool
)

// Param is one interpolation value, named by the key a message bundle expects.
type Param struct {
	Key   string
	Value ParamValue
}

// StringParam returns a text param.
func StringParam(key, value string) Param {
	return Param{Key: key, Value: ParamValue{kind: KindString, str: value}}
}

// IntParam returns a whole-number param.
func IntParam(key string, value int64) Param {
	return Param{Key: key, Value: ParamValue{kind: KindInt, integer: value}}
}

// FloatParam returns a fractional-number param.
func FloatParam(key string, value float64) Param {
	return Param{Key: key, Value: ParamValue{kind: KindFloat, number: value}}
}

// BoolParam returns a boolean param.
func BoolParam(key string, value bool) Param {
	return Param{Key: key, Value: ParamValue{kind: KindBool, boolean: value}}
}

// ErrInvalidParamValue reports a param that is not one of the scalar kinds.
var ErrInvalidParamValue = errors.New("fault: invalid param value")

// ParamValue is a JSON scalar: a string, a whole number, a fractional number
// or a boolean.
//
//nolint:recvcheck // encoding.Unmarshaler mandates a pointer receiver; every other method is a value method.
type ParamValue struct {
	kind    ParamKind
	str     string
	integer int64
	number  float64
	boolean bool
}

// Kind returns the JSON type of the value.
func (v ParamValue) Kind() ParamKind { return v.kind }

// String renders the value as text for an English message. It is not the JSON
// encoding: a string is returned unquoted.
func (v ParamValue) String() string {
	switch v.kind {
	case KindString:
		return v.str
	case KindInt:
		return strconv.FormatInt(v.integer, 10)
	case KindFloat:
		return strconv.FormatFloat(v.number, 'g', -1, 64)
	case KindBool:
		return strconv.FormatBool(v.boolean)
	default:
		return ""
	}
}

// MarshalJSON encodes the value as its JSON scalar.
func (v ParamValue) MarshalJSON() ([]byte, error) {
	switch v.kind {
	case KindString:
		return marshalScalar(v.str)
	case KindInt:
		return marshalScalar(v.integer)
	case KindFloat:
		return marshalScalar(v.number)
	case KindBool:
		return marshalScalar(v.boolean)
	default:
		return []byte("null"), nil
	}
}

// UnmarshalJSON reads a scalar back into its kind. A whole number stays an
// integer, so a large id survives the round trip.
func (v *ParamValue) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidParamValue, err)
	}

	switch value := decoded.(type) {
	case string:
		*v = ParamValue{kind: KindString, str: value}
	case bool:
		*v = ParamValue{kind: KindBool, boolean: value}
	case json.Number:
		return v.unmarshalNumber(value)
	default:
		return fmt.Errorf("%w: %s is not a scalar", ErrInvalidParamValue, data)
	}

	return nil
}

func (v *ParamValue) unmarshalNumber(number json.Number) error {
	if integer, err := number.Int64(); err == nil {
		*v = ParamValue{kind: KindInt, integer: integer}

		return nil
	}

	fractional, err := number.Float64()
	if err != nil {
		return fmt.Errorf("%w: %q: %w", ErrInvalidParamValue, number, err)
	}

	*v = ParamValue{kind: KindFloat, number: fractional}

	return nil
}

// Params are the values a client interpolates into its own translation of a
// code. Every value that appears in the English message appears here too.
type Params []Param

// Keys returns the param names in order.
func (p Params) Keys() []string {
	keys := make([]string, 0, len(p))
	for _, param := range p {
		keys = append(keys, param.Key)
	}

	return keys
}

// Get returns the value stored under key.
func (p Params) Get(key string) (ParamValue, bool) {
	for _, param := range p {
		if param.Key == key {
			return param.Value, true
		}
	}

	return ParamValue{}, false
}

// MarshalJSON encodes the params as a JSON object in insertion order. An empty
// set encodes as {}, never null.
func (p Params) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteByte('{')

	for i, param := range p {
		if i > 0 {
			buf.WriteByte(',')
		}

		key, err := marshalScalar(param.Key)
		if err != nil {
			return nil, err
		}

		value, err := param.Value.MarshalJSON()
		if err != nil {
			return nil, err
		}

		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(value)
	}

	buf.WriteByte('}')

	return buf.Bytes(), nil
}

// UnmarshalJSON reads the object back in the order it was written.
func (p *Params) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*p = nil

		return nil
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	if open, err := decoder.Token(); err != nil || open != json.Delim('{') {
		return fmt.Errorf("%w: %s is not an object", ErrInvalidParamValue, data)
	}

	out := make(Params, 0)

	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidParamName, err)
		}

		name, ok := key.(string)
		if !ok {
			return fmt.Errorf("%w: %v", ErrInvalidParamName, key)
		}

		var value ParamValue
		if decodeErr := decoder.Decode(&value); decodeErr != nil {
			return decodeErr
		}

		out = append(out, Param{Key: name, Value: value})
	}

	*p = out

	return nil
}

func marshalScalar(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("fault: encode param: %w", err)
	}

	return encoded, nil
}
