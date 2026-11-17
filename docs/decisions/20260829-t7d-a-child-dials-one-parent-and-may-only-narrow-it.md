---
title: "ADR-20260829-t7d: a child dials one parent and may only narrow it"
description: "How a node is named, who admits a child, which way the link is opened, what crosses it, how authorization replicates downward, and what a node does while the link is down."
date: 2026-08-29
status: superseded in part by ADR-20260921-wkf
tags:
  - adr
  - federation
  - authorization
  - events
  - security
---
# ADR-20260829-t7d: a child dials one parent and may only narrow it

- status: superseded in part by ADR-20260921-wkf. Rules 8 to 11
  give way to the join exchange, rules 12 to 14 and 39 do not apply, and the
  rest stands
- date: 2026-08-29

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) · Builds on [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md), [ADR-20260826-c77](20260826-c77-one-name-for-one-thing-on-the-wire.md), [ADR-20260826-5yj](20260826-5yj-a-device-is-the-object-and-a-printer-is-the-type.md) · Reads with [ADR-20260827-xca](20260827-xca-a-fleet-says-what-is-wrong-with-it.md), [ADR-20260828-rp4](20260828-rp4-a-published-event-type-declares-its-payload.md)

A second site wants to see the printers at the first one. A design requirement says
those sites sit behind NAT, nest to whatever depth an operator's organisation
has, and keep working when the link between them is out.

Nothing in this tree does that yet. `internal/transport/stream` fans events out
in process, `internal/authz` answers from one database, and the site token in
every `source` is `config.DefaultSite`, the string `local`.

Three research notes argue the shape and are not repeated here.
Earlier research settles the topology,
earlier research settles how authorization
replicates, earlier research settles the
envelope. Between them they leave four things a builder has to have. What names
a node. Who admits a child. Whether a subject puts the node path first or the
facet first. Where the link lives in this server's own configuration. This
record decides those four and turns the rest into rules.

## what the tree already holds

Read this before building anything below. Several of the items earlier research calls
expensive to retrofit are in the tree already. A second copy of any of them is
the defect this section exists to prevent.

| already built | where |
|---|---|
| `epoch`, `hops` and `evclass` on the envelope, and `ttl` on commands only | `sdk/event/envelope.go` |
| the hop bound, at 32, refused by `Validate` and by `Forward` | `sdk/event.MaxHops` |
| a relative TTL decremented by measured elapsed time, rounded up | `event.Envelope.Forward` |
| `(source, epoch, sequence)` minted by the owning node, one writer per source | `internal/transport/stream/channel.go` |
| a resumable cursor carrying the epoch, so a reader learns its stream was recreated | `sdk/event/cursor.go` |
| ULID identity for a device, with the path held as an attribute, which is an earlier design decision | `internal/kernel/ref` |
| `node` as a resource kind and as an authorization principal | `internal/kernel/ref/ref.go`, `internal/authz/vocabulary.go` |
| supervision with a bounded queue, a per-call timeout, a breaker, restart and a status event | `internal/app/resourcehost` |

## rules

### a node and its name

1. Identify a node by a ULID minted at first start and persisted beside the
   configuration. That ULID is the site token of every `source` this node
   publishes. It is never reused and never rewritten by a relay.
2. Keep `site:` in the configuration as the node's display name. It lives in the
   directory, an operator may change it, and it appears in no envelope, no
   cursor and no signature payload.
3. Name a peer with the two vocabularies that already name one, `ref.KindNode`
   and `authz.TypeNode`. Add no third name for the same thing, which is
   [ADR-20260826-c77](20260826-c77-one-name-for-one-thing-on-the-wire.md) rule 1.

### the link

4. Declare the link as one supervised resource under `resources:`, of a kind
   `uplink` that the core constructs and no module may supply. Refuse a second
   instance of it.
5. Dial outward, always. A child opens the connection and a parent never dials a
   child.
6. Hold exactly one parent link open. A second parent address is a failover
   endpoint taken one at a time, and taking it bumps the link epoch.
7. Carry a command down the connection the child opened, and re-enter it through
   the authorization a local API call takes. A parent is not an administrator of
   its child.

### admitting a child

8. Admit a child with a single-use join token the parent issues, carrying an
   explicit lifetime and naming the node ULID the child supplied. Burn it on
   first use, and refuse a lifetime of zero or less at parse time rather than
   reading it as unbounded.
