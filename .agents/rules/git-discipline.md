# Git discipline

How a commit is shaped, when to commit, how several agents share one branch,
and what to check before a push.

## Commit prefixes

| prefix | when |
|---|---|
| `checkpoint:` | before starting work or anything risky. A rollback target |
| `feat:` | new functionality |
| `fix:` | bug fix |
| `refactor:` | restructure, no behaviour change |
| `wip:` | pausing mid-work |
| `docs:` | documentation only |
| `style:` | formatting, no logic |
| `perf:` | performance |
| `test:` | tests only |
| `infra:` | docker, ci, config |
| `schema:` | migration or schema change |
| `chore:` | deps, lockfile, generated files |

Format: `prefix: lowercase imperative, no period`. An optional scope goes in brackets: `feat(api): add rate limit headers`.

## The prefix is not the message

The prefix is a tag for tooling. It does not excuse a lazy subject or a missing body.

- the **subject** states what changed in one imperative sentence, at most 72 characters, no period
- the **body** explains why: the constraint that forced the change, the alternative rejected, the incident, the benchmark. The diff already shows what
- a body is required for any schema change, any infra change, any fix that is not obvious, and any behaviour change a teammate would want context for
- a body that would restate the subject should not be written. Rewrite the subject instead

How long the body may be, what belongs in it, and the voice it is written in are
in [what a body may say](#what-a-body-may-say). A script checks those rules.

Bad:

```
fix: null check
```

Good:

```
fix: null check in upload handler when payload is empty

Requests to /v1/upload with a zero-byte body returned 500 instead of 400.
The handler assumed the multipart form always had a file field; it now
returns a validated error upstream of the parser.
```

## What a body may say

The rules below come from measuring the commit bodies here against golang/go
and other control projects. The contrast frame named
in rule 10 is the "X, not Y" sentence shape.

1. A body runs to at most **20 lines of prose**. Nine in ten of golang/go's
   recent bodies are shorter than that.
2. Pasted evidence does not count against rule 1. Indent it by four spaces or
   put it in a fence. A gate going red, a benchmark, a stack trace or a diff is
   what a reader cannot reproduce, so it earns the room. The body Dependabot
   writes is release notes it pasted, so a `dependabot[bot]` commit is judged on
   its subject alone.
3. A body says why this change and why now. The diff says what.
4. Each record has one home, and it is the split
   [ADR-20260826-zbh](../../docs/decisions/20260826-zbh-a-comment-outlives-the-request-that-produced-it.md)
   rule 2 already settled. See [where each record goes](#where-each-record-goes).
5. A body that names an ADR, a changelog entry or an issue
   links to it and does not restate it.
6. A body may name the request that produced the change. It is the only record
   that may. ADR-20260826-zbh refuses that in a comment because a source file has no
   requester; git holds the author and the date, so the moment belongs here.
7. A subject runs to at most **72 characters**, imperative, no closing period.
   Move the reason into the body. The ` (#123)` a squash merge on GitHub
   appends is not counted.
8. A subject **begins at its first character** with a prefix from the table
   above, an optional scope, then a colon and a space. Nothing may stand in
   front of it. A leading character, such as the `@` a PowerShell here-string
   opener leaves, hides the prefix from `.githooks/commit-msg`, which matches
   from the first character. The commit then skips the changelog gate and
   carries no `Changelog:` trailer. A trailer is a `Key: value` line at the
   end of a commit message. The subjects `Merge`, `Revert`, `fixup!` and
   `squash!` are git's own and are allowed.
9. No em dash in a subject, a body or a trailer. `writing-style.md` refuses
   one in a commit message, and check 1 reads the message for it.
10. The contrast frame is budgeted across commits and never inside one. At most
   **6.00 per thousand words** over the **last 50 commits**, which is the budget
   [ADR-20260826-gtb](../../docs/decisions/20260826-gtb-writing-that-reads-as-written.md) rule 1
   sets for the same construction in a markdown file. Under **800 prose words**
   the window is counted and nothing is refused.
11. Every message already written stays exactly as it is. The gate starts at the
    commit that added it and judges nothing before. Rewriting a message rewrites
    every commit after it, and this is a shared branch.
12. A `fix:` commit names the commit that introduced the defect, in a `Fixes:`
    trailer:

    ```
    Fixes: 8443b06e21c1 ("feat(gates): read the refs git hands the pre-push hook")
    ```

    The value is a twelve-character short sha and that commit's subject, or
    `none`. `none` is a real answer, and the right one where the defect was
    there in the first version of the thing.

    [ADR-20260831-wab](../../docs/decisions/20260831-wab-a-finding-has-one-of-four-homes.md)
    rule 7 states why. A class of failure is counted by a query over trailers
    and never by a person.

### Where each record goes

| the thing you want to write | its home |
|---|---|
| why this change, and why now | the commit body |
| what a self-hoster or an extension author now observes | a fragment in `docs/changes/`, which the release files into `docs/CHANGELOG.md` |
| the argument for a design, and the options it beat | an ADR in `docs/decisions/` |
| a measurement, a corpus, a command that produced a number | the ADR it supports, or the pull request description |
| work the change leaves owed, when it is worth tracking | a GitHub issue |
| what the code does and why it does it | a comment beside the code |

The body links to each of those and does not repeat them. A commit that settles
something still records it in the same change, which is the section below.

### Habits found in real bodies

The markers are the measurable half. Reading a sample of a hundred bodies
found these habits, with the count of each.

| habit | count | what to write instead |
|---|---|---|
| the body summarises the ADR the same commit adds | 49 | link to it and say why it lands now |
| the body narrates the process: what was probed, what came back green | 20 | the finding, once. The probe belongs in the record that owns it |
| `which is the`, `which is why`, `which is exactly` | 25 | say the thing. The relative clause carries no extra fact |
| a measurement restated from the note that holds it | 12 | the one number that decides the change, and a link |
| a closing paragraph reassuring the reader that nothing broke | 2 | delete it. The gates say that |

None of these is gated, and no word list is proposed for any of them. An
earlier study scored this corpus on the published list of machine-writing words.
It came out cleaner than the human corpora measured beside it, so a word list
cannot find this. A reviewer holds this half, through the writing lens in
[adversarial-review](adversarial-review.md).

### Enforcement

`make commit-style-check`, run by the `commit-msg` hook (a script git runs
when a commit is made) on the message being written, by `.githooks/pre-push`,
and by CI. Each check fails, none warns.

| rule | gate |
|---|---|
| 1, 2 | check 3, prose lines in the body, with a fenced or four-space-indented block and a `dependabot[bot]` body not counted |
| 7 | check 2, subject characters, counted as characters and not bytes |
| 8 | check 5, the prefix table anchored at the subject's first character |
| 9 | check 1, the U+2014 byte sequence anywhere in the message |
| 10 | check 4, the frame over the window, in range mode only |
| 11 | `FLOOR` and `ATTRIB_FLOOR` in `scripts/check-commit-style.sh`, and `internal/arch/commit_style_test.go` refuses an empty one |
| [Attribution](#attribution) | check 6, a `Co-Authored-By:` line anywhere in the message |
| 12 | check 7, a `fix:` subject with no `Fixes:` trailer, or a value that is neither the null nor a sha and a subject |
| 3, 4, 5, 6 | none. Which record an argument belongs in is a judgement over the argument |

Every threshold above is a hard cap and none is a ratchet. A ratchet is a
limit that may only move one way, and it needs a previous value for the thing
it judges; a message is written once. The correction to a recorded value
would also be a commit, whose body moves the rate the value was just set to.

`internal/arch/commit_style_test.go` holds the wiring: the script exists, all
four callers name it, and every check this section credits is named in the
script's own header. A gate nothing calls is the way this class of check dies.

### Rejected

| option | why not |
|---|---|
| a banned word or phrase list | an earlier study measured it. This corpus scores cleaner than three human corpora on the published list |
| a cap on the contrast frame inside one body | median 1, p90 3. A cap at 4 flags 1% of the last hundred commits and sees nothing |
| a rate of any kind on one body | the median body is 186 prose words. ADR-20260826-gtb rule 1 puts its own floor at 800 |
| a cap on sentence length | 26% of sentences here are over 30 words, and no control is clean enough to say whether that is out of band |
| a ratchet on any of it | the correction to the recorded value is itself a commit that moves the value. There is no fixed point |
| rewriting the history to the rules | a shared branch. Rewriting one message rewrites every commit after it |
| a warning instead of a refusal | ADR-20260826-gtb rule 9. A warning is how a gate dies |
| counting body lines including pasted output | it charges an author for the evidence, so the cheapest way to pass is to delete the evidence |

## A decision is committed with the code

A commit that settles something records it in the same change. See `engineering-standards.md` for where each kind goes. An ADR holds structure and contracts, and a fragment in `docs/changes/` holds behaviour a user notices. A release files it into `docs/CHANGELOG.md`.

Every bump-worthy commit (`feat`, `fix`, `perf`, `schema`, `infra`) carries a `Changelog:` trailer, and a prose entry in a fragment in `docs/changes/` staged in the same commit. Bump-worthy means the change would move the version number. The trailer value is the entry's first sentence, and the entry is one bullet written for a human reading release notes.

`Changelog: none` is the trailer for a bump-worthy commit nothing outside the repository can observe, and it is the only value that ships without an entry. A commit with any other prefix carries a trailer too when the change is user-visible: a `refactor:` that moves an exported package is user-visible. What earns an entry, the format, and the audience marker for `sdk/` are in `changelog-discipline.md`.

## Before starting any non-trivial change

```bash
git commit --allow-empty -m "checkpoint: before <what>"
```

This gives a clean rollback target if the change goes wrong. The commit is
empty on purpose: staging everything here would take in another agent's files.

## Commit often, do not batch

The most common failure is editing fifteen files across an hour and then asking whether to commit. Five real commits, made while you work, are better. One big commit leaves no rollback target between the steps, gives the reviewer one tangled diff, and forces the body to cover several unrelated changes at once.

Commit when any of these is true:

- you finished something that works on its own
- you have touched more than about three files since the last commit
- you are about to start something risky, which is a `checkpoint:`
- you are about to run the test suite on a chunk of changes for the first time
- a user message interrupts the task, so commit the progress before switching

Count ideas, not lines. A two-hundred-line refactor of one file is one commit. A four-line change across five files is usually two or three.

## Several agents share one branch and one checkout

More than one agent commits here at the same time, into the same working tree.
Nothing warns you that a file you are about to commit was written by somebody
else a minute earlier.

1. Commit by explicit path: `git commit -o <path> [<path>...]`, naming only the
   paths you changed.
2. Read what `-o` will take before taking it. It reads the **working tree** for
   the named paths, not the index, so a file another agent left changed goes in
   with yours. `git diff HEAD -- <paths>` is that list.
3. Name the file in the body when a commit carried one you did not write. The
   body is where a reader finds it.
4. Never run `git add -A`, `git add .`, `git stash`, `git reset` or
   `git commit --amend` on a shared branch. Each of them acts on paths you did
   not name.
5. Hold your commit while a merge is open. `git commit -o` prints `fatal:
   cannot do a partial commit during a merge`, and a merge here can carry
   twenty unmerged paths.

## A new record takes a dated id

A decision record is named by an id: the date in UTC and three random
characters, such as `20261025-k7q`. Write one with `make adr-new TITLE="..."`. Two pull requests never pick one id, and no file lists the ids,
so neither has to rebase over the other.

- Cite a record by its id, such as `ADR-20261025-k7q`.
- Every record carries a dated id. A record never takes a number.
- A record that replaces or changes an older one names it under `supersedes:`
  or `amends:` in its own front matter. The older record is not edited.

| the rule | gate |
|---|---|
| one id names one file across the two folders | `TestEveryRecordIDNamesOneFile` |
| a new record takes a dated id, not a number | `TestANewRecordTakesADatedID` |
| a file's name, heading and front matter say one id | `TestEveryNumberedDocumentAgreesWithItself` |
| front matter names a real status, date and older record | `TestEveryDecisionRecordHasItsFrontMatter` |

A collision of two random ids fails the second pull request when CI tests it
against the merged tree. Rename that file with a fresh id.

## Before pushing

Run these in order.

1. `git log --oneline @{upstream}..HEAD` and read every subject line
2. squash the noise. Interactive rebase before pushing if the list contains `checkpoint:`, `wip:`, or a `fix:` that exists only because the previous commit was wrong
   - `checkpoint: before X` then `feat: X` becomes just the feat
   - `feat: X` then `fix: typo in X` becomes one commit
   - do **not** squash two separate features, or a schema change and the code that uses it. A revert should be able to target one of them
3. `git diff @{upstream}..HEAD` and read every line of the net diff. Remove debug prints, dead comments, temporary workarounds
4. run the gates: `make lint`, `make test-unit`, and whatever else the change touched
5. confirm everything in the push belongs together. Unrelated cleanup goes on a different branch
6. each message describes the final state of that commit, not the steps that produced it

## Merging a pull request

A pull request of one commit may be squash-merged. A pull request of several
commits is rebase-merged.

GitHub writes a squash commit's message at merge time, from every commit on the
branch: the pull request title, then each commit's message as a bullet. No CI
run on the pull request reads that message, and the push to `main` checks it
for the first time. The bullets run past the 20-line body limit. The last
paragraph holds only the last commit's trailers, so a `fix:` title over a final
`test:` commit lands with no `Changelog:` trailer. A rebase merge puts on `main`
the messages the pull request already checked.

A branch whose commits should land as one is squashed locally before the push,
following [before pushing](#before-pushing), and then merged either way.

## Verify against a clean checkout

`.gitignore` can silently exclude a file you added, and the working tree will not tell you. Before pushing a change that adds files, confirm they are tracked:

```bash
git archive --format=tar HEAD | tar -t | grep <path>
```

An ignore rule with no leading slash can exclude a whole directory that exists locally, and CI then builds without it.

Listing a path is not building it. A commit can build in the working tree and
fail from a fresh checkout, because a file it references is still untracked.
So extract the commit and build what comes out:

```bash
tmp=$(mktemp -d) && git archive HEAD | tar -x -C "$tmp" && (cd "$tmp" && go build ./...)
```

Any commit that adds a file gets this before the push. A fresh checkout is what
CI reads and what a colleague pulls.

## Hard rules

- never amend a pushed commit
- the commands in [several agents share one branch and one checkout](#several-agents-share-one-branch-and-one-checkout) are hard rules too
- never force-push `main` or any shared branch
- never use `--no-verify`. If a hook complains, fix the cause
- one commit per logical unit, not one per file
- code comments describe what the code does now, never what it did before. `// fixed: was broken` and `// changed from X` are both forbidden

## Attribution

No `Co-Authored-By` lines. Check 6 of `scripts/check-commit-style.sh` refuses
one anywhere in a message, in both modes.

The line costs more than an untidy log. Git reads only a message's last
paragraph as trailers. A `Co-Authored-By:` paragraph written after the
`Changelog:` trailer turns that trailer into body prose, and
`%(trailers:key=Changelog)` then returns nothing to the push gate or to CI.
Repairing a message rewrites every commit after it on a shared branch, so an
agent does not do it.

The one line nobody writes is the one GitHub appends when it squash-merges a
pull request a bot opened: `Co-authored-by: dependabot[bot] <...>`. Check 6
passes a line naming a `[bot]` account, and `scripts/check-changelog.sh` drops
a last paragraph of such lines before it reads the `Changelog:` trailer.

`.githooks/commit-msg` refuses the same shape from the other direction, for
every prefix. A `Changelog:` line the message carries and `git
interpret-trailers` does not report is refused, whether or not the prefix owes
one.
