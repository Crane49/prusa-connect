package authz

import (
	"context"
	"database/sql"
	"errors"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

const (
	pointCheckSQL = `SELECT 1 FROM device_access
	WHERE subject_type = ? AND subject_id = ? AND device_id = ? AND (actions & ?) <> 0`

	// The kind-level check. It is what a create goes through, because a create
	// names no instance: the object that exists before the instance does is the
	// kind, which is the same object every other resource decision reads.
	resourceKindCheckSQL = `SELECT 1 FROM resource_access
	WHERE subject_type = ? AND subject_id = ? AND kind = ? AND (actions & ?) <> 0`

	// The instance-level check. The join is what makes an instance the server
	// does not have refuse identically to one the caller may not see: neither
	// produces a row, and one row is the whole answer.
	resourceInstanceCheckSQL = `SELECT 1 FROM resource_access a
	JOIN authz_resource r ON r.kind = a.kind
	WHERE a.subject_type = ? AND a.subject_id = ? AND r.kind = ? AND r.resource_id = ?
	  AND (a.actions & ?) <> 0`
)

// Can reports whether subject may perform action on object. It is one indexed
// lookup in the projection for that action's object type, and it is the only
// point-check entry point for an object named by type and id: every transport
// asks this question the same way, because a permission enforced on one
// transport and missing on another is the top bug class in this area.
//
// An action is defined on exactly one object type. Asking a device verb of a
// resource kind, or the reverse, is a caller bug and is refused rather than
// answered false, because the two masks share no bit and the lookup would miss
// for a reason that reads like a denial.
func (s *Store) Can(ctx context.Context, subject Subject, action Action, object Object) (bool, error) {
	mask, err := checkable(ctx, subject, action)
	if err != nil {
		return false, err
	}

	// checkable has already refused an action outside the closed set, so the
	// domain is always known here.
	domain, _ := action.domain()
	if object.typ != domain {
		return false, unsupportedObject.New("this action is not defined on that object type").
			With(paramObject(object), paramAction(action))
	}

	query := pointCheckSQL
	if domain == TypeResourceKind {
		query = resourceKindCheckSQL
	}

	return s.one(ctx, query, subject.typ.String(), subject.id, object.id, int64(mask))
}

// Require is Can for a caller that wants the refusal as an error: it returns a
// fault carrying ACCESS_DENIED, which a transport renders as its own answer,
// 404 rather than 403 wherever the existence of the object is itself worth
// hiding.
func (s *Store) Require(ctx context.Context, subject Subject, action Action, object Object) error {
	allowed, err := s.Can(ctx, subject, action, object)
	if err != nil {
		return err
	}

	if !allowed {
		return denied(action, object)
	}

	return nil
}

// denied is the refusal for an object named by type and id. It is one function
// so that every surface refusing on this shape returns the same bytes: an
// object the caller may not reach and an object that is not there are the same
// answer, and two spellings of it would be a difference to read.
func denied(action Action, object Object) error {
	return accessDenied.New("the subject holds no grant reaching this action").
		With(paramObject(object), paramAction(action))
}

// DeniedObject is the refusal Require answers, for a caller that has already
// asked Can and been told no. It is the same bytes, so a surface that decided
// on one answer refuses the way Require would have.
func DeniedObject(action Action, object Object) error { return denied(action, object) }

// CanResource reports whether subject may perform action on one configured
// instance. It answers false both for an instance the caller may not reach and
// for one the server does not have, in one statement and by one path, so the
// two are not distinguishable by a caller, by a timing difference or by the
// error a transport renders.
//
// The grant it reads is written on the instance's kind. What varies per
// instance is only whether the instance is registered.
func (s *Store) CanResource(
	ctx context.Context, subject Subject, action Action, instance ref.Ref,
) (bool, error) {
	mask, err := checkableResource(ctx, subject, action, instance)
	if err != nil {
		return false, err
	}

	return s.one(
		ctx, resourceInstanceCheckSQL,
		subject.typ.String(), subject.id, instance.Kind().String(), instance.ID(), int64(mask),
	)
}

// RequireResource is CanResource for a caller that wants the refusal as an
// error. The fault it returns carries the reference the caller named and
// nothing else, so a refusal for an absent instance and a refusal for a
// forbidden one are the same bytes.
func (s *Store) RequireResource(
	ctx context.Context, subject Subject, action Action, instance ref.Ref,
) error {
	allowed, err := s.CanResource(ctx, subject, action, instance)
	if err != nil {
		return err
	}

	if !allowed {
		return accessDenied.New("the subject holds no grant reaching this action").
			With(paramInstance(instance), paramAction(action))
	}

	return nil
}

// Denied is the refusal a caller gets for an action they hold no grant
// reaching, for a surface that has already taken the decision through
// CanResource and only needs to render it. It exists so that one code answers
// this question everywhere: a second registration for the same answer is a
// second thing to keep in agreement with the message bundle.
//
// It is never the answer for an instance the caller may not see. That refusal
// is the refusal for an instance that is not there, and the transport renders
// it as one.
func Denied(action Action, instance ref.Ref) error {
	return accessDenied.New("the subject holds no grant reaching this action").
		With(paramInstance(instance), paramAction(action))
}

// one runs a lookup that either finds a row or does not, and reports which.
func (s *Store) one(ctx context.Context, query string, args ...any) (bool, error) {
	var found int

	err := s.db.QueryRowContext(ctx, s.q(query), args...).Scan(&found)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, storeFailure("point check", err)
	default:
		return true, nil
	}
}

