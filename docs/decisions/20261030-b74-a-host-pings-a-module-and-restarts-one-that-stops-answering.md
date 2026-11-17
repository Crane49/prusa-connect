---
title: "ADR-20261030-b74: a host pings a module and restarts one that stops answering"
description: "How the host pings a service module, which evidence adds a second signal, how it kills and counts a hang, and what a ping cannot prove."
date: 2026-10-30
status: accepted
supersedes: []
amends: [ADR-20260827-7jn]
tags:
  - adr
  - extensions
  - supervision
  - modules
---
# ADR-20261030-b74: a host pings a module and restarts one that stops answering

- status: accepted
- amends [ADR-20260827-7jn](20260827-7jn-the-plugin-wire-is-a-specification.md) rule 20.
  The lifecycle set gains a method
- relates to [ADR-20261027-5m4](20261027-5m4-a-service-module-runs-under-a-memory-budget.md)
  rules 24 and 25, which reserve `not-responding` and count it
- date: 2026-10-30

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260827-7jn](20260827-7jn-the-plugin-wire-is-a-specification.md) rule 20 | Builds on [ADR-20261027-5m4](20261027-5m4-a-service-module-runs-under-a-memory-budget.md)

A service module can stop answering and still be a live process. The breaker
sees only calls, and an idle module has none. This record rules how the host
finds such a module and what it does then.

## rules

1. The lifecycle set is `plugin.info`, `plugin.shutdown`, `plugin.configure`
   and `plugin.ping`. This amends rule 20 of ADR-20260827-7jn, which names the
   first two. Every child answers `plugin.info`, `plugin.shutdown` and
   `plugin.ping`, whatever contract it serves. A child answers
   `plugin.configure` only when it names it in `Info.Serves`.
2. `plugin.ping` takes no params and answers nothing (JSON `null`). The SDK
   answers it inside the connection, before any handler runs, so a module
   cannot replace it and module code does not change. The lifecycle contract
   goes from `1.0.0` to `1.1.0`.
3. A host counts an answer of `-32601` (method not found) to `plugin.ping` as
   alive. A child built against contract 1.0.0 proves the same thing by
   answering it.
4. The host pings every 15 seconds. A ping has a timeout of 5 seconds. Three
   misses in a row mean the module is not responding. Any answer resets the
   count.
5. The ping goes over the connection directly. It takes no bulkhead slot, and
   the breaker never sees it.
6. The host sends no ping before `plugin.info`, and `plugin.configure` on a
   child that serves it, have succeeded. The watch starts after the start has
   finished.
7. A second signal does not need a missed ping. It fires when the breaker has
   not been closed for more than 2 minutes and the host saw hang evidence in the
   last minute. The window is 1 minute, not the breaker's 10 seconds. Under a
   breaker that is not closed the only calls are probes, one every 30 seconds.
   Each has a 10 second timeout, so a 10 second window would miss most of them.
8. Only two things are hang evidence: a call that ended because the host's own
   deadline passed, and a call that waited for a bulkhead slot until the
   deadline passed. A full bulkhead whose wait ends because the caller gave
   up is not evidence. An error the module answers is not evidence. A call the
   open breaker refused is not evidence. A module that answers errors quickly
   is failing, not hung, and the breaker already handles it.
9. A breaker that opened and saw no call since is old evidence. The host never
   kills an idle module on it.
10. A not-responding module is killed with its whole process group. The host
    does not send `plugin.shutdown`, because a module that does not answer
    will not answer that either. The stop has the result `not-responding`.
    When two stops race, for example a memory stop and a hang stop, the first
    one keeps its reason.
11. A `not-responding` stop counts in `failures_in_window` like any other
    failed stop. It counts toward the switch-off of `core.modules_switch_off`
    or the instance's own `switch_off`
    ([ADR-20261027-5m4](20261027-5m4-a-service-module-runs-under-a-memory-budget.md)
    rule 25). It does not have a limit of its own. With `failures: 0` it is
    counted and never switches the instance off.
12. `on_over_memory: warn` does not exempt a module from hang detection. A
    module over its budget that misses three pings is still killed.
13. A ping proves that the process is scheduled, that its read loop runs and
    that its stdout drains. It does not prove that a handler is alive. Each
    request runs in its own goroutine (`sdk/wirerpc/conn.go`), so a module with
    a deadlocked handler still answers pings.
14. A deadlocked handler on a module that gets fewer than 10 calls in 10
    seconds never opens the breaker (`DefaultBreakerMinRequests`). The ping
    cannot see it either. Rule 7 needs hang evidence and an open breaker, so
    it does not fire there. The host does not detect this case.
15. A host starved of CPU for 35 to 50 seconds (3 misses of 15 seconds) can
    kill every module at once. The host accepts this. A module killed this way
    is restarted and counted by rules 10 and 11.

## enforcement

- `go test ./internal/app/resourcehost/ ./internal/extension/supervisor/`
  covers the ping, the evidence rules and the kill.
- `make wire-check` step 5 runs the `sdk/wirerpc` tests, which cover the ping
  answer.

## rejected

| option | why not |
|---|---|
| Send `plugin.shutdown` first and kill after a wait | A hung module does not answer it, so the wait only delays the restart. |
| Count the ping through the breaker | A ping would hold a slot and mix a liveness signal with call health. |
| A 10 second evidence window, the breaker's own | Probes under an open breaker come every 30 seconds. The window would rarely hold one. |
| Kill on an open breaker alone | An upstream outage opens the breaker on answered errors. The module is not hung. |
| A hang limit of its own, apart from the switch-off | Two counters for one idea. Rule 25 of ADR-20261027-5m4 already counts every failed stop. |
