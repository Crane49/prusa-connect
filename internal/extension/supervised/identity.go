package supervised

import (
	"context"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
)

// ErrNotAnIdentity reports an instance that cannot verify a bearer token: one
// the host is not supervising, one of another kind, or one whose target does
// not satisfy the identity contract.
var ErrNotAnIdentity = errors.New("supervised: the instance is not an identity provider")

// ErrNoBearer reports an identity instance that does not advertise the bearer
// flow. Its login flows come from core.auth, and there is nothing here to ask
// it.
var ErrNoBearer = errors.New("supervised: the identity provider verifies no bearer token")

// verifyOp is the operation name a token check is counted under.
const verifyOp = "verify_token"

// Identity is a supervised identity provider, as the bearer path reads it.
//
// Every token check goes through the instance's bulkhead and deadline, so a
// plugin that stalls on its key set uses up its own slots and nobody else's,
// and one that is not serving is refused at the gate. The instance is
// type-erased, because the host supervises every kind through one loop, so the
// assertion back to the contract happens per call, inside the supervised span.
type Identity struct {
	instance *supervisor.Instance[any]
}

// AttachIdentity wraps an instance the host is already supervising.
func AttachIdentity(instance *supervisor.Instance[any]) (*Identity, error) {
	if instance == nil {
		return nil, fmt.Errorf("%w: no instance", ErrNotAnIdentity)
	}

	if kind := instance.Ref().Kind(); kind != ref.KindAuth {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotAnIdentity, instance.Ref(), kind)
	}

	return &Identity{instance: instance}, nil
}

// Bearer reports whether the running provider advertises the bearer flow. It is
// false for an instance that is not serving. That is the safe answer: the token
// is refused instead of checked against nothing.
func (i *Identity) Bearer() bool {
	bearer, ok := supervisor.Peek(i.instance, func(target any) bool {
		provider, isProvider := target.(authapi.IdentityProvider)
		if !isProvider {
			return false
		}

		_, verifies := target.(authapi.TokenVerifier)

		return verifies && provider.Capabilities().Bearer
	})

	return ok && bearer
}

// Name is the provider type the running instance registered under, and the key
// a pairing is stored under. It is empty for an instance that is not serving.
func (i *Identity) Name() string {
	name, _ := supervisor.Peek(i.instance, func(target any) string {
		if provider, ok := target.(authapi.IdentityProvider); ok {
			return provider.Name()
		}

		return ""
	})

	return name
}

// VerifyToken checks one token through the running provider.
func (i *Identity) VerifyToken(ctx context.Context, token string) (authapi.Identity, error) {
	return supervisor.Call(ctx, i.instance, verifyOp, func(ctx context.Context, target any) (authapi.Identity, error) {
		provider, ok := target.(authapi.IdentityProvider)
		if !ok {
			return authapi.Identity{}, fmt.Errorf("%w: %s built %T", ErrNotAnIdentity, i.instance.Ref(), target)
		}

		verifier, verifies := target.(authapi.TokenVerifier)
		if !verifies || !provider.Capabilities().Bearer {
			return authapi.Identity{}, fmt.Errorf("%w: %s", ErrNoBearer, i.instance.Ref())
		}

		return verifier.VerifyToken(ctx, token)
	})
}
