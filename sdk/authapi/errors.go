package authapi

import "errors"

var (
	// ErrUnknownProvider is returned by BuildProvider when no factory is
	// registered for the requested provider type.
	ErrUnknownProvider = errors.New("unknown identity provider type")
	// ErrInvalidCredentials is returned by VerifyCredentials for a bad
	// username/password pair (or equivalent).
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrUnsupportedFlow is returned when a provider is asked to perform a flow
	// it does not advertise in Capabilities.
	ErrUnsupportedFlow = errors.New("authentication flow not supported by provider")
	// ErrExpired is returned by the host token validator when a token exists
	// but has passed its expiry.
	ErrExpired = errors.New("token expired")
	// ErrIdentityRevoked is returned by RefreshIdentity when the subject is
	// gone or disabled at the provider. The host then ends that user's
	// sessions and tokens.
	ErrIdentityRevoked = errors.New("identity revoked at provider")
)

// The refusals a TokenVerifier reports. A verifier wraps the one that applies.
// The host inspects it with errors.Is and tells the caller which one happened.
var (
	// ErrTokenMalformed reports text that is not a token of the shape the
	// provider verifies.
	ErrTokenMalformed = errors.New("authapi: the token is malformed")
	// ErrTokenSignature reports a token whose signature does not verify against
	// any key the issuer publishes, or one signed with an algorithm the
	// provider does not accept.
	ErrTokenSignature = errors.New("authapi: the token signature does not verify")
	// ErrTokenExpired reports a token past its expiry, or one not yet valid.
	ErrTokenExpired = errors.New("authapi: the token has expired")
	// ErrTokenIssuer reports a token another issuer signed.
	ErrTokenIssuer = errors.New("authapi: the token names another issuer")
	// ErrTokenAudience reports a token issued for another audience.
	ErrTokenAudience = errors.New("authapi: the token was issued for another audience")
	// ErrTokenKeys reports that the issuer's signing keys could not be read, so
	// no signature can be checked. A retry after the issuer recovers can pass.
	ErrTokenKeys = errors.New("authapi: the issuer's signing keys are unavailable")
)
