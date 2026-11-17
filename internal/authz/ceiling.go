package authz

import (
	"context"
	"slices"
	"strings"
)

// Scope names a part of what a principal holds that one credential may use. A
// credential with scopes speaks for its principal with the principal's grants,
// cut down to the actions its scopes name. A scope never adds an action: a
// request is allowed only when the graph confers the action and the scopes
// allow it.
//
// The set is closed and the core owns it. A scope is a ceiling over the closed
// action set, so an extension must not be able to add one.
type Scope string

// The scopes, from the narrowest up. Each one allows everything the one below
// it allows, so a credential with operate never needs read beside it.
const (
	// ScopeRead allows the read actions: observing a device and its telemetry,
	// reading the audit of who did what, seeing a configured instance and
	// reading the files in a storage.
	ScopeRead Scope = "read"
	// ScopeOperate adds acting on a machine in its normal duty, operating a
	// configured instance and writing files into a storage. It is what a
	// client that starts, pauses and stops prints needs.
	ScopeOperate Scope = "operate"
	// ScopeAdminister allows every action the principal holds, the dangerous
	// tier included. A credential with it is limited by the graph alone.
	ScopeAdminister Scope = "administer"
)

// readMask is what ScopeRead allows. Every scope allows it, so every scope can
// see the objects it may act on.
const readMask = maskObserve | maskViewAudit | maskViewResource | maskReadStorage

// operateMask is what ScopeOperate allows.
const operateMask = readMask | maskOperate | maskOperateResource | maskWriteStorage

// Scopes returns every scope, from the narrowest up.
func Scopes() []Scope { return []Scope{ScopeRead, ScopeOperate, ScopeAdminister} }

// Valid reports whether s is one of the scopes.
func (s Scope) Valid() bool {
	return s == ScopeRead || s == ScopeOperate || s == ScopeAdminister
}

// String returns the scope as it is written on the wire and in the store.
func (s Scope) String() string { return string(s) }

// ParseScopes reads the scopes a request or a stored row names. An empty list
// and a name outside the set are refused: a scoped credential with no scope
// would allow nothing, and one with a name this build does not know must not be
// read as allowing more than it says. Duplicates are dropped and the answer is
// in the order Scopes declares.
func ParseScopes(names []string) ([]Scope, error) {
	if len(names) == 0 {
		return nil, invalidScope.New("a scoped credential names at least one scope")
	}

	seen := map[Scope]bool{}

	for _, name := range names {
		scope := Scope(strings.TrimSpace(name))
		if !scope.Valid() {
			return nil, invalidScope.New("the scope is not one this server knows").
				With(paramName(name))
		}

		seen[scope] = true
	}

	out := make([]Scope, 0, len(seen))

	for _, scope := range Scopes() {
		if seen[scope] {
			out = append(out, scope)
		}
	}

	return out, nil
}

// ScopeNames renders scopes for the wire and the store.
func ScopeNames(scopes []Scope) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, scope.String())
	}

	return out
}

// WithinScopes reports whether every scope in asked is allowed by held. It is
// what keeps a credential from handing out more than it has: a pairing a
// person approves, or a token a scoped credential names, may only narrow.
func WithinScopes(asked, held []Scope) bool {
	ceiling := ceilingOf(held)

	for _, scope := range asked {
		if !ceiling.allows(ceilingOf([]Scope{scope})) {
			return false
		}
	}

	return true
}

// ceiling is what the scopes of the request's credential allow. The zero value
// allows everything, which is a request whose credential carries no scopes: a
// browser session, or a token another issuer signed.
type ceiling struct {
	// limited is false for a request with no scopes, or with administer.
	limited bool
	// set is the actions a limited request may reach.
	set actionSet
}

// ceilingKey is the context key the scopes of the request's credential ride
// under.
type ceilingKey struct{}

// WithScopes returns a context under which every decision this package makes
// is cut down to what the scopes allow. Nil scopes leave the context as it is.
//
// The transport sets it once, where it resolves the credential, so a decision
// made anywhere below reads the same ceiling. A scope set on a context that
// already carries one narrows it further and never widens it.
func WithScopes(ctx context.Context, scopes []Scope) context.Context {
	if scopes == nil {
		return ctx
	}

	next := ceilingOf(scopes)
	if current := ceilingFrom(ctx); current.limited {
		if !next.limited {
			return ctx
		}

		next.set &= current.set
	}

	return context.WithValue(ctx, ceilingKey{}, next)
}

// ScopesFrom returns the scopes a context carries, nil for one that carries
// none. It is how a transport that hands a request on, to a module or to a
// stream that outlives it, keeps the ceiling with it.
func ScopesFrom(ctx context.Context) []Scope {
	current := ceilingFrom(ctx)
	if !current.limited {
		return nil
	}

	if current.set&operateMask == operateMask {
		return []Scope{ScopeOperate}
	}

	return []Scope{ScopeRead}
}

// ceilingFrom reads the ceiling a context carries.
func ceilingFrom(ctx context.Context) ceiling {
	if ctx == nil {
		return ceiling{}
	}

	current, _ := ctx.Value(ceilingKey{}).(ceiling)

	return current
}

// ceilingOf is the ceiling one set of scopes draws. An empty set allows the
// read actions only: the store refuses such a row before it gets here, and a
// mistake that reached this line must not open everything.
func ceilingOf(scopes []Scope) ceiling {
	if slices.Contains(scopes, ScopeAdminister) {
		return ceiling{}
	}

	if slices.Contains(scopes, ScopeOperate) {
		return ceiling{limited: true, set: operateMask}
	}

	return ceiling{limited: true, set: readMask}
}

// clamp cuts a mask down to what the ceiling allows.
func (c ceiling) clamp(mask actionSet) actionSet {
	if !c.limited {
		return mask
	}

	return mask & c.set
}

// allows reports whether c allows everything other allows.
func (c ceiling) allows(other ceiling) bool {
	if !c.limited {
		return true
	}

	if !other.limited {
		return false
	}

	return other.set&^c.set == 0
}

// administers reports whether the ceiling allows administering: writing a
// grant, reading the grants on an object, changing an org role, accepting a
// declaration change and reaching the dangerous tier. Only a request with no
// scopes, or with administer, does.
func (c ceiling) administers() bool { return !c.limited }

// requireAdministering refuses a request whose scopes do not allow
// administering, before the graph is read. The refusal is the one a missing
// grant gets, because to the caller the two are the same answer: this
// credential may not do that.
func requireAdministering(ctx context.Context, object Object) error {
	if ceilingFrom(ctx).administers() {
		return nil
	}

	// A refusal about no one object names the identity surface, which is
	// what administering access is about.
	if object.IsZero() {
		if auth, err := authResourceKind(); err == nil {
			object = auth
		}
	}

	return denied(ActionAdminister, object)
}

// RefuseBelowAdminister is the refusal a transport gives a credential whose
// scopes stop short of administer, for an act the graph is not asked about,
// such as revoking another credential. It is the refusal requireAdministering
// gives, so the two read alike.
func RefuseBelowAdminister() error {
	return requireAdministering(WithScopes(context.Background(), []Scope{ScopeRead}), Object{})
}
