---
title: "ADR-20261102-vnk: a Fractal node forgets a node below that it revoked"
description: "A Fractal node that revokes a node below deletes its shadow of that node and of every node under it, and deletes the method invocations that wait for it. It keeps the NATS account of the link, because the account carries the revocation."
date: 2026-11-02
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - federation
  - fractal
---
# ADR-20261102-vnk: a Fractal node forgets a node below that it revoked

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

An operator revokes a node below, and that node never connects again. A node
keeps a shadow of each node below, which is its copy of that node's state, and
a queue of the method invocations that wait for that node. The Fractal
specification of 2026-10-20 says the shadow of a removed node SHOULD be
deleted (fractal.md), and that all data of a removed node SHOULD be deleted
(fractal-requirements.md). A revocation is the one permanent removal a node
knows of, so it deletes both.

## rules

1. A node that revokes a node below deletes the shadow of that node and of
   every node under it. The binding calls `Engine.ChildRevoked` after
   `ChildLost`, and the engine deletes the shadow with `Store.RemoveNode`.
2. The binding purges the stream of method invocations that wait for the
   revoked node. It keeps the NATS account of the link, because the account
   carries the revocation. A purge that fails is logged and not tried again,
   and those method invocations expire after `stream_max_age`.
3. The binding takes the stream from the link before the purge, and it waits
   for every publish that already holds the stream. A method invocation for the
   revoked node that is sent during the revocation is then refused or purged.
   None waits in the stream after the purge. The test of this rule sends calls
   while the revocation runs, so a broken rule fails it in some runs only.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `TestARevokedChildLeavesTheShadow` in `cd sdk && go test ./fractal/node/` | exists |
| 1, 2 | `TestARevokedNodeLeavesNothingBehind` in `cd packages/fractal && go test ./natsbind/` | exists |
| 3 | `TestCallsSentDuringARevocationAreDeleted` in `cd packages/fractal && go test ./natsbind/` | exists |

## rejected

| option | why not |
|---|---|
| keep the shadow and mark it out of date | the node never reports again, so its shadow stays out of date and is never removed |
| keep the waiting method invocations until they expire | the node never connects to read them, so they only take disk space until `stream_max_age` |
