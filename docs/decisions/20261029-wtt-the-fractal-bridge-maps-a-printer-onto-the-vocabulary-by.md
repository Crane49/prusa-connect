---
title: "ADR-20261029-wtt: the Fractal bridge maps a printer onto the vocabulary by its state and its tools"
description: "The bridge in packages/fractal reports a printer that is away as BLOCKED, follows the state rules of domain-3d-print.md before it sends a command, and exposes one instance per print head and per filament slot."
date: 2026-10-29
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - fractal
---
# ADR-20261029-wtt: the Fractal bridge maps a printer onto the vocabulary by its state and its tools

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20261020-tca](20261020-tca-a-fractal-node-keeps-a-shadow-of-the-nodes-above-it.md), [ADR-20261025-pze](20261025-pze-the-tool-tree-is-the-only-place-a-tool-lives.md)

The bridge in `packages/fractal/bridge` shows each printer of this server as
interface instances of the Prusa vocabulary, `domain-3d-print.md` of the
Fractal specification of 2026-10-20. `mapping.json` names the reading behind
each property and the command behind each method. This record says how the
bridge fills `Printer1.state` where the printer's own word does not say it,
and when it refuses a call.

## rules

### a printer that is away

1. A printer the host reports as offline, or no longer lists, reads `BLOCKED`
   in `Printer1.state`. The property may not be null, and Fractal has no
   stale mark for one interface instance. Every property read from the
   state tree that may be null reads null. The properties read from the
   host's record of the device, such as `model` and `serialNumber` of
   `Device1`, keep their values. The host keeps that record while the
   printer is away, and the values say which printer it is, not what it
   does. A reading that arrives while the host reports the printer as
   offline, or after it stopped listing the printer, is dropped. The host
   can publish such a reading, from the queue of a printer that has just
   disconnected. The host stores every reading before it publishes it, so
   the bridge reads the dropped readings again when the printer connects.
2. A non-nullable property with a value table and no reading takes the default
   of the table. A non-nullable property with no value table and no reading
   is omitted from a full report: `speed`, `flow`, `firmwareVersion`, `vendor`
   or `type`. `fractal.md` asks for the full state, and the vocabulary gives
   no value to send in its place, so any value the bridge sent would be false.
   So the node above keeps the last `speed` and `flow` of a printer that is
   away, beside `state` `BLOCKED`. A report that omits a property does not
   remove it from the shadow above (`fractal.md`, Reported Properties
   Message).

### a dialog over a print

3. A printer reports `ATTENTION` while a dialog is on its screen. While the
   word at `/job/state` is `PRINTING` or `PAUSED`, the print is still on the
   bed and held, so `Printer1.state` reads `PAUSED`. With no print it reads
   `BLOCKED`. `CancelJob` and the other job methods then follow the state
   rules of the vocabulary. `ResumeJob` on a print held by a dialog gets
   `0x2012`, because the firmware resumes only a print it reports as
   `PAUSED`.
   A printer shows a preview dialog after a print starts, and `/job/state`
   names no running print yet. So `state` reads `BLOCKED` until a person
   confirms the dialog, where `domain-3d-print.md` says `PrintFile` does not
   change `state`. No reading says that a print is about to start, so the
   bridge follows the printer's word.

### state rules before a command

4. The host checks none of the state rules `domain-3d-print.md` gives a
   method, so the bridge checks them before it sends a command or starts a
   fetch. A method row in `mapping.json` lists the states it is accepted in
   under `onlyIn`, or refused in under `refuseIn`. A call the state forbids
   gets `0x2013`, a refinement of `ERR_DENIED`, and nothing reaches the
   printer. A call for a printer the host no longer lists gets `0x4042`
   before the bridge checks its parameters, because `fractal.md` resolves
   the target of a call first. The node checks the parameter types against
   the interface earlier, so a wrong type still gets `0x4010`. The answer
   also comes before the URL checks of ADR-20261023-7r3 rule 41.
   ADR-20261020-tca rule 20 gives that code to a printer that is gone. Such
   a printer reads `BLOCKED` by rule 1, and a state rule would refuse it as
   if it were still there.
5. The state check comes after the parameters are checked, because
   `fractal.md` checks parameters before a deliberate refusal. The host
   checks the range of an argument only when the bridge sends the command.
   So `0x4013` and `0x4014` from the host come after the state check.
