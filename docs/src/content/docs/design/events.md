---
title: Events have one shape
description: Every event is sent in the same envelope, with a registered type and the device it came from.
audience: build
rewrite:
  describes: current
  implementedBy:
    - sdk/event
    - internal/transport/stream
---

Everything that happens is published as an event in one envelope. The
envelope names the type of the event, the device or resource it came from,
and a position a client can resume from.

A browser reads events over a stream, and modules and sinks can subscribe to
them. An event type is registered before it is published.

## Why is it this way?

One shape means one client, one filter and one way to resume after a
disconnect. A module can react to something another module did without
knowing that module.

## What does it mean for my change?

Publish an event when something a client or a module would want to know has
happened. Register its type first.

Before adding an event, check who listens to it. An event that starts
another event, which starts a third, makes the system do things nobody
planned.
