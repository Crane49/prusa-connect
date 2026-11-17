---
title: "ADR-20260826-zbh: a comment outlives the request that produced it"
description: "Where a change, a decision, a moment and a history each belong. The one test a writer applies to tell a reason from a date. Check 15 refuses the forms whose before can only be a past revision of this repository. Comment prose gets no rate cap, and here is the measurement that settles why."
date: 2026-08-26
status: accepted
tags:
  - adr
  - documentation
  - writing-style
  - enforcement
---
# ADR-20260826-zbh: a comment outlives the request that produced it

- status: accepted
- date: 2026-08-26

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Extends [ADR-20260826-gtb](20260826-gtb-writing-that-reads-as-written.md), [ADR-20260826-shb](20260826-shb-a-document-is-read-at-speed.md) | Rules: [writing-style](../../.agents/rules/writing-style.md), [adversarial-review](../../.agents/rules/adversarial-review.md)

A comment here is often an answer to a question asked in a chat that day. It
reads correctly on the day it is written and stops meaning anything a month
later. This record settles what a comment may say about time, and it declines a
rate cap on comment prose.

## rules

1. A comment states what the code does and why it does it. Its truth does not
   depend on when it is read.
2. Each record has one home. A comment carries the code and a changelog entry
   carries the change. An ADR carries the decision and its reasoning, a commit
   message carries the moment, and a stable citation carries the history.
3. A comment names history only through a handle that does not move: a commit
   hash, an ADR number, a research note number, an issue number, or a released
   version. A handle a reader can look up is history. A time word is not.
4. Name what the sentence compares the code to, and the answer decides it. A
   previous revision of this repository is deleted. A state the running program
   can be in, or a fact about printers, firmware or the world, stays.
5. Give the reason and never its date. `This gate exists because review alone
   missed the clause` states a mechanism and holds in five years.
   `Until now the clause had no gate` states the same fact against the day it
   was written. It expires as soon as a reader cannot date the line.
6. `used to` is not written in a comment. One spelling carries the past
   habitual and the sense `employed to`, and the reader has to guess which.
   Write the plain verb: `stubStore asserts the plumbing`.
7. Comment prose carries no rate cap and no per-file budget. The constructions
   [ADR-20260826-gtb](20260826-gtb-writing-that-reads-as-written.md) rules 1 and 3 budget in
   markdown are counted in markdown only.
8. A change that writes or rewrites comment prose goes to the writing lens in
   [adversarial-review](../../.agents/rules/adversarial-review.md). A comment
   corrected for a typo does not.

Rules 4 to 8 are this record's, and an agent may revisit them.

## where each record belongs

| record | carries | why there |
|---|---|---|
| a comment | what the code does and why | it is read next to the code, at any date |
| a changelog entry | the change | a reader wants the release, not the file |
| an ADR | the decision and its reasoning | it is cited, and it is superseded rather than edited |
| a commit message | the moment | git already holds the date and the author |
| a hash, an ADR number, a research number | the history | the handle resolves, and a time word does not |

## the line rule 4 draws

Rule 4 is one question, and it separates the whole set. Every form below was
counted in this tree before it was placed.

| the sentence compares the code to | verdict | instance |
|---|---|---|
| a past revision of this repository | delete | `until now the principal was threaded through every store` |
| the mechanism that failed | keep | `review alone missed this clause` |
| a state the running program reaches | keep | `an instance that no longer exists` |
| a stage earlier in the same build | keep | `a base naming an earlier stage inherits its setting` |
| a fact about printers or firmware | keep | `a certificate that expires next week still serves every printer today` |

`no longer` was the largest count in the first survey of this tree, at 47. Every
one of the 47 compares the code to a runtime state, so none is a defect and none
is gated. The same holds for `currently` at 6, `the old` at 13 and `earlier` at
18. Time words are not the subject of this record. A comparison against a past
revision of this repository is.

`today` is the interesting one, at 19. About half are the world, and the other
half hedge a claim about this build: `RouteDeps.Router is gin-typed today`. A
hedge still parses correctly in five years, so rule 4 keeps it and check 15
skips it.

## Why comment density gets a reviewer and no cap

Earlier research
measured the contrast frame in `internal/**` at 29 times the Go standard
library's rate, the largest ratio in that note, and left the decision here. Four
measurements taken for this record, on branch `restructure`, go1.26.5:

| measurement | ours | go stdlib | ratio |
|---|---|---|---|
| the frame, all comments | 4.60 /kw (770 in 167,440) | 0.138 /kw (349 in 2,533,478) | 33x |
| the frame, doc comments only | 4.63 /kw | 0.11 /kw | 42x |
| the frame, implementation comments only | 4.41 /kw | 0.15 /kw | 29x |
| `because`, all comments | 3.46 /kw | 1.04 /kw | 3.3x |

