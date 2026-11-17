---
title: "ADR-20260825-zkq: HTTP API compatibility"
description: "What a client may rely on across versions of the HTTP API, which changes are breaking, how a client pins a version, and what the gate refuses."
date: 2026-08-25
status: accepted
tags:
  - adr
---
# ADR-20260825-zkq: HTTP API compatibility

- status: accepted
- date: 2026-08-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) · Enforced by `make api-check` · Vocabulary: [ADR-20260825-dee](20260825-dee-the-capability-vocabulary.md) · Layout: [ADR-20260824-kkr](20260824-kkr-repository-layout-and-package-boundaries.md)

## rules

1. Version the HTTP API by the path segment after `/api`. `v1` is the only version this build serves.
2. A client pins a version by using its routes and by nothing else. There is no version header, no dated version and no per-account default: a request's path is the whole of its pin.
3. Classify every difference between the surface a release published and the surface a build serves as **breaking**, **additive**, **neutral** or **unknown**. The table in `tools/apicompat/classify.go` is the definition and this document is its summary.
4. Treat a change as breaking when a client compiled against the published surface would need a code change to keep working. The set is:
   - a path or operation removed
   - an `operationId` changed or removed
   - a request parameter removed, added as required, or made required
   - a request body removed
   - a response status code removed
   - a property removed from a schema
   - a response property leaving `required`
   - a request property entering `required`
   - a value leaving a request enum
   - a value entering a response enum
   - a type, format, `$ref`, `items` or `additionalProperties` changed
   - a constraint (`minimum`, `maximum`, `minLength`, `maxLength`, `minItems`, `maxItems`, `pattern`, `default`) changed
   - a security requirement demanded where none was, which is the first entry to arrive in a `security` list
   - a scheme added to a `security` entry that already exists, because the schemes inside one entry are all required together
   - a `servers` entry removed, or its `url` changed. It is the base URL a generated client resolves every path against
   - OpenAPI's `default` response removed from an operation that is left listing no response at all
5. Treat a change as additive when a released client keeps working without reading it. The set is:
   - a path, operation, optional parameter, response status code or schema property added
   - a request property leaving `required`
   - a response property entering `required`
   - a value entering a request enum
   - a value leaving a response enum
   - a security requirement removed
   - a `security` entry added beside one that was already there. The list is alternatives, so a released client keeps passing on the entry it satisfied
   - OpenAPI's `default` response removed from an operation that lists its answers instead. `default` answers a status the operation does not list. A client reads it where it has no branch of its own
   - a `servers` entry added. A released client keeps resolving against the base URL it was generated with, and a document carrying no `servers` already means `[{"url": "/"}]` under OpenAPI 3.1 section 4.8.5
