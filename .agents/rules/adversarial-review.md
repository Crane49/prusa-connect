# Adversarial review

Every change that is not trivial is reviewed by somebody who did not write it, before it is reported done. The author is the worst available reviewer of their own diff. They review the code they meant to write, and the bug is always in the gap between that and the code that is there.

This is a gate: a check that refuses a change. A change reported done without a review is reported wrong. "I re-read it and it looks right" is not a review.

## Run it. An estimate is not a review

**Every statement in a review is executed, or it is labelled a hypothesis in
that word.** There is no third category. A reviewer who reasons about what the
code would do has produced an opinion. An opinion reported as a finding costs
more than silence, because somebody then does work against it.

This binds both directions, and the second one is the one people skip:

- **to claim a defect**, run the input and paste the wrong output. Not the line
  of code that looks wrong: the observed behaviour. Where it needs a test to
  reach, write the test, run it, paste it passing, and delete it afterwards
- **to claim something is sound**, try to break it and paste what happened. "I
  read it and it looks correct" is the author's mistake under another name.
  A reviewer who reports an area clean without an attempt against it has
  reported nothing
- **to claim a gate holds**, change the code it guards so the gate should
  fail, run it, paste the failure text, revert, and run it green. Tests here
  have passed against the very defect they were written for
- **to claim a gate does not hold**, make the same change and paste the
  **green** run. A gate called weak without that is a guess

**A gate is tested by running the gate, never a copy of it.** A pattern
retyped into a shell loses a level of backslashes. A retyped `\b` reaches the
tool as `b`, so the copy matches nothing and the check looks dead while the
gate is correct.

- run the gate, or the script holding it, and quote what it printed
- to inspect a pattern, take the bytes from the file:
  `git show HEAD:path | sed -n 'Np' | od -c`, or the file the gate built.
  Never a line you typed again
- a claim that a check is dead carries the check's own output, and a probe sent
  through the gate's entry point

The failure is not the shell. It is that a reproduction can be tested against a
transformed copy of its own input and look conclusive.

What a hypothesis is for: a real mechanism you could not reach, on a platform
you do not have, or behind state you could not construct. Say so in the word
"hypothesis", give the mechanism, and say exactly what would settle it. That is
useful. Presenting it as a finding is not.

Numbers get the same treatment. A latency, a size, a series count or a memory
figure is measured on a named platform, or it does not appear. "Roughly",
"about" and "should be around" in a review mean the measurement was not taken.

## Every claim carries a proof and a test

A claim with no evidence is a note, and a note is not a finding. This binds both sides of the review.

- **the author.** "It works", "the gate holds", "the limit is enforced" and "nothing else reads this" are all claims. Each carries the observation that produced it (the command, its output, the row count, the status code) and the test that keeps it true
- **the reviewer.** A finding names the defect, the concrete input or state that triggers it, and what goes wrong. "This could overflow" is a note. "At `n = 0` this divides by zero and returns NaN to the caller, which stores it" is a finding. A note may be raised as a question, never counted as a finding
- **the fix.** It lands with a test that fails before it and passes after, in the same commit. Check the test: undo the fix, watch the test fail with the message you expect, restore. A test never seen red proves nothing
- **a claim you cannot test** is either not true yet or not a claim. Say which, and what it would take to test it. "Verified by reading" and "should work" are the phrases to catch yourself on

Nothing is reported done on the strength of a note.

## When it is required

| situation | review |
|---|---|
| new feature, new endpoint, new module, new screen | required |
| anything touching authorization, authentication, tokens, the printer path, storage writes, migrations, federation | required, and the reviewer is told which |
| a change to what `sdk/` exports | required, and reviewed as a permanent commitment |
| a fix to a bug that a test did not catch | required. The missing test is part of the finding |
| a refactor that moves behaviour between files | required |
| a document, an ADR, a changelog line | required, on the writing lens explained below |
| new or rewritten comment prose | required, on the writing lens explained below. [ADR-20260826-zbh](../../docs/decisions/20260826-zbh-a-comment-outlives-the-request-that-produced-it.md) rule 8 |
| typo, log line, formatting, version bump, a comment corrected for a typo | skip |

