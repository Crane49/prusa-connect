// Package faulttest closes the first of the three error code gates: a code the
// server returns that the registry does not document.
//
// A handler test hands the bytes it already asserts on to
// AssertBodyRegistered, which finds every code member in the document and
// fails the test for any the registry does not know. It works on whatever the
// surface answers with: an RFC 9457 problem document, an event envelope's
// data.error, a printer command result, or a list of any of those.
//
//	rec := httptest.NewRecorder()
//	handler.ServeHTTP(rec, req)
//	faulttest.AssertBodyRegistered(t, fault.Global(), rec.Body.Bytes())
//
// Passing a nil registry means the global one, which is what a server binary
// answers from.
package faulttest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// ErrMalformedCode reports a code member whose text is not a domain/REASON
// pair. A response carrying one is broken in the same way an unregistered code
// is: no client can branch on it.
var ErrMalformedCode = errors.New("faulttest: malformed code member")

// codeMember is the JSON member every rendering of a Fault carries its
// identifier in: Payload, Problem and EventError all name it code.
const codeMember = "code"

// AssertBodyRegistered fails tb unless every error code in body is registered.
// A body carrying no code passes: this gate is about codes that are returned,
// not about which responses must carry one.
func AssertBodyRegistered(tb testing.TB, registry *fault.Registry, body []byte) {
	tb.Helper()

	codes, err := Codes(body)
	if err != nil {
		tb.Errorf("faulttest: read codes from response: %v", err)

		return
	}

	for _, code := range codes {
		AssertRegistered(tb, registry, code)
	}
}

// AssertRegistered fails tb unless the registry documents c.
func AssertRegistered(tb testing.TB, registry *fault.Registry, c fault.Code) {
	tb.Helper()

	if !registryOr(registry).Registered(c) {
		tb.Errorf(
			"faulttest: %s is returned but not registered. Register it with fault.Register and run make generate-error-codes",
			c,
		)
	}
}

// Codes returns every error code the document carries, in the order they
// appear and without repeats. A code member that is not a string is left alone,
// because a member of that name can mean something else; a string that is not a
// valid code reports ErrMalformedCode.
func Codes(body []byte) ([]fault.Code, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var (
		stack []frame
		found []fault.Code
	)

	seen := make(map[fault.Code]struct{})

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			// Token reports a truncated document as a clean end of input, so a
			// container left open is the only sign that bytes are missing.
			if len(stack) > 0 {
				return nil, fmt.Errorf("faulttest: decode document: %w", io.ErrUnexpectedEOF)
			}

			return found, nil
		}

		if err != nil {
			return nil, fmt.Errorf("faulttest: decode document: %w", err)
		}

		if delim, isDelim := token.(json.Delim); isDelim {
			stack = applyDelim(stack, delim)

			continue
		}

		if key, isKey := readKey(stack, token); isKey {
			markKey(stack, key)

			continue
		}

		code, ok, err := codeToken(stack, token)
		if err != nil {
			return nil, err
		}

		if _, repeat := seen[code]; ok && !repeat {
			seen[code] = struct{}{}
			found = append(found, code)
		}

		valueConsumed(stack)
	}
}

// frame is one open JSON container. Only objects need state: which of the two
// tokens in a member comes next, and whether the key was the code member.
type frame struct {
	object    bool
	expectKey bool
	codeKey   bool
}

func registryOr(registry *fault.Registry) *fault.Registry {
	if registry == nil {
		return fault.Global()
	}

	return registry
}

func applyDelim(stack []frame, delim json.Delim) []frame {
	switch delim {
	case '{':
		return append(stack, frame{object: true, expectKey: true})
	case '[':
		return append(stack, frame{})
	default: // } or ]: the container was itself a value in its parent
		stack = stack[:len(stack)-1]
		valueConsumed(stack)

		return stack
	}
}

func readKey(stack []frame, token json.Token) (string, bool) {
	top := len(stack) - 1
	if top < 0 || !stack[top].object || !stack[top].expectKey {
		return "", false
	}

	key, ok := token.(string)

	return key, ok
}

func markKey(stack []frame, key string) {
	top := len(stack) - 1
	stack[top].codeKey = key == codeMember
	stack[top].expectKey = false
}

func codeToken(stack []frame, token json.Token) (fault.Code, bool, error) {
	top := len(stack) - 1
	if top < 0 || !stack[top].codeKey {
		return fault.Code{}, false, nil
	}

	text, isText := token.(string)
	if !isText {
		return fault.Code{}, false, nil
	}

	code, err := fault.ParseCode(text)
	if err != nil {
		return fault.Code{}, false, fmt.Errorf("%w %q: %w", ErrMalformedCode, text, err)
	}

	return code, true, nil
}

func valueConsumed(stack []frame) {
	top := len(stack) - 1
	if top < 0 || !stack[top].object {
		return
	}

	stack[top].expectKey = true
	stack[top].codeKey = false
}
