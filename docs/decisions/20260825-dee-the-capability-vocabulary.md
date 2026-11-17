---
title: "ADR-20260825-dee: the capability vocabulary"
description: "What a printer can do is a descriptor merged by intersection, and the sdk/connectorapi surface a driver implements to declare one."
date: 2026-08-25
status: accepted
tags:
  - adr
  - contracts
  - printers
---
# ADR-20260825-dee: the capability vocabulary

- status: accepted
- date: 2026-08-25

<!-- nav:related -->
> [!NOTE]
> Contracts: [ADR-20260824-gfr](20260824-gfr-the-extension-sdk-is-a-separate-module.md) · Registration: [ADR-20260825-rre](20260825-rre-one-registration-rule-and-one-descriptor.md) · Decisions: C1, C2, C4, C5, C6, C9

The shipping server asks what firmware a printer runs and branches on the
answer, in roughly 1,600 lines of matrix and a validator of hand-written range
checks. Every branch is a claim about a device the server has never seen, and
each one is wrong for somebody: a rebadge, a beta build, a model added after the
branch was written.

This record replaces the question. A printer says what it can do, in a closed
vocabulary, and the host commands only what it resolved.

## rules

1. A capability is a key in a closed vocabulary: dotted lowercase segments, each
   starting with a letter, at most 253 bytes. `core.` namespaces the keys the
   host mints, and a driver's own keys are namespaced by its module path.
   - **A guard takes no key.** [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
     rule 7 declares a guard beside the capability set, so no state condition is
     spelled as a capability key.
2. One argument of a verb is its own key, `verb#argument`. A verb accepts exactly
   the arguments it carries keys for, so presence is the declaration and a tier
   subtracts an argument by not carrying its key.
3. A descriptor value is IPP's supported-and-default pair: whether the capability
   is there, the kind its argument carries, the closed set of values, the
   default, whether it is required, an inclusive lower bound and a bound on
   magnitude. A bare boolean is not a descriptor.
   - **A guard is not a descriptor value either.** This pair says what a device
     supports. Whether it accepts a verb in the state it is in is the guard of
     [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
     rules 7 and 8, carried as data the host evaluates.
4. Three tiers describe one printer: the driver's compiled baseline, the embedded
   profile keyed by model and firmware range, and the device's own probe. They
   merge by **intersection only**. There is no union and none is added, so a
   stale or wrong lower tier produces a missing capability and never one the
   printer does not have.
   - **A guard subtracts in this direction.** The guard of
     [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
     rule 7 removes verbs the device's current state does not accept, and adds
     none. It is no fourth tier: the three above still merge by intersection,
     and the guard is read after that merge, at the attempt.
5. An absent tier and an empty tier are different. Absent narrows nothing; empty
   narrows everything away.
6. A key's name is a contract. A key or an argument whose meaning changes takes a
   new name, and the old one is retired in `reserved.json` and never reissued.
7. The host validates every argument against the resolved descriptor before the
   driver's `Do` is called, and fills in declared defaults. A driver never
   re-checks an argument it declared.
8. A rule the descriptor cannot state is stated in the driver, at the point the
   value reaches the wire, and is listed with the value that shows it in the
   dialect's divergence gate. It is never left to the printer to be the first
   thing that refuses a command.
9. A firmware version reaches two packages: `internal/printer/profile`, which
   selects a data file by parsing the components as numbers, and
   `internal/printer/capability`, which reports it. Nothing else reads the text
   and nothing anywhere compares two of them.
10. A firmware version is ordered by semver precedence. Build metadata is
    dropped, a prerelease is below the release it is a candidate for, and a range
    bound admits the candidates for its own version by carrying a prerelease tag
    of its own. A prerelease is read whichever separator opens it, because Buddy
    firmware spells one fact two ways: `6.2.0.alpha` and `6.4.0-alpha` are both
    candidates for their release. A fourth number counts builds of a release
    rather than naming a candidate for one, so it is dropped.
11. A resolved capability set is returned with an epoch. The epoch moves on
    every arrival, so a set cached across a reconnect is detectably stale; the
    set's digest is what says whether the permissions themselves moved.
12. A merged set that breaks the descriptor rules is pruned and the removal is
    reported. It is never refused as a whole, and never commanded against as it
    stands.
13. `Driver` is `Baseline()` and nothing else. How a device is reached is
    `Outbound` or `Inbound`, a driver implements at least one, and one that
    implements neither is a startup error.
14. Profile data is extracted from the shipping implementation mechanically, with
    a gate that fails when the two disagree. A matrix a person transcribes is
    wrong within a month, and this repository has already proved it twice.
15. A firmware band that nothing has recorded is not written. An invented band
    ships as a fact and subtracts a capability a printer really has.
16. `sdk/connectorapi` imports nothing outside the standard library, and every
    change to it ships a case in `sdk/connectorapi/connectorapitest`.
17. A printer no profile covers takes the baseline bounded by what that driver's
    profiles cover between them. That tier is a union of data, built in
    `internal/printer/profile`; the merge that applies it is still an
    intersection, and `connectorapi` still has no union.
18. A driver that ships no profiles keeps its whole baseline. The alternatives
    are a printer that accepts no command and a printer the host will not admit,
    and both are worse than trusting a `Baseline()` its conformance suite
    already checked.
19. A resolution names which tier bounded it. A matched printer, one held to its
    driver's coverage and one whose driver ships no profiles are three different
    answers, and a reader of the resolution tells them apart without a log line.
20. A dialect whose liveness is server-initiated pings on the shipping
    connector's own interval and holds the connection to the shipping read
    deadline. A read that blocks with no deadline turns a printer that vanished
    into a session that queues nothing and loses every command written to it.
21. What is stored on a device is a second interface, `Filesystem`, reached
    from a `Session` by type assertion. A dialect driving a machine with no
    storage answers nothing, and a `Session` forced to declare the methods would
    answer them with a lie.
22. A file path in the contract is storage-prefixed and relative:
    `usb/models/part.bgcode`. A device's own absolute spelling is the driver's
    business and is translated at the driver boundary, so one path means one
    thing across every dialect.
23. A path carrying a `..` segment is refused with `PathNotAllowed` and never
    reaches a device. The segment is refused rather than resolved, and the check
    runs on the path that reaches the wire rather than the one that arrived. The
    code is part of the contract, because a driver refusing with an error of its
    own is indistinguishable from one that sent the path and received no answer.
24. A field that a device may not report carries a companion flag rather than a
    zero value. `Storage.FreeSpace` and `Storage.FreeSpaceKnown` are the pair,
    on the model of `Value.Limit` and `Value.HasLimit`. A device with nothing
    left and a device that said nothing are different answers, and only one of
    them makes an upload fail.
25. A dialect whose devices volunteer nothing about their storages asks on every
    connection. The host commands only what it resolved, so a driver that never
    asked leaves a printer whose file tree no API can reach.
26. `connectorapi.Capability` is an open vocabulary and is not an enum. A driver
    mints keys under its own module path, so no map over it is exhaustive and
    `exhaustive` is told so in `.golangci.yml`. ADR-20260825-bhe rule 6 is the same
    statement from the other side.
27. A profile is selected on what the printer is, not on how its firmware
    spelled it. A model name matches with case and every separator ignored, a
    device type code matches as it is written, and a spelling neither reaches is
    a row in `aliases.json`. One spelling may reach only one model, and a file
    where two models answer to one spelling is refused at load.

## enforcement

One row per gate. The third column is what that case covers, where the name
does not already say it.

| rule | gate | what the case covers |
|---|---|---|
| 1, 2, 3 | `Set.Problems` | run over a driver's baseline by `connectorapitest`, and over the merged set by the resolver |
| 4, 5 | `TestALowerTierSubtractsAndNeverAdds` | |
| 4, 5 | `TestAnAbsentTierIsNotAnEmptyTier` | |
| 6 | `profile.ValidateWithReserved` | against `reserved.json`, at load |
| 7 | `TestARefusedArgumentNeverReachesTheDriver` | asserts the driver was not called |
| 8 | `TestTheArgumentBoundsMatchTheShippingValidator` | one corpus of values through the shipping validator and the descriptor, failing on an unlisted divergence |
| 9 | a `forbidigo` rule on `connectorapi.FirmwareLabel.String` | outside those two packages |
| 9 | `TestFirmwareTextIsNeverOrdered` | an ordering operator on any `String` result |
| 9 | `TestTheFirmwareLabelGateIsArmed` | refuses a nolint or an exclusion that would switch the `forbidigo` rule off |
| 10 | `TestRangeContains` | |
| 10 | `TestTheFirmwareFloorMatchesTheRetiredMatrix` | |
| 11 | `TestAReconnectMovesTheCapabilityEpoch` | |
| 11 | `TestEachUnitCountsItsOwnEpoch` | |
| 12 | `TestAMalformedMergedSetIsPrunedRatherThanCommandedAgainst` | |
| 13 | `connectorapitest` | |
| 13 | the registration refusal | for a driver implementing neither direction |
| 14 | `TestProfilesMatchTheRetiredMatrix` | regenerates `profiles.json` and diffs it |
| 15 | `TestNoNewerFirmwareChangesAnythingTheMatrixRecords` | |
| 15 | `floorOf` | refuses to flatten a matrix that grew a second band |
| 16 | `internal/arch`'s import gates | |
| 16 | the sdk module having no non-standard requirement | |
| 17 | `TestAnUnmatchedIdentityIsHeldToWhatTheProfilesCoverLoudly` | |
| 17 | `TestTheCoverageTierAppliesOnlyWhenNoProfileMatched` | |
| 17 | `TestAnUnrecognisedPrinterIsHeldToWhatTheProfilesCover` | measures it against the real driver baseline |
| 18 | `TestADriverWithNoProfilesKeepsItsWholeBaseline` | |
| 18 | `TestCoverageOfAnEmptyRegistryIsAbsentRatherThanEmpty` | |
| 19 | `TestTheResolutionConditionSaysWhichTierWasInForce` | over all three bases |
| 20 | `TestAPrinterThatStopsAnsweringLosesItsConnection` | |
| 20 | `TestAPrinterThatAnswersPingsKeepsItsConnection` | the two fail in opposite directions |
| 21 | `TestFilesReportsWhetherTheDialectHasAFilesystemHalf` | |
| 21 | `TestADialectWithNoFilesystemIsRefusedWithACode` | holds the host to a code, and not an error nothing above it reads |
| 22 | `TestAFileInfoPayloadBecomesADirectoryListing` | |
| 22 | `TestADirectoryIsListedFromWhatThePrinterReports` | the two read a real simulated printer's tree back through the session |
| 23 | `connectorapitest`'s `a filesystem half refuses a path that leaves its storage` | asserts the code and not merely an error |
| 23 | `TestAFileVerbRefusesAPathThatLeavesItsStorage` | |
| 23 | `TestAPathThatLeavesItsStorageIsRefusedOnThePublishedSurface` | on the HTTP surface |
| 24 | `TestAStorageThatReportedNoFreeSpaceIsNotAFullOne` | in the contract and in `internal/app` |
| 24 | `TestAStorageWithNoReportedFreeSpaceSerialisesAsNull` | on the bytes a client reads |
| 25 | `TestAConnectedPrinterIsAskedWhatStoragesItHas` | fails when the request is not sent |
| 25 | `TestAnInfoEventCarryingNoCommandIDStillTeachesTheSessionItsStorages` | fails when the answer is dropped |
| 26 | none | a linter setting, where a wrong one appears as noise and not as a defect |
| 27 | `TestEveryModelIsReachedByEverySpellingAPrinterSends` | generates the spellings from each model, so a model added to the file is covered the day it is added |
| 27 | `TestADeviceTypeCodeReachesItsModel` | the code a registration carries, which is what a real Core One reports |
| 27 | `TestASharedFirmwareDeviceTypeIsNotASpelling` | the code two models share names neither, so an MK3.9 never takes the MK4 profile |
| 27 | `TestASpellingNoRuleReachesMatchesNothing` | the other direction: a stranger's printer stays unmatched and loud |
| 27 | `profile.Validate` | refuses two models one spelling reaches, and an alias row naming a model no profile covers |

Every gate in this table was watched fail on a deliberate mutation before it was
trusted green.

## rejected

Each heading below is the option, unchanged, so a reviewer can link to the
argument rather than repeat it.

### ask the firmware version and branch

It is the model this record replaces. Every branch is a claim about a device
nobody has seen, and a rebadge or a beta build breaks it silently.

### a union or an override in the merge

One tier able to add is one file able to grant a printer a capability it does
not have. The intersection is the whole safety property.

### a set of capability strings

A bare boolean forces every caller to hard-code the supported values and the
default.

### a method per verb

A compile-time capability declaration cannot vary per device, which is what
forces a not-supported error back onto the caller.

### `FirmwareLabel` as a string

`<` and `>` compile against a string, and `"6.4.0" > "6.10.0"` is true as text.

### a struct so `<` cannot compile, and nothing else

Measured false. `String` is exported for the two readers that need it, so the
comparison compiles through it. The rule is held by the three gates above and
not by the type.

### dropping a prerelease tag before comparing

It makes `6.5.0-RC` equal to `6.5.0`, so a candidate build is offered what the
finished release added. That is the failure this vocabulary exists to prevent,
arriving through the comparator.

### refusing a merged set that breaks the rules

It answers one unusable key with a printer that accepts no command at all.
Pruning is a subtraction, which is the direction the model already allows.

### bumping the epoch only when the merged set differs

The host cannot promise that nothing changed across a disconnection it did not
watch. The digest is what answers "did the permissions move".

### a divisibility rule, an exclusive bound or a character class in the descriptor

Each is one argument's rule in the whole matrix, and a vocabulary that grows a
field per rule stops being a closed vocabulary. They live in the driver under
rule 8, listed with the value that shows them.

### leaving an unmatched printer the whole baseline

A matched profile subtracts 8 to 15 verbs per model, so the widest set in the
system went to the device nobody has ever seen. The tier that exists to
subtract widened the set by being absent.

### the intersection of the driver's profiles as the unmatched tier

It withholds a capability from a printer on the evidence that one other model
lacks it, and the shipped matrix would leave an unknown device 30 verbs of 45
with nothing recording why.

### refusing a printer no profile covers

A rebadge and a firmware ahead of the file are the ordinary cases, and neither
is a reason to leave a machine unusable. The whole model answers a missing
profile with a reduced set.

### an empty tier for a driver with no profiles

It resolves to nothing, so a fresh community driver ships a printer that
accepts no command at all, and its author sees a working handshake and a dead
API.

### generating a driver from `profiles.json`

The profile carries the vocabulary and not the wire. 31 of 57 verbs are
rewritten before the socket, so a driver generated from it sends a frame no
firmware receives.

## open items

- One verb is left to have no wire form in `internal/connector/xbuddy`:
  `START_ENCRYPTED_DOWNLOAD`. It names a second, encrypted channel this host
  does not serve, so starting it would leave a device fetching from somewhere
  nothing is listening. Its argument bounds stay in the shipping validator until
  the channel exists. The list it is read from is `notPorted` in that package's
  limits test, held to this bullet by
  `TestTheOpenItemNamesTheVerbsWithNoWireForm`.
- Reading a file back off a printer has no verb. Writing one to it does:
  `POST /api/v1/printers/{printer}/files` stages from a storage resource and the
  device fetches the bytes over its own connection, which is the block-request
  path in `transfer.go`.
- A listing is a command, so browsing a printer takes `send_command` on it. The
  authorization vocabulary has no read-only file action, and adding one is an
  `internal/authz` change nothing has taken yet.
- The thirteen shipped xBuddy profiles cover the whole driver baseline between
  them, so rule 17's tier subtracts nothing from it: an unrecognised printer
  resolves 45 verbs where an MK4 resolves 32, the same counts as before the rule
  existed. The fail-open path is closed and the numbers have not moved.