// canHoldGrants reports whether a type is something a credential resolves to.
// An org, a device group, a device, a resource kind, or a group named without
// its member relation is a place grants are written, not a caller: asking
// whether one of them may act is a caller bug, and answering false from an
// empty probe would hide it.
func canHoldGrants(t Type) bool {
	switch t {
	case TypeUser, TypeServiceAccount, TypeDeviceCredential, TypeNode, TypeSink, TypeHistory:
		return true
	case TypeGroup, TypeOrg, TypeDeviceGroup, TypeDevice, TypeResourceKind:
		return false
	default:
		return false
	}
}

// asks validates the side of a decision that names who is asking. A userset is
// a set of principals, not a caller, so it cannot be the subject of a decision.
//
// It is separate from checkable because a review of everything a subject may do
// on one object names no action, and a review that skipped this would answer an
// empty set for a userset instead of refusing it.
func asks(subject Subject) error {
	if subject.IsZero() {
		return unknownType.New("check for an unconstructed subject")
	}

	if subject.IsUserset() {
		return invalidSubject.New("a decision is made about a principal, not a userset").
			With(paramSubject(subject))
	}

	if !canHoldGrants(subject.typ) {
		return invalidSubject.New("this type holds grants, it does not make requests").
			With(paramSubject(subject))
	}

	return nil
}

// checkable validates the pair a decision is asked about and returns the mask
// the projection is tested with, cut down to what the scopes of the request's
// credential allow. An action the scopes leave out tests with an empty mask,
// which no row matches, so every check, list and count refuses it by the path
// a missing grant takes.
func checkable(ctx context.Context, subject Subject, action Action) (actionSet, error) {
	if err := asks(subject); err != nil {
		return 0, err
	}

	if err := ValidateAction(action); err != nil {
		return 0, err
	}

	return ceilingFrom(ctx).clamp(action.mask()), nil
}

// checkableResource adds what an instance decision needs on top: the action has
// to be one the resource surface defines, and the reference has to name a kind
// that surface governs. Both are properties of the request rather than of the
// graph, so refusing them tells a caller nothing about what exists.
func checkableResource(ctx context.Context, subject Subject, action Action, instance ref.Ref) (actionSet, error) {
	mask, err := checkable(ctx, subject, action)
	if err != nil {
		return 0, err
	}

	domain, _ := action.domain()
	if domain != TypeResourceKind {
		return 0, unsupportedObject.New("this action is not defined on a resource instance").
			With(paramInstance(instance), paramAction(action))
	}

	if err = configurableInstance(instance); err != nil {
		return 0, err
	}

	return mask, nil
}

// DeviceAccessKey is what a statement of another store over the host database
// filters device_access by, so a list resolves the permission inside its own
// query: the subject's type and id as the projection keys them, and the mask of
// the action an (actions & mask) <> 0 predicate tests. It refuses what a
// decision would refuse, and the mask is cut down to what the scopes on ctx
// allow, as a decision's is.
func DeviceAccessKey(
	ctx context.Context, subject Subject, action Action,
) (subjectType, subjectID string, mask int64, err error) {
	set, err := checkable(ctx, subject, action)
	if err != nil {
		return "", "", 0, err
	}

	return subject.typ.String(), subject.id, int64(set), nil
}
