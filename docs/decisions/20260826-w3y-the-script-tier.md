---
title: "ADR-20260826-w3y: the script tier"
description: "That the script tier runs expr-lang/expr, that a script returns a message and never performs an effect, where an operator may point that message including this server's own event stream, what a script's environment may contain, and the four bounds a host applies before compiling anything."
date: 2026-08-26
status: accepted
tags:
  - adr
  - extensions
  - scripting
---
# ADR-20260826-w3y: the script tier

- status: accepted, while the runtime runs well and stays stable
- date: 2026-08-26

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) · Tiers: [ADR-20260824-nyw](20260824-nyw-extension-mechanisms-and-the-hot-path.md) · Descriptors: [ADR-20260825-rre](20260825-rre-one-registration-rule-and-one-descriptor.md)

**The runtime is linked into every build and the arm64 full budget is raised to
admit it.** That is rule 22. Rule 22 has the three gates a build tag would hide the code from,
and `scripts/check-binary-budget.sh` already makes that argument for the
developer view.

## rules

### the runtime

1. Use `github.com/expr-lang/expr`, v1.17.8 or later, as the only embedded script runtime. It is pure Go, declares no dependency outside the standard library, links no cgo, and is the only candidate measured whose grammar has no loop, no recursion and no function definition.
2. Pin the minor version and read the project's advisories before every bump. Two High-severity denial-of-service advisories have shipped against it, CVE-2025-29786 and CVE-2025-68156, and the fix for the first is incomplete in a way rule 12 exists to cover.
3. Keep the runtime in `internal/`. No package under `sdk/` imports it, because `sdk/` imports nothing outside the standard library. The `sdk/` side of the contract carries the event and message types and nothing else.
4. Compile a script once, at instance start, and pool one `vm.VM` per script. Compilation is 93 µs against 2.3 µs for a call, so compiling per event is a defect.
5. Report a compile failure as the instance's `failed` result, with expr's own message and caret unchanged. It names the line, the column and the token, and rewriting it loses that.

### what a script is

6. A script returns a message, or a list of messages, and the host publishes them. A script never performs the effect itself.
7. Do not put a function that performs I/O, opens a socket, writes a file or sends a request into a script's environment. One expression made 999,999 outbound host calls in 74 ms, so a host that lets a script initiate an effect cannot bound the effects it initiates.
8. Do not put a function into the environment that takes a URL, a host name, a port or a filesystem path from the script. The host names the destination; the script names a routing key and a body.
9. Declare the environment as a Go struct with concrete field types, never `map[string]any`. Against a map the checker accepts `event.jbo.name` and `event.printer_id + 1`, so a typed runtime handed an untyped event is a dynamic runtime.
10. Hand the script flat copies. Never a pointer to a live host object, never a value carrying a method, never a value with a cycle. Each has been reached: `$env` returns the whole environment, a method on a value in it read an unexported field, and a cyclic value reaches the recursion guard rather than the refusal a reader expects.
11. Hand the script one event. Never a collection sized by anything an operator controls: `sum`, `count`, `filter` and `all` over fifty million rows each took over a second, cost no memory, and cannot be interrupted.

### the bounds on a script

12. Refuse a script source longer than 8 KiB before calling `expr.Compile`. The parser is mutually recursive with no depth limit, roughly 1 to 2 KiB of stack per nesting level, and a deep enough expression ends in `fatal error: stack overflow`. `recover` cannot catch that, and it kills the host process. 8 KiB bounds the worst case to 8 MiB of stack. The most complex adapter written for earlier research is 873 bytes.
13. Never rely on `expr.MaxNodes` or a `recover` to bound the parser. Both were run against a 1,000,000-deep expression and both ended in the same unrecoverable crash, because the node budget is applied after the AST exists.
14. Disable `repeat` with `expr.DisableBuiltin("repeat")`. It is the only builtin that multiplies a length by a count, its internal cap is `1e6` iterations of a string the script may build, and `len(repeat(repeat("x", 999999), 4000))` made the host allocate 3,815 MiB before the memory budget refused it. A Pi is killed by the kernel first.
15. Set `expr.MaxNodes` explicitly rather than taking the default. The default of 10,000 nodes is on, and stating it makes the bound reviewable next to rule 12.
16. Leave `vm.MemoryBudget` at its default. It is set when the field is zero, and a host that constructs its own `vm.VM` and sets the field has raised it without meaning to.
17. Treat every bound as a host obligation, not a runtime property. Earlier research recommended this runtime because *"its containment is not a flag, a table or a budget that somebody has to remember to set"*, and rules 12 to 16 are the measured correction to that sentence.

### what a script may not be

