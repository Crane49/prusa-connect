---
title: "ADR-20260825-rre: one registration rule and one descriptor"
description: "Every extension registers the same way, under a namespace it cannot forge, carrying a descriptor the published reference is generated from."
date: 2026-08-25
status: accepted
amends: [ADR-20260824-nyw]
tags:
  - adr
  - extensibility
---
# ADR-20260825-rre: one registration rule and one descriptor

- status: accepted
- date: 2026-08-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Supersedes part of [ADR-20260824-nyw](20260824-nyw-extension-mechanisms-and-the-hot-path.md) | Rule 14's rendering is superseded by [ADR-20260826-6x5](20260826-6x5-a-namespace-is-legal-everywhere-it-is-used.md), which reads `github.com/acme/mk4` as `com.github.aux.acme.mk4`

Six registries existed and three of them ran a bare `factories[type] =
factory`. Silent replacement, and no namespace at all, so whichever `init` ran
last owned `filesystem`, `local` or the identity provider. Go computes
cross-package init order from the import graph, so adding an import anywhere in
the tree can change which package wins. That order appears in no source file
and in no diff, so a reviewer has nothing to read.

That was tolerable while every registrant was ours. It stopped being tolerable
once the community writes the drivers we do not maintain.

## rules

1. An extension registers through `sdk/extension`. A contract package is a
   typed wrapper over its `Set` and `Registry`, never a second mechanism.
2. A registry refuses a second registration of one qualified name, and the
   refusal names the module each registrant came from.
3. A registration arriving after the host has built its table is an error, not
   a silent no-op.
4. A namespace is derived by walking the live stack to the first frame outside
   the fixed list of registration wrappers `sdk/extension` holds. Nothing an
   author passes selects the frame, and a wrapper package registers nothing on
   its own behalf, because its frames are the ones the walk skips.
5. A name is a DNS-1123 label, which is the one grammar legal as a config key,
   a metric label, a URL segment and a subject token at once.
6. A name is unique per extension point, not globally. Storage `local` and sink
   `local` are two things.
7. Every registration carries a descriptor. `Summary`, `Description`,
   `Version`, `Contract` and `Stability` are mandatory, and a registration
   missing one does not start the server.
8. `Version` is the implementation's. `Contract` is the contract version it was
   compiled against. They are separate because a host upgrade invalidates one
   and not the other, and `Contract` is checked at registration so an
   incompatible extension is refused before anything is built.
9. `ReplacedBy` is required when `Stability` is `deprecated` and refused
   otherwise. A deprecation that names no replacement tells the reader nothing.
10. Optional fields are still validated. A config key with no summary, a
    required key that also carries a default, an emitted event with no version,
    and a requirement with no capability are all registration errors. Nothing on
    the generated page may be an empty cell.
11. The published extension reference is generated from the registries.
    Nobody edits it directly.
12. A new extension point ships with its descriptor fields, not after them.
13. Only the main module of a build holds `core`. Any other module path that
    would render as `core` is refused, whatever `replace` directive put it in
    the graph.
14. Two module paths that differ hold two namespaces. A path is a namespace as
    it stands or the registration is refused, so `example.com/a_b`,
    `example.com/a-b` and `example.com/A-B` are refused rather than folded onto
    one. The label `aux` separates the reversed host from the rest of the
    path, so `github.com/acme/mk4`, `acme.github.com/mk4` and
    `com/github/acme/mk4` render as three namespaces rather than one.
    `github.com/acme/mk4` is `com.github.aux.acme.mk4`, and a module path that
    carries `aux` as a label is refused. [ADR-20260826-6x5](20260826-6x5-a-namespace-is-legal-everywhere-it-is-used.md)
    rule 3 states that label.
15. A descriptor string carries no control character and no line break, and no
    backtick where the reference publishes it inside a code span. The generated
    page escapes what markdown treats as markup on top of that.
16. A tool that has to answer what namespace a module path holds asks
    `extension.NamespaceOf`. A second derivation is a second answer, and the one
    nobody exercises is the one that is wrong.

## enforcement

