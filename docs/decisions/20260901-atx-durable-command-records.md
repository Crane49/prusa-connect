---
title: "ADR-20260901-atx: durable command records"
description: "A command's lifecycle is published on the bus as append-only transitions: requested, authorized, settled. A host-minted command_id joins them, one visibility predicate gates the audit frame on every transport that serves it, a database adapter folds the transitions into its own projection, VerbOf stops special-casing the command class, and a command abandoned by its caller still terminates its own record."
date: 2026-09-01
status: accepted
tags:
  - adr
  - devices
  - events
  - storage
  - audit
---
# ADR-20260901-atx: durable command records

- status: accepted. Built. Every gate is held except two named in the table. ADR-20260828-rp4's per-type JSON Schema is not generated yet, and ADR-20260901-rf2's conformance suite does not exist yet
- date: 2026-09-01

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Overturns an earlier design decision ("a command is never stored") | Amends an earlier design decision, which stays the ack and stops being the only record | Upholds HI1, HI4, HI5, F3, F4, F11, [ADR-20260829-t7d](20260829-t7d-a-child-dials-one-parent-and-may-only-narrow-it.md) rule 37, [ADR-20260829-jdq](20260829-jdq-the-host-bounds-the-text-a-driver-hands-it.md) rules 2 and 7, [ADR-20260827-wgc](20260827-wgc-an-extension-reaches-persistence-through-the-sdk.md) rules 2 and 7 as amended by ADR-20260901-rf2, ADR-20260827-wgc rule 11, which rule 13 here builds on both transports, and C6 | Amends HI2 in one field: `historyapi.Fact` gains the frame's class | Follows [ADR-20260828-rp4](20260828-rp4-a-published-event-type-declares-its-payload.md) for the three new types and [ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md) for the vocabulary version | Reads with [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) and with [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md), whose rule 3 defines the `view_audit` action rule 12 here checks

Today a command is sixteen stages and one HTTP response. Nothing on the bus says
a command was requested, who authorized it, or how it ended; the only records
are two metrics and a 256-entry debug ring. Worse, a command whose caller's
context dies after the pump has written it to the wire may run with no record at
all. The reply arrives to a settled call and is discarded. This record puts the
lifecycle on the bus as three append-only transitions, joined by a host-minted
id, so that any subscriber, a database adapter first among them, can keep it.

