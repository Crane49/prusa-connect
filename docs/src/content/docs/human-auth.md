---
title: Human Authentication
description: How a person logs in to the core, and the sdk/authapi contract an identity provider implements.
audience: run
rewrite:
  describes: current
  implementedBy:
    - internal/app/resource
    - internal/platform/auth
    - sdk/authapi
---

This page is about authenticating **people**: the web UI, the REST API, the
event stream and MQTT. Authenticating a **printer** is a separate plane and the
two never mix.

There is no `admin` and `viewer` role model here. Permission is decided by
relation tuples over three enforcement surfaces, which is [the authorization graph](./internals/authorization.md).

## How a person logs in to the core

One deployment serves one identity provider. `core.auth.local` and
`core.auth.issuer` cannot both be set, and a configuration naming two is refused
at startup rather than resolved at runtime.

| You have | Set | Then |
|---|---|---|
| a company login | `core.auth.issuer`, `client_id`, `redirect_uri`, and `client_secret` for a confidential client | people log in through your OIDC provider, OAuth 2.1 authorization code with PKCE |
| no identity provider | `core.auth.local: true` | this server holds the accounts. See [Local accounts](./local-accounts.md) |
| neither, on a trusted network | `core.principal` | every request is served as that subject, and nobody logs in |

A session and an API token are rows in the server's database, so a restart
keeps everybody signed in. Only a sign-in that is still at the provider is held
in memory. A restart in the middle of one means pressing the sign-in button
again.

### Signing in at an OpenID Connect provider

The setup wizard offers this as **Sign in with your own provider**, on both
paths. It asks for four things and writes them as keys:

| The wizard asks | Key | Notes |
|---|---|---|
| Issuer address | `core.auth.issuer` | Read before the restart, see below. |
| Client ID | `core.auth.client_id` | |
| Client secret | `core.auth.client_secret` | Optional. Empty is a public client. |
| Redirect address | `core.auth.redirect_uri` | Empty uses this server's address and `/api/v1/auth/callback`. Register the same address at the provider. |
| Administrator's subject | `core.principal` | The administrator's `sub` claim at the provider. |

The wizard reads the issuer's discovery document before it restarts. It
refuses an issuer it cannot read, one whose document names another issuer, and
one that does not offer S256 PKCE.

**Public or confidential client.** Every login is an authorization code
exchange protected by PKCE. A *public* client stops there and sends only its
client id. A *confidential* client also proves itself with its client secret.
This server sends it as HTTP Basic authentication on the token request, as
RFC 6749 section 2.3.1 describes, and never in the form body. Set
`client_secret` exactly when the provider issued one:

| Provider | Client | Secret |
|---|---|---|
| Google | "Web application" OAuth client | required; Google refuses the code exchange without it, PKCE or not |
| Keycloak | "Client authentication" on (the default for a new client) | required |
| Keycloak | "Client authentication" off | none, a public client |
| Authentik | client type "Confidential" (the default) | required |
| Authentik | client type "Public" | none |

**Test the provider before you confirm.** The provider step has a **Test
the provider** button, which is `POST /api/v1/setup/oidc` behind the setup
code. It does not sign anyone in and saves nothing, and reports three steps:

| Step | What it does | What it proves |
|---|---|---|
| Discovery | reads `/.well-known/openid-configuration` | the issuer answers, names itself, and offers S256 PKCE |
| Client | sends the token endpoint a made-up authorization code, with the secret | `invalid_grant` means the client id and secret were taken, and `invalid_client` that they were not |
| Redirect | opens the authorization endpoint and follows no redirect | a 4xx or the provider's own error page means the client id or the redirect address is refused |

A token endpoint authenticates the client before it reads the code, so a made-up
code tells a taken secret from a refused one. Google sends a refused
authorization request to its error page at `/signin/oauth/error`. Any other
redirect, or a page, is a pass but not proof: a provider may show its refusal
on a page it answers 200 with.

A provider that answers in a way none of these reads, such as a 500, is
reported as unknown with what it said. The confirm still reads the discovery
document again, so an issuer that stopped answering is refused there.

The wizard writes the secret into `config.d/setup.yaml`, and writes that file
readable by its owner only (`0600`) whenever it holds a secret.

