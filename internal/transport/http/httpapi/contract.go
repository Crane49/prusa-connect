package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
)

// infrastructureRoutes are the routes that carry the contract and do not
// implement it: the document itself and the reference that renders it. Every
// exception to the gate is in this one list, so adding one shows as a line in
// a diff.
var infrastructureRoutes = []string{
	http.MethodGet + " " + specJSONPath,
	http.MethodGet + " " + specYAMLPath,
	http.MethodGet + " " + docsPath,
	http.MethodGet + " " + docsScriptPath,
}

// groupPatterns are the patterns excluded for every method they were
// registered for.
//
// There is one: the group every module's own surface is served under. A module
// chooses its own verbs, so the pattern is registered for all of them, and none
// of them is this server's contract. Publishing them would put an installed
// module's routes into the document every client generates from, and
// uninstalling that module would then be a breaking change to /api/v1. What a
// caller must hold to reach one is decided by moduleHandler and is the same
// permission the resource surface uses.
//
//nolint:gochecknoglobals // a closed list read at validation time, never written.
var groupPatterns = []string{modulesRoutePattern}

// verifyDocumented reports any disagreement between the routes the mux will
// serve and the operations the OpenAPI document publishes.
//
// It catches what registering through Huma cannot: a route added straight to
// the mux, and an operation registered with Hidden set, which reaches the
// router and leaves the document. New calls it, so both are a startup failure
// and not only a test failure.
//
// mounted is the patterns Config.Mounts declared. They are excluded from the
// comparison, because a device wire and a frontend bundle are not operations,
// and publishing them would describe a websocket handshake as REST.
// validateMounts has already refused any mount that carries no reason or sits
// inside this server's own paths.
func verifyDocumented(mux chi.Routes, api huma.API, mounted []string) error {
	served, err := servedRoutes(mux, mounted)
	if err != nil {
		return err
	}

	published := publishedOperations(api)

	undocumented := missing(served, published)
	unimplemented := missing(published, served)

	if len(undocumented) == 0 && len(unimplemented) == 0 {
		return nil
	}

	var b strings.Builder

	b.WriteString(ErrUndocumentedRoute.Error())

	if len(undocumented) > 0 {
		b.WriteString("\n  served but not in the OpenAPI document:\n    ")
		b.WriteString(strings.Join(undocumented, "\n    "))
		b.WriteString("\n  register it with huma.Register, drop Hidden if it is set, or add it to " +
			"infrastructureRoutes if it carries the contract rather than implementing it")
	}

	if len(unimplemented) > 0 {
		b.WriteString("\n  in the OpenAPI document but not served:\n    ")
		b.WriteString(strings.Join(unimplemented, "\n    "))
		b.WriteString("\n  a client would call it and get a 404")
	}

	return fmt.Errorf("%w:%s", ErrUndocumentedRoute, strings.TrimPrefix(b.String(), ErrUndocumentedRoute.Error()))
}

// servedRoutes is every method and pattern the mux will match, minus the
// infrastructure allowlist. The gate needs a router that can list its routes.
// chi can, and http.ServeMux has no route-enumeration API.
func servedRoutes(mux chi.Routes, mounted []string) ([]string, error) {
	var served []string

	walk := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		if !slices.Contains(infrastructureRoutes, key) &&
			!slices.Contains(leftOutRoutes(), key) &&
			!slices.Contains(groupPatterns, route) &&
			!slices.Contains(mounted, route) {
			served = append(served, key)
		}

		return nil
	}

	if err := chi.Walk(mux, walk); err != nil {
		return nil, fmt.Errorf("walk the router: %w", err)
	}

	sort.Strings(served)

	return served, nil
}

// publishedOperations is every method and path the document declares.
func publishedOperations(api huma.API) []string {
	var published []string

	for path, item := range api.OpenAPI().Paths {
		for method, op := range operationsOfPath(item) {
			if op != nil {
				published = append(published, method+" "+path)
			}
		}
	}

	sort.Strings(published)

	return published
}

func operationsOfPath(item *huma.PathItem) map[string]*huma.Operation {
	return map[string]*huma.Operation{
		http.MethodGet:     item.Get,
		http.MethodPost:    item.Post,
		http.MethodPut:     item.Put,
		http.MethodPatch:   item.Patch,
		http.MethodDelete:  item.Delete,
		http.MethodHead:    item.Head,
		http.MethodOptions: item.Options,
		http.MethodTrace:   item.Trace,
	}
}

func missing(from, in []string) []string {
	var out []string

	for _, key := range from {
		if !slices.Contains(in, key) {
			out = append(out, key)
		}
	}

	return out
}
