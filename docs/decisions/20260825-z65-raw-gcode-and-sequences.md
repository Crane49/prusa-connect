---
title: "ADR-20260825-z65: raw gcode, sequences and presets"
description: "Arbitrary gcode is a capability, an action of its own and a driver refusal set, sequences are ordered and never atomic, and a preset is a deployment-wide stored resource that pins its models and is refused on anything else."
date: 2026-08-25
status: accepted
tags:
  - adr
  - printers
  - security
---
# ADR-20260825-z65: raw gcode, sequences and presets

- status: accepted
- date: 2026-08-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Rule 8's action leaves the core vocabulary under [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md), which keeps it at manager and gives the name to the xBuddy adapter | Builds on [ADR-20260825-dee](20260825-dee-the-capability-vocabulary.md), [ADR-20260825-smx](20260825-smx-storage-confinement-and-local-file-handles.md), [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md)

Every other command this server sends is a declared verb, checked against a
printer's resolved capability set, with each argument validated against a
descriptor before the driver is reached. A raw gcode payload has none of that.
The host cannot know what it does, cannot know whether this model supports it,
and has no principle it already holds that would let it refuse a dangerous one.

This record says what makes it safe enough to ship, and is explicit about what
that does not reach.

## what the shipping server does today

Measured, not assumed. `legacy/` has no caller-facing raw gcode path:

| observation | where |
|---|---|
| the only caller entry is `AddCommandWithValidation` | `legacy/transport/httpapi/handlers.go:1393` |
| it resolves the command through the closed 57-name matrix, and fails `ErrCommandNotFound` for anything else | `legacy/printer/queue/manager.go:210` |
| every non-test `AddCommand` call passes type `"J"` and a matrix name | `legacy/connector/xbuddy/connection.go:173,421`, `legacy/printer/files/loader.go:85` |
| `G` frames are produced only by `buildGCodeCommand`, a closed switch over 19 matrix verbs | `legacy/connector/xbuddy/commands/builder.go:111` |
| no command name in the matrix means raw gcode | `legacy/kernel/types/enums.go`, 57 names |

The frontend's `GCODE_COMMAND_TYPES = ['G', 'F', 'D']`
(`frontend/src/clients/api-schemas.ts:215`) is a **rendering** rule for a queue
row whose stored type is a gcode frame. `HOME` is stored as type `G` with data
`G28`, and the transform turns that into `{gcode: "G28"}` so the history shows
the payload rather than failing to parse it as JSON. It is not evidence of an
entry point.

`D` is a frame type, not a gcode letter, and the two are separate questions.
The `D` **frame** is `D{id:08x}{data}` and carries a debug payload
(`legacy/connector/xbuddy/protocol/buddy_encoder.go:105`); nothing in the
shipping server constructs one. Buddy **D-codes** are a different thing: `D3`
writes memory by address, `D5` to `D7` reach flash and OTP. Rule 6 refuses the
letter for that reason, and this port still sends no `D` frame.

So this is new behaviour, not restored behaviour, and it is treated as new.

## rules

1. Raw gcode is two verbs in the closed vocabulary: `core.command.send_gcode`
   for a queued `G` frame, and `core.command.force_gcode` for an out-of-band
   `F` frame that the firmware runs without waiting for the print queue. They
   are separate keys because they are separate risks, and a profile that
   withholds one keeps the other.
2. A payload is exactly one gcode command. Several commands are a sequence and
   never a payload: a payload the host cannot split is a payload whose refusals
   it cannot apply line by line.
3. The payload is the `gcode` argument, a required string bounded at 256 bytes,
   with no `Supported` set in a driver's baseline. A deployment that wants a
   closed word list narrows `Supported` in a profile, and the intersection
   merge already does the work.
4. A driver refuses a payload that is not one gcode word optionally followed by
   parameters, where a word is `G`, `M` or `T` immediately followed by digits
   and at most one decimal part. Refusing what it cannot parse is what makes
   the word extraction total: a refused word must not be reachable through a
   spelling the extractor cannot parse.
5. A driver refuses any byte outside printable ASCII, `0x20` to `0x7E`. That is
   what stops a second command being smuggled behind a carriage return, and it
   is stated once rather than per verb.
6. A driver refuses a closed set of words no caller may ever send: every `D`
   word, and `M997`, `M28`, `M29`, `M30`. The set is named in one place and its
   membership carries a reason each.
