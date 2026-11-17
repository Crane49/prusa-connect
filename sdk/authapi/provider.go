// Package authapi is the contract for identity providers. A separate Go
// module implements IdentityProvider and registers itself from an init().
// Registration follows sdk/extension.
//
// The scope is human authentication: people using the web UI, API, MQTT or
// SSE. Printer authentication belongs to a connector.
//
// The host owns token issuance and validation. A provider only proves who the
// caller is and returns an Identity. It never sees the token store or the
// local user database.
package authapi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/internal/registration"
)

// Identity is the subject every provider returns. The host's token validator
// returns it too, with the token fields filled.
type Identity struct {
	// Subject is a stable, provider-scoped identifier (local: the user id;
	// OIDC: the "sub" claim).
	Subject string `json:"subject"`
	// Email is the user's email address (may be empty for some providers).
	Email string `json:"email"`
	// DisplayName is a human-friendly label for the UI.
	DisplayName string `json:"display_name"`
	// Roles is the set of granted roles, e.g. ["admin"] or ["viewer"].
	Roles []string `json:"roles"`

	// The host's token validator fills the following fields, never a login
	// flow. A TokenVerifier fills ExpiresAt.

	// UserID is the host-local user row id the token belongs to.
	UserID string `json:"user_id,omitempty"`
	// ExpiresAt is the token expiry as Unix seconds. 0 means no expiry.
	ExpiresAt int64 `json:"expires_at,omitempty"`
	// TokenKind is the validated token's kind (session | api | mqtt). The host
	// scopes a token to a transport with it. A provider never sets it.
	TokenKind string `json:"-"`
}

// HasRole reports whether the identity has the given role.
func (i Identity) HasRole(role string) bool {
	return slices.Contains(i.Roles, role)
}

// Capabilities says which authentication flows a provider supports.
type Capabilities struct {
	// Credentials is true when the provider supports VerifyCredentials
	// (username/password, basic-auth, API key).
	Credentials bool `json:"credentials"`
	// Redirect is true when the provider supports the BeginLogin/CompleteLogin
	// redirect flow (OIDC / OAuth / SAML).
	Redirect bool `json:"redirect"`
	// Revalidation is true when the provider supports RefreshIdentity. The
	// host then re-checks subjects periodically, updates their roles and
	// ends the sessions of a revoked subject.
	Revalidation bool `json:"revalidation"`
	// Bearer is true when the provider verifies a token its issuer signed,
	// which a caller presents on every request. Such a provider implements
	// TokenVerifier. The host maps the subject onto a local principal.
	Bearer bool `json:"bearer"`
}

// TokenVerifier is the optional interface a provider implements when it
// advertises Capabilities.Bearer.
type TokenVerifier interface {
	// VerifyToken checks a token the caller presented and returns the
	// subject and its claims, with ExpiresAt filled. On a refusal it wraps
	// one of the ErrToken errors of this package.
	VerifyToken(ctx context.Context, token string) (Identity, error)
}

// IdentityProvider authenticates a human and returns an Identity. A provider
// implements the flows it advertises in Capabilities. For any other flow it
// must return ErrUnsupportedFlow.
type IdentityProvider interface {
	// Name is the provider's unique identifier / type (e.g. "local", "oidc").
	Name() string
	// Capabilities reports which flows this provider supports.
	Capabilities() Capabilities

	// VerifyCredentials authenticates a username/password (or equivalent).
	// Returns ErrInvalidCredentials on a bad pair, ErrUnsupportedFlow if the
	// provider does not support credential auth.
	VerifyCredentials(ctx context.Context, username, password string) (Identity, error)

	// BeginLogin starts a redirect flow. The host generates the CSRF `state`.
	// The provider embeds it in the authorize request, as `state` for
	// OAuth/OIDC or `RelayState` for SAML. It returns the URL to send the
	// browser to and an opaque providerState, such as a PKCE code_verifier.
	// The host stores providerState and passes it unchanged to CompleteLogin.
	BeginLogin(ctx context.Context, redirectURI, state string) (authorizeURL, providerState string, err error)

	// CompleteLogin finishes a redirect flow from the provider callback. params
	// is the union of the callback's query and form values, so it serves an
	// OAuth/OIDC GET and a SAML HTTP-POST. providerState is the value
	// BeginLogin returned for this state.
	CompleteLogin(ctx context.Context, params url.Values, providerState string) (Identity, error)

	// RefreshIdentity re-checks an authenticated subject at the provider and
	// returns the current Identity. It returns ErrIdentityRevoked when the
	// subject is gone or disabled, and ErrUnsupportedFlow when the provider
	// does not advertise Revalidation.
	RefreshIdentity(ctx context.Context, subject string) (Identity, error)
}

