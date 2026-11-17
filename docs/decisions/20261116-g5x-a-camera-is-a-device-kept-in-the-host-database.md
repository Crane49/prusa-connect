---
title: "ADR-20261116-g5x: a camera is a device kept in the host database"
description: "A camera is a device row and a camera row in the host database, written together through its own API and run by the supervision plane. No configuration file declares one, and the first start moves an older build's cameras out of the files once."
date: 2026-11-16
status: proposed
supersedes: []
amends: [ADR-20260826-5yj, ADR-20261106-km2]
tags:
  - adr
  - cameras
---
# ADR-20261116-g5x: a camera is a device kept in the host database

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260826-5yj](20260826-5yj-a-device-is-the-object-and-a-printer-is-the-type.md) rule 18 and [ADR-20261106-km2](20261106-km2-a-module-offers-camera-types-that-the-core-runs-on-its.md) rule 5 | Related: [ADR-20261106-ypn](20261106-ypn-a-camera-is-one-generic-type-with-a-still-source-and-an.md)

A person adds a camera on a printer's page, and the camera is a device with a
row and grants in the host database, like the printer. Its declaration was a
resource instance in a file: the operator's `config.yaml` or the overlay the
resource API writes. So a camera was two things kept in step by a sync after
every write: an instance in a file and a device in the graph. A hand edit could
declare or retype a camera behind the graph's back. A copy of the database left
the cameras behind. The sync had to guess when a camera was first attached, to
copy its printer's grants once.

## rules

1. A camera is a device row in `authz_device`, kind `camera`, and a row in the
   `camera` table: its type, its settings and a generation. The camera row
   references the device row, and goes with it.
2. Add, change and remove a camera through `PUT` and `DELETE
   /api/v1/cameras/{camera}`, and read the settings with `GET
   /api/v1/camera-settings`. A write writes the device row, its parent edge and
   the camera row in one transaction, then starts, restarts or stops the camera.
3. Fence a write on the camera's own version, its generation: `If-None-Match: *`
   adds and `If-Match` changes or removes. A write with neither is refused.
4. Copy the printer's grants onto the camera when it is put on that printer
   (ADR-20260826-5yj rule 17), at the write that puts it there.
5. `camera` is a device kind (`ref.ClassDevice`): the supervision plane runs it
   and no configuration file declares it. The resource API and the loader do
   not serve it. A camera runs a core type, or a module's offered type on the
   generic camera (ADR-20261106-km2).
6. A camera's settings are still granted on `resource_kind:camera`. This amends
   ADR-20260826-5yj rule 18: a camera's lifecycle events and stored stills are
   on its own origin, and reach whoever observes the camera device.
7. Remove a retired printer's cameras with it.
8. On the first start of this build, move an older build's cameras into the
   table. An overlay camera is moved and taken out of the overlay. A camera of
   the operator's file is moved while the device row that older build wrote
   for it carries no camera. After that the block is logged and not read, so a
   camera removed after the move stays removed. A camera device no camera
   carries is removed.
9. Amends ADR-20261106-km2 rule 5: the camera API and the host judge a camera
   of a module's type as `generic`.

## enforcement

| rule | gate |
|---|---|
| 1, 2, 3 | `TestACameraIsADeviceRowAndACameraRow` in `internal/app` |
| 1 | `TestADeviceAndWhatItRunsAreOneWrite` in `internal/authz`, on both engines |
| 1 | `TestACameraRowGoesWithItsDevice` in `internal/platform/devicestore`, on both engines |
| 5 | `TestEveryKindCarriesOneClass` in `internal/kernel/ref` |
| 5 | `TestACameraBlockDeclaresNothing` in `internal/app/config/install` |
| 6 | `TestACameraIsWrittenOnlyByWhoConfiguresCameras` in `internal/app` |
| 6 | `TestACameraIsAnnouncedOnTheSameEventPlaneAsEverythingElse` in `internal/app` |
| 7 | `TestARemovedPrinterTakesItsCameras` in `internal/app` |
| 8 | `TestAnOlderBuildsCamerasMoveIntoTheDatabaseOnce` in `internal/app` |
| 8 | `TestTheFilesCamerasAreReadForTheMove` in `internal/app/resource` |
| 9 | `TestACameraIsJudgedByItsType` in `internal/app` |

## rejected

| option | why not |
|---|---|
| keep a camera a resource instance and store the overlay entry in a table | two identities kept in step by a sync, and a version token over files and rows |
| a store of its own beside the supervision plane | the plane already starts, restarts, reports and lists a camera |
| keep reading a `camera:` block of the file after the move | a camera removed in the app would come back on the next start |
| refuse to start while the file declares a camera | an upgrade would be an outage for a block the server can move itself |
| a marker row that records the move | the device row an older build wrote already says which cameras it declared |
| a camera's own `enabled` switch | the app never switches a camera off; removing it is the switch |
