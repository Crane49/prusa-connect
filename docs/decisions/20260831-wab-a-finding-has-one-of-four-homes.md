---
title: "ADR-20260831-wab: a finding has one of four homes"
description: "That there is no sixth record for a repeated finding, which of the four existing homes takes each kind, and the five rules earlier research specifies with the gate or the honest absence behind each. The class name lives in a helper because a reviewer reads a rule and a gate author reads a name."
date: 2026-08-31
status: accepted
tags:
  - adr
  - documentation
  - enforcement
---
# ADR-20260831-wab: a finding has one of four homes

- status: accepted
- date: 2026-08-31

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) · Records: [ADR-20260826-zbh](20260826-zbh-a-comment-outlives-the-request-that-produced-it.md) · Git rules: [git-discipline](../../.agents/rules/git-discipline.md)

A gate that runs, reports green and reads nothing was found seven times here.
Seven different people found it, and each wrote it down somewhere else.

Two commit bodies count the class and disagree. One says "the seventh gate in
this repository that reports green over nothing". The other says "the eighth
swallowed failure this Makefile will not have". Neither number can be queried,
so the eighth author cannot tell whether what they found is new.

The answer is not a sixth record. It is that each kind of finding already has a
home that refuses something, and one of them was a number a person kept.

## rules

1. Write a repeated failure class into one named helper in `internal/arch`, and name that helper from the review suspicion that asks for it. A rule text is read by a reviewer, and a helper name is read by the author of the next gate at the moment they write it. That is the only moment the seventh reinvention can be stopped. The name is `requireScanned` in `internal/arch/scanned_test.go`.
2. Never write the count of a class. Compute it from commit trailers. A count written by a person is two prose numbers that disagree within three days, which is what the two bodies above are.
3. Send a measurement back to the research note that holds it, corrected in place with the reason, and name the check that recomputes it in that note's front matter. Research is not append-only here, which `engineering-standards.md` already requires.
4. Send a fact about one machine to a check that prints what it observed, or to a backlog task. A fact that never ticks is not a task, and a note one question wide cannot take a stream.
5. Send a process failure between agents to `.agents/rules/git-discipline.md`. Its subject is what one agent does to another's work, and three of the failures counted in earlier research are that.
6. Do not add a sixth record for any of it. `docs/map.md` already indexes fourteen destinations and missed one of the five until earlier research read it, and a person cannot keep track of that many files.
7. A `fix:` commit names the commit that introduced the defect, as `Fixes: <12 hex> ("<subject>")`, with `Fixes: none` as the null. That trailer is what makes rule 2 computable: grouping the named commits replaces both prose counts.
8. An arming assertion takes one name across the tree. A gate that scans a set owes a check on the size of that set, and seven of them were written under seven names before rule 1 gave it one.
9. A `docs:` commit stages no line of Go under `internal/`, `sdk/`, `cmd/` or `adapters/` that is neither a comment, a blank line, nor a string literal. Code landing under a documentation prefix is code nobody will find again. 141 lines of `mountConnectors` went in under a six-line documentation edit, and the `feat(connectors)` commit describing it holds neither the function nor the file.
10. `style:` is not judged by rule 9. The prefix table gives it formatting with no logic, so formatting a Go file changes code lines by definition. All three `style:` commits in the measured window are correct as written.
11. A machine fact becomes a check that prints what it observed, and never a sentence asserting it. The sentence is read on a day the writer cannot predict, and the machine has moved.
12. A finding becomes a check on its own only when it is a verified bug or a
    machine fact: something a test can hold without anybody deciding. A
    finding whose fix adds a feature, a surface or complexity to the program
    is discussed first, as an owner question, before anything is built.

## enforcement

