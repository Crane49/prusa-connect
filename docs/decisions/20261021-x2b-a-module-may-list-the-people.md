---
title: "ADR-20261021-x2b: a module may list the people"
description: "The list_people permission: a module an operator approved may list the people the server holds, each with a subject, a name and a kind, and nothing else about them."
date: 2026-10-21
status: accepted
tags:
  - adr
  - authorization
  - modules
  - security
---
# ADR-20261021-x2b: a module may list the people

- status: accepted
- date: 2026-10-21

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Related: [ADR-20260929-f4n](20260929-f4n-a-module-may-ask-about-a-person-who-is-away.md)

A module that plans work for people has to name them. A planner assigns a
run to a person, and a record names the person who wrote it. Without a
list, the module keeps its own list of names, and that list does not match
the people who sign in. With this permission, the module asks the server
for the people it holds and uses the same subject the server uses for
grants. The subject is the `user:<id>` principal that `access.check` takes.

[ADR-20260929-f4n](20260929-f4n-a-module-may-ask-about-a-person-who-is-away.md) gave a
module a way to ask about one person and deliberately gave no way to list
them. A list tells a module who exists on this server, which a question
about one person does not. So the list is a permission of its own, and the
operator approves it by name.

## rules

### what the module declares and the operator grants

1. A module asks for the permission with the manifest key
   `list_people: true`. It is a boolean beside `check_absent_people`, and it
   is not an entry of `permissions`.
2. The declaration carries it as `service.list_people`. Absent or false is a
   module without the permission.
3. `module add` and `POST /api/v1/modules` write the key into the declaration
   only with approve, as they write `check_absent_people`. Approving it takes
   the same admin role on the organization that approving a permission takes.
4. An install without approve lists it among the requests, as
   `permission list_people`. A marketplace row and a module index row carry
   it as `list_people`, and an approved install whose manifest asks for it
   when the row did not is refused with `MODULE_REQUESTS_CHANGED`.
5. The permission grants no action in the authorization graph. The instance
   writes no grant for it, and it does not widen `access.check`.

### what the module is told

6. The method is `access.people`. The host installs it only for an instance
   whose declaration carries `list_people`. Any other instance reads
   `CodeMethodNotFound`.
7. The answer is `{"people": [...]}`. Each row is `subject` (`user:<id>`),
   `name` and `kind`. The name is the account's display name, or its username
   when it has none. The kind is `user`.
8. A row never carries an e-mail address as such, a role, a grant, a
   password date or any other field of the account. A username that is an
   e-mail address is still the name when the account has no display name.
9. The rows are sorted by name, then by subject. The answer holds at most
   5000 rows, `accessapi.MaxPeople`. A server that holds more answers the
   first 5000 by name, and a client refuses an answer longer than that.

### which people are listed

10. On a server that keeps local accounts, the list is one row per account.
11. A server with an OpenID Connect login, or one in open mode, keeps no
    list of people and answers an empty list. Its pairings are not listed:
    a pairing names a credential and a label an administrator wrote, and
    neither is a person's name.
12. The list is what the server holds at the moment of the call. A module
    that keeps a subject asks `access.check` again before it acts for that
    person.

### audit and bounds

13. Every listing spends a token from the bucket of
    [ADR-20260929-f4n](20260929-f4n-a-module-may-ask-about-a-person-who-is-away.md) rule 15,
    before anything else. With no token left, the answer is
    `ErrTooManyQuestions`, code 6003.
14. The host writes one info log line for each listing that rule 13 lets
    through. The line names the instance, the number of rows and the answer.
15. `access.people` adds no error code. A failure of the host's own is
    `CodeInternal` with the fixed sentence `HostFailed`.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `sdk/manifest` tests, which parse the key and refuse a key that is not a boolean | exists |
| 3, 4 | `internal/app` module install tests, over the command and over HTTP, and the ceiling test | exists |
| 6, 13, 14 | `internal/app/resourcehost` people tests: served only when approved, the audit line and the bound | exists |
| 7, 9, 15 | `sdk/accessapi` wire tests: the round trip, the order, the bound on both sides, the refusals | exists |
| 10, 11 | `internal/app` people test against the real account store, in both modes | exists |

## rejected

| option | why not |
|---|---|
| widen `check_absent_people` to list | an operator who approved one question per person did not approve a list of everybody |
| let the module keep its own list of people | its names do not match the people who sign in, and it cannot ask `access.check` about them |
| carry the e-mail address and the role | the module needs a name to show and a subject to keep. More is a wider read than the work needs |
| page the answer | the bound fits every deployment this server targets, and a page adds a cursor the module has to keep |
| list the pairings on an OpenID Connect server | a pairing is not a person and has no name |

## open items

- an OpenID Connect deployment answers an empty list. A record of people
  seen at login would let rule 11 list them
- the account store has no disabled state. When one exists, the list leaves
  a disabled person out