Row two and row three kill the excuse. A doc comment documents an API and an
implementation comment argues a choice, so a corpus weighted towards the second
would score higher for reasons of composition. Our two halves score 4.63 and
4.41, so the finding survives its own confound and this record does not dismiss
it.

Row four is the honest correction in the other direction. Our comments carry
stated reasons at 3.3 times the standard library's rate, and the frame at 33
times. So roughly a tenth of the gap is that our comments explain where the
standard library describes. Nine tenths is voice.

The finding is real. A rate cap is still the wrong answer, for three measured
reasons: there is no denominator, the instances are correct, and the cheapest
repair is the harmful one.

There is no denominator. The median file under `internal/` carries 333 comment
words. Only 51 of 355 files reach 800, and those 51 hold 41 % of the comment
words and 38 % of the occurrences. Drop the floor to 400 words and it reads
73 %. Drop it to 200 and one occurrence in one file scores 5.0 per thousand,
above any threshold worth setting, so the rate has become a count wearing a
rate's clothes.

The instances are correct. Thirty-two occurrences were sampled and read, and
every one names the option it rejects. One of them: `it is a table rather than
a switch so a refusal can list what the kind offers`. That is what
[writing-style](../../.agents/rules/writing-style.md) asks a comment to be,
the constraint a reader would otherwise break.

The cheapest repair is the harmful one. A file over a cap passes again by
deleting the rejected option, and passes again by splitting the sentence in two
and keeping it. The second is the repair this project wants and the first is one
keystroke. No pattern tells them apart, and a gate that cannot tell rewards the
wrong one under time pressure.

So rule 8 sends it to a reviewer.
[adversarial-review](../../.agents/rules/adversarial-review.md) already carries
the writing lens and already lists the question that finds this: *"what
mechanism does this phrase name"*. That file listed `comment` in the skip row of
its own table, so the lens never reached a comment. This record changes that
row, which is the whole difference between a decision and a hope.

## enforcement

`make style-check` runs check 15 from the pre-push hook and in CI.

| rule | gate | flags at adoption |
|---|---|---|
| 1, 2, 3, 5 | check 15, the forms whose before can only be a past revision | 8 hits in 7 files |
| 4 | none. Which past a sentence names is a judgement over the sentence | not gated |
| 6 | check 15, `used to` in a comment | 3, every one the `employed to` sense |
| 7 | none is wanted. The four measurements above are the argument | not gated |
| 8 | none. `adversarial-review.md` binds a reviewer, and no gate reads for sense | not gated |

Check 15 read 36 hits in 32 files when its pattern was settled. A sweep against
that list ran while this record was written, so the baseline it ships with is
smaller and every line in it is meant to be deleted.

Check 15 reads the comment markers check 2 reads, in `*.go`, `*.yml`, `*.yaml`,
`*.sh` and the HTML comments of `*.md`. It carries a baseline on
[ADR-20260826-gtb](20260826-gtb-writing-that-reads-as-written.md) rule 9's model, so a count
that rises fails and a count that falls fails until the line is corrected.

The gate was watched failing before it was trusted. A probe file carrying five
comments, one per refused form, produced five hits:

```
internal/arch/probe/probe.go:4:// Until now the store did not fence a write.
internal/arch/probe/probe.go:7:// It used to fence on the etag.
internal/arch/probe/probe.go:10:// This change adds the fence.
internal/arch/probe/probe.go:13:// The previous version fenced on the etag.
internal/arch/probe/probe.go:16:// Before this, the store did not fence.
```

A second probe of four comments that must pass produced no hit. The four: an
origin that no longer exists, a read that returns before this call completes, a
claim hedged with `today`, and a value validated before this was reached.

A markdown probe carried the same sentence twice, once in prose and once inside
a fence. It produced one hit and named the prose line, which is
[ADR-20260826-gtb](20260826-gtb-writing-that-reads-as-written.md) rule 7 working. Deleting both
probes returned the run to its earlier count.

The pattern refuses these and nothing else:

```
this change | this commit | this patch | this pull request | this diff
this revision | until now | up to now | until this | previously | used to
the previous version | implementation | behaviour | design | approach
the previous shape | handler | scheme | rule | was changed | were changed
before this,  |  before this the | a | an | nothing
before this <noun> existed | exists | could | would | had | did | was | were
```

`before this` splits on the word after it, and the split was measured. A noun
after it names something the repository grew, so the sentence is a diff. A verb
after it is control flow inside the running program. Over the whole tree the
narrow form drops exactly three lines that the broad form catches, and all three
are control flow: `before this returns` twice and `before this was reached`
once.