**Who administers the server.** No password is kept on this server, so the
first administrator grant is written for `core.principal`, and the wizard sets
it to the subject you type. A login is the subject the issuer names in `sub`,
so whoever signs in with that subject is the administrator. Find it in the
provider's admin console (Keycloak: the user's **ID**), or sign in once and
read the `subject` of the line `a person signed in at the identity provider` in
the server log. An issuer you add to `config.yaml` yourself needs
`core.principal` set the same way. Left at the default `operator`, nobody
administers the server, and start-up logs a warning that says so.

**Any subject works.** Type the subject in the wizard exactly as the provider
shows it. A user id on this server is lowercase letters, digits and dashes, at
most 63 characters. A subject that already has that form is the user id, as
Keycloak's UUIDs and Google's numbers are. Any other subject gets a user id
made from the issuer and the subject: `oidc-` and 32 characters of their
SHA-256 hash. The default subjects of authentik, Auth0 and Dex all get such an
id. A subject that starts with `oidc-` gets one too. The same person always
gets the same id. The server keeps the subject and the name behind each id it
made.

**Providers this server is checked against.** `make oidc-providers-check`
starts a provider in a container and runs the whole sign-in in a browser. It
covers the wizard, the restart, the administrator, a second person and the
sign-out. CI runs it at all three providers below. On your own machine it runs
one: Authelia on Linux, and Keycloak on macOS and Windows, where the core cannot
trust Authelia's certificate. `OIDC_PROVIDERS=all` runs all three, and
`OIDC_PROVIDERS=authentik` runs one by name.

| Provider | Version | What to set at the provider | ID tokens signed with |
|---|---|---|---|
| Keycloak | 26.8 | nothing beyond a client and its redirect address | RS256 |
| authentik | 2026.8 | nothing beyond a provider and an application | HS256, with no signing key |
| Authelia | 4.39 | a key under `jwks`, and the administrator's subject made ahead | ES256 |

**How an ID token is checked.** This server takes an ID token signed with any
algorithm the provider lists in its discovery document and this server can
check. These are RS256, RS384, RS512, PS256, PS384, PS512, ES256, ES384,
ES512, EdDSA, and HS256, HS384 and HS512. An HS algorithm is checked with the
client secret, as OpenID Connect Core section 10.1 says. That is what an
authentik provider with no signing key sends. So such a provider needs the
client secret set here too. The wizard's provider test fails at discovery
when no algorithm the provider lists can be checked, and says why. The server
log says it again at every start.

**What the page calls a person.** The page shows the name and the address
the provider sends, not the user id. It reads them from the ID token. When the
ID token carries no name or address, it reads them from the provider's
userinfo endpoint, which is where Authelia puts them. The name is the first
of the claims `name`, `preferred_username`, `username` and `email` that the
provider sends. Synology SSO sends `username` and no `preferred_username`. A
person signed in at the provider has no password on this server, so
**Settings > Account** offers no password change. The server keeps what the
provider said at the last sign-in. The sign-in button names the provider by
its host, as in **Sign in with sso.example.com**. Set
`core.auth.provider_name` to call it something else, such as
`Company login`.

**When a sign-in fails.** A sign-in can fail after the provider sends the
person back here, for example on an ID token this server cannot check. The
browser then returns to the sign-in page, which says what went wrong in one
sentence. The server log has the cause under `a sign-in at the identity
provider failed`.

Authelia makes a person's subject at their first sign-in. To name the
administrator in the wizard first, make it with
`authelia storage user identifiers add <username>`, which prints the subject.

