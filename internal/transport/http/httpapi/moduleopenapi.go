package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// moduleOpenAPIPath is where a module's OpenAPI fragment is served, rebased
// under its mount. It is the one address under the module group that this
// server answers and the module does not, so a client finds every module's
// document at the same relative name.
//
// It is a literal segment under the namespace, and chi prefers it to the
// module's own catch-all, so a module cannot serve a different document at the
// address a generator reads.
const moduleOpenAPIPath = modulesPathPrefix + "/{" + namespaceParam + "}/openapi.json"

// openAPIDocumentName is the last segment of moduleOpenAPIPath, for the
// listing to render the address from a namespace.
const openAPIDocumentName = "/openapi.json"

// ModuleMount is the path a module's routes are served under. The listing
// renders it and the supervision plane rebases a fragment onto it, so both
// use this one function.
func ModuleMount(namespace string) string {
	return modulesPathPrefix + "/" + namespace
}

// ModuleOpenAPIPath is where a module's fragment is served, for the listing
// to publish beside the instance.
func ModuleOpenAPIPath(namespace string) string {
	return ModuleMount(namespace) + openAPIDocumentName
}

// ModuleOpenAPI is the module's document as this operation answers it: the
// fragment the module shipped, with every path under its mount.
type ModuleOpenAPI []byte

// MarshalJSON writes the document through unchanged. The supervision plane
// already held it to routesapi.ParseFragment, so it is an object.
func (d ModuleOpenAPI) MarshalJSON() ([]byte, error) {
	if len(d) == 0 {
		return []byte("null"), nil
	}

	return d, nil
}

// Schema describes the answer as an OpenAPI document. Reflection would
// describe it as a byte string.
func (ModuleOpenAPI) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{
		Type: huma.TypeObject,
		Description: "An OpenAPI 3.1 document describing the module's own routes, with every path " +
			"rebased under the module's mount: /api/v1/modules/{namespace}/, or the root prefix the " +
			"operator named. Run a client generator on it the way the core's own document is generated from.",
		AdditionalProperties: true,
	}
}

type getModuleOpenAPIInput struct {
	Namespace string `path:"namespace" maxLength:"253" doc:"The module's namespace, derived from its type." example:"com.github.aux.acme.planner"`
}

type getModuleOpenAPIOutput struct {
	Body ModuleOpenAPI
}

// registerModuleOpenAPI publishes the document address.
//
// It is registered whatever a deployment wired, like the module mount below
// it: the document is a contract, and a build with no module plane answers the
// same address with the same refusal a namespace nothing claims gets.
func registerModuleOpenAPI(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "get-module-openapi",
		Method:      http.MethodGet,
		Path:        moduleOpenAPIPath,
		Summary:     "A module's OpenAPI document",
		Description: "The OpenAPI 3.1 document a running module ships for its own routes, with every path " +
			"rebased under the module's mount so that a client generated from it calls the module where " +
			"it is reached. A module declares the document as part of its contract, and this server " +
			"serves it rather than the module, so the address is the same for every module. A module " +
			"the operator mounted at a root prefix serves the same document at <prefix>/openapi.json too.\n\n" +
			"It takes view_resource on the module's instance, the same permission as reading the " +
			"module's own surface. A namespace no running module claims, and a module that ships no " +
			"document, both answer 404: the listing at /api/v1/resources says which instances carry one.",
		Tags:   []string{"resources"},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, in *getModuleOpenAPIInput) (*getModuleOpenAPIOutput, error) {
		document, err := moduleDocument(ctx, cfg, in.Namespace)
		if err != nil {
			return nil, err
		}

		return &getModuleOpenAPIOutput{Body: ModuleOpenAPI(document)}, nil
	})
}

// moduleDocument returns the document a running module ships, for the caller
// on ctx. It is the whole check get-module-openapi makes, and the reference
// page lists a module through it too, so the page names a module only to a
// reader this route would serve.
func moduleDocument(ctx context.Context, cfg Config, namespace string) ([]byte, error) {
	if extension.Namespace(namespace).Validate() != nil || cfg.Modules == nil || cfg.ModuleAccess == nil {
		return nil, httpfault.RouteNotFound(http.MethodGet, ModuleOpenAPIPath(namespace))
	}

	surface, claimed := cfg.Modules.Module(namespace)
	if !claimed || surface.Instance.IsZero() {
		return nil, httpfault.RouteNotFound(http.MethodGet, ModuleOpenAPIPath(namespace))
	}

	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	if err = cfg.ModuleAccess.RequireResource(ctx, who, authz.ActionViewResource, surface.Instance); err != nil {
		return nil, moduleFault(err)
	}

	if len(surface.OpenAPI) == 0 {
		return nil, httpfault.RouteNotFound(http.MethodGet, ModuleOpenAPIPath(namespace))
	}

	return surface.OpenAPI, nil
}
