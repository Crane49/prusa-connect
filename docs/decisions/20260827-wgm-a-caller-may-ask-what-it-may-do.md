---
title: "ADR-20260827-wgm: a caller may ask what it may do"
description: "Which permission-review shapes this API serves, that a review is self-scoped and answers from the rows the server enforces on, that no shape validates an object id before answering, and that an answer names actions and carries the authorization epoch."
date: 2026-08-27
status: accepted
tags:
  - adr
  - authorization
  - api
  - security
---
# ADR-20260827-wgm: a caller may ask what it may do

- status: accepted
- date: 2026-08-27

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260825-zkq](20260825-zkq-api-compatibility.md), [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md), [ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md)

A client learned its permissions by attempting an action and reading the
refusal. That is correct and it is not enough. An interface cannot grey a
control it has not attempted. Sending raw gcode to learn whether raw gcode is
allowed is not a probe anybody wants a printer to receive.

Earlier research measured the four shapes a client needs. They are two queries with
two projections each, because the graph walk already happened on the write
path. It also measured the way this surface is most likely to go wrong. A check
endpoint that validates an object id is how a server stops being able to hide
which objects exist.

## rules

1. Answer a permission question only about the caller. The subject of every
   review is the principal the middleware resolved, and no request member may
   name another.
2. Refuse a request that names a subject rather than ignoring the member. A
   member this server drops while answering about the caller reads to its sender
   as an answer about the principal they named.
3. Charge no permission for a self-scoped review. A caller who cannot ask what
   they may do reads a blank interface and files a bug, which is why Kubernetes
   binds `SelfSubjectAccessReview` to every authenticated user.
4. Take an identity for every review. Unprivileged is not unauthenticated:
   without a credential there is no self to answer about.
5. Validate no object id against the directory before answering. An id that is
   well formed is answered whether or not this server has an object by that
   name, and grammar is the only thing a review may check.
6. Answer an object the caller reaches nothing on and an object this server does
   not have with the same bytes. Earlier research executed the pair on
   both engines, and the zero mask is what a missing row and an unreachable row
   both produce.
7. Compute every review from the rows the enforcement surfaces read, and never
   from a registration record or a second query shape. A review that says yes
   where `Can` says no is worse than no review.
8. Name actions with the identifiers `GET /api/v1/vocabularies/authorization`
   publishes. Never a mask and never a bit position. A mask is meaningless
   without a bit assignment `internal/authz/vocabulary.go` is free to renumber.
   `actionSet(1)<<32` is zero, which a client cannot tell from a denial.
