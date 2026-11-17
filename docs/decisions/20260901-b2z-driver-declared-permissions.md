---
title: "ADR-20260901-b2z: driver-declared permissions"
description: "A driver declares its own verbs, each verb carries a class from a closed core set, the floor is operate because every verb is a write to the transport, the verb table gains a device-kind dimension with a per-build half, dangerous verbs take an explicit per-verb grant that reuses the declared-grant table, view_audit is defined as the audit-sight action, and the second grant system goes. Trust has one tier: the build."
date: 2026-09-01
status: accepted
amends: [ADR-20260831-c7b, ADR-20260827-rqc, ADR-20260825-z65, ADR-20260831-nbj, ADR-20260825-bhe]
tags:
  - adr
  - authorization
  - devices
  - extensibility
  - modules
  - security
---
# ADR-20260901-b2z: driver-declared permissions

- status: proposed. Every gate is owed
- date: 2026-09-01

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md) rules 11, 12, 13, 16, 17, 18, 19, 20 and 33 to 36, [ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md) rules 1, 3, 4, 5, 9, 11 and 12, [ADR-20260825-z65](20260825-z65-raw-gcode-and-sequences.md) rules 8, 28 and 29, [ADR-20260831-nbj](20260831-nbj-a-module-declares-a-device-kind-and-the-host-places-it.md) rules 14 and 18, and [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule 4 in part | Upholds ADR-20260825-z65 rules 9, 25 as read in the amendment table, 27, 30 and 31, and [ADR-20260827-2bp](20260827-2bp-a-module-travels-as-a-verified-artefact.md) rule 5, which this record does not touch | Builds ADR-20260831-nbj rule 15 and ADR-20260828-xdp rule 8's split | Builds on [ADR-20260825-dee](20260825-dee-the-capability-vocabulary.md), [ADR-20260827-wgm](20260827-wgm-a-caller-may-ask-what-it-may-do.md) | Reads with ADR-20260831-c7b rules 5 to 9 and 21 to 32, which stand, and with [ADR-20260901-atx](20260901-atx-durable-command-records.md), whose rule 12 uses the `view_audit` action rule 3 here defines | Arms [ADR-20260825-zkq](20260825-zkq-api-compatibility.md) at first release (rule 41)

A driver declares its own verbs, in its own namespace, and each verb carries a
class from a closed set the core owns. The core never learns what a verb means.
It checks that the verb is declared for the device's kind, that its class is a
member of the closed set, and that the caller's grant carries that class.
Grants name classes, held in the one tuple system that exists today. The single
exception is the dangerous tier: a verb no role confers, reached only by an
explicit row naming that verb.

The requirement: a community adapter's verbs dispatch and are grantable with
no core source edit. It is ADR-20260831-nbj rule 15 finally built, plus two corrections
to the first sketch: `observe` is not a verb class, and the verb table takes a
device-kind dimension.

## why observe is deleted as a verb class

This is the reasoning the whole model stands on, so it is stated before the
rules.

Answering even a pure read puts a command on a wire that carries one command at
a time. `internal/authz/vocabulary.go` already says so in the comment that
explains why `send_info` sits in `send_command`. **Every verb is a write to the
transport, whatever it does to the device**. Observation, telemetry, state,
subscription, is a different surface with a different object, and it stays
where it is (`Visible`, the read mask).

Once no verb can carry `observe`, the attack the closed table existed to stop
has no target: there is no read-shaped rung to hide a destructive verb in. The
floor is `operate`, because there is no lower rung, that is the honest claim,
and it is the whole claim. It does **not** follow that `operate` is a proven
ceiling for a stranger's semantics. `stop_print` sits where it sits because a
person weighed it (the table's own comment records the weighing), and nobody
has weighed a stranger's `factory_reset`. What answers that gap is not the
class lattice. It is rule 21's host-computed marking, rule 15's operator
marking, rules 22 and 23's pin, and rule 24's readable inventory. The
residue past those is named in the open items rather than argued away.

## trust has one tier: the build

The owner has ruled out live adapter loading for this release. An adapter is
compiled in, always. Someone writes a build configuration that names the
adapter module, and the build pulls it and links it. The resulting binary is
the core exactly as it was plus the adapter. There is no install step, no
runtime admission, and no signature. ADR-20260827-2bp rule 5's own enforcement row
records that nothing signs anything yet, so a trust model keyed on signatures
would be a table the binary cannot evaluate.

So provenance has exactly one answer: **a kind's verbs are in the vocabulary
because someone chose to build them in.** The build is the trust act. `go.sum`
already pins every module's content by hash, and the module path is the
publisher's name. A four-tier provenance table, a ceiling clamp per tier, a
quarantine and an install-time confirmation were all designed for admission at
runtime. With no runtime admission they defend nothing, and they are deleted.
Earlier research's out-of-process design is **deferred, not adopted**: it remains
the likely shape of live loading if the owner ever wants it, and nothing here
precludes it.

One direction note survives the deletion, because it corrects the earlier
draft's reasoning. A ceiling defends against a declaration claiming **too
much**. The attack this model is actually about is a declaration claiming
**too little**, hiding a destructive verb where a harmless one lives. No
ceiling can touch that. The rules that do the work are the floor (rule 4), the
host-computed `opaque_payload` marking (rule 21), first-sight pinning (rules 22
and 23) and the served inventory (rule 24). Rule 21 carries the most weight,
because it is the only check a lying declaration cannot route around.

What this record does not decide: the build-composition mechanism itself. That
covers how a separate Go module reaches the core's registries when Go's
`internal/` rule forbids importing them. It also covers how the roughly nine
core-side files a new adapter touches today collapse into one build
configuration. That is a separate record;
the open items name what it must cover. This record's rules are written so
that nothing in them changes when it lands.

## the rules this record amends

Each entry names the rule, why it blocks the requirement, what it protected,
and what protects that thing now. That is the bookkeeping bar. The inline sub-bullets on the old records are placed at merge, per
the amendment map §5.2.

| rule | why it blocks | what it protected | what protects it now |
|---|---|---|---|
| ADR-20260831-c7b rule 11, a declared grant is a row beside the tuples | a second grant system: two write paths, two revocation stories, two audit surfaces, AGENTS.md's named top bug class | an operator reads a name, and a revocation has a row to delete | rule 38 reads what an operator holds from one system; the dangerous tier keeps a named row per verb there (rule 26) |
| ADR-20260831-c7b rules 12, 13, 33 to 36, the name-onto-bits mapping | they map permission *names*, never the *verb* table that blocks a community adapter; finishing them still edits core | a projected row carries core bits only, so the list filter keeps one predicate | unchanged: classes are core bits, the projection compiles tuples as today, the predicate keeps one parameter (rule 32) |
| ADR-20260831-c7b rule 16, revoke by deleting the row or retiring the name; never reuse a retired name | moot with the name mapping | a name a second adapter can claim after an uninstall must not come back meaning something else | never-reissue survives whole: rule 22's first-seen record applies it to a verb's class, keyed against a kind rename |
| ADR-20260831-c7b rule 17, one core action for an opaque payload, spelled without a dialect, at manager | a level is a blunt instrument: it hands the payload to every manager of every device | a payload nothing inspected does not reach a caller by holding an ordinary role | the dangerous tier (rules 16, 17, 21): no class or level confers the payload verb; reaching it takes a per-verb row |
| ADR-20260831-c7b rule 18, refuse a mapping that places a payload action below manager | the level model it floors on goes | the manager floor was the only check left before an act nobody reviewed | rule 21 keeps the gate, stronger: a verb the host finds unconstrained is marked `opaque_payload` and conferred on nobody |
| ADR-20260831-c7b rule 19 (owner's), add two actions to the core floor: reading what a device produced, and clearing a fault | the class set has no room for either as a distinct action, and adding rungs is what this record refuses | an axis the floor omits is one every adapter maps onto the wrong action | clearing a fault is a declared verb at `operate`; reading media is **deferred**, `observe` for now, row on the old backlog |
| ADR-20260831-c7b rule 20 (owner's), keep the three job actions; a camera ignoring them costs nothing | the level model the three actions live in goes, and a grant that confers nothing is a grant an operator misreads | job management stays a common concept with common names | `upload` and `cancel_job` fold into `operate`, no reach change; `fetch_job` stays the agent's; rule 38 renders it (P3 stands) |
| ADR-20260827-rqc rules 1, 3, 4, 5 (owner's), 9, 11, 12 | all are mechanism for a declared permission *name*, and no declared permission names exist under this record | who holds a new permission the first time anybody hears of it | nothing is held on declaration: a verb enters a class existing grants scope; the dangerous tier starts empty (rule 16) |
| ADR-20260825-z65 rule 8, `send_gcode` enters at manager | the level model itself goes; and the level was the wrong tool (see ADR-20260831-c7b rules 17 and 18 above) | a payload the host cannot read is not an operator's to send | tightened: `send_gcode` and `force_gcode` are dangerous-tier (rules 16, 40), explicit grant only; none shipped (rule 41) |
| ADR-20260825-z65 rule 28 (owner's), the four verbs take `provision`, above operator | `provision` ceases to exist as an action name | what changes the machine, and cannot be undone, is not an operator's | kept: the four are `configure` marked `irreversible`, above operator and manager-by-role; explicit grant or owner (rule 19) |
| ADR-20260825-z65 rule 29, `provision` is one action for the four and not one per verb | the dangerous tier is exactly one grant per verb, the shape rule 29 refuses with reasons | four grants where one would do is toil, and a per-verb grant invites naming actions at call sites | ownership implies every dangerous row on the owned device (rule 19); for everyone else the per-verb row is the point |
| ADR-20260825-z65 rule 25, the table behind `ActionForVerb` names every verb the published vocabulary carries, and is total | totality of one static table cannot survive a second half | no default arm decides for a verb nobody weighed | read forward: totality holds over the union, one class per kind per verb, no default arm, all published (rule 24) |
| ADR-20260831-nbj rule 14, a module verb sits on `send_command` at the weakest; `send_gcode` or `provision` is refused | the names it uses cease to exist | an install does not grant itself a dialect escape or a provisioning act by declaring one | rule 4: the floor is `operate`, no verb declares `administer` or `view_audit`; rule 18: nothing clears a host marking |
| ADR-20260831-nbj rule 18, keep the per-kind permission reachable and build nothing for it | superseded by building the per-kind verb table instead of a per-kind permission column | opening later must rewrite no stored grant | no stored grant moves: the per-build half is a new table dimension, tuples are untouched (rule 5) |
| ADR-20260825-bhe rule 4, in part, a vocabulary is a static table, not a map assembled by `init()` | a compiled-in adapter's verbs reach the table through the `init()` registration every extension descriptor uses | zero heap and zero start-up registration for what the build already knows | the generated half stays static; the per-build half is gathered at start from the binary's own registrations (rule 5) |
| a design requirement, an extensible role set, no enum in the core | the shipped code violates it; nothing reconciled it with ADR-20260825-bhe | roles are not one vendor's roles | rule 45: the *permission* set is extensible (declared verbs); the *class* set stays closed, placement being escalation |
| a design requirement, no per-vendor special cases above the driver | the 59-row `core.command.*` table is one vendor's verb list compiled into the authorization package | one addressing model for every device | satisfied: the printer's verbs become the generated half keyed to kind `printer`, one kind among others (rule 5) |

Read again, unamended, because a reader will ask: **ADR-20260825-z65 rule 27**, the
refusal of an unknown verb happens before a printer is resolved and carries no
fact about a printer. Rule 6 here is built to keep it, and the two-stage check
there is this record's answer for how. **ADR-20260825-z65 rule 31**, ownership reaches
`send_gcode` and stops short of `manage`; rule 19 implements it and rule 47
names the shipped code that still contradicts it. ADR-20260827-rqc rules 13 (the
relation vocabulary stays closed) and 15 stand. ADR-20260825-z65 rules 9 (the action is
derived from the verb by one function, this record only adds the kind) and 30
stand. ADR-20260831-c7b rules 5 to 9 (states, transitions, guards), 21 to 32
(conditions, the hold, the refusals) and 37 to 38 stand. Rule 37's "declared
grant" means the per-verb row of rule 17 here, and both of its masks survive.
ADR-20260831-nbj rules 12, 13, 15 and 16 stand, which this record builds rather than
amends. ADR-20260827-2bp rule 5 (owner's) stands: the three signing levels stand as
what a deployment *accepts*, and this record simply stops pretending anything
evaluates them before something signs. AGENTS.md's remaining never-pluggable
items stand: the graph, its relation vocabulary and the class set stay closed.
Only the verb table opens, and the verb table is not the graph.

## decisions this record changes

One row per earlier decision, with this record's claim about it.

| decision | status here |
|---|---|
| an administrator holds a permission an extension declares | **overturned.** No declared permission names exist, so nothing is held on declaration; a verb enters a scoped class |
| a declared grant is a row beside the tuples, the second grant table | **mechanism changed; arguably satisfied.** that table is the per-verb dangerous tier (rule 26) |
| a declared permission may be granted at operator | **subsumed.** every class from `operate` up is grantable at operator or above by tuples, a superset of the request |
| no per-kind permission yet, with adapters and modules extending them later | **satisfied differently.** no per-kind permission *name* is added; the extension arrives as the per-kind verb *table* |
| flashing sits above operator, and an owner keeps it on their own printer | **mechanism changed, intent kept.** the four go dangerous-tier; owners keep them by rule 19 |
| an owner may send raw gcode on their own printer, ADR-20260825-z65 rule 31 | **implemented at last.** ownership reaches every dangerous verb, gcode included (rule 19); rule 47 pins the mask defect |
| keep the three job actions; a camera ignoring them costs nothing, ADR-20260831-c7b rule 20 | **amended.** `upload` and `cancel_job` fold into `operate`, no reach change; `fetch_job` stays the agent's; `jobs` facet rejected |

## rules

### the requirement

1. A community adapter's verbs dispatch, are checked and are grantable with no
   edit to core source. Every other rule here is measured against it. The adapter is a Go module a build
   configuration names. The mechanism that links it without core edits is a
   separate record (open items), and nothing in this one depends on its shape.
2. The core validates that a message is well formed and speaks the common
   language where one exists. It never asks what a verb means to the machine.
   Which commands exist, and what telemetry is stored, are the adapter's.

### the closed class set

3. A verb carries exactly one class from a closed set the core owns. The set is:

   | class | what it authorizes | replaces |
   |---|---|---|
   | `operate` | send any verb of this class: act on the machine in its normal duty | `send_command`, `cancel_job`, `upload` |
   | `configure` | send any verb of this class: change what the machine *is*, not the work on it | `provision` |
   | `administer` | administer the grants on the object. **No verb may carry this class** | `manage` |

   Two actions sit beside the classes and are never carried by a verb:

   - `observe`, read telemetry and state, subscribe. It replaces
     `view_telemetry`. The reasoning is the section above: every verb is a
     write to the transport.
   - `view_audit`, read audit-class events: who authorized which command on
     this device. It is new, minted here because
     [ADR-20260901-atx](20260901-atx-durable-command-records.md) rule 12 gates the `authorized`
     frame on it and nothing else defines it. It is conferred at manager
     (rule 33) and by nothing lower. An operator is a device viewer, and
     conferring audit sight at operator would restore exactly the widening
     ADR-20260901-atx rule 12 refuses. That widening is every device viewer learning
     which operator sent which command.

   The agent pair, `report_telemetry` and `fetch_job`, is unchanged and stays
   reachable from the `agent` relation alone. The resource and storage action
   sets are unchanged.
4. The lowest class a verb may carry is `operate`. There is no read class for a
   verb, and a declaration naming `observe`, `view_audit` or `administer` on a
   verb is refused when the declaration is validated (rule 11).

### the verb table and the kind

5. `ActionForVerb` takes the device kind:
   `ActionForVerb(kind ref.DeviceKind, verb string) Action`. It is backed by
   two halves, on ADR-20260828-xdp rule 8's split, which is ADR-20260831-nbj rule 15 built
   with one word moved: install becomes build. The **generated half** is the
   59 printer rows, still a static Go table under ADR-20260825-bhe rule 4, keyed to
   kind `printer`. The **per-build half** is rows gathered once at start from
   the declarations compiled into this binary, keyed by `(kind, verb)`. They
   are registered the way every extension descriptor already registers, and
   never read from configuration or from a running process.
6. The check refuses in two stages, so that ADR-20260825-z65 rule 27's uniform answer
   is kept rather than quietly given up:
   - **Stage one, before the device is resolved.** The verb is looked up in
     the **union** of every compiled-in kind's verbs, both halves together. A
     verb the union does not hold is refused with `core.printer/VERB_UNKNOWN`,
     carrying the verb and no fact about any device. That is the same answer
     whatever the caller holds and whether or not the id names a machine. The union is
     part of the published vocabulary (rule 24), served to a caller holding no
     credential, so the refusal discloses nothing the vocabulary route does
     not.
   - **Stage two, behind the read grant.** A verb the union holds but the
     resolved device's kind does not declare is refused with a distinct fault
     naming the kind mismatch. That fault reaches only a caller who holds at
     least `observe` on that device. A caller holding nothing receives the ordinary
     access refusal, indistinguishable from a device that does not exist. So
     the device-kind oracle the single-stage design opened is closed: what a
     caller learns from stage two is the device's kind, which the read grant
     already discloses.

   A `(kind, verb)` pair nothing declared still yields the zero `Action`,
   `Valid()` is false, and no default arm exists, the fail-closed property
   does not move. The shelf's verb does not exist for kind `printer`, and no
   global profile leaks it there.
7. The kind reaches `capability.Resolve`: its `Tiers` gains the device kind so
   profile lookup is per kind. This is the only other core signature that
   moves. One invariant is stated because rule 21 depends on it. The resolved
   live capability set is produced by intersection only (`Resolve` narrows,
   never widens), so a unit's probe can never widen an argument beyond the
   declaration rule 21 marked. A resolver change that could widen is a change
   to this rule.
8. `CanonicalVerb` suggests only from the same union stage one reads, so every
   caller gets the same suggestion for the same spelling, and it still never
   invents a verb.

### the declaration

9. A kind declaration (ADR-20260831-nbj rule 8, as extended by ADR-20260831-c7b rules 5 and 6)
   gains, per verb, a `class` and an optional `dangerous` marking. There is no
   facet field. Example:

   ```yaml
   kind: acme.io/filament-shelf
   display_name: Filament shelf
   verbs:
     acme.io.filament-shelf.command.set_slot_attention: {class: operate}
     acme.io.filament-shelf.command.calibrate_scale:    {class: configure}
     acme.io.filament-shelf.command.factory_reset:      {class: configure, dangerous: irreversible}
   ```

10. `sdk/connectorapi` carries the declaration types, the closed class enum
    and the per-verb entry, so an adapter author writes the declaration
    beside `Baseline()` and the conformance suite reads it. The namespace is
    derived by the host (`extension.NamespaceOf`) and one passed in is
    refused, which is ADR-20260827-rqc rule 16 and ADR-20260831-c7b rule 4 unchanged. ADR-20260828-xdp
    rule 2's "never from a running module" binds modules the binary did not
    carry. A compiled-in declaration is the binary's own data,
    the same trust domain as the generated half.
11. The core validates a declaration when the registry drains at start, and
    never at the first command. That is ADR-20260831-nbj rule 16's timing, moved from
    install to start because there is no install. It checks the verb's syntax, and
    that its namespace matches the declaring module. It checks that the class
    is a member of the closed set and at or above `operate`. It checks that a
    verb rule 21 marks dangerous is marked so, and that no class moved
    downward since first sight (rule 23). A declaration failing any of these refuses that kind, its
    verbs classify nowhere and its health condition names the refusal, and
    never the boot. The adapter's own conformance suite runs the same
    validation at its build, so an author learns of a refusal before any
    deployment does.

### trust: the build, and what defends a lying declaration

12. A declared class is a request the host validates, never a fact, and
    nothing may ever declare `administer`. The owner's threat still holds:
    placing a name into the lattice is the escalation decision, so the lattice
    stays the core's. Beyond that there is no provenance ceiling, because
    there is no provenance ladder. **A kind's verbs are checkable because
    someone compiled them into this binary**. The build is the trust act,
    `go.sum` pins what was built, and the module path names who wrote it.
13. What defends against a declaration that lies low, a destructive verb
    declared where a harmless one lives, is not a ceiling, which only stops
    lying high. It is four mechanisms, each cheap. The floor (rule 4) leaves
    no read-shaped rung to hide in. The host-computed marking (rule 21) is the
    one check a declaration cannot route around. First-sight pinning (rules 22
    and 23) means a rebuild cannot silently widen reach under existing grants.
    The served inventory (rule 24) lets the person who added the module to
    the build, who has very likely not read its verb table, read it after.
14. Out-of-process execution is not part of this model. Earlier research measured
    it viable and cheap. Compile-time composition is chosen instead, so
    it is deferred whole, and no rule here reads a runtime, a digest or an
    artefact path.

### markings

15. A verb may be marked `dangerous`, as `irreversible` or `opaque_payload`.
    Three hands hold the pen, and they are ordered. The **host** computes
    `opaque_payload` (rule 21) and its marking is cleared by nobody. The
    **operator** may mark any verb, and an operator marking is cleared only by
    an operator. The **adapter** may mark its own verb, raising its own bar is
    always allowed, and may never clear a marking it did not make. The
    operator's pen is what answers the two cases no computation reaches. One
    is a closed argument set hiding a destructive value
    (`Supported: ["normal","factory_wipe"]` is bounded and not harmless), and
    a zero-argument verb (`factory_reset` with no arguments is trivially
    constrained). The inventory of rule 24 is where an operator reads what to
    mark.

### the dangerous tier

16. A verb marked `dangerous` is conferred by no class, no role and no level.
17. Reaching a dangerous verb takes a row naming it: `(subject, device, verb)`,
    written by `PUT /api/v1/grants/device/{id}/verbs` with `{verb, subject}`
    and revoked by deleting it. This is the only place a verb name appears in
    a grant; everywhere else grants name classes.
18. Nothing but the marker clears a marking, and the host's marking is cleared
    by nobody (rule 15's ordering, stated as its own rule because grants
    depend on it). A change that removes a marking is rule 23's loud case,
    inert until accepted.
19. Ownership reaches the dangerous tier on the owned device. The relation
    `device#owner` implies one per-verb row per dangerous verb of that
    device's kind, computed and not stored. This implements the owner's
    clauses of 2026-08-27 (`provision` on the owned machine) and 2026-08-28
    (raw gcode on the owned machine) in one mechanism, and retires the
    `ownerMask` exception. Ownership still stops short of `administer`, which
    is ADR-20260825-z65 rule 31 kept whole.
20. The dangerous tier fails closed twice: an absent row is a refusal, and a
    verb that no longer classifies (rule 6) is refused before the row is read.
21. The host marks a verb `opaque_payload` itself, from the declaration's
    capability descriptor, whatever the declaration says. A verb is marked
    when any of its arguments is unconstrained, and an argument is
    unconstrained when any of these holds:
    - its kind is `KindOpaque`, a value the host passes through untouched. Its
      own doc says `Supported` and `Limit` do not apply to it, so it can
      never be anything but unconstrained;
    - it is a string with no `Supported` set;
    - it is a numeric kind with no declared magnitude bound. `HasLimit` is
      what bounds a number (the `Limit` is a magnitude, so it bounds both
      sides). A `Min` alone leaves the value unbounded upward and does not
      count.

    The descriptor rule 21 reads is the declaration the registry holds, the
    same object command validation resolves from, which rule 7's intersection
    invariant guarantees is never widened at runtime. This is ADR-20260831-c7b rule
    18's owed gate (*"a module verb carrying an unconstrained argument is
    refused a placement below manager"*) turned from a level rule into a tier
    rule. The conclusion is stronger: the marked verb is conferred on nobody
    rather than on every manager. The marking is a payload-shape check and
    nothing more. It does not, and cannot, decide whether a *constrained*
    verb is harmful. Rule 15's operator pen and rule 24's inventory are the
    answer to that, and the residue past them is named in the open items.

### first sight is pinned

22. The host records the first-seen class and marking per
    `(module path, kind, verb)` in a never-truncated record in its own store,
    repurposing `authz_declared_retired` and carrying ADR-20260831-c7b rule 16's
    never-reissue principle forward. The module path is in the key so the
    record survives what a declaration author controls. Renaming the *kind*
    does not shed the module's history (rule 23's third case). A kind name
    that reappears from a **different** module path than first seen is refused
    outright, because two publishers do not share a kind. That is the same
    refusal `holdKind` already makes at the directory (ADR-20260831-nbj rule 10) and
    Android's dangling-custom-permission lesson kept. The refusal is not
    permanent. A legitimate module rename is accepted by the operator through
    rule 23's acceptance route, naming the old and new module paths. The old
    record is retired onto the new key with every first-seen class and marking carried
    over unchanged, so acceptance moves the publisher's name and never a pin.
23. At start, each compiled-in declaration is compared to the record:
    - a class **raised**, or a marking **added**, applies silently, reach
      only narrows;
    - a class **lowered**, or a marking **removed**, does not apply. The verb
      keeps the recorded class and marking, and the kind's health condition
      names the disagreement per verb with old and new. The change applies
      only after an operator accepts it by name through the grants surface.
      There is no interactive prompt, because there is no install step, the
      pin simply holds until a person moves it. The one genuinely silent
      supply-chain move is a rebuild pulling a v2 that quietly moves `flash`
      from `configure` to `operate` under grants written against v1. This
      turns it into a visible, inert difference instead of a widened grant;
    - a **new kind** from a module path the record already knows applies, and
      is noted in the inventory and the module's health condition. The
      **exception** is any verb whose class or marking is weaker than the
      record holds for a same-named verb on a kind this module declared
      before. That verb is held exactly as a lowered class is. Otherwise a
      kind rename would be a one-directory-cheaper spelling of the move the
      pin catches (v2 renames the kind and lowers `flash` in one commit). No
      collision, applies whole.

    The acceptance route is one surface, minted here:
    `POST /api/v1/grants/kinds/{kind}/acceptances` with `{module, verb, from,
    to}`, `verb` omitted for rule 22's module-path acceptance. An acceptance
    is a row the inventory (rule 24) renders beside the disagreement it
    settles; rule 17's PUT writes verb grants, never acceptances.
24. The served authorization vocabulary
    (`GET /api/v1/vocabularies/authorization`, ADR-20260828-xdp rule 8's split) lists
    both halves. They hold every kind, every verb, its class, its markings
    and who marked it, and the first-seen record where it disagrees. This is
    the inventory the person who added a module to the build reads after the
    fact, and it is one surface, not four. The earlier draft's install
    report, quarantine notice, confirmation prompt and clamp printout are all
    deleted with the install step they decorated.

### upgrade and removal against written grants

25. Class grants (tuples) never dangle: they name classes, not verbs, so no
    rebuild invalidates one. What an existing grant *reaches* may narrow when
    a class rises; it widens only through rule 23's accepted change.
26. `authz_declared_grant` is repurposed as the dangerous tier's table: its
    `permission` column becomes `verb` and its shape survives. The
    name-mapping machinery around it, `InstallDeclaredPermissions`,
    `authz_declared_permission`, and the mapping half of
    `authz_declared_retired`, is deleted: 1,678 lines today across
    `declaration.go`, `declared.go` and `declaredgrant.go`, most of which go.
    The table is empty everywhere. `WriteDeclaredGrant` exists and executes
    an insert, but no caller outside its own tests exists anywhere in the
    tree. Nothing has shipped, so the development
    migration is squashed: the table is dropped and recreated with the `verb`
    column, and nothing converts.
27. A rebuild that removes a verb leaves its dangerous-tier rows in place and
    inert: the verb no longer classifies, so rule 20 refuses before the row is
    read. The rows confer nothing and remain visible to the operator, who
    deletes them or waits for the verb to return.
28. A change that marks a previously unmarked verb starts that verb with no
    rows: no subject held it by name before, so none holds it after. The boot
    log and the inventory list every subject whose class grant stopped
    reaching the verb.
29. A build that drops a kind entirely leaves its dangerous-tier rows inert
    under rule 27's logic and keeps the first-seen record forever. A kind that
    comes back is checked against the record like any change (rules 22
    and 23), so a kind that returns meaning something else is held, not
    trusted.

### the graph surface

30. The tuple engine stays, plainly. Not for throughput, the owner is right
    that one command per multi-hour job on 400 printers makes read cost
    irrelevant. It stays because the materialised `device_access` projection
    is what a design requirement stand on. A design requirement's federation needs
    the recursive machinery later, and the engine is written and
    differentially validated. Replacing a validated authorization engine to
    save runtime the server never spends is refused.
31. The surface shrinks. `group` and `device_group` enter
    `administrableTypes` and become writable through the one grant API, both
    are fully built and tested and merely unexposed. `blocked` is deleted: a
    deny relation that cannot be written is a deny relation never debugged.
    `node` and `device_credential` are deleted as declared types and return
    with the code that uses them. `service_account` stays: a design requirement
    demands it and it is next, not never.
32. The relation vocabulary and the class set stay closed, the list predicate
    stays `(actions & ?) <> 0` with one parameter, and the projection
    compiles tuples exactly as today. ADR-20260827-rqc rule 13 stands.
33. Levels confer masks the way `level.deviceMask()` confers action masks
    today: viewer confers `observe`; operator confers `observe` and `operate`;
    manager confers those plus `configure`, `administer` and `view_audit`. No
    level confers the dangerous tier.
34. The grant `Bootstrap` writes for a `history:<id>` principal
    (ADR-20260901-rf2 rule 6) confers `observe` and `view_audit` on **every device
    origin, present and future**. It is relation-shaped over the device type,
    the way the `agent` relation already sets its pair, never an enumerated
    origin list. It confers nothing else, never a manager level, because a
    provider must read audit without holding `administer` on anything. A device
    registered later is covered without re-granting, and ADR-20260901-rf2 rule 6's
    per-class degraded check stays honest: a per-origin hole is not
    expressible here. Without this, ADR-20260901-atx rule 12's filter would silently
    starve every provider consuming the `audit` class, the failure ADR-20260901-rf2's
    own open item names.

### grant ergonomics

35. `GET /api/v1/auth/accounts` renders each account's subject in tuple
    spelling (`"subject": "user:<id>"`), so an admin copies instead of
    guessing.
36. `POST /api/v1/auth/accounts` takes an optional `role`, `viewer`,
    `operator` or `admin`, applied as one org tuple in the same transaction,
    so one call creates a usable person.
37. `GET /api/v1/grants/subject/{subject}` answers what a subject holds, read
    from the projection the point check already uses.
38. What a person holds is answerable in one place: classes from the tuples,
    dangerous verbs from the per-verb rows, listed together by rule 37's
    route, and rendered **per kind**. The answer for a subject on a shelf
    lists the shelf's declared verbs their classes reach, so "operator on a
    shelf" shows what it is. That is the display problem the rejected
    `jobs` facet was built for, solved where display problems belong.
39. Grants reach groups: a class of students is one tuple on a `group`, a
    room of printers is one `device_group` object (rule 31).

### migration

40. The printer's 59 verbs keep their exact discrimination in the generated
    half, keyed to kind `printer`. The 49 `send_command` verbs, `stop_print`
    and the three `start_*_download` verbs become `operate`, 53 rows.
    `flash`, `upgrade`, `set_token` and `set_hostname` become `configure`
    marked `irreversible`. `send_gcode` and `force_gcode` become `operate`
    marked `opaque_payload`. Nothing widens: every one of the 53 `operate`
    verbs is at the operator level today, `level.deviceMask()` confers all
    three folded actions there.
41. There is no stored-grant migration. Nothing has shipped and no tuples exist outside development databases, so the vocabulary
    is rewritten in place and the development migrations are squashed. The
    retired action names never appear in any release: no dual reading, no
    refusal naming a replacement, no narrowing report, because there is no
    client to serve one to. Owed at release: from the first release forward ADR-20260825-zkq
    binds the served vocabulary as written, and a name that leaves it *then*
    takes a deprecation cycle.
42. Ownership assignment at registration is a feature to schedule before
    first release, not a migration clause. The graph carries `RelOwner` but
    nothing writes an ownership fact when a device registers (who becomes owner
    is owner question 41). Until it
    lands, rule 19's computed rows confer nothing and the dangerous tier is
    reached only by rows an operator writes, fail closed, but not the product
    described here. Coverage is zero until something ships.
43. The `verbActions` doc comment and AGENTS.md's never-pluggable paragraph
    are rewritten to match, in the merge commit that lands this record. In
    `internal/authz/doc.go` the *type and relation* vocabularies stay closed
    as written (rule 32); what changes is the transcribed `send_gcode
    manager` line and the permission-vocabulary prose.
44. `TestEveryClassifiedVerbTakesAPrinterAction` and the profile-coverage
    tests are re-keyed by kind;
    `TestNoShippedProfileDeclaresACommandThisDriverCannotSend` passes
    untouched because the shelf's verbs never existed for kind `printer`. One
    assertion inverts rather than re-keys, and it is named because it
    currently pins a defect. `internal/authz/owner_test.go:211` asserts
    `ownerMask&maskSendGCode` is zero with a message that says the opposite of
    its assertion (*"an owner sends raw gcode …"*). It becomes a `NotZero`
    with a message citing ADR-20260825-z65 rule 31, and it is watched failing against
    the unfixed mask before it is trusted green, per the house mutation rule.

### a design requirement asks for an extensible permission set, not an open class set

45. A design requirement (*"an extensible role set; the core does not fix the roles
    in an enum"*) is amended to say what it needs. An extensible *permission*
    set over a closed *class* set. Adapters extend what can be permitted;
    the core alone says what a permission may escalate to. This resolves the
    disagreement between a design requirement and ADR-20260825-bhe.

### what this record makes true, independent of the design

46. The declared-grant machinery, 1,678 lines across `declaration.go`,
    `declared.go` and `declaredgrant.go` (261, 305 and 1,112), with a live
    migration, is today built, migrated and wired to no surface. `reproject`
    reads `authz_declared_grant` through `loadDeclaredIndex`, and
    `WriteDeclaredGrant` and `WithdrawDeclaredGrant` are exported with no
    caller outside their own tests. Rule 26 resolves it: the table survives
    repurposed, the name-mapping half is deleted, and the compiler stops
    reading a table nothing fills.
47. `ownerMask` (`internal/authz/vocabulary.go:178`) still excludes
    `maskSendGCode`, and its comment argues the omission is deliberate,
    citing 2026-08-27. That is not merely stale against an owner remark. It
    contradicts **ADR-20260825-z65 rule 31**, a current rule whose own sub-bullet
    records the 2026-08-28 amendment. The contradiction is pinned by the
    inverted test rule 44 names. Rule 19 implements the rule; if this record
    is delayed, the mask and the assertion are fixed on their own as a
    defect.

## enforcement

Every gate is owed; the record precedes the code by design. Each is watched
failing on a deliberate mutation before it is trusted green.

| gate | holds |
|---|---|
| owed, jointly with the composition record the open items name | an outside module's verbs dispatch and are granted, nothing under `internal/` changed; rule 1's gate and ADR-20260831-nbj rule 1's |
| owed | no declaration may carry `observe`, `view_audit` or `administer` on a verb, refused at validation |
| owed | stage one: same fault, no device fact, for a real, wrong-kind or missing id; stage two: kind mismatch needs `observe` |
| owed | `CanonicalVerb` suggests from the union: a misspelt shelf verb is suggested against a printer id, for every caller alike |
| owed | `KindOpaque`, an unbounded string, and a number with `Min` but no `Limit` each mark `opaque_payload`, whatever was declared |
| owed | a dangerous verb is reachable through no level mask, asserted over every level on both engines |
| owed | a lowered class holds until accepted by name, health names it, watched failing unpinned; a raised class applies silently |
| owed | a kind name reappearing from a different module path than first seen is refused |
| owed | the list predicate stays one parameter over the generated SQL, unchanged from ADR-20260831-c7b's owed gate |
| owed, with the registration feature rule 42 schedules | registering a device writes the `device#owner` tuple rule 19 reads; until then the dangerous tier confers on nobody |
| owed | `device#owner` reaches every dangerous verb on the owned device and does not reach `administer` |
| owed | `group` and `device_group` grants write and revoke through the one grants API |
| owed | the `history:<id>` grant carries `view_audit`: under ADR-20260901-atx rule 12's filter the provider stores an `authorized` fact |
| owed | `owner_test.go:211`'s inverted assertion flips and is watched failing against the unfixed `ownerMask` |

## rejected

| option | why not |
|---|---|
| the owner's sketch taken literally (design A), with `observe` as a verb class | `observe` is where a destructive verb hides as a read; every verb writes to a one-command wire, so the floor is `operate` |
| `transfer` as a verb class, from the same sketch | upload, cancel and fetch are verbs of the kinds that have them, not a level; they land at `operate` where they already sit |
| a `jobs` facet, a per-kind flag conferring `upload` and `cancel_job` | a display problem solved in the lattice; rule 6 fail-closes undeclared job verbs, rule 38 renders per kind; P3 stays |
| finish ADR-20260831-c7b as written (design B) | maps *permissions*, never the *verb* table blocking a community adapter; two grant systems; `Can` cannot tell names apart |
| a second grant table for declared permissions in general | two write paths, revocation stories and audit surfaces: the named top bug class; the table becomes the dangerous tier |
| a provenance ladder, signature tiers with per-tier class ceilings | nothing signs (ADR-20260827-2bp rule 5's row), so no code tells the tiers apart; and there is no admission point to clamp at |
| out-of-process adapters this release | earlier research measured the wire viable and cheap; compile-time composition is chosen, so it is deferred whole |
| an install-time quarantine and confirmation flow | for runtime admission, which does not exist; the judgement it staged lives with whoever writes the build (rules 24, 15) |
| marking every out-of-tree verb `dangerous` by default | a row per subject per device for every community verb guts the grant story; the pen and the inventory answer instead |
| keep the dangerous verbs at manager level | a level hands them to every manager of every device (why ADR-20260827-rqc rule 2 went); a per-verb row names who may do what |
| an open class set, or adapter-defined classes | placement in the lattice is the escalation decision (earlier research: one line moved an admin's mask with no tuple) |
| replace the tuple engine with something smaller | a design requirement rebuild `device_access` anyway, 12 needs the recursion later, and the engine is validated |
| grandfather manager grants onto the dangerous verbs at migration | it converts a level grant into a named-verb grant nobody wrote; nothing has shipped (rule 41), so nothing to grandfather |

## open items

- **The build-composition record is owed, and it is not this one.** One ADR
  must rule on three questions. First, how a build configuration names adapter
  modules and produces a binary with no core source edit. Second, the public
  composition surface a separate module registers through, since Go's
  `internal/` rule forbids an outside module importing the core's internals
  (`sdk/` being its own module is the precedent). Third, the collapse of the
  roughly nine core-side files a new adapter touches today into that one
  configuration. Those files are the tags manifest,
  `internal/app/builtin/adapters.go`, build-tag lists, a generator import, and
  lint, Makefile, CI and arch-recipe lines. Rule 1's gate cannot pass before it
  lands, and rules 5, 10 and 11 here are written to survive whatever shape it
  takes.
- A *driver dialect* for an existing kind (a community printer driver,
  ADR-20260831-nbj rule 3's first tier) has verbs, and so does the kind itself. The
  two need one stated precedence when both declare; nothing here decides it.
- The guard expression language of ADR-20260831-c7b rule 8 is still unchosen, and
  conditions compose with classes untested.
- The per-build half's bound per kind is unmeasured, inheriting ADR-20260831-nbj
  rule 11's owed number.
- Nothing registers a declaration yet. The registration path and the
  composition record above are the critical path, and everything from rule 11
  to rule 24 runs on no real input until they land.
- Whether `stop_print` deserves a marking of its own is open (it destroys work
  in progress but is reversible in the sense that nothing breaks). This record
  leaves it plain `operate`, where a person weighing it again would look
  first.
- `capability.Resolve` lives under `internal/printer/`, a package named for
  one device class. Rule 7 moves its signature; a design requirement eventually
  moves the package, and that move is not designed here.
- The residue, stated plainly: no computation tells a harmless verb from a
  destructive one whose arguments are all constrained. The person who adds a
  module to a build has probably not read its verb table. Rule 24 gives them
  the table and rule 15 gives them the pen. Past that this model relies
  on the floor being `operate` and on grants being deliberate. That is the
  proportionality this record settles for, and it is written down so nobody
  mistakes it for a proof.
- The two changes this record makes to earlier decisions are the repurposing
  of the second grant table, and the fold of the job actions into `operate`.
  Neither changes what any subject reaches today.
