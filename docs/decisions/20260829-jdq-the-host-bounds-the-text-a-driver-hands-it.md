---
title: "ADR-20260829-jdq: the host bounds the text a driver hands it"
description: "Where a bound on driver-supplied text lives, the number each bound is taken from, what happens to a value over it, and what counts the loss."
date: 2026-08-29
status: accepted
tags:
  - adr
  - connectors
  - sdk
  - memory
---
# ADR-20260829-jdq: the host bounds the text a driver hands it

- status: accepted, rules 1 to 6 are gated, rules 7 and 8 are review
- date: 2026-08-29

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260824-gfr](20260824-gfr-the-extension-sdk-is-a-separate-module.md), [ADR-20260825-zkq](20260825-zkq-api-compatibility.md)

A printer sends a refusal reason, a dialog title, a filename. This server keeps
it, answers with it, and hands it to every subscriber. Nothing measured how big
it was.

Two figures, observed on this branch. One command answered **1,048,576 bytes**
of reason a printer chose. One `STATE_CHANGED` put **1,048,578 bytes** at
`/dialog/title`, and the state tree held it for the life of the dialog.

The owner refused to hard block a number nobody can justify. So every number
below is taken from a published contract for the same shape of field.

## rules

1. Bound driver-supplied text in the **host**, and nowhere else.
   `sdk/connectorapi` states a floor and carries no cap.
   `sdk/connectorapi.RetainedTextFloor` is that floor, and it refuses nothing.
   A dialect carries its own frame limit and no retention bound. A cap in
   `sdk/connectorapi` loses because `Result` and `StatePatch` are structs. No
   SDK code runs when a driver fills one in, so the number would refuse a value
   the driver has already allocated. It would also freeze into a module that is
   about to become its own repository under
   [ADR-20260824-gfr](20260824-gfr-the-extension-sdk-is-a-separate-module.md). A bound each
   driver applies loses because retention cost belongs to the host and the
   fleet, and a community author knows neither.
2. Hold any single free-text value the host retains to **32,768 bytes**.
   `machinetext.MaxBytes` at `internal/kernel/machinetext/machinetext.go:23` is
   that number and the only copy of it. It is Kubernetes' bound on
   `Condition.message`, which is the same shape of field, and this tree already
   declares it at `internal/kernel/condition/condition.go:16`. Every reader is
   held to it: the state tree, the command answer, the raw gcode answer, the
   developer command log and the audit line. Keeping 256 loses. Measured against
   `prusa3d/Prusa-Firmware-Buddy` `src/lang/po/` on 2026-08-29, **98 of 14,368**
   translated strings in the eight shipping catalogues are over 256 bytes, and
   none is over 512. A number sized to the widest of those, 444 bytes, loses as
   well: it fits the one dialect anybody here has measured and no other.
3. Cut a value over the bound on a rune boundary and mark it with `…`. A cut
   through a multi-byte character reaches a JSON encoder as the replacement
   character, and most of what a firmware writes is not in English. A value over
   the bound that is not a JSON string is refused instead of cut. Half an object
   is not JSON, and a client would fail to parse what this server wrote.
   Refusing the value outright, on Kubernetes' model, loses: a printer cannot be
   told and cannot send it again. Dropping it silently loses too, because the
   marker is what tells a reader whose cut they are looking at.
4. Hold what one device's whole state tree retains to **256 KiB**, and let an
   operator raise it. `core.device_state_budget` is the key and
   `DefaultDeviceStateBudget` is the number, which is Kubernetes' bound on every
   annotation of one object. A full xBuddy tree is 20 paths and 1,293 bytes with
   the widest dialog the firmware can render, so a real printer spends half a per
   cent of it. A fixed cap loses because a design requirement names a Raspberry Pi and
   the same binary runs a four hundred printer fleet. No bound at all loses
   because paths cost a driver almost nothing. One driver put 10,000 paths of
   three bytes each on one tree and nothing refused any of them.
5. Over the budget, refuse a **new** path and still take a new reading for a path
   the tree already holds. Never cut a path, because a cut path addresses a
   different node. Refusing every write loses: a device that described itself too
   fully once would lock itself out of `/state` for the rest of the session.
   Evicting the oldest path loses too. A state tree is not a log, and a client
   cannot tell an evicted `/state` from a machine that stopped reporting.
6. Count every value cut and every path refused, by driver, and never per
   printer. `pcl_printer_state_bounded_total` carries a `bound` label of
   `clipped` or `refused`. The per-printer fan-out allowance is six series and
   `observability.Fleet` already holds five, so two more there would spend the
   whole remaining budget on a defect. The count is taken by the fleet sampler
   from totals the entry carries, because resolving a metric handle inside
   `record` is a label hash on the frame path.