9. Carry the caller's action set on every row a list already returns. It is one
   column of the row the query filtered by, so it costs no statement and cannot
   disagree with the filter that admitted the row.
   - **This property survives a condition.**
     [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
     rules 23 and 37 put two masks on a projected row. The filter reads the
     mask this rule returns, and the conditional subset is a second column
     beside it. So the set on the row is still the set the query filtered by,
     and the conditional actions are marked and not subtracted.
10. Stamp every answer with the authorization epoch it was computed at. Any
    grant write moves it, so a client comparing it against a cached answer knows
    the answer is stale.
11. Say in the operation's own description that a review answer is a hint and
    never the decision. The server checks the permission again when the action
    is attempted.
    - **A state answer is a hint on the same terms.**
      [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
      rule 25 keeps this rule over a published precondition. The device's state
      at the attempt is what decides.
12. Publish the refusal shape as a contract. `core.auth/ACCESS_DENIED` carries
    `action` and `object`. A client learns what it lacked from the attempt it
    was going to make anyway, at no query cost and with nothing to probe.
13. Serve a batch review as one statement and publish its cap as a number in the
    OpenAPI document. A loop of point checks costs 1.02 s at 400 ids on
    Postgres against 5.45 ms for the same answer in one statement.
14. Refuse an object type no action is defined on, rather than answering the
    empty set for it. An org holds grants and carries no actions, so "you may do
    nothing on org:acme" states a decision about the org that nobody took.

## enforcement

| rule | gate | what it refuses |
|---|---|---|
| 1, 4 | `TestTheCheckTakesAnIdentity` | an answer produced for a request carrying no principal |
| 1 | `TestTheCheckOperationCarriesTheRootSecurityRequirement` | an operation declaring the empty security requirement |
| 2 | `TestABodyNamingAnotherSubjectIsRefused` | a `subject` member reaching the graph |
| 5, 6 | `TestAnAbsentObjectAndAnUnreachableOneAnswerTheSameBytes` | any difference between the two refusals at the transport |
| 5, 6 | `TestAnAbsentPrinterReviewsLikeAnUnreachableOne` | the same difference in the graph, on both engines |
| 7 | `TestObjectActionsMatchThePointCheck` | a printer answer disagreeing with `Can` |
| 7 | `TestResourceKindActionsMatchThePointCheck` | a resource-kind answer disagreeing with `Can` |
| 8 | `TestTheReviewNamesActionsInTheVocabularyOrder` | an order the published vocabulary does not render |
| 8 | `cmd/generate-authz-vocabulary -check` | a name outside the published vocabulary |
| 9 | `TestAListedRowCarriesTheActionsThePointCheckPermits` | a listed row whose set disagrees with the point check |
| 9 | `TestTheWalkCarriesTheSameActionsTheListingDoes` | a column added to one directory query and not the other |
| 12 | `make error-codes-check` | a param leaving `ACCESS_DENIED`'s published list |
| 14 | `TestTheReviewRefusesAnObjectTypeThatCarriesNoActions` | an object type with no actions answering empty |
| 14 | `TestTheReviewableTypeEnumMatchesTheVocabulary` | an enum that has drifted from the vocabulary |
| 3, 10, 11 | review | see [what a reviewer holds](#what-a-reviewer-holds) |
| 13 | none | nothing serves a batch. See open items |

### Two mechanisms produce the refusal for rule 1

The middleware reads the operation's security requirement, and the handler
refuses a context carrying no principal. Either alone answers 401, so breaking
one leaves `TestTheCheckTakesAnIdentity` green. A reader checking that gate has
to break both, and the test says so beside itself.

### What a reviewer holds

Rules 3, 10 and 11 have no gate. Whether the operation charges a permission is
visible in one line of its registration. Whether the answer carries the epoch is
one field. Whether the description says a review answer is a hint is a judgement
over prose, and no pattern makes it.

## rejected

| option | why not |
|---|---|
| returning the raw `actionSet` integer | a mask is meaningless without the bit assignment, and it couples every client to a layout the vocabulary may renumber |
| putting the review behind an `inspect_access` action | it makes a self-scoped question privileged, which is what Kubernetes' own reasoning refuses |
| telling clients to attempt the action and handle the refusal | it is the status quo, and it makes a correct interface impossible |

### A check that accepts any subject

It is Kubernetes' `SubjectAccessReview`, and Kubernetes gates it separately for
a reason. A caller who may ask about another principal enumerates the graph one
bit at a time: every id, every action, one request each. This project has no
action for "may inspect another principal's access", so there is nothing to gate
such a check with, and inventing one is a decision of its own.

### A review answered from a registration record

It would report a permission the enforcement surfaces do not carry. Earlier research
section 2.1 names the case. The event fan-out filter routes on `event.Origin`
with no action predicate, so a declared permission finer than an origin is
unenforceable on that surface. A review sourced from the declaration would tell
a client it may do something the stream cannot stop it observing.

### The grant surface as the client's permission query

`Grants` is gated by `mayAdminister`, so it answers only for a caller who
already administers the object. A viewer holding one printer is refused when
they ask about the printer they can see. It also returns tuples rather than
effective permissions, so a client would have to re-implement the projection to
read an answer out of them.

### A deadline and a max-results cap on a list answer

OpenFGA's defaults exist because an engine walks a graph per request.
Earlier research's C3 measured what they cost. `ListObjects` returned 116 of 400
printers, with no error and no marker saying it had truncated. Here the walk
happened on the write path, so the equivalent read is a range scan. The deadline
has nothing to protect against. A limit that truncates a visibility answer is
the failure an earlier design decision rejected `ListObjects` for.

### The self rules shape

`SelfSubjectRulesReview`'s equivalent, one answer listing every object the
caller holds anything on, does not ship.

Rules 9 and 13 cover every use but one. A client may render a fleet-wide
control before it has fetched any printer, and the first page of the fleet
listing answers that at no extra cost.

Against that, it hands a stolen token the whole visibility map in one request.
Measured on the fleet fixture: 34 KB and 780 µs for an org admin, against 2.5 KB
for the common case. The answer holds nothing the token could not obtain by
asking eleven times. So it is a convenience for an attacker rather than a new
capability, and it is not worth building for a caller who already has the
listing.

Ship it when a client exists that cannot use the listing, and audit its access
when it does.

## open items

- rule 13 has no gate and nothing serves a batch. The shape is decided and the
  cap is not. Earlier research puts the crossover between an `IN` list
  and a prefix scan somewhere between 50 and 400 ids on SQLite, and does not
  locate it. The row is on the old backlog
- the authorization epoch is on a review answer and not on the event stream, so
  a subscriber whose grant widens still picks the new origin up silently.
  Earlier research, and the row is on
  the old backlog
- rule 6 rests on an absence of evidence for timing. Earlier research
  measured 29.9 µs against 27.3 µs on SQLite, with the spread inside each group
  wider than the gap. Nothing below that noise floor is ruled out
- no shape answers "why can I see this". `Grants` is gated by `mayAdminister`,
  so a viewer cannot read the grant that gives them their own access. That is
  the first question on a support ticket. Earlier research
- a review answers two outcomes and
  [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
  rule 24 asks for three: permitted, refused, and permitted under a condition.
  Nothing here carries the third. It changes a published response shape, so it
  owes [ADR-20260825-zkq](20260825-zkq-api-compatibility.md) a migration note, and ADR-20260831-c7b's
  enforcement table marks its gate owed