9. Issue that token to a holder of `org:<org>#admin` and to nobody below it.
10. Generate the child's key pair on the child. The private half never leaves it
    and the parent learns only the public half.
11. Pin one root public key at every node and verify a parent's delegation
    certificate against it. Enrolment is the only moment trust material arrives
    out of band, and in an air-gapped deployment that is a file an operator
    carries.

### what crosses the link

12. Forward the bytes that arrived. Decode into `event.Envelope` to route, and
    never re-marshal that struct back onto the wire.
13. Change `hops` and `ttl` and nothing else, by splicing the received bytes at
    those two fields. Every signature covers the envelope without them.
14. Decide propagation from `evclass` and never from `type`. Forward a `type`
    this build has never heard of, and read an `evclass` it has never heard of
    as `state`.
15. Give a parent a rollup whose size is a function of its child count and never
    of the printers beneath them. Propagate only mergeable aggregates: `(sum,
    count)` and never a mean, with a capped exception list and an `omitted`
    count.
16. Keep byte streams off the link. A camera frame and a gcode file take a path
    of their own. A key for a relayed camera rides the authorization channel and
    never the media path, which is
    earlier research.

### addressing

17. Route on `<node-path>.<kind>.<id>.<facet>[.<detail>]`, node path first.
    Identity stays `urn:prusa:site:<node-ULID>/<kind>/<id>` and is never parsed
    for meaning.
18. Resolve a subscription through the directory and never against a literal
    subject. A rename is a subject mapping and a re-parent is a directory
    generation bump. Neither is a data migration.

### authorization

19. Replicate two planes. A ceiling flows down and combines by intersection, and
    a grant combines by union and is then clipped by the ceiling. Resolve
    `EC(n) = EC(parent(n)) ⊓ local_narrowing(n)` once at sync into one row, and
    never walk the chain at check time.
20. Enforce the ceiling in the evaluator. A record exceeding its issuer's
    ceiling verifies as authentic and evaluates to deny everywhere, including at
    the node that owns the resource.
21. Read "no ceiling record" as "inherit the parent's resolved ceiling
    verbatim", never as the empty set. Refuse to persist an empty ceiling
    without an explicit confirmation.
22. Inherit every deny. No flag stops one reaching a child.
23. Key every replicated record `(Issuer, Epoch, Seq)`, sign it with the
    issuer's key, and relay that signature unchanged. A parent forwards a subset
    and never re-signs one.
24. Order by `Seq`. Write a revocation as `revoke_all_issued_before_seq`, and
    let `NotAfter` be the only field that reads a clock.
25. Bump `Epoch` automatically when a node's persisted high-water `Seq` exceeds
    what its database holds. Recreate `Epoch` and the counter it scopes together
    or recreate neither.
26. Send existence up and authority down. A parent learns that a resource exists
    beneath it and never learns a grant from below.
27. Say `RESYNC_REQUIRED` rather than continue past a watermark the parent can
    no longer bridge. A node in that state keeps serving locally-issued
    authority and refuses to answer for remote-issued authority until the
    resync completes.

### loops and freshness

28. Exchange ancestor sets at the handshake and refuse a cycle there, naming
    both nodes in the refusal.
29. Bound a storm with `hops` at `event.MaxHops`. Log, count and alert an
    envelope that exceeds it, with its `source`, its `id` and the ingress link.
    Never drop one silently.
30. Refuse an event whose `source` node lies inside this node's own subtree and
    arrived on this node's parent link.
31. Route toward a named node ULID. There is no primitive that sends to the
    root, and `$upstream` may appear in configuration and never in a routing
    decision.
32. Express a command's freshness as remaining seconds, decremented per hop by
    the elapsed time that hop measured on its own monotonic clock. Let no event
    expire.

### the link is down

33. Treat the link being down as the ordinary state. One component owns link
    state and tells the subsystems; nothing polls for it.
34. Authorize offline indefinitely from the local copy. Locally-issued authority
    never expires.
35. Write the uplink's queue to disk in the transaction that writes the state
    change, and drop nothing from it. Cap it by bytes and by age, and publish
    queue depth, queue bytes and the age of the oldest unsent event per link.
36. Derive liveness from the parent's view of the link and never from the
    child's self-report. Carry the worst freshness marker among a node's
    children upward in its rollup, and render staleness rather than hide it.
37. Fail an interactive command fast rather than queue it. Anything that must
    survive an outage is desired state, reconciled on reconnect.
38. Reconnect with full jitter and no attempt cap, bound the reconnect buffer,
    and reset the backoff on application-level liveness rather than on a dial
    returning.