6. `LoadFilament` refuses `NOTHING` with `0x4013`. `Extrude` refuses with
   `0x2014` when `targetNozzle` is null and `material` is null, `NOTHING` or
   `OTHER`, because no temperature is known for those.
   An `Extrude` that names no feedrate gets 27 mm/min, because
   `domain-3d-print.md` asks the node to choose a safe rate. The firmware of
   a Prusa printer purges a flexible filament at 27 to 30 mm/min, its
   slowest purge rate, so every filament takes it.
   A feedrate the call names is rounded to a whole number, and so is a
   heater target a desired-properties message writes, `targetBed` or
   `targetNozzle`. The vocabulary gives both as numbers that may have a
   fraction, and the core takes both as whole numbers. The property then
   reads the rounded value.
   `Extrude` does not heat the nozzle, where `domain-3d-print.md` says it
   MUST. `core.command.move_e` only moves the filament, and the core has no
   command that heats and moves in one step. The firmware refuses to move
   the filament below 170 °C, so an `Extrude` on a cold nozzle moves
   nothing, and the call still succeeds.
   `LoadFilament` with `OTHER` sends the core's load like any other
   material, and the load may heat the nozzle, where `domain-3d-print.md`
   says it MUST NOT. The core has no load that leaves the nozzle cold, and
   the printer then asks a person on its screen for the filament.
   After `DisableSteppers`, `axisX`, `axisY` and `axisZ` read the position
   the printer reports, where `domain-3d-print.md` says they SHOULD read
   null until the axes are homed. No reading says that the axes are not
   homed.
   During `LevelBed`, `LoadFilament`, `UnloadFilament` and `Extrude`,
   `state` reads `BUSY` only while the printer reports `BUSY`, where
   `domain-3d-print.md` says `state` MUST read `BUSY` for the whole
   sequence. After `LevelBed`, `state` reads `READY` only when the printer
   reports `READY`. No reading says that a sequence runs, so the bridge
   follows the printer's word.
7. `SetReady`, `Reset`, `Beep` and `UpgradeFromFile` have no state rule. The
   printer may still refuse them, and the answer is then `0x2012`.
   A printer refuses `core.command.set_printer_ready` in `ERROR`, so
   `SetReady` there gets `0x2012`, where `domain-3d-print.md` names `ERROR`
   as a state it is meant for. The call to use in `ERROR` is `Reset`.

### job values

8. `PrintJob1` reads null for every property while `Printer1.state` is neither
   `PRINTING` nor `PAUSED`. So does `FDMPrintJob1`. A printer keeps the last
   job's values after the print ends, and the vocabulary says null when no
   job is active. An interface row lists such states under `activeIn`.

### the multi-material unit

9. The multi-material unit of a printer is the feeder of the first head in
   the tool tree that has one. `FDMPrinter1.activeSlot` is the slot of that
   feeder loaded into the head, counted from 1, `0` with no slot loaded, and
   null when no head has a feeder.

### identity

10. `Device1.uuid` is the identifier this server gave the device, a UUID,
    as its 16 bytes in unpadded base64url. An identifier that is not a UUID
    in its canonical text form reads null.
    A printer has one `UpgradableDevice1`, for its main board, where
    `domain-common.md` says each component that can be upgraded on its own
    SHOULD have its own instance. The core reads no firmware version of a
    multi-material unit or a tool head, and has no command that flashes one.

### print heads

11. A printer has one `FDMPrinterHead1` per print head: head 0, which every
    printer has, and every other tool of the tool tree that is enabled and
    of type `FFF_PRINT_HEAD`. Head `n`, counted from 0, has the instance id
    `<device>_<n+1>`. An interface row with `"per": "head"` reads its paths
    under `/tools/{tool}/`.
12. A head that appears later gets its instance then. A head that is
    disabled later keeps its instance and reads null, because Fractal has no
    message that removes an instance.
13. A write of `targetNozzle` names its head in the `tool` argument of
    `core.command.set_nozzle_temperature`, on a printer whose capabilities
    declare that argument. The host refuses an argument a printer does not
    declare, so a printer that declares none gets the write with no `tool`
    argument. The command then acts on the head the printer has picked. So
    on a printer with more than one head and no `tool` argument, the bridge
    rejects a write for any other head, and the write never heats the wrong
    head. On a printer that declares the argument, it rejects a write for a
    head that is not head 0 or an enabled print head. A rejected write sends
    no command. The Fractal node logs it and answers nothing, because a
    desired-properties message has no answer (`fractal.md`).
