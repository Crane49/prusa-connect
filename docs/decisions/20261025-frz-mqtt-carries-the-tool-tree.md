---
title: "ADR-20261025-frz: MQTT carries the tool tree"
description: "The MQTT sink publishes each head and each feeder slot on data/tools/<id>/ from the tool tree, and a sink takes a device's live state through the optional eventsapi.SampleObserver."
date: 2026-10-25
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - sdk
  - sinks
---
# ADR-20261025-frz: MQTT carries the tool tree

- status: accepted
- date: 2026-10-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20261025-pze](20261025-pze-the-tool-tree-is-the-only-place-a-tool-lives.md) and [ADR-20261023-gna](20261023-gna-a-printers-tools-are-a-tree-of-heads-and-feeder-slots.md)

The MQTT topic table carried one material and one nozzle for the whole
printer, and three `data/slot/...` topics. None of them had a producer: a
sink was told durable facts and the printer state, and no telemetry. ADR-20261025-pze
moved every tool into the tree, and the slot topics counted from a base the
tree does not use. A subscriber was shown topics that never
carried a value.

## rules

1. The MQTT sink publishes four topics per tool, under
   `v1/devices/printers/{printerId}/data/tools/{toolId}/`. `{toolId}` is the
   tool tree's id: `0` is the first head and `0.2` is slot 2 of the feeder on
   head 0.

   | topic | from the tree | QoS | retained |
   |---|---|---|---|
   | `active` | `active`, as `1` or `0` | 1 | yes |
   | `material` | `material_package_instance/package/material/abbreviation` | 1 | yes |
   | `temp/nozzle/current` | `nozzle_temp`, whole degrees | 0 | no |
   | `temp/nozzle/target` | `nozzle_target_temp`, whole degrees | 1 | no |

2. A feeder slot has `active` and `material` only. It prints through the
   nozzle of its head.
3. A topic carries a value the tree holds, or an empty payload. The empty
   payload is zero bytes, and the sink never publishes the text `null`. The
   cases are:

   | the tree says | payload |
   |---|---|
   | a heater that is off, `nozzle_target_temp` 0 | `0` |
   | nothing loaded, `NoMaterial` | empty |
   | a node the device removed, a null | empty |
   | a tool with `enabled` false, or with `enabled` removed | empty on every topic of the tool |
   | a tool that a whole tree no longer names | empty on every topic of the tool that held a value |

   An empty payload on a retained topic is also retained, and per MQTT that
   deletes the retained message. A subscriber that joins later reads nothing,
   and a subscriber reads an empty payload as a value that is not known.

   The sink does not read the broker. A retained topic that an earlier run of
   the server left behind is cleared when the tree reports that tool with
   `enabled` false, and stays otherwise.
4. The printer-wide `data/material` and `data/temp/nozzle/...` topics and the
   three `data/slot/...` topics are removed. The project has no released
   contract to keep, so there is no alias.
5. `sdk/eventsapi` has an optional interface, `SampleObserver`. The host
   offers a device's live state only to a processor that implements it, as
   one `Samples` per device per delivery window, after the hub has conflated
   the window. A processor that does not implement it is offered nothing, and
   a supervised sink checks before it queues, so a sample never takes the
   queue slot of a durable fact.
6. The host hands a sink that takes samples the whole tree of a device, with
   `Whole` set. It does so when the sink can first read the device, and after
   a window that lost continuity. The hub attaches a reader at the head of each
   channel, and a node that does not change would otherwise never reach it.

## enforcement

| gate | what it holds |
|---|---|
| `adapters/events/mqtt/tools_test.go` | each node lands on its topic, an off heater is 0, and each unknown case of rule 3 is empty |
| `adapters/events/mqtt/topics_test.go` and `testdata/shipping-asyncapi.yaml` | the topic table equals the published document, which the MQTT reference page is generated from |
| `internal/extension/supervised/sink_test.go` | a sink that does not take samples loses no queue slot to them |
| `internal/app/sinks_test.go` | a window becomes one `Samples` per device, and the whole tree is built in path order |
| `internal/app/mqtttools_test.go`, run by `make test-adapters` | a simulated printer reaches a real broker per head and slot, and rule 3 holds on the broker |

## rejected

| option | why not |
|---|---|
| keep the 1-based `data/slot/{slotId}` topics beside the tool topics | two names for one value, and the project has no released contract to keep |
| keep the printer-wide material and nozzle topics for the tool printing now | ADR-20261025-pze removed the same values from the state tree |
| hand every telemetry frame to every sink | a webhook or a queue would carry a second telemetry feed, and samples would push durable facts out of its queue |
| one call per sample | a whole tree is a hundred nodes, and a fleet attaching at once would overrun the sink's queue |
| `data/tool/{id}` in the singular | the tree names the node `tools`, and the singular is the path ADR-20261025-pze removed |
| leave the last value on the broker when a node is removed | a subscriber reads a slot of an MMU that is off as loaded, with no way to tell |
| publish `null` for a value that is not known | a subscriber reads the text `null` as a value, and a retained `null` never goes away |
| publish an empty target for a heater that is off | a target of 0 °C is what the firmware's `M104 S0` means. It is known, so it has a value |
