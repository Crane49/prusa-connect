---
title: "ADR-20260916-b3x: instances announce themselves on the network"
description: "The core announces its own API socket over mDNS from a core block, core.discovery, on by default everywhere, with four TXT keys, one SDK interface for a module, and a responder of its own behind it."
date: 2026-09-16
status: proposed
tags:
  - adr
  - discovery
  - mdns
  - config
  - sdk
  - security
---
# ADR-20260916-b3x: instances announce themselves on the network

- status: proposed
- rule 7 is superseded in part by
  [ADR-20260925-jne](20260925-jne-printers-find-each-other-and-a-person-pairs-them.md) rule 55.
  A per-printer setting, `hidden`, also switches the announcer off. The other
  rules stand
- date: 2026-09-16

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20260910-cqq](20260910-cqq-graphql-and-the-old-schemas-are-modules.md), [ADR-20260824-gfr](20260824-gfr-the-extension-sdk-is-a-separate-module.md) | Research: 79, instances announce themselves on the network

A slicer on the same network finds a running instance by asking for a service
name over mDNS. mDNS is DNS sent to every machine on the local link. The core
has every value such an answer needs: the API port, the title, the build
version and whether TLS is on. Nothing in `internal/`, `sdk/` or `cmd/` sends
one. This record says what the core announces, where the switch is, what a
container logs, what a module may add through the SDK, and what the
responder is built on.

## rules

### what is announced

