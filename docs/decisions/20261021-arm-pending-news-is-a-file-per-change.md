---
title: "ADR-20261021-arm: pending news is a file per change"
description: "A change a user observes writes its changelog entry as one fragment file in docs/changes/, named by the time it was written. A release files every fragment into docs/CHANGELOG.md and deletes it, so no two pull requests edit the same lines of the news."
date: 2026-10-21
status: accepted
amends: [ADR-20260826-53y]
tags:
  - adr
  - changelog
  - release
  - gates
---
# ADR-20261021-arm: pending news is a file per change

- status: accepted
- date: 2026-10-21

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends: [ADR-20260826-53y](20260826-53y-documentation-tiers-and-release-notes.md) rules 15 and 20 | Rules: [changelog-discipline](../../.agents/rules/changelog-discipline.md)

Every change that a user can observe wrote its entry under one `## Unreleased`
heading in `docs/CHANGELOG.md`. Two open pull requests then edited the same
lines. Whichever merged first left the other in conflict. That pull request
needed a rebase and a full CI run again, tens of minutes each time, for a
change that did not touch its code. Now a change writes its entry as a file of
its own. A release files the files into the changelog. No two changes edit
one line of the news, so the news never conflicts.

## rules

### the fragment

1. A pull request with a change that a self-hoster or an extension author can
   observe adds one file to `docs/changes/`. The file is a fragment. `docs/changes/README.md`
   says how one is written and is no fragment itself.
2. A fragment is named `<YYYYMMDD>-<HHMMSS>-<words>.md`. The date and time are
   in UTC, from when it was written. The words are lowercase letters and
   digits with one hyphen between each run. Two pull requests therefore never
   pick one name, and the names sort in the order the fragments were written.
3. `make changelog-new NAME=<words> CATEGORY=<category>` writes the fragment
   with the time it is now. `scripts/new-change.sh` does it.
4. A fragment holds one or more `### <category>` headings, each with at least
   one bullet under it. The categories are the six of
   [changelog-discipline](../../.agents/rules/changelog-discipline.md):
   Security, Removed, Changed, Deprecated, Added and Fixed. It holds no other
   heading and no text before its first category.
5. An entry is written as changelog-discipline says. Its first sentence is the
   commit's `Changelog:` trailer, and that rule does not change.
6. One pull request writes one fragment, holding every entry it ships,
   however many commits it takes. The first commit with an entry adds the
   fragment. A later commit in the same pull request adds its entry to that
   fragment, or rewords one there, and adds no fragment of its own. A pull
   request whose commits all carry `Changelog: none` writes no fragment.
7. A relative link in a fragment starts with `../`, because the fragment sits
   one folder below the changelog. The release takes one `../` off each such
   link outside a code sample, so the link resolves before and after.

### the changelog

8. `docs/CHANGELOG.md` holds released versions only. It carries no
   `## Unreleased` heading. Its introduction links `docs/changes/`.
9. Only a release adds a version section to `docs/CHANGELOG.md`. A change may
   still correct the wording of a released entry.

### the release

10. `scripts/cut-release-changelog.sh <version>` files every fragment under a
   new `## <version> - <date>` heading. It puts each category once, in the
   order of rule 4. Within a category the entries come in the order of the
   fragment names. It then deletes every fragment it filed.
11. It refuses a release with no entry, a version that already has a heading,
    and a fragment that breaks rule 2, 4 or 7. It files nothing then.
12. With `PREVIEW=1` it prints the section and changes nothing.
    `make changelog-preview` runs it that way.
13. The commit that files the fragments also deletes them.
    `.github/workflows/cut-release.yml` stages `docs/CHANGELOG.md` and
    `docs/changes` together.
14. `scripts/check-release-changelog.sh` refuses a tag while `docs/changes/`
    holds a fragment, while `docs/CHANGELOG.md` carries `## Unreleased`, and
    while the newest version heading is not the version being tagged.

### the gates

15. `scripts/check-changelog.sh` looks for a trailer's sentence in what the
    whole push or pull request adds to `docs/changes/` and
    `docs/CHANGELOG.md`, and not in the commit that carries the trailer. A
    commit therefore needs no fragment of its own, and the one fragment of
    rule 6 backs every trailer of its pull request. The changelog is read
    because a release files the fragments there in the same push that deletes
    them.
16. `scripts/check-changes.sh` refuses a fragment that breaks rule 2, 4 or 7, and
    a `## Unreleased` heading in `docs/CHANGELOG.md`. `make changelog-check`
    runs it on every push and in CI, through `scripts/check-changelog.sh`,
    before any answer that ends that gate early.

### what stays

17. The `Changelog:` trailer, `Changelog: none`, `scripts/changelog.exceptions`
    and every rule on how an entry is written stay as they are.
18. An extension under `extensions/<name>/` keeps its own `CHANGELOG.md`, as
    changelog-discipline rule 14 says. This record covers `docs/CHANGELOG.md`
    alone.

## enforcement

| rule | gate | state |
|---|---|---|
| 2, 4, 7, 8, 16 | `TestTheChangesGateRefusesWhatAReleaseCannotFile`, `internal/arch`, and `scripts/check-changes.sh` in `make changelog-check` | exists |
| 7, 10, 11 | `TestTheChangelogCutFilesEveryFragment` and `TestTheChangelogCutRefusesAReleaseWithNoNews`, `internal/arch` | exists |
| 12 | `TestTheChangelogPreviewChangesNothing`, `internal/arch` | exists |
| 13 | `TestTheReleaseWorkflowCommitsTheDeletedFragments`, `internal/arch` | exists |
| 14 | `TestTheReleaseGateReadsTheChangelogItIsGiven`, `internal/arch` | exists |
| 15 | `TestAFragmentIsWhereATrailerFindsItsEntry`, `internal/arch`, over real commits | exists |
| 1, 3, 5, 6, 9 | none. Review holds them | none |

## rejected

| option | why not |
|---|---|
| one `## Unreleased` section that every change edits | every two open pull requests conflicted in it, and each merge cost the others a rebase and a CI run |
| the entries generated from the `Changelog:` trailers at release | a trailer is frozen in history, so a wrong entry could never be reworded |
| `merge=union` for `docs/CHANGELOG.md` in `.gitattributes` | GitHub ignores a merge driver, so a pull request still shows the conflict |
| the pull request number in the fragment's name | the number exists only once the pull request is open, and a fragment is written with the commit before that |
| a fragment per category | one change with an entry in two categories would need two files, and rule 6 would break |
| a fragment per commit | it names the steps of the work, not what the release ships, and fills the folder |
| a gate that counts the fragments of a pull request | a pull request may carry two changes that are worth two fragments, and only a person can tell which it is |

## open items

- nothing checks that a pull request wrote one fragment and not two. Review
  holds rule 6
