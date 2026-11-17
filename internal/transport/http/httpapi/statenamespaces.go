package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// stateNamespacesPath is the operator's view of what the modules are keeping.
// No endpoint under it returns a stored value. This one publishes a name, a
// count and a byte total, which answers "which module is filling the disk"
// without answering "what is in it".
const stateNamespacesPath = "/api/v1/state/namespaces"

// StateNamespace is one module's record store as an operator reads it.
type StateNamespace struct {
	Namespace string `json:"namespace" doc:"The namespace, which is the module's own and is derived from its type."`
	Instance  string `json:"instance"  doc:"The configured instance that claims the namespace."`
	Keys      int64  `json:"keys"      doc:"How many records the namespace holds."`
	Bytes     int64  `json:"bytes"     doc:"What those records count toward the namespace byte limit."`
	Limit     int64  `json:"limit"     doc:"The byte limit the namespace is held to: 8 MiB, or the room the operator granted the module as service.records_mib."`
}

// StateStore is the record store as this transport reads it. It answers the
// accounting only: there is no method here that returns a value, so no
// operation on this surface can return one.
type StateStore interface {
	// Namespaces answers every namespace holding a record, with its key count
	// and its byte total.
	Namespaces(ctx context.Context) ([]StateNamespace, error)
}

type listStateNamespacesOutput struct {
	Body struct {
		Items []StateNamespace `json:"items"`
	}
}

// registerStateNamespaces publishes the listing.
//
// It is registered whatever a deployment wired, like the storage listing above
// it and for the same reason: the document is a contract, and a deployment with
// no module declared answers the same route with an empty listing.
func registerStateNamespaces(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "list-state-namespaces",
		Method:      http.MethodGet,
		Path:        stateNamespacesPath,
		Summary:     "What each module is keeping",
		Description: "Every module record namespace the caller may see, with how many records it holds " +
			"and how many bytes those records count toward the namespace limit. No stored value is " +
			"returned by this operation or by any other under this path.\n\n" +
			"It takes view_resource on the instance that claims the namespace, which is the same " +
			"permission as reading that module's own surface: a caller who may see what a module " +
			"serves may see how much of the disk it is using. A namespace no running instance claims " +
			"is not listed, because there is no instance to decide the permission against.",
		Tags:   []string{"resources"},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*listStateNamespacesOutput, error) {
		out := &listStateNamespacesOutput{}
		out.Body.Items = []StateNamespace{}

		records, err := resolveState(cfg)
		if err != nil {
			return nil, err
		}

		if records == nil {
			return out, nil
		}

		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		held, err := records.Namespaces(ctx)
		if err != nil {
			return nil, httpfault.Unexpected("read what the modules are keeping")
		}

		out.Body.Items = permittedNamespaces(ctx, cfg, who, held)

		return out, nil
	})
}

// StateResolver answers the record store this request reads, per call.
//
// It is a resolver for the reason HistoryResolver is: the backend behind
// sdk/stateapi is a supervised instance, and the plane supervises it.
// Resolving per call also breaks a cycle: the plane does not need, at its own
// construction, a value the plane itself will build.
type StateResolver func() (StateStore, error)

// resolveState is the record store this request reads, and nil for a
// deployment serving none. A nil resolver is a build that wired none.
func resolveState(cfg Config) (StateStore, error) {
	if cfg.State == nil {
		return nil, nil //nolint:nilnil // no store and no failure is the shape the caller branches on.
	}

	return cfg.State()
}

// permittedNamespaces keeps the rows this caller may see.
//
// The permission is resolved per row and not inside the statement. Elsewhere
// in this codebase that is refused, because a listing that fetches and then
// filters leaks through its pagination and its counts. This one has neither,
// and the whole row set is one row per installed module.
//
// It could not be resolved in the statement in any case. A namespace is not an
// authorization object and never becomes one, so there is no column in the
// record store to join the graph against. The instance that claims the
// namespace decides the answer, and only the supervision plane knows which one
// that is.
func permittedNamespaces(
	ctx context.Context, cfg Config, who authz.Subject, held []StateNamespace,
) []StateNamespace {
	permitted := make([]StateNamespace, 0, len(held))

	for _, one := range held {
		if cfg.Modules == nil || cfg.ModuleAccess == nil {
			break
		}

		surface, claimed := cfg.Modules.Module(one.Namespace)
		if !claimed || surface.Instance.IsZero() {
			// A namespace whose module is not running. Its rows are still on
			// the disk, and the tombstone publishes them, not this listing.
			// There is no instance here to decide a permission against, and
			// showing the row to everybody would be wrong.
			continue
		}

		if err := cfg.ModuleAccess.RequireResource(
			ctx, who, authz.ActionViewResource, surface.Instance,
		); err != nil {
			continue
		}

		one.Instance = surface.Instance.String()
		permitted = append(permitted, one)
	}

	return permitted
}
