---
title: "ADR-20260827-qvk: a published version is refused or it is not published"
description: "Which of the eleven versions this build publishes a consumer may act on, what moves each one, and the gate that refuses a set that moved while its version stood still."
date: 2026-08-27
status: accepted
tags:
  - adr
  - versioning
  - api-compatibility
  - sdk
---
# ADR-20260827-qvk: a published version is refused or it is not published

- status: accepted, rule 3 as a deprecation process
- date: 2026-08-27

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) · Enforced by `make vocab-check` · Reads with [ADR-20260824-gfr](20260824-gfr-the-extension-sdk-is-a-separate-module.md), [ADR-20260825-zkq](20260825-zkq-api-compatibility.md), [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md), [ADR-20260827-7jn](20260827-7jn-the-plugin-wire-is-a-specification.md)

Eleven kinds of version live in this build. Each one names a different consumer,
so eleven is the right count. Six are refused when a peer gets them wrong. One
is acted on by a person. Three are printed on a page, served by
`GET /api/v1/vocabularies`, and read by no code at all. A reader cannot tell the
two groups apart by looking, and that is what this record fixes.

`fa8f5cc3` is the cost. It took the authorization vocabulary from 20 published
entries to 22 and left the version at `2.0.0`. Every gate passed. The version is
written into the artefact from the Go constant, so regenerating agrees with
itself. Six other set changes did move the version, so the rule is understood
and nothing holds it.

## every version this build publishes

| version | who names it | what refuses a mismatch |
|---|---|---|
| the application, `VERSION` | an operator, by tag | nothing. It is the install unit, and rule 3 reads it |
| the HTTP API, `/api/v1` | a client, by path | a route that is not served, and `make api-check` |
| a vocabulary's content version | a client, a peer or a module naming the set it speaks | `make vocab-check`, rule 1 |
| a vocabulary's format version | a program parsing the artefact | `make vocab-check`, rule 2 |
| the eight port contracts | an author, in the descriptor | `ErrIncompatibleContract`, before the entry is built |
| the plugin wire major | a peer, at the handshake | `ErrProtocolMajor` |
| the plugin wire minor | a peer, at the handshake | the handshake takes the newest minor both implement |
| the event envelope `specversion` | a consumer, per event | `ErrInvalidEnvelope` |
| the authz database schema and ledger | a migration | `ErrSchemaTooNew` |
| the overlay and account file formats | a migration | `ErrConfigInvalid` |
| the version a registered entry declares | an operator reading the extensions reference | nothing yet. Rule 5 |

The `sdk` module carried no version at all. Rules 6 and 7 give it one.

## rules

1. **Move a vocabulary's content version with its set.** An entry added demands
   a minor. An entry retired demands a major. A version that goes backwards is
   refused. A change to prose, a summary or a class demands nothing, and a
   version that moves for one of those is accepted. `6490c4a7` and `53082906`
   are both correct.
2. **Move a vocabulary's format version with its shape.** A field added to a
   published entry demands a format minor. A field removed demands a format
   major. The format version is the artefact's shape and the content version is
   what is in it, so the two move for different reasons.
3. **Retire an entry from a published set through a deprecation, never in
   silence.** Below `1.0.0` a retirement is allowed on a major bump, because
   no release has published the set. At or above it, an entry leaves in two
   releases. The first marks it `deprecated` in the published set, names the
   release it goes in and the entry that replaces it, and says so in the
   release notes. The second removes it, on a major bump, with the removal in
   the release notes again. A client reads the deprecation off the set it
   already fetches, so nobody learns of a removal from a failure. That is
   [ADR-20260825-zkq](20260825-zkq-api-compatibility.md) rule 9 over the sets, with the
   process the end user is owed. The gate reads `VERSION` the way
   `tools/apicompat/freeze.go` does, and refuses a removal that no earlier
   release deprecated.