## The reviewer must be unbiased

Unbiased means structurally unable to inherit the author's belief, not "asked to be objective".

- a fresh context on a strong reasoning model. A reviewer that watched the code being written already agrees with it
- give it the diff and the requirement. Do **not** give it the plan, the reasoning, the commit body, or "here is why this is correct". That hands the reviewer the conclusion and asks it to agree
- ask it to find what is wrong and propose the fix, ranked by consequence. A review that returns "looks good" without having tried to break something did not run
- it judges quality, not only correctness: is this the right shape, does it duplicate an invariant that already exists, will the next person understand it

## The dimensions of a review

A change is reviewed on every dimension below. Each is a lens: one question a reviewer holds while reading. A reviewer handed one lens finds what a reviewer handed "review this" does not.

| dimension | asks |
|---|---|
| security | what does this let somebody do that they must not. Authorization on every new path, untrusted input reaching a sink, what the payload returns, secrets in logs or errors, and the same gap on the paths this change did not touch |
| design | is this the right shape. Does it belong where it was put, does it repeat a rule that already lives somewhere, is the boundary between parts in the right place, will the next change here be cheap |
| usability | the person receiving it. Can they find it, does it say what happened, does a refusal say what to do next, is the failure state as designed as the working one |
| logic and flow | trace one real story from start to end. State transitions, ordering, retries, partial failure, the second call, the concurrent call, the one that arrives out of order, and every state nobody designed for |
| standards | does it match this codebase's conventions and the language's, or invent a competing pattern beside an existing one. `.agents/rules/` is the standard and the code next to it is the reference |
| sanity | does it do what was asked, is it obviously too much or too little code for the job, would a colleague reading it for the first time understand why it exists |

On a large change, give each lens its own reviewer instead of several identical sceptics. Different lenses catch what repeating one lens cannot.

## Writing is a lens of its own

Prose ships in the same commit as the code. The ADR, the
changelog line, every heading and every comment are read later by somebody who
was not here. No gate in this repository reads any of them for sense.

So writing is a lens of its own, and it goes to a reviewer the way the
dimensions above do. That reviewer does not have to be a writer. Give it the document and
these questions.

| ask | it catches |
|---|---|
| where did you lose the thread, and why exactly there | the sentence that broke, named by the person it broke |
| which sentences could go with no finding lost | overexplaining, and no gate sees this one |
| does this measurement change the verdict | a number added because a number looks rigorous |
| what mechanism does this phrase name | a formula that sounds like insight and names nothing |
| can this document support this claim | a claim about every library, with no library named |
| does each heading name what is under it | WCAG 2.4.6, so an accessibility defect and not a taste |
| read each heading with the section covered. Who or what is it about | a pronoun with no antecedent, and a heading written from inside a conversation the reader was not in |
| would a B2 reader decode this word | vocabulary above B2, idiom, and phrasal verbs |
| is each project term linked to the glossary where the page first uses it | a term the reader was expected to know already. ADR-20260826-shb rule 6, and no gate holds it yet |
| what does this sentence compare the code to | a comment dated to the day it was written. ADR-20260826-zbh rule 4 |

The first question is the strongest one found here so far, and it is the only
one a reader answers without knowing a rule. It returns a location. "Where did
you lose the thread" cannot be answered with "it looks fine", and the answer
names the line to repair.

These defects need saying, because a reviewer who has never seen one does not
look for it.

- **overexplaining.** Measurements and things that do not matter, that do not
  affect the outcome, and that add text for the sake of adding text. It
  arrives as a measurement that supports no verdict, a run that confirmed what
  everybody expected, or a paragraph saying why an obvious thing is obvious
- **the formula that skipped the reasoning.** `which no author chooses and no
  reviewer sees in a diff` is one. It sounds like insight and names no
  mechanism. The repair is to name the mechanism, never to reword the phrase,
  because a reworded formula is the next formula
