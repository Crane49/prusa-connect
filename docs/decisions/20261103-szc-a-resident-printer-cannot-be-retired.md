---
title: "ADR-20261103-szc: A resident printer cannot be retired"
description: "A dialect says its printers are the machines the server runs on. The host refuses to retire such a printer, and the device record tells a client so."
date: 2026-11-03
status: proposed
supersedes: []
amends: []
tags:
  - adr
  - devices
  - sdk
---
# ADR-20261103-szc: A resident printer cannot be retired

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

Some printers run this server themselves. The printer's own system starts the
server, and its driver reaches only that one machine, over a bus inside the
printer. Retiring such a printer does not remove it. The driver connects again
within a second, and the host gives the machine a new device id. The new
record has no name, no tags and no grants, and the history is split between
the two ids. A resident printer is a printer whose driver runs on the same
machine as the server.

## rules

1. A driver declares that its printers are resident in `Dialect.Resident`. The
   flag is for a driver that reaches only the machine the server runs on. A
   driver that reaches printers over a network leaves it false.
2. The host refuses to retire a device of a resident connector, with
   `DEVICE_RESIDENT` and status 409. It checks the caller's permission first,
   so a caller who may not administer the device gets the same refusal as for
   any other device.
   The connector is not asked to forget the device, and the record is not
   changed. A record that is already retired is not refused, so a repeated
   removal still answers 204.
3. The device record has `resident: true` for a device of a resident
   connector, and leaves the field out for every other device. A client hides
   the controls that retire such a device.
4. Renaming and tagging a resident printer stay allowed. Only the retirement
   is refused.

## enforcement

- Rule 1: `TestAResidentDialectReachesTheRegistration` in `sdk/connectorapi`.
- Rule 2, in `internal/app`:
  - `TestAResidentPrinterIsNotRetired`
  - `TestAResidentPrinterRefusesAViewerOnPermission`
  - `TestARetiredResidentPrinterAnswersARepeatedRemoval`
  - `TestAPrinterOfANetworkConnectorIsStillRetired`
  - `TestTheServerRefusesToRetireThePrinterItRunsOn`
- Rule 3: the same `internal/app` tests check the field. The frontend tests of
  the printer card, the printer info panel and `printerRowOf` check that no
  control to remove a resident printer is drawn.
- Rule 4: `TestAResidentPrinterIsRenamed` in `internal/app`.

## rejected

| option | why not |
|---|---|
| a field `retirable: false` on the device record | it says what is refused and not why. A client that shows a reason has to guess it |
| take `administer` away from a resident printer | renaming and tagging take `administer` too, so they would be lost with it |
| let the driver refuse the connection after a retirement | the printer would vanish from the server that runs on it, and nothing could add it again |
