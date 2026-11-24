# Buddy3D camera

A module for Prusa Connect that is the server a Buddy3D camera talks to
instead of Prusa's cloud. The camera sends its pictures to this module over
HTTPS, and the module hands each one to the core's camera, linked to a
printer. The camera's Socket.IO session stays open, so the module sets how
often the camera sends a picture and can ask for one at any time.

The live view is a WebRTC connection straight from the camera to the browser.
The module carries only the offer and the answer over the camera's session,
so the video never passes through the server. The browser must reach the
camera's address, which on one network it does.

Nothing leaves your network.

## How a camera is set up

1. On a printer's Cameras tab, add a camera of the type Buddy3D camera. The
   core lets this module feed a camera of its type, so no grant is written by
   hand.
2. Under that camera, the Buddy3D section offers to pair it. Each pairing
   makes a new token.
3. A camera on no Wi-Fi yet, or one that should join another, needs the
   Wi-Fi: check **Set wifi credentials** and type its name and
   password. Unchecked, the camera keeps the Wi-Fi it has. The password
   stays in your browser.
4. Download `local_cfg.tar`, copy it onto a FAT32-formatted microSD card,
   put the card in the camera and start it. The archive holds `srv_cfg.txt`, `self-signed.pem` and
   `sdcard_cfg.txt` with the token, and the Wi-Fi when you typed it. Your browser makes the
   archive. The camera needs firmware 3.1.7 or later.

Once the camera has used its token, the section under the camera shows
whether it is connected and when it sent its last picture. It can ask the
camera for a picture now, and restart it. Play on the camera's picture starts
the live view. While the camera is connected, three tabs hold the rest:

- **Control** turns a camera that can turn, by a step from 1° to 90° a press,
  and sets its infrared to automatic, off or on and its video quality. The
  camera reports no position, so a turn is always from where it points now,
  and a picture follows each turn.
- **Settings** chooses how often the camera sends a picture, or switches its
  pictures off. It also chooses the one stream the camera runs: WebRTC for the
  live view, RTSP for a video player, or none. It starts a timelapse on the camera's SD card and
  sets the speaker volume.
- **Information** shows the camera's name, firmware, hardware, temperature,
  network and SD card. The name can be changed here.

Changing anything takes a person who may operate the camera. The module keeps
the picture interval and the stream, and tells the camera each time it
connects. The camera keeps the rest itself, so it changes only while the
camera is connected. Each control shows only on a camera that says it has it.

The first request the camera sends binds its fingerprint to the token. A token
that no camera uses within 15 minutes stops working; forget the camera and set
it up again for a new one.

## Settings

| key | default | meaning |
|---|---|---|
| `address` | `""` | The address a camera dials and the certificate names. Empty is this machine's first IPv4 address. |
| `snapshot_interval` | `30` | How often a camera sends a picture, in seconds, from 10 to 254, unless the camera has its own. |

## The time server

A camera with the wrong time does not work, and it says nothing about it.
`srv_cfg.txt` names no time server, so a camera keeps its own, which needs
the internet. On a network with no internet, check **Use a custom time
server** when you pair the camera. Name one on your network, such as your
router, or this server with `core.time_server.enabled` on. Give a host name or
address with no port: the camera asks port 123 only.

## What the module needs

- `camera.accept`, the wire method that hands a picture to a camera
  (ADR-20261106-2er in the core).
- The `buddy3d` camera type it offers (ADR-20261106-km2). The core grants the
  module `operate` on every camera of that type.
- `access.check`, to ask whether the person on the page may see a camera
  before it starts that camera's live view.
- TCP port 9443, approved with `-approve-listen`. It is not 8443, which the
  core's printer listener takes. The operator can move it with
  `-listen camera=<port>`.
- A data directory, for the paired cameras and the certificate.

## Building

The module builds against the `sdk/` and the `packages/` of this checkout.
Its gate is `make -C extensions check EXT=buddy3d-camera`. In this folder:

| target | what it does |
|---|---|
| `check` | lint, test, check the client, then build and check `ui/` |
| `test` | build, vet and test the Go half |
| `ui-deps` | build the core's packages, then install `client/` and `ui/` |
| `client` | generate `client/` again from `openapi.json` |
| `release` | the release directory for this machine, in `bin/release` |

## The camera's protocol

`internal/device` and `internal/sio` serve what the firmware 3.1.7 was seen to
use: `PUT /v1/camera-info`, `PUT /v1/snapshot` and a Socket.IO 4 websocket
whose events are protobuf (`internal/camproto`, schema 4.4 to 4.6). The
module encodes the messages itself, and `camproto_test.go` checks them against
the schema in `internal/camproto/testdata`.