14. `core.command.load_filament`, `unload_filament` and `move_e` take no
    head and act on the head the printer has picked. So `LoadFilament`,
    `UnloadFilament` and `Extrude` work on that head, which is head 0 on a
    printer with one head. Any other head refuses them with `0x2015`, a
    refinement of `ERR_DENIED`. So does a disabled head, and every head of a
    printer with more than one head that has picked none.

### filament slots

15. A printer with a multi-material unit has one `FilamentSlot1` per slot of
    it. Slot `k` of the feeder, counted from 0, has the instance id
    `<device>_<k+1>`, and an interface row with `"per": "slot"` reads its
    paths under `/tools/{tool}/`. A unit fitted later gets its instances
    then.

### a change of several properties

16. A desired-properties message, DES in `fractal.md`, may ask for several
    properties. The bridge sends one command per property, in the order of
    the property names. The first command the host or the printer refuses
    rejects the DES, and the commands sent before it stay applied.
    `fractal-requirements.md` says a rejected DES changes nothing. The core
    has no command that sets several properties at once, and an undo would
    be one more command, which can fail as well.

### a call that asks for nothing

17. `Home` with `x`, `y` and `z` all false homes no axis.
    `domain-3d-print.md` says to home the axes set to true, and the three
    parameters are valid, so the call is not refused for its parameters.
    Once the state rules allow the call, the answer is success and the
    printer receives nothing, because `core.command.home` with no axis homes
    every axis.

## enforcement

| rule | gate |
|---|---|
| 1, 2 | `TestAPrinterThatIsAwayIsBlocked` in `packages/fractal/bridge` |
| 3 | `TestADialogOverAPrintIsAPause` in `packages/fractal/bridge` |
| 4, 6, 7 | `TestAMethodFollowsTheStateRulesOfTheVocabulary` in `packages/fractal/bridge` |
| 4 | `TestACallForADeviceThatWentAwayIsNotFound` in `packages/fractal/bridge` |
| 5 | `TestPrintFileRefuses` in `packages/fractal/bridge` |
| 6 | `TestAMethodBecomesOneCommand` in `packages/fractal/bridge` |
| 6 | `TestARequestedChangeBecomesOneCommand` in `packages/fractal/bridge` |
| 8 | `TestAJobReadsNullWhileNoPrintRuns` in `packages/fractal/bridge` |
| 9 | `TestTheActiveSlotComesFromTheToolTree` in `packages/fractal/bridge` |
| 10 | `TestADeviceUUIDIsSixteenBytesInBase64URL` in `packages/fractal/bridge` |
| 10 | `TestADeviceBecomesInterfaceInstances` in `packages/fractal/bridge` |
| 11, 12 | `TestEveryHeadIsAnInstance` in `packages/fractal/bridge` |
| 13 | `TestAHeadIsAddressedByItsTool` in `packages/fractal/bridge` |
| 13 | `TestASingleHeadNamesNoTool` in `packages/fractal/bridge` |
| 13 | `TestAnUndeclaredToolHeatsOnlyThePickedHead` in `packages/fractal/bridge` |
| 13 | `TestATargetForADisabledHeadIsRefused` in `packages/fractal/bridge` |
| 14 | `TestAFilamentMethodGoesToThePickedHead` in `packages/fractal/bridge` |
| 14 | `TestAFilamentMethodNeedsAPickedHead` in `packages/fractal/bridge` |
| 15 | `TestEverySlotIsAnInstance` in `packages/fractal/bridge` |
| 16 | `TestAChangeOfSeveralPropertiesIsSentInNameOrder` in `packages/fractal/bridge` |
| 17 | `TestHomingNoAxisSendsNothing` in `packages/fractal/bridge` |

## rejected

| option | why not |
|---|---|
| keep the last state of a printer that is away | a server above reads `READY` and sends a print to a printer that cannot take it |
| `ATTENTION` as `BLOCKED` in every case | `CancelJob` must refuse a `BLOCKED` printer, so a server above could never cancel a print with a dialog on the screen |
| leave the state rules to the firmware | the firmware accepts most verbs in every state but `ERROR`, so `Home` would move the axes of a printing machine |
| pick the head with `T<n>` before a filament command | two commands, not one, and the second can fail after the first has moved the tool changer |
| check every value of a DES against the printer's capabilities before the first command | the gap gets smaller and stays open, because the printer can still refuse the second command |
| refuse a `Home` with no axis with `ERR_INVALID_PARAMS` | three booleans are valid parameters, and the vocabulary asks to home the axes that are true, which here are none |
| `ERROR` for a printer that is away | `ERROR` is a fault that cannot be recovered, and a printer that returns works again |