One property matters enough to state up front. The `authorized` frame
names which principal sent which command, and that fact is deliberately not
something viewing a printer grants. The frame travels on **two transports**,
the live stream and the history read. A rule enforced on one transport and
not the other is this project's named top bug class. Rules 12 and 13 are one
predicate applied to both, and they land in one change or not at all. ADR-20260827-wgc
rule 11 (*"an event a caller may not see on the stream is unreadable from
history"*) is the standing rule they satisfy.

## the rules this record amends

**HI3, "a command is never stored", is overturned.** The decision rested on two claims and both fail. "A command that
expired has no value": a stored `requested` frame is a record that an
instruction was issued, and issuance does not expire. Only *acting* on the
instruction does, and `Envelope.Expired()` is the guard for that, checked by an
actor and never by a store (rule 11). "The record of who authorized it is
already an audit event": nothing publishes `ClassAudit`, and an audit event
cannot subsume the command. It is written before the outcome exists, and it is
signed per event so its status cannot advance. Its subject is a principal
where the command's subject is a device, gated by different permissions. What
HI3 was actually bought for, a provider reads one envelope attribute and knows
where a row goes, is kept and strengthened by rule 2. That makes the rule
smaller, not larger. The class-picks-the-verb half of HI3 stands. Sub-bullet
for the register row and for `sdk/historyapi`'s `VerbOf` comment:

> - **Overturned by [ADR-20260901-atx](20260901-atx-durable-command-records.md) rules 1 and 2.**
>   The class still picks the verb, and
>   the rule shrinks: telemetry is a sample, everything else is a fact. The
>   the old backlog checkbox carrying this decision is unchecked and re-tested.

**E3, "command acks ride the POST response", is amended, not contradicted.**
The POST response stays and is still the ack. Rule 18 makes it stop being the
only record of one.

**HI2, "one resource kind, five methods", is amended in one field and no
method.** `historyapi.Fact` gains `Class` for one reason. A store that holds
audit rows must be able to answer a query that excludes them (rule 13). The
class is not recoverable from a stored type string for a type the build has
never heard of. The five methods, the one kind and the dedup key do not move.

**ADR-20260829-jdq rule 7 keeps its conclusion on a new premise, and the sub-bullet
records it.** The rule gives `Result.Raw` no bound *"because no host record
retains it"*. This record creates durable command records, so the premise now
holds only because rule 9 here keeps `Raw` out of every published payload, and
a gate watches it. Sub-bullet for the old rule at merge:

> - **Premise defended by [ADR-20260901-atx](20260901-atx-durable-command-records.md) rule 9.**
>   Command outcomes are durable since that record, so "no host record retains
>   it" is no longer a fact about the absence of stores: it is rule 9's
>   prohibition, gated by a case asserting `Raw` appears in no published
>   payload. The day that rule moves, rules 2 and 3 here apply as written.

**ADR-20260827-wgc rule 11 is not amended, it is the rule this design is held to.**
This record introduces the first stream-side narrowing history must mirror.
Rule 13 is the mirror, landed in the same change as rule 12. The enforcement
table carries the equivalence gate.

Everything else this design touches, it upholds. HI4 holds (no new queue, no
new subscriber), and HI5 holds (the precedent for type-directed projection).
F3 and ADR-20260829-t7d rule 37 hold (this stores the interactive verb and builds no
queue). F4/F11 hold (TTL stays remaining-seconds, commands only). ADR-20260829-jdq
rule 2 holds (driver text is clipped where the host keeps it), and C6 holds
(validation stays above the driver, publication above validation).

## rules

1. Commands are stored. A history provider that consumes them keeps them, and
   how long it keeps them is the adapter's configuration under
   [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rule 14, never a core rule.
2. `historyapi.VerbOf(event.ClassCommand)` returns `VerbFact`, and the comment
   claiming a command is never stored is rewritten. The whole table then reads:
   telemetry is a sample, everything else is a fact. That is a smaller rule
   than the one it replaces, and the last special case a provider needed is
   gone.
3. The command lifecycle is published on the bus, as three declared core event
   types, one append-only envelope per transition:

   | type | class | TTL |
   |---|---|---|
   | `com.prusa.connect.device.command.requested` | `command` | yes, it is an instruction with a deadline |
   | `com.prusa.connect.device.command.settled` | `state` | no, a transition worth keeping; a late "the pause was refused" still has value |
   | `com.prusa.connect.device.command.authorized` | `audit` | no, a record of who did what |

   All three publish on the device's origin, so one visibility answer covers
   them together with every other device event, narrowed, for the audit class
   alone, by rules 12 and 13. The transport was always ready for this:
   `Class.Durable()` already holds for both classes, the FIFO lane already
   carries them, and `Last-Event-ID` resumption works unchanged.
4. A `command_id` is minted at the single chokepoint every transport passes
   through, `devices.Command`, before authorization, so a refusal is also
   addressable. It is a ULID, the tree's existing shape for a host-minted id.
   It is not the caller's idempotency key; the two identities are distinct and
   both are carried.
5. A transition is never superseded in place. The updatable view of a command
   is a fold over its transitions, computed by whoever wants one. The bus has
   no supersession for a durable class and gains none: conflation stays a
   telemetry-lane mechanism, and the FIFO lane's never-silently-dropped
   guarantee is untouched.
6. `CausationID` is the join, used for exactly what it was declared for:
   `settled` and `authorized` carry `CausationID = command_id`, and `requested`
   carries the `command_id` in its own data. A consumer folds by `CausationID`.
7. Publication order has three points. `authorized` publishes at the
   authorization point, the only place that holds the principal, the action
   and the device together. `requested` publishes before `session.Do`, which
   narrows the window in which a command can run unrecorded from the whole
   call to the feed's flush interval. `settled` publishes after the call
   returns, including on the `ctx.Err()` path. It is published on a context
   detached from the request (`context.WithoutCancel`), so a command
   abandoned by its caller still terminates its own record.
8. A `settled` frame is honest about what the host observed. Its status is one
   of `succeeded`, `failed`, `refused`, `timed_out`, `abandoned`. A command
   that was or may have been on the wire when its caller died settles as
   `timed_out` or `abandoned` with the outcome marked unknown, never guessed.
   This fixes half of today's gap and says so. Today such a command leaves no
   record at all. Under this record the invocation, the authorization and the
   abandonment are always recorded. The printer's true outcome after
   abandonment is still known only to the printer, and the record does not
   pretend otherwise.
9. Payload discipline, with numbers, because the replay window is per origin
   and bounded at 32 events and 32 KiB and rule 10 does the arithmetic.
   - `Result.Raw` reaches no published payload, that is what keeps ADR-20260829-jdq
     rule 7's premise true, and it is gated. `machine_reason` is clipped
     through `machinetext.Clip` (ADR-20260829-jdq rule 2).
   - `requested` carries the verb, the key, the capability epoch and digest it
     was validated against, and the arguments as the caller sent them. The
     whole payload has a declared bound of 4 KiB. Arguments that would
     break the bound are replaced by their SHA-256 digest with
     `args_omitted: true`, so the record still proves what was issued without
     ever storing an unbounded payload. One deliberate narrowing rides on the
     same shape. A verb marked `dangerous`
     ([ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rule 15) publishes its
     arguments as that digest always, never plain. `requested` travels on the
     device origin to every subscriber holding `observe`, and raw gcode
     broadcast to every device viewer is the same widening the debug log
     refused for the actor. The digest still proves what was issued; the cost,
     accepted here, is that the durable record of a dangerous verb's payload
     is its hash, not its text. Unmarked verbs carry their arguments plain,
     deliberately: an operate-class argument is what any viewer watches the
     machine do anyway.
   - `settled` carries the outcome fields, the elapsed time and the
     `command_id`, and **not** the arguments. The fold has them from
     `requested`, and storing them twice was the earlier draft's arithmetic
     error. Its declared bound is 1 KiB.
   - `authorized` carries the `command_id`, the verb, and the authorizing
     principal as `auth_type` and `auth_id`, the same pair the envelope's
     audit-only attributes carry. The pair is duplicated into the declared
     payload **deliberately**. The envelope attributes are what a hop reads
     without parsing data, and the payload is what a store keeps, because
     `historyapi.Fact` stores the data member and not the envelope. Without
     this the durable audit record would have no actor, which was the earlier
     draft's jointly-impossible pair of rules. Its declared bound is 1 KiB.
   - `requested` and `settled` carry no principal in any field. Who authorized
     lives in the `authorized` frame alone, which is the frame rules 12 and 13
     gate.
10. A command contributes exactly three durable frames to its device's origin
    and no more, and the cost is stated with the real numbers rather than
    waved at. The replay window holds 32 events per origin, so **eleven
    commands evict a printer's entire window**. At rule 9's worst-case
    payloads (about 6 KiB across the three frames) the 32 KiB byte bound
    holds five commands and the sixth evicts it. A UI walking a device through a ten-step preheat
    sequence is not a burst. It is normal use, and it will cost a reconnecting
    stream reader its continuity on that origin. That loss is bounded and
    recoverable by design. The reader is told continuity was lost, the facts
    are in the history provider, and HI5's gap rows exist for exactly this
    read-back. So the window is a stream-continuity cost and never a record
    loss, the operator documentation says so, and the per-origin window
    constants stay a measured change if it bites. `dispatched` and
    `acknowledged` are not published: they are observable only inside the
    driver, and publishing them would change `sdk/connectorapi`. If they are
    ever wanted, the shape is an optional interface on the session, asked for
    and never assumed.
11. Only `requested` carries a TTL, and the hub keeps refusing one on any other
    class. A consumer that *acts* on a command frame, replayed or forwarded,
    checks `Envelope.Expired()` first; a consumer that merely *stores* one does
    not. The record of what happened is never expirable; the instruction always
    is. That split is the reason `settled` is a separate frame on a separate
    class.
12. **The audit visibility predicate, stream side.** A `ClassAudit` frame is
    delivered only to a subscription whose principal holds `view_audit` on the
    frame's origin. That is the action [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md)
    rule 3 defines, conferred at manager and by nothing lower. The check sits
    in `Subscription.deliver` beside the existing type filter, one boolean per
    subscription and one comparison on the publish path. The boolean is
    **not** fixed at subscribe time. It is recomputed in the same
    re-authorization pass that already rebuilds the subscription's allowed
    origins on every graph epoch move. Revoking audit sight therefore takes
    effect exactly when revoking an origin does, and a long-lived SSE stream
    cannot outlive its grant. The cost is one bit per subscription per epoch,
    so E10's one-check-per-subscription cost model holds. This is a narrowing
    inside an already-authorized origin, not a second authorization axis. The
    origin model stays whole, and the widening the debug log refused, every
    device viewer learning which operator sent which command, stays refused.
    The relation `Bootstrap` writes for a `history:<id>` provider principal
    confers `observe` and `view_audit` and nothing else (ADR-20260901-b2z rule 34). A
    provider that declares the `audit` class in `Consumes` is therefore fed
    it rather than silently starved, without holding `administer` on anything.
13. **The same predicate, history side, in the same change.** Without this,
    `GET /api/v1/history/facts?types=…command.authorized` hands the
    operator-to-command mapping to any caller holding read on the printer.
    That is because the fact path already stores every `ClassAudit` frame
    (rule 2's table routes it) and a fact query reduces by origin alone. So:
    - `historyapi.Fact` gains `Class event.Class`, written by the feed from
      the frame it already reads the class off. The reference adapters' schema
      is born with the column, nothing has shipped, so
      there are no rows to backfill.
    - `historyapi.FactQuery` gains `Classes []event.Class`: the classes the
      answer may contain. It is set by the host's reader, never by a caller.
    - `historyReader.Facts` computes `Classes` with the same predicate rule 12
      uses, fail closed over the whole query. `FactQuery.Refs` is plural and
      `Classes` is one list, so the reader intersects the caller's grants
      across every queried origin. `audit` is included only when the caller
      holds `view_audit` on all of them. A caller holding it on printer A and
      not printer B gets no audit facts from a query naming both, and queries
      the audit-granted origins alone to read them. The reader never splits
      the query itself, because the provider-minted cursor is per query. The
      reader additionally drops from the answer any fact whose class the query
      excluded. A provider that ignores the new field therefore shows up as
      missing rows in a conformance failure, never as leaked rows in production.
    - every read of stored facts goes through that one reader. The old backlog's planned
      `GET .../commands` and `.../commands/{command_id}` endpoints are the
      history search narrowed by type, as the old backlog already specifies. They are
      served through the same reader and inherit the predicate, and this
      record does not mint them.

    With rules 12 and 13 together, ADR-20260827-wgc rule 11 holds as written. An event
    a caller may not see on the stream is unreadable from history, and the
    enforcement table gates the two paths against each other.
14. The mutable command row is the adapter's projection. A provider that wants
    one derives it in its own schema, keyed by `command_id`, driven by event
    type, the same type-directed move HI5 already makes for gap rows.
    `historyapi.Provider`'s five methods and `Caps` are unchanged. The fold of
    `requested` plus `settled` carries the whole record, so the upsert is
    idempotent and order-tolerant. Keeping or expiring the projection and
    the facts is the adapter's business.
15. Replay stores nothing twice: `Fact` is idempotent on
    `(ref, epoch, sequence)` as it stands, and a fold keyed on `command_id`
    converges regardless of arrival order. No new dedup rule exists.
16. Idempotency is scoped to `(device, key)`, the key optional as
    `connectorapi.Command.Key` already declares. In flight, a second command
    with a live `(device, key)` is not dispatched twice. It is answered with
    the first invocation's `command_id`, and it publishes **no frames of its
    own**. The record says so here because it is a real gap. The durable trail
    then shows one principal requesting a command two callers asked for, and
    the second asker exists only in the access log. The window of that gap is
    the first invocation's lifetime, which the caller's context bounds. For a
    printer that is away, the queue-and-drain comment at `devices.Command` is
    explicit that the context is the only bound, so the window is not
    necessarily short. Dedup against *settled* commands is best-effort where a
    history provider serves and absent-and-says-so where none does (HI1).
    Dedup across a restart is not promised, and nothing may promise it. The
    `requested` frame is in memory for up to the flush interval, and nothing
    in the core waits on a provider. The driver session is gone on restart
    anyway, so only the printer knows what it executed.
17. A durable command record is not a queue. ADR-20260829-t7d rule 37 stands. Anything
    that must survive an outage is desired state reconciled on reconnect, and
    this record builds no retry and no persistence a dispatch waits on. (What
    "fails fast" means for a printer that is away is the driver's
    queue-and-drain behaviour, bounded by the caller's context. This record
    changes nothing about it and claims nothing beyond it.) The two `core.job`
    faults, `JobCommandNotFound`, `JobDuplicateCommand`, describe the queued
    verb this record deliberately does not build. They are referenced by
    nothing, and are today published in the fault vocabulary as documentation for a
    feature that does not exist. **They are deleted**, plainly: nothing has
    shipped, so no release note is owed and no client can
    miss them; ADR-20260825-zkq binds from the first release forward. Re-minting them
    is part of building the queued verb, which is a change to ADR-20260829-t7d rule 37.
    The earlier draft's arch gate freezing them unreferenced is not
    built. A grep test whose only job is to assert two constants stay unused
    protects nothing a review does not, and it would have preserved a
    published lie.
18. The HTTP surface grows additively under ADR-20260825-zkq: `PrinterCommand` gains an
    optional `key`, `PrinterResult` gains `command_id`, the OpenAPI document is
    regenerated and the examples grow the members. The route, the refusal set
    and the response shape are otherwise unchanged, the POST response stays
    the ack (E3, amended).
19. No driver-contract change. Every transition is observable above the driver,
    at the chokepoint that already holds the principal, the verb, the
    arguments, the start instant and the result. An adapter author writes no
    new code. Validation stays above the driver and publication stays above
    validation, which is C6.
20. The three types follow ADR-20260828-rp4 in full: exported payload types born in
    `sdk/event`, generated per-type JSON Schema carrying rule 9's declared
    bounds, hub-stamped `dataschema`, and `VocabularyVersion` moves under
    ADR-20260827-qvk. The hub already refuses an undeclared core type at publish time,
    so a missing entry is a hard failure.

## enforcement

Every gate is owed and each is watched failing on a deliberate mutation before
it is trusted green.

| rule | gate | state |
|---|---|---|
| 2 | `VerbOf` table test; the the old backlog HI3 row unchecked and re-tested with the new expectation | held: `sdk/historyapi/history_test.go` |
| 3, 20 | `make event-types-check` both directions; regenerated `docs/vocabularies/event-types.json`; the hub's `ValidateType` refusal | held for the vocabulary; the per-type JSON Schema half is not built anywhere in the tree yet and is owed with ADR-20260828-rp4 |
| 6 | a fold test over the three transitions in shuffled order, converging by `CausationID` | held: `adapters/db/internal/histstore/command_test.go`, and the join asserted in `internal/app/commandframes_test.go` |
| 7, 8 | kill the request context mid-flight; a `settled` frame, status `abandoned`, outcome unknown, published on a detached context | held: `internal/app/commandframes_test.go` |
| 9 | sizes per type; no `Result.Raw`; no principal on `requested`/`settled`, actor on `authorized`; dangerous verbs: digest only | held: `internal/app/commandframes_test.go`, four cases, the last paired with an unmarked verb publishing plain arguments |
| 10 | a case counting durable frames per command at exactly three | held: `internal/app/commandframes_test.go` |
| 11 | a case that an expired `requested` frame is stored and a consumer acting on it refuses | held: `internal/app/commandframes_test.go`, and the TTL split asserted on all three frames |
| 12, 13 | **ADR-20260827-wgc rule 11's equivalence gate**: no `view_audit`, no `authorized` frame on either transport; plus mixed grants | held: `internal/transport/stream/audit_test.go` and `internal/app/commandaudit_test.go`, each watched failing unguarded |
| 12 | a revocation of `view_audit` mid-stream stops `ClassAudit` delivery at the next epoch move without a reconnect | held: `internal/transport/stream/audit_test.go` |
| 13 | ADR-20260901-rf2's conformance suite gains the class-restriction case; the reader drops an excluded class's row regardless | half held: no suite yet; the case sits on the reference adapter, the reader's drop in `internal/app/commandaudit_test.go` |
| 14 | the reference adapter's `history_command` projection, driven through replay, upsert idempotent | held: `adapters/db/internal/histstore/command_test.go`, both engines |
| 16 | an in-flight duplicate gets the first `command_id`, dispatched once, with no second `requested` or `authorized` frame | held: `internal/app/commandframes_test.go` |
| 17 | the fault vocabulary regenerates without the two `core.job` codes | held: `make error-codes-check`, 74 codes |
| 18 | ADR-20260825-zkq's compatibility gate on the regenerated OpenAPI document | held: `make api-check` classifies `key`, `command_id` and `duplicate` as additive |

## rejected

| option | why not |
|---|---|
| command records in `sdk/stateapi` | a module's *own* data door: namespaced, capped, no query, no watch; a core index inside it is what that door is not for |
| `historyapi` gains an upsert verb behind a `CapCommands` bit | the provider parses a schema it does not own; `Fact`'s dedup key fails for a changing row; breaks HI2's five methods |
| a third contract, `commandapi` | doubles config, health, grants and supervision for halves sharing origin, identity, time base, retention |
| superseding the record in place on the bus | the only supersession is telemetry conflation: at-most-once, invisible to the fact path; FIFO must not conflate |
| audit subsumes the command | it fails on time, signing, subject and existence, see the amendments section |
| publish `dispatched` and `acknowledged` | only the driver sees them, so v1 would change `sdk/connectorapi` for two marginal frames at real replay-window cost |
| audit on a non-device origin | reopened after the history leak, rejected again: a `ref.Kind` for nothing, split device events; rules 12 and 13 cost less |
| audit on the device origin with the widening accepted | contradicts the debug log's reasoning: which operator sent which command is not something viewing a printer grants |
| the actor on the envelope attributes alone, `Fact` unchanged | jointly impossible as a durable record: `Fact` stores the data member only, so the stored audit record loses its actor |
| a filter on the stream alone, history later | one transport and not the others is the named top bug class and violates ADR-20260827-wgc rule 11; rules 12 and 13 land as one |
| a synchronous durable write before dispatch | contradicts HI1's premise that nothing in the core waits on a provider, and buys a promise a restart cannot keep |
| an arch gate freezing the two `core.job` faults unreferenced | ceremony guarding a published lie; the faults are deleted instead (rule 17) |

## open items

- The the old backlog open row on `ClassCommand`/`ClassAudit` is answered by rule 3. It
  was an open question, so this is not a reversal.
- The claims file row carrying "A command is never stored" regenerates with the
  code; never hand-edited.
- `historyFeed` needs no logic change beyond writing `Fact.Class` from the
  frame it already reads, the class routing at the feed is untouched. The
  reference adapter owes the projection table and the class column, both born
  in its initial schema: no backfill migration exists, because no rows do.
- **Rule 12 says "one boolean per subscription" and the build carries one per
  origin.** A subscription spans every device its principal may see, and
  `view_audit` is a device action. So a single boolean would have to be the
  intersection across the whole routing table, and it would withhold one
  printer's audit frames because of another. The boolean is therefore the value
  of the origin lookup `Subscription.deliver` already performs, which costs the
  same probe and no second one. It is rebuilt from the grants in the same
  re-authorization pass. The cost model rule 12 cites is unchanged.
- **Two things the build found that the rules did not say.** First, rule 3's
  table calls the `requested` TTL a yes. In practice the TTL is the remaining
  seconds of the *caller's* context. This tree has no server-side command
  deadline anywhere: a caller that sets no deadline publishes an instruction
  with no expiry. Inventing one would expire a live instruction, so
  the frame carries none and says so. Second, rule 13's intersection is over
  every origin the query names. A caller who names none is asking about
  every origin they may read, which includes resource origins that carry no
  audit class at all. The audit class is then withheld from the whole answer.
  That is the fail-closed behaviour the rule asks for and it is worth knowing
  before reading a query result: to read audit facts, name the device.
- **`PrinterResult` grew one member rule 18 does not name**, `duplicate`. Rule
  16 says an in-flight duplicate is "answered with the first invocation's
  `command_id`", and the response otherwise carries the printer's outcome.
  Without a second member the honest answers were a lie (`ok: true` for
  something no printer has answered) or a silence a caller cannot tell from a
  success. It is additive under ADR-20260825-zkq and the compatibility gate classifies
  it as such.
- The in-flight dedup map of rule 16 costs one map beside the existing pending
  gauge and survives nothing, by design. If the queued verb is ever wanted,
  that is F3's second half and a change to ADR-20260829-t7d rule 37.
- ADR-20260901-rf2's open item on a provider silently storing nothing gains one
  concrete case from rule 12: a provider declaring `audit` in `Consumes` whose
  principal lost `view_audit`. The degraded-health entry that surfaces it is
  ADR-20260901-rf2's to rule; the bootstrap grant of ADR-20260901-b2z rule 34 is what prevents
  it on first run.
- Replay-window pressure is documented with its real numbers in rule 10, not
  solved. Continuity on a busy origin is the price of three durable frames per
  command, and the history read-back is the recovery. The per-origin window
  constants are a measured change if practice says otherwise.
