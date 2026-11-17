package httpapi

import (
	"context"
	"net/http"
	"reflect"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
)

// readinessPath is where the readiness verdict is served. It is inside the
// sealed surface because an operator and a load balancer both read it, and
// everything this server answers is published in its OpenAPI document.
const readinessPath = "/api/v1/readiness"

// readinessSchemaName is the name the report carries in the document. It is
// registered before huma derives one from the operation id, so both the 200 and
// the 503 point at the same component.
const readinessSchemaName = "ReadinessReport"

// Readiness is the process's own verdict: whether every instance the operator
// declared required is serving. It is declared here because this is where it is
// consumed, and the composition root is the only thing that can see both the
// declared inventory and the supervision plane.
//
// Liveness is a different question and a different operation. A process that
// answers this 503 is running correctly and refusing traffic on purpose. One
// that fails liveness is stuck and needs a restart.
type Readiness interface {
	// Ready computes the verdict on every call. It is not cached: a readiness
	// answer read from a cache describes a moment that has passed, and the
	// caller acting on it is a load balancer.
	//
	// principal is empty for a caller this server could not identify. The
	// verdict and the three counts are the same either way, because they
	// describe the process and not anything the caller owns. The names are
	// not: an unidentified caller is told whether the process is ready and
	// never which instances exist.
	Ready(ctx context.Context, principal string) ReadinessReport
}

// The three answers ReadinessReport.App gives. They are separate because an
// operator who switched the app off and one who built a binary without it read
// the same 404 at the origin root and need different things done about it.
const (
	// AppServed is a build that carries the app with core.frontend.enabled on.
	AppServed = "served"
	// AppOff is a build that carries the app with core.frontend.enabled off.
	AppOff = "off"
	// AppAbsent is a build that carries no app, whatever the configuration
	// says. `make build-minimal` is the usual one.
	AppAbsent = "absent"
)

// ReadinessReport is one verdict and what it was computed from.
type ReadinessReport struct {
	App       string              `json:"app"                 doc:"Where the web app stands: served, off when core.frontend.enabled is false, or absent when this build carries none. It never makes the process unready: a deployment serving its own page wants this off."`
	Discovery string              `json:"discovery,omitempty" doc:"Where the local network announcement stands: announcing, hidden when a person hid this server, off when core.discovery.enabled is false, or failed when this machine would not open the mDNS socket or other machines hold every name it may take. Absent for a caller who does not hold view_resource on the link resource kind, the grant GET /api/v1/discovery/visibility takes. It never makes the process unready: an instance nobody can discover still serves every request."`
	Ready     bool                `json:"ready"               doc:"False while an instance the configuration declares required is not serving."`
	Serving   int                 `json:"serving"             doc:"How many declared instances calls are being routed to."`
	Declared  int                 `json:"declared"            doc:"How many instances the merged configuration declares."`
	Blocked   []string            `json:"blocked"             doc:"The required instances that are not serving, in the configuration's order. Empty when ready, and empty for a caller this server could not identify."`
	Instances []ReadinessInstance `json:"instances"           doc:"The declared instances this caller may see, and where each is. Empty for a caller this server could not identify."`
}

// ReadinessInstance is one declared instance as the verdict saw it.
type ReadinessInstance struct {
	Resource string `json:"resource" doc:"The instance, as kind:name."`
	State    string `json:"state"    doc:"The lifecycle state the supervision plane last observed."`
	Required bool   `json:"required" doc:"Whether this instance failing makes the process unready."`
	Serving  bool   `json:"serving"  doc:"Whether calls are being routed to it."`
}

// readinessOutput answers 200 or 503 with the same body. The status is what a
// load balancer reads and the body is what an operator reads, so a probe that
// takes the instance out of rotation and the line that says why come from one
// answer. Two endpoints could disagree.
type readinessOutput struct {
	Status int
	Body   ReadinessReport
}

func registerReadiness(api huma.API, probes Readiness, principals Principals, grants GrantStore) {
	register(api, huma.Operation{
		OperationID: "get-readiness",
		Method:      http.MethodGet,
		Path:        readinessPath,
		Summary:     "Whether this process is ready to serve",
		Description: "Answers 200 while every instance the configuration declares required is serving, " +
			"and 503 while one of them is not. An instance that is not required never makes the process " +
			"unready, so a failing optional resource cannot take the printer path out of service. " +
			"It needs no identity, because the probe that reads it cannot hold a " +
			"credential, and it names no instance to a caller it could not identify: the verdict and " +
			"the counts describe the process, while blocked and instances name resources and are " +
			"narrowed to what the caller may already see through /api/v1/resources.",
		Tags:      []string{"system"},
		Security:  noSecurity,
		Responses: unavailableResponse(api),
	}, func(ctx context.Context, _ *struct{}) (*readinessOutput, error) {
		principal, scopes := optionalPrincipal(ctx, principals)
		ctx = authz.WithScopes(ctx, scopes)
		report := probes.Ready(ctx, principal)

		// The announcer's state says whether this server hides itself, which
		// is what GET /api/v1/discovery/visibility answers. So it is told only
		// to a caller that route would answer.
		if !seesDiscovery(ctx, grants, principal) {
			report.Discovery = ""
		}

		out := &readinessOutput{Status: http.StatusOK, Body: report}
		if !report.Ready {
			out.Status = http.StatusServiceUnavailable
		}

		return out, nil
	})
}

// optionalPrincipal resolves the caller without requiring one. The operation
// declares the empty security requirement so a probe with no credential is
// served, which means principalMiddleware short-circuits and the handler has to
// ask for itself.
//
// A credential that does not resolve is the same as none. This endpoint is the
// one place in the sealed surface where that is right: refusing a probe because
// its token expired takes a healthy process out of rotation, and the answer an
// unidentified caller gets names nothing anyway.
func optionalPrincipal(ctx context.Context, principals Principals) (string, []authz.Scope) {
	request := requestFrom(ctx)
	if request == nil || principals == nil {
		return "", nil
	}

	caller, err := principals.Caller(request)
	if err != nil {
		return "", nil
	}

	return caller.Principal, caller.Scopes
}

// unavailableResponse documents the 503 this operation answers with. Huma
// derives a response from the output struct's Body and from Errors, and this
// operation's second status is neither: it carries the same report as the 200,
// so a client reads one schema whichever answer it gets.
func unavailableResponse(api huma.API) map[string]*huma.Response {
	schema := api.OpenAPI().Components.Schemas.Schema(
		reflect.TypeFor[ReadinessReport](), true, readinessSchemaName)

	return map[string]*huma.Response{
		strconv.Itoa(http.StatusServiceUnavailable): {
			Description: "A required instance is not serving. The body names which.",
			Content: map[string]*huma.MediaType{
				"application/json": {Schema: schema},
			},
		},
	}
}
