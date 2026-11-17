---
title: "ADR-20261106-ypn: a camera is one generic type with a still source and an optional stream"
description: "core/generic takes still images that are sent to it or fetched, and streams from an RTSP or MJPEG address when one is set. It is the type the app declares a new camera as, built from the push, mjpeg and rtsp drivers."
date: 2026-11-06
status: proposed
supersedes: []
amends: []
tags:
  - adr
  - cameras
---
# ADR-20261106-ypn: a camera is one generic type with a still source and an optional stream

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Related: [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md)

A camera was declared as one driver. `push` holds the pictures a camera sends,
`mjpeg` reads the addresses a camera answers, and `rtsp` reads an H.264 stream.
A real camera is often two of them. A Buddy3D sends its pictures to a server
and serves an RTSP stream, and no single type held both.

A person adding a camera also had to know which driver fits before they knew
what the camera does. What they know is where its pictures come from and
whether it streams.

## rules

1. `core/generic` takes its still images one of two ways. With no
   `snapshot_url` it keeps the pictures it is sent, through
   `PUT /api/v1/cameras/{camera}/snapshot` or a module's `camera.accept`.
   With `snapshot_url` it fetches them, and a picture sent to it is refused,
   because the next fetch would replace it.
2. It streams only from `stream_url`. An `rtsp://` or `rtsps://` address is
   an H.264 stream, which the WebRTC relay plays in a browser. An
   `http://` or `https://` address is relayed as MJPEG. Any other scheme is
   refused when the camera is declared.
3. It declares no MJPEG made of the pictures it is sent. At one picture every
   half minute such a stream shows nothing new, and a listing that offers it
   would offer a live view that is not one.
4. A stream is read only while somebody watches it, as every driver's is.
5. It is built from the push, mjpeg and rtsp drivers and adds no media code.
   A user name and a password go in the address, where those drivers already
   keep them out of every log.
6. The app declares a new camera as `generic`, or as a type an installed
   module offers, which runs on `generic` (ADR-20261106-km2). `push`, `mjpeg` and `rtsp`
   stay for the cameras already declared as them and for the configuration
   file, and are listed and changed as they are.

## enforcement

| rule | gate |
|---|---|
| 1 | the first two tests in `adapters/camera/generic/generic_test.go` |
| 2, 3 | the last two tests there, and the first one's check that no stream opens |
| 5 | `make binary-check`: the driver links nothing the three drivers do not |
| 6 | the Cameras tab's tests, which add a camera as `generic` (ADR-20261106-xm4) |

## rejected

| option | why not |
|---|---|
| a new driver with RTSP and HTTP code of its own | it would repeat two drivers tested against real cameras, and a fix would reach one copy |
| keeping `push` and `mjpeg` in the app's dialog beside `generic` | a person would still pick a driver first, and `generic` covers every camera those two do |
| removing `push`, `mjpeg` and `rtsp` | cameras are declared as them today, in overlays and configuration files that nothing rewrites |
| a list of streams per camera | no camera seen so far offers two streams worth choosing between, and a list would make every page ask which one to play |