// ProviderSpec describes one identity provider to build, from configuration.
// Config is an open map, because a provider's settings are its own.
type ProviderSpec struct {
	Type   string
	Config map[string]any
}

// ProviderFactory builds an IdentityProvider from its spec. A provider gets no
// host services: it never receives the local user store or the token store.
type ProviderFactory func(spec ProviderSpec) (IdentityProvider, error)

// Contract is the version of the identity provider contract this build serves. An
// implementation declares the version it compiled against and is refused at
// registration when the two are incompatible. A provider built against 1.0.0
// still registers.
const Contract = extension.Version("1.1.0")

// The errors a registration or a lookup reports. Callers inspect them with
// errors.Is. Most are the sdk/extension errors under another name.
var (
	// ErrDuplicateType reports one qualified name registered twice. The
	// second registration never replaces the first.
	ErrDuplicateType = extension.ErrDuplicate
	// ErrSealed reports a registration that arrived after the host built its
	// registry.
	ErrSealed = extension.ErrSealed
	// ErrUndocumented reports a registration missing a mandatory descriptor
	// field.
	ErrUndocumented = extension.ErrUndocumented
	// ErrAmbiguousType reports a bare name that two namespaces both answer to.
	ErrAmbiguousType = extension.ErrAmbiguous
	// ErrNoFactory reports a registration with nothing to build with.
	ErrNoFactory = errors.New("authapi: type has no factory")
)

// Registration is one registered type: its descriptor, and how an instance of
// it is built. The zero value is refused.
type Registration struct {
	descriptor extension.Descriptor
	factory    ProviderFactory
}

// Name returns the bare name, as an operator writes it in configuration.
func (r Registration) Name() string { return r.descriptor.Name() }

// Namespace returns the host-derived owner of the name.
func (r Registration) Namespace() extension.Namespace { return r.descriptor.Namespace() }

// Descriptor returns the identity and documentation this type registered.
func (r Registration) Descriptor() extension.Descriptor { return r.descriptor }

// Qualified renders "namespace/name", which is unique.
func (r Registration) Qualified() string { return r.descriptor.Qualified() }

// IsZero reports whether the registration was never declared.
func (r Registration) IsZero() bool { return r.factory == nil }

// Build makes one instance from its spec.
func (r Registration) Build(spec ProviderSpec) (IdentityProvider, error) {
	return registration.Build(
		registration.Target{Requested: spec.Type, Qualified: r.Qualified(), NoFactory: ErrNoFactory},
		r.factory, spec)
}

// Declarations is a gathering list of registrations. Registration happens in
// an init function, and the host drains the list once. The zero value is an
// empty list, ready to use, and safe for concurrent use.
type Declarations struct {
	set extension.Set[ProviderFactory]
}

// Add appends one declaration. It reports ErrSealed once the list has been
// drained and ErrDuplicateType for a qualified name already present.
func (d *Declarations) Add(r Registration) error {
	if r.IsZero() {
		return fmt.Errorf("%w: %s", ErrNoFactory, r.descriptor.Name())
	}

	return d.set.Add(extension.Entry[ProviderFactory]{Descriptor: r.descriptor, Value: r.factory})
}

// Drain seals the list and returns its registrations. Draining twice returns
// the same registrations.
func (d *Declarations) Drain() []Registration { return registrationsOf(d.set.Drain()) }

// Descriptors returns the documentation of everything declared, without sealing
// the list.
func (d *Declarations) Descriptors() []extension.Descriptor { return d.set.Descriptors() }

// Sealed reports whether the list has been drained.
func (d *Declarations) Sealed() bool { return d.set.Sealed() }