| rule | gate |
|---|---|
| 1, 8 | `requireScanned` in `internal/arch/scanned_test.go`, and [the arming name](#the-arming-name) |
| 7 | check 7 of `scripts/check-commit-style.sh`, run by the commit-msg hook, the pre-push hook and CI |
| 2 | `git log --grep='^Fixes:'`, armed by check 7. See [the count is a query](#the-count-is-a-query) |
| 3 | none yet. A `supersededBy:` key asserted by `internal/arch/numbered_docs_test.go`, open as a row on the old backlog |
| 4, 5, 6, 11 | review. Which record a finding belongs in is a judgement over the finding, held by the six lenses in [adversarial-review](../../.agents/rules/adversarial-review.md) |
| 9, 10 | `scripts/check-docs-commit-scope.sh`, run by `.githooks/commit-msg` |

### The arming name

`requireScanned` refuses a gate whose input set is empty, so a tick cannot
stand for coverage the run did not have. `grep -rn requireScanned internal/arch`
is the list of gates that carry one, which is rule 8's whole point: one name a
person can query beats seven a person has to remember.

### The count is a query

The `Fixes:` trailer is the query rule 2 needs, and check 7 arms it. The query
returns nothing on this branch today, which is correct rather than broken: the
check starts at `5831177`, so it names what arrives from now on. The 78 `fix:`
commits before that floor cannot be revised on a shared branch, which is
git-discipline rule 11.

### What rule 9 refuses

A `docs:` commit whose staged Go diff changes a line that is not a comment, not
blank, and not a string literal. `internal/arch/docs_commit_scope_test.go` runs
the script over five fixtures rather than a second copy of its logic. One
refuses and four pass, and the four are what stop the refusal being a script
that refuses everything.

Rule 9's gate runs in no CI job, and `gateScriptsCIExempt` carries the reason. A
runner stages nothing, so `git diff --cached` there is empty and the check would
report a tick over nothing, which is rule 1's own failure in a second language.

The honest summary: **rules 1, 8, 9 and 10 are gated. Rules 2, 3 and 7 name the
gate they wait on, and rules 4, 5, 6 and 11 are a reviewer's.**

## rejected

Each of five records was tested against a real entry rather than against its
charter, in earlier research.

| option | why not |
|---|---|
| a sixth record, a directory of its own | [a sixth record](#a-sixth-record) |
| earlier research | a note is one question wide and this class is a stream |
| `docs/decisions/` | an ADR is a rule, and "a gate reports green over nothing" is not one |
| the old backlog | [the backlog is the closest](#the-backlog-is-the-closest) |
| `docs/CHANGELOG.md` | nothing in this class is observable from outside the repository |
| a prose count a person keeps current | measured: two bodies, three days apart, disagree by one |
| a rule 9 reading the staged paths | [why rule 9 reads the diff](#why-rule-9-reads-the-diff) |
| a rule 9 counting every non-comment line | the same section. Prose and a path live in Go string literals here |

### A sixth record

A new directory costs more than it looks. `docs/map.md` already indexes
fourteen destinations and missed one of the five records until earlier research read
it. A person cannot keep track of that many files.

The decisions README also states that an accepted ADR is never edited. So a
growing list could not live in `docs/decisions/` even if an ADR were the right
shape for it.

### The backlog is the closest

It already carries most of the instances, so it is worth saying why it is still
not the answer.

Its own README says a task nobody could tell was finished is a story rather
than a task, and a machine fact can never be ticked. An epic also organises by
area of work, and this class crosses every area. That is why it arrives four
times on the epic about gates and twice more on epics that are not about gates.

### Why rule 9 reads the diff

A `.go` file whose changed lines are all comments is documentation, and two
commits in the measured window are exactly that. The path rule flags four
commits and the diff rule flags one.

Counting every non-comment line as code is the next wrong answer. Prose and a
path are written in Go string literals here. Two of that rule's three `docs:`
flags change nothing but a sentence in an arch test's exception map, or a
file name to its full path in a list.

## open items

- rule 7's ancestor check runs in message mode alone. Range mode judges the shape and not whether the sha resolves, because it would fork `git cat-file` once per commit for an answer that cannot change after the fact
- rule 3 has no gate. The `supersededBy:` key and the path assertion are not built
- rule 2 cannot be computed until rule 7 lands. Until then the class is a table in the old backlog that somebody maintains
- rule 9 was measured over 260 commits, which hold 113 `docs:` and 5 `style:`. Re-measure it over a later window before tightening it. One commit in 113 is a low rate, and a rate that stays at zero for a year is a rule everybody already follows rather than a gate worth keeping
