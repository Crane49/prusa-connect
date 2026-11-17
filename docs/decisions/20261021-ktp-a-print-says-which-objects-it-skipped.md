---
title: "ADR-20261021-ktp: a print says which objects it skipped"
description: "The objects of a print that a machine can cancel one by one: the state path a driver reports them at, the bounds the host holds them to, the column that keeps them after the print, and the fields a module and the HTTP API read."
date: 2026-10-21
status: accepted
tags:
  - adr
  - connectors
  - sdk
  - modules
---
# ADR-20261021-ktp: a print says which objects it skipped

- status: accepted
- date: 2026-10-21

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Related: [ADR-20260829-jdq](20260829-jdq-the-host-bounds-the-text-a-driver-hands-it.md), [ADR-20261016-pmj](20261016-pmj-a-table-schema-lives-beside-its-code.md)

A slicer can label each object on the bed. A machine that reads the labels can
then skip one object and finish the others, and this server already sends the
two verbs that do it, `core.command.cancel_object` and
`core.command.uncancel_object`. Until now it did not read what the machine
answers, so nobody could see which objects a print skipped. A module that
counts good parts needs that answer after the print has ended.

Buddy firmware reports the set in one event, `CANCELABLE_CHANGED`. The event
carries the job number and an array of entries, each with an `id` and a
`canceled` flag, and no name (Prusa-Firmware-Buddy, `src/connect/render.cpp`).
The firmware sends it when the set changes, when a print starts, and as the
answer to either verb. It holds at most 1024 objects
(`src/feature/cancel_object/cancel_object.hpp`) and clears the set only when
the next print starts.

## rules

### what a driver reports

1. A driver reports the objects at `/job/objects`,
   `connectorapi.JobObjectsPath`. The value is a JSON array of
   `connectorapi.JobObject`: `id`, an optional `name`, and `cancelled`. The
   path is optional. An empty array is a print with no labelled objects, and
   an absent path is a machine that said nothing about them.
2. A driver reports the whole array each time the set changes, so a reader
   never merges one change into the last. It reports null at the path when the
   machine starts a different print.
3. The xBuddy driver reads the job number on the event before the array, and
   reports both. A set that arrives while no print is named is dropped. A
   machine that connects after a print ended still announces that print's set,
   and the tree has no print to hang it on.
4. The xBuddy driver keeps at most the 1024 ids the firmware can hold. It
   clears the array when the machine names a print other than the one the
   array belongs to. Job number 0, which the firmware reports between prints,
   does not clear it.

### what the host keeps

5. The host reads the path, keeps the later of two entries for one id, and
   orders the set by id. It keeps at most `deviceapi.MaxJobObjects`, 256
   objects. A set over the bound keeps every cancelled object before the
   others. The count is the number of distinct objects the machine reported.
6. The host cuts a name to `deviceapi.MaxJobObjectNameBytes`, 64 bytes, on a
   character boundary, with the marker of ADR-20260829-jdq rule 3 inside the bound.
7. The host stores the bounded set on the print's row, in
   `printer_job.objects`, as one JSON document with `count` and `objects`.
   Migration `devicestore/0002_job_objects.sql` adds the column on both
   engines. The column is written with the rest of the reading and is frozen
   with it when the print ends.

### what a reader sees

8. `DeviceJob` on `GET /api/v1/devices/{device_id}/jobs`, `GET
   /api/v1/jobs/{job_id}` and the job history, and `DeviceJobSummary` on the
   device listing, carry `object_count` and `objects`. Each object has `id`,
   `name` when the machine gave one, and `cancelled`. Both fields are absent
   for a machine that said nothing about objects.
9. `deviceapi.Job` carries the same two fields as `ObjectCount` and
   `Objects`, on `device.jobs` and `device.job_history`. They are optional, so
   a module built before them reads the same answer. A client refuses a job
   with more than 256 objects or a name over 64 bytes.
10. A finished print keeps the last set the host stored before the flush that
    stamped its end. A change the machine reports after that flush is not
    stored. Buddy firmware does not change the set after a print ends, so in
    practice the stored set is the final one.
11. `device.get` carries the array the driver reported at `/job/objects` in
    the state tree. The event stream carries each new array as a
    `com.prusa.connect.device.telemetry.sampled` event at that path, as it
    carries every other path.

## enforcement

| rule | gate | state |
|---|---|---|
| 1, 2, 3, 4 | `internal/connector/xbuddy/cancelobjects_test.go`: the firmware's frame, a new print, the 1024 limit | exists |
| 5, 6 | `internal/app/jobobjects_test.go`: the bound keeps the cancelled objects, a long name is cut on a character | exists |
| 7, 10 | `internal/platform/devicestore` job tests: a fresh handle, the history page, a write over a finished print | exists |
| 8, 9 | the round trip in `internal/app/jobobjects_test.go`, and `sdk/deviceapi/jobobjects_test.go` | exists |
| 9 | `make api-check` against the committed OpenAPI document | exists |

## rejected

| option | why not |
|---|---|
| a table with one row per object | the set is read and frozen with the print's row. A second table adds a join to every jobs read |
| keep all 1024 objects on the row | a page of 200 jobs could then carry 200,000 objects. Nearly every print has far fewer than 256 |
| keep the first 256 ids when a print has more | an object cancelled above id 255 would vanish, and which objects were cancelled is the question a reader asks |
| one patch per object, such as `/job/objects/7` | a machine reports the whole set, and a tree of a thousand paths spends the device budget of ADR-20260829-jdq rule 4 on one print |
| hold the array in a reserved slot of the xBuddy patch queue | a reserved slot overtakes the queue, so a new set could reach the host before its job number |

## open items

- The objects a finished print skipped are not in the job ended event of
  `sdk/event`. A module reads them from `device.jobs` or
  `device.job_history`.
- Buddy firmware shows object names on its own screen and does not send them.
  A firmware that sends them fills `name` with no change here.
