---
title: "ADR-20260826-53y: documentation tiers and release notes"
description: "A page declares the reader it assumes with an audience tier, the tier is gated by the repository paths the page is allowed to name, and a release note is assembled by hand from the changelog section a tag closed and published at one URL."
date: 2026-08-26
status: accepted
tags:
  - adr
  - documentation
  - release-notes
  - audience
  - enforcement
---
# ADR-20260826-53y: documentation tiers and release notes

- status: accepted
- date: 2026-08-26

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Rules: [changelog-discipline](../../.agents/rules/changelog-discipline.md), [writing-style](../../.agents/rules/writing-style.md) | Builds on [ADR-20260824-dym](20260824-dym-legacy-quarantine.md), [ADR-20260826-swm](20260826-swm-a-generated-reference-is-read-by-a-person.md), [ADR-20260826-gtb](20260826-gtb-writing-that-reads-as-written.md) | Amended by [ADR-20261021-arm](20261021-arm-pending-news-is-a-file-per-change.md) rules 15 and 20

There are three documentation sets: for a self-hoster who writes no Go, for
somebody modifying the product, and for us. Release notes sit beside the
changelog. Both matter less than moving the product forward, so
this record is adoptable in pieces and the enforcement table says which pieces
are hours.

Earlier research measured the first ask and found the set with the most readers is
the empty one. `docs/src/content/docs/getting-started.md` opens with
`Go 1.26.5 or newer`, `make`, Node and a private deploy token, and never names
the eight binaries `.github/workflows/release.yml` has published on every tag
since it was written. It measured the second over eleven Obsidian releases and
218 bullets: four stable section names, sections dropped when empty, a lede in
one release of eleven, zero tables, and a median bullet of 13 words against this
project's changelog median of 46. The finding there is narrow. An entry written
to answer whether a reader must act cannot also be the sentence somebody reads
to learn what shipped.

This record settles what a tier is, what a gate may read to decide one, where a
release note lives, and what it is assembled from. The release gates are still
owed, which is why the status is `proposed`.

## the measurement this record corrects

Earlier research proposes the tier check and reports that it "fails on six
pages today, small enough to fix in the same change so it ships with no
baseline". Re-run on 2026-08-26 with the extractor from
`internal/arch/onboarding_docs_test.go` over all 45 published pages, with the
frontmatter separated from the body and the tiers as this record assigns them:

| counter | pages failing | which |
|---|---|---|
| a `legacy/` path in a `run` or `extend` body | 9 | `getting-started`, `config`, `logging`, `metrics`, `tracing`, `sse-overview`, `sse-asyncapi`, `mqtt-overview`, `mqtt-asyncapi` |
| an `internal/`, `sdk/` or `cmd/` path above the tier | 6 | `connector-contract`, `writing-a-driver`, `capabilities`, `resource-kinds`, `authorization-vocabulary`, `developer-view` |
| both | 2 | `human-auth`, `storage` |

17 of the 27 reader-facing pages, against six. The count in earlier research is short
because it takes `getting-started.md` and `certificates.md` as the shape of the
problem. The shape is wider. Nine of the 17 fail on `legacy/` alone, which is
the retirement of the old server showing through the documentation, and not tier drift at
all. Separately, 24 of the 27 name a restricted path in `rewrite.implementedBy`,
where ADR-20260824-dym requires it. Rules 4, 7 and 8 are what those three numbers force.

## rules

### tiers

1. Every published page carries `audience: run`, `audience: extend` or
   `audience: build`. The value is a required enum in
   `docs/src/content.config.ts` beside `rewrite.describes`, so a page that omits
   it fails the whole collection at build time and cannot ship untagged.
2. The tier is what a page may assume its reader already holds. A `run` page
   names a URL, a config key, a flag, an environment variable, a port, a volume,
   an error code, an event name and a topic. An `extend` page names all of those
   and an import path under `sdk/`, an exported symbol and a descriptor field. A
   `build` page names all of those and a path under `internal/` and a line
   number. Storage has a page in every tier; the subject decides nothing.
3. A page names no repository path above its tier in its body. `run` names none
   under `internal/`, `sdk/`, `cmd/` or `legacy/`. `extend` names none under
   `internal/` or `legacy/`. `build` is unrestricted and every path it names
   resolves, which is the check `internal/arch/onboarding_docs_test.go` already
   applies to eight pages.
