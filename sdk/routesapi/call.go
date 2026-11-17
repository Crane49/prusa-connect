package routesapi

// One module calling the routes of another, through the host. The host checks
// that the caller declared the call and decides who the call runs as.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MethodCall is the call a module makes to reach another module's routes. It
// takes one request and answers the other module's response.
const MethodCall wirerpc.Method = "routes.call"

// MaxCallNamespace bounds the namespace a call names.
const MaxCallNamespace = 253

// CallParams is the params of MethodCall.
type CallParams struct {
	// Module is the namespace of the module to call, the segment its routes
	// are mounted under: /api/v1/modules/<namespace>.
	Module string `json:"module"`
	// Principal is who the call runs as. Empty is the calling module itself.
	// A person may be named only while the host is serving that person's
	// request to the calling module.
	Principal string `json:"principal,omitempty"`
	// Method is the HTTP method.
	Method string `json:"method"`
	// Path is the route relative to the other module's mount, as an escaped
	// URL path starting with "/", such as /jobs. It must stay inside the
	// mount: no "." or ".." segment, no empty segment, no backslash, no
	// escaped "/" or "\" and nothing escaped twice. The host refuses any
	// other path and never rewrites it.
	Path    string              `json:"path"`
	Query   string              `json:"query,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	// Body is at most MaxModuleBody bytes, as a request a person makes.
	Body []byte `json:"body,omitempty"`
}

// callMethods are the methods a call may use.
//
//nolint:gochecknoglobals // a closed table read on every call, never written.
var callMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
}

// Validate checks a call against the bounds of this contract. The host checks
// them again.
func (p CallParams) Validate() error {
	switch {
	case p.Module == "" || len(p.Module) > MaxCallNamespace:
		return fmt.Errorf("%w: the call names no module", ErrCallInvalid)
	case !slices.Contains(callMethods, p.Method):
		return fmt.Errorf("%w: the method %q is not one a call may use", ErrCallInvalid, p.Method)
	case len(p.Body) > MaxModuleBody:
		return fmt.Errorf("%w: the body is %d bytes and the limit is %d", ErrCallInvalid, len(p.Body), MaxModuleBody)
	case strings.ContainsAny(p.Query, "#"):
		return fmt.Errorf("%w: the query %q carries a fragment", ErrCallInvalid, p.Query)
	}

	return checkCallPath(p.Path)
}

// checkCallPath refuses a path that would not stay inside the other module's
// mount once net/http unescapes it:
//
//   - it starts with one "/", never "//", which a URL reads as a host
//   - it has no "?", "#", backslash or control character
//   - it has no escaped "/" or "\" (%2f, %5c), and nothing still escaped
//     after one unescape (%252e)
//   - unescaped, it has no "." or ".." segment and no empty segment. One
//     "/" at the end is kept
//
// A refused path is never rewritten. The host runs the same check.
func checkCallPath(raw string) error {
	refuse := func(why string) error {
		return fmt.Errorf("%w: the path %q %s", ErrCallInvalid, raw, why)
	}

	switch {
	case !strings.HasPrefix(raw, "/"):
		return refuse("does not start with /")
	case strings.HasPrefix(raw, "//"):
		return refuse("starts with //, which names a host")
	case strings.ContainsAny(raw, "?#\\"):
		return refuse("carries a query, a fragment or a backslash")
	case strings.Contains(strings.ToLower(raw), "%2f") || strings.Contains(strings.ToLower(raw), "%5c"):
		return refuse("carries an escaped separator")
	}

	unescaped, err := url.PathUnescape(raw)
	if err != nil {
		return refuse("is not a valid escaped path")
	}

	if strings.ContainsAny(unescaped, "%\\") || strings.ContainsFunc(unescaped, isControl) {
		return refuse("is escaped twice or carries a control character")
	}

	cleaned := path.Clean(unescaped)
	if strings.HasSuffix(unescaped, "/") && cleaned != "/" {
		cleaned += "/"
	}

	if cleaned != unescaped {
		return refuse("leaves the module's mount or is not a clean path")
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil ||
		parsed.Fragment != "" || parsed.RawQuery != "" || parsed.Path != unescaped {
		return refuse("does not read back as the same path")
	}

	return nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// Request is the call as the other module receives it on routes.serve, for
// the principal the host decided. Its headers pass through RequestHeaders, so
// a credential of one module never reaches another.
func (p CallParams) Request(principal string) Request {
	return Request{
		Method: p.Method, Path: p.Path, Query: p.Query, Headers: RequestHeaders(p.Headers), Body: p.Body,
		Principal: principal,
	}
}

// The codes of this contract. They are outside the range JSON-RPC 2.0
// reserves and outside the ranges of the other contracts.
const (
	// CodeCallNotDeclared is ErrCallNotDeclared.
	CodeCallNotDeclared = 7000
	// CodeCallUnavailable is ErrCallUnavailable.
	CodeCallUnavailable = 7001
	// CodeCallDenied is ErrCallDenied.
	CodeCallDenied = 7002
	// CodeCallInvalid is ErrCallInvalid.
	CodeCallInvalid = 7003
)

// The refusals of MethodCall. A module branches on them with errors.Is.
var (
	// ErrCallNotDeclared is a call to a module the calling module did not
	// name under calls, or that the operator did not approve.
	ErrCallNotDeclared = errors.New("routesapi: the module did not declare a call to that module")
	// ErrCallUnavailable is a call to a module that is not installed, not
	// running, or serves no routes. The caller cannot tell which.
	ErrCallUnavailable = errors.New("routesapi: the module called is not available")
	// ErrCallDenied is a call in a person's name that the person may not
	// make, or that names a person whose request the host is not serving.
	ErrCallDenied = errors.New("routesapi: the call is not allowed")
	// ErrCallInvalid is a call outside the bounds this contract publishes.
	ErrCallInvalid = errors.New("routesapi: the call is outside the published bounds")
)

// Modules is the host's side of MethodCall, bound to the calling module. The
// host supplies it and a module never constructs one.
type Modules interface {
	Call(ctx context.Context, params CallParams) (Response, error)
}

// CallHandlers is the host's method set for this contract. The set is empty
// for a nil modules.
func CallHandlers(modules Modules) wirerpc.Methods {
	if modules == nil {
		return wirerpc.Methods{}
	}

	return wirerpc.Methods{MethodCall: callHandler(modules)}
}

func callHandler(modules Modules) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var params CallParams

		if len(call.Params) == 0 || json.Unmarshal(call.Params, &params) != nil {
			return nil, &wirerpc.Error{
				Code:    wirerpc.CodeInvalidParams,
				Message: string(MethodCall) + " carries a params object",
			}
		}

		if err := params.Validate(); err != nil {
			return nil, callRefusal(err)
		}

		answered, err := modules.Call(ctx, params)
		if err != nil {
			return nil, callRefusal(err)
		}

		return answered, nil
	}
}

// CallHostFailed is the message a module gets when a call failed inside the
// host, such as the other module crashing mid-call.
const CallHostFailed = "routesapi: the host could not relay the call"

func callRefusal(err error) error {
	for _, known := range []struct {
		code     int
		sentinel error
	}{
		{CodeCallNotDeclared, ErrCallNotDeclared},
		{CodeCallUnavailable, ErrCallUnavailable},
		{CodeCallDenied, ErrCallDenied},
		{CodeCallInvalid, ErrCallInvalid},
	} {
		if errors.Is(err, known.sentinel) {
			return &wirerpc.Error{Code: known.code, Message: err.Error()}
		}
	}

	return &wirerpc.Error{Code: wirerpc.CodeInternal, Message: CallHostFailed}
}

// Caller is the half of a wirerpc session a module calls through. *wirerpc.Conn
// is one.
type Caller interface {
	Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error)
}

// Client is a module's way of calling another module's routes.
type Client struct {
	conn Caller
}

// NewClient returns the client a module calls through.
func NewClient(conn Caller) *Client { return &Client{conn: conn} }

// Call makes one call and answers the other module's response, whatever its
// status. A refusal of the host is an error that wraps one of the ErrCall
// sentinels.
func (c *Client) Call(ctx context.Context, params CallParams) (Response, error) {
	if err := params.Validate(); err != nil {
		return Response{}, fmt.Errorf("%s: %w", MethodCall, err)
	}

	if c.conn == nil {
		return Response{}, fmt.Errorf("routesapi: %s: the client holds no session", MethodCall)
	}

	body, err := c.conn.Call(ctx, MethodCall, params)
	if err != nil {
		if coded, ok := errors.AsType[*wirerpc.Error](err); ok {
			if sentinel := callSentinel(coded.Code); sentinel != nil {
				return Response{}, fmt.Errorf("%s: %w: %s", MethodCall, sentinel, coded.Message)
			}
		}

		return Response{}, fmt.Errorf("routesapi: %s: %w", MethodCall, err)
	}

	var answered Response
	if err = json.Unmarshal(body, &answered); err != nil {
		return Response{}, fmt.Errorf("routesapi: %s: read the answer: %w", MethodCall, err)
	}

	if answered.Status == 0 {
		answered.Status = http.StatusOK
	}

	return answered, nil
}

func callSentinel(code int) error {
	switch code {
	case CodeCallNotDeclared:
		return ErrCallNotDeclared
	case CodeCallUnavailable:
		return ErrCallUnavailable
	case CodeCallDenied:
		return ErrCallDenied
	case CodeCallInvalid:
		return ErrCallInvalid
	}

	return nil
}
