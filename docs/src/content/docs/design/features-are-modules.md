---
title: Features are modules
description: What goes beyond the basics is added as a module, outside the core.
audience: build
rewrite:
  describes: current
  implementedBy:
    - internal/extension
    - sdk/manifest
---

The core covers identity and permissions, events, the registry of resources,
running modules, and updating and backing up the server. Everything else is
a module or an adapter.

A module is a separate program with a manifest. It can add pages and tabs to
the web app, serve its own routes, keep its own data and react to events. An
adapter is compiled into the server and implements one contract, such as a
driver, a storage or a database.

## Why is it this way?

A small core is easier to keep correct and to run on small hardware. A
feature in a module can be installed, switched off or replaced without
touching the server.

## What does it mean for my change?

Adding a module changes nothing under `internal/`. If a module needs
something the core does not offer, add that to the SDK as a general
capability. Do not add the feature itself to the core.

Keep a module small enough that another module can build on it.
