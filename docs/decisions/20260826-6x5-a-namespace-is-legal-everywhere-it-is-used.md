---
title: "ADR-20260826-6x5: a namespace is legal everywhere it is used"
description: "The namespace a host derives from a module path is a dotted sequence of DNS-1123 labels, and every vocabulary that carries one accepts every label of it."
date: 2026-08-26
status: accepted
amends: [ADR-20260825-rre]
tags:
  - adr
  - extensibility
  - naming
  - sdk
---
# ADR-20260826-6x5: a namespace is legal everywhere it is used

- status: accepted
- date: 2026-08-26

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Supersedes rule 14 of [ADR-20260825-rre](20260825-rre-one-registration-rule-and-one-descriptor.md) | Builds on [ADR-20260825-dee](20260825-dee-the-capability-vocabulary.md), [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md)

[ADR-20260825-rre](20260825-rre-one-registration-rule-and-one-descriptor.md) rule 16 gives one
derivation from a module path to a namespace, and rule 14 puts a boundary label
between the reversed host and the rest of the path. That label has to be legal
where the namespace is consumed. A boundary label of `-` is not:
`connectorapi.Capability.Valid`, `fault.NewDomain` and `event.ValidateTypeSyntax`
all refuse it, and it would affect every module path with a path element, which
is every module on GitHub and GitLab. A community driver could then mint no
capability key, no error domain and no event type of its own.

The boundary label is one of two causes. The other is the label alphabet: a
DNS-1123 label carries hyphens and may start with a digit, and both vocabularies
that refused the boundary also refused `prusa-driver-acme` and `99designs`.

## rules

1. Every label of a namespace is a DNS-1123 label, and every vocabulary that
   carries a namespace accepts every DNS-1123 label as one segment of an
   identifier. The vocabularies are capability keys, error domains and event
   types.
2. A capability key segment and an error domain label are lowercase
   alphanumerics at each end with `-` or `_` between, which contains DNS-1123
   and is the grammar an event type segment already had.
3. The label between the reversed host and the rest of a module path is `aux`,
   and a module path carrying that label anywhere is refused. `github.com/acme/mk4`
   is `com.github.aux.acme.mk4`. The refusal is what keeps the rendering
   reversible, because a host of `mk4.acme.aux.github.com` renders onto the same
   namespace otherwise.
4. A namespace has at least two labels, because one label is not an error
   domain.
5. A namespace is at most 96 bytes, which leaves 32 inside `sdk/event`'s
   128-byte type limit for the name of what happened.
6. One package owns the syntax of each identifier a registration descriptor
   cross-references, and the vocabulary that owns the identifier answers from
   it. `extension.IsCapabilityKey` is the capability key's, and
   `connectorapi.Capability.Valid` calls it.
7. A descriptor's cross-reference to another vocabulary is validated at
   `Declare` by that vocabulary's own validator. A value the vocabulary refuses
   is a registration error, not a cell on the published reference.
8. A candidate rendering is run through every consuming validator before it is
   chosen, and the run is a test in the tree rather than a reading of the
   grammars.

## enforcement

| rule | gate |
|---|---|
| 1, 2 | `sdk/extension`'s `TestANamespaceIsLegalInEveryVocabularyThatCarriesIt`, six module paths through `NamespaceOf` and all three validators, and `TestEveryLabelANamespaceCanCarryIsLegalInEveryVocabulary`, which generates the labels rather than listing them |
| 1, 8 | `TestACommunityAuthorMintsEveryIdentifierFromOneNamespace`: one module path to a capability key, an error code and an event type, each asserted as the exact string |
| 3 | `TestDistinctModulePathsDoNotShareANamespace`, which renders every module path each namespace it produced can be cut from, boundary label included, and `TestAModulePathThatIsNotAlreadyALegalNamespaceIsRefused` |
| 4, 5 | `TestAModulePathThatIsNotAlreadyALegalNamespaceIsRefused` on a single-label path and on a path that renders past the limit |
| 6 | `TestARequiredCapabilityIsCheckedAgainstTheVocabularyThatOwnsIt` asserts `Declare` and `connectorapi.Capability.Valid` give one answer per input |
| 7 | the same test, on prose, an uppercase key and two argument separators |
| 7 | `TestAnEmittedEventTypeIsCheckedAgainstTheVocabularyThatOwnsIt` asserts `Declare` and `event.ValidateType` give one answer per input, on prose, one segment, an MQTT topic pattern and a core type the vocabulary does not carry, and on three types it does accept |
| 7 | `cmd/generate-extensions`'s `TestAnEmittedEventTypeIsRefusedWhileTheShippedSinksStillRegister`, which asserts the four shipped sinks are in the catalogue in the same test, because a refusal gate passes against a `Declare` that refuses everything |