4. `rewrite.implementedBy` sits outside rule 3 and carries a check of its own:
   every path in it resolves, at every tier. ADR-20260824-dym
   requires that field to name the code a page describes, so a tier gate reading
   the frontmatter would forbid the record ADR-20260824-dym asks for.
5. A page that two tiers need becomes two pages. There is no second `audience`
   value and no `mixed`, because a gate handed two tiers has no restriction left
   to apply.
6. A generated page's `audience` is written by its generator, and the generator
   emits no path above that tier outside the frontmatter. Five of the six
   generated pages name `internal/` or `sdk/` in the sentence that says where
   the page came from, so this is a change in `cmd/`, not an edit to a page.
7. The two counters in rule 3 are baselined apart. A `legacy/` path in a
   reader-facing body falls to zero as the retirement deletes the file it names, and
   the fix is a deletion somebody else is already doing. A path under
   `internal/`, `sdk/` or `cmd/` above the tier is drift, and the fix is
   rewriting the sentence. One number cannot say which of those moved.
8. The tier check ships as a baseline ratchet, under ADR-20260826-gtb rule 9. One line
   per page per counter, failing when a count rises and failing when a count
   falls until the line is corrected. A page with no line is held to zero.
9. One build, one content root, one domain. A tier is a frontmatter value and a
   sidebar group, and it is neither a directory, nor a domain, nor a build of
   its own. `rehypeRelativeMarkdownLinks` and `docs/scripts/check-links.mjs`
   both assume a single `CONTENT_DIR`, and a link that crosses out of it is
   broken for the site reader and the vault reader at once.
10. The four Diátaxis forms are a contract on one page and never a level of
    navigation. The sidebar's top level is the three tiers plus reference, on
    Django's model, where the front page names the four forms and the navigation
    is by subsystem.
11. `index.mdx` routes by reader. It is the one page that names all three tiers,
    and it is `run`.

### release notes

12. A tag has one release note, at
    `docs/src/content/docs/releases/<version>.md`, tiered `run`. Earlier research
    section 8 puts it at `docs/releases/`, which is inside the Obsidian vault
    and outside the `CONTENT_DIR` set at `docs/astro.config.mjs:11`, so the site
    would not publish it, the link rewriter would not touch a link into it, and
    `make docs-check` would not see it. The published path is the one both
    readers reach.
13. The GitHub release body links to that page and lists the downloads. It
    carries no prose of its own, so a mistake is corrected in a file instead of
    in a published release.
14. A person assembles a note from the changelog section for its version.
    Every bullet traces to an entry under the matching `## <version>` heading,
    which is what keeps one source of fact and makes drift between the two
    detectable.
15. The fragments in `docs/changes/` are filed under `## <version> - <date>`
    before the tag is written, and deleted (ADR-20261021-arm). Assembly has no input
    until something files them, and `scripts/release.sh` has never opened
    `docs/CHANGELOG.md`.
16. Sections appear in this order and are omitted when empty:
    `Before you upgrade`, `New`, `Improvements`, `Fixes`,
    `For extension authors`. A release with a theme may add one further section
    after `New`, named for the theme.
17. `For extension authors` carries every `[sdk]` entry of that version and
    holds nothing else, so a self-hoster skips one heading and skips every line
    that is not for them.
18. A bullet is one present-tense sentence whose grammatical subject is the
    reader's system. It names no commit, no pull request, no ticket, no
    contributor and no repository path, and a release note holds no table.
19. A lede appears only where the release has a headline. Ten of the eleven
    sampled Obsidian notes open straight into a section.
20. Category headings under one release heading come from the list in
    `changelog-discipline.md` and appear at most once each, in the order the
    rule states. `scripts/cut-release-changelog.sh` writes them that way, once
    per category, from every fragment.

## the tier of every published page

Earlier research left twelve assignments arguable and named `glossary.md` and
`error-codes.md` as the two that most need settling. Each is settled here by
rule 2 and nothing else: what may the page assume, and what does its body
already name.

