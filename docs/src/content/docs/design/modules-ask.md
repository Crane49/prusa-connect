---
title: A module asks, and the operator approves
description: A module declares what it needs in its manifest, and gets it only when the operator agrees at install.
audience: build
rewrite:
  describes: current
  implementedBy:
    - sdk/manifest
    - internal/app/resourcehost
---

The manifest of a module lists what it needs: permissions, network ports, a
database of its own, and its settings with their defaults. The operator sees
that list at install and approves it.

When a module acts for a person, it acts with the permissions of that
person. It is handed who the caller is. It is never handed a password or a
token.

## Why is it this way?

An operator has to be able to trust a module they did not write. That needs
a list they can read before they agree, and a server that gives the module
nothing beyond the list.

## What does it mean for my change?

Ask for the least a module needs. Give every setting a default that works,
and one sentence that says what it does.

Verify what a module downloads before it runs it.
