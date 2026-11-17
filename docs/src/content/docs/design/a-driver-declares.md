---
title: A driver declares what its device can do
description: The core reads what a driver declares and never works it out from a model name or a firmware version.
audience: build
rewrite:
  describes: current
  implementedBy:
    - sdk/connectorapi
---

A driver tells the core what its devices can do: the commands they accept,
with their arguments, and the states they can be in. The core offers a
command only when the device declared it.

The core never compares firmware versions to decide what a device supports.
It reads capabilities.

## Why is it this way?

A list of models and versions inside the core is out of date the day a new
firmware ships. A declaration travels with the driver that knows the device.

## What does it mean for my change?

To support a new command or a new state, declare it in the driver. The API,
the permissions and the web app pick it up from the declaration.

A command the device refuses is an answer and not an error. Return it as a
result with a registered code, so a client can tell a refusal from a failure.
