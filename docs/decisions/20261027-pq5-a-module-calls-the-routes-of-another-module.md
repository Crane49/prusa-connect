---
title: "ADR-20261027-pq5: a module calls the routes of another module"
description: "A module names the modules it calls under calls in its manifest, the operator approves the list, and the module calls their routes with routes.call: as the person it is serving, checked as the HTTP mount checks them, or as itself. An approved install also grants write_storage on the storage kind."
date: 2026-10-27
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - modules
  - sdk
---
# ADR-20261027-pq5: a module calls the routes of another module

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Related: [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20260929-f4n](20260929-f4n-a-module-may-ask-about-a-person-who-is-away.md), [ADR-20261021-x2b](20261021-x2b-a-module-may-list-the-people.md)

A module could reach the host's fleet, storages and event stream, and could
not reach another module. A planner that needs G-code from a slicing module,
or a queue that asks a statistics module, had two ways around it. The page
could call both modules as the person and carry the answer from one to the
other. That fails for work done with no page open, such as a plan made at night. Or
the core could offer each such service itself. That makes the core know every
module. So a module calls another module's routes through the host, the same
door a person uses.

A module that writes files also needs `write_storage`. No role on the
organization that a module's principal may hold reaches it, so an approved
install wrote no grant, and an operator had to write one through the API.

## rules

### the declaration

1. A manifest names the modules it calls under `calls:`, as a list of
   namespaces, at most 16. A namespace is the segment the module's routes are
   mounted under, `/api/v1/modules/<namespace>`.
2. `calls` is a request. The operator approves it at install, as events and
   `list_people`, and the declaration records it under `service.calls`. An
   approved update keeps a call the operator took away out.
3. A module named under `calls` need not be installed. The calling module
   starts without it, and a call to it is refused.

### the call

4. The wire method is `routes.call`. Its params are the namespace, an
   optional principal, and one HTTP request: method, path, query, headers and
   a body of at most 1 MiB. The answer is the other module's response, as
   `routes.serve` answers it. A larger transfer goes through a storage.
5. The path stays inside the other module's mount once it is unescaped, as
   `net/http` unescapes it on the other side. It starts with one `/`. It has
   no `.` or `..` segment, no empty segment, no backslash, no `?` and no `#`.
   It has no escaped `/` or backslash (`%2f`, `%5c`), and nothing still
   escaped after one unescape (`%252e`). Any other path is `ErrCallInvalid`,
   code 7003. A path is refused and never cleaned into one that passes. The SDK checks
   it before the call leaves, and the host checks it again where it relays
   the call.
6. The host serves `routes.call` to a module whose declaration names at
   least one call, and to a module whose manifest requires `routes.call`.
   It refuses a namespace the declaration does not name with
   `ErrCallNotDeclared`, code 7000. So a module that requires the method
   still starts when the operator approved no call. The same holds when the
   operator took the last call away on an update. Each of its calls is then
   refused.
7. A namespace no running module with routes holds, or the calling module's
   own, is `ErrCallUnavailable`, code 7001. So is a module that did not
   answer. The two read alike, so a module learns nothing about what the
   server runs beyond what it may call.

### who the call runs as

8. A call naming a person runs as that person, and only while the host is
   serving that person's request to the calling module. It is held to the
   scopes of that request. The host then asks the graph for
   `view_resource` on the other module's instance for a GET or HEAD. It asks
   for `operate_resource` for any other method, as the HTTP mount asks
   (ADR-20260827-wmv rule 13). A refusal is `ErrCallDenied`, code 7002.
9. A call that names no principal is the calling module's own work. It runs as the
   module's principal, `sink:<instance>`, with no scope. The approved `calls`
   entry is what lets it reach the other module. The host asks the graph
   nothing more.
10. Whatever the other module then does for that principal is held to the
   principal's own grants. A slicing module reads the project and checks who
   may write the G-code as the calling module. So the calling module needs
   `read_storage` and `write_storage` of its own.
11. The host boards the call on the other module as a request it forwards.
    While the call is open, the other module may make storage, fleet and
    stream calls as that principal. Later it may ask `access.check` about
    it, as about a person it served.

### the grant of write_storage

12. An approved install writes each permission as the smallest role on the
    organization that reaches it, as before. `read_storage` and
    `write_storage`, when the organization cannot give that role to a
    module's principal, are written as that role on the `storage` resource
    kind, where the storage routes check them. In practice `read_storage` is
    granted on the organization and `write_storage` on the storage kind.
    An update whose manifest no longer asks for it and a removal take the
    grant back like any other.

## enforcement

| rule | gate |
|---|---|
| 1 | `TestTheTopLevelKeysAreValidated` and `TestAFullManifestReadsEveryKey` in `sdk/manifest` |
| 2 | `TestAnUpdateKeepsACallTheOperatorTookAwayOut` |
| 4 | `TestACallReachesTheHostAndAnswersTheOtherModule`, `TestACallOutsideTheBoundsIsRefusedBeforeItLeaves` |
| 5 | `TestAPathThatLeavesTheMountIsRefused`, `TestACleanPathStaysAsItIs`, `TestARelayedCallNeverLeavesTheMountOfTheModuleCalled` |
| 6 | `TestAModuleCallsOnlyTheModulesItDeclared`, `TestAModuleThatRequiresCallsStartsWithNoneApproved` |
| 7 | `TestAModuleCallsOnlyTheModulesItDeclared`, `TestAModuleCannotCallItself` |
| 8 | `TestAModuleCallInAPersonsNameIsHeldToThatPerson` |
| 9 | `TestAModuleCallWithNoPersonRunsAsTheModule` |
| 11 | `TestTheCallIsBoardedOnTheModuleCalled` |
| 12 | `TestWriteStorageIsGrantedOnTheStorageKind`, `TestAStorageGrantIsTakenBackOnUpdateAndRemoval` |
| 3, 10 | none. Review holds them |

## rejected

| option | why not |
|---|---|
| a `requires: - type:` entry as the permission to call | it makes the other module a hard dependency, and a module that only slices on request must start without it |
| the page calls both modules and carries the answer | a plan made at night has no page open |
| the core offers slicing, statistics and each such service | the core would have to know every module, and an operator who installs none pays for all |
| refuse the install of a module that requires `routes.call` with no call approved | the update that takes the last call away fails too. See rule 3 |
| a call as the module checked in the graph against the other instance | the operator approved `calls`, which is the decision such a grant would record |
| a stream for large bodies | a file moves through a storage, which both modules reach with their own grants |

## open items

- a chain of calls, A to B and B back to A, is bounded only by the deadline of
  each call
- the Extensions page lists `calls` among the requests, and does not yet say
  whether the module named is installed
