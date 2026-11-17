package app

import (
	"net/http"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// unauthenticated resolves every request to one subject.
//
// httpapi has no default for this: an unauthenticated server has to be chosen
// explicitly, in one place. This is that place, chosen when core.auth.issuer
// is empty. The subject is a configuration key, so an operator sets it and
// does not have to find it in the source. It is not a bypass. No credential
// check is skipped, because on this wiring there is no credential to check.
//
// It never reads a client-set header, so a caller cannot choose who they are.
// Any replacement must keep that property.
type unauthenticated struct {
	subject string
}

var _ httpapi.Principals = unauthenticated{}

// Principal returns the configured subject.
func (u unauthenticated) Principal(*http.Request) (string, error) { return u.subject, nil }

// Caller returns the configured subject, with no scope: there is no
// credential to carry one.
func (u unauthenticated) Caller(r *http.Request) (httpapi.Caller, error) {
	return httpapi.Unscoped(u.Principal(r))
}
