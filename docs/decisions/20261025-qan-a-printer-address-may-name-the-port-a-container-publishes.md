---
title: "ADR-20261025-qan: a printer address may name the port a container publishes"
description: "The setup wizard's address printers use takes an optional port, and core.frontend.printer_address then names that port instead of the listener's, so a container published under another port gives printers an address they reach."
date: 2026-10-25
status: accepted
supersedes: []
amends: [ADR-20261015-ejz]
tags:
  - adr
  - first-run
  - printers
---
# ADR-20261025-qan: a printer address may name the port a container publishes

- status: accepted
- supersedes in part [ADR-20261015-ejz](20261015-ejz-a-new-install-is-configured-by-a-wizard.md)
  rule 36, on the port `core.frontend.printer_address` names. The rest of rule
  36 stands
- date: 2026-10-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20261015-ejz](20261015-ejz-a-new-install-is-configured-by-a-wizard.md)

A container on a bridge network is published under a port of the host, for
example `192.168.1.24:8481 -> 8080`. The server listens on 8080 inside the
container, and a printer on the network reaches it only at 8481.
ADR-20261015-ejz rule 36 wrote the listener's port into
`core.frontend.printer_address`, and the wizard had no field for another one.
The add-printer page then wrote `port = 8080` into the printer's settings file,
and the printer never connected. The server cannot learn the published port
from inside the container, but the browser knows the port through which it
opened the wizard.

## rules

1. The wizard's address printers use is a name or an IPv4 address, then an
   optional port written as `:port`, from 1 to 65535. It refuses IPv6 because
   Buddy printers connect over IPv4.
2. The limit of 20 characters and the refusal of a loopback name apply to
   the name or the address alone. A printer keeps its port apart from it.
3. With a port in the field, `core.frontend.printer_address` names that port.
   Without one, it names the printers' listener port. While printers share the
   web app's listener, it names the port through which the browser opened the
   wizard, because a container may publish the listener under another port.
   A new web listener address entered in the wizard names its own port instead.
4. A port in the field changes no listener. The printers' listener and the
   web app keep the addresses the wizard writes for them.
5. The field's hint says that in a container the address is the host's. A
   person adds a port when printers reach it through a different published
   port than the web page.

## enforcement

| rule | gate |
|---|---|
| 1, 2 | `encryption.test.ts`: a host with a port passes, and a scheme, a path or a port out of range is refused |
| 3, 4 | `encryption.test.ts`: the address names the browser's port or the port in the field, and the listener keeps its own |

## rejected

| option | why not |
|---|---|
| a second field for the port printers use | one more field on every install, and most installs need none |
| read the published port from the server | a process in a container does not see how its ports are published |
| leave the field empty in a container | the server only guesses whether a network is a bridge |

## open items

- The server logs a warning when it sees a bridge address, and the wizard does
  not show it. Showing it needs a field in the setup contract.