7. The refusal set is a denylist and stays one. It stops what cannot be undone,
   and it does not try to stop everything dangerous. Rule 21 says what it does
   not reach.
8. Raw gcode is its own authorization action, `send_gcode`, reached at manager
   level and above and never by an operator. A caller who may pause a print is
   not also a caller who may reconfigure the machine.
   - **The name moves and the level stays.**
     [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
     rules 17 and 18 keep one core action for a payload the host cannot
     inspect, spelled without a dialect. `send_gcode` becomes the xBuddy
     adapter's own name for that action, and the dialect-free core action takes
     the bit `send_gcode` holds. No stored row moves. The manager floor is
     refused to nobody by rule 6: that denylist refuses `M997` to a manager
     too. What buys the floor is rule 7 and this rule, which stop what cannot
     be undone.
   - **Kept and tightened by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md)
     rules 16 and 40.** The level model this rule enters at goes. `send_gcode`
     and `force_gcode` become dangerous-tier verbs, reached by an explicit
     per-verb row and by no level at all, which is narrower than manager.
9. The action a command takes is derived from its verb by one function,
   `authz.ActionForVerb`. A call site that names an action directly is right
   only for the verbs that existed when it was written, and this server has
   more than one transport that sends commands.
10. A sequence is ordered. It is not atomic and it is not exclusive: there is no
    transaction on a printer, nothing undoes a completed move or a heat, and
    another caller's command may interleave between two steps.
11. A sequence stops at the first step the host refused or the printer did not
    accept. Every step reports its own outcome, and a step that was not
    attempted says so in those words rather than being absent or reported as a
    success.
12. A sequence carries at most 64 steps and at least one. Every step is checked
    against the resolved capability set and against the caller's grants exactly
    as a single command is, so a sequence is never a way to reach a verb a
    single command could not.
13. A raw gcode command is logged at the transport with the principal, the
    printer, the payload and the outcome. It is the one command an operator
    will want a record of afterwards, and the payload is the whole of what
    happened.
14. A preset is a named, stored gcode sequence. It is a resource in the sense
    `internal/app/resource` means: stored in the machine-owned configuration
    overlay, written atomically under a version token, and addressed by kind
    and name.
15. A preset pins the models it was written for. A preset naming no model runs
    on any printer; a preset naming models runs only on a printer whose
    reported model is one of them. Gcode that is safe on an MK4 is not also
    safe on a CoreOne.
16. Writing, renaming or deleting a preset takes `configure_resource` on the
    preset kind. Reading one takes `view_resource`. Running one takes the
    action its steps take on that printer, which is `send_gcode`, and nothing
    on the preset kind at all.
17. A preset is expanded into its steps and every step is validated at run
    time. A preset stored before a printer arrived cannot have been checked
    against that printer's capability set. A validation done only at write time
    is a check against a machine that was not there.
18. A profile carries the host-minted keys the shipping firmware matrix has no
    opinion about, and the generator writes them in from one declared list. A
    key is a verb the matrix does not name, or an argument it does not record
    for a verb it does name. The list may limit a key to some models, such as
    the tool changers. Hand-editing the generated profile file is not the
    mechanism.
19. The HTTP surface for a sequence answers 200 whenever the sequence was
    accepted for execution, and reports each step's outcome in the body. A
    stopped sequence is a 200 whose body says where it stopped.
20. An idempotency key belongs to a step, never to a sequence. A sequence
    replayed after a partial failure is not the same request as the one that
    failed, and a key on the whole would claim it was.
21. This record states what the refusals do not stop, and the statement is kept
    current. Not stopped: an allowed word with a destructive parameter
    (`M104 S500`, `G1 Z-100`), a factory reset (`M502`), an EEPROM write
    (`M500`). Also not stopped: a print started outside the `start_print` verb
    (`M23`, `M24`), and a sequence another caller interleaves with.
22. A preset belongs to the deployment. There is no owner and no per-printer
    scope: the only thing that narrows where one applies is the model pin of
    rule 15. Who may read, write and run one is the authorization graph's
    answer, on `resource_kind:gcode-preset` and on the printer.
23. Running a preset on a printer whose reported model the preset does not name
    is refused, and nothing is sent. Not warned about, and not run: a warning
    arrives after the move into the wall.
