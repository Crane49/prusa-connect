---
title: "ADR-20261031-2kf: a Fractal node refused permanently never asks again"
description: "A Fractal node that the node above denies with a code that allows no retry, or that refuses a grant, keeps that refusal in its data directory. It sends no further join request, also after a restart, and serves the nodes below without a link to the node above."
date: 2026-10-31
status: accepted
supersedes: []
amends: [ADR-20261020-tca, ADR-20261023-7r3]
tags:
  - adr
  - federation
  - fractal
---
# ADR-20261031-2kf: a Fractal node refused permanently never asks again

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20261020-tca](20261020-tca-a-fractal-node-keeps-a-shadow-of-the-nodes-above-it.md) rule 19, [ADR-20261023-7r3](20261023-7r3-fractal-files-are-shared-folders-fetched-over-https.md) rule 9

A Fractal node that the node above denies permanently keeps the denial in its
data directory. It sends no further join request until an operator clears the
denial, also after a restart of the module or of the machine. It keeps serving
the nodes below without a link to the node above. The Fractal specification of
2026-10-20 says a node denied with `ERR_JOIN_REJECTED`, `ERR_JOIN_COLLISION` or
`ERR_JOIN_REVOKED` MUST NOT retry, and that the denial stands until an operator
acts (fractal.md, Error Codes). A host starts a module that stops again, and a
reboot is not an operator act, so a denial kept only in memory is not enough.

## rules

1. A denial the joiner does not retry is a permanent refusal. These are
   `ERR_JOIN_REJECTED`, `ERR_JOIN_COLLISION` and `ERR_JOIN_REVOKED`. They are
   also `ERR_JOIN_UNAUTHORIZED`, because the module gets no new join
   credential while it runs. The binding logs the denial as an error.
   A grant the node refuses by fractal.md, After the Join Exchange, is a
   permanent refusal too. A node with children refuses a grant whose parent
   identity differs from the saved one, before it opens the connection.
   It refuses a grant with another Node Path the same way. It adopts nothing
   and logs the error. The same parent at a new address carries the saved
   identity, so the node takes that grant. A grant of a protocol version the
   node does not support, or with join data an extension refuses, is a
   permanent refusal as well (fractal-files.md, Join Data). This replaces "The
   module stays up without a connection until it is started again" in
   ADR-20261023-7r3 rule 9. fractal.md does not name a grant that misses a
   required field. The joiner of the sdk keeps that refusal like the others,
   so the binding keeps it as a permanent refusal too.
2. The binding writes a permanent refusal to `refusal.json` in the data
   directory. While it holds one, `Run`, `Join` and the renewals send no join
   request, and `Join` returns the refusal. The binding keeps a denial or a
   refused grant before it sends the next request. It sends one join request
   at a time, so a `Join` during a renewal does not ask again. A `Join` that
   waits for its turn while another one is denied permanently returns that
   denial at once. A new start of the module reads the file, so it asks
   nothing either. `Status.Uplink` shows the code and the reason, also after a
   restart. The binding writes the file to a temporary file first and renames
   it, so a power loss leaves the whole file or none. A file that is empty or
   cannot be read still refuses, and the binding logs the error.
   `Status.Uplink` then marks the refusal unreadable, and the links page shows
   its own fault, which names `refusal.json`.
3. The module does not stop on a permanent refusal. `Run` waits until its
   context ends. This replaces "before the module stops" in ADR-20261020-tca
   rule 19.
4. A node refused permanently that has a saved Node Path runs its NATS server
   without the leaf remote to the node above, and serves the nodes below. This
   holds when the refusal arrives at the start, in a renewal, or in a `Join`
   from outside `Run`, and after a restart. The files client calls such a
   `Join` when the node above refuses its credentials. A node that never
   joined has no Node Path and starts no NATS server.
5. A node refused while it runs stops the renewals of its credentials and
   removes the leaf remote. The NATS server then does not dial the node above
   again.
6. On `ERR_JOIN_REVOKED` the binding also forgets the ancestor shadow with
   `Node.ForgetAncestors` and the join data of the node above
   (ADR-20261020-tca rule 19).
7. An operator clears a permanent refusal: stop the module, delete
   `refusal.json` from its data directory, and start it. A refusal with
   `ERR_JOIN_UNAUTHORIZED` also ends when the module starts with another join
   credential, because fractal.md says to retry once a valid one is obtained.
   An operator who admits a revoked node again on the node above also deletes
   `refusal.json` on that node.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `TestADeniedNodeLogsTheDenial` in `cd packages/fractal && go test ./natsbind/` | exists |
| 1, 2, 4, 5 | `TestARefusedNodeNeverAsksAgain` in `cd packages/fractal && go test ./natsbind/` | exists |
| 1, 4 | `TestANodeWithChildrenRefusesAnotherParent` in `cd packages/fractal && go test ./natsbind/` | exists |
| 1 | `TestANodeWithChildrenFollowsItsParentToANewAddress` in `cd packages/fractal && go test ./natsbind/` | exists |
| 2 | `TestAJoinDuringARefusedRenewalAsksNothing` in `cd sdk && go test ./fractal/join/` | exists |
| 2 | `TestAJoinThatWaitsTakesTheDenial` in `cd packages/fractal && go test ./natsbind/` | exists |
| 2 | `TestARefusalReplacesItsFileWhole` in `cd packages/fractal && go test ./natsbind/` | exists |
| 2 | `TestAnUnreadableRefusalStillRefuses` in `cd packages/fractal && go test ./natsbind/` | exists |
| 2 | `TestTheReadingSaysWhatTheConnectionIsDoing` in `cd packages/fractal && go test .` | exists |
| 2, 3 | `TestARevokedNodeDoesNotAskAgainWhenItStarts` in `cd packages/fractal && go test ./natsbind/` | exists |
| 4 | `TestARevokedNodeServesItsChildrenWhenItStarts` in `cd packages/fractal && go test ./natsbind/` | exists |
| 2, 3, 5 | `TestARevocationLearnedElsewhereStopsTheRenewals` in `cd packages/fractal && go test ./natsbind/` | exists |
| 5 | `TestARevokedNodeStopsDialingItsParent` in `cd packages/fractal && go test ./natsbind/` | exists |
| 6 | `TestRevocationDropsExtension` in `cd packages/fractal && go test ./natsbind/` | exists |
| 6 | `TestARevokedNodeForgetsItsAncestorShadow` in `cd packages/fractal && go test ./natsbind/` | exists |
| 7 | `TestAPermanentRefusalIsCleared` in `cd packages/fractal && go test ./natsbind/` | exists |

## rejected

| option | why not |
|---|---|
| let the process end and the host start it again | the new process asks again, which fractal.md forbids |
| keep the refusal only in memory | a restart by the host or a reboot is not an operator act, and the new process would ask again |
| ask the host not to start the module again | the module could then not start after an operator fixed the cause, and the host has no such state for one module |
| take the whole node down on a refusal | the nodes below lose their link for a decision about the node above |
