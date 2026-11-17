---
title: The core knows devices, not printers
description: The core handles devices and names no brand, model, firmware or file format.
audience: build
rewrite:
  describes: current
  implementedBy:
    - internal
    - sdk/connectorapi
---

The core handles a device: its identity, its permissions, its state and its
events. It does not know what a printer is. It names no brand, model, firmware
or file format.

What is specific to one kind of device lives in its driver. The driver
declares what the device can do, which states it has and which files it
accepts. The core and the web app read what was declared.

## Why is it this way?

People run printers from many makers, and other devices beside them. A core
that knows one brand has to be changed for every other one. A core that reads
declarations works with a device nobody on the team has seen.

## What does it mean for my change?

If a change in the core, the SDK contracts or the shared web components
mentions a model or a brand, it belongs in a driver. Add what is missing to
the declaration of the driver and read it from there.
