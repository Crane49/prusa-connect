---
title: "ADR-20260828-dse: a module's start budget is thirty seconds and a report buys another"
description: "The default start budget, how a module buys more time, where the second budget goes, what bounds a starting child's memory, and when a module may migrate its records."
date: 2026-08-28
status: accepted
amends: [ADR-20260827-rwq]
tags:
  - adr
  - extensions
  - supervision
  - modules
---
# ADR-20260828-dse: a module's start budget is thirty seconds and a report buys another

- status: accepted, rule 8 is gated and seven rows are owed
- date: 2026-08-28

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md) rule 5 | Builds on [ADR-20260827-7jn](20260827-7jn-the-plugin-wire-is-a-specification.md), [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20260827-2bp](20260827-2bp-a-module-travels-as-a-verified-artefact.md)

An operator installs a module and it never appears. The process launched, the
resource page says starting, and nothing says how long it was given.

It was given 30 seconds. That is `DefaultStartTimeout` at
`internal/extension/supervisor/policy.go:29`. No configuration key, no operator
setting and no declaration reaches it, and a second budget of 10 seconds sits
inside it on no policy at all. This record says what a module may spend
starting, how it asks for more, and what a module that asks for nothing costs.

## rules

1. Keep 30 seconds as the default start budget.
   `supervisor.Policy.StartTimeout` bounds construction and `Start` for every
   instance, and this record moves neither the number nor its home. Everything
   earlier research could
   measure is three orders of magnitude inside it. A module's launch and
   handshake is 16.6 to 29.1 ms. This project's own seven migrations on a fresh
   SQLite file are 16.5 to 19.8 ms, and 400 device records read back in under
   6 ms. One longer default for every instance loses twice. It charges every
   module for the slowest one. It also holds more for longer, because a stalled
   module's cost is memory and not seconds. At 90 seconds a module holding
   545 MB holds it three times as long, on every rung of the ladder.
2. Buy more time with a progress report, and never with a promise. A child may
   send a progress report on the wire after its handshake line and before it
   answers `plugin.info`. Each report restarts the budget from the moment the
   host reads it. One report buys one further `StartTimeout` and no more, so a
   module with a two minute migration sends four of them. The host caps a single
   extension and caps no total, which is systemd's `EXTEND_TIMEOUT_USEC` rule
   with a maximum on one interval. A module that hangs is then failed 30 seconds
   after its last report rather than 120. A number the module declares in its
   descriptor loses, and that was the owner's own leaning. A declaration is a
   promise about the future. A module that declares two minutes and hangs reads
   exactly like one that needs them, until both have spent two minutes.
3. Write the start budget once. `wirehost` carries no start constant of its
   own. `startTimeout = 10 * time.Second` at
   `internal/extension/wirehost/launch.go:44` is a package constant on no
   policy, and `identify` at `launch.go:265` is the only reader of it.
   `wirehost.Options` takes the budget from its caller instead, and
   `internal/app/resourcehost/service.go` passes `Policy.StartTimeout` into it.
   Keeping the inner constant loses because it is a second number nobody can
   see: it sits on no policy, no operator reaches it, and two copies of one
   number drift. Deleting the inner bound and adding nothing loses as well,
   because `Launch` runs under `context.WithoutCancel(startCtx)` at
   `service.go:142`, so a call inside the launch would carry no deadline.
4. Let an operator raise one instance's budget in the declaration.
   `Spec.Required` at `internal/app/resource/model.go:59` is where the
   operator's other supervision switch already sits, and the budget sits beside
   it. `policyFor` at `internal/app/resourcehost/instance.go:210` applies it the
   way it already applies `Required`. This is the escape hatch for a module
   whose author never sent a progress report, and it is a companion to rule 2.
   An operator key on its own loses, because the operator knows no more about a
   module's start-up than the module does.
5. Publish a starting child's memory, and bound it per instance. Nothing bounds
   it today. `start` at `internal/extension/wirehost/launch.go:312` is a bare
   `exec.CommandContext` with no `SysProcAttr`, no rlimit, no job object and no
   cgroup, and a search of `internal/`, `sdk/` and `cmd/` for `SysProcAttr`
   returns nothing. Earlier research built a module that holds 545 MB before its
   handshake, and the host called it `starting` with no number attached. So the
   host reads the child's resident memory while the instance is starting and
   publishes it on the instance's condition. The ceiling is an operator setting
   per instance, and a child over it is killed and its attempt failed with a
   named reason. A fixed default ceiling loses, because a design requirement names a
   Raspberry Pi and this project also runs on a 16 GB laptop.
6. Open the record store to the instance whose start is in flight. This amends
   [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md) rule 5.
   Rule 5 asks a module to migrate its own store before it reports ready, and
   `gatedRecords.ready()` at `internal/app/resourcehost/statecall.go:105`
   refuses every call from an instance the host is not serving. A module whose
   data is in `Records` therefore cannot do what rule 5 asks. The gate admits a
   call arriving on the session the host opened for the attempt in flight, and
   refuses every other not-serving state. The routing half of rule 5 does not
   move: nothing outside the host reaches a module that has not reported ready.
   A module gating its own handlers on a flag of its own loses. Every author
   then re-implements readiness, and one who forgets it serves half-migrated
   data while the host reports the instance healthy.