| rule | gate |
|---|---|
| 2, 3 | `sdk/extension` tests: a duplicate, and a registration after the drain |
| 4 | `TestTheRegistrantIsTheFirstFrameOutsideTheWrappers`, on a stack where a plugin frame sits under a host frame, and `internal/arch`'s `TestTheNamespaceWalkKnowsEveryWrapper`, which fails when a listed wrapper has moved and when a package calling `Declare` is not listed |
| 6 | `TestOneNameIsTakenOncePerExtensionPointAcrossSets`: the process-wide catalogue every `Set` records into refuses a kind and qualified name it already holds, and `cmd/generate-extensions` refuses a duplicate on either side of its comparison |
| 7, 10 | `TestAMissingMandatoryFieldIsARegistrationError` and `TestWhitespaceIsNotDocumentation` |
| 11 | `cmd/generate-extensions -check`, which refuses a committed artefact that differs from the registries, and refuses an entry that vanished while an operator's configuration may still name it |
| 13, 14 | `TestOnlyTheMainModuleHoldsTheCoreNamespace`, `TestAModulePathThatIsNotAlreadyALegalNamespaceIsRefused`, and `TestDistinctModulePathsDoNotShareANamespace`, which renders every module path one namespace can be cut from rather than a list of inputs |
| 16 | `TestNamespaceOfAnswersWhatTheBuildGraphWould`, and `cmd/scaffold-driver`'s `TestAModulePathNoHostCanNamespaceIsRefused` and `TestTheScaffoldedTestNamesTheNameAHostGives` |
| 15 | `TestTextThatWouldBreakThePublishedPageIsRefused` at registration, and `TestTheReferenceDoesNotPublishMarkupAnEntryCarries` at render |
| all | `internal/arch`'s `TestOnlyLegacyRegistersWithoutADescriptor`, which stops the `legacy/` exemption spreading, on the value as well as the call |

Every one of those was watched fail on a deliberate mutation before it was
trusted green.

## rejected

| option | why not |
|---|---|
| converge `sdk/fault` too | its identity is a globally unique `domain/REASON`, not a module-namespaced label, and its `Doc` carries status, retry and redaction, which mean nothing to a driver. It already refuses a duplicate and already generates its own reference. Converging it breaks four working gates for no security gain |
| let an author pass their own namespace | that is the forgery this record exists to prevent |
| one global name across all extension points | forces `storage-local` and `sink-local` naming, and makes adding an extension point a rename of everything |
| full semver for `Version` | nothing orders two versions beyond compatibility, and a prerelease tag would have to be ordered |
| let the caller pass a frame count | that is the same forgery in a different form: the count is a value an author writes, and a plugin called from a host frame passes the count that reads the host's module. The walk is the host's or the rule is not enforced |
| lowercase a module path that is not already a label | it is what let three module paths share one namespace. An author whose path carries an uppercase letter or an underscore is refused with the label named, and renames the module or does not register |
| refuse a module path whose first element carries no dot | it stops one kind of namespace theft and leaves another. `acme.github.com/mk4` carries a dot in its first element and rendered as `com.github.acme.mk4`, which is `github.com/acme/mk4`. It also refuses every module reached through `replace` or `go.work`, which is how a driver is developed before it is published |
| a `/` between the reversed host and the rest of the path | `/` already separates a namespace from a name, so `com.github/acme/mk4/printer` cannot be cut back into the two, and `internal/app/config/install` cuts at the first one to read the namespace |
| escape a descriptor string at render time only | the value is still in the registry, and every other publisher of the catalogue would have to repeat the escaping. Refusal at `Declare` stops it entering the registry, and render-time escaping still runs after that, for markdown's own markup |

## open items

- Nothing in the running application builds four of the five `sdk` registries.
  `internal/app/plane.go` drains `internal/extension/registry` only, so those
  lists never seal in a real process. Wiring belongs in `internal/app`.
- `sdk/fault` does not seal. It has no moment at which the host builds it, and
  `fault.TryRegister` is deliberately callable after start-up. The duplicate
  refusal is there, so only late registration is open.
- `sdk/routesapi` still exposes `gin.IRouter`, which remains the precondition
  for tagging `sdk/v1`.
- A namespace is refused for a module path that is not already one, and the
  refusal reaches an author whose path carries an uppercase letter. Nothing
  offers them an escaped spelling, so the answer is to rename the module.
  `cmd/scaffold-driver` refuses the path at the flag, and an author who wrote
  their `go.mod` themselves still meets the refusal at start-up.
- The catalogue holds a name for the life of the process and never releases it.
  Two independent hosts in one process therefore share one collision domain.
  That is what makes rule 6 true, and it is why a test that registers a name in
  two gathering lists has to name them differently.