- **the claim the document cannot support.** A page said that no library
  round-trips a file "without changing more lines than the file has". No
  library was named and none was run. A claim over every member of a set needs
  the set, or a smaller claim

### What the gate can answer

`make style-check` holds a floor under some of the questions above and cannot
reach the rest. A reviewer holds the rest, and a review that skips them has
covered the cheap half.

| question | gate |
|---|---|
| the heading names its section | check 4, and only six templates of it. `writing-style.md` lists them |
| the word sits above B2, or is idiom | check 12. Check 10 is owed the word list |
| the sentence runs long, the cell runs long | checks 13 and 14 |
| the comment is dated to its day | check 15, and only the forms whose before is this repository |
| the reader loses the thread | none. A gate cannot read |
| the sentence could go | none, and this is the expensive one |
| the phrase names a mechanism | none. The phrase is grammatical and specific |
| the document supports the claim | none. It needs the sources the claim is over |
| the pronoun has an antecedent | check 4 in a heading. In body prose none: 20 of 20 sampled pronouns point at their own heading |
| the term is linked at first use | none built |

Overexplaining is the clearest case of the second kind. Every sentence in an
overlong document is well formed, on topic and true, so nothing is wrong with
any one of them. The defect is visible only against the question "what is lost
if this goes", and that question needs a reader who knows what the document is
for.

## The questions this project asks on every review

Ask these on every review here, because this is where its defects live.

1. **is the permission checked on every transport?** A rule enforced in the HTTP handler alone is not enforced. The event stream, the list query and the path to a joined server need it too. Closing one path and leaving the others open is not a fix
2. **does the list resolve permissions in the query?** Fetching rows and then filtering them leaks through page counts, totals and timing even when the returned rows are correct
3. **can a subscriber learn a resource exists?** Absence must look exactly like no access, in events as well as in responses
4. **does anything in the hot path allocate per subscriber?** The hot path is the code that runs for every printer message. Marshalling once per subscriber and queues with no limit are defects, not tuning opportunities
5. **is the third-party call supervised?** Bounded queue, drop policy with a counter, timeout, recovery. A contract that asks the author not to block is not a control
6. **does the sdk change break a compiled plugin?** An added interface method does
7. **is the gate watched rejecting something?** Change the guarded code and watch it fail. Tests have passed against the very defect they were written for. A gate that scans a set also owes a check on the size of that set. That check has one name, `requireScanned` in `internal/arch/scanned_test.go`, and `grep -rn requireScanned internal/arch` is the list of gates that carry one
8. **does this duplicate a rule that already lives somewhere?** One invariant written twice is the most expensive shape in a codebase. Only one copy gets updated, and the other keeps enforcing the old rule

## Running the review

- **the reviewer must not spawn agents.** See the rule in `engineering-standards.md`: a spawned agent does not spawn agents. One reviewer, one context, its own tool calls
- **ask for findings as they come.** A review interrupted at eighty per cent should still have delivered its first four findings
- **more lenses beat more reviewers.** One reviewer per dimension, from the table above, and one on the writing lens where the change carries prose

## Acting on findings

- **verify before you act.** Check the consequence against the running code. A finding can state a real gap with an inflated consequence, and the right action is to record it, not to change a contract on a guess
- a finding you disagree with gets an argued answer, not silence
- a real finding the test suite could not have caught means a gate was missing, so the fix carries the test that would have caught it
- findings that are real but out of scope go to the tracker with an owner, never to "later"

## The review stages

Review is not one act with a yes or no at the end. It is a loop, and a change
is only settled when the loop has closed.

| stage | what has happened | what it does not mean |
|---|---|---|
| **built** | the code is committed and the gates are green | nothing about whether it is correct |
| **reviewed** | an unbiased reviewer has attacked it and its findings are recorded | not that the findings are fixed |
| **fixed** | every finding is closed, argued down, or moved to the tracker with an owner | not that the fixes are correct |
| **re-reviewed** | a second unbiased reviewer has checked the fixes **and the area around them** | not that it is finished, if that reviewer found something |
| **settled** | the re-review found nothing new, or what it found went round again, and the log says what was tried | not a label anybody may write without the trace behind it |

