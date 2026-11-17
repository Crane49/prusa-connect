---
title: "ADR-20261106-jnb: a module may ask about an action it does not take"
description: "The checks manifest key: a module lists the actions it asks access.check about for a person, without taking them as itself, and an install withholds nothing for them."
date: 2026-11-06
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - authorization
  - modules
---
# ADR-20261106-jnb: a module may ask about an action it does not take

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Related: [ADR-20260929-f4n](20260929-f4n-a-module-may-ask-about-a-person-who-is-away.md), [ADR-20261021-x2b](20261021-x2b-a-module-may-list-the-people.md)

`access.check` holds the action a module asks about to the module's
`permissions:` list. Materials asks whether a person may operate a printer
before it lets them change the material on a tool, so it listed `operate`.
An approved install turns each permission into a grant on the organization.
The organization cannot confer `operate` on a module, so no grant was written
and the install reported `operate` as withheld, with a warning. The module
worked as intended, because the question needs only the list, and the warning
said that something was missing.

A module that asks about an action and a module that takes it are two
different requests. The operator should read which one a module makes.

## rules

1. A module lists the actions it asks `access.check` about, without taking
   them as itself, under the manifest key `checks:`. Each entry is an action
   name of the authorization graph, as in `permissions:`.
2. An action is not listed under both `checks:` and `permissions:`. A
   permission may be asked about already, and the manifest is refused.
3. The declaration carries the list as `service.checks`. `module add` and
   `POST /api/v1/modules` write it only with approve, and approving it takes
   the same admin role on the organization that approving a permission takes.
4. An install without approve lists each entry among its requests, written
   as `check <action>`. A marketplace row and a module index row carry the
   list under `checks`. `MODULE_REQUESTS_CHANGED` refuses an approved install
   whose manifest checks an action the row or `expect` did not show.
5. An approved update keeps the checks the operator left and leaves out the
   ones they removed. It names each check the release before did not ask for.
6. A check grants no action. The install writes no grant for it and reports
   nothing withheld for it.
7. `access.check` answers a question whose action is in `permissions:` or in
   `checks:`, and refuses any other with `accessapi.ErrDenied`. The subject
   rules do not change.
8. A check never widens what the module does as itself. A call the module
   makes as itself is held to `permissions:` alone.
9. A permission an install could not grant is still reported as withheld,
   and the report says the module may still ask whether a person holds it.

## enforcement

- `sdk/manifest` tests refuse an action listed twice and one listed under both
  keys.
- `internal/app/modulechecks_test.go` installs a module with `checks:` and
  asserts the declaration, the approved lines and an empty withheld list.
- `internal/app/moduleaccess_test.go` asks about a checked action for a
  person and asserts that the module cannot take it as itself.

## rejected

| option | why not |
|---|---|
| Only reword the warning | The install still reports a missing permission, on every install. |
| Leave `operate` out of Materials' permissions | `access.check` refuses every question about `operate`. |
| Let the organization confer `operate` on a module | The module would operate every printer as itself. |