7. Make a module's start work idempotent and restartable. Every rung of the
   ladder runs the whole start again from its first statement, so a migration
   that ran halfway runs again from the beginning. The host enforces none of
   this and cannot: it never sees a store the module owns. ADR-20260827-rwq rule 6
   ships no down migration, so a partly applied step has nothing to roll it
   back.
8. Add no second ladder and no give-up of this record's own.
   `internal/extension/supervisor/policy.go` carries full jitter, a five minute
   cap, a two second start scatter, a stability window, and a give-up bounded by
   time with a floor on attempts. A module cannot reach the `Required` switch
   that turns give-up off. `IsCore` at
   `internal/app/config/install/install.go:405` requires `RuntimeBuiltin`, so
   `required: true` on a `runtime: service` type is refused at all three doors.
   A stalled module therefore latches after about an hour and 27 attempts.
   Raising `GiveUpAfter` for a slow starter loses: a module that reports
   progress never reaches the ladder, and one that reports nothing is not slow.

## the steps the budget bounds

Rules 1 to 3 make one budget out of two, so every step below is bounded by the
same number and expiry ends the same attempt.

| step | what expiry does |
|---|---|
| verify the artefact and spawn the process | the launch unwinds and the child is killed |
| write the handshake line | `handshake` reads with no context, so killing the process is what unblocks it |
| answer `plugin.info` | the child is killed and the start fails |
| migrate a store the module owns | the attempt fails and the ladder retries from the first statement |
| migrate records in the host's store, under rule 6 | the attempt fails and the ladder retries from the first statement |

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | none. A gate over a default constant restates the constant | review |
| 2 | `TestAProgressReportBuysOneFurtherStartBudget`, and `TestAChildThatStopsReportingIsFailedAtTheNextInterval` | owed |
| 3 | `TestTheStartBudgetIsWrittenInOnePlace`, an arch gate refusing a duration constant in `wirehost` | owed |
| 4 | `TestAnOperatorRaisesOneInstancesStartBudget`, over the declaration and `policyFor` | owed |
| 5 | `TestAStartingChildsMemoryReachesItsCondition`, and `TestAChildOverItsCeilingIsKilled` beside it | owed |
| 6 | `TestAModuleMigratesItsRecordsBeforeItReportsReady` in `internal/app`, over a live module | owed |
| 6 | `TestAModuleCallingTheStoreFromAStaleSessionIsRefused`, which is the half that must stay refused | owed |
| 7 | none possible. The host never sees a store the module owns | review |
| 8 | `TestRequiredIsRefusedOnAnImplementationTheCoreDidNotWrite`, in `internal/app` and in `install` | exists |

## rejected

| option | why not |
|---|---|
| a number the module declares in its descriptor | a promise about the future, and a wrong one reads the same as a right one until it expires |
| one longer default for every instance | it charges every module for the slowest, and triples what a stalled module holds |
| no bound on the start at all | `handshake` at `launch.go:232` reads with no context, so a silent child would hold the loop for ever |
| bounding the total extension | a total is a second number to pick, and a missed interval already separates work from a hang |
| a second retry ladder for a slow start | `supervisor.BackoffPolicy` is the ladder, and two ladders over one instance is two states to reconcile |
| polling the module the way a startup probe does | the child holds an open session, so a report costs one frame against a poll's round trip per interval |
| dropping `wirehost`'s inner budget and adding nothing | `Launch` runs under `context.WithoutCancel`, so the call would then carry no deadline |
| an operator budget key with no progress report beside it | the operator knows no more about a module's start-up than the module does |
| `RLIMIT_AS` as the memory bound | a Go child doing nothing holds 5.64 MB of working set and reserves 5.53 GB of address space |
| a fixed memory ceiling for every deployment | a design requirement names a Raspberry Pi, and the same binary runs on a 16 GB laptop |
| a module gating its own handlers on a readiness flag | every author re-implements readiness, and one who forgets serves half-migrated data |
| a second readiness signal beside the supervisor's | two states over one instance is two states to reconcile, which is the ladder's reason |
| raising `GiveUpAfter` for a slow starter | a module that reports progress never reaches the ladder, and one that does not is stuck |

## open items

- No progress frame exists, so rule 2 is a design. It is a wire minor under
  [ADR-20260827-7jn](20260827-7jn-the-plugin-wire-is-a-specification.md) rule 11, negotiated
  the way `wire.Negotiate` already negotiates one, so an older child that never
  sends it is unaffected. Wire major 1 has one minor, which is 0.
- `startOnce` at `internal/extension/supervisor/lifecycle.go:96` fixes the
  deadline once with `context.WithTimeout`. A deadline a report moves cannot be
  a fixed one, and what replaces it is rule 2's first task.
- `currentGeneration` at `internal/extension/supervisor/instance.go:357` is
  unexported, so rule 6 has no handle to match a session against. Which value
  identifies the attempt in flight is the open half of that rule.
- Where the progress frame sits is decided here and not measured. `handshake`
  at `launch.go:232` reads exactly one line, so a report before that line breaks
  the parse, and the frame sits after it.
- No module with a slow start exists to measure. Every figure behind rule 1 is
  a module that does nothing, or one built to allocate.
- Every figure is from a laptop with 16 threads and an NVMe disk. A design requirement
  names a Raspberry Pi, where a process launch may differ in kind.
- `EXTEND_TIMEOUT_USEC` was read from the systemd manual page and not exercised
  against a running systemd. That it bounds no total is the manual's silence.
