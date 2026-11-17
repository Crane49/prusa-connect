---
title: "ADR-20260929-f4n: a module may ask about a person who is away"
description: "The check_absent_people permission: a module an operator approved may ask access.check about any person the server holds, at any time, and the server reads that person's current rights for every question."
date: 2026-09-29
status: accepted
tags:
  - adr
  - authorization
  - modules
  - security
---
# ADR-20260929-f4n: a module may ask about a person who is away

- status: accepted
- date: 2026-09-29

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

A module asks `access.check` whether a person may do an action on an object.
Without this permission the server answers only about a person the module
served since it started. With it, the server answers about any person it
holds, and it reads that person's current rights every time. A print queue
then keeps printing after a restart.

## rules

### what the module declares and the operator grants

1. A module asks for the permission with the manifest key
   `check_absent_people: true`. It is a boolean beside `permissions`, and it is
   not an entry of that list.
2. The declaration carries it as `service.check_absent_people`. Absent or false
   is a module without the permission.
3. `module add` and `POST /api/v1/modules` write the key into the declaration
   only with approve, as they write `permissions` and `events`.
4. An install without approve lists it among the requests, as
   `permission check_absent_people`.
5. The permission grants no action in the authorization graph. The instance
   writes no grant for it.

### what the module may ask

6. The permission widens only the subject of `access.check`. The module
   never acts as the person. Every fleet, storage and stream call still
   takes the person only while their request is open.
7. The action is still held to the instance's `permissions` list. An action
   outside it is `ErrDenied`, on a server that keeps a list of people.
8. A subject the host served since the instance started takes the path it
   took without the permission, and rule 11 holds there too. Rules 9, 10 and
   12 to 15 apply to every other subject.

### which persons it may ask about

9. The subject is `user:<id>`. Any other subject the host did not serve is
   `ErrUnknownSubject`.
10. The server answers only when it keeps local accounts. A server with an
    OpenID Connect login or in open mode answers `ErrUnknownSubject`. It
    refuses before it reads the graph, so the action does not matter there.
    The question still spends a token under rule 15.
11. On a server that keeps local accounts, a yes about a person counts only
    while the person has an account row, on every path. A deleted person and
    a person who never existed both answer no. A server without local
    accounts answers a person the module served from the graph alone.

### what the answer reflects

12. The server reads the authorization graph for every question. It keeps no
    answer between questions, so a revoked grant or a lowered role shows in
    the next answer.
13. The action check of rule 7 and the graph read run before the account
    lookup. So an action outside the list is `ErrDenied` for a deleted person
    too.

### audit and bounds

14. The host writes one info log line for each question on this path that
    rule 15 lets through. That includes the refusals of rules 9 and 10 and a
    failed account lookup. The line names the instance, the subject, the action,
    the object and the answer, each cut to 256 bytes. A failed lookup on the
    served path writes a warning, limited the way rule 15 limits its own.
15. Each instance has a token bucket for this path. It holds 600 questions
    and refills 10 a second. Every question on the path spends a token before
    anything else, a refusal too. A question with no token left is
    `ErrTooManyQuestions` and writes no audit line. The host warns about the
    first such refusal at once, and then at most once a minute per instance,
    with the number of refusals it left out.

### error codes

16. `access.check` answers these codes. The first three exist, and the fourth
    is added.

| code | sentinel | when |
|---|---|---|
| 6000 | `ErrDenied` | the action is not in the instance's `permissions` list |
| 6001 | `ErrUnknownSubject` | the host did not serve the subject and may not answer about it |
| 6002 | `ErrInvalidQuestion` | the question is outside the published bounds, such as an object type over 64 bytes |
| 6003 | `ErrTooManyQuestions` | the instance has no token left under rule 15. A retry a tenth of a second later can pass |

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `sdk/manifest` tests, which parse the key and refuse a key that is not a boolean | exists |
| 3, 4 | `internal/app` module install tests, over the command and over HTTP | exists |
| 6 to 15 | `internal/app/resourcehost` absent-person tests, one permit and one refusal per rule | exists |
| 10, 11 | `TestTheComposedServerAnswersAboutARealAccount`, over the plane and identity layer `serve` builds | exists |
| 11 | `internal/app` people test against the real account store | exists |
| 11 | `TestServeBuildsItsIdentityThroughIdentityFor`, the only callers of `buildIdentity` and `identityFor` | exists |
| 12 | `internal/app` module access tests, a lowered role against the real graph | exists |
| 16 | `sdk/accessapi` wire tests, each code round-trips to its sentinel | exists |

## rejected

| option | why not |
|---|---|
| keep the served memory on disk | a person stays asked-about forever, and the operator never sees the choice |
| put the key in the `permissions` list | that list holds graph actions, and the install writes a grant for each one |
| let the module keep a credential per person | the module could then act as each person, not only ask |
| ask the person to open the queue after every restart | every restart stops every waiting print |
| answer from the graph alone | a deleted person's grants stay in the graph, so the answer would be yes |
| a fixed window of 600 a minute | a burst at the edge of two windows lets 1,200 questions through in a moment |
| answer about service accounts and nodes too | the queue needs people, and each extra subject type is more of the graph to read |

## open items

- an OpenID Connect deployment keeps no account list, so rule 10 refuses
  there. A record of people seen at login would let rule 11 answer
- the account store has no disabled state. When one exists, rule 11 must
  answer no for a disabled person too
- the audit of rule 14 is a log line, not a durable record