39. Define the link protocol over one duplex byte stream and name no substrate
    in it.

## the node ULID is the site token

Rule 1 changes a value that is already on the wire, so it needs its reason
written down rather than assumed.

`config.DefaultSite` is `local`. Two nodes that never met both publish
`urn:prusa:site:local/printer/...`, and a parent receiving both has no way to
tell them apart. The comment beside that constant already says so. Nothing in
the configuration loader can fix it, because uniqueness across a tree is not a
property one node can check.

A name also cannot survive being changed. `source` is the dedup key CloudEvents
mandates, the scope `sequence` is counted within, and the payload any signature
covers. Renaming a site would invalidate all three for every event already
stored, at every node that stored one.

A ULID is unique without coordination and is never renamed, so the display name
becomes free to change. That is an earlier design decision applied to the node itself, and
`internal/kernel/ref` already mints one for a device.

## the node path comes before the facet

Earlier research names this trade
and refuses to pick, so this record picks. The subject is at once the routing
key, the per-link publish boundary and, on any substrate with a storage filter,
the retention filter.

`>` must be the final token on the substrate earlier research surveys. So node path
first cannot express "all reported state across every site", and facet first
cannot express "everything at and below this site".

Node path first wins because the publish boundary is a subtree. A child
publishes under its own prefix and nowhere else, which is one rule that holds
for a facet added next year. Under facet first the boundary is one rule per
facet, and the facet nobody remembered is the fail-open case.

The query the other form buys is answered elsewhere. "What can this principal
see, anywhere" is routed to the node that homes the principal and answered from
the visibility projection, which is earlier research
§7.3. It was never going to be a subject wildcard.

## enforcement

| rule | gate | state |
|---|---|---|
| 1, 2 | `TestASourceURNCarriesTheNodeULIDAndNotTheConfiguredName` | owed. `Hub.Options.Site` takes the configured string today |
| 3 | `internal/arch` naming gate over a third peer name | none today |
| 4 | `TestASecondUplinkInstanceIsRefused` | owed. No `uplink` kind exists |
| 5, 6 | `TestTheUplinkOpensOneOutboundLinkAndAcceptsNone` | owed |
| 7 | `TestACommandFromTheParentIsAuthorizedLocally` | owed |
| 8, 9 | `TestAJoinTokenIsSingleUseAndRefusesANonPositiveLifetime` | owed |
| 10, 11 | `TestAChildKeyIsGeneratedLocallyAndTheRootKeyIsPinned` | owed |
| 12, 13 | `TestAnUnknownExtensionAttributeSurvivesFourHops` | built. `sdk/event.ForwardBytes` and its tests |
| 14 | `TestAnUnknownEventClassIsForwardedAsState` | owed |
| 15 | `TestRollupSizeDependsOnChildCountAndNotOnLeafCount` | owed |
| 16 | `make style-check` check on encryption claims, plus `internal/arch/encryption_claims_test.go` | partial. The claims gate exists; nothing forbids media on the link |
| 17, 18 | `TestASubscriptionResolvesThroughTheDirectory` | owed |
| 19 to 22 | `TestAnOverBroadChildRecordEvaluatesToDeny` | owed. Neither plane exists |
| 23 to 25 | `TestASeqRegressionBumpsTheEpoch` | owed |
| 26, 27 | `TestASyncPastTheTombstoneFloorSaysResyncRequired` | owed |
| 28 to 30 | `TestAHandshakeRefusesACycleAndNamesBothNodes` | owed |
| 29 | `Envelope.Validate` and `Envelope.Forward` refuse past `MaxHops` | built for the bound. Nothing counts or alerts |
| 31 | `internal/arch` gate refusing `$upstream` outside configuration | none today |
| 32 | `TestForwardDecrementsHopsAndExpiry` | built |
| 33 to 38 | `TestTheOutboxSurvivesADisconnectAndDrainsInOrder` | owed |
| 39 | none. A gate would assert the absence of a dependency nothing adds | none today |

## rejected

### an operator-chosen site name as the identity

This is what the tree does today and it is the option somebody will keep, because
it costs nothing to leave alone. The reason it loses is above, under "the node
ULID is the site token". A name is neither unique across a tree nor stable
across a rename, and `source` needs both.

Take the cost seriously. Every golden fixture and every test that reads
`urn:prusa:site:local` moves in the same change, and an operator reading a raw
event stream loses a readable site label. The directory gives that label back at
the surfaces a person reads, which is what rule 18 is for.

