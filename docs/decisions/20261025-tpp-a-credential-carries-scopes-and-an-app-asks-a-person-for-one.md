---
title: "ADR-20261025-tpp: a credential carries scopes and an app asks a person for one"
description: "Every api credential is limited by scopes to a part of what its principal holds, the authorization graph applies the limit on every transport, and an app gets a scoped credential through a code a person approves in the browser."
date: 2026-10-25
status: proposed
amends: [ADR-20260916-b3x]
tags:
  - adr
  - auth
  - authz
  - security
---
# ADR-20261025-tpp: a credential carries scopes and an app asks a person for one

- status: proposed
- supersedes in part [ADR-20260916-b3x](20260916-b3x-instances-announce-themselves-on-the-network.md)
  rules 20 and 21. A pairing route exists now, and a pasted credential still
  works
- date: 2026-10-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md), [ADR-20260916-b3x](20260916-b3x-instances-announce-themselves-on-the-network.md)

An api credential is a bearer token a person mints and pastes into a script
or an app. Until now it carried every grant of its principal. A dashboard
that only reads temperatures held the same reach as its owner, and that owner
is often the administrator. There was also no way for an app to get a token
unless a person copied it from one screen into another.

This record says what a scope is, where it is enforced, what the existing
tokens become, and how an app asks a person for a credential. The flow is
the device authorization grant of RFC 8628. An app shows a short code, a
person confirms it in the browser, and the app collects a token.

## rules

### what a scope is

1. Every api credential carries one or more scopes from a closed set:
   `read`, `operate` and `administer`. The set is the core's, in
   `internal/authz/ceiling.go`, and no extension adds to it.
2. Each scope allows a part of the closed action set. Each one allows what the
   one before it allows.

   | scope | allows |
   |---|---|
   | `read` | `observe`, `view_audit`, `view_resource`, `read_storage` |
   | `operate` | what `read` allows, and `operate`, `operate_resource`, `write_storage` |
   | `administer` | every action, the dangerous tier included |

3. A credential never reaches more than its principal. A decision is what the
   graph confers and what the scopes allow, both at once. A scope only removes.
4. A session has no scope, and neither has a Prusa Account token that a
   pairing names a principal with. Both are limited by the graph alone.

### where it is enforced

5. The authorization graph applies the scopes, because every transport already
   asks it. The scopes ride on the request context. Every point check, list
   and count tests a mask reduced to what they allow. An action they leave out
   matches no row. A listing names on each
   row only the actions they allow.
6. Writing or reading a grant, a verb grant or an org role, reading what a
   subject holds, accepting a declaration change and reaching the dangerous
   tier all take `administer`.
7. The HTTP surface puts the scopes on the context once, where it resolves the
   credential: the middleware of every operation, the module mount and the
   readiness report.
8. An event stream asks again under the context it was opened with, so a
   stream that outlives its request keeps the scopes.
9. While a person's request to a module is open, a call the module makes in
   that person's name is held to the narrowest scopes of their open requests.
   A session open beside a limited request does not widen it.
10. A credential whose scopes stop short of `administer` may revoke itself and
    no other credential.

### minting and the tokens that already exist

11. `POST /api/v1/auth/credentials` takes `scopes`. A mint that names none
    gets `read`. Only a sign-in mints, as before.
12. A listing shows each credential's `scopes` and its `origin`: `mint` or
    `app_pairing`. `GET /api/v1/auth/credentials?origin=app_pairing` lists the
    paired apps.
13. A credential minted before this record is limited to `read`. A migration
    writes that on every existing row.
14. An api credential row with no scope is refused when it is presented, and
    one with a scope this build does not know is refused too.
15. The schema floor is 9, the version of that migration. A build older than
    this record cannot open the database, so it cannot read a row without its
    scopes.

### how an app asks

16. `POST /api/v1/auth/app-pairings` takes no credential. The body names the
    app and the scopes it asks for. The answer is the one RFC 8628 section 3.2
    names: `device_code`, `user_code`, `verification_uri`,
    `verification_uri_complete`, `expires_in` and `interval`. The last two are
    seconds, as the RFC names them, and ADR-20260826-c77 rule 10 makes an exception
    for them.
17. The verification page is `/pair` on the origin the app reached. The page
    shows the app's name, the scopes and the address the request came from.
    It says that nothing checks the name.
18. A person reads, approves or denies a pairing at
    `/api/v1/auth/app-pairings/{user_code}`. Each takes a sign-in as a person.
    An api credential cannot approve one, because approving hands out access.
