package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/danielgtaylor/huma/v2"
)

// errDuplicateMember reports a JSON object carrying the same member twice.
var errDuplicateMember = errors.New("duplicate member")

// strictJSONFormat is the body decoder for this transport. It is Huma's own
// with one refusal added: an object that names a member twice.
//
// encoding/json resolves a duplicate last-wins and says nothing, so
// {"type":"local","type":"evil"} is a write of evil that every intermediary,
// log line and reviewer reading the request sees as local. type decides which
// implementation an instance is built from, so the disagreement is answered
// 422 and never resolved in silence.
var strictJSONFormat = huma.Format{
	Marshal:   huma.DefaultJSONFormat.Marshal,
	Unmarshal: unmarshalStrictJSON,
}

// strictJSONFormats is the format table, keyed the way Huma keys its own: by
// media type and by the short name the Accept negotiation uses.
func strictJSONFormats() map[string]huma.Format {
	return map[string]huma.Format{
		"application/json": strictJSONFormat,
		"json":             strictJSONFormat,
	}
}

func unmarshalStrictJSON(data []byte, v any) error {
	if err := rejectDuplicateMembers(data); err != nil {
		return err
	}

	//nolint:wrapcheck // Huma reports this verbatim as the body's error detail; a prefix would only repeat.
	return json.Unmarshal(data, v)
}

// jsonFrame is one open container. expectName alternates so a string that is a
// member's value is not mistaken for a second member of that name: {"a":"a"} is
// legal and must stay legal.
type jsonFrame struct {
	object     bool
	expectName bool
	members    map[string]struct{}
}

// rejectDuplicateMembers walks the token stream and fails on the second
// appearance of a member name in one object. It is a separate pass and not a
// custom decoder because the body is already bounded by bodyLimitMiddleware,
// and because the members this transport passes through verbatim, the opaque
// config block, have to be checked too.
func rejectDuplicateMembers(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var stack []jsonFrame

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			// A malformed body is json.Unmarshal's to report, in the words it
			// already uses. This pass only refuses what that one accepts.
			return nil //nolint:nilerr // deliberate: the second pass produces the message.
		}

		if stack, err = step(stack, token); err != nil {
			return err
		}
	}
}

func step(stack []jsonFrame, token json.Token) ([]jsonFrame, error) {
	if delim, ok := token.(json.Delim); ok {
		if delim == '{' || delim == '[' {
			return append(stack, jsonFrame{
				object:     delim == '{',
				expectName: delim == '{',
				members:    map[string]struct{}{},
			}), nil
		}

		return valueDone(stack[:len(stack)-1]), nil
	}

	if len(stack) == 0 {
		return stack, nil
	}

	top := &stack[len(stack)-1]
	if !top.object || !top.expectName {
		return valueDone(stack), nil
	}

	name, ok := token.(string)
	if !ok {
		return stack, nil
	}

	if _, seen := top.members[name]; seen {
		return stack, fmt.Errorf("%w %q: a JSON object may name a member once", errDuplicateMember, name)
	}

	top.members[name] = struct{}{}
	top.expectName = false

	return stack, nil
}

// valueDone marks the innermost object as ready for the next member name. It is
// called when a value ends, whether that value was a scalar or a container.
func valueDone(stack []jsonFrame) []jsonFrame {
	if len(stack) > 0 && stack[len(stack)-1].object {
		stack[len(stack)-1].expectName = true
	}

	return stack
}