### the facet before the node path

Argued above under "the node path comes before the facet". Reopen it only with a
measured cross-tree query workload, because the projection in
earlier research is the answer
this record expects that workload to want.

### embedding `nats-server` in the core binary today

An earlier design decision chooses NATS leaf nodes and F2 gates that on one experiment nobody
has run. This record does not overturn either. It declines to let the wire
contract depend on the outcome, which is what rule 39 says.

The budget is the first thing standing between F1 and a build.
`scripts/check-binary-budget.sh` leaves between 262 kB and 311 kB of headroom on
its six rows. A dependency of any size is therefore a budget raise with a reason
in the commit, taken deliberately.

The second is that the substrate would not carry rule 28 anyway. NATS ADR-59
scopes server-side cycle detection out of sources across external domains, and
every link in a federated tree is one of those. See
earlier research.

| option | why not |
|---|---|
| a parent that dials its children | a design requirement puts sites behind NAT, so no child is reachable |
| two active parent links for availability | two links reorder one source's events by construction, which is an earlier design decision |
| the full ancestor path in every envelope | about 27 bytes a hop, 553 at depth 20, on a class emitted 1,600 times a second |
| a maximum depth, enforced | depth is unbounded. Every vendor cap sits between 5 and 10 and none publishes a reason |
| decode the envelope and re-marshal it to forward | it deletes every attribute this binary predates and breaks every signature over it |
| a shared fleet key baked into an image | anyone holding the image joins the tree. balena ships this and says so |
| hand-carried configuration bundles per node | Azure's nested edge does this over USB, and it is why its enrolment story is the worst surveyed |
| a `link` or `federation` resource kind beside `node` | two names for one peer, which ADR-20260826-c77 rule 1 refuses |
| a queued command that survives the outage | a `pause` delivered forty minutes late is a defect. balena calls this out in its own docs |
| an absolute expiry timestamp on a command | every node in the chain would have to agree on wall time, and a Pi has no RTC |
| CRDTs for the replicated authorization state | one writer per record, so nothing conflicts, and tombstone collection waits on a node offline for six weeks |
| a ceiling held by permission to write policy, as GCP does | the writer owns the disk and is offline, which describes every node this product ships |
| a non-inheriting deny, as Azure's `DoNotApplyToChildScopes` allows | it breaks the invariant that makes an offline decision sound |
| signing the whole bundle rather than each record | a parent forwards a scoped subset, so the bundle hash stops matching at the first hop |
| a wall clock anywhere on the ordering path | NATS tolerates zero skew where Kerberos allows five minutes, and these are boards with no clock |
| a hop limit of 1, which is HiveMQ's default | a limit of 1 means no chain at all, and it fails open on MQTT 3 |
| relaying camera frames through the event link | 400 streams is about 480 Mbit/s, and the link carries 0.05 Mbit/s of state |

## open items

- **The token in `source` is the configured name and rule 1 says it is a ULID.**
  Nothing federates, so nothing is wrong today. The change touches
  `internal/app/config`, `internal/transport/stream` and every fixture holding
  `urn:prusa:site:local`, and it has to land before a second node exists.
- **Neither authorization plane exists.** `internal/authz` holds one tuple store
  with no issuer, no epoch, no sequence and no signature. Rules 19 to 27 describe
  a table nothing writes.
- **No `uplink` kind exists**, so rules 4 to 7 constrain nothing. `ref.Kinds()`
  is a compile-time table and adding one is a change to that file, which is
  [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule 4
  working as intended.
- **`Hub.epoch` is minted per process, not per store.** That is safe: a
  restarted node is a new generation, and a parent's high-water mark resets with
  it. It does mean a parent resyncs a child on every restart. Rule 25 holds the
  pairing, and whether the epoch should be persisted is a measurement nobody has
  taken.
- **Rule 9 uses org admin because a node is not an object in the graph.**
  `authz.TypeNode` is a subject type only. If a deployment needs to delegate
  enrolment below org admin, the replacement is `node` as an object type
  carrying the three roles, and nothing else in this record moves.
- **Every number earlier research owe is still unmeasured**, including the
  per-hop latency, the Ed25519 rate on a Pi and the outbox drain rate. No rule
  here quotes one, and none may until
  the old backlog records them.
- **F2's experiment is unrun.** It must also measure what `nats-server` adds to
  each of the six budget rows, because that half decides as much as the sync
  behaviour does.
