package main

import (
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// How a refusal reaches a client.
//
// The core refuses with a fault: a registered code, a type URI pointing at the
// section of the error reference that explains it, and the values the code
// fills in. The host turns that fault into one of the sentinels sdk/deviceapi
// publishes before it crosses the wire, because a module branches on a
// sentinel rather than on a sentence.
//
// This file turns the sentinel back into the code the core raised and sends it
// out as a GraphQL error carrying the code, the type URI and the parameters in
// extensions. A client written against the REST surface branches on the same
// identifier here, and a message bundle indexed by code needs no second set of
// entries.
//
// The mapping is the inverse of the host's. Where the host maps several codes
// onto one sentinel, this file picks the one a reader of the GraphQL surface
// will meet.

// refusal is one wire refusal on its way out as a GraphQL error. The library
// reads Extensions through its ExtendedError interface and writes what it
// answers under the error's extensions member.
type refusal struct {
	code      fault.Code
	detail    string
	params    map[string]any
	retryable bool
}

// Error is the sentence the client reads on the error's message.
func (r refusal) Error() string { return r.detail }

// Extensions is what the client reads beside the message: the core's own
// identifier for this failure, where it is documented, and the values it
// names.
func (r refusal) Extensions() map[string]any {
	params := r.params
	if params == nil {
		params = map[string]any{}
	}

	return map[string]any{
		"code":      r.code.String(),
		"type":      r.code.TypeURI(),
		"params":    params,
		"retryable": r.retryable,
	}
}

// codeUnexpected is what a refusal with no mapping carries. The core already
// has the detail in its own log, and a module inventing a code for it would
// put a string into a client's branch that the error reference does not
// document.
//
//nolint:gochecknoglobals // a code read on a refusal path; a const cannot hold one.
var codeUnexpected = mustCode(fault.DomainInternal(), "UNEXPECTED")

// The codes core.auth/ACCESS_DENIED and core.validation/RESOURCE_NOT_FOUND,
// which sdk/fault does not export as definitions. They are built by name
// rather than invented: both are in the published error reference, so a client
// gets the same identifier and the same documentation link a REST caller
// gets.
//
//nolint:gochecknoglobals // codes read on a refusal path.
var (
	codeAccessDenied     = mustCode(fault.DomainAuth(), "ACCESS_DENIED")
	codeResourceNotFound = mustCode(fault.DomainValidation(), "RESOURCE_NOT_FOUND")
)

// mustCode builds one core code by name. A misspelling is a programming
// mistake in this file, so it stops the process at start-up rather than
// answering a client with an empty identifier.
func mustCode(domain fault.Domain, reason string) fault.Code {
	code, err := fault.NewCode(domain, reason)
	if err != nil {
		panic(fmt.Sprintf("graphql: %s/%s is not a code: %v", domain, reason, err))
	}

	return code
}

// refuse renders one wire refusal as a GraphQL error.
//
// params is what the resolver knows about the call: the device it names, the
// verb it sent. The core's own params do not cross the wire, so these are the
// module's, and they are the values a client would put into a message.
func refuse(err error, params map[string]any) error {
	if err == nil {
		return nil
	}

	code, retryable := codeFor(err)

	return refusal{code: code, detail: err.Error(), params: params, retryable: retryable}
}

// codeFor is the core code behind one wire sentinel, and whether sending the
// same request again could work.
func codeFor(err error) (fault.Code, bool) {
	switch {
	case errors.Is(err, deviceapi.ErrDenied):
		return codeAccessDenied, false
	case errors.Is(err, deviceapi.ErrNoDevice):
		return fault.PrinterNotFound.Code(), false
	case errors.Is(err, deviceapi.ErrNoRegistration):
		return codeResourceNotFound, false
	case errors.Is(err, deviceapi.ErrVerbUnknown):
		return fault.PrinterVerbUnknown.Code(), false
	case errors.Is(err, deviceapi.ErrNotReady):
		// The printer's session has ended, or the module is not serving yet.
		// Both pass: the machine reconnects and the host marks the module
		// ready, and neither needs the caller to change the request.
		return fault.PrinterSessionEnded.Code(), true
	case errors.Is(err, deviceapi.ErrBusy):
		return connectorapi.CommandQueueFull.Code(), true
	case errors.Is(err, deviceapi.ErrInvalidQuery):
		return connectorapi.PathNotAllowed.Code(), false
	case errors.Is(err, deviceapi.ErrArgument):
		return connectorapi.ArgumentNotSupported.Code(), false
	}

	return codeUnexpected, false
}
