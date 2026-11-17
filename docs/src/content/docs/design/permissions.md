---
title: Permissions are checked everywhere
description: Every way into the server gives the same answer about who may do what.
audience: build
rewrite:
  describes: current
  implementedBy:
    - internal/authz
---

Who may do what is recorded in one place: a graph of people, groups and
devices. Every way into the server asks it: the HTTP API, the event stream,
and a module that acts for a person.

A list contains only what the caller may see. The filter is part of the
query, so page counts and totals reveal nothing about the rest.

## Why is it this way?

A permission checked on one path and forgotten on another is the most common
way such a system leaks. One source and one check make that hard to do.

## What does it mean for my change?

A new route checks the caller before it does anything. A new list filters in
the query, never after fetching. A new event is delivered only to
subscribers who may see its device.

A command that can damage a device is granted by name to one person on one
device. A role does not include it.
