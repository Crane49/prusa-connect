---
title: One registry for each kind of name
description: Error codes, event types, permissions, resource kinds and settings are each registered in one place, and the reference pages are generated from there.
audience: build
rewrite:
  describes: current
  implementedBy:
    - sdk/fault
    - sdk/event
    - internal/kernel/ref
---

Error codes, event types, permissions, resource kinds and configuration keys
are each registered once, in code. The reference pages on this site are
generated from those registries, and a check fails when a page and the code
differ.

## Why is it this way?

A name that exists in one place can be found, documented and translated. An
error shown by its code leads to the line that raised it. Documentation that
is generated cannot fall behind the code.

## What does it mean for my change?

A new error is a registered code with a message, and the web app shows it by
that code. A new event type, permission or setting is added to its registry,
with one sentence that says what it is.

After adding one, run the generator for that reference page and commit the
result. A module registers its own names the same way.