| page | tier | why this one |
|---|---|---|
| `glossary.md` | `build` | 93 repository paths in its body, 55 of them under `internal/` and 21 under `sdk/`, and one of its three groups defines Go concepts. A page that has to explain `goroutine` is written for somebody reading Go. Earlier research filed it `extend` in section 1 and grouped it with the `build` pages in section 5, and this record settles the contradiction in favour of the measurement |
| `error-codes.md` | `run` | a self-hoster holds a `code` out of a 409 and needs the sentence next to it. Its body names no repository path at all, so it passes rule 3 unchanged. `sdk/fault` stays in `implementedBy` under rule 4, and the author registering a code reads `extensions.md` |
| `storage.md` | `run`, split | it documents a volume an operator mounts and the `sdk/storageapi` contract a driver author implements. Rule 5 makes that two pages |
| `human-auth.md` | `run`, split | the same shape over `sdk/authapi` |
| `logging.md`, `metrics.md`, `tracing.md` | `run` | an operator turns these on in a config file. Each names one path under `legacy/` and each is `describes: legacy`, so rule 7 baselines the path and the retirement removes it |
| `sse-overview.md`, `sse-asyncapi.mdx`, `mqtt-overview.md`, `mqtt-asyncapi.mdx` | `run` | somebody writing a client against the stream needs an event name, a topic and a URL, and no Go. Earlier research filed all four as `extend`, and nothing in any of them requires the sdk |
| `prusa-xbuddy-websocket-protocol.md` | `extend` | a wire protocol is what a driver author implements against. Its one `legacy/` path is in the frontmatter, so it passes rule 3 today |
| `emulator.md` | `extend` | it is how a driver is exercised without a printer. It names only `tools/` and passes rule 3 today |
| `developer-view.md` | `extend` | the reader is a driver author watching frames arrive. Its six paths under `internal/transport/devtools/` are the page describing its own implementation, and they go |
| `capabilities.md`, `resource-kinds.md`, `event-types.md`, `authorization-vocabulary.md`, `extensions.md` | `extend` | each is a vocabulary a third party registers into. Rule 6 puts the fix in their generators |
| `connector-contract.md` | `extend` | 17 `internal/` paths against 9 under `sdk/`. The reader compiles against the second set, and the 17 `internal/` paths are what the page owes a rewrite |
| `architecture.md`, `prusa-command-architecture.md`, `creating-connectors.md` | `build` | all three are `describes: legacy` or `describes: superseded`, and two of them sit one sidebar click from container deployment. A reader-facing tier does not hold a design nothing runs |

That gives 16 `run`, 11 `extend` and 18 `build`, against earlier research's 12, 16
and 17. The four stream pages and the protocol page account for the movement out
of `extend`, and `glossary.md` accounts for the movement into `build`.

### The pages the table above does not name

The tree carries 54 published pages now, not 45. The table above settled the
twelve assignments earlier research left arguable, and the rest follow from rule 2
alone. They are listed here so a reader can check the tag on a page without
reading its body. This applies rule 2 and changes no rule.

| page | tier | why this one |
|---|---|---|
| `certificates.md`, `config.md`, `device-issues.md`, `printer-configuration.md` | `run` | each is a thing an operator does in a file or on the printer, with no Go |
| `container.md`, `getting-started.md`, `kubernetes.md`, `local-accounts.md` | `run` | all four carried the tag already |
| `extension-concepts.md`, `plugin-architecture.md`, `plugin-wire-protocol.md` | `extend` | each is written for somebody choosing a runtime and compiling against `sdk/` |
| `writing-a-driver.md`, `writing-a-module.md` | `extend` | the two end-to-end guides for an author outside this repository |
| the seven `internals/` chapters | `build` | each is organised file by file under `internal/`, with a line number per symbol |
| the eight `onboarding/` pages | `build` | they exist to take a new developer through this tree |
| `bench-preflight.md` | `build` | our bench, and it names the script that runs it |
| `config-retired-server.md` | `build` | `describes: legacy`, which is the reason `architecture.md` moved |

The tree now carries 19 `run`, 14 `extend` and 21 `build`.

## enforcement

Rules 1, 3, 4, 7 and 8 are built and the rest is owed. The state column marks what exists and the cost column is
what makes this record adoptable in pieces: the hours rows can ship on their own
and each is useful without the ones below it.

