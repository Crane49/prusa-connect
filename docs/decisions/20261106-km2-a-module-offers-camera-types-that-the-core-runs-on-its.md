---
title: "ADR-20261106-km2: a module offers camera types that the core runs on its generic camera"
description: "A module's manifest offers camera types. A camera of one is declared as <namespace>/<name>, runs on the core's generic driver with no module code inside the core, is fed by that module, and has its live view drawn by the module in the camera-view slot."
date: 2026-11-06
status: proposed
supersedes: []
amends: [ADR-20260827-wmv, ADR-20261106-2er]
tags:
  - adr
  - extensions
  - cameras
---
# ADR-20261106-km2: a module offers camera types that the core runs on its generic camera

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md) rule 5 and [ADR-20261106-2er](20261106-2er-a-module-hands-a-frame-to-a-push-camera.md) rules 9 and 10 | Related: [ADR-20261106-ypn](20261106-ypn-a-camera-is-one-generic-type-with-a-still-source-and-an.md), [ADR-20261106-xm4](20261106-xm4-a-printer-page-has-a-cameras-tab-and-a-module-draws-under.md)

A module that serves a kind of camera, such as one that stands in for a cloud
camera's server, set up its cameras as plain push cameras. A person could not
tell such a camera from any other, and had to know that the module needed one
before they added it. The module could not recognise its own cameras either,
so it drew under every push camera and asked the person to pick.

The camera should be a type of its own, which a person picks when adding a
camera. A module may not run inside the core (ADR-20260827-wmv rule 5), and no
camera contract crosses the wire. But a camera of such a type needs no driver
of the module's: the module already hands it its pictures with
`camera.accept`, and the core's generic camera holds them.

## rules

### the offer

1. A module offers camera types under `camera_types` in its manifest, each a
   `name` and a `title`, at most eight. The install records them, and a module
   that offers one runs a process and names `operate` in `permissions`.
2. A camera of an offered type is declared as `<namespace>/<name>`, with the
   module's namespace. Two modules cannot offer the same type.
3. A module offers its types while it is enabled. A camera of a switched-off
   or removed module's type stops, and its start names the type it lacks.

### what the core runs

4. A camera of an offered type runs on `core/generic` (ADR-20261106-ypn), with
   its settings. No module code runs inside the core. This narrows
   ADR-20260827-wmv rule 5 for this one case: the type is the module's, the
   code that runs is the core's.
5. The resource API, the overlay and the host all judge such a camera as
   `generic`. The operator's file declares no camera of a module's type.
   Amended by ADR-20261116-g5x: the camera API takes the resource API's
   place, and no file declares a camera at all.
6. The camera listing names each camera's type, and
   `GET /api/v1/camera-types` lists the types a new camera may be declared as:
   `generic` and every offered type, with its title and module.

### who feeds it and who draws it

7. A frame the module sends as itself reaches a camera of the module's type
   with no grant. The host reads the camera's declared type at each call, so a
   camera whose type changes leaves the module at once. This amends
   ADR-20261106-2er rules 9 and 10 for these cameras.
8. `camera-view` is a slot on the printer-detail screen, in the area
   `camera-picture`, of the section form, handed the camera's name. It is drawn
   over a camera's picture while a person plays it, and only for the module
   that offers the camera's type. The Cameras tab and the Now tab offer play
   for such a camera when that module registered a view.
9. The module's camera sections still follow ADR-20261106-xm4. A module knows
   its cameras by their type, and needs no key of its own on them.

## enforcement

| rule | gate |
|---|---|
| 1 | the tests in `sdk/manifest/cameratypes_test.go` |
| 2, 4, 5 | `TestACameraOfAnOfferedTypeIsDeclaredAsGeneric` in `internal/app/resource` |
| 4, 6, 7 | `TestACameraOfAModulesTypeIsFedByThatModule` in `internal/app`, on the real host and graph |
| 6 | `TestTheCameraTypesAreListed` in the HTTP API |
| 8 | the `PrinterCamerasPanel` and `SlotOutlet` tests, and the every-slot test |

## rejected

| option | why not |
|---|---|
| a camera driver in the module, called for each frame | no camera contract crosses the wire, and every viewer would wait on the module |
| a push camera with a key naming the module | the camera takes no key a module adds, and a person still picks a driver |
| a grant the core writes on each sync | nothing removes it when the camera's type changes, because the graph cannot tell it from an admin's |
| a grant the person must write | a camera of the module's type is useless without it, so asking for it is a step that can only be forgotten |
| any module drawing a camera's live view | two modules would draw one camera's view, and the camera serves its stream to one viewer |
