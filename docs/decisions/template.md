---
title: "ADR-YYYYMMDD-xxx: short title"
description: "One sentence saying what this record rules about."
date: YYYY-MM-DD
status: proposed
supersedes: []
amends: []
tags:
  - adr
---
# ADR-YYYYMMDD-xxx: short title

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

Start a record with `make adr-new TITLE="short title"`, which writes this shape
with a fresh id. List an older record this one replaces under `supersedes`, and
one it changes in part under `amends`. Never edit the older record.

## rules

Numbered, imperative, one rule per line. Each one is something a contributor
can follow or a reviewer can point at. State what to do, not what was done.

1. ...
2. ...

## enforcement

The gate that makes a rule fail CI rather than fail review. Omit if there is
none, and expect the rules to decay.

## rejected

| option | why not |
|---|---|
| the alternative someone will propose again | one line |

## open items

Where the code does not yet obey the rules, and what unblocks it. One line each.
Delete the section when it is empty.