| rule | gate | state | cost |
|---|---|---|---|
| 1 | the `audience` enum in `docs/src/content.config.ts`, beside `rewrite` | exists. All 54 pages carry a tier, and the build refuses a page without one | - |
| 2 | none. The table is what a reviewer points at, and no check reads a page's assumptions | none, and honestly so | - |
| 3, 7, 8 | `TestNoPageNamesAPathAboveItsTier` in `internal/arch/tier_paths_test.go`, two counters over `scripts/tier-paths.baseline` | exists. 20 pages carry a line: 11 `legacy/` paths and 63 above-tier | - |
| 4 | `TestEveryImplementedByPathResolves` in `internal/arch/published_docs_test.go`, over the frontmatter of every page the directory holds | exists. 153 paths on 54 pages, and `TestEveryPublishedPageNamesWhatImplementsIt` is the floor under it | - |
| 5 | none. A page serving two readers is a review judgement, and the enum in rule 1 is what makes the second tag impossible | none | - |
| 6 | the same tier check, plus each generator's existing `-check` target, which ends in `git diff --exit-code` and already refuses an uncommitted regeneration | owed for five of the six generators | days |
| 9, 10 | `make docs-links-check`, which already refuses a link that fails for either reader and refuses a link the build did not rewrite | exists. It is what makes rule 9 cheap to keep | - |
| 11 | none. Routing is prose | none | - |
| 12, 13 | `scripts/release.sh` refuses to tag `v<x>` when `docs/src/content/docs/releases/<x>.md` is absent; `.github/workflows/release.yml` reads the published URL instead of the constant at line 192 | owed | hours |
| 14 | `make changelog-check`, extending the trailer-to-entry scan in `scripts/check-changelog.sh` so every note bullet is found inside an entry under the matching version heading | owed. The script exists and the target is wired | days |
| 15 | `scripts/cut-release-changelog.sh` files the fragments, and `scripts/check-release-changelog.sh` refuses a tag while `docs/changes/` holds one or the newest `##` heading does not match `VERSION` | exists | - |
| 16, 17, 19 | none. Section choice, section order within what rule 16 allows, and whether a release has a headline are all editorial | none, and deliberately | - |
| 18 | none for the prose. `make style-check` already reads the file for em dashes, repeats and the four density budgets, because a release note is a markdown file under `docs/` like any other | partial, by inheritance | - |
| 20 | `make changelog-check`. A heading outside the list, or one appearing twice under one release heading | owed. It fires on the tree today | hours |

Deliberately outside every gate: note length, bullet count, which entries were
selected, the section names, and link liveness. The first two are the signal
that a release was small, the third is the editorial act a note exists for, the
fourth is already held by `writing-style.md` and ADR-20260826-gtb rule 6, and a network
call inside a gate makes an offline commit fail.

## rejected

Each heading below is the option, unchanged, so a reviewer can link to the
argument rather than repeat it.

### a documentation site per tier, or a domain per tier

Earlier research costs it: one link-rewrite plugin per build, a link
checker with three content roots, and three Astro builds. It also allows a
cross-tier link that resolves in the vault and 404s on the site. OctoPrint runs
four properties, and its technical site still carries the apology, years old.

### the four Diátaxis names as top-level navigation

Its own author writes "Don't do that. It's horrible" about adopting it whole.
Django does not do it, and Diátaxis came out of Django. The loudest unprompted
complaint is the click it puts in front of the API reference. Earlier research
rejected the same thing for the same reason.

### abandon Diátaxis

It is right about what a reference page is, and ADR-20260826-swm already depends on its
approval of generated reference. Rule 10 keeps it where the endorsements put
it, on the page.

### a `docs/community/` tree outside the site

A directory the site does not build is a directory no link check and no table
check reads.

### tier by subject, so every storage page is one tier

Storage appears in all three and so does authorization. Subject is the axis the
sidebar already uses, and it is why `architecture.md` sits between container
deployment and human authentication today.

### a second `audience` value on a page two tiers read

It disarms the gate on exactly the pages most likely to drift. `describes:
mixed` already demonstrates the failure: 11 of 45 pages carry it, and nothing
can be checked about any of them.

### tier `glossary.md` as `extend` and strip its `internal/` paths

Its Go-concepts group is the half a `run` reader most needs, and the half that
proves the page assumes Go. Stripping the definition sites costs the `build`
reader the only thing the page is precise about.

### a second glossary for the `run` tier

A second copy of 60 definitions, half-maintained inside a year. The `run` pages
define their own terms at first use instead.

### ship the tier check as a warning until the pages are fixed

ADR-20260826-gtb rule 9 is explicit that a warning is how a gate dies, and the same
record already ratchets 9 prose checks over this tree.