6. Treat a difference in `description`, `summary`, `title`, `example`, `tags`, `externalDocs`, `deprecated`, `info`, an `x-sample-` extension or the document's own `openapi` version as neutral. Nothing branches on prose, and the `x-sample-` family only feeds the sample rendered on an operation's reference page. Any other `x-` extension is unknown, because a client could branch on it.
7. Read a change to a component schema on the strictest side. A schema under `components` is reachable from a request and from a response, and which one this build uses is not something the document says, so a change that is breaking on either side is breaking.
8. Hold `v1` at stability `reporting` while `VERSION` is below `1.0.0`. Below that floor a breaking change is reported and does not fail the build, because no release has published the surface and there is no client to break. Freeze every published major at the first `1.0.0`, and let the release do it rather than a person. `tools/apicompat` reads `VERSION` and applies `frozen` whatever `policy.json` says. It then refuses a `policy.json` still reading `reporting` at such a release. A prerelease of `1.0.0` is below it under semver section 11, so a release candidate still reports.
9. Once a version is frozen, add to it and never subtract. A change rule 4 calls breaking gets a new version segment, `/api/v2`, served beside the old one.
10. Serve a deprecated version for **three minor releases of the application** after its successor ships, and mark every operation on it `deprecated: true` in the change that announces it. A minor release is a `vX.Y.0` tag `scripts/release.sh` cut, and a patch release does not advance the window. The window is counted in releases and not in weeks. The argument for three is [below](#why-the-window-is-three-releases).
11. Move the baseline with `make api-accept`, never with a flag on the check. Accepting a breaking change is a diff in `tools/apicompat/baseline.json` that a reviewer reads next to the changelog entry it needs.
12. Regenerate `internal/transport/http/httpapi/openapi.json` from the registered operations. Nobody edits it directly, and a build serving something else already fails.
13. Keep the printer-facing wire out of this. `/p/register` and `/p/ws` are frozen by firmware nobody updates on this project's schedule, they are not in the OpenAPI document, and what holds them is the model and firmware matrix in `test/fleet`, not this gate.

## why the window is three releases

Two projects publish a deprecation window counted in releases, and this rule
takes the larger of the two numbers.

| project | the window it publishes |
|---|---|
| [Kubernetes](https://kubernetes.io/docs/reference/using-api/deprecation-policy/) rule 4a | 9 months or 3 minor releases after deprecation, whichever is longer |
| [Django](https://docs.djangoproject.com/en/stable/internals/release-process/) | at least 2 feature releases |

Kubernetes says a beta API is "no longer served 9 months or 3 minor releases
after deprecation (whichever is longer)". It gives its reason beside that: the
window "ensures beta API support covers the maximum supported version skew of 2
releases". Django removes a feature in `B.0`, or in `B.1` when it was deprecated
in the last `A.x`. Its reason is "to ensure deprecations are done over at least
2 feature releases".

Three reasons to take three rather than two.

Kubernetes writes its window as a maximum of two things, and this project can
only count one of them. `9 months or 3 minor releases, whichever is longer`
exists so a fast release train cannot shorten the window. This project has no
release schedule, so the months half has nothing to measure, and the release
half is what is left. Dropping to the smaller number would take the guard
Kubernetes put there and remove it.

Django's two rests on a schedule this project does not have. Django ships a
feature release about every eight months on a published calendar, so two
releases is a duration a customer can plan against. Two releases here could be
two weeks.

Three tolerates a customer who skips one release. Kubernetes states this reason
itself, as a version skew of two. Take an operator who upgrades every other
minor release. A two-release window can put the deprecation in a release they
skipped and the removal in the next one they run. A three-release window puts at
least one warning release in every such path.

## enforcement

| rule | gate | what it refuses |
|---|---|---|
| 1, 2 | review | a version segment is a route, and a route absent from the document already fails `make test-unit` |
| 3, 4, 5, 6, 7 | `make api-check` | a difference between the baseline and the served document that the rule table cannot classify. An unknown difference always refuses, whatever the policy says, because a classifier that passed a shape it did not understand would report "no breaking changes" about a shape it could not classify |
| 8 | `make api-check` | a `policy.json` reading `reporting` while `VERSION` is `1.0.0` or above, and a `VERSION` it cannot parse |
| 8, 9 | `make api-check` | a breaking change while the major is frozen, whether the policy froze it or the release did |
| 11 | review | the baseline is a committed file, so moving it is a diff |
| 12 | `make test-unit` | `internal/transport/http/httpapi`'s spec test, which compares the served document against the committed one |
| 13 | `make test-matrix` | a change to the frozen handshake, across every model and firmware version the profile data covers |
| 10 | none | nothing records which release deprecated a version, so no gate can count three from it. See open items |

The classifier's own rules are held by `tools/apicompat/classify_test.go`, which applies one mutation to the real committed document per rule and requires the verdict. A rule with no mutation is a rule nobody has seen fire.

The two rules that read a list rather than a leaf have a case set each.
`TestASecurityRequirementIsClassified` covers three. An entry arrives beside one
already there. The first entry arrives in an empty list. An entry leaves.
`TestTheCatchAllResponseIsClassified` covers three more. `default` gives way to
named statuses. `default` leaves an operation with nothing else. `default`
arrives. Each case asserts the reason as well as the verdict, because two of
them are additive for different reasons.

## rejected

| option | why not |
|---|---|
| a dated version in a `Prusa-Version` header, as Stripe does | Stripe's model needs a per-account default, a rollback window and a version-aware serialiser for every resource. It gives per-client upgrade timing, which matters when clients are third parties on their own schedule. Ours are a bundled frontend and an operator's script against a self-hosted binary, and both upgrade with the binary |
| semantic versioning of the whole API, tied to the binary's version | the binary's version moves for reasons that are not the API's. A client that pins `0.4.x` to keep an endpoint has pinned the storage engine and the connector set too |
| adopting oasdiff as the gate | it is the better classifier and its rule set is where rule 4 comes from. It is a separate binary a runner installs, and a runner that fails to install a tool passes a gate by skipping it. This API is thirteen paths and seventeen schemas; when it stops being small, adopt oasdiff and delete `tools/apicompat` |
| refusing every breaking change today | the surface has no released client, so every refusal would fire on a change that breaks a client the deployment does not have. A gate whose every refusal is spurious trains people to move the baseline without reading it, which is exactly the habit rule 11 exists to prevent later |
| a person setting `policy.json` to `frozen` at `1.0.0` | see [editing the policy at 1.0.0](#editing-the-policy-at-100) |
| a window in months, or in weeks | the unit is releases. Nothing here dates a deprecation either |
| two minor releases, as Django does | see [why the window is three releases](#why-the-window-is-three-releases) |
| no gate until 1.0.0 | the classifier is what makes rule 4 a definition rather than an opinion, and it is worth having written down before the first breaking change rather than after |
| treating a new value in a response enum as additive, as oasdiff's default does | a client that switches exhaustively over a closed set meets a value it has no branch for. oasdiff calls it a warning because a specification cannot say whether a client is strict; this project's own frontend generates types from this document and is strict |

### editing the policy at 1.0.0

Rule 8 could ask a person to set `policy.json` to `frozen` in the change that
cuts `1.0.0`. That is a rule somebody has to remember on exactly one day. The
cost of forgetting it is a gate that prints a breaking change and exits 0, which
is the failure this whole tool exists to stop.

`VERSION` is already in the tree and already says which release this is. So the
release takes the decision and the file records it.

## open items

- rule 10 has its number and no gate can count it. A window of three minor releases needs the release a version was deprecated in. Nothing records that: `policy.json` carries `since`, which is the release a version first shipped in, and no `deprecated_in`. There is no second major to count against either. `VERSION` reads `0.1.0`, one tag exists, and `/api/v1` is the only version served. A gate written today would refuse nothing and report green over an empty set. Add `deprecated_in` to a major's entry in the change that first deprecates one, and the count is arithmetic over two committed numbers
- a minor release is a `vX.Y.0` tag and this repository has cut one tag in its history, `v0.1.0`. `scripts/release.sh` cuts them and runs no gate before it does. So the window is a promise about releases that do not exist yet, and the first thing it needs is a second release
- the served document carries `info.version` from the build, so every build with a different version stamp differs from the baseline in one neutral leaf. It is classified neutral and reported, and a `version` that stops being neutral would be an unknown
- `tools/apicompat` reads OpenAPI 3.1 as a JSON tree and does not follow a `$ref` into the schema it names. A `$ref` that changes target is breaking by rule 4, and a schema that changes under a stable `$ref` is caught where the schema itself is, so nothing is missed; a `$ref` cycle would be, and this document has none
- the SSE event vocabulary is gated by [ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md) and not by this record. `com.prusa.connect.printer.telemetry.sampled` and its siblings reach the OpenAPI document only inside an `example`, which rule 6 above calls neutral, so `make api-check` cannot see a change to the set. `make vocab-check` holds the set to its own version instead
- rule 5 identifies a `security` entry by its position in the list, because that is how `tools/apicompat` addresses a JSON array. Reordering the list is therefore classified as an entry changing rather than as no change. Nothing reorders it today. A rule that identified an entry by its schemes would not have the hazard
- a `security` entry removed is classified additive. That holds only while another entry still accepts the credential a released client sends. Removing the last one that does narrows what is accepted, and one verdict covers both cases
- two wire changes owe this record a migration note, and both are from [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md). Rule 17 moves `send_gcode` off the core action vocabulary, so a published action name goes and a dialect-free one arrives under it. Rule 24 adds a third review outcome, so a published response shape gains the conditional answer. Neither is built. `tools/apicompat/classify.go` is what says which verdict each one takes, and the note is owed in the change that lands it
