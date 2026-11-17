---
title: "ADR-20261020-tca: a Fractal node keeps a shadow of the nodes above it"
description: "sdk/fractal and packages/fractal implement the Fractal specification of 2026-10-20: exposure per interface, a second shadow for the nodes above with AREP, ALOS and ARBR, extension data in the grant, a typ on every signature, and the new error codes. The core still runs without Fractal."
date: 2026-10-20
status: accepted
amends: [ADR-20260921-wkf]
tags:
  - adr
  - federation
  - fractal
---
# ADR-20261020-tca: a Fractal node keeps a shadow of the nodes above it

- status: accepted
- date: 2026-10-20

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Supersedes in part [ADR-20260921-wkf](20260921-wkf-fractal-is-a-module-and-the-protocol-is-in-the-sdk.md), rule 5 and the gates of rules 6 and 13

The Fractal specification lets a node read part of the state of the nodes
above it. A node keeps a copy of that state, called the ancestor shadow, next to
its copy of the state of the nodes below. Three messages fill it. AREP is the
report a node sends down to its children. ALOS is the message a node sends
down when it loses its parent. ARBR is the request a node sends up for a full
state report of the nodes above. This record says how `sdk/fractal` and the
module `packages/fractal` implement the version of the specification that
the section [the version this record implements](#the-version-this-record-implements)
lists, change by change.

## rules

### the specification and the core

1. `sdk/fractal` implements the version of the Fractal specification of
   2026-10-20. When `sdk/fractal` moves to a newer version, a new record that
   amends this one lists what changed, as the last section here does.
2. The core runs the same with the Fractal module disabled. Nothing in
   `internal/`, `adapters/`, `cmd/` or `compose/` imports `sdk/fractal`, and
   every change to running code this record makes is in `sdk/fractal` and
   `packages/fractal`.

### exposure

3. An interface definition states its exposure: `upward`, `downward` or `both`.
   A definition file without it does not load. The Prusa interfaces in
   `sdk/fractal/prusa` are all `upward`.
4. `node.Instance.Exposure` defaults to zero. Zero means the exposure of the
   definition. An instance with no definition is `upward`. `Register` refuses
   an instance whose exposure differs from its definition.
5. A node sends REP for an instance exposed upward, and AREP to every child for
   an instance exposed downward. A root has no parent and still sends AREP. It
   encodes an AREP once and sends the same bytes to every child.
6. Exposure applies to state only. A node calls methods only on nodes below it,
   never on an ancestor.

### the ancestor shadow

7. One store type, `shadow.Store`, holds both shadows. `node.Config.Ancestors`
   is the second store, with a selection of its own, and is empty by default.
   `Node.Ancestors` returns it.
8. A selection entry that names an interface with no instance id selects every
   instance of it. The store creates it when the first report for it arrives.
9. Each time a node gets a new Node Path, it rebuilds its ancestor shadow. It
   drops every node and recreates only the interfaces its selection names, of
   nodes above the new path. It then refuses to create a node that is not
   above that path.
10. A node drops an RPC, an AREP or an ALOS from its parent whose sender is not
    a node above it.
11. A node passes an AREP to every child, unless it dropped the AREP because
    its timestamp was older than the stored one. It passes it on also when it
    does not keep that interface.
12. ALOS marks stale the ancestors of its sender, and never the sender.

### ARBR and the parent link

13. One goroutine in `sdk/fractal/node`, the upkeep goroutine, owns the two
    ARBR timers. The window collects ARBR from the children. The limit says how
    often this node sends an ARBR of its own, and this goroutine sends it.
    When the window closes, it puts a signal in a channel with one slot. A
    second goroutine reads the signal and sends the answer. Neither uses the
    work queue, because that queue drops a job when it is full. The upkeep
    goroutine never calls an owner, so an owner that does not return delays
    only the answers. Windows that close during an answer get one more
    answer between them.
14. A node sends an ARBR of its own each time its parent link opens, within the
    limit, and never while the link is closed. A start or a new Node Path only
    marks one as needed, and the next opening of the link sends it. A new Node
    Path resets the limit.
15. The binding tells the engine about the parent link with `ParentConnected`
    and `ParentLost`. Both are methods of the `Engine` interface in
    `packages/fractal/natsbind`, so an engine without them does not compile.
    `fractaltest.Engine` has the same two methods, and `fractaltest.Link`
    calls them on the engine below, so the transport suite tests a transport
    against them. On `ParentLost` the node marks its whole ancestor shadow
    stale and sends ALOS to every child.
16. A full state report has the time the owner read the values.
    `Owner.Snapshot` returns that time, or zero for the current time.

### join and signatures

17. The grant, the parent's answer to a join request it accepts, has the join
    data of the extensions the parent offers. That data is a map from extension
    name to data that `sdk/fractal` stores and never reads. A child keeps the
    entries of the extensions it runs, saves them with its membership, and
    ignores the rest.
    An extension may refuse a grant before the parent link is open.
18. Every signature the identity key makes names its purpose in `typ`. A join
    request has `fractal-join+jws`, and a parent refuses any other value before
    it reads the rest.
19. A child that learns its admission was revoked discards its ancestor
    shadow in full with `Node.ForgetAncestors`. The binding calls
    `ForgetAncestors` on the engine when the node above answers a join
    request with `ERR_JOIN_REVOKED`, before the module stops. It is a method
    of the `Engine` interface, for the reason of rule 15.

### error codes and URLs

20. `ERR_OBJECT_NOT_FOUND`, the constant `fractal.CodeObjectNotFound` with
    value `0x4040`, answers a call to an instance or a method the node does not
    have, before the parameters are checked. In the bridge a printer is an
    instance. A printer that is unknown maps to `0x4043`, a printer that is
    gone to `0x4042`, and a method a printer does not have to `0x4041`. All
    three are refinements of `0x4040`.
21. `fractal.CodeNodeNotFound` is `0x4020`. `fractal.CodeNotFound` is a
    deprecated alias for it.
22. `PrintFile` and `UpgradeFromFile` take a string with no scheme as the path
    of a file the printer already holds. The bridge supports no scheme. It
    answers a string with a scheme with `0x4015`, a refinement of
    `ERR_INVALID_PARAMS`, and does not ask the printer.

### what replaces parts of ADR-20260921-wkf

23. Every package under `sdk/fractal` imports the standard library alone, so
    any node or tool speaks the protocol without this server. `sdk/fractal`,
    `sdk/fractal/shadow`, `sdk/fractal/schema` and `sdk/fractal/prusa` do no
    input and no output, start no goroutine and read no clock.
    `sdk/fractal/node`, `sdk/fractal/join` and `sdk/fractal/fractaltest` run a
    node and its tests. `node` and `fractaltest` start goroutines, and all
    three read the clock. This rule replaces ADR-20260921-wkf rule 5.
24. `sdk` and `packages/fractal` are Go modules of their own, so a gate runs
    its command inside the module. The gate of ADR-20260921-wkf rule 6 is
    `cd sdk && go test ./fractal/...`, and the gate of its rule 13 is
    `cd packages/fractal && go test .`.

### the leafnode address a server gives the nodes below

25. A server that accepts nodes below gives them its `accept.advertise`
    address as its leafnode address, with port 7422 when the address names
    none. A node below redials that address after a drop. The module refuses
    to start with an `accept.advertise` that has no scheme or no host.

## enforcement

| rule | gate | what it refuses |
|---|---|---|
| 1 | none. A reviewer checks the last section against the version | |
| 2 | `TestTheCoreImportsNoFractal`, in `go test ./internal/arch/` | a package of the core that imports `sdk/fractal`, test files counted |
| 3 | `cd sdk && go test ./fractal/schema/` | a definition without exposure |
| 3 | the vocabulary test, with `FRACTAL_SPECS_DIR` set | a Prusa file that disagrees with the specification |
| 4 to 14, 16, 17, 20, 21 | `cd sdk && go test ./fractal/...` | a change that breaks a test of the rule, and a conformance row with a missing test, an empty cell or a repeated sentence |
| 15, 17, 18, 22 | `cd packages/fractal && go test ./...` | a change that breaks a test of the rule, and a conformance row with a missing test, an empty cell or a repeated sentence |
| 4 to 18, 20 to 22, 25 | none. Review checks that a rule with no conformance row has a test | |
| 15 | `cd packages/fractal && go build ./...` | an engine without the parent link methods |
| 19 | `TestARevokedNodeForgetsItsAncestors`, in `cd sdk && go test ./fractal/fractaltest/` | a node that keeps its ancestor shadow, or creates an interface again from a later AREP |
| 19 | `TestARevokedNodeForgetsItsAncestorShadow`, in `cd packages/fractal && go test ./natsbind/` | a binding that does not tell the engine of a revocation |
| 19 | `cd packages/fractal && go build ./...` | an engine without `ForgetAncestors` |
| 23 | `make sdk-check` | an import outside the standard library in `sdk/`, test files counted |
| 23 | none for goroutines and the clock. A reviewer checks the four packages | |
| 24 | none. A reviewer runs the command the row names | |
| 25 | `TestTheLeafnodeAddressSentBelowIsTheGrantedOne`, in `cd packages/fractal && go test ./natsbind/` | a leafnode address sent below that is not the advertised one |
| 25 | `TestSettingsThisBuildWillNotTakeAreRefusedByName`, in `cd packages/fractal && go test .` | an `accept.advertise` with no scheme or no host |

## consequences

- file distribution, the next piece of work, reads the root hash of a store
  above from the ancestor shadow, and its join data is sent in the grant
- every credential renewal closes and opens the parent link, so the subtree
  marks its ancestor shadow stale and repairs it about every 18 hours at the
  default settings
- a child of an earlier release sends no `typ`, and a parent of this release
  refuses it. A parent of an earlier release accepts a child of this release.
  So a network updates its leaves first and its root last
- a node above that matched `0x4022`, `0x4021` or `0x4011` from a printer
  receives `0x4043`, `0x4042` or `0x4041`. A check of the base code sees
  `0x4040` where it saw `0x4020` or `0x4010`
- a node above tells a URL with a scheme, `0x4015`, from a value the printer
  refuses, `0x4013`

## rejected

### a separate package for the ancestor side

A package `sdk/fractal/ancestor` with its own store and message handling would
need the path state, the children, the send path, the clock and the owned
interfaces of `node`. `node` would need it back to route three message types,
so the two packages import each other or copy types. AREP would be built in two
places, and a second store would hold the stale-timestamp check, the limits,
the subscriptions and the status table a second time. A change in the
specification would touch both packages instead of one.

### a read-only type for the ancestor shadow

The program that builds the node builds the store and passes it in, so it holds
the full type anyway. `Node.Shadow` already returns the full store. The type
would be six methods that forward and protect nothing.

### an optional interface for `ForgetAncestors`

An engine without the method would keep the shadow of a revoked node, and
nothing would log it. Ending the process is not enough, because a program
that uses `sdk/fractal/node` with another transport keeps running.

### refusing an instance with no exposure

Every instance reports upward. Treating zero as `upward` keeps what runs and
breaks no program outside this repository.

### a URL scheme for a file on the printer

`file:///usb/benchy.bgcode` would change what every caller sends, and the
specification refuses only a scheme the node does not support. A path with no
scheme has none.

### a limit on forwarded ARBR

A child can send ARBR as often as it likes. Each window then makes every node
above it send a full report to its children and an ARBR to its parent. The
specification makes the answer to every window a MUST, and it sets no limit on
a forwarded ARBR. `fractal-requirements.md`, Security, says: "The protocol MAY
assume that nodes behave correctly and are not adversarial." This server takes
that assumption, so it does not defend against a node that floods its parent
on purpose.

### a size limit on extension data from the parent

The parent can send join data of any size up to the NATS payload limit, and a
child that runs the extension saves it with its membership. The parent is the
node the child chose to trust. The same sentence of `fractal-requirements.md`,
Security, applies: the protocol may assume that nodes are not adversarial, and
this server takes that assumption.
`JoinerConfig.CheckExtensions` can refuse the data before the child saves it.

## what is not decided

- whether the Fractal team counts a path with no scheme as a URL. If not,
  rule 22 changes to `file:`
- when the alias `fractal.CodeNotFound` is removed

## open items

- the binding misses most drops of the parent link that last about one poll,
  as the last section measures. It sees them once it reads the NATS system
  events for leafnode connections

## the version this record implements

The Fractal team published this version on 2026-10-20. `sdk/fractal` was
written against the version of 2026-09-04. A document name such as
`fractal.md` is a document of the specification. The vocabulary file
`interfaces.md` is now two files, `domain-common.md` and `domain-3d-print.md`.

| change | where | what a node must do |
|---|---|---|
| exposure | `fractal.md`, Interface Instance | report each instance in the directions its interface definition states: up by REP, down by AREP, or both |
| ancestor shadow | `fractal.md`, Shadow | keep a second shadow of the nodes above, apart from the shadow of the nodes below |
| AREP | `fractal.md`, AREP | send the state of a downward interface to every child, and pass an AREP from the parent to every child |
| ALOS | `fractal.md`, ALOS | on losing the parent, mark the whole ancestor shadow stale and send ALOS to the children |
| ARBR | `fractal.md`, ARBR | ask the nodes above for their full downward state after connecting to the parent |
| sender from above | `fractal.md`, Sender Validation | if a message from the parent has a sender field, the sender is an ancestor of this node |
| extension data in the grant | `fractal.md`, The Join Exchange | the grant has the join data of the extensions the parent offers, and the child keeps the data of the extensions it runs |
| purpose of a signature | `fractal-nats.md`, Proof of Possession | a join request has `typ: fractal-join+jws`, and a parent refuses any other value |
| renamed error code | `fractal.md`, Error Codes | `ERR_NOT_FOUND` is now `ERR_NODE_NOT_FOUND` |
| new error code | `fractal.md`, Error Codes | `ERR_OBJECT_NOT_FOUND` (`0x4040`) answers a call to an instance or method the node lacks |
| full state report time | `fractal.md`, REP | a full state report has the time the node read the state, not the time the state last changed |
| URL scheme | `domain-common.md`, Conventions | a method that takes a URL answers `ERR_INVALID_PARAMS` for a scheme the node does not support |
| TLS | `fractal-nats.md`, Security | support TLS on every connection. A deployment disables it only on a protected link or in a test |

### a short drop is mostly not detected

The binding polls the leafnode connections once a second. The NATS server
dials a closed leafnode connection again after its reconnect interval, also
one second. So a drop lasts about one poll, and a poll can miss it.

`TestAPollMissesMostShortDrops` in `packages/fractal/natsbind` measures it. It
cuts the link of a node for 10 ms, ten times, at the default settings, and
passes while it sees at most half of the drops. In 80 runs, the node above saw
1 drop of 10 each time. The node below saw 1 or 2 of 10. The test fails once
the binding reads the NATS system events for leafnode connections, which
`fractal-nats.md`, Connectivity Detection, asks for.

### moving to a newer version

1. Compare the new version of the specification with the table above.
2. Find each changed sentence in the conformance tables under `sdk/fractal`.
   A table pairs a MUST of the specification with the test that checks it.
3. Change the test first, then the code, then write the record that amends
   this one.