7. Give `Result.Raw` no bound, because no host record retains it. It reaches the
   driver's own caller and stops there. A bound would cost a measurement on every
   result and save nothing. This settles
   earlier research open
   item 2. The day a host record keeps `Raw`, rules 2 and 3 apply to it as
   written.
   - **Premise defended by [ADR-20260901-atx](20260901-atx-durable-command-records.md) rule
     9.** Command outcomes are durable since that record. So "no host record
     retains it" is no longer a fact about the absence of stores. It is rule
     9's prohibition, gated by a case asserting `Raw` appears in no published
     payload. The day that rule moves, rules 2 and 3 here apply as written.
8. Leave what a dialect retains in its own session to the dialect. Rules 2 to 5
   bound what the **host** keeps, and a driver holding a second copy of a value
   is memory this record does not reach. A dialect that retains a device's text
   bounds it where it retains it.

## what each reader kept before this record

| reader | before | after |
|---|---|---|
| the state tree, read by every subscriber | none | rule 2, and rule 4 over the whole tree |
| the HTTP command answer | none | rule 2, in `refusalReason` |
| the raw gcode step answer | none | rule 2, through the same `refusalReason` |
| the developer command log | 256 bytes | rule 2 |
| the raw gcode audit line | 256 bytes | rule 2 |
| a dialect's own session | none | rule 8, which is the dialect's |

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `TestTheHostKeepsAtLeastTheFloorTheDriverContractPromises`, over both numbers | exists |
| 2 | `TestAnOversizedTelemetryValueIsClippedInTheStateTree` in `internal/app` | exists |
| 2 | `TestAnOversizedMachineReasonIsClippedInTheCommandAnswer` in `internal/app` | exists |
| 2 | `TestALongRefusalReachesTheCallerAndIsClippedInTheLog` in `httpapi` | exists |
| 3 | `TestAClippedValueIsStillTextAndSaysItWasCut`, over four character widths | exists |
| 3 | `TestTextInsideTheBoundIsTheDevicesOwnWord`, the half that must not cut | exists |
| 3 | `TestEveryConstantBoundedTextCutIsRuneSafe` in `internal/arch`, over every tree but `legacy/` | exists |
| 4, 5 | `TestATreeOverItsBudgetRefusesANewPathAndStillUpdatesAnExistingOne` | exists |
| 6 | `TestAClippedValueIsCounted` in `internal/app` | exists |
| 6 | `TestAScrapeCarriesWhatOnePrintersOwnWordsCostTheHost`, over the exposition | exists |
| 7 | none. A field nothing retains has nothing to measure | review |
| 8 | none. A dialect's own memory is the dialect's | review |

## rejected

| option | why not |
|---|---|
| a cap in `sdk/connectorapi` | a struct field. The driver allocated the value first, so the number saves nothing and blocks an author |
| a negotiated cap, on MQTT 5's model | the right mechanism with nowhere to put it. `Identity` has no limits field |
| keeping 256 as the one bound | 98 of 14,368 strings the shipping firmware displays are over it, measured |
| a bound sized to the widest catalogue string | 444 bytes fits the one dialect measured here and says nothing about the next |
| refusing an oversized value, on Kubernetes' model | right for an API a person drives. A printer cannot be told and cannot resend |
| cutting a value that is not a JSON string | half an object is not JSON, so the tree would stop parsing |
| evicting the oldest path over budget | a state tree is not a log. An evicted `/state` looks like a machine that went quiet |
| a fixed per-device tree budget | a design requirement names a Raspberry Pi, and the same binary runs a four hundred printer fleet |
| per-printer counters for the two losses | the fan-out allowance is six series and `observability.Fleet` holds five |
| counting inside `record` | one printer sends fifteen patches per frame at four hertz, and a handle there is a label hash on the frame path |
| spilling a large value to storage | a design requirement is an air-gapped Pi, and a state tree needing a disk is a second failure mode |
| relying on the dialect frame limit alone | it is per frame. A printer sending one oversized title a second is inside it every time |

## open items

- `internal/connector/xbuddy/inbound.go` keeps `dialogFields` for the life of a
  dialog, and rule 8 leaves that to the dialect. It is unbounded today, and the
  row is on the old backlog.
- The departed table at `internal/app/devices.go` keeps a device's whole tree
  after it leaves, so rule 4's ceiling is 800 trees rather than 400. Whether a
  departed device needs its tree or only its last state is earlier research open
  item 3 and is not settled here.
- A printer's own words reach a client on one more path, and no rule here
  bounds it. `humanReason` at `internal/connector/xbuddy/filemanager.go:71`
  reads the firmware's sentence out of `Result.Raw` and puts it in a problem
  document, beside an unbounded `machine_reason`. Three call sites do it, in
  `filemanager.go` and `transfer.go`. Nothing is retained, so rule 7 still
  holds, and the answer is still a string a printer sizes. The row is on
  the old backlog.
- Nothing in CI sends a value near any bound over a real socket. The emulator's
  widest dialog is 46 bytes, so every gate above drives the plane directly or
  writes the frame onto the socket itself.
- Every catalogue figure is Prusa's. A community driver for a device nobody has
  seen is the case these rules are written for and the case no measurement here
  covers.
- The 256 KiB budget was reached in a test and the 200 MiB worst case behind it
  is arithmetic. It was not measured with 800 trees at the full budget.
