package app

// The one question a module asks the authorization graph about somebody else:
// may this subject perform this action on this object.

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
)

// moduleAccess answers sdk/accessapi for one module instance.
//
// It holds the half of the bound the ceiling decides: an instance asks about an
// action the operator listed for it and about nothing else. The other half, the
// subject, is the supervision plane's, because only the host knows which
// requests it forwarded to this module.
//
// A question outside the ceiling is refused rather than answered false. False
// is an answer about the graph, and a module reading it would learn that the
// subject holds nothing, which is the bit the ceiling exists to withhold.
type moduleAccess struct {
	grants   httpapi.GrantStore
	standing moduleStanding
}

var _ accessapi.Access = moduleAccess{}

// moduleAccessors binds an instance to the graph it may ask.
//
// It is a late binding for the reason moduleFleets is: the graph exists before
// the supervision plane, but the plane is built inside the composition root
// before the value that carries it is in scope here.
type moduleAccessors struct {
	grants httpapi.GrantStore
}

// bind fills the holder. It runs once, before any module starts.
func (m *moduleAccessors) bind(grants httpapi.GrantStore) { m.grants = grants }

// forModule is what resourcehost.Config.Access takes.
func (m *moduleAccessors) forModule(module resourcehost.Module) accessapi.Access {
	if m.grants == nil {
		return nil
	}

	return moduleAccess{grants: m.grants, standing: standingOf(module)}
}

// Check answers whether the subject may perform the action on the object.
//
// The action is held to the instance's ceiling and its checks, so a module
// installed with observe asks about observe and learns nothing about who may
// operate a printer. A module that lists operate under checks asks about it
// and still cannot operate a printer as itself. The subject is held by the
// supervision plane in front of this.
//
// An empty subject is the instance itself, which is the one principal a module
// always knows about, and it is held to the same ceiling.
//
// An object this server does not have answers exactly as one the subject
// reaches nothing on: allowed false. That is the rule the HTTP permission
// review follows, and it is why this call says nothing about which printers
// this server holds.
func (m moduleAccess) Check(
	ctx context.Context, params accessapi.CheckParams,
) (accessapi.Decision, error) {
	action := authz.Action(params.Action)

	if !slices.Contains(m.standing.ceiling, action) && !slices.Contains(m.standing.checks, action) {
		return accessapi.Decision{}, fmt.Errorf("%w: %s declares %v and checks %v and not %s",
			accessapi.ErrDenied, m.standing.principal,
			authz.ActionNames(m.standing.ceiling), authz.ActionNames(m.standing.checks), params.Action)
	}

	subject := params.Subject
	if subject == "" {
		subject = m.standing.principal
	}

	who, err := authz.ParseSubject(subject)
	if err != nil {
		return accessapi.Decision{}, fmt.Errorf("%w: %s is not a principal",
			accessapi.ErrInvalidQuestion, subject)
	}

	object, err := authz.NewObject(authz.Type(params.ObjectType), params.ObjectID)
	if err != nil {
		return accessapi.Decision{}, fmt.Errorf("%w: %s:%s is not an object: %w",
			accessapi.ErrInvalidQuestion, params.ObjectType, params.ObjectID, err)
	}

	allowed, err := m.grants.Can(ctx, who, action, object)
	if err != nil {
		return accessapi.Decision{}, fmt.Errorf("ask the authorization graph: %w", err)
	}

	return accessapi.Decision{Allowed: allowed}, nil
}

// moduleRouteAccess is the graph a module's call to another module's routes
// in a person's name is decided by. It is built empty, because the plane
// exists before the graph, and bound before any module starts.
type moduleRouteAccess struct {
	graph httpapi.ModuleAccess
}

// bind fills the holder. It runs once, before any module starts.
func (m *moduleRouteAccess) bind(graph httpapi.ModuleAccess) { m.graph = graph }

// RequireResource answers as the HTTP mount answers, and refuses everything
// while no graph is bound.
func (m *moduleRouteAccess) RequireResource(
	ctx context.Context, who authz.Subject, action authz.Action, instance ref.Ref,
) error {
	if m.graph == nil {
		return errors.New("no authorization graph is bound")
	}

	return m.graph.RequireResource(ctx, who, action, instance) //nolint:wrapcheck // the graph's own refusal.
}