19. An approval names the scopes the app asked for or fewer. Naming none
    approves what it asked for. Naming more is refused.
20. The app polls `POST /api/v1/auth/app-pairings/token`. It is told
    `APP_PAIRING_PENDING` until a person decides. A poll sooner than the
    interval is told `APP_PAIRING_SLOW_DOWN`, and the interval grows by five
    seconds. A denial is told once with `APP_PAIRING_DENIED`.
21. After an approval the next poll collects the credential, once. It speaks
    for the person who approved, with the scopes they approved, its origin is
    `app_pairing`, and it lives ninety days.
22. The credential is fenced at the generations read before the person's
    sign-in was checked. A password change between the approval and the
    collection leaves it refused.
23. A code works once. The row is deleted when the app collects it, and every
    write names the state it expects, so two polls or two replicas cannot
    collect one credential.

### the bounds

24. A pairing waits ten minutes. At most 32 wait at once over the whole
    server, and one address may ask five times in ten minutes.
25. A user code is eight consonants with no vowel, shown with a dash in the
    middle. A person types it in any case, with or without the dash.
26. The table `auth_app_pairing` keeps the SHA-256 of the device code and
    never the code.
27. Each step writes one audit line at info with `audit=true`: the request,
    the approval or the denial, the collection, and a pairing that ended with
    no credential.

## enforcement

| rule | gate |
|---|---|
| 2, 3, 5 | `TestAScopeCutsEveryDeviceDecisionDownToWhatItAllows` in `internal/authz` |
| 2, 3, 5 | `TestAScopeCutsEveryResourceDecisionDownToWhatItAllows` |
| 6 | `TestAScopeBelowAdministerAdministersNothing` |
| 1, 14 | `TestScopesAreReadStrictly` |
| 7 | `TestTheScopesOfACredentialReachTheModuleGateAndTheModule` in `httpapi` |
| 8 | `TestAReauthorizationCarriesWhatTheFirstQuestionCarried` in `internal/transport/stream` |
| 9 | `TestAModuleCallIsHeldToTheScopesOfTheRequestItServes` in `internal/app/resourcehost` |
| 10 | `TestAScopedCredentialRevokesItselfAndNoOther` |
| 11 | `TestAMintNamesItsScopesAndNoneIsRead` |
| 13, 15 | the migration list and the floor in `internal/app/hostschema` |
| 16 to 23 | `AppPairingContract` in `internal/platform/auth/authtest`, run on the memory store and on both engines |
| 3, 18 to 21 | `TestAnAppPairedByAPersonGetsATokenWithOnlyTheApprovedScopes` in `internal/app`, over the shipping composition |
| 18, 20 | `TestADeniedPairingIsToldOnceAndATokenCannotDecideOne` |
| 24 | `TestOneAddressAsksForAFewPairingsAndIsThenRefused`, and the bound of 32 in the contract |
| 17 to 19 | the end-to-end journey `app-pairing.spec.ts`, which approves a pairing in the browser and uses the token |

## consequences

- every token a person minted before this record can only read until they
  mint a new one
- a client that drives printers names `operate` when it asks, and a client
  that manages the server names `administer`
- a new table and two new columns in the host's database, and a schema floor
  an older build cannot pass

## rejected

| option | why not |
|---|---|
| a service account for each paired app | no API creates one, and each grant would be written separately. A scope gives the same bound |
| a scope as any list of actions | nobody approving an app can judge fourteen actions. A screen can explain three steps |
| existing tokens keep their full reach | a forgotten token would keep the administrator's reach. `read` is the safest default |
| revoke every existing token | it breaks every script, and `read` already removes the risk |
| scopes checked in each handler | a handler that forgets is a hole. The graph is the one place every transport asks |
| read means safe HTTP methods | a module route and a command are both POST, and the action is what the graph knows |
| pairings kept in memory | two replicas behind one address would each know half of the pairings |
| this server as an OAuth authorization server | RFC 8628 is the one flow an app needs, and the rest is code nobody asked for |
| the first poll held to the interval | an app that polls the moment it has its codes has done nothing wrong yet |

## open items

- A module that acts in a person's name after their request has closed, as a
  queue does at night, is not held to the scopes that request carried.
- A Prusa Account token carries no scope. A pairing could name one.
- The verification page is built from the Host header the app sent. Behind a
  proxy that rewrites it, the app shows an address the person cannot open.