18. Do not add a host function so a script can keep state, retry, sleep, batch across events, or branch on anything that happened before this event. The language has no assignment, so a mutable environment value is a side effect performed for its effect, which rule 7 refuses. An adapter that needs to remember is a tier 2 child.
19. Do not grow the environment's function surface to give a script a capability a library would supply. There is no `sprintf`, `base64`, `gzip` or `sha256`, and each one added is a change to our binary, which is the opposite of what the tier exists for. Two such additions mean the adapter is a tier 2 child instead.
20. Do not let a script name a type, register a resource, or reach the registry. The host registers on its behalf, as it does for every other tier.

### the shape the host provides

21. Supply typed constructors for the returned message rather than accepting a map literal. A map literal's keys and types are unchecked, so `{"endpont": "a"}` compiles and ships. A `msg(endpoint, key, body string) Message` constructor refuses it at compile and returns a value the host takes with a type assertion, deleting a 5,969 ns decode per event.
22. Link the runtime into every build. It goes behind no build tag, because a tag takes the tagged code out of the committed OpenAPI document, out of `golangci-lint run` and out of `go test ./...`, which is three gates traded for a fraction of a binary.
23. Raise the budget in the commit that first imports `expr`, in that commit and not before, and state the measurement in the commit body. Follow the rule `scripts/check-binary-budget.sh` states for every previous raise: the size measured on the day, plus the headroom that row already carried. On 2026-08-26 arm64 full measured 49,217,698 against a budget of 51,249,152, so the row carries 2,031,454, and +2,228,224 makes it **53,477,376**. Re-measure rather than copying that number, because the tree moves.
24. Do not raise a budget for a dependency no build links yet. There is nothing to measure until the import exists, and a row raised early is a row whose commit message cannot carry the measurement the gate exists to demand.
25. Shadow `toJSON` with a compact encoder. The builtin is `json.MarshalIndent(v, "", "  ")`, so every body a script builds with it goes on the wire pretty-printed.

### what a script may emit

A script sends data to an adapter, or listens to this server's events and
emits one of its own.

26. Let an operator point an endpoint name at this server's own event stream, written `event:<event type>`. The host publishes what the script returned there instead of posting it. The endpoint table stays the only thing that resolves a name. The operator names the destination and the event type, so a script chooses neither of the two things a reader routes on.
27. Refuse an `event:` destination whose type sits under `com.prusa.connect.`, at instance start. That namespace is closed and every type in it is a gated published identifier ([ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md)). An instance minting one puts a name on the wire that no vocabulary carries.
28. Publish an emission under the ref of the instance that ran the script, as class `state`. The origin is the host's, so a script cannot publish as a printer. The class is `state` because a fact a script asserts is not superseded by the next one. History is kept per origin and bounded, so a fast script fills its own instance's window and no printer's.
29. Never feed a sink an event a sink emitted. A script handed the event it emitted is a loop with nothing to end it. The federated link refuses the cycle the same way, with `Envelope.Hops` as the backstop rather than the detector. Anything that widens what a sink is fed carries this rule with it.

## enforcement

| gate | what it stops |
|---|---|
| `scripts/check-binary-budget.sh` | rules 22 to 24. The gate fails the moment `expr` is imported and keeps failing until that same commit raises the row, which is what forces the measurement into the commit body |
| a test that compiles an 8 KiB script and refuses an 8 KiB plus one script | rule 12. Mutation-check it by removing the length check and watching a 1.4 MB source kill the test binary |
| a test asserting `repeat` is absent from a compiled program's builtins | rule 14 |
| a test that every declared script environment type has no pointer, interface, method or slice field sized by config | rules 10 and 11. This is the rule most likely to decay, because it decays by somebody adding a convenient field |
| `internal/arch` | rule 3, that no package under `sdk/` imports `expr` |
| a conformance script in `sdk/plugintest` that exercises the environment | rules 6 to 9, and it is the artefact an author copies |
| `TestAnEventEndpointIsRefusedBeforeAnythingRuns` | rule 27 and the three other refusals an `event:` endpoint carries. Each was removed in turn and watched failing |
| `TestAScriptEmitsAnEventEveryReaderOfTheStreamSees` | rule 26 on the real composition root, with the frame read back off `/api/v1/events` |
| `TestAScriptCannotEmitInTheProjectsOwnNamespace` | rule 27 as an operator meets it: the instance's failed start, naming the namespace |
| `TestASinkIsNotFedAnEventASinkEmitted` | rule 29. The printer-origin control beside it is what stops the case passing against a feed that carries nothing |
| none | rules 18 and 19. They are a review judgement, and the honest statement is that a reviewer holds them or nothing does |

## rejected