### one counter for every path above the tier

It merges the retirement of the old server with tier drift. Nine of the 17 failures are
`legacy/` paths that disappear when somebody else deletes a file, and a single
number would credit that deletion as documentation work.

### gate `implementedBy` by tier along with the body

ADR-20260824-dym requires a `run` page describing legacy behaviour to name `legacy/`.
The gate would forbid the record the other ADR mandates.

### generate the release note from `docs/CHANGELOG.md`

The entries carry no ordering signal and run 46 words at the median. Choosing
four of forty is the act the note exists for, and a generator cannot make it.

### generate it from commit subjects

Earlier research measured about 40% chores, tests and docs in real commit-derived
output, and `google-cloud-go` ships the entry "Update API sources and
regenerate".

### write the note fresh instead of assembling it

It is a second description of behaviour already described, in a second place,
going stale on its own schedule. Assembly costs the same time minus the
research.

### keep the note only in the GitHub release body

It cannot be corrected without editing a published release, the vault reader
cannot see it, and `make style-check` does not read it.

### Obsidian's section names

`Shiny new things` and `No longer broken` are the voice of a note-taking app.
This reader is running a print farm. `writing-style.md` requires sentence case,
and ADR-20260826-gtb rule 6 requires a heading to name what is under it.

### Obsidian's ordering, which opens with the new features

`changelog-discipline.md` orders its own categories by impact with `Security`
first, and earlier research picked the same for the same reason. A self-hoster's
first question is whether this upgrade needs anything of them, so
`Before you upgrade` leads.

### one section per changelog category, so the note mirrors the file

The note becomes a shorter changelog, and the themed section that makes a
release read as one thing has nowhere to go. Rule 14 traces per bullet, so the
sections are free.

### a second changelog file for extension authors

`changelog-discipline.md` rejected it already: at this volume both are
half-maintained, and `[sdk]` is where the file splits when `sdk/` tags v1.
Rule 17 makes that marker a section in the note and nothing more.

### gate release-note length or wording

A one-fix release should produce a one-sentence note, and a checker that passes
bad prose approves it.

### adopt swyx's documentation levels instead

It is a maturity ladder, and its own level 5 is where audience splitting
arrives. `VERSION` is `0.1.0` with one tag, which makes it the honest answer
about cost and not a structure.

## open items

- The tier gate has no baseline file and no test. Until both exist, rules 3, 6
  and 7 are prose, and the 17 failing pages are recorded here and nowhere a
  build can see.
- `internal/arch/onboarding_docs_test.go` carries a hard-coded path floor per
  page, from 18 to 130. Rule 3 brings ten more `build` pages under the same
  check and therefore ten more numbers that a page rewrite has to edit. That is
  the price of the floor and it is the right price.
- The forty-line estimate earlier research gives for the tier check is untested. It
  assumes `pathsIn` generalises without change, which is likely, since it
  already reads paths out of prose, tables and diagrams alike.
- `docs/src/content/docs/plugin-architecture.md` carries no `rewrite` block at
  all, so nothing records whether it describes what ships. Rule 1 adds an
  `audience` line to it; making `rewrite` required is a separate edit to the
  same schema and it is on the old backlog.
- Rule 5 splits `storage.md` and `human-auth.md`, and neither split has an owner
  or a target page. The `extend` half of each has no natural home until the
  sidebar is reorganised.
- No release note exists and `v0.1.0` is already tagged. The first note is
  written for the next tag, and nothing backfills the one that shipped, on
  `changelog-discipline.md` rule 12's principle that a discipline starts where
  it starts.
- Rule 16's five section names have never been used. They are chosen against
  Obsidian's measured structure and this project's reader, and the first two
  releases are what will show whether `Before you upgrade` is empty every time.
- `scripts/release.sh` runs `git commit` and `git tag` with no check of any kind
  in front of them. Rules 12 and 15 both put a refusal there, which makes that
  script a gate for the first time and gives it a failure path it has never had.
- The sidebar labels in `docs/astro.config.mjs` are title case, which
  `writing-style.md` forbids and no check reads. Rule 10 edits that file anyway.
- `error-codes.md` is tiered `run` and sits in the reference group beside five
  `extend` pages. Rule 10's sidebar has three tiers plus reference, so a
  reference page's group and its `audience` disagree by construction, and the
  gate reads only the second.
