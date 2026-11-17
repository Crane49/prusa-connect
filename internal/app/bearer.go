package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/extension/supervised"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
)

// errNoBearerProvider is the cause behind refusing a bearer token on a
// deployment with no identity instance that verifies one. It reaches a log
// line and never a client.
var errNoBearerProvider = errors.New("app: no running identity instance verifies a bearer token")

// bearerProviders is auth.BearerVerifier over the identity instances the
// supervision plane runs. It reads the plane on every token and not once at
// startup. So an instance that restarts under supervision, or one an operator
// declares while the server runs, verifies the next token.
//
// A declared identity instance is consulted here for every bearer token, so
// the status the plane reports for the auth kind is true. The login flows
// belong to core.auth.
type bearerProviders struct {
	host instanceReader
}

var _ auth.BearerVerifier = bearerProviders{}

// VerifyToken checks a token against every running identity instance that
// advertises the bearer flow, in name order, and returns the first identity
// one of them vouches for together with that provider's type.
//
// When two providers both refuse, the refusal reported is the one that says
// most: a provider that answers "not my issuer" is describing itself, and the
// other provider's answer is the one about the token.
func (b bearerProviders) VerifyToken(ctx context.Context, token string) (authapi.Identity, string, error) {
	running := b.host.Supervising(ref.KindAuth)

	names := make([]ref.Ref, 0, len(running))
	for instance := range running {
		names = append(names, instance)
	}

	slices.SortFunc(names, func(a, c ref.Ref) int { return strings.Compare(a.String(), c.String()) })

	var refusal error

	for _, name := range names {
		identity, err := supervised.AttachIdentity(running[name])
		if err != nil || !identity.Bearer() {
			continue
		}

		found, verifyErr := identity.VerifyToken(ctx, token)
		if verifyErr == nil {
			return found, identity.Name(), nil
		}

		if refusal == nil || errors.Is(refusal, authapi.ErrTokenIssuer) {
			refusal = fmt.Errorf("%s: %w", name, verifyErr)
		}
	}

	if refusal == nil {
		refusal = errNoBearerProvider
	}

	return authapi.Identity{}, "", refusal
}

// announceBearerProviders writes one line per declared identity instance at
// startup. The line says whether the instance verifies a bearer token or is
// started and consulted by nothing. An operator who declared core/oidc under
// resources then reads that the login still comes from core.auth.
func announceBearerProviders(ctx context.Context, host instanceReader, logger *slog.Logger) {
	for name, instance := range host.Supervising(ref.KindAuth) {
		identity, err := supervised.AttachIdentity(instance)
		if err != nil {
			continue
		}

		if identity.Bearer() {
			logger.InfoContext(ctx, "an identity instance verifies bearer tokens",
				"instance", name.String(), "provider", identity.Name(),
				"pairing", "POST /api/v1/auth/pairings")

			continue
		}

		logger.WarnContext(ctx, "an identity instance verifies no bearer token and is consulted by nothing; "+
			"a login flow is configured under core.auth", "instance", name.String())
	}
}
