package install

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// StagedRoute is one route of routes.staged as the declaration records it:
// the route, and the largest body the host takes on it in MiB. In a module's
// ask it is what the manifest asked for, and in the service block it is what
// the operator granted.
type StagedRoute struct {
	Method string `json:"method"  yaml:"method"`
	Path   string `json:"path"    yaml:"path"`
	MaxMiB int    `json:"max_mib" yaml:"max_mib"`
}

// Route is the route as sdk/manifest matches it.
func (r StagedRoute) Route() manifest.Route { return manifest.Route{Method: r.Method, Path: r.Path} }

// String is the route and its limit as one line, such as
// "PUT /files/{name} up to 1024 MiB".
func (r StagedRoute) String() string {
	return r.Route().String() + " up to " + strconv.Itoa(r.MaxMiB) + " MiB"
}

// StagedRoutesOf is the declaration's record of each staged route of a
// manifest.
func StagedRoutesOf(declared []manifest.StagedRoute) []StagedRoute {
	var out []StagedRoute
	for _, route := range declared {
		out = append(out, StagedRoute{Method: route.Method, Path: route.Path, MaxMiB: route.MaxMiB})
	}

	return out
}

// CheckStagedMiB refuses a staged route limit the host does not take. key
// names the value in the refusal.
func CheckStagedMiB(key string, mib int) error {
	if mib < 1 || mib > manifest.MaxStagedMiB {
		return fmt.Errorf("%s is %d, expected 1 to %d", key, mib, manifest.MaxStagedMiB)
	}

	return nil
}

// routeGrantsOf reads the three grants that name routes: service.calls, each a
// module namespace, service.public_routes, each a method and a path such as
// POST /hooks/announce/, and service.staged_routes, each a method, a path and
// a limit. It answers the public routes and the staged routes.
func routeGrantsOf(declared *ServiceEntry) ([]manifest.Route, []StagedRoute, error) {
	for _, namespace := range declared.Calls {
		if err := extension.Namespace(namespace).Validate(); err != nil {
			return nil, nil, fmt.Errorf("service.calls names %q, which is not a module namespace: %w", namespace, err)
		}
	}

	var public []manifest.Route

	for _, line := range declared.PublicRoutes {
		route, err := manifest.ParseRoute(line)
		if err != nil {
			return nil, nil, fmt.Errorf("service.public_routes names %q: %w", line, err)
		}

		public = append(public, route)
	}

	staged, err := stagedGrantsOf(declared.StagedRoutes)
	if err != nil {
		return nil, nil, err
	}

	// The manifest refuses the same pair (ADR-20261120-xys rule 4). A hand
	// edit of the service block is checked again here, because the host would
	// stop staging the route while every page went on showing its limit.
	for _, route := range staged {
		if slices.ContainsFunc(public, route.Route().Overlaps) {
			return nil, nil, fmt.Errorf("service.public_routes and service.staged_routes both reach %q, "+
				"and a staged route takes a caller", route.Route().String())
		}
	}

	return public, staged, nil
}

// stagedGrantsOf holds each staged route to the route grammar and the limit,
// and refuses two routes one request can reach.
func stagedGrantsOf(declared []StagedRoute) ([]StagedRoute, error) {
	var out []StagedRoute

	for index, held := range declared {
		key := fmt.Sprintf("service.staged_routes[%d]", index)

		route, err := manifest.ParseRoute(held.Route().String())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}

		if err = CheckStagedMiB(key+".max_mib", held.MaxMiB); err != nil {
			return nil, err
		}

		// The host holds a request to the first route it matches, so two that
		// one request can reach would show a limit the host does not keep.
		for _, seen := range out {
			if seen.Route().Overlaps(route) {
				return nil, fmt.Errorf("service.staged_routes names %q and %q, and one request can reach both",
					seen.Route().String(), route.String())
			}
		}

		out = append(out, StagedRoute{Method: route.Method, Path: route.Path, MaxMiB: held.MaxMiB})
	}

	return out, nil
}

// RequestHeader is one header of routes.headers as a module's ask records it:
// the name in its canonical form and the reason the operator reads before
// approving it (ADR-20261118-nnm).
type RequestHeader struct {
	Name   string `json:"name"   yaml:"name"`
	Reason string `json:"reason" yaml:"reason"`
	// Required says the module cannot work without the header. An approved
	// install that would not grant it is refused.
	Required bool `json:"required,omitempty" yaml:"required"`
}

// RequestHeadersOf is the ask's record of each header of a manifest.
func RequestHeadersOf(declared []manifest.RequestHeader) []RequestHeader {
	var out []RequestHeader
	for _, header := range declared {
		out = append(out, RequestHeader{Name: header.Name, Reason: header.Reason, Required: header.Required})
	}

	return out
}

// RequestHeaderNames is the name of each header.
func RequestHeaderNames(headers []RequestHeader) []string {
	var out []string
	for _, header := range headers {
		out = append(out, header.Name)
	}

	return out
}

// headerGrantsOf reads service.request_headers. It holds each name to the
// rules the manifest holds it to, so a hand edit cannot pass a header no
// module ever gets, such as Cookie, and it answers each in its canonical form.
func headerGrantsOf(declared []string) ([]string, error) {
	var out []string

	for _, name := range declared {
		if err := manifest.CheckRequestHeader(name); err != nil {
			return nil, fmt.Errorf("service.request_headers names %q: %w", name, err)
		}

		canonical := http.CanonicalHeaderKey(name)
		if slices.Contains(out, canonical) {
			return nil, fmt.Errorf("service.request_headers names %q twice", canonical)
		}

		out = append(out, canonical)
	}

	return out, nil
}