## rejected

| option | why not |
|---|---|
| a rate cap on the contrast frame in comments | no denominator, 32 of 32 instances correct, and the cheapest repair loses the reason. Argued above |
| a per-file budget on the absent-agent family in comments | 0.75 per thousand words here, under ADR-20260826-gtb rule 3's own cap of 2.00. Nothing to gate |
| gate `no longer` | 47 hits and every one is a runtime state. It would cost an author an argument 47 times for no defect |
| gate `today` and `currently` | 25 hits, about half the world and half a hedge. A hedge still parses in five years |
| gate `the old` and `earlier` | 31 hits, mostly the old instance and an earlier build stage |
| widen check 2 instead of adding check 15 | check 2 fails hard at zero and the tree carries hits today. Rule 9 of ADR-20260826-gtb forbids shipping that as a warning |
| ship check 15 as a warning until the sweep lands | ADR-20260826-gtb rule 9. A warning is how a gate dies |
| a git-scoped ratchet, on checks 12 to 14's model | it reads only the files a push changes, and the whole tree has to be enumerated |
| a banned-word list from the AI-tell articles | ADR-20260826-gtb already rejected one, measured. Earlier research scores this corpus cleaner than three human corpora |
| extend check 12, the idiom gate, to comments | 115 comment lines carry a seeded idiom. It is a real finding and a separate sweep, on the old backlog |
| leave `used to` alone, because some of its hits are the innocent sense | the reader cannot tell the two senses apart either, and the innocent sense rewrites to a plain verb in one edit |
| an exemption marker a writer types in a comment | ADR-20260826-gtb rejected one for markdown. A marker a writer can type anywhere becomes the way to silence the gate |

## this record against its own rules

Measured with the same scripts, over 2,067 prose words.

| rule | this file |
|---|---|
| ADR-20260826-gtb rule 1, the contrast frame | 1.94 /kw against the 6.00 cap |
| ADR-20260826-gtb rule 3, the absent agent | 0.48 /kw against the 2.00 cap |
| ADR-20260826-gtb rule 4, bolded lead-ins | 0 |
| ADR-20260826-gtb rule 5, a repeat at twelve words | 0 |
| ADR-20260826-shb rule 8, sentences over 30 words | 0 |
| ADR-20260826-shb rule 9, cells over 120 rendered characters | 0 |
| ADR-20260826-shb rule 7, idiom and phrasal verbs | 0 |
| this record, rules 1 to 6 | every refused form here sits inside a fence or a code span |

## open items

- The baseline carries the lines check 15 still flags. A separate sweep is
  clearing them, and every line is meant to be deleted rather than kept. The
  task is on the old backlog.
- **closed.** Check 15 lengthens `scripts/writing-style.baseline`, and
  [ADR-20260826-gtb](20260826-gtb-writing-that-reads-as-written.md) rule 9 says a check the
  tree fails ships as a ratchet. The ratchet wins and the baseline may grow.
  ADR-20260826-shb is amended.
- Rule 4 has no gate and no proxy was tried. Which past a sentence names is a
  judgement over one sentence, and the same reach check 4 lacks for headings.
- **closed.** Check 15's recall is measured. A hundred comment blocks were
  drawn at random from the 9,295 under `internal/` on 2026-08-30 and read one
  by one against rule 4. One carries a defect the pattern misses, and it is
  repaired. So the pattern is a floor under the rule and not the rule.
  No extension follows. The form the miss takes is `until` with a past-tense
  verb, and that pattern flags three lines here. One is the defect and one is
  correct as written. The old backlog
  carries the working.
- Check 12 reads markdown only, so [ADR-20260826-shb](20260826-shb-a-document-is-read-at-speed.md)
  rule 7 binds a `.go` file and no gate reads one. Running check 12's own
  pattern file over the comments in `internal/`, `sdk/` and `cmd/` counts 153
  lines in 111 files. That is a count and not yet a finding. 81 of the 153 are
  `read as` and `reads as`, and whether those are idiom or ordinary technical
  English needs somebody to read a sample. Do that before extending the check.
- A gate verified outside itself can be tested against a transformed copy of
  its own pattern. Two reviewers called check 12 dead on one day, both having
  retyped its `sed` line into a shell that ate a backslash level. The rule is
  now in [adversarial-review](../../.agents/rules/adversarial-review.md) under
  `Run it`: run the gate, and read a pattern out of the file as bytes. No gate
  enforces it, and the same hazard reaches any check whose regex is built by a
  transformation.
- Rule 8 binds a reviewer and nothing counts whether the lens ran. That is the
  same gap `adversarial-review.md` already records for the four questions no
  gate reaches.