For a provider that does not speak OpenID Connect, such as LDAP or SAML, write
an identity provider of your own; see the next section.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/auth/info` | which login this server serves, and whether it is set up |
| `GET` | `/api/v1/auth/login` | start a redirect login |
| `POST` | `/api/v1/auth/login` | log in with a password |
| `GET` | `/api/v1/auth/callback` | finish a redirect login |
| `POST` | `/api/v1/auth/logout` | end this session |
| `POST` | `/api/v1/auth/setup` | create the first administrator |
| `GET` | `/api/v1/principal` | the caller this server resolved |

Who a logged-in person may command is not a role. It is the authorization graph
in `internal/authz`, and a caller asks what it may do with
`POST /api/v1/access/check`. See
[the authorization vocabulary](./authorization-vocabulary.md).
## Writing an identity-provider plugin

An identity provider is an in-process adapter: a Go module that implements one
interface and registers itself, compiled into the binary. No gRPC and no
subprocess, which is what keeps it affordable on a Raspberry Pi.

Two ship. `local` is this server's own accounts, and `oidc` in
`adapters/auth/oidc` is the OpenID Connect provider. Read that adapter beside
this page: it is the worked example, and it passes the same published
conformance suite yours will.

This mirrors the [storage provider model](./storage.md#writing-a-storage-provider).

### The contract

The public, stable contract is the `authapi` package (`github.com/prusa3d/prusa-connect/sdk/authapi`), deliberately **not** under `internal/`, so a foreign module can import it.

```go
// sdk/authapi/provider.go
type IdentityProvider interface {
    Name() string
    Capabilities() Capabilities

    // Credential flow (username/password, basic-auth, API key, LDAP bind).
    VerifyCredentials(ctx context.Context, username, password string) (Identity, error)

    // Redirect flow (OIDC / OAuth2 / SAML). The host owns `state`.
    BeginLogin(ctx context.Context, redirectURI, state string) (authorizeURL, providerState string, err error)
    CompleteLogin(ctx context.Context, params url.Values, providerState string) (Identity, error)

    // Re-check an already authenticated subject (role sync / deprovisioning).
    RefreshIdentity(ctx context.Context, subject string) (Identity, error)
}

type Capabilities struct {
    Credentials  bool // VerifyCredentials is supported
    Redirect     bool // BeginLogin / CompleteLogin is supported
    Revalidation bool // RefreshIdentity is supported
}

type Identity struct {
    Subject     string   // STABLE provider-scoped id (OIDC "sub", SAML NameID, LDAP DN)
    Email       string
    DisplayName string
    Roles       []string // e.g. ["admin"] or ["viewer"]
    // UserID / ExpiresAt / TokenKind are filled by the host token validator, never by a provider.
}
```

A provider implements only the flow(s) it advertises in `Capabilities`. For a flow it does not support it returns `authapi.ErrUnsupportedFlow`. Sentinel errors a provider returns:

| Error | When |
|-------|------|
| `ErrInvalidCredentials` | Bad username/password, and, for enumeration resistance, also for an **unknown** user (same error). |
| `ErrUnsupportedFlow` | A method for a flow this provider does not advertise. |
| `ErrIdentityRevoked` | `RefreshIdentity`: the subject no longer exists / is disabled. |

### Roles a provider returns

`Identity.Roles` is what a provider reports, and it is not what the core
authorizes with. The core reads the relation graph in `internal/authz`, so a
provider cannot grant itself a permission by naming one.

### Why the redirect flow is split in two

`BeginLogin` and `CompleteLogin` are separate calls so the provider holds no
state across the redirect. The host generates and owns the CSRF `state`, so the
strength of that value never depends on a provider. The provider returns an
opaque `providerState` blob, the PKCE `code_verifier` and the OIDC `nonce` in
practice, which the host persists beside the state and replays verbatim to
`CompleteLogin`.

A restart between the two calls, or a second instance answering the callback,
therefore still completes the login.

`adapters/auth/oidc` is the worked implementation: OAuth 2.1 authorization code
with PKCE, `S256` only, and an ID token whose signature is checked against the
issuer's published keys before any claim in it is read. Read it rather than a
copy here, which would go stale.

### Example: a SAML provider

SAML uses the same `Redirect` capability, but the IdP returns the assertion via an **HTTP-POST** to the ACS URL rather than a `GET` with a code. The host's callback already accepts both `GET` and `POST`:

- In `BeginLogin`, build the IdP SSO redirect and put the host's `state` in **`RelayState`**; stash the SAML request id (for `InResponseTo` validation) in `providerState`.
- In `CompleteLogin`, read `params.Get("SAMLResponse")`, validate its **signature** and that its `InResponseTo` matches the id from `providerState`, then map attributes to an `Identity`.

```go
func (p *provider) Capabilities() authapi.Capabilities {
    return authapi.Capabilities{Redirect: true}
}

