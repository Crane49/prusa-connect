package resourcehost

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// One module calling the routes of another.
//
// Every request reaches a module through the host, from a person and from
// another module. A call is relayed only to a module the caller named under
// calls and the operator approved. It runs as the person the caller is
// serving, checked as the HTTP mount checks that person, or as the caller's
// own principal. In both cases the call is recorded in flights for the other
// module as a request the host forwards. That module may then act for the
// principal while the call is open, and ask access.check about it afterwards.

// RouteAccess is the check a call in a person's name makes on the
// authorization graph. The HTTP mount makes the same check on the module's
// instance. *authz.Store satisfies it.
type RouteAccess interface {
	RequireResource(ctx context.Context, who authz.Subject, action authz.Action, instance ref.Ref) error
}

// callsFor is the routes.call a module's session is given, and nil for a
// module the operator let call nothing and whose manifest does not require
// the method, so the method is not served.
//
// A module that requires routes.call is served it even when the operator
// approved no call, or removed the last one on an update. Each call then
// reads ErrCallNotDeclared, and the module starts. Without the method the
// start would fail on the missing capability, after the install had
// succeeded.
func (h *Host) callsFor(module Module, required bool) routesapi.Modules {
	if len(module.Calls) == 0 && !required {
		return nil
	}

	return moduleCalls{host: h, caller: module}
}

// moduleCalls relays the calls of one module.
type moduleCalls struct {
	host   *Host
	caller Module
}

func (c moduleCalls) Call(ctx context.Context, params routesapi.CallParams) (routesapi.Response, error) {
	// The wire handler already checked the bounds. They are checked again
	// here, where every call is relayed, so a path that leads outside the
	// other module's mount never reaches that module, even from a caller
	// inside this process.
	if err := params.Validate(); err != nil {
		return routesapi.Response{}, fmt.Errorf("relay a call: %w", err)
	}

	if !slices.Contains(c.caller.Calls, params.Module) {
		return routesapi.Response{}, fmt.Errorf("%w: %s did not name %s under calls",
			routesapi.ErrCallNotDeclared, c.caller.Instance, params.Module)
	}

	// A module calls only a module it names under requires or suggests, so
	// the releases it works with are known.
	if !slices.Contains(c.caller.Dependencies, params.Module) {
		return routesapi.Response{}, fmt.Errorf("%w: %s names %s under neither requires nor suggests",
			routesapi.ErrCallNotDeclared, c.caller.Instance, params.Module)
	}

	if extension.Namespace(params.Module).Validate() != nil {
		return routesapi.Response{}, fmt.Errorf("%w: %q is not a namespace", routesapi.ErrCallInvalid, params.Module)
	}

	target, supervised, ok := c.host.routedModule(params.Module)
	if !ok || target == c.caller.Instance {
		return routesapi.Response{}, fmt.Errorf("%w: %s", routesapi.ErrCallUnavailable, params.Module)
	}

	principal, ctx, err := c.principalOf(ctx, params, target)
	if err != nil {
		return routesapi.Response{}, err
	}

	// The call is recorded for as long as it is open, as a request from a
	// person is. The called module may then read and write as the principal
	// and ask about it later.
	leave := c.host.flights.board(target, principal, authz.ScopesFrom(ctx))
	defer leave()

	answered, err := supervisor.Call(ctx, supervised, string(routesapi.MethodServe),
		func(callCtx context.Context, held any) (routesapi.Response, error) {
			return serveThroughChild(callCtx, held, params.Request(principal))
		})
	if err != nil {
		c.host.cfg.Logger.WarnContext(ctx, "a module call was not answered",
			"caller", c.caller.Instance.String(), "module", params.Module, "path", params.Path, "error", err.Error())

		return routesapi.Response{}, fmt.Errorf("%w: %s did not answer", routesapi.ErrCallUnavailable, params.Module)
	}

	return answered, nil
}

// principalOf decides who the call runs as, and the context it runs under.
//
// A call naming nobody is the caller's own work and runs as the caller's
// principal. The approved calls entry lets it reach the other module. A call
// naming a person runs as that person. It is allowed only while the caller is
// serving them and only when the person may reach the other module's routes.
// It is limited to the scopes of their open request.
func (c moduleCalls) principalOf(
	ctx context.Context, params routesapi.CallParams, target ref.Ref,
) (string, context.Context, error) {
	if params.Principal == "" {
		return authz.TypeSink.String() + ":" + c.caller.Instance.ID(), authz.WithScopes(ctx, nil), nil
	}

	narrowed, serving := c.host.flights.within(ctx, c.caller.Instance, params.Principal)
	if !serving {
		return "", ctx, fmt.Errorf("%w: %s is not a principal this host handed %s in a request it is serving",
			routesapi.ErrCallDenied, params.Principal, c.caller.Instance)
	}

	access := c.host.cfg.RouteAccess
	if access == nil {
		return "", ctx, fmt.Errorf("%w: no authorization graph stands behind the modules", routesapi.ErrCallDenied)
	}

	who, err := authz.ParseSubject(params.Principal)
	if err != nil {
		return "", ctx, fmt.Errorf("%w: %s is not a principal", routesapi.ErrCallDenied, params.Principal)
	}

	if err = access.RequireResource(narrowed, who, routeAction(params.Method), target); err != nil {
		return "", ctx, fmt.Errorf("%w: %s may not reach %s", routesapi.ErrCallDenied, params.Principal, params.Module)
	}

	return params.Principal, narrowed, nil
}

// routeAction is the permission a call needs on the other module's instance:
// view for GET and HEAD, operate for every other method, as on the HTTP mount.
func routeAction(method string) authz.Action {
	if method == http.MethodGet || method == http.MethodHead {
		return authz.ActionViewResource
	}

	return authz.ActionOperateResource
}

// requiresCalls reports whether the module's manifest requires routes.call.
func requiresCalls(declared *serviceSpec) bool {
	return slices.ContainsFunc(requirementsOf(declared), func(required install.Requirement) bool {
		return required.Capability == string(routesapi.MethodCall)
	})
}

// routedModule is the running module under a namespace that serves routes.
func (h *Host) routedModule(namespace string) (ref.Ref, *supervisor.Instance[any], bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for instance, running := range h.running {
		held, ok := moduleNamespaceOf(running.declaration)
		if !ok || held != namespace || !moduleServesRoutes(running.runner) {
			continue
		}

		return instance, running.runner, true
	}

	return ref.Ref{}, nil, false
}
