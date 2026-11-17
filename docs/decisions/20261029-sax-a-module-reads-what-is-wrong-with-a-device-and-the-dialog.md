---
title: "ADR-20261029-sax: a module reads what is wrong with a device and the dialog it waits on"
description: "The module device record carries the device's issues and the dialog it waits on, and one event says when either changes, so a module can notify somebody or answer the dialog."
date: 2026-10-29
status: accepted
supersedes: []
amends: [ADR-20260827-xca]
tags:
  - adr
  - extensions
  - devices
---
# ADR-20261029-sax: a module reads what is wrong with a device and the dialog it waits on

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260827-xca](20260827-xca-a-fleet-says-what-is-wrong-with-it.md)

The host works out what is wrong with each printer and the HTTP listing carries it, but the module contract did not. A module that wanted to tell somebody about a printer that needs a person had to read the state tree and repeat the host's rules. Nothing told it when the answer changed, so it had to poll.

A tester asked for an extension that sends the server's events to messengers through Apprise. This record is what such an extension needs from the core. The extension itself is left to the community.

## rules

1. `deviceapi.Device` carries `Issues`, the list the HTTP listing carries, worst first, each with its reason, severity and detail.
2. `deviceapi.Device` carries `Dialog` while the device waits on a person: the id `core.command.dialog_action` takes, the firmware's code, the title, the text and the button labels, as the device encoded them. A departed device carries none, as its issue list names no dialog.
3. The dialog is read from the reserved dialog subtree, which gains `/dialog/buttons`. A driver that writes these paths gets the dialog on the module record without the core knowing its dialect.
4. `com.prusa.connect.device.issues.changed` is published when a device's issues or its dialog change. It carries the whole list and the dialog, so a reader that missed a frame is correct on the next one.
5. Changes that arrive within half a second are folded into one frame that carries what they settled on. A list that did not change publishes nothing. A departure is not repeated in this event.
6. The issues the event judges use the link reading the link report took. The event takes no reading of its own, because a reading rolls the window it is taken over.

## enforcement

`TestAModuleReadsWhatIsWrongAndTheDialog` holds rules 1 and 2. `TestADialogIsOneIssuesFrameAndItsEndIsAnother` holds rules 4 and 5 on a real hub. `TestTheDialogPathsShareOneSubtree` holds rule 3. The event-types vocabulary gate holds the new type.

## rejected

| option | why not |
|---|---|
| a module reads the state tree and works the issues out itself | every module would repeat the host's rules, and they would drift apart |
| one event per issue raised or cleared | a module would rebuild the list from edges, and a missed frame would leave it wrong |
| publish each change at once | a dialog is written one path at a time, so a module sending a message per frame would send two |
| an Apprise extension in this repository | the core gives every extension what it needs. Which messengers to support is for the people who use them |
