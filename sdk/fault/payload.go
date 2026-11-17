package fault

import "slices"

// Payload is the canonical rendering of a Fault, and the form a printer command
// result stores under its error member. Every other surface wraps this object
// unchanged.
type Payload struct {
	Code      Code   `json:"code"`
	Title     string `json:"title"              doc:"The registered summary of the code, the same for every occurrence."`
	Detail    string `json:"detail,omitempty"   doc:"The sentence for this occurrence. Absent when there is none, or when the code is registered as redacted."`
	Params    Params `json:"params"`
	Retryable bool   `json:"retryable"          doc:"Whether the same request may succeed if sent again."`
	TraceID   string `json:"trace_id,omitempty" doc:"The W3C trace id of the request, which an operator finds in the log."`
}

// Problem is the RFC 9457 application/problem+json framing: the payload plus
// the three members the RFC defines in HTTP terms. code, params, retryable and
// trace_id are extension members.
type Problem struct {
	Payload

	Type     string `json:"type"               doc:"The RFC 9457 type URI: the error reference page and this code's own section of it, so opening it lands on what the code means and what to do about it. A page can offer it as a help link. Nothing has to dereference it, and an air-gapped install loses nothing by not doing so."`
	Status   int    `json:"status"             doc:"The HTTP status the response was sent with."`
	Instance string `json:"instance,omitempty" doc:"The request path this occurrence was answered on."`
}

// EventError is the payload under an event envelope's data.error. The trace id
// is cleared, because the envelope has it as an attribute.
type EventError struct {
	Payload
}

// Payload returns the canonical object. A code registered as redacted renders
// its title, status, code and trace id, without the occurrence message and
// the params.
func (f *Fault) Payload() Payload {
	fault := f.resolved()
	entry := fault.def.entry

	payload := Payload{
		Code:      entry.Code,
		Title:     entry.Summary,
		Detail:    fault.message,
		Params:    slices.Clone(fault.params),
		Retryable: fault.retryable,
		TraceID:   fault.traceID,
	}

	if entry.Redaction == RedactDetail {
		payload.Detail = ""
		payload.Params = nil
	}

	return payload
}

// ProblemJSON frames the payload for an HTTP response. instance identifies this
// occurrence, conventionally the request path plus the request id.
func (f *Fault) ProblemJSON(instance string) Problem {
	fault := f.resolved()

	return Problem{
		Payload:  fault.Payload(),
		Type:     fault.def.entry.Code.TypeURI(),
		Status:   fault.def.entry.Status,
		Instance: instance,
	}
}

// EventError frames the payload for an event envelope.
func (f *Fault) EventError() EventError {
	payload := f.Payload()
	payload.TraceID = ""

	return EventError{Payload: payload}
}