24. The preset kind is configurable and not supervised. It carries a
    `resource_kind` grant like every other configurable kind, and the resource
    instance surface refuses it, because a preset names no implementation for
    the lifecycle plane to construct.
25. The table behind `authz.ActionForVerb` names every verb the published
    capability vocabulary carries, and it is total. A default arm decides for
    every verb written after it, and those are the verbs nobody weighed.
    `cancel_job` was declared, granted and published while
    `core.command.stop_print` travelled under `send_command`.
26. A verb the table does not name takes no action at all, and every check of
    that action is refused. Answering with the weakest write action this
    vocabulary has is the failure this rule exists to stop.
27. Such a verb is refused before a printer is resolved, with
    `core.printer/VERB_UNKNOWN`. The refusal carries the verb and no fact about
    a printer. It is therefore the same answer whatever the caller holds, and
    whether or not the id belongs to a machine.
28. `core.command.flash`, `core.command.upgrade`, `core.command.set_token` and
    `core.command.set_hostname` take `provision`, which the manager level
    confers and the operator level does not. The four change the machine rather
    than the job on it, and this server can undo none of them. That is the bar
    rule 6 already holds `M997` to inside a raw payload.
    - **Kept whole by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rules 15
      and 40.** The rule here is not reversed.
      `provision` ceases to exist as an action name; the four verbs take the
      `configure` class and the `irreversible` marking, so they sit above
      operator and above manager-by-role. The action's name and the level it
      entered at are not part of the rule, so ADR-20260901-b2z may change them.
29. `provision` is one action for the four and not one per verb, for the reason
    the rejected table below gives about `send_gcode`. A caller reaching for any
    of them is reaching to change what the machine is.
    - **Amended by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rules 17 and
      19.** The dangerous tier is one row per verb, which is the shape this
      rule refuses. The toil it refused is answered where it was real:
      ownership implies every dangerous row on the owned device, computed and
      never stored, so the common case writes nothing. Rule 9 here stands, no
      call site names an action.
30. Whoever owns a device holds the four on it, whatever role they hold.
    Ownership is the relation `device#owner`. It is not a role and carries no
    level. It adds the command actions and `provision` to what the role layer
    already gave that principal.
31. Ownership stops short of `manage` and reaches `send_gcode`. Not `manage`,
    because administering the grants on a device is not a command. It is also
    what stops an owner handing ownership on: whoever administers the device says
    who owns it. It does reach `send_gcode`: an owner may send raw gcode on their own
    printer.
    - **this amends the rule's first form**, which held
      `send_gcode` outside `ownerMask` on rule 8's argument about a payload the
      host cannot read. Rule 8 still puts raw gcode at manager for the role
      layer. Ownership is added after that layer, so an owner reaches it and an
      operator who owns nothing does not.
    - what does not move: the refusal set of rule 6, the parse and byte rules 4
      and 5, and the log line rule 13 writes. An owner sends what any manager
      could send and nothing more.
32. A block on a device removes ownership with everything else. Ownership is
    added after the role layer is resolved, so the deny is applied to both or it
    covers neither, which is the failure rule 6's kind of set exists to avoid.
33. A caller is told when a payload the refusals admit still looks dangerous, and
    the warning travels to whatever is driving. Rule 21's list is
    what the warning is drawn from, so the two are kept in step. A warning is not
    a refusal: rule 7 keeps the denylist to what cannot be undone, and this rule
    carries the rest to a person.

## the safety argument

Three layers, and each one answers a different question.

| layer | question it answers | what it is |
|---|---|---|
| capability | can this printer do it at all | `core.command.send_gcode` in the closed vocabulary, withheld by a profile, narrowed by `Supported` |
| authorization | may this caller do it | `send_gcode`, manager and above, disjoint from `send_command` |
| driver refusal | may anybody do it | the denylist, the ASCII rule and the parse rule, applied where the value reaches the wire |

**Why a denylist and not an allowlist.** An allowlist in the baseline is a
ceiling fixed at compile time. Too narrow, and a customer workflow is
impossible until a release ships, which is worse than a rule an operator can
turn off, because it cannot be turned off. And its marginal safety is small:
gcode's danger lives overwhelmingly in parameters, which no word list reaches.
`M104` is on any plausible allowlist and `M104 S500` is on the other side of
the boundary the allowlist was supposed to draw. The denylist gives the one
thing the allowlist cannot: a short, stable, enumerable set of words that
destroy a machine irreversibly, whose absence breaks nobody's macro.

