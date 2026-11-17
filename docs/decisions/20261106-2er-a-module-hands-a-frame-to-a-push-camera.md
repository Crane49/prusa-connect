---
title: "ADR-20261106-2er: a module hands a frame to a push camera"
description: "camera.accept: a module sends one JPEG to a push camera on a stream of its own, and the call takes operate on the camera like the HTTP push route."
date: 2026-11-06
status: proposed
supersedes: []
amends: []
tags:
  - adr
  - modules
  - cameras
---
# ADR-20261106-2er: a module hands a frame to a push camera

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Related: [ADR-20261026-c7m](20261026-c7m-a-module-writes-a-file-into-a-storage.md), [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20260910-cqq](20260910-cqq-graphql-and-the-old-schemas-are-modules.md)

Some cameras speak a protocol of their own to a server. A cloud camera, for
example, uploads its picture over HTTPS and takes commands over Socket.IO. A
module can serve that protocol on a port of its own (ADR-20261019-3ns), and
then it holds a JPEG with no way to give it to the core. A push camera keeps
the last frame it was sent, but the only door into one was the HTTP route. A
module holds no credential for that route (ADR-20260910-cqq rule 6), and the
host is a module's only door (ADR-20260827-wmv rule 13).

`camera.accept` is that door. The frame lands in a camera that keeps the
pictures it is sent: a `push` camera, or a `generic` one with no snapshot
address (ADR-20261106-ypn). The snapshot, the history and the camera listing
then serve it like a frame a device sent.

## rules

### the call

1. The method is `camera.accept`. A module makes it on a stream it opened and
   writes the frame's bytes on that stream as data after the call, with the
   ready byte of ADR-20261026-c7m rule 3.
2. The params are `principal`, `camera` and `size`. `size` is the number of
   bytes that follow, from 1 to 8 MiB (`cameraapi.MaxAcceptBytes`). That is
   the bound the HTTP push route holds a device to.
3. A stream that carries fewer bytes than `size`, or more, is
   `ErrShortFrame`, code 8003, and the camera keeps the frame it had. A call
   made with no stream is the same refusal.
4. Bytes that do not start the way a JPEG does are `ErrInvalidSnapshot`, code
   8002, as they are on the HTTP route.

### what the host does

5. The host hands the frame to the camera plane's `Push`, the function the
   HTTP route calls. The frame is stamped with the time it arrived, never a
   time the module names.
6. `ErrNoCamera`, code 8000, is the one refusal for three cameras. They are
   one that is not declared, one the caller cannot see, and one that fetches
   its pictures. Its message names no camera.

### who may send

7. The call takes `operate` on the camera's device row, the permission the
   HTTP push route takes.
8. A call naming a person runs as that person while the host is serving their
   request, as `storage.write` does.
9. A call naming nobody is the module's own work. It is held to the
   instance's `permissions:` list first, so an instance without `operate` in
   that list sends nothing. It is then held to the graph for the instance's
   own principal, `sink:<instance>`.
10. Approving the install writes no grant for `operate`, because a sink holds
    no org role that reaches it. The person who sets up a camera writes the
    grant on that camera, `device:<camera>#operator@sink:<instance>`, through
    the grants API. A module never reaches a camera nobody gave it.
11. A refusal for the graph is `ErrDenied`, code 8001.
12. The host decides rules 6 to 11 before it writes the ready byte, and asks
    again once the frame is read. A refused call costs it no frame's memory.

## enforcement

| rule | gate |
|---|---|
| 1, 2, 3 | the tests in `sdk/cameraapi/wire_test.go`, over a real session, with `TestManySmallFramesAllEnd` for the ready byte |
| 4, 5, 7, 9 | `TestAModuleHandsAFrameOnlyToACameraItWasGranted` in `internal/app`, in both directions, on the real camera plane and graph |
| 6, 10 | `TestAModuleWithNoGrantReachesNoCamera` in `internal/app` |
| 8 | `TestAFrameInAPersonsNameNeedsTheirOpenRequest` in `internal/app/resourcehost` |
| 12 | `TestAnUnadmittedFrameIsNeverRead` in `sdk/cameraapi` |

## rejected

| option | why not |
|---|---|
| the bytes inside the call, as base64 | bulk bytes go in data frames (ADR-20260827-7jn rule 8), and 1 MiB is the most a control frame holds |
| a credential in a `secret` option, for the HTTP route | a module would hold a token (ADR-20260910-cqq rule 6), and it outlives its camera |
| a driver in the module, which the core calls for each frame | no camera contract crosses the wire, and every viewer would wait on the module |
| a config key on the camera naming the module that may feed it | a second authorization rule beside the graph, which already grants a camera on its own row |

## open items

- a module cannot create the camera it feeds. The person's session does that
  through the resource API, and writes the grant of rule 10 at the same time
