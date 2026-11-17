---
title: "ADR-20261025-z7t: a dialect declares its states, and each argument says its range"
description: "A driver declares its state set, with the verbs each state accepts, on its dialect, and the host places it for every mounted connector. Each argument carries a unit, and one device's arguments are served with their type and range."
date: 2026-10-25
status: accepted
tags:
  - adr
  - devices
  - capabilities
  - sdk
---
# ADR-20261025-z7t: a dialect declares its states, and each argument says its range

- status: accepted
- date: 2026-10-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md) rules 3 to 9 and [ADR-20260825-dee](20260825-dee-the-capability-vocabulary.md) | Reads with [ADR-20261016-qrp](20261016-qrp-a-driver-declares-the-models-it-reaches.md)

A client that drives printers over the API needs two answers it could not get
from the server. The first is which states a printer reports and which verbs it
accepts in each. `GET /api/v1/vocabularies/device-states` answered an empty
list on every server, because nothing placed a declaration. The second is what
to send a verb. A device record lists its capability keys, and the type and the
range of each argument stayed in the driver's source. This record says where
both come from and how they are served.

## rules

### the state set

1. A driver declares its state set on its `Dialect`, in `States`.
   `connectorapi.Register` refuses a set `StateSet.Problems` refuses, so the
   author learns of a bad one at build time. The set is data the driver's
   package holds, which keeps ADR-20260831-c7b rule 3.
2. The host places the set of every mounted connector whose dialect declares
   one, at start, and serves it in the device-state vocabulary. This fills the
   install path ADR-20260831-c7b left open.
3. There is one entry per device kind per connector type. Two dialects may each
   declare the printer kind, because a state name is read inside the dialect
   that declared it (ADR-20260831-c7b rule 4). The entry names the connector type and
   every mounted instance of it, and a client finds a device's entry by the
   instance the device names under `driver`.
4. A state lists the verbs a device accepts in it, in `Accepts`. This is the
   guard of ADR-20260831-c7b rule 7, written as data the host reads (rule 8). A set
   lists them for every state or for none. A state that accepts nothing has an
   empty list, and a set that lists them for one state and leaves another nil
   is refused.
5. A transition caused by a verb is refused when the state it leaves does not
   accept that verb. The two lists would otherwise tell a client two things
   about one verb.
6. The guard is a hint, as ADR-20260831-c7b rule 25 says of every published
   precondition. The device's answer to a command decides. The host does not
   refuse a verb a state does not accept, and that refusal is
   still owed.
7. A guard repeats the rule of the machine and invents none. The xBuddy rows
   come from the public firmware source, and each row names the function it
   reads.

### the argument

8. A numeric argument may declare a `Unit` from a closed set: `celsius`,
   `percent`, `mm`, `mm/min`, `s` and `byte`. A unit on a value that is not a
   number, or a unit outside the set, is refused by `Set.Problems`.
9. A merge keeps the unit of the tier that declared one. Two tiers that declare
   different units leave nothing, because two units under one key are two
   meanings under one name. A profile file declares no unit and
   leaves the driver's standing.
10. The unit is part of `Set.Digest`. It permits nothing, but a set that changed
    what its numbers mean is a different set to a client that cached it.
11. `GET /api/v1/devices/{device_id}/capabilities` serves one device's resolved
    set, with each argument's name, type, whether it is required, its default,
    its range, its unit and its closed set of values. It takes the action
    reading the device takes.
12. The range is written as a minimum and a maximum. The descriptor's limit
    bounds a magnitude, so a limit with no floor is served as a range on both
    sides of zero. A floor below the negative of the limit is served as that
    negative. A client reads one range and never the magnitude rule.
13. The device record keeps its list of capability keys. The detail is a
    second request, because a fleet page of four hundred printers would carry
    every argument of every verb on every row.

## enforcement

| rule | gate |
|---|---|
| 1 | `TestADialectWithARefusedStateSetIsRefused` in `sdk/connectorapi` |
| 2, 3 | `TestEveryMountedDialectsStatesArePlaced` and `TestADialledPrinterServesItsStatesAndItsArguments` in `internal/app` |
| 3 | `TestTwoConnectorTypesMayEachDeclareOneKind` in `internal/kernel/devicestate` |
| 4, 5 | `TestTheGuardIsRefusedWhereItCannotBeRead` in `sdk/connectorapi` |
| 4 | `TestAStateThatAcceptsNothingPublishesAnEmptyList` in `internal/transport/http/httpapi` |
| 7 | `TestTheGuardsAreTheFirmwares` and `TestErrorAcceptsOnlyTheFirmwaresFourCommands` in `internal/connector/xbuddy` |
| 8 | `TestProblemsRefusesAUnitNothingCanRead` |
| 9 | `TestAMergeKeepsTheUnitOneTierDeclared` and `TestAMergeOfTwoUnitsKeepsNeither` |
| 10 | `TestDigestSeparatesEveryFieldOfAValue` |
| 11, 12 | `TestTheProjectionWritesEveryArgumentAsARange` and `TestEveryServedRangeIsOneTheHostAccepts` |

## rejected

| option | why not |
|---|---|
| one state set per device kind, as the placement first allowed | two dialects drive printers and report different words. One of them would have to be refused |
| a guard per verb that lists its states, with every unlisted verb accepted everywhere | the firmware refuses all but four commands in `ERROR`, and a guard per verb cannot say that without a row for every verb |
| the guard as an expression the host evaluates | ADR-20260831-c7b leaves the language open, and a list is the smallest form that answers which controls to offer |
| the arguments on every device record | the listing would grow by every argument of every verb of every row |
| units as free text | a client converts and labels by the unit, and a word it has no meaning for leaves it with a bare number |
| the unit read from the argument's name | `celsius` and `nozzle_temperature` both count in degrees Celsius, and a name is no contract about a unit |
