package manifest

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

// MaxRequestHeaders is how many headers routes.headers may name.
const MaxRequestHeaders = 8

// MaxHeaderReason is the longest reason a header request may give, in
// characters.
const MaxHeaderReason = 200

// MaxHeaderName is the longest header name a module may ask for, the most the
// API that offers the request carries.
const MaxHeaderName = 64

// RequestHeader is one request header a module asks the host to pass to its
// routes, beside the headers every module gets (ADR-20261118-nnm).
type RequestHeader struct {
	// Name is the header's name in its canonical form, such as
	// Print-After-Upload.
	Name string
	// Reason is the sentence the operator reads before approving the header.
	Reason string
	// Required says the module cannot work without the header. An install that
	// would not grant it is refused, and the host does not start an instance
	// whose grant lacks it.
	Required bool
}

// DefaultRequestHeaders are the request headers the host passes to every
// module, in lower case. A module never asks for one of them.
//
//nolint:gochecknoglobals // a closed table read on every request, never written.
var DefaultRequestHeaders = []string{
	"accept", "accept-language",
	"content-language", "content-type",
	"if-match", "if-modified-since", "if-none-match", "if-range", "if-unmodified-since",
	"range", "user-agent",
}

// deniedHeaders are the request headers no module ever gets, in lower case.
// They carry a credential, describe one connection, or name the client in a
// way the client controls.
//
//nolint:gochecknoglobals // a closed table read at parse and on every grant, never written.
var deniedHeaders = []string{
	// A credential. With it a module could call the core as the person.
	"authorization", "proxy-authorization", "cookie", "x-api-key", "dpop",
	// The one-time code that runs the first setup of this server.
	"x-setup-code",
	// A printer's credential on its connection to this server, and the code it
	// registers with.
	"token", "fingerprint", "code",
	// The connection and the routing.
	"host", "connection", "keep-alive", "proxy-connection", "te", "trailer", "transfer-encoding", "upgrade",
	// Where the request came from, which a client can write itself.
	"forwarded", "via", "x-real-ip", "x-client-ip", "x-cluster-client-ip", "true-client-ip", "cf-connecting-ip",
}

// deniedHeaderPrefix is the start of every header name a proxy adds about the
// client, which a client can also write itself.
const deniedHeaderPrefix = "x-forwarded-"

// grantableHeaders are the only request headers a module may ask for, in lower
// case. A header joins the list by a change to the core that judged it: it
// carries no credential, no proxy or browser writes it on its own, and it does
// not change what the host authorized. The list only grows, so a manifest that
// asks for a header on it keeps its meaning on every later core.
//
//nolint:gochecknoglobals // a closed table read at parse and on every grant, never written.
var grantableHeaders = []string{
	// PrusaSlicer 2 says with it whether a printer with PrusaLink starts the
	// file it uploads.
	"print-after-upload",
}

// CheckRequestHeader refuses a header name a module may not ask for: a name
// that is not an HTTP token, one of the denied headers, one the host passes to
// every module already, or one that is not on the list of headers a module may
// ask for. The host calls it again on every grant it reads, so a hand-edited
// configuration cannot pass any other header either.
func CheckRequestHeader(name string) error {
	if !isToken(name) {
		return fmt.Errorf("%w: the header name %q is not an HTTP token", ErrInvalid, name)
	}

	lower := strings.ToLower(name)

	if slices.Contains(deniedHeaders, lower) || strings.HasPrefix(lower, deniedHeaderPrefix) {
		return fmt.Errorf("%w: the header %s is never passed to a module, because it carries a credential, "+
			"describes the connection or names the client", ErrInvalid, name)
	}

	if slices.Contains(DefaultRequestHeaders, lower) {
		return fmt.Errorf("%w: the header %s is passed to every module already, so a module does not ask for it",
			ErrInvalid, name)
	}

	if !slices.Contains(grantableHeaders, lower) {
		return fmt.Errorf("%w: the header %s is not one a module may ask for. A header joins that list by a "+
			"change to the core (ADR-20261118-nnm rule 3)", ErrInvalid, name)
	}

	return nil
}

// isToken reports whether name is a token as RFC 9110 section 5.6.2 defines
// it, which is what a header name is.
func isToken(name string) bool {
	if name == "" {
		return false
	}

	for _, c := range name {
		if !tokenRune(c) {
			return false
		}
	}

	return true
}

func tokenRune(c rune) bool {
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
		return true
	}

	return strings.ContainsRune("!#$%&'*+-.^_`|~", c)
}

// headerKeys is every key a header request may carry.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var headerKeys = []string{"name", "reason", "required"}

func readHeaders(block map[string]any) ([]RequestHeader, error) {
	entries, err := nodeList(block, "routes", "headers")
	if err != nil {
		return nil, err
	}

	if len(entries) > MaxRequestHeaders {
		return nil, fmt.Errorf("%w: routes.headers names %d headers, and the limit is %d",
			ErrInvalid, len(entries), MaxRequestHeaders)
	}

	var out []RequestHeader

	for index, entry := range entries {
		where := fmt.Sprintf("routes.headers[%d]", index)

		header, readErr := readHeader(where, entry)
		if readErr != nil {
			return nil, readErr
		}

		for _, held := range out {
			if held.Name == header.Name {
				return nil, fmt.Errorf("%w: routes.headers names %s twice", ErrInvalid, header.Name)
			}
		}

		out = append(out, header)
	}

	return out, nil
}

func readHeader(where string, entry map[string]any) (RequestHeader, error) {
	if err := refuseUnknown(entry, where, headerKeys); err != nil {
		return RequestHeader{}, err
	}

	name, err := requiredText(entry, where, "name")
	if err != nil {
		return RequestHeader{}, err
	}

	if n := len(name); n > MaxHeaderName {
		return RequestHeader{}, fmt.Errorf("%w: %s.name is %d characters, and the limit is %d",
			ErrInvalid, where, n, MaxHeaderName)
	}

	if err = CheckRequestHeader(name); err != nil {
		return RequestHeader{}, fmt.Errorf("%w (%s.name)", err, where)
	}

	reason, err := requiredText(entry, where, "reason")
	if err != nil {
		return RequestHeader{}, err
	}

	// The operator reads the reason before approving the header.
	if err = plainText(where, "reason", reason); err != nil {
		return RequestHeader{}, err
	}

	if n := utf8.RuneCountInString(reason); n > MaxHeaderReason {
		return RequestHeader{}, fmt.Errorf("%w: %s.reason is %d characters, and the limit is %d",
			ErrInvalid, where, n, MaxHeaderReason)
	}

	required, err := truth(entry, where, "required")
	if err != nil {
		return RequestHeader{}, err
	}

	return RequestHeader{Name: http.CanonicalHeaderKey(name), Reason: reason, Required: required}, nil
}