func registrationsOf(entries []extension.Entry[ProviderFactory]) []Registration {
	out := make([]Registration, 0, len(entries))
	for _, entry := range entries {
		out = append(out, Registration{descriptor: entry.Descriptor, factory: entry.Value})
	}

	return out
}

// declared is the gathering list init functions write to.
//
//nolint:gochecknoglobals // registration happens before main; there is nowhere else for it to go.
var declared Declarations

// Register declares a type in the gathering list NewRegistry drains. It panics
// on a rejected registration, so call it from an init function.
func Register(name string, doc extension.Doc, factory ProviderFactory) {
	if err := tryRegister(name, doc, factory); err != nil {
		panic(err.Error())
	}
}

// TryRegister declares a type in the gathering list, reporting a rejected
// registration instead of stopping the process.
func TryRegister(name string, doc extension.Doc, factory ProviderFactory) error {
	return tryRegister(name, doc, factory)
}

// Declare returns a registration without adding it to the gathering list, for a
// host that wires its own built-ins and for tests.
func Declare(name string, doc extension.Doc, factory ProviderFactory) (Registration, error) {
	return declare(name, doc, factory)
}

// Drain seals the package gathering list and returns its registrations. The
// host calls it once, after the blank imports have run.
func Drain() []Registration { return declared.Drain() }

// Descriptors returns what init functions declared, without sealing the list.
func Descriptors() []extension.Descriptor { return declared.Descriptors() }

// Sealed reports whether the package gathering list has been drained.
func Sealed() bool { return declared.Sealed() }

func declare(name string, doc extension.Doc, factory ProviderFactory) (Registration, error) {
	if factory == nil {
		return Registration{}, fmt.Errorf("%w: %s", ErrNoFactory, name)
	}

	descriptor, err := extension.Declare(extension.KindIdentity, Contract, name, doc)
	if err != nil {
		return Registration{}, err
	}

	return Registration{descriptor: descriptor, factory: factory}, nil
}

func tryRegister(name string, doc extension.Doc, factory ProviderFactory) error {
	registration, err := declare(name, doc, factory)
	if err != nil {
		return err
	}

	return declared.Add(registration)
}

// Registry is the type table one host owns. It is immutable once built, so it
// is safe to share without a lock.
type Registry struct {
	inner *extension.Registry[ProviderFactory]
}

// NewRegistry drains the declarations gathered by Register into a Registry. The
// host calls it once, after the blank imports have run.
func NewRegistry() (*Registry, error) { return NewRegistryFrom(Drain()...) }

// NewRegistryFrom builds a Registry from registrations declared explicitly, for
// a host that wires its own built-ins and for tests.
func NewRegistryFrom(registrations ...Registration) (*Registry, error) {
	entries := make([]extension.Entry[ProviderFactory], 0, len(registrations))

	for _, registration := range registrations {
		if registration.IsZero() {
			return nil, fmt.Errorf("%w: %s", ErrNoFactory, registration.Name())
		}

		entries = append(entries, extension.Entry[ProviderFactory]{
			Descriptor: registration.descriptor,
			Value:      registration.factory,
		})
	}

	inner, err := extension.NewRegistry(extension.KindIdentity, entries...)
	if err != nil {
		return nil, err
	}

	return &Registry{inner: inner}, nil
}

// Lookup resolves a name written in configuration. A qualified name matches
// exactly, a bare name when one namespace answers to it.
func (r *Registry) Lookup(name string) (Registration, error) {
	entry, err := r.inner.Lookup(name)
	if err != nil {
		return Registration{}, err
	}

	return Registration{descriptor: entry.Descriptor, factory: entry.Value}, nil
}

// Registered reports whether a name written in configuration resolves here.
func (r *Registry) Registered(name string) bool { return r.inner.Registered(name) }

// BuildProvider resolves the spec's type and makes one instance from it.
func (r *Registry) BuildProvider(spec ProviderSpec) (IdentityProvider, error) {
	found, err := r.Lookup(spec.Type)
	if err != nil {
		return nil, err
	}

	return found.Build(spec)
}

// Types returns every registered type, sorted by qualified name.
func (r *Registry) Types() []Registration { return registrationsOf(r.inner.Entries()) }

// Descriptors returns the documentation of every registered type.
func (r *Registry) Descriptors() []extension.Descriptor { return r.inner.Descriptors() }
