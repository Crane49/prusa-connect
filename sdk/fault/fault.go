// Package fault is the error taxonomy every surface shares. A Fault has a
// registered code, the params a client interpolates, an English message, a
// retry hint and a trace id. It renders as RFC 9457 problem+json for HTTP, as
// an event envelope's data.error and as the error of a printer command result.
//
// The server never translates. The English message is always sent, except
// for a code registered as redacted, which sends its title only.
//
// Codes are namespaced domain/REASON. NewDomain refuses the core namespace,
// so an extension's codes cannot collide with the core's.
package fault

import (
	"errors"
	"slices"
	"strings"
)

var (
	// unregisteredCode is the code of a fault whose own code is not in the
	// registry. It is redacted.
	unregisteredCode = Register(DomainInternal(), "UNREGISTERED_CODE", statusInternal, Document(
		"The server returned an error it does not document",
		NoAction,
		RetryNever,
		RedactDetail,
	))

	// errNoDefinition stands in for a Fault that was never built, including a
	// nil receiver, so an error path cannot panic on the way to a client.
	errNoDefinition = unregisteredFault(Code{}, "")
)

// Fault is a failure on its way to a client. Build it from a Definition, add
// params and a cause, then pass it to the surface that answers.
//
//nolint:errname // a client-visible error is a fault here, per go-standards.
type Fault struct {
	def        *Definition
	message    string
	params     Params
	undeclared []string
	retryable  bool
	traceID    string
	cause      error
}

// New returns a Fault for a code resolved through the global registry. An
// unregistered code gives a redacted core.internal/UNREGISTERED_CODE fault,
// and the code that was asked for appears in the log line only.
func New(code Code, message string) *Fault {
	definition, ok := globalRegistry.definitions()[code]
	if !ok {
		return unregisteredFault(code, message)
	}

	return definition.New(message)
}

// HasCode reports whether err, or any error it wraps, is a Fault with code c.
// Use it to branch on a failure. A zero code matches nothing.
func HasCode(err error, c Code) bool {
	var fault *Fault
	if !errors.As(err, &fault) || fault == nil {
		return false
	}

	return !c.IsZero() && fault.Code() == c
}

// Error is the operator-facing line, for logs. It includes the wrapped cause
// and any param the code does not declare. Neither is ever sent to a client.
func (f *Fault) Error() string {
	fault := f.resolved()

	var b strings.Builder

	b.WriteString(fault.def.entry.Code.String())

	if fault.message != "" {
		b.WriteString(": ")
		b.WriteString(fault.message)
	}

	if fault.cause != nil {
		b.WriteString(": ")
		b.WriteString(fault.cause.Error())
	}

	if len(fault.undeclared) > 0 {
		b.WriteString(" [undeclared params: ")
		b.WriteString(strings.Join(fault.undeclared, ", "))
		b.WriteString("]")
	}

	return b.String()
}

// Unwrap returns the wrapped cause, so errors.Is and errors.As reach it.
func (f *Fault) Unwrap() error {
	if f == nil {
		return nil
	}

	return f.cause
}

// Is matches any Fault with the same code, so errors.Is(err, def.New(""))
// works through wrapping.
func (f *Fault) Is(target error) bool {
	other, ok := target.(*Fault)
	if !ok {
		return false
	}

	return f.resolved().def.entry.Code == other.resolved().def.entry.Code
}

// Code returns the registered identifier.
func (f *Fault) Code() Code { return f.resolved().def.entry.Code }

// Title returns the English summary of the code, constant across occurrences.
func (f *Fault) Title() string { return f.resolved().def.entry.Summary }

// Status returns the advisory HTTP status from the registry.
func (f *Fault) Status() int { return f.resolved().def.entry.Status }

// Redacted reports whether the message and the params stay out of every
// rendering.
func (f *Fault) Redacted() bool { return f.resolved().def.entry.Redaction == RedactDetail }

// Message returns the English text for this occurrence, whether or not the code
// redacts it.
func (f *Fault) Message() string { return f.resolved().message }

// Retryable reports whether the caller may repeat the operation.
func (f *Fault) Retryable() bool { return f.resolved().retryable }

// TraceID returns the W3C trace id that links this fault to the log line with
// its cause.
func (f *Fault) TraceID() string { return f.resolved().traceID }

// Params returns a copy of the interpolation values.
func (f *Fault) Params() Params { return slices.Clone(f.resolved().params) }

// With returns a copy with the given params. A repeated key replaces the
// earlier value. A key the code does not declare is dropped and named in
// Error().
func (f *Fault) With(params ...Param) *Fault {
	out := f.resolved().clone()

	for _, param := range params {
		if !out.def.declares(param.Key) {
			out.undeclared = append(out.undeclared, param.Key)

			continue
		}

		if i := slices.IndexFunc(out.params, func(p Param) bool { return p.Key == param.Key }); i >= 0 {
			out.params[i] = param

			continue
		}

		out.params = append(out.params, param)
	}

	return out
}

// Wrap returns a copy with err as its cause. The cause appears in the log
// line, never in a rendering.
func (f *Fault) Wrap(err error) *Fault {
	out := f.resolved().clone()
	out.cause = err

	return out
}

// WithTrace returns a copy with the W3C trace id, the 32 hex characters of
// the traceparent trace-id field.
func (f *Fault) WithTrace(traceID string) *Fault {
	out := f.resolved().clone()
	out.traceID = traceID

	return out
}

// WithRetry returns a copy that overrides the registered retry hint.
func (f *Fault) WithRetry(retry Retry) *Fault {
	out := f.resolved().clone()
	out.retryable = retry == RetryAllowed

	return out
}

func (f *Fault) resolved() *Fault {
	if f == nil || f.def == nil {
		return errNoDefinition
	}

	return f
}

func (f *Fault) clone() *Fault {
	out := *f
	out.params = slices.Clone(f.params)
	out.undeclared = slices.Clone(f.undeclared)

	return &out
}

func unregisteredFault(code Code, message string) *Fault {
	requested := code.String()
	if requested == "" {
		requested = "(none)"
	}

	detail := "unregistered code " + requested
	if message != "" {
		detail += ": " + message
	}

	return &Fault{def: unregisteredCode, message: detail}
}
