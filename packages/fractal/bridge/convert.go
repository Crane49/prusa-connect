package bridge

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// rawNull is the value of a property whose device sent no reading for it.
//
//nolint:gochecknoglobals // one immutable JSON value, never written into.
var rawNull = json.RawMessage(`null`)

// read returns the value of one property for one device, converted to the
// declared type of the property. When the device sent no reading, or a reading
// that does not convert, read returns null for a property that allows null,
// and the default of its value table for one that does not. With no default,
// ok is false and the report leaves the property out. tool is the head or the
// slot of the instance, and empty for an instance per device.
func (p *PropertyMap) read(device deviceapi.Device, tool connectorapi.ToolID) (value json.RawMessage, ok bool) {
	raw, found := p.source(device, tool)
	if !found {
		raw = rawNull
	}

	if p.table != nil {
		return p.table.convert(raw, p.property.Nullable(), device.State)
	}

	if schema.IsNull(raw) {
		return p.missing()
	}

	value, ok = p.coerce(raw)
	if !ok {
		// The reading does not convert to the type of the property. Treat it
		// as a missing reading, so the bridge never reports a made-up value.
		return p.missing()
	}

	if p.ZeroAsNull && isZero(value) {
		return rawNull, true
	}

	return value, true
}

// isZero reports whether a number reads as 0.
func isZero(raw json.RawMessage) bool {
	number, err := schema.ReadFloat(raw)

	return err == nil && number == 0
}

// source returns the raw JSON the device reported for this property. That is
// the derived reading, the named device field as a JSON string, or the value
// at the State path with the tool of the instance in it. ok is false when the
// field is empty or the path has no value.
func (p *PropertyMap) source(device deviceapi.Device, tool connectorapi.ToolID) (raw json.RawMessage, ok bool) {
	if p.Derive != "" {
		return derive(p.Derive, device, tool)
	}

	if p.Field != "" {
		text, known := deviceField(device, p.Field)
		if !known || text == "" {
			return nil, false
		}

		if p.Encode == encodeUUID {
			return uuidBase64URL(text)
		}

		return jsonString(text), true
	}

	path := p.State
	if p.templated {
		path = strings.ReplaceAll(path, toolPlaceholder, string(tool))
	}

	raw, ok = device.State[path]
	if !ok || len(raw) == 0 {
		return nil, false
	}

	return raw, true
}

// encodeUUID writes a UUID as its 16 bytes in unpadded base64url, the form
// of Device1.uuid (domain-common.md).
const encodeUUID = "uuid-base64url"

// uuidBase64URL returns a UUID in its canonical text form as a JSON string of
// its 16 bytes in unpadded base64url, and false for any other text.
func uuidBase64URL(text string) (json.RawMessage, bool) {
	const canonical = 36

	if len(text) != canonical || text[8] != '-' || text[13] != '-' || text[18] != '-' || text[23] != '-' {
		return nil, false
	}

	const size = 16

	raw, err := hex.DecodeString(strings.ReplaceAll(text, "-", ""))
	if err != nil || len(raw) != size {
		return nil, false
	}

	return jsonString(base64.RawURLEncoding.EncodeToString(raw)), true
}

// missing returns the value of a property that has no reading: null when the
// property allows null, and false otherwise.
func (p *PropertyMap) missing() (json.RawMessage, bool) {
	if p.property.Nullable() {
		return rawNull, true
	}

	return nil, false
}

// coerce converts a reading into the declared type of the property. A printer
// may send the same reading as 37 or as 37.0. coerce returns a value that
// already has the right type unchanged. It reads any other value as a number,
// and writes it as an int, rounded, or as a float. It returns false for a
// reading that is not a number.
func (p *PropertyMap) coerce(raw json.RawMessage) (json.RawMessage, bool) {
	if p.property.Check(raw) == nil {
		return raw, true
	}

	number, err := schema.ReadFloat(raw)
	if err != nil {
		return nil, false
	}

	switch p.property.Type().Kind() {
	case schema.TypeInt:
		return schema.RawInt(int64(math.Round(number))), true
	case schema.TypeFloat:
		written, floatErr := schema.RawFloat(number)
		if floatErr != nil {
			return nil, false
		}

		return written, true
	case schema.TypeString, schema.TypeBool, schema.TypeEnum:
	}

	return nil, false
}

// convert maps one word a device reported to a value of the enumeration. While
// a print runs on the device, a word the table lists under duringJob takes
// that value. For a word the table does not list, it returns the default of
// the table. When the table has no default, ok is false and the report leaves
// the property out.
func (t *valueTable) convert(
	raw json.RawMessage, nullable bool, state map[string]json.RawMessage,
) (json.RawMessage, bool) {
	if schema.IsNull(raw) {
		if nullable {
			return rawNull, true
		}

		return t.fall()
	}

	word, err := schema.ReadString(raw)
	if err != nil {
		return t.fall()
	}

	if value, held := t.duringJob.valueOf(word, state); held {
		return value, true
	}

	if value, known := t.raw[word]; known {
		return value, true
	}

	return t.fall()
}

// valueOf returns the value of a word while a print runs, and false when the
// table names none for it or no print runs.
func (j *jobValues) valueOf(word string, state map[string]json.RawMessage) (json.RawMessage, bool) {
	if j == nil {
		return nil, false
	}

	value, listed := j.raw[word]
	if !listed {
		return nil, false
	}

	job, err := schema.ReadString(state[j.path])
	if err != nil {
		return nil, false
	}

	_, running := j.running[job]

	return value, running
}

// fall returns the default of the table, or false when it has none.
func (t *valueTable) fall() (json.RawMessage, bool) {
	if t.fallRaw == nil {
		return nil, false
	}

	return t.fallRaw, true
}

// deviceField returns one field of a device by the name mapping.json uses for
// it. It returns false for any other name, and the property is then reported
// as a missing reading. ParseMapping does not check the field names.
func deviceField(device deviceapi.Device, name string) (value string, ok bool) {
	switch name {
	case "id":
		return device.ID, true
	case "kind":
		return device.Kind, true
	case "name":
		return device.Name, true
	case "vendor":
		return device.Vendor, true
	case "model":
		return device.Model, true
	case "firmware":
		return device.Firmware, true
	case "serial":
		return device.Serial, true
	case "driver":
		return device.Driver, true
	}

	return "", false
}

// argument converts one JSON value into the text of a command argument. Every
// argument of a device command is text, and the host checks the text against
// the capability descriptor of the command. It returns false for null.
func argument(raw json.RawMessage) (text string, ok bool) {
	if len(raw) == 0 || schema.IsNull(raw) {
		return "", false
	}

	switch raw[0] {
	case '"':
		word, err := schema.ReadString(raw)
		if err != nil {
			return "", false
		}

		return word, true
	case 't', 'f':
		flag, err := schema.ReadBool(raw)
		if err != nil {
			return "", false
		}

		return strconv.FormatBool(flag), true
	}

	number, err := schema.ReadFloat(raw)
	if err != nil {
		return "", false
	}

	return strconv.FormatFloat(number, 'f', -1, 64), true
}
