package wirerpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Version is the JSON-RPC version of a control frame. A peer that states
// another is refused.
const Version = "2.0"

// The JSON-RPC 2.0 error codes this package mints. The specification reserves
// the range from -32768 to -32000, and a contract numbers its own errors
// outside it.
const (
	// CodeInvalidRequest reports an object that is not a usable request.
	CodeInvalidRequest = -32600

	// CodeMethodNotFound reports a method the answering side does not serve.
	CodeMethodNotFound = -32601

	// CodeInvalidParams reports params a method could not read.
	CodeInvalidParams = -32602

	// CodeInternal reports a handler that failed for a reason of its own.
	CodeInternal = -32603

	// CodeResultTooLarge reports an answer that does not fit in one control
	// frame. The answering side sends this error in its place. It is in the
	// range JSON-RPC 2.0 leaves to an implementation.
	CodeResultTooLarge = -32001
)

// The refusals the control codec returns.
var (
	// ErrBatch reports a JSON-RPC batch, which this wire does not accept.
	ErrBatch = errors.New("wirerpc: a control frame carries one object and never a batch")

	// ErrControlObject reports a payload that is not one JSON-RPC object, or
	// one that is neither a request nor a response.
	ErrControlObject = errors.New("wirerpc: control payload is not one JSON-RPC object")

	// ErrVersion reports an object whose jsonrpc member is not "2.0".
	ErrVersion = errors.New("wirerpc: control object does not state jsonrpc 2.0")

	// ErrMethodName reports a method name outside the grammar Method.Valid
	// states.
	ErrMethodName = errors.New("wirerpc: method name is not usable")
)

// maxMethodBytes bounds a method name.
const maxMethodBytes = 128

// Method names one call in a method set. A contract publishes its own set, and
// the lifecycle set in this package is the one every child answers.
type Method string

// Valid reports whether the name is usable: non-empty UTF-8 of at most 128
// bytes, with no control character or space, and not starting with the "rpc."
// prefix JSON-RPC 2.0 reserves.
func (m Method) Valid() bool {
	if m == "" || len(m) > maxMethodBytes {
		return false
	}

	if !utf8.ValidString(string(m)) || strings.HasPrefix(string(m), "rpc.") {
		return false
	}

	return !strings.ContainsFunc(string(m), func(r rune) bool {
		return r < 0x21 || r == 0x7f
	})
}

// Request is one call or one notification. A request with no ID is a
// notification, which gets no answer.
type Request struct {
	// JSONRPC states the version and is always Version.
	JSONRPC string `json:"jsonrpc"`
	// Method names the call in the method set the peer publishes.
	Method Method `json:"method"`
	// Params is the argument object, as the method documents it.
	Params json.RawMessage `json:"params,omitempty"`
	// ID is unique within its stream. It is absent on a notification.
	ID json.RawMessage `json:"id,omitempty"`
}

// Response is one answer. One of Result and Error is set, never both.
type Response struct {
	// JSONRPC states the version and is always Version.
	JSONRPC string `json:"jsonrpc"`
	// Result is what the method returned.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is why the method did not return a result.
	Error *Error `json:"error,omitempty"`
	// ID is the id of the request this answers.
	ID json.RawMessage `json:"id"`
}

// Error is the failure a method reports. A handler returns one to choose the
// code and the message the peer reads. Any other error becomes CodeInternal.
//
// The message reaches the peer's log, so put no secret and no private path
// in it.
type Error struct {
	// Code is the JSON-RPC error code.
	Code int `json:"code"`
	// Message is one sentence saying what went wrong.
	Message string `json:"message"`
	// Data is what the method adds for a caller that can act on it.
	Data json.RawMessage `json:"data,omitempty"`
}

// Error returns the code and the message.
func (e *Error) Error() string {
	return fmt.Sprintf("wirerpc: %d: %s", e.Code, e.Message)
}

// IsNotification reports a request that expects no answer.
func (r Request) IsNotification() bool {
	return len(r.ID) == 0
}

// control is one decoded control payload. One member is set.
type control struct {
	request  *Request
	response *Response
}

// decodeControl reads one control payload. A request has a method. A response
// has a result or an error. An object that is both, or neither, is refused.
func decodeControl(payload []byte) (control, error) {
	trimmed := bytes.TrimLeft(payload, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return control{}, ErrBatch
	}

	var read struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  *Method         `json:"method"`
		Params  json.RawMessage `json:"params"`
		Result  json.RawMessage `json:"result"`
		Error   *Error          `json:"error"`
		ID      json.RawMessage `json:"id"`
	}

	if err := json.Unmarshal(payload, &read); err != nil {
		return control{}, fmt.Errorf("%w: %w", ErrControlObject, err)
	}

	if read.JSONRPC != Version {
		return control{}, fmt.Errorf("%q: %w", read.JSONRPC, ErrVersion)
	}

	answers := read.Result != nil || read.Error != nil

	switch {
	case read.Method != nil && !answers:
		if !read.Method.Valid() {
			return control{}, fmt.Errorf("%q: %w", *read.Method, ErrMethodName)
		}

		return control{request: &Request{
			JSONRPC: read.JSONRPC,
			Method:  *read.Method,
			Params:  read.Params,
			ID:      read.ID,
		}}, nil
	case read.Method == nil && answers:
		return control{response: &Response{
			JSONRPC: read.JSONRPC,
			Result:  read.Result,
			Error:   read.Error,
			ID:      read.ID,
		}}, nil
	default:
		return control{}, ErrControlObject
	}
}

// encodeRequest renders a call or a notification.
func encodeRequest(method Method, params any, id json.RawMessage) ([]byte, error) {
	if !method.Valid() {
		return nil, fmt.Errorf("%q: %w", method, ErrMethodName)
	}

	encoded, err := encodeParams(params)
	if err != nil {
		return nil, err
	}

	object, err := json.Marshal(Request{JSONRPC: Version, Method: method, Params: encoded, ID: id})
	if err != nil {
		return nil, fmt.Errorf("encode a %s request: %w", method, err)
	}

	return object, nil
}

// encodeParams renders the argument object of a request. A nil params is an
// absent member, not a JSON null.
func encodeParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encode params: %w", err)
	}

	return encoded, nil
}

// encodeResponse renders an answer to the request that carried id.
func encodeResponse(id json.RawMessage, result any, failure *Error) ([]byte, error) {
	answer := Response{JSONRPC: Version, ID: id, Error: failure}

	if failure == nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("encode result: %w", err)
		}

		answer.Result = encoded
	}

	object, err := json.Marshal(answer)
	if err != nil {
		return nil, fmt.Errorf("encode response: %w", err)
	}

	return object, nil
}