| option | why not |
|---|---|
| a `publish()` function a script calls | one expression performed 999,999 outbound calls in 74 ms and returned cleanly. The host cannot bound an effect it did not initiate |
| an `emit(type, body)` function, so a script emits without an endpoint | the same measurement, and the type would be the script's as well |
| a script naming the event type beside the endpoint name | the type reaches an SSE event field and a messaging subject verbatim. An identifier a script mints is one nobody gated |
| a hop count as the guard against a script loop | it bounds a cycle rather than preventing one, and a new envelope field moves one of the four unpluggable things |
| refusing the direct self-loop alone | two scripts pointed at each other is the same loop one step longer. Rule 29 refuses the class, not the instance |
| a `fetch(url)` or any function taking a destination from the script | `fetch("http://169.254.169.254/latest/meta-data/")` issued the request. The host function is the only boundary there is |
| a mutable state map in the environment, so a script can remember | the language has no assignment. A host `set()` is an effect, which rule 7 refuses, and the requirement is a tier 2 child |
| `MaxNodes` or `recover` as the guard against a large script | both were run against a 1,000,000-deep expression and both ended in `fatal error: stack overflow` |
| the default memory budget as the guard against memory | 3,815 MiB was allocated by a 47-character expression before it fired |
| passing the event as `map[string]any` | the checker then accepts `event.jbo.name`. Measured twice, in earlier research |
| returning a map literal and decoding it host-side | the decode costs more than running the script, and the returned keys and types are unchecked |
| Starlark, gopher-lua or Teal instead, on earlier research's findings | every finding except the parser overflow reproduces on all three, and each of them adds the infinite loop expr cannot have |
| yaegi | earlier research: `go func() { for {} }()` needs no import and survives cancelling the interpreter |
| a script tier that can loop, so an adapter can be a program | a script maps, a program runs. The tier below it is a compiled child with a process boundary, and that is where a program belongs |

## open items

- Rules 1 to 21 and 25 to 29 are implemented. `internal/extension/script` is the host, `sdk/scriptapi` is the environment, `internal/kernel/ref` carries the `script` runtime and publishes it, and `internal/app/config/install` refuses it for every kind but a sink. A script is not a resource kind: it is a runtime a sink declares, so `ref.Kind` did not gain a member.
- **A script hears one fact, and it should hear all of them.** `sdk/eventsapi.Processor` carries five, `internal/app/sinks.go` produces one of them, `printer.state_changed`, and the hub carries six core types. One standardized set covers everything this server produces, a second contract for scripts is refused, and the shape goes to research before a record. Rules 26 to 29 are the emitting half and do not change; the listening half is widened, and the old backlog carries the work.
- Rule 29 holds structurally rather than by a check of its own. The feed carries a frame to a sink only when a printer published it, and an emission carries a sink origin. No redundant guard was added, because a second check behind the first one would pass whether or not it worked. The test names the property and the origin filter is what fails when it is removed.
- Rule 26 added no dependency, and it was measured rather than assumed. `expr` was already linked and the emit path reaches the standard library and the hub. One tree built twice on `linux/amd64` full, with the emit code present and reverted: 58,351,778 bytes and 58,343,586. Rules 26 to 29 cost **8,192 bytes**, and no row moved for them.
- An emission reaches every reader of the event stream. It does not yet reach another sink, for the reason in the second item: the sink feed carries one domain fact and that fact is a printer's. So sending data to an adapter is served for a reader outside this process and not for an adapter inside it. A standardized subscribable set closes this, because that set is what a sink would be fed from.
- **Rule 23's raise was not needed, and the number it rests on is 2.8 times the real one.** The measurement is on `cmd/prusa-connect` rather than on a probe. `linux/arm64` full is 49,873,058 bytes with the runtime stubbed out and 50,659,490 with `expr` linked. The cost is **786,432 bytes**, against the 2,228,224 earlier research measured. All six targets are green with the runtime linked and the tightest margin is 589,662 on arm64 full, so no row moved. Rule 24 is what says a budget is not raised for a cost nothing is paying.
- `scripts/check-binary-budget.sh` no longer claims a margin no run reports. Its comment carries the six measured margins from the run in the commit that linked the runtime.
- Rule 22 holds: the runtime is behind no build tag.
- Wall time is not bounded. There is no way to interrupt a running program, and the containment is the caller's. A script runs on the goroutine draining one instance's bounded queue, so a slow script slows that instance alone.
- Rules 18 and 19 have no gate and the enforcement table says so. The first adapter that asks for state is the test of whether a reviewer holds them.
- Every measurement behind these rules is `windows/amd64`. The stack thresholds in rules 12 and 13 have not been reproduced on `linux/arm64`, where the mechanism is the same and the numbers are not.
