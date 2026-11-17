# Changelog discipline

`docs/CHANGELOG.md` is read by a stranger deciding whether to upgrade. It is not
a record of work done. Commit mechanics are in [git-discipline.md](git-discipline.md).

A trailer, named throughout this file, is a `Key: value` line at the end of a
commit message.

## Rules

1. decide by user visibility, never by commit prefix. The question is what a
   self-hoster or an extension author now observes, or now has to do differently
2. every commit with a bump-worthy prefix (`feat`, `fix`, `perf`, `schema`,
   `infra`) carries a `Changelog:` trailer. Bump-worthy means the change would
   move the version number. No exemption, no exception
3. a commit with any other prefix carries one too when the change is
   user-visible. A `refactor:` that moves an exported package is user-visible
4. the trailer value is the entry's first sentence, in prose. Not a category
   keyword
5. `Changelog: none` is the trailer for a change nothing outside the repository
   can observe. It is the only value that ships without an entry
6. an entry that is not `none` is a fragment in `docs/changes/`, staged in the
   same commit as the code. Never as a follow-up, and never in
   `docs/CHANGELOG.md`, which only a release writes. See
   [the pending news](#the-pending-news) and ADR-20261021-arm
7. one entry per user-visible behaviour, not one per commit and not one per file
8. an ordinary entry is at most two sentences. A breaking change may run longer
   and states the old behaviour, the new behaviour and the migration
9. write the entry from the reader's system, not from the diff. Name the API
   path, config key, flag or observed behaviour. Never an internal symbol
10. an entry that affects `sdk/` starts with `[sdk]`. That token is rare on
    purpose and is the only emphasis this file has
11. never link a ticket, name a person, or reference a conversation. There is no
    shared context with the reader
12. do not add entries for past commits. The file starts where the discipline
    starts
13. a commit whose trailer and entry can never agree is recorded in
    `scripts/changelog.exceptions`, with the full sha, which of the two is at
    fault, and why. Only these reasons qualify. The commit carries no
    trailer, and a neighbouring commit staged its entry. Or the trailer no
    longer describes what ships, so writing it into the file would publish
    something untrue. Rewording the entry is the repair. A line here is what is
    left when a frozen trailer makes the repair impossible. An entry that a
    later commit in the same range rewords needs no line, because the gate
    reads every commit's additions. Never add a line for a commit on an
    unmerged branch. A squash or rebase merge deletes that sha, and the line
    then fails on `main` and on every later pull request
14. a change under `extensions/<name>/` is recorded in that extension's own
    `CHANGELOG.md`, in the same commit. The core does not ship the extension,
    so the change has no entry in `docs/CHANGELOG.md`. The commit carries
    `Changelog: none` unless it also changes the core

## What earns an entry

| change | entry |
|---|---|
| behaviour a self-hoster observes at runtime | yes |
| new, changed or removed API path, event, config key, CLI flag, env var | yes |
| a change to anything exported from `sdk/` | yes, `[sdk]` |
| container, volume, port or permission an operator must act on | yes |
| security fix, in any layer | yes |
| dependency bump carrying a CVE | yes |
| internal package, refactor, rename, file move under `internal/` | `none` |
| test, CI, build, lint, tooling, emulator fix | `none` |
| documentation, comment, ADR, rule | `none` |
| dependency bump with no behaviour change | `none` |

The trailer is still mandatory in the `none` rows. Deciding is the work; the
trailer records that the decision was taken.

## The pending news

News that no release has shipped yet is one file per change in
`docs/changes/` (ADR-20261021-arm). Every pull request used to write under one
`## Unreleased` heading, and each conflicted with every other one there. Each
merge then sent the open pull requests back to a rebase and a full CI run. A change that adds a
new file edits no line another change edits.

- `make changelog-new NAME=<words> CATEGORY=<category>` writes the file. The
  name is `<YYYYMMDD>-<HHMMSS>-<words>.md`, the date and time in UTC when it
  was written, so two pull requests never pick one name
- the file holds one or more `### <category>` headings from the list below,
  with the bullets under each. No other heading and no text before the first
- one pull request writes one file, with every entry it ships, however many
  commits it takes. A later commit adds its entry to that file, or rewords one
  there, and adds no file of its own. A pull request whose commits all carry
  `Changelog: none` writes none
- a relative link starts with `../`, as the fragment sits one folder below the
  changelog. The release takes one `../` off when it files the entry
- `make changelog-preview` prints the section the next release would file
- `scripts/cut-release-changelog.sh <version>`, which
  `.github/workflows/cut-release.yml` runs, files every fragment under
  `## <version> - <date>` in `docs/CHANGELOG.md`, grouped by category and in
  the order of the file names, and deletes the fragments in the same commit

## Format

Categories in this order, highest impact first. Omit the ones with nothing under
them. A fragment uses the same headings, and a release writes the version
heading above them.

```markdown
## 0.0.4 - 2026-08-24

### Security
### Removed
### Changed
### Deprecated
### Added
### Fixed

- One bullet per behaviour, sentence case, prose, no trailing period unless the
  bullet is more than one sentence.
```

| element | rule |
|---|---|
| release heading | `## 0.0.4 - 2026-08-24`, newest first, ISO 8601 date |
| prerelease heading | `## 0.0.4-alpha.1 - 2026-08-24` or `-beta.N`, one section per alpha and beta |
| pending | a fragment in `docs/changes/`, never a `## Unreleased` section in `docs/CHANGELOG.md` |
| bullet | prose, no prefix, no ticket id, no sha |
| `[sdk]` | leading token on any entry an extension author must read |
| withdrawn release | `## 0.2.0 - 2026-08-24 [YANKED]`, kept in place |

A stable `## 0.0.4` holds only what came after its last beta. `VERSION` stays
`0.0.4` through the whole cycle; the label is the tag's.

## Writing an entry a stranger can read

Both examples are the same real change: `3eba871 fix(storage): keep the existing
file when an upload fails partway`.

Bad, because it restates the diff and hides the one line that matters under
lines that do not:

```markdown
### Fixed
- storage: `Save` now writes through `os.CreateTemp` in the destination
  directory instead of opening the destination with O_TRUNC
- storage: the deferred `Close` is replaced with an explicit close whose error
  is checked
- storage: the temporary file shares the destination's directory so the rename
  cannot fail with EXDEV
- storage: three tests added, one mutation-checked
- storage: the case joins the `storageapitest` conformance suite
```

Good, and this is what shipped:

```markdown
### Fixed
- An upload that failed partway destroyed the file it was replacing and left a
  truncated file in its place. Writes now land under a temporary name and are
  renamed into place only once the whole transfer has been flushed to disk, so
  an interrupted upload leaves the previous file untouched.
```

Old behaviour, new behaviour, what the reader now observes. No symbol names, no
test count, no reasons. The reasons belong in the commit body, which for this
commit is eight paragraphs long and correctly stayed there.

The test: **delete every noun that only exists inside this repository. If the
entry stops meaning anything, rewrite it.**

## Operators and extension authors

| | operator | extension author |
|---|---|---|
| upgrades | the binary, image or compose stack | the `sdk` module import |
| finds a break | at redeploy | at compile |
| needs named | endpoint, config key, env var, volume, port, uid | package, import path, exported symbol, signature |
| marker | none | `[sdk]` |

One file until `sdk/` tags v1, then the marker is where the file splits.

A breaking change to `sdk/` names both sides. From `6b70903 refactor: move the
public contracts into a separate sdk module`:

```markdown
### Changed
- [sdk] Extension contracts moved out of the application module. An extension
  importing `github.com/prusa3d/prusa-connect/storageapi` changes the
  import to `github.com/prusa3d/prusa-connect/sdk/storageapi`. No
  contract, name or behaviour changed.
```

Both modules are `0.x`, so the version number promises nothing and there is no
deprecation cycle to make a break easier. The entry is the whole migration
document.

## Enforcement gate

`make changelog-check`, run by `.githooks/pre-push` and by CI, on the model of
`make error-codes-check`. The `commit-msg` hook runs the first check alone, at
commit time, because the question it forces can only be answered while the
change is being written. Each check fails, none warns.

The comparison drops code-span markers from both sides before it looks for the
trailer in the file. A trailer lives in a commit message, where a backtick is a
backtick, and an entry lives in markdown, where the same words carry code
spans. A word that differs still fails.

The trailer is looked for in two places. One is the lines the range adds net.
The other is the lines any commit in the range added to a news file the range
leaves added or changed. An
entry a later commit rewords still backs the earlier trailer. A fragment the
range deletes backs nothing.

| checked mechanically | left to review |
|---|---|
| a bump-worthy commit has a `Changelog:` trailer | whether `none` was the honest answer |
| a trailer that is not `none` has its sentence among the lines the range's commits add to a surviving fragment | whether the entry is readable by a stranger |
| a fragment's name, headings and entries, and no `## Unreleased` heading (`scripts/check-changes.sh`) | whether one fragment should have been two |
| every line of `scripts/changelog.exceptions` still describes its commit | whether the exception was the honest answer |
| | entry length and tone |
| | whether `[sdk]` was needed |
| | whether one entry should have been two |

`scripts/check-release-changelog.sh`, run by `scripts/release.sh` before it
writes anything. It refuses a release while `docs/changes/` holds a fragment,
and when the newest version heading is not the version being tagged.
`internal/arch/release_changelog_test.go` runs the script over fixtures.

Link liveness is never checked. A network call in a gate makes an offline commit
fail, and a dead link is not a reason to block a fix.

## Rejected

| option | why not |
|---|---|
| conventional-changelog, release-please or git-cliff | they publish commit subjects. Real output measured at ~40% chores, tests and docs; `google-cloud-go` ships the entry "Update API sources and regenerate" |
| GitHub auto-generated release notes | it is a list of merged pull requests, not a changelog |
| one `## Unreleased` section that every change edits | each merge left the open pull requests in conflict there. ADR-20261021-arm |
| the entries generated from the `Changelog:` trailers at release | a trailer is frozen in history, so a wrong entry could never be reworded |
| `merge=union` for `docs/CHANGELOG.md` in `.gitattributes` | GitHub ignores a merge driver, so a pull request still shows the conflict |
| category keyword trailers (`Changelog: fixed`) | the category is already the `###` heading. A prose trailer is the entry, so drift between trailer and file can be detected |
| exempting non-visible commits from the trailer | the exemption is where the rule dies. `Changelog: none` keeps the gate binary |
| two files, one per audience | at this volume both would be half-maintained. Revisit when `sdk/` tags v1 |
| Keep a Changelog's category order | it is roughly alphabetical, not ordered by impact. A reader scans from the top |
| bold, emoji or a prefix on every entry | emphasis on every entry is emphasis on none of them |
| gating entry length or wording | a checker that passes bad prose approves bad prose |

## Open items

- `scripts/release.sh` does not edit `docs/CHANGELOG.md`. A release cut by
  hand runs `scripts/cut-release-changelog.sh <version>` and commits the
  changelog and the deleted fragments before the release (`make release-alpha`,
  `release-beta` or `release-stable`). `.github/workflows/cut-release.yml`
  does both
- a range reaching past a release judges its trailers against
  `docs/CHANGELOG.md`, where the release filed the reworded entry and not the
  first wording. That is what `scripts/changelog.exceptions` absorbs
- bump-worthy commits on `main` older than this rule carry no trailer, and
  they stay that way
