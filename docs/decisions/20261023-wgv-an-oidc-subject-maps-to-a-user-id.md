---
title: "ADR-20261023-wgv: an OIDC subject maps to a user id"
description: "How a person who signs in at an OpenID Connect provider gets a user id in the authorization graph when the provider's sub claim does not fit the id grammar."
date: 2026-10-23
status: accepted
tags:
  - adr
  - auth
  - authz
---
# ADR-20261023-wgv: an OIDC subject maps to a user id

- status: accepted
- date: 2026-10-23

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

A person who signs in at an OpenID Connect provider is named by the
provider's `sub` claim. The authorization graph names a user by an id of
lowercase letters, digits and dashes, at most 63 characters. Many providers
hand out a `sub` outside that grammar: authentik's default is a 64-character
hex hash, Auth0 writes `auth0|...`, and Dex writes long mixed-case base64.
Such a person signed in at the provider and was then refused here. This
record says which user id a subject gets.

## rules

1. A subject that is a valid user id, and does not start with `oidc-`, is the
   user id unchanged.
2. Any other subject gets the user id `oidc-` followed by the first 32
   characters of the lowercase, unpadded base32 text of
   `SHA-256(issuer + NUL + subject)`. That is 37 characters and 160 bits of
   the hash.
3. The issuer in rule 2 is `core.auth.issuer` without a trailing slash. A
   subject is unique only at one issuer, so another issuer gives another id.
4. A subject that starts with `oidc-` always takes rule 2, even when it fits
   the grammar. So a subject a person can choose at a provider, such as a
   username, can never be kept as the id another person's subject derives to.
5. A subject is refused when it is empty, longer than 255 bytes, or holds a
   space or a control character. OpenID Connect Core section 2 allows 255
   ASCII characters.
6. `core.principal`, with `core.auth.issuer` set, is the administrator's
   subject as the provider spells it. The first administrator grant is
   written for the id rules 1 to 4 give it, which is the id the administrator
   signs in as. Without an issuer it is the user id itself, as before.
7. Every sign-in writes a row in `auth_oidc_subject`: the id, the issuer, the
   subject, the name and the address the provider sent, and the first and last
   time. A login never reads it, because the id is derived each time. It is
   how a person who sees `oidc-...` in a grant finds who it is.
8. The derivation never changes. A change would move every derived person's
   grants to an id nobody holds.

## enforcement

| rule | gate |
|---|---|
| 1 | `TestPrincipalIDKeepsASubjectThatIsAlreadyAnID` in `internal/platform/auth/oidc` |
| 2 | `TestPrincipalIDDerivesAnIDForASubjectTheGraphCannotName` |
| 3 | `TestPrincipalIDTiesTheIDToTheIssuer` |
| 4 | `TestPrincipalIDNeverKeepsASubjectThatLooksDerived` |
| 5 | `TestCheckSubject` |
| 6 | `TestASubjectTheGraphCannotNameAdministersTheOrgUnderItsDerivedID` in `internal/app` |
| 6 | `TestAWizardTakesASubjectTheGraphCannotNameAsItIs` in `internal/app` |
| 7 | `TestASignInIsRecordedUnderTheDerivedID` in `internal/platform/auth/dbstore` |
| 8 | `TestPrincipalIDIsStable`, against values Python's `hashlib` computed |
| 2, 6 | `make oidc-providers-check` signs in at authentik with its default subject and expects the derived id |

## rejected

| option | why not |
|---|---|
| derive an id for every subject | every person who already signed in with a UUID subject would get a new id and lose their grants |
| widen the id grammar to take any subject | every resource id uses it, and an id is also a URL path segment, a config key and an MQTT topic level |
| keep a fitting subject as it is with no reserved prefix | a person who chooses their own subject could pick another person's derived id |
| a random id stored at the first sign-in | the first administrator is named before anybody signs in, so the id must be known from the subject alone |
| the hash without the issuer | two issuers may hand out the same subject to two different people |
| the full 52 base32 characters of the hash | 160 bits are already far beyond a collision anybody can search for, and a shorter id is easier to read in a log |
| ask the operator to change the provider's subject mode | authentik's other modes move when a name or address changes. Auth0 and Dex have none |

## open items

- ADR-20261016-pmj rule 2 lists the tables of `internal/platform/auth/dbstore` as
  accounts, credentials and pairings. `auth_oidc_subject` is the fourth.
- A subject that starts with `oidc-` and fitted the grammar was its own id
  before this record. Such a person now gets a derived id and must be granted
  again. No provider this server is checked against hands out such a subject.