1. The core announces one service instance for each name in
   `core.discovery.services`. The shipped list is `_prusalink._tcp` and
   `_prusa-connect._tcp`. The first is the name a Prusa printer answers to
   today (earlier research, "PrusaLink announces three types and the firmware
   one"). The second says a server answered and not a printer.
2. The instance name is `core.frontend.title`. On a name conflict on the link
   the responder appends ` (2)`, as RFC 6762 section 9 asks, then ` (3)` and
   so on up to ` (100)`. A conflict on that last name makes it stop
   announcing. It logs that once, and the readiness report says `failed`.
3. The port is the API listener's port from `core.api.addr`
   (`DefaultAPIAddr`, `internal/app/config/config.go:35`). The host name is
   the machine's host name with `.local`. There is no second port.
4. The TXT record has these four keys and no other key:

   | key | value | source |
   |---|---|---|
   | `path` | `/` | a constant in the announcer |
   | `version` | the build version | `Options.version()` in `internal/app/options.go` |
   | `api` | `v1` | a constant in the announcer |
   | `tls` | `1` when the API listener uses TLS, absent otherwise | `core.api.tls` |

5. The record never contains a token, a password, a session, an account name,
   the site token or the setup state. The reason is in earlier research, "What an
   announcement discloses, and how the slicer signs in".
6. The announcer runs while a setup decision is still pending. A fresh install
   is the case a person scans for, and the first-run page is what they open.

### where the switch is

7. The block is `core.discovery` in `config/config.yaml`, with two keys:
   `enabled`, default `true`, and `services`, default the list in rule 1. The
   build reads nothing from the block. `enabled: false` is the one way to
   switch the announcer off.
8. The announcer starts after `bind` in `internal/app/listen.go` returns every
   listener, and it stops when the listeners drain. It has no lifecycle of
   its own.
9. A failure to announce is a logged warning. The core keeps serving.

### what happens in a container

10. The announcer is on in every deployment. In a container without host
    network the packets stay on the bridge and reach no machine, and the
    startup line says so. Nothing switches the announcer off by itself.
11. At startup the announcer logs one line naming every address it announces.
12. The process counts as containerised when `/.dockerenv` exists or
    `KUBERNETES_SERVICE_HOST` is set. In that case, when every announced
    address is a private address on one interface, the line adds the warning
    below. The words are fixed, and a test reads them.

    ```text
    announcing <instance> on <addresses> as <services>
    <address> is a bridge address that no machine on the network reaches; set network_mode: host in compose or hostNetwork: true in Kubernetes
    ```

### what the SDK exposes to a module

13. `sdk/discoveryapi` declares one interface, `Announcer`, with two methods.
    `Announcement(ctx)` returns what the core announces: instance name, host,
    port, TLS, the service list, the TXT keys and the addresses. `Add(ctx,
    Record)` adds one record for the module. A module reaches both over the
    wire as `discovery.read` and `discovery.add`, two methods beside
    `routes.serve` (`sdk/routesapi/module.go`).
14. A `Record` has one field, the service name. The host fills the rest. The
    port and the instance name are the core's. `path` is the module's mount,
    `/api/v1/modules/<namespace>`, and `version` is the module's version
    from its descriptor.
15. A module never adds a secret, and the SDK gives it no field for one. No
    string a module supplies reaches the TXT record. The keys are the four of
    rule 4, and the values come from the mount and the descriptor.
16. A module never opens UDP 5353 and never runs a responder of its own. The
    core's announcer is the only one, under ADR-20260827-wmv rule 13.

### what is refused

17. A TXT value built from a config field other than `core.api.tls` fails the
    gate. The instance name is not a TXT value and reads `core.frontend.title`
    outside that file.
18. A second announcer fails the gate: no package outside `internal/discovery`
    imports the packages of rule 22. `sdk/` imports only the standard library
    (ADR-20260824-gfr), so a module cannot import them either.
19. A `services` entry fails the config load unless it has the shape
    `_<name>._tcp` or `_<name>._udp`, with a name of 1 to 15 letters, digits
    and hyphens (RFC 6763 section 7).

### how a client pairs

20. Pairing is a credential minted by `POST /api/v1/auth/credentials`, shown
    once, and pasted into the client as a bearer token. This record adds no
    pairing route.
21. A code-approval flow, where the client asks and a person approves in
    Settings, is its own research with the slicer team and its own record.
    The announcement has no field that flow would change.

### what the responder is built on

22. The responder is the core's own, in `internal/discovery`. It is built on
    `github.com/miekg/dns` for the DNS messages and on `golang.org/x/net/ipv4`
    and `golang.org/x/net/ipv6` for the multicast sockets. Both modules have
    the BSD licence and are pinned to an exact version under the
    security-supply-chain rule.
23. The responder answers PTR, SRV, TXT, A and AAAA records. It compares
    names in their wire form with ASCII letters in lower case (RFC 6762
    section 16). After a random wait of up to 250 ms it probes for a name
    conflict, and renames on one (RFC 6762 sections 8 and 9). It announces
    twice, and again on an interface that appears. When it stops announcing
    on a link it sends a goodbye there, on the interface's index, which
    reaches the link while the system still has the interface. It sends a
    record to one place at most once a second, or once in 250 ms in answer
    to a probe, and remembers the last 1024 answers for that. A multicast
    answer with a shared record waits 20 to 120 ms. It answers only a packet
    that arrived on an interface it announces on.
24. The process has one mDNS socket for each address family, and the
    announcer and the listener share it. The socket starts two readers and
    a watch that reads the interfaces every ten seconds, and the announcer
    starts one loop. The listener adds its own loop, a goroutine that hands
    it the packets, and one short goroutine for each question it sends.
    Stop ends every one of them.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 to 4 | a test in `internal/discovery` that browses its own record over loopback | exists |
| 4, 5, 17 | the TXT-source gate in `internal/arch`, which parses the file that builds the record | owed; a test in `internal/discovery` refuses a fifth key |
| 7, 19 | the strict loader (`internal/app/config/strict.go`) refuses an unknown key | exists |
| 8, 9 | a test in `internal/app` that the announcer starts after `bind` and stops on drain | exists |
| 10 to 12 | a test that fixes the words of the two lines | exists |
| 13 to 15 | `make sdk-check`, and a wire test for `discovery.read` and `discovery.add` | the first exists; the second is owed |
| 16, 18, 22 | an import gate in `internal/arch` over the packages of rule 22 | exists |
| 23, 24 | the responder and link tests in `internal/discovery`, over a fake network and over the real one | exists |
| 20, 21 | none. A pairing route needs its own record | none |

## consequences

- two modules in `go.mod`, and one UDP socket on 5353 for each address
  family in the process, shared by the announcer and the listener
- a new package under `sdk/`, `sdk/discoveryapi`, and two wire methods
- a `core.discovery` block in the shipped file, on by default
- one warning line in every shipped container until the operator changes the
  network mode

## rejected

| option | why not |
|---|---|
| a shipped module as the announcer | it must run before any module is installed, and it would be a `sink` with no events |
| `_octoprint._tcp` in the shipped list | it reaches only the 2.x slicer dialog, and the 3.0 build does not compile that dialog |
| `site` and `setup` in the TXT record | both describe the deployment to anyone on the link and no client reads them |
| off by default inside a container | one default everywhere, and the log line says why nothing is found |
| a module-supplied TXT string | the one path by which a secret could reach the link |
| `github.com/hashicorp/mdns` | it answers queries and never probes, so two instances with one name go unnoticed |
| `github.com/grandcat/zeroconf` | one tag ever, and no push in 33 months |
| `github.com/pion/mdns` | no PTR, SRV or TXT record, so no service query is answered |
| `github.com/brutella/dnssd` | it takes no socket, so nothing drops a packet from off the link, and each probe leaves goroutines behind |

## open items

- `internal/discovery` announces the core's own socket. Nothing under `sdk/`
  does, so rules 13 to 15 are still owed.
  The old backlog is the work
- the slicer has no Prusa Connect host type, so the Connect name in rule 1 is
  agreed with the slicer team, and the `services` key is where it changes
- the pairing flow of rule 21 has no research note yet
