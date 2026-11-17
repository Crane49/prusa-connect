package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// moduleMarketplacePath is the marketplace switch. It is a fixed segment beside
// the index, and no module can claim it: a namespace has at least two labels,
// so a single word is never one.
const moduleMarketplacePath = modulesPathPrefix + "/marketplace"

// moduleMarketplaceOutput is the marketplace as this server reads it now.
type moduleMarketplaceOutput struct {
	Body ModuleMarketplace
}

// setModuleMarketplaceInput is the choices the switches take.
type setModuleMarketplaceInput struct {
	Body struct {
		Enabled   bool  `json:"enabled"             doc:"True reads the marketplace core.marketplace.url names, false reads nothing from it."`
		Community *bool `json:"community,omitempty" doc:"True lists the community extensions in the web app, the rows whose trust is verified or community, and false hides them. Omitted, the stored choice is kept."`
	}
}

// moduleMarketplaceErrors are what both operations answer besides the
// marketplace: no credential, no permission, no marketplace configured,
// modules switched off by the operator, no installer in this build, and a file
// this server could not write.
var moduleMarketplaceErrors = []int{
	http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict,
	http.StatusNotImplemented, http.StatusInternalServerError,
}

// registerModuleMarketplace publishes reading and switching the marketplace.
func registerModuleMarketplace(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "get-module-marketplace",
		Method:      http.MethodGet,
		Path:        moduleMarketplacePath,
		Summary:     "The marketplace this deployment reads modules from",
		Description: "Reports the marketplace core.marketplace.url names, whether a person switched it " +
			"off, and how the last read of its catalogue went. A deployment that names no marketplace " +
			"answers 404. It takes configure_resource on a resource kind, as the index does. While the " +
			"operator's core.modules_enabled is false it answers MODULES_DISABLED, and SAFE_MODE in " +
			"safe mode, even where no marketplace is named.",
		Tags:   []string{"resources"},
		Errors: moduleMarketplaceErrors,
	}, func(ctx context.Context, _ *struct{}) (*moduleMarketplaceOutput, error) {
		if err := requireModuleAdmin(ctx, cfg); err != nil {
			return nil, err
		}

		market, ok, err := cfg.ModuleInstall.Marketplace(ctx)
		if err != nil {
			return nil, err
		}

		if !ok {
			return nil, noMarketplace()
		}

		return &moduleMarketplaceOutput{Body: market}, nil
	})

	register(api, huma.Operation{
		OperationID: "set-module-marketplace",
		Method:      http.MethodPut,
		Path:        moduleMarketplacePath,
		Summary:     "Switch the marketplace off, or on again",
		Description: "Stores the switch in the machine-owned file and applies it at once, with no " +
			"restart. Switched off, this server fetches nothing from the marketplace: the index lists " +
			"only its local rows and an install that names no address is refused. Modules already " +
			"installed from it keep running. With community, it also stores whether the web app lists " +
			"the community extensions, the rows whose trust is not official. The stored value wins over a configured " +
			"core.marketplace.url. A deployment that names no marketplace answers 404. It takes the " +
			"admin role on this server's organization, the role that administers its grants, because " +
			"the switch decides where every administrator's installs come from. While the operator's " +
			"core.modules_enabled is false it answers MODULES_DISABLED, and SAFE_MODE in safe mode, " +
			"and stores nothing, even where no marketplace is named.",
		Tags:   []string{"resources"},
		Errors: moduleMarketplaceErrors,
	}, func(ctx context.Context, in *setModuleMarketplaceInput) (*moduleMarketplaceOutput, error) {
		if err := requireModuleAdmin(ctx, cfg); err != nil {
			return nil, err
		}

		if err := requireOrgAdmin(ctx, cfg, "where this server installs modules from"); err != nil {
			return nil, err
		}

		market, ok, err := cfg.ModuleInstall.SetMarketplace(ctx, in.Body.Enabled, in.Body.Community)
		if err != nil {
			return nil, err
		}

		if !ok {
			return nil, noMarketplace()
		}

		return &moduleMarketplaceOutput{Body: market}, nil
	})
}

// noMarketplace is the answer of a deployment whose configuration names none.
func noMarketplace() error {
	return httpfault.ResourceNotFound(httpfault.Reference("module", "marketplace"))
}

// requireOrgAdmin refuses a caller who does not hold the admin role on this
// server's organization. A setting that applies to the whole server is
// administered by whoever administers the organization's grants, so the
// question is the one the grant surface asks: may this caller read the grants
// written on the organization. Nothing is read for the caller.
func requireOrgAdmin(ctx context.Context, cfg Config, what string) error {
	if cfg.Grants == nil {
		return httpfault.Unexpected("ask the authorization graph who may administer " + what)
	}

	who, err := caller(ctx)
	if err != nil {
		return err
	}

	org, err := authz.NewObject(authz.TypeOrg, bootstrapOrgName)
	if err != nil {
		return httpfault.Unexpected("name this server's organization")
	}

	if _, err = cfg.Grants.Grants(ctx, who, org); err == nil {
		return nil
	}

	if raised, ok := errors.AsType[*fault.Fault](err); ok && raised.Code() == authz.CodeAccessDenied() {
		return fault.New(authz.CodeAccessDenied(),
			"this caller holds no admin role on the organization, so it may not administer "+what).With(
			fault.StringParam("object", org.String()))
	}

	return httpfault.Unexpected("ask the authorization graph who may administer " + what)
}