4. **Ship a vocabulary with a version and a baseline.** The alternative is a
   line in the debt list. `tools/vocabcompat/baseline.json` carries
   `unversioned`, which holds the three sets that publish an empty version
   today. Remove a name when that vocabulary gains its two constants. Never add
   one. A new vocabulary arrives with a version, a format and a baseline, which
   is [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule
   12.
5. **Let the host stamp a built-in entry's version.** An in-tree entry declares
   no `Doc.Version` of its own, and the registry writes the application's
   version onto it. A built-in ships with the binary, so nobody runs one version
   of `adapters/events/mqtt` against another version of the host. A number an
   operator cannot act on is a promise the page should not print. An out-of-tree
   entry keeps declaring its own, because its artefact travels on its own
   schedule.
6. **Cut both tags in a release.** `scripts/release.sh` writes `sdk/VERSION` and
   cuts `sdk/vX.Y.Z` beside `vX.Y.Z`. It cuts the sdk tag only when `sdk/` moved
   since the last one. Go resolves a module in a subdirectory by a tag carrying
   that subdirectory as a prefix, so the tag reads `sdk/v0.1.0`. This is
   [ADR-20260824-gfr](20260824-gfr-the-extension-sdk-is-a-separate-module.md) rule 4, which the
   one tag in this repository's history has never met.
7. **Keep the sdk below `v1` while `sdk/routesapi` exposes `gin.IRouter`.** A
   `v0` tag promises a resolvable version and nothing else. That is what an
   author needs in place of a pseudo-version over a commit hash. `sdk/v1` would
   pin gin into every extension author's module graph for the life of the major.
   A break after it renames every import line an author wrote. `sdk/go.mod`
   names the precondition and
   the old backlog
   carries the work.
8. **Define `CoreContract` over the published sets and the wire method set.** It
   is one semver. Its major moves when any of those retires or redefines an
   entry. Its minor moves when any of them adds one, which rule 1's baselines
   already compute per set. A module declares a range against it, and the
   catalogue filters on the range.
   Earlier research and
   earlier research
   both ask for that number. It waits on rule 4, because a contract over seven
   sets with three of them unversioned has three holes in it.

## the release floor and the deprecation window

### 1.0.0 is the release where the backlog is done

The backlog holds 628 unchecked rows, so the condition is countable rather than
a date. The freeze needs no switch: rule
3 and `tools/apicompat/freeze.go` both read `VERSION`. Every breaking change has
to land before that release, the device rename in
[ADR-20260826-5yj](20260826-5yj-a-device-is-the-object-and-a-printer-is-the-type.md) included.

### The deprecation window is three minor releases everywhere

The same number covers the HTTP API, the `sdk` module and every vocabulary, so
an author remembers one rule. [ADR-20260825-zkq](20260825-zkq-api-compatibility.md) rule 10
already sets it for the API. A deprecated sdk symbol and a retired vocabulary
entry get the same three releases. `extension.StabilityDeprecated` takes its
expiry from that.

## enforcement

| rule | gate | what it refuses |
|---|---|---|
| 1, 2 | `make vocab-check` | a set or a shape that moved further than its version did, and a version that went backwards |
| 3 | `make vocab-check` | an entry leaving a published set at or above the freeze floor, whatever the version says |
| 4 | `make vocab-check` | an artefact with no baseline, a baseline with no artefact, and an index row publishing no version off the debt list |
| 5 | none, owed | nothing, until the registry stamps a built-in's version |
| 6 | none, owed | nothing. One tag exists, so a gate would report green over an empty set |
| 7 | `make sdk-check` | a third-party import in `sdk/`, which is the condition rule 7 waits on |
| 8 | none, owed | nothing. `CoreContract` waits on rule 4 |

`tools/vocabcompat/compare_test.go` runs twelve mutations against the committed
artefacts, one per clause, red before the version moves and green after. The
first of them reconstructs `fa8f5cc3` out of the committed document.
`TestAFrozenReleaseRefusesARetirementAtAnyVersion` drives one retirement either
side of the floor. Rule 4's third clause lives beside three hundred other tests
in `internal/transport/http/httpapi`, so its recipe counts the cases it ran.

The gate reads the committed artefacts rather than the Go tables. Every
vocabulary already has a generator gate refusing an artefact that disagrees with
its table, in both directions, so the artefact is the set. A second reader of
the tables would be a second thing to keep in step. `tools/apicompat` reads the
committed OpenAPI document for the same reason.

## rejected

| option | why not |
|---|---|
| one version for every published surface | a client pinning a path would pin the storage engine too. ADR-20260825-zkq rejects it for the API |
| dropping the vocabulary versions instead of gating them | a peer and a module both need to say which set they speak, which is ADR-20260825-bhe rule 3 |
| a stability switch a person edits per vocabulary | a rule remembered on exactly one day is the failure this gate exists to stop |
| adopting oasdiff for the vocabularies | a vocabulary is a closed set in a stable order, so the classifier is a set difference |
| gating the bump only once `VERSION` reads `1.0.0` | the version is published today, and `fa8f5cc3` shipped at `0.1.0` |
| guessing which field identifies an entry | an identity guessed wrong reports every entry as retired and added at once |
| tagging `sdk/v1` now | it would freeze `gin.IRouter` into every author's module graph until `sdk/v2` |
| waiting for the repository split before tagging `sdk/` | a Go submodule tags with a path prefix from this repository today |
| leaving `Doc.Version` to the author of a built-in | every in-tree entry reads `1.0.0`, and `adapters/events/mqtt` is four commits past its literal |
| a version negotiated per vocabulary at the handshake | seven negotiations per peer, and a module author tracking seven numbers |

### Why a set change is a minor and not a major

A value entering a closed set breaks a client that switches exhaustively over
it. ADR-20260825-zkq rule 4 calls exactly that breaking for a response enum. One thing
separates the two cases. A vocabulary is served at its own endpoint, and a
client reads the set at run time. The strict consumer ADR-20260825-zkq protects is the
generated type rather than the vocabulary reader. Where a vocabulary is spelled
into a request enum, `make api-check` classifies it under ADR-20260825-zkq and this gate
does not overrule that. `/api/v1/resources/{kind}` and
`/api/v1/grants/{object_type}/{object_id}` are the two paths that do it. A
retirement is the case both records call breaking, and rule 3 is where they
meet.

## open items

- Rule 5 has no gate until the registry stamps a built-in's version. The
  extensions reference prints a number an operator cannot act on
- `Emission.Version` and `Destination.Version` are declared beside `Doc.Version`
  and read by nothing either. An emission naming a core event type could be held
  to that vocabulary's own version, which rule 1 now tracks
- Error codes, extensions and capability verbs carry no content version and no
  format version, so rule 4's debt list has three names in it. Capability verbs
  have no artefact under `docs/vocabularies` at all, so the gate cannot reach
  them even once they gain the constants
- An event type and a device-issue reason reach the OpenAPI document only inside
  an `example` or a `description`, which ADR-20260825-zkq rule 6 calls neutral. Rule 1
  is the only thing that sees a change to either
- `scripts/release.sh` runs no gate of this record before it tags, and
  [changelog-discipline](../../.agents/rules/changelog-discipline.md) records
  that it leaves the filing of the news to `scripts/cut-release-changelog.sh`
- The event envelope carries `specversion` for CloudEvents, and this project's
  own envelope extensions carry no version beside it. A peer reading an
  extension member it does not know has the handshake problem again
