---
title: "ADR-20261023-nvm: an ID token is checked with the algorithm the issuer advertises"
description: "Which JWS algorithms an OpenID Connect ID token may be signed with, which key checks each one, and what a person and an operator see when the check fails."
date: 2026-10-23
status: accepted
tags:
  - adr
  - auth
  - sdk
---
# ADR-20261023-nvm: an ID token is checked with the algorithm the issuer advertises

- status: accepted
- date: 2026-10-23

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

An OpenID Connect provider signs each ID token with a JWS algorithm and lists
the ones it uses in its discovery document. This server took RS256 only. An
authentik provider with no signing key signs HS256 with the client secret, and
an Authelia or Keycloak client can sign ES256. Each such sign-in failed after
the person had signed in at the provider, and nothing said why. This record
says which algorithms are taken, which key checks each one, and what a failure
shows.

## rules

1. An ID token is taken when its `alg` is in the provider's
   `id_token_signing_alg_values_supported` and this server can check it. A
   provider that lists nothing gets RS256, the default of OpenID Connect
   Discovery section 3.
2. This server checks RS256, RS384, RS512, PS256, PS384, PS512, ES256, ES384,
   ES512, EdDSA (also named Ed25519), HS256, HS384 and HS512. It never takes
   `none`, and it does not check Ed448.
3. An HS algorithm is checked with the client secret, as OpenID Connect Core
   section 10.1 says, and never with a key the provider publishes. Without a
   client secret no HS algorithm is taken.
4. Any other algorithm is checked with the published key the token's `kid`
   names, and only when that key's type fits the algorithm. An ES algorithm
   takes a key on its own curve only. A token with no `kid` takes the one
   published key that fits, and is refused when there are several.
5. An EC key whose point is not on its curve is skipped, as is any key that
   cannot be read. A key set with no key left is a failed fetch, and the keys
   held before stay.
6. When no algorithm the provider lists can be taken, the wizard's provider
   test fails at discovery and names the cause. A server running on such a
   provider logs the cause at error level at every start.
7. A sign-in that fails after a login this server started returns the browser
   to `/?sign_in_error=<reason>` with the session cookie expired. The reasons
   are `provider`, `id_token`, `subject`, `expired` and `failed`. The sign-in
   page shows one sentence for each, and the server logs the cause at warning
   level. A callback whose state this server did not start still answers 401.
8. `sdk/authapi/oidctoken` is the one place a JWS signature is checked.
   `Verify` takes a `Policy` and `Accepted` builds its list, and both the
   built-in sign-in and `adapters/auth/oidc` call it. The Prusa Account
   adapter keeps RS256 alone, because that issuer signs nothing else.

## enforcement

| rule | gate |
|---|---|
| 1 | `TestAcceptedIsWhatTheIssuerListsAndThisPackageChecks` in `sdk/authapi/oidctoken` |
| 1 | `TestAnAlgorithmTheIssuerDidNotAdvertiseIsRefused` in `internal/platform/auth/oidc` |
| 2 | `TestEveryAcceptedAlgorithmVerifies` |
| 3 | `TestAnHS256IDTokenIsCheckedWithTheClientSecret` in `internal/platform/auth/oidc` |
| 2, 3, 4 | `TestATokenOutsideThePolicyIsRefused` |
| 4 | `TestATokenWithNoKeyIDFindsTheOnlyKeyItsAlgorithmUses` |
| 5 | `TestAnECKeyOffItsCurveIsRefused` and `TestAnAnswerWithNoUsableKeyLeavesTheHeldOnesAlone` |
| 6 | `TestAProviderThatSignsWithTheSecretNeedsTheSecret` in `internal/app` |
| 7 | `TestAFailedSignInReturnsToTheSignInPageWithItsReason` in `internal/transport/http/httpapi` |
| 7 | `TestASignInFailureIsSortedByWhatFailed` in `internal/app` |
| 1 to 4 | `make oidc-providers-check`: Keycloak signs RS256, authentik HS256 and Authelia ES256 |

## rejected

| option | why not |
|---|---|
| take whatever `alg` the token names | a token naming `none`, or HS256 keyed with a public key, would pass |
| take every algorithm this server can check, whatever the provider lists | a token could pick a weaker algorithm than the provider uses |
| check HS256 against a published key | the published keys are public, so anybody could sign |
| refuse to start when no algorithm can be taken | the provider is somebody else's service, and its outage or change would stop the printer farm |
| show the cause itself on the sign-in page | the page is public, and the cause names keys and the provider's setup |
| a JOSE library | the standard library has every primitive, and a dependency grows the binary on small hardware |
