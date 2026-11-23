---
title: "ADR-20261112-nay: an app pairing credential also travels in a JWT shape"
description: "A collected app pairing answers the credential bare and in the shape of a JWT, the server reads that shape in the Authorization header, and the app opens a short page session in an HttpOnly cookie for the pages it shows, which never receive the credential."
date: 2026-11-12
status: proposed
supersedes: []
amends: [ADR-20261025-tpp]
tags:
  - adr
  - auth
---
# ADR-20261112-nay: an app pairing credential also travels in a JWT shape

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20261025-tpp](20261025-tpp-a-credential-carries-scopes-and-an-app-asks-a-person-for-one.md), [ADR-20261112-2eb](20261112-2eb-prusaslicer-3-reaches-this-server-through-a-compatibility.md)

PrusaSlicer 3 in offline mode signs in to this server as an app. It runs the
app pairing of ADR-20261025-tpp: it asks for a code, the person approves it in
the browser, and the slicer collects a credential. The credential lives ninety
days. The slicer sends it as `Authorization: Bearer` on every request, bare or
in the shape of a JWT. A JWT is three base64url segments separated by dots: a
header, a payload of claims and a signature.

The page the slicer shows in its Connect tab must never receive the
credential. Module bundles run in that page, and a credential any script on
the page can reach is a credential any of them, or any cross-site scripting
fault, can send elsewhere. That holds for a cookie a script writes, for the
data of a window event, and for an argument a script can catch by wrapping
`_prusaConnect_v2.init`. So the slicer itself opens a page session, a short
api credential of its own, and puts its HttpOnly cookie into the web view.
The page only ever holds that cookie, which no script reads.

## rules

### the JWT shape

1. A collection at `POST /api/v1/auth/app-pairings/token` that answers the
   credential carries `access_token` beside `token`. `token` stays as
   ADR-20261025-tpp rule 21 says. `access_token` is the same credential in
   the JWT shape. This rule amends ADR-20261025-tpp rule 21.
2. The JWT shape has three segments. The header is
   `{"alg":"none","kid":"pcl","typ":"JWT"}`. The payload carries `sub`, `iat`,
   `exp`, `iss` and `jti`: the subject, the issue time and the expiry in
   seconds since the Unix epoch, `prusa-connect`, and the credential's
   id. The third segment is the `pcl_` token. `exp` is ninety days after the
   collection, the life of an app pairing credential.
3. The server reads the shaped token in `Authorization: Bearer` only. It
   reads only the third segment, and checks it as it checks the bare token. A
   revoked, expired or fenced credential is refused in either shape.
4. The server refuses a token of this shape when its `jti`, `sub` or `exp`
   differs from the row the third segment names. It refuses one whose row is
   not an app pairing credential. A session, a credential a person minted and
   a page session are refused in this shape.
5. The shape is not signed. Only this server checks the token, against its
   row.

### the page session

6. `POST /api/v1/auth/app-pairings/session` takes an app pairing credential
   in `Authorization: Bearer`, bare or in the JWT shape, and no body. The app
   calls it, never a page. It sets the session cookie to a page session, and
   answers `expires_at` and the same cookie as `cookie`: `name`, `value`,
   `path`, `expires`, `http_only`, `same_site` and `secure`. An app puts that
   cookie into its web view's cookie store without parsing `Set-Cookie`.
7. A page session is an api credential. It speaks for the person of the app
   pairing credential, with its scopes, at its generations, and names it as
   its parent. It lives one hour, and never past the parent's expiry. It is
   not a sign-in as a person, so it mints nothing and decides no pairing.
8. The cookie is HttpOnly and SameSite=Strict, with the path `/`. It is
   Secure unless the server is reached over plain HTTP, as the session cookie
   is.
9. The verifier reads the parent on every request. A page session whose
   parent is revoked, expired or fenced is refused at once.
10. The route reads only the Authorization header. A request that carries
    the credential in a cookie, or carries any other kind of credential, is
    refused with `PAGE_SESSION_REFUSED`. A page on another site cannot set the
    header without a CORS check this server answers only for its allowed
    origins, so it cannot open a page session.
11. One app pairing credential opens at most thirty page sessions from one
    address in ten minutes. One more is refused with `PAGE_SESSION_LIMITED`.
    The count is taken only once the credential is known to be a live app
    pairing credential, so another token or rubbish spends nothing.
