---
title: "ADR-20261026-kfh: a Fractal node sees every drop of a leaf connection"
description: "The Fractal binding reads the connect and disconnect events of its embedded NATS server, so both ends of a link see every drop, however short. The poll of the leafnode connections stays and decides when a link is up."
date: 2026-10-26
status: accepted
supersedes: []
amends: [ADR-20261020-tca]
tags:
  - adr
  - federation
  - fractal
---
# ADR-20261026-kfh: a Fractal node sees every drop of a leaf connection

- status: accepted
- date: 2026-10-26

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20261020-tca](20261020-tca-a-fractal-node-keeps-a-shadow-of-the-nodes-above-it.md), its open item on short drops

A leafnode connection is the NATS connection from the server of a node below
to the server of the node above. `fractal-nats.md`, Connectivity Detection,
says: "A parent MUST treat the loss of a child's leaf connection as that
child's disconnect, however briefly it lasts and regardless of heartbeat
timing". The binding read the leafnode connections once a second. The NATS
server dials a closed connection again after one second, so a drop lasted
about one poll, and the node above saw 1 drop of 10
(ADR-20261020-tca, open items).

This record closes that open item of ADR-20261020-tca, and replaces its
section "a short drop is mostly not detected". No rule of ADR-20261020-tca
changes.

The embedded server runs with a system account, so it publishes an event
when any connection opens or closes. nats-server v2.14.3 has no other hook
for a leafnode connection.

## rules

1. The binding subscribes to `$SYS.ACCOUNT.*.CONNECT` and
   `$SYS.ACCOUNT.*.DISCONNECT` of its own server, on a connection of its own
   in the system account. It reads only the events whose connection kind is
   `Leafnode`.
2. A disconnect event for the leafnode connection a node below last listened
   on marks that link lost at once. The engine gets `ChildLost`, and then
   `ChildConnected` when the poll sees the new connection listen, even when
   the next poll already finds the new connection open.
3. A disconnect event for an older connection of the link changes nothing.
4. The poll of the leafnode connections stays. It decides when a link is up:
   two polls in a row must see the subject the other end subscribes to. It
   also settles the state when an event is lost.
5. After a connect event for a leafnode connection, the binding polls every
   5 ms until that connection listens, for at most one poll interval after
   that event. An event for another connection does not extend it. Two
   polls are never closer than 5 ms.
6. Every event makes the binding poll at once. So a node below calls
   `ParentLost` when its server reports that the connection to the node
   above closed, and `ParentConnected` when the poll sees the new connection
   listen. A node below does this for every drop, as a node above does in
   rule 2.

## enforcement

| rule | gate | state |
|---|---|---|
| 1, 2, 5, 6 | `TestBothEndsSeeEveryShortDrop` in `cd packages/fractal && go test ./natsbind/` | exists |
| 6 | `TestTheNodeBelowSeesItsParentDropAtOnce` in the same package | exists |

`TestBothEndsSeeEveryShortDrop` replaces `TestAPollMissesMostShortDrops`,
which ADR-20261020-tca names. It cuts the link of a node for 10 ms, ten
times, at the default settings, and requires all ten drops at both ends.

## rejected

| option | why not |
|---|---|
| a shorter poll interval | a poll reads every leafnode connection with its subscriptions, and a drop shorter than the interval is still missed |
| the events alone, without the poll | an event the client drops when its buffer is full would leave a link in the wrong state until the next event |
| mark a link up on the connect event | the event comes before the node below has sent its subscriptions, and a full state report sent then is lost |