func (p *provider) BeginLogin(_ context.Context, acsURL, state string) (string, string, error) {
    reqID := "_" + randomURLSafe(20)
    redirect := p.buildAuthnRequestRedirect(acsURL, reqID, state /* → RelayState */)
    return redirect, reqID, nil // providerState = the request id
}

func (p *provider) CompleteLogin(_ context.Context, params url.Values, providerState string) (authapi.Identity, error) {
    assertion, err := p.validateSAMLResponse(params.Get("SAMLResponse"), providerState /* InResponseTo */)
    if err != nil {
        return authapi.Identity{}, authapi.ErrInvalidCredentials
    }
    return authapi.Identity{
        Subject: assertion.NameID, Email: assertion.Email,
        DisplayName: assertion.DisplayName, Roles: mapGroupsToRoles(assertion.Groups),
    }, nil
}
```

> **Note on the binding.** A cross-site SAML `POST` does not carry the `SameSite=Lax` pre-auth cookie, so the host cannot bind the SAML flow with the cookie. SAML's own `InResponseTo` (validated by you against `providerState`) is what binds the response to the request, so a SAML provider **must** validate it.

### Example: a credential provider (LDAP)

A non-redirect provider implements only `VerifyCredentials`:

```go
func (p *provider) Capabilities() authapi.Capabilities {
    return authapi.Capabilities{Credentials: true}
}

func (p *provider) VerifyCredentials(ctx context.Context, username, password string) (authapi.Identity, error) {
    entry, err := p.bind(ctx, username, password)
    if err != nil {
        return authapi.Identity{}, authapi.ErrInvalidCredentials // same error for unknown user
    }
    return authapi.Identity{
        Subject: entry.DN, Email: entry.Mail, DisplayName: entry.CN,
        Roles: mapGroupsToRoles(entry.MemberOf),
    }, nil
}
// BeginLogin / CompleteLogin / RefreshIdentity return authapi.ErrUnsupportedFlow.
```

### Keeping the host in sync (revalidation)

A web session, once issued, has the host's own lifetime. The host runs no loop that calls `RefreshIdentity`, so a change at the provider reaches this server at the person's next sign-in. A provider that can re-check a subject still advertises `Capabilities{Revalidation: true}` and implements `RefreshIdentity`, as `local` does.

### Wiring a provider into a build

A provider registers itself from an `init()`, the same way every other extension
does. `Register` takes the descriptor the published
[extension reference](./extensions.md) is generated from, so a provider with no
descriptor is refused rather than published as a blank row.

```go
//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { authapi.Register(Type, doc(), New) }
```

A provider that lives outside this repository is a sibling Go module depending
on `github.com/prusa3d/prusa-connect/sdk`. A distribution binary
blank-imports it, which runs its `init()`:

```go
import _ "github.com/example/prusa-connect-auth-acme"
```

A binary that never imports the provider does not have it, and naming an absent
type in configuration is refused at startup. That is what stops a proprietary
provider from becoming a silent requirement of the open build.

### Conformance tests

Every provider passes the same published suite, ours included, so the behaviours
the host relies on hold for all of them: an enumeration-resistant credential
refusal, `ErrUnsupportedFlow` for a flow the provider does not advertise, and
`ErrIdentityRevoked` for a subject that is gone.

```go
func TestIdentityProviderContract(t *testing.T) {
    authapitest.RunIdentityProviderContract(t, func(t *testing.T) authapitest.Harness {
        return authapitest.Harness{Provider: newMyProvider(t)}
    })
}
```

A redirect provider also sets `Harness.Callback`, because a login is bound to a
nonce and a PKCE verifier minted inside `BeginLogin` and cannot be replayed from
a fixed set. `adapters/auth/oidc/oidc_test.go` drives it through a test issuer.

### Confidentiality

`prusa-connect` is open-source (OCL). A proprietary provider (e.g. the real Prusa Account integration) lives in a **private** repository and is linked only into a private distribution build, it never enters this repo. The reference stub here demonstrates the wiring without any proprietary protocol details.
