---
title: Creating a new printer connector
description: The Connector interface and the Environment struct this page described are gone. What replaced them, and where a driver is written today.
audience: build
rewrite:
  describes: superseded
  implementedBy:
    - sdk/connectorapi
    - adapters/connector/echo
---

:::caution[Superseded]
The `Connector` interface and the `*Environment` struct this page documented no
longer exist. A driver implements `sdk/connectorapi` instead. It declares what a
printer can do, and the host validates every command against that declaration
before the driver sees it.

To write a driver, start at [writing a printer driver](./writing-a-driver.md).
:::

One idea from the old interface is still true, because it is a fact about
printers rather than a choice about code. A dialect reaches a device in one of
two directions, and the host treats both the same way once a session is open.

| direction | who opens the connection | interface |
|---|---|---|
| outbound | the host dials the device on an address configuration names | `Outbound` in `sdk/connectorapi/direction.go` |
| inbound | the device dials the host and identifies itself in the handshake | `Inbound` in the same file |

Prusa printers are inbound: nothing in configuration names them, so there is
nothing for the host to dial. An inbound driver returns an `http.Handler` and
the host mounts it under a prefix the driver never sees.

Whichever direction it is, the host resolves the same capability tiers over the
driver's `Baseline`, validates each command against the resolved descriptor, and
supervises the session itself. A driver translates one command and reports
state, and does nothing else.

## Where to read the real thing

| what | where |
|---|---|
| the contract a driver implements | `sdk/connectorapi` |
| the conformance suite every driver passes | `sdk/connectorapi/connectorapitest` |
| the reference inbound driver | `adapters/connector/echo` |
| the shipping Prusa driver | `internal/connector/xbuddy` |
| the scaffold that writes a new driver | `cmd/scaffold-driver` |
| what the host promises in return | [the connector contract](./connector-contract.md) |