The mechanism for a deployment that does want an allowlist is rule 3. It is
positive rather than negative, and it costs the operator a list of every code
they do want.

**Why `M500` and `M502` are not refused.** Persisting a calibration is
`M851 Z-1.2` then `M500`, which is an ordinary maintenance macro and the exact
shape a preset exists for. Refusing `M500` breaks it. `M502` destroys a
calibration and costs hours to redo, and it is recoverable, which is the line
this set draws. Both are named in rule 21 rather than quietly permitted.

**What `M28`, `M29` and `M30` are doing in a safety set.** They are not a
safety refusal, they are a confinement one. Writing and deleting files on the
printer has verbs with a path check that ADR-20260825-smx put there, and a gcode
channel that also writes files is a second way into the same place with no
check on it.

## sequence failure semantics

**Stop at the first refusal, report every step, name what was not attempted.**

A sequence is a maintenance macro: home, heat, purge, park. If the heat step
fails, the purge step extrudes cold filament through a cold nozzle. Continuing
past a failure produces a machine state nobody designed for, and the caller who
wants the rest of the steps can send them, having seen exactly where it stopped.

Atomic is a promise this server cannot keep and does not make. There is no
transaction on a printer and no undo for a completed move or a heat, so a
sequence that failed at step three leaves the machine wherever steps one and
two put it. The response says so by construction: it reports two accepted
steps, one refused, and the rest not attempted.

Exclusive is a promise it does not make either. Nothing holds the printer for
the duration of a sequence, so another caller's command may land between two
steps.

## enforcement

One row per gate. The third column is what that case covers, where the name
does not already say it.