Every one of those was watched fail on a deliberate mutation before it was
trusted green: the boundary label set to `-`, the boundary reservation removed,
each of the two widened grammars narrowed back, the two namespace bounds
removed, and the cross-reference check removed.

## rejected

| option | why not |
|---|---|
| change the boundary label alone | it fixes one label and leaves the alphabet. Executed: with any boundary, `example.com/prusa-driver-acme` and `github.com/99designs/mk4` are still refused by `Capability.Valid` and the second by `NewDomain`, because a hyphen and a leading digit are legal in a module label and were not legal in those two grammars |
| a boundary label carrying `_`, which no module path can hold | it needs no reservation, and it costs the invariant in rule 1: the label is not DNS-1123, so every check that reads a namespace label learns an exception. `extension.isName` is the first, and it validates the namespace half of a `namespace/name` an operator writes in configuration |
| a boundary label that is legal in all three vocabularies and illegal as a module label | there is none. The three grammars intersect at lowercase alphanumerics with `-` or `_` between, a module label is that set without `_`, and `_` is the only character a module label cannot hold. Choosing it is the row above |
| a second rendering for wire use | ADR-20260825-rre rule 16 exists to stop that: two answers, and the one nobody exercises is the one that is wrong |
| escape a hyphen or a leading digit into the narrower alphabet | ADR-20260825-rre already rejected rewriting a module path, and the reason holds: a rewriting that is not injective folds two modules into one collision domain, and one that is injective is unreadable in the identifier a person is meant to recognise |
| refuse a module path whose labels are not already in the narrow alphabet | it refuses `github.com/prusa3d/prusa-connect`. Hyphenated repository names are the norm |
| a count of host labels as the boundary, so nothing is reserved | it looks like noise at the most visible position: `h2.com.github.acme.mk4` is a fault domain and an event type prefix a person has to recognise, and it no longer sorts by owner |
| `pkg`, `mod` or `host` as the boundary label | each is a real module path element. `github.com/pkg/errors` and `golang.org/x/mod` are the counterexamples. `aux` is one of the names Go's own module path checker refuses as an element and as the first label of a host, so the reservation reaches almost nobody |
| relax the three validators to admit `-` as a whole label | it admits a label that is not a name in any of the three vocabularies, and loses the property that makes a published identifier greppable |
| put the shared label grammar in a new `sdk` package every vocabulary imports | `sdk/fault` is a leaf and stays one. The three grammars are not identical: a capability key and an event type carry `_` and an error domain label does not, so what is shared is the containment in rule 1, and a gate states it once |
| leave `Requires[].Capability` unvalidated because the host refuses it later | the host is not the reader. The value reaches `extensions.md` as a host requirement, and ADR-20260825-rre rule 10 already refuses an empty cell there |

## open items

- Rule 7 now covers `Emits[].Type`, which `Declare` runs through
  `event.ValidateType`. The four event sinks that declared transport
  destinations in that field declare them in `Publishes[].Address` instead, a
  second field whose value is in the extension's own transport vocabulary. That
  split is enforced and is not written as a rule here, because this record is
  accepted: rule 7 says a cross-reference is validated by the vocabulary that
  owns it and says nothing about a field no vocabulary owns. The amendment is on
  the old backlog.
- `Publishes[].Address` is held to what the published reference can carry and to
  nothing else. No package owns an MQTT topic, an AMQP routing key or the
  discriminator in a webhook body, so there is no validator to answer from, and
  a wrong address is caught by a gate in the adapter rather than by a grammar.
- `docs/src/content/docs/writing-a-driver.md` line 190 publishes the rule 14
  rendering, `com.example.-.prusa-driver-acme`, which is now
  `com.example.aux.prusa-driver-acme`.
- The rendering is injective over module paths and is not prefix-free over
  namespaces. `github.com/acme/mk4` minting `command.beep` and
  `github.com/acme/mk4/command` minting `beep` compose one capability key. Rule
  14's property is that two module paths hold two namespaces, and it holds; a
  key is unique per namespace and not across namespaces.
- `core` is the one namespace this record does not cover. It is a legal
  capability prefix and a legal event type prefix, and `fault.NewDomain` refuses
  it as reserved rather than as malformed, which is ADR-20260825-rre rule 13 working.
- ADR-20260825-dee rules 1 and 26 say a driver's own keys are namespaced by its module
  path. That is now possible and still unimplemented: nothing mints a key under
  a namespace, and `capabilities.md` is generated from `profiles.json`, which
  would never carry one.
