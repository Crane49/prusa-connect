---
title: The core keeps no history
description: The core publishes events and holds the current state. A provider stores history when one is installed.
audience: build
rewrite:
  describes: current
  implementedBy:
    - sdk/historyapi
---

The core holds the current state of each device and publishes events. It
does not store temperature curves, past states or statistics.

A history provider is an adapter that subscribes to events and stores them.
When one is installed, the history routes answer from it. When none is, they
say so.

## Why is it this way?

How much history to keep, and where, differs for a home server and a farm.
Making it an adapter lets each installation choose, and keeps the core small.

## What does it mean for my change?

A feature that needs the past reads it from the history contract and works
without it where it can. Do not add a table to the database of the core to
remember what happened.