| rule | gate | what the case covers |
|---|---|---|
| 1 | `TestTheTwoRawVerbsTravelUnderDifferentFrames` | |
| 1 | `TestAProfileThatWithholdsRawGCodeTurnsItOff` | its second case withholds one verb and keeps the other |
| 2, 4, 5 | `TestARefusedPayloadNeverReachesTheWire` | a smuggled second command, a bare newline, a tab, a null byte, a non-ASCII message, every unparseable spelling |
| 3 | `TestTheRawPayloadArgumentIsBoundedAndRequired` | reads the descriptor, then runs a payload past the bound through `Set.Validate` |
| 6 | `TestARefusedPayloadNeverReachesTheWire` | each refused word |
| 6 | `TestARefusedWordCannotBeReachedByRespelling` | thirteen spellings of three of them |
| 7 | `TestTheRefusalSetStopsNothingItDoesNotName` | fails if the denylist grows into an allowlist |
| 8 | `TestSendGCodeIsAManagerActionAndNotAnOperatorOne` | both directions, against both engines |
| 8 | `TestTheAgentMaskDoesNotCarrySendGCode` | |
| 9 | `TestActionForVerbSeparatesRawGCodeFromEveryOtherVerb` | the verbs that take an action of their own, and four spellings that take none |
| 9 | `TestRawGCodeVerbsAndActionForVerbAgree` | |
| 9 | `TestSendGCodeIsInTheClosedActionSet` | |
| 28 | `TestProvisionIsAManagerActionAndNotAnOperatorOne` | both directions, against both engines, over eight principal shapes |
| 28 | `TestFlashingFirmwareIsAboveOperator` | the same three principals through the real composition root, so the refusal is the device plane's and not a helper's |
| 28, 29 | `TestTheFourIrreversibleVerbsTakeProvision` | both directions: the four take it, and nothing else does |
| 28 | `TestProvisionIsInTheClosedActionSet` | the mask is outside the agent mask and outside the operator level |
| 30 | `TestAnOwnerFlashesTheirOwnDeviceAndAnOperatorDoesNot` | both engines: an owner holding operator flashes, an operator owning nothing does not |
| 30 | `TestAnOwnerFlashesTheirOwnPrinterThroughTheDevicePlane` | the same pair through the real composition root, over two printers one principal owns half of |
| 30 | `TestOwnershipMatchesTheGraphWalk` | the projection against an independently written oracle, which shares no table with the compiler |
| 31 | `TestOwnershipIsInsideTheManagerMask` | the four omissions and the invariant a grant write rests on |
| 31 | `TestAnOwnerCannotHandOwnershipOn` | the refusal and the org admin who can |
| 32 | `TestABlockedOwnerReachesNothing` | |
| 25 | `TestEveryPublishedVerbTakesADecidedAction` | both directions against `internal/printer/profile`, so a new verb fails the build until somebody names its action |
| 25 | `TestEveryClassifiedVerbTakesAPrinterAction` | every entry names an action a printer defines, and no verb is classified twice |
| 26 | `TestAVerbOutsideTheTableIsRefusedRatherThanDowngraded` | through the real graph, on both engines, for a caller holding every printer action there is |
| 26 | `TestEveryPublishedActionIsCheckedSomewhere` | the reachability half: an action nothing checks fails, which is how `cancel_job` would have been caught |
| 27 | `TestAVerbTheVocabularyDoesNotNameIsRefusedWithoutNamingAPrinter` | the two refusals compared as the parameters a caller can branch on |
| 27 | `TestAVerbThePrinterDoesNotHaveNeverReachesTheDriver` | the other case: a verb that exists and that this printer does not resolve |
| 10, 11 | `TestASequenceStopsAtTheFirstRefusalAndNamesWhatItDidNotAttempt` | the unattempted steps never reached the device plane |
| 10, 11 | `TestAPrinterRefusalStopsTheSequenceToo` | the same, for a refusal the printer sent |
| 12 | `TestASequenceIsBounded` | both sides of the bound |
| 12 | `TestTheGenericCommandEndpointRefusesARawGCodeVerbToAnOperator` | the other path to the same verb |
| 13 | `TestEveryRawGCodeCommandIsLoggedWithItsPayloadAndItsCaller` | |
| 18 | `TestTheShippedProfilesCoverTheWholeDriverBaseline` | fails on a baseline verb no profile carries |
| 18 | `TestProfilesMatchTheRetiredMatrix` | regenerates the file and diffs it |
| 19 | `TestASequenceStopsAtTheFirstRefusalAndNamesWhatItDidNotAttempt` | asserts the 200 |
| 19 | `TestAnUnknownPrinterIsRefusedTheSameWayEverywhereElse` | asserts what is not a 200 |
| 14 | `TestAPresetIsWrittenIntoTheOverlayUnderTheSharedVersionToken` | |
| 14 | `TestAPresetAndAnInstanceShareOneOverlayAndOneToken` | a preset write moves an instance's token, and the reverse |
| 15, 23 | `TestAPresetPinnedToAnotherModelIsRefusedAndSendsNothing` | what reached the device plane, and the code and both model parameters a client branches on |
| 15, 23 | `TestAPresetPinnedToTheTargetModelRunsWhateverTheCase` | the same |
| 15, 23 | `TestAPresetNamingNoModelRunsOnAnyPrinter` | the same |
| 16 | `TestReadingAPresetTakesViewResourceAndWritingTakesConfigureResource` | both directions |
| 16 | `TestAPrincipalWithNoGrantOnTheKindLearnsNothing` | |
| 16 | `TestACallerWhoMayRunAPresetNeedNotBeAbleToEditIt` | |
| 16 | `TestACallerWhoMayEditAPresetNeedNotBeAbleToRunIt` | |
| 17 | `TestAPresetRunsOnAPrinterThroughTheSameStopRule` | drives a preset through the sequence path, and the unattempted step never reached the device plane |
| 24 | `TestTheGCodePresetKindIsConfigurableAndNotSupervised` | |
| 24 | `TestEverySupervisedKindIsConfigurable` | |
| 24 | `TestTheGenericResourceSurfaceRefusesAPresetKind` | |
| 24 | `TestAKindWithNothingToSuperviseIsRefusedInTheResourcesBlock` | |

Two rows carry more than the case column holds:

- `TestProvisionIsAManagerActionAndNotAnOperatorOne` runs eight principal
  shapes: an operator, a manager, an org admin, a service account, a node, a
  viewer, a device credential and a principal holding nothing.
- `TestAnOwnerFlashesTheirOwnDeviceAndAnOperatorDoesNot` also asserts that an
  owner reaches no device they do not own.

Every gate above was watched fail on a deliberate mutation before it was
trusted green. The mutations:

- the ASCII check removed
- the refusal set emptied
- the refused letter changed
- the host's capability check bypassed
- the generic path unguarded
- the missing-authorizer branch made permissive
- the stop rule deleted
- the audit line dropped

Rule 20 has no gate. A step-level idempotency key is not built, and the open
items say so rather than the table looking like a promise met.

The preset gates were each watched fail the same way. Their mutations:

- the write grant forced true
- the read grant ignored
- the printer authorization removed from the run path
- the model pin made always satisfied
- the step boundary check removed
- the fencing token check removed
- the instance surface widened back to the configurable set
- the preset read moved ahead of the printer resolution
- the preset kind declared supervised

## rejected

Each heading below is the option, unchanged, so a reviewer can link to the
argument rather than repeat it.

### an allowlist of gcode words in the driver baseline

An allowlist is a compile-time ceiling on what a customer may send. Too narrow
breaks a workflow until a release, and the safety it buys is small, because the
danger is in the parameters. [The safety argument](#the-safety-argument) states
the case in full.

### no refusal set at all, on the argument that the action is the boundary

`D3` writes memory by address and `M997` flashes firmware. A caller who holds
the action still must not be able to ruin a machine permanently by a typo in a
macro.

### refusing `M500` and `M502` as EEPROM and factory reset

`M851` then `M500` is how a Z offset is persisted, and refusing it breaks the
exact maintenance macro presets exist for. `M502` is recoverable by
recalibration, which is the line this set draws.

### a multi-line payload in one command

The host would have to split it to apply the refusals, and a splitter that
disagrees with the firmware's parser by one byte is a bypass. Splitting is the
caller's job and the result is a sequence.

### one verb with a `forced` boolean argument

A profile could then withhold neither half. Two keys are what lets a model keep
queued gcode and lose the out-of-band frame.

### an atomic sequence, or a rollback

A printer has no transaction and a heat has no undo. Promising a rollback would
be false, and a caller acting on the promise would skip the check that tells
them where it stopped.

### continuing a sequence past a failed step

The steps after a failure were written on the assumption it succeeded. Purging
through a nozzle that did not heat is the ordinary case.

### a sequence that holds the printer exclusively for its duration

The device plane has no lease, and a sequence long enough to matter is long
enough to hold a fleet's printer against everybody else. Rule 10 says it is not
exclusive instead of implying it is.

### a non-2xx status for a sequence that stopped

It discards the per-step body, which is the only thing that says where it
stopped and what ran.

### a preset stored per user

A maintenance macro is a property of the machine and the team, not of whoever
typed it. Per-user storage produces thirty copies of one purge sequence,
twenty-nine of which are stale.

### a preset scoped to one printer

A printer identity is minted by the device plane, and a printer re-registering
takes a new one, so a preset pinned to an id dies with the machine it was
written for. The model is the durable thing about a printer, and rule 15
already pins that.

### a preset scoped to one model, as its owner rather than its pin

It is the same set as rule 15's pin and one more place to keep it. A macro for
two models would then be two presets.

### running a mismatched preset with a warning in the body

The warning is in the response, which arrives after the steps did. `G1 Z-100`
on a printer with a shorter Z is not a warning.

### a force flag on the run request rather than on the preset

Whether a sequence goes ahead of the print queue is part of what the sequence
is, and a caller who may amend it at the call site is running something other
than what was reviewed.

### a preset written through the resource instance surface

It would need a `type` naming an implementation nothing registers, and the
supervision plane would be handed an instance that can never become ready.

### a preset file of its own beside the overlay

Two writers of two files under two locks, and no token that covers both. One
file is what makes a preset write and an instance write serialise.

### validating a preset only when it is written

The printer it will run on may not have arrived yet, and its capability set
moves on every reconnection.

### a preset that runs on any model

Gcode is model-specific. A CoreOne chamber command on an MK4 is at best refused
and at worst a move into a wall.

### an action per verb

The action set is closed and every new verb would widen it. An action per verb
class that is qualitatively different is the narrowest change that answers the
question, which is two of them: `send_gcode` for a payload the host cannot
read, and `provision` for the four verbs it cannot undo.

### reusing `send_command` for firmware flashing

An operator runs prints on a fleet they do not own. Replacing the firmware on
one of those machines is not part of running a print, and rule 6 already
refuses the same outcome inside a raw payload. Leaving the four under
`send_command` held one outcome to two different bars.

### making ownership a role level above manager

An owner who holds only operator on their own machine is the case rule 30
exists for, so ownership has to add to a role rather than replace it. A level
above manager would also make every owner an administrator of the grants on
their device, which is wider than ownership needs. It would also let an
owner hand ownership on with nobody administering the fleet involved.

### making an administrator an owner, with a tuple per device

An org `admin` already reaches `provision` on every device in the org through
the manager level, so the tuples would change no answer this server gives and
would grow with the fleet. The two readings of "plus the admins" separate only
when ownership confers something an administrator does not have, and that
question is open.

### a migration that gives every existing printer an owner

The only owner it could pick is the bootstrap principal or the org
administrators, who already reach `provision`. It would confer nothing and would
write a false record of who owns a machine, which is worse than a printer that
is honestly unowned.

### a level of its own between operator and manager for the four verbs

It would be a fourth role nobody grants. The role layer is three relations
written on printers, printer groups and orgs. A new level means a new relation
on every one of them, a new org role, and a deployment that has to re-grant to
use it. `provision` at manager reaches the same set today. What a further
level would have to answer first is an open question.

### reusing `send_command` for raw gcode

Granting an operator the ability to pause a print would grant them `M502`. The
two are not the same permission and nothing about the role model says they are.

## open items

- `send_gcode` leaving the core action vocabulary is unbuilt. It is a wire
  change, so it owes [ADR-20260825-zkq](20260825-zkq-api-compatibility.md) a migration note,
  and [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
  carries the gate. Rules 16 and 31 here name the action by its current
  spelling, and the dialect-free name is what they will read
- Nothing writes an owner tuple on its own, so in a shipping deployment every
  printer is unowned. The rule names the approver as the owner, and the core has no
  approver. `Driver.Approve` and `Driver.Pending` are methods on the xBuddy
  driver with no production caller, and no API route reaches them. A printer is
  admitted with no person involved, because `auto_register` defaults to true.
  Rules 30 to 32 are what an administrator then assigns through the grant
  surface. The approval surface is an open question.
- Every printer registered before rule 30 is unowned, and no migration guesses
  an owner. The reason is in
  [the rejected migration](#a-migration-that-gives-every-existing-printer-an-owner).
- A verb outside the core namespace carries no action, so a driver's own verb
  reaches no printer. Rule 26 is what makes that true. The direction is the safe
  one: the alternative hands an unclassified verb to whoever holds
  `send_command`. It costs nothing today, because the one driver in this tree
  declares core verbs alone. ADR-20260827-rqc is the mechanism a third party would use
  to declare a permission, and it is proposed rather than built.
- `core.command.start_connect_download`, `start_encrypted_download` and
  `start_inline_download` take `upload`, which is what makes that action
  reachable. No driver here can send them. They are in
  `internal/printer/profile` and in no `wireTable`, so such a command is
  authorized and then refused with `core.printer/CAPABILITY_NOT_SUPPORTED`. The
  classification is the one an upload path will need, and it is not one yet.
- A preset carries no per-step idempotency key, so rule 20 is stated and not
  implemented. A replayed run is a second run.
- Nothing rate-limits a preset run any more than it rate-limits a raw sequence,
  and a preset is 64 steps a caller reaches with one request.
- The model pin is compared against the model string a printer reported, with
  no normalisation beyond case folding and trimming. A profile that renames a
  model between firmware releases silently stops matching a preset written for
  the old spelling.
- A build that predates presets refuses an overlay that carries one, because
  the overlay decoder rejects unknown fields and the format version was not
  moved. That is the designed refusal reached by an undesigned message.
- The preset scenario is proved against the real binary by a person and has no
  entry in `test/e2e/scenarios_test.go`.
- There is no per-deployment way to spell a denylist. Rule 3 offers an
  allowlist in a profile, which an operator who wants `M502` refused has to
  express as every code they do want.
- A profile file is generated and there is no operator-authored profile, so
  "a deployment narrows `Supported`" is a mechanism with no file behind it.
  `profile.Load` exists and nothing calls it with an operator's data.
- Nothing rate-limits raw gcode. A caller holding the action can fill the
  driver's command queue. That queue is bounded and drops rather than blocking,
  and the bound is the driver's rather than a policy anybody set.
