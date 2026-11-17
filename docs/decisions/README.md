---
title: "Decisions: architecture decision records"
description: "Index of the ADRs binding on the codebase, what each one rules about, and when a change needs a new one."
status: done
tags:
  - adr
  - index
  - moc
---
# architecture decision records

Rules that shape the codebase. Each file is a set of numbered rules, the gate
that enforces them, the alternatives already rejected, and where the code does
not yet follow them.

An ADR is a rule you follow, not a record of how it was made. Keep the
reasoning to the `rejected` table, one line per option, enough to stop it being
proposed again.

An accepted ADR is never edited. A rule that no longer holds is replaced by a
new ADR, which lists the old one in its own front matter. It uses `supersedes:`
when it replaces the whole record, and `amends:` when it changes some rules. The old
record keeps its status. The site shows "superseded by" and "amended by" beside
it, computed from the newer records. Some older records still carry a
`superseded in part by` status from before this rule, and it stays as written.

## who decided a record

A record states its rules and does not say who decided them. Who decided a
rule, and when, is in the commit that added it. An agent that believes a rule
is wrong opens a GitHub issue.

## when a change needs an ADR

- a new top-level directory, or a change to the package groups in ADR-20260824-kkr
- a change to the public surface under `sdk/`
- a new extension mechanism, or a change to which tier something runs in
- a dependency in a hot path or in `sdk/`

## index

This folder keeps no index, because every new record would edit the same
table. The documentation site builds it from the front matter of each record,
on the page **Records / Decisions**: its id, title, status, date, and the
records it supersedes or amends. In the vault, Dataview lists the same:

```dataview
TABLE status, date, supersedes, amends
FROM "docs/decisions"
WHERE file.name != "README" AND file.name != "template"
SORT date ASC
```

## writing a record

```sh
make adr-new TITLE="a server answers to one id on every address"
```

This writes `docs/decisions/<date>-<xxx>-<slug>.md`, such as
`20261025-k7q-a-server-answers-to-one-id-on-every-address.md`, and the record is
cited as `ADR-20261025-k7q`. The id is the date in UTC and three random
characters, so two pull requests never take one id and no file lists them.
Every record carries such an id. The records written before the scheme took
the date they were written on. `go test ./internal/arch/` refuses a numbered
file, two files with one id, and front matter that does not match the file name.

New records start from [template.md](template.md).

## where these sit

An ADR is the subset of an earlier design proposal that has
hardened into a rule the code is held to. The proposal is the whole target; this
directory is what is already binding. The evidence for both is
earlier research.
