package resource

import (
	"context"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// Graph is the authorization graph as the resource routes use it. It is
// declared here because this package uses it, and *authz.Store satisfies it.
//
// Every method takes a subject, an action and an object. No method asks
// whether a principal is an admin: a caller checks an action and never a
// role, so the roles can change without a change to the callers.
type Graph interface {
	// Can reports whether the subject may perform an action on an object named
	// by type and id. This package always passes a resource kind. A create
	// names no instance, so the kind is the only object that exists before
	// the instance does.
	Can(ctx context.Context, subject authz.Subject, action authz.Action, object authz.Object) (bool, error)

	// CanResource reports whether the subject may perform an action on one
	// configured instance. It answers false for an instance the caller may
	// not reach and for one that is not registered. Both cases go through the
	// same code, so a caller cannot tell them apart.
	CanResource(
		ctx context.Context, subject authz.Subject, action authz.Action, instance ref.Ref,
	) (bool, error)

	// ListResources is every instance the subject may perform an action on,
	// with the permission resolved inside the query.
	ListResources(ctx context.Context, subject authz.Subject, action authz.Action) ([]ref.Ref, error)

	// Resources is the whole directory, without any access check. A reconcile
	// of the directory against the configuration needs every row.
	Resources(ctx context.Context) ([]ref.Ref, error)

	// RegisterResources records instances in that directory.
	RegisterResources(ctx context.Context, instances ...ref.Ref) error

	// RemoveResource removes one instance from it.
	RemoveResource(ctx context.Context, instance ref.Ref) error

	// SyncResources makes the directory hold exactly these instances, in one
	// transaction. A reconcile calls it. Registering and removing one by one
	// recompiles the projection once per instance, and a stop in the middle
	// leaves rows in the directory that no configuration declares.
	SyncResources(ctx context.Context, instances ...ref.Ref) error
}

// subject parses the caller's principal into the subject the graph uses. The
// server builds the string from the caller's credential, so a string that
// does not parse is a defect in the server. It is reported as an internal
// error that names no instance, so a caller learns nothing from it.
func (s *Store) subject(principal string) (authz.Subject, error) {
	who, err := authz.ParseSubject(principal)
	if err != nil {
		return authz.Subject{}, s.internal("read the caller's principal", err)
	}

	return who, nil
}

// visible reports whether the caller may see this instance. False covers an
// instance the server does not have and an instance the caller holds no grant
// on, and the caller gets the same refusal for both.
func (s *Store) visible(ctx context.Context, who authz.Subject, instance ref.Ref) (bool, error) {
	allowed, err := s.graph.CanResource(ctx, who, authz.ActionViewResource, instance)
	if err != nil {
		return false, s.internal("read what the caller may see", err)
	}

	return allowed, nil
}

// writable reports whether the caller may rewrite the configuration of this
// instance's kind. The grant is on the kind and not on the instance, so this
// is also the answer for a create, which names no instance.
func (s *Store) writable(ctx context.Context, who authz.Subject, kind ref.Kind) (bool, error) {
	object, err := authz.NewObject(authz.TypeResourceKind, kind.String())
	if err != nil {
		return false, s.internal("name the resource kind", err)
	}

	allowed, err := s.graph.Can(ctx, who, authz.ActionConfigureResource, object)
	if err != nil {
		return false, s.internal("read what the caller may configure", err)
	}

	return allowed, nil
}

// operable reports whether the caller may act on a running instance of this
// kind without rewriting its settings. The grant is on the kind, like every
// other resource grant. It is the middle level: an operator holds it, a viewer
// does not, and a manager holds it through configure_resource.
func (s *Store) operable(ctx context.Context, who authz.Subject, kind ref.Kind) (bool, error) {
	object, err := authz.NewObject(authz.TypeResourceKind, kind.String())
	if err != nil {
		return false, s.internal("name the resource kind", err)
	}

	allowed, err := s.graph.Can(ctx, who, authz.ActionOperateResource, object)
	if err != nil {
		return false, s.internal("read what the caller may operate", err)
	}

	return allowed, nil
}

// authorizeWrite is the access check every write passes. It runs before the
// configuration is read from disk, so the answer cannot depend on what the
// configuration holds.
//
// mayCreate separates an apply from a delete: an apply may name an instance
// that does not exist yet, and a delete may not. A caller who can neither see
// the instance nor configure its kind gets the refusal for an instance that
// does not exist.
func (s *Store) authorizeWrite(
	ctx context.Context, who authz.Subject, instance ref.Ref, mayCreate bool,
) error {
	seen, err := s.visible(ctx, who, instance)
	if err != nil {
		return err
	}

	writes, err := s.writable(ctx, who, instance.Kind())
	if err != nil {
		return err
	}

	switch {
	case writes && (seen || mayCreate):
		return nil
	case seen:
		// The caller can already see the instance, so the refusal names the
		// missing grant.
		return authz.Denied(authz.ActionConfigureResource, instance)
	default:
		return httpfault.ResourceNotFound(instance.String())
	}
}

// permitted is the set of instances a caller may see. List returns only the
// declared instances that are in it.
func (s *Store) permitted(ctx context.Context, who authz.Subject) (map[ref.Ref]struct{}, error) {
	instances, err := s.graph.ListResources(ctx, who, authz.ActionViewResource)
	if err != nil {
		return nil, s.internal("list what the caller may see", err)
	}

	out := make(map[ref.Ref]struct{}, len(instances))
	for _, instance := range instances {
		out[instance] = struct{}{}
	}

	return out, nil
}

// syncDirectory makes the graph's instance directory hold exactly the
// instances the configuration declares. Access checks read the directory, so
// a row for an instance that is not declared would make the graph answer for
// an instance that does not exist.
//
// It reconciles against the directory and not against the last reconciled
// document, because the process has no last document for an instance that was
// removed while it was down.
//
// The graph takes the whole declared set and does the diff inside one
// transaction. Removing rows one by one would recompile the projection once
// per removed instance, and a stop in the middle would leave the directory
// partly updated.
func (s *Store) syncDirectory(ctx context.Context, next Document) error {
	instances := make([]ref.Ref, 0, len(next.Instances))
	for _, instance := range next.Instances {
		instances = append(instances, instance.Ref)
	}

	return s.graph.SyncResources(ctx, instances...)
}
