---
title: "ADR-20261106-xm4: a printer page has a cameras tab and a module draws under each camera"
description: "A printer's page has a Cameras tab with one card per camera: its still frame, a live view a person starts, the camera's controls, and the sections modules draw for it in the camera-section slot, handed the camera's name."
date: 2026-11-06
status: proposed
supersedes: []
amends: [ADR-20260911-4p6]
tags:
  - adr
  - extensions
  - cameras
---
# ADR-20261106-xm4: a printer page has a cameras tab and a module draws under each camera

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md) | Related: [ADR-20261029-dgv](20261029-dgv-a-file-page-has-tabs-a-module-may-add-to.md), [ADR-20261106-ypn](20261106-ypn-a-camera-is-one-generic-type-with-a-still-source-and-an.md), [ADR-20261106-2er](20261106-2er-a-module-hands-a-frame-to-a-push-camera.md)

A printer's cameras had no place of their own. The Status tab drew the first
camera's picture, and nothing in the app added, changed or removed a camera.
A module can feed a camera, for example as the server a cloud camera talks
to. It knows whether the camera is connected and how often it sends, and it
has settings only it can change. It had no place to draw any of that.

Every slot a module had was about a whole printer or a file. A camera is
neither, and a printer can have several.

## rules

### the tab

1. A printer's page has a Cameras tab, after Files. It is on the page only
   when the printer has a camera or the person may add one. A page never
   draws a tab that only says it has nothing to show. A retired printer has
   no Cameras tab.
2. The tab draws one card per camera that watches the printer. No second list
   of the same cameras is drawn.
3. A card shows the camera's last still frame. A camera that declares a stream
   the page can play has a play button: MJPEG in an image, H.264 through the
   relay's WebRTC answer. The stream runs only while the person plays it. It
   costs the camera, the network and this server for as long as it runs. The
   still frame stays under the live view until the stream's first frame
   covers it. The picture says it is live only while it plays. The Now tab
   draws the printer's first camera beside a running print in the same way,
   with no larger copy of the picture.
4. A camera that is not running is drawn with no picture and says so, and a
   picture that does not load gives way to the same words.
5. For a person who may configure cameras, each card holds the camera's
   controls: change it, remove it, and copy its upload address when it keeps
   the pictures it is sent. The tab has one button that adds a camera, as
   `generic` (ADR-20261106-ypn) or as a type an installed module offers
   (ADR-20261106-km2). A camera from the configuration
   file offers no control, because the server refuses a write to it.
6. On a wide card the picture is on the left, and the camera's type and the
   modules' sections are beside it. On a narrow card they are under it.

### the slot

7. `camera-section` is a slot on the printer-detail screen, in the area
   `camera-foot`, of the section form. It is handed the camera's name, the
   one the camera's HTTP routes take. Each entry is drawn for every camera of
   the printer, in install order.
8. The core does not record which module a camera belongs to. A module draws
   nothing for a camera it does not look after. So the core draws a camera
   section with no heading of its own, and a section that draws nothing takes
   no room.
9. A section is drawn for every person who may see the camera. It reaches
   only what that person may reach, as every slot does (ADR-20260911-4p6
   rule 20).

## enforcement

`make slots-check` holds the table, the generated files, the reference and the
scaffold template for rule 7. The `PrinterCamerasPanel` tests hold rules 2 to
5. They cover the still frame, the live view a person plays, over WebRTC too,
a camera that is not running and a picture that does not load. They also
cover the controls a person who may manage cameras gets. The
`PrinterStatusPanel` test holds the Now tab's part of rule 3. The printer
page's test holds rule 1. The
`SlotOutlet` test holds rule 8, and the every-slot test finds the
`camera-section` outlet on its screen.

## rejected

| option | why not |
|---|---|
| a live view that starts when the tab opens | every camera on the printer would stream to every person who opened the page, for as long as it stayed open |
| the cameras' controls in a list of their own on the Printer tab | the same cameras twice on one page, one list with pictures and one with buttons |
| a camera section handed the device id | a printer can have several cameras, and the module could not tell which one a section is for |
| a `camera-tab` slot, one tab per module | a camera's settings belong beside its picture, and the tab strip would grow with every module |
| a key on the camera naming the module that draws for it | the camera resource takes no key a module adds (ADR-20260827-wmv rule 14) |

A module draws the live view of a camera of a type it offers in the
`camera-view` slot (ADR-20261106-km2).
