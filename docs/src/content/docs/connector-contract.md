---
title: The connector contract
description: What a printer driver implements, what the host does before and after it, what is guaranteed about ordering and delivery, and what a driver must never do.
audience: extend
rewrite:
  describes: current
  implementedBy:
    - sdk/connectorapi
    - internal/printer/capability
    - internal/app/connectors.go
---

Two halves of one contract. A driver implements the interfaces below; the host promises what is on this page in return, and nothing beyond it. Where a promise is not kept yet, the row says so and names the file, because a guarantee an author cannot check is not a guarantee.

The [authoring guide](./writing-a-driver.md) is the path from nothing to a driver that passes the suite. This page is what that driver is held to.

## Key files

| path | purpose |
| --- | --- |
| `sdk/connectorapi/driver.go` | `Driver`, `Session`, `Identity`, `Command`, `Result`, `StatePatch` |
| `sdk/connectorapi/direction.go` | `Outbound`, `Inbound`, `Arrivals`, and `Directions` |
| `sdk/connectorapi/set.go` | the capability set, its intersection and its digest |
| `sdk/connectorapi/validate.go` | the host-side argument check, and the codes it returns |
| `internal/printer/capability/session.go` | where that check runs, above the driver |
| `internal/app/connectors.go` | how a driver is built and mounted |
| `internal/app/devices.go` | the host's `Arrivals`, and the goroutine that reads a session |

## What a driver implements

| member | called | must |
| --- | --- | --- |
| `Driver.Baseline` | when the host wires the driver, and more than once by the conformance suite | be pure, return a fresh set, and be the ceiling of everything below it |
| `Outbound.Connect` | when the host dials a device | narrow the profile it was handed, never widen it, and return a session |
| `Inbound.Handler` | once, when the host wires the driver | route from its own root, never from where it was mounted |
| `Session.Identity` | after the session exists | carry a non-empty `Model`, or no profile can ever be selected |
| `Session.Capabilities` | any time | return a copy, and not move within an epoch |
| `Session.Do` | per command, from many goroutines | refuse a verb the resolved set does not carry, and never report OK for an outcome it did not observe |
| `Session.State` | once per session | return a non-nil channel that closes when the session does |
| `Session.Close` | possibly more than once | be idempotent, and close the state channel |

A driver implements `Outbound`, `Inbound` or both. One that implements neither is a startup error rather than a printer that never appears, which is what `connectorapi.Directions` reports.

## What the host does before a driver is reached

| the host | where |
| --- | --- |
| resolves three tiers by intersection: the driver's baseline, the profile for the reported identity, the device's own probe | `internal/printer/capability/resolver.go` |
| prunes a merged key the descriptor rules refuse, and drops a verb whose required argument went with it | `sdk/connectorapi/problems.go` |
| refuses a verb the resolved set does not carry, an argument the verb does not accept, a value outside its kind, its supported set or its bounds | `sdk/connectorapi/validate.go` |
| fills in a declared default for an argument the caller left out | `sdk/connectorapi/validate.go` |
| decides whether it owns a printer that dialled in, and answers with the profile tier | `internal/app/devices.go` |

A driver never re-checks an argument it declared. That check is one implementation above every dialect instead of one inside each, and it is what makes a new firmware revision a profile edit rather than a release.

## What the host promises about a live session

| promise | held by |
| --- | --- |
| a session is admitted before it is announced. One that arrives without an admission is refused with `ErrSessionNotTaken` | `internal/app/devices.go` |
| exactly one goroutine reads `State()`, in the order the driver sent the patches | `internal/app/devices.go` |
| the host owns the session from `Arrived` and closes it when the state channel closes, so closing that channel is how a driver says the printer went away | `internal/app/devices.go` |
| one printer has one entry. A session superseded by a newer one for the same printer is closed | `internal/app/devices.go` |
| a panic in an inbound driver's handler, or in `Do` reached from a route, is answered as a problem document rather than a dropped connection | `internal/transport/http/httpapi/middleware.go` |
| a request body a device sends is bounded before it allocates | `internal/transport/http/httpapi/middleware.go` |
| the host commands against the resolved set, and a driver's own `Capabilities` does not override it | `internal/printer/capability/session.go` |

## What the host does not promise

An earlier design decision says the host owns queue, drop policy, timeout and panic recovery for every third-party call. That is built, in `internal/extension/supervisor`, and it is applied to supervised resource instances. A connector is not one of them.

| not promised to a connector | consequence |
| --- | --- |
| a per-call deadline of the host's own | `Do` is bounded by the caller's context and by the HTTP server's timeouts, and by nothing else |
| a circuit breaker | a device that fails every command is asked again on the next request |
| a bulkhead | concurrent commands to one printer are as concurrent as the callers are |
| a bounded command queue | whatever queueing exists is the driver's own. `adapters/connector/echo` bounds its own and fails a command rather than deepening a queue nothing drains |
| a restart ladder | a connector's lifetime is the process's. The reason is in `internal/app/connectors.go`: its routes are mounted on a sealed router, and a handler swapped underneath a mounted route would be a device wire that changed identity while a printer was on it |
| recovery of a panic on a goroutine the driver started | it takes the process down, as any panic on an unrecovered goroutine does |

A driver that needs any of those provides them itself, and says so in its descriptor's description.

## What a driver must not do

1. Do not widen. A profile or a probe subtracts; nothing adds. `connectorapi.Set` has no union and is not getting one.
2. Do not re-check an argument the descriptor declared. The host checked it, and a second check is a branch that will disagree with the first.
3. Do not report `OK` for a command whose outcome was not observed. A cancelled context, a closed session and a timeout are all failures.
4. Do not name a parameter in `Result.Ignored` or `Result.Substituted` that the caller did not send.
5. Do not hand out the live capability map. A caller that mutates it moves the set the digest describes.
6. Do not close the state channel from anywhere except its single writer. Two closers is a panic under load and works in a unit test.
7. Do not leave the state channel open after `Close`. Every subscriber ranging over it blocks for the life of the process.
8. Do not encode the prefix the host mounted an inbound driver under. The host strips it, and a driver that writes absolute paths works at the root and nowhere else.
9. Do not change what a capability key means. A key whose meaning changes takes a new name, and the old one is retired in `internal/printer/profile/reserved.json` for good.
10. Do not order firmware labels as text. `6.4.0` sorts above `6.10.0`, so a version comparison decides nothing here. Behaviour comes from the capability set.
11. Do not block in `Baseline`. It is called while the host is wiring the driver, and it reads nothing.

## Errors a driver returns

A command that fails carries a registered code, so a caller branches on the code rather than on a message. The codes host-side validation returns are in `sdk/connectorapi/faults.go` and are published on the [error reference](./error-codes.md).

`connectorapi.CapabilityNotSupported` is the one code a driver raises itself, for a verb the resolved set does not carry. The host returns the same code when it refuses a verb before the driver is reached. Every other code on that list is decided above the driver and cannot reach a caller as something the printer said.

## Registration

| rule | consequence |
| --- | --- |
| a driver registers from `init`, through `connectorapi.Register` | a build reaches it through a blank import |
| the namespace is derived from the registering module | nothing an author passes selects it, so two authors cannot land on one name |
| the name is a DNS-1123 label | one string is legal as a config key, a metric label, a URL segment and a subject token at once |
| the descriptor's `Summary`, `Description`, `Version`, `Contract` and `Stability` are mandatory | a registration missing one panics at startup rather than publishing an empty cell |
| `Contract` is the contract version the driver compiled against | a host serving another major refuses it at registration rather than at first use |