A change reported done at **fixed** is a change nobody has checked since it
changed. That is the most dangerous stage, because it looks like the work is
over.

What is tracked is not the stage word but the **trace**, the written record of
each round. Every area carries a review log, one entry per round, and each
entry names:

- when it ran and which lens
- what it found, as tasks in that area's list
- what closed each finding, and **which test pins it**
- what the reviewer tried and could not break
- what is still open

A stage word on its own is not reportable. "Settled" tells the next person
nothing they can check, and it hides the facts that matter: what was looked
at, and what has changed since. A log entry with no round after it is
how an area says the code moved and nobody looked again.

An area reported clean records the attempts. A round with no attempts recorded
is a round that did not happen.

## A fix is reviewed too

The fix for a finding is new code and gets the same treatment as the code it
replaces. It is not exempt because it was written in response to a review.

A fix can close one escape and open another, let a rename skip an allowlist,
or answer a review by deleting a capability the gate had. In each of those the
fix is the defect, and the second review exists to catch it.

**The re-review is not scoped to the fix diff.** A reviewer handed only the
patch asks "does this patch close the finding". That is the one question the
fixing author already believes the answer to. The re-review asks these and
reports them separately:

1. is each original finding closed, tested against the concrete input the
   first review named
2. did the fix introduce anything new, anywhere it can reach, not only on the
   lines it touched
3. did the fix **degrade** something that was working: a capability removed
   instead of repaired, a check made narrower, a guarantee quietly weakened, a
   test loosened to make the fix pass

Point 3 is the one that needs saying, because it does not look like a bug. It
looks like the change being smaller than expected.

Further rules for the second pass:

- the re-reviewer gets the diff and the **original findings**, never the fixing
  author's report or their explanation of why the fix is right
- it must not be the first reviewer. A reviewer re-reading their own findings
  checks whether they were understood, not whether they were answered
- a fix that closes a finding by deleting the capability instead of repairing
  it is a finding
- a fix whose test would pass against the unfixed code is not a fix, and that
  is checked by running the new test against the old code
- **a loosened test is a finding.** If a test changed in the same commit as the
  fix, the re-review says why, and "it was too strict" needs the argument
- if the re-review finds something, the loop runs again. There is no round
  after which findings stop counting

## A reviewer is not paid by the finding

A review with nothing to report is a legitimate outcome and a useful one. The
failure on the other side is a reviewer who invents work to look diligent, and
it costs more than a missed defect because it arrives as real work.

- a finding names a concrete input and the wrong output it produces. Anything
  else is labelled a hypothesis, in those words
- **"this could be simpler" is not a finding** unless the complexity answers no
  requirement or decision, and the simpler version is shown to pass the same
  tests
- do not propose an abstraction with one implementation, an interface with one
  caller, or a gate for something already gated
- a proposal that adds more code than it removes says so, with both numbers
- a style preference with no rule behind it is not a finding. The rules are in
  this directory and they are the whole list
- where the code is right, say so briefly and continue. A reviewer who cannot
  name anything sound has not read enough to be trusted about the rest

## Reporting

When the change is reported done, say who reviewed it, which dimensions were covered, what each found, and what was fixed and what was only recorded. Say whether the writing lens ran, and over which document. Every claim carries its proof and the test pinning it.

A "reviewed, no findings" line with no named dimensions and no attempt to break something is a review that did not happen. That is usually what it is.

The report is checkable or it is not a report. Every finding carries the input that triggers it and the output it produced. Every "this is sound" carries what was tried against it. Every gate called green carries the change that was watched making it red. Anything that cannot show its work is a hypothesis and is labelled one.

A report made entirely of hypotheses says that the review could not be completed. That is a legitimate thing to say, and it is a different thing to say.