12. One app pairing credential keeps at most four live page sessions. Opening
    another deletes its expired ones and its oldest live ones past four.
    Revoking the credential deletes every page session it opened. The server
    deletes expired credentials when it starts and every hour.
13. The listing of credentials never shows a page session.
14. The server never reads a credential it minted from the cookie
    `auth.access_token`, bare or shaped. That cookie carries only a Prusa
    Account token.
15. The page in the slicer never receives the credential. It drops
    `accessToken` and `sessionId` from `init` before any event, ignores
    `setAccessToken`, and stores, logs and sends on no token. It relies on the
    cookie the slicer set.
16. The page asks the slicer with `REQUEST_CONFIG` 45 minutes after the core
    last accepted its page session, and after a 401 while a person is signed
    in. A 401 with nobody signed in asks nothing: the slicer may still be
    pairing. The slicer opens a new page session and calls `init`, and the
    page asks the core again. One `REQUEST_CONFIG` and one check are in
    flight at a time, and a `REQUEST_CONFIG` the slicer has not answered in
    30 seconds no longer stops the next one.

## enforcement

| rule | gate |
|---|---|
| 1, 2 | `TestACollectedCredentialAlsoComesInAJWTShape` in `internal/app`, over the shipping composition |
| 2 to 4 | `TestAnAppPairingCredentialIsReadInItsJWTShape` in `internal/platform/auth` |
| 3, 14 | `TestAnAppPairingCredentialInItsJWTShapeIsCheckedByTheVerifier` in `internal/platform/auth/session` |
| 6 to 8, 10, 11, 13 | `TestAPageSessionStandsInForTheAppsCredential` in `internal/app` |
| 6 | `TestAPageSessionAnswersTheCookieItSets` in `internal/app` |
| 7, 9 | `TestAPageSessionStandsInForItsAppPairingCredential` and `TestAPageSessionEndsWithItsParent` in `internal/platform/auth` |
| 12 | `TestAParentKeepsAFewPageSessionsAndTakesThemWithIt` in `internal/platform/auth` |
| 12 | `TestTheCredentialsAreSweptWhileTheServerRuns` in `internal/app` |
| 9, 12 | `TestAPageSessionKeepsItsParentInTheTable` in `internal/platform/auth/dbstore`, on both engines |
| 15, 16 | the bridge tests in `frontend/src/lib/slicer/bridge.svelte.test.ts` |

## rejected

### the page keeps the credential in a cookie it writes

A cookie a page script writes cannot be HttpOnly, so every script on the page
reads it. Module bundles run in the page, so one bundle, or one cross-site
scripting fault, could send a ninety-day credential with `operate` elsewhere.
The page session is in an HttpOnly cookie, and a stolen page session cookie
ends within the hour and with the app's credential.

### the page trades the credential for the page session

The page would need the credential for the trade, so it would pass through
`init`, a window event or a variable. A module can wrap `init` before the
bridge reads it, or ask for `REQUEST_CONFIG` itself and catch the answer. The
app opens the page session instead, and the page never sees the credential.

### a cookie limited to a path

A path limits which requests carry the cookie. It does not stop a script on
a page under that path from reading it, and the Connect tab and the module
pages share one origin and one path tree.

### a shorter credential alone

A shorter app pairing credential makes the person pair the slicer again more
often, and the credential in a readable cookie can still be sent elsewhere
while it lives. The page session is short while the credential the slicer
holds stays long.

### other options

| option | why not |
|---|---|
| answer only the JWT shape | a client written to ADR-20261025-tpp reads `token`, and it keeps working |
| a signed JWT | the server checks the row for revocation in any case. A key would add a secret and its rotation, and no check |
| a page session of the kind session | a session is a sign-in as a person: it mints credentials and decides pairings, which the app's credential cannot |
| an OAuth authorization server in the core for the slicer | the app pairing already gives an app a credential a person approved. ADR-20261025-tpp rejects the server |

## open items

- `verification_uri_complete` is built from the Host header of the request,
  as ADR-20261025-tpp names in its open items. The slicer builds the address
  of `/pair` from the server address the person typed instead.
- A build older than this record does not read `parent`. Started on the same
  database, it accepts a page session until its hour ends, even after its
  parent was revoked.
