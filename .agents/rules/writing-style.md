# Writing style

This rule applies to prose: markdown documents, readmes, code comments that explain what or why, commit messages, changelog entries and pull request descriptions.

It does not apply to config files (`.env.example`, `Makefile`, `Dockerfile`, compose yml). There a banner or a `# ── section ──` separator helps a reader scan, and that is the house standard for those files.

## How to write it

Read this before the rules. The rules below say what to avoid. This part says
what to do, with a test you can run on your own sentence.

**Write for one person who is new here.** The reader is a person, not a
native English speaker, at B2 level. B2 is the upper-intermediate level of the
common European scale: a person who reads everyday English well but not idiom
or rare words. They do not know our names for things, and they will not
understand complex wording or deep technical terms. The text has to be easy to
read and still technical enough to use. Before you write, name what they came
to learn. Answer that in the first paragraph. A reader who leaves after one
paragraph should already have the answer.

**Say it the way you would say it to them in person.** Short sentences. One
idea in each. Plain words before technical ones, and a technical word explained
the first time it appears. Active voice: "the server sends a command", not "a
command is sent".

**A heading is the first line of the answer.** Read the heading aloud to a
colleague. If it sounds like something you would say, keep it. If it sounds
like a slide title, a chapter name or a headline, rewrite it.

| rejected | accepted |
|---|---|
| The three shapes, judged | Which option should we pick? |
| Documentation that cannot go stale | How we keep the documentation in step with the code |
| One mechanism per boundary | There isn't one system, there are four |
| The verdict on the two claims | So does it hold up? |
| What is decided, what is built, what is open | What is built and what is planned |

**Plain verbs, not pictures.** A component renders, a request goes through
the SDK, a function returns a value. Metaphor verbs are the sound of a model
writing: `rides`, `hangs`, `draws`, `spells`, `holds`, `carries`. So are the
set phrases around them: `X and nothing else`, `which is why`, `is what
makes`, `the way Y does`, `It decides four things:`. Use the ordinary verb.
If a list follows, start the list without announcing it.

| rejected | accepted |
|---|---|
| `holds the rules about printing, with nothing of a transport or a screen in them` | holds the rules about printing, as plain functions. There is no network code in it |
| `a device record is handed in as the API spells it` | you pass a device record in exactly as the API returns it |
| `every request rides the SDK` | every request goes through the SDK |
| `it is handed the printer's id and nothing else` | its only input is the printer's id |
| `It decides four things: a heater's state, ...` | `heaterState` compares a heater with its target. `projectLifecycleVerb` says whether ... |

**Describe, do not claim.** "A test fails when the line moves" is something a
reader can check. "Cannot go stale" is a promise nobody can keep. If a sentence
would embarrass you when it proves wrong, rewrite it as what happens.

**No history in a document about the present.** "Previously", "used to", "after
round six" and "was rewritten" are history. History lives in git and the
changelog. A document describes the present state of the thing.

**No names and no dates in a rule.** A rule says what to do. Who decided it,
and when, goes in the commit message. A question for the
maintainers goes in a GitHub issue.

```
rejected: The owner set the bar on 2026-09-11 and only he can change it.
accepted: The bar is B2.
```

**Show one thing.** A concrete example beats a description of the category. "A
script is a one-liner in the config file, like forwarding an event to a URL"
beats "scripts provide lightweight event-handling capabilities".

**Read it back as the person you named, then run the check.** The check
catches sentences over thirty words, idioms, em dashes and a few shapes. It
cannot tell whether the page answers the question. That part is yours.

The target, from the README:

> The printer dials the server over a WebSocket, the kind of connection that
> stays open, and shows a short code on its screen. You type that code into the
> web page to approve it, or let the server approve automatically on a trusted
> network. The server hands the printer a token. From then on the printer
> reconnects with that token and its fingerprint whenever it powers up. Down
> that open line the printer reports temperatures, progress and state a few
> times a second. Up the same line the server sends commands: start this file,
> pause, cancel.

It says what happens, in order, in the real words, and nothing about how clever
the design is.

## Rules

- headings are required. Structure a document with them; do not run prose together
- headings use sentence case: capitalise the first word and proper nouns only. `Quick start`, never `Quick Start`
- a heading describes the topic or purpose of what is under it, and the word a reader looks for comes first. See [headings](#headings)
- body prose is sentence case too. Lowercase unless it is a proper noun or starts a sentence
- no emoji unless the user asks
- no decorative arrows (`-->`, `==>`), no ascii art in prose
- no em dashes anywhere: prose, UI copy, comments, commit messages. Write a plain hyphen, a comma, or a new sentence. Mixed punctuation across a repository looks like two authors
- a punctuation sweep edits text inside a quotation too. The wording never changes, so a quotation here carries the source's words and not always its characters. Cite the source, and read it there when the characters matter
- tables and lists beat paragraphs. Paragraphs are the exception
- a table cell holds a value, and a table does one of two jobs: it compares
  values, or it indexes entries written out below it. An argument goes under a
  heading, which has an anchor a reader can link to. See
  [ADR-20260826-shb](../../docs/decisions/20260826-shb-a-document-is-read-at-speed.md) rules 9
  and 13
- say what things are, not what they are not
- if a concept needs more than three sentences, either the concept or the code is too complex
- length is a cost. A document nobody finishes tells the reader nothing, however correct it is. Keep the decision and the evidence for it, and cut the rest

## An example of the target

Every rule in this file is a prohibition. A page can pass every check and
still be wrong, because a prohibition cannot show the target. One answer is
the target. Asked whether Fractal copies whole databases, the reply took
about 250 words, with no table and no heading, in words the reader already
had. It was easy to understand and it answered the question.

What that answer did, in order:

1. took the reader's position seriously in its first sentence: "You are
   right in principle, and Fractal is narrower than that makes it sound"
2. named the two concrete things at issue, each under a plain bold label
   with one short paragraph: the shadow, and undelivered messages held on
   disk
3. gave the reason each one is not the feared thing, in one sentence per
   thing: a mirroring protocol has to define what a mirror holds, or
   "mirror" means nothing
4. stated the real problem in a single sentence: their shadow and our state
   tree are the same thing, held twice, merged by different rules
5. ended with something the reader can do: ask whether the shadow can be an
   interface instead of an implementation

Write a page toward that shape. Say what the reader got right, name the
concrete things, give reasons and never bare assertions, state the one real
problem, and end with an action. The rules above only remove what hides
this. No check measures it. The writing review in
[adversarial-review](adversarial-review.md), where a second reader reads a
document for sense, holds a draft against it.

## Start from what the reader sees and does

This is the shape every question, rule and commit message takes. It is a rule,
and the examples below show it.

- start from what the reader sees and does, never from the system's names for things
- one idea per sentence
- explain a term where it first appears, in the same sentence or the one after
- give an everyday comparison when it helps
- never assume the reader holds the project's names in their head
- a heading is a natural title, the kind a person reads in a book or a manual. A comparison or a metaphor stays in the text, never in a heading
- no counting in prose. Do not write "the sign has three parts: a name, an address and four notes" or "the two things to decide". Say what the thing holds
- no "today" in a heading or in a sentence about the current state. Say how it works, or name the current implementation
- a rule file states the rule as it stands, once. It does not record older versions of the rule or the corrections that led to it

A question put to the person who decides, in that shape:

> Background first. Every printer on the server has a queue: a list of jobs
> waiting to print, in order. When a print ends, the server starts the first
> job in the queue. You also said that a print farm may want to pick the next
> job itself, for example by the filament that is loaded. That choice is code,
> and it decides which job a printer starts next. So the question is who is
> allowed to write it. There are two kinds of add-on code in this project. One
> kind is built into the server binary by us, reviewed like the rest of the
> core. The other kind is a module: a file an operator can install on a running
> server. Which kind may pick the next job?

Why it works: "queue" is explained in the sentence that introduces it. The
choice is explained as "it decides which job a printer starts next". A module
is explained as "a file an operator can install on a running server". Every sentence carries one
idea. The question comes last, after the reader has everything needed to
answer it, and it can be answered in one word.

A rule statement in the same shape:

```
rejected:
Every request rides the SDK; a service never reaches for the global fetch.

accepted:
A page in the browser needs data from the server. It asks through the SDK,
the small library under packages/typescript that knows the server's address
and the login token. A service file never calls fetch itself. If it did, it
would need the address and the token too, and there would be two places to
keep them.
```

A commit message in the same shape. The subject says what a user notices, and
the body says what happened before, what happens now, and why:

```
rejected:
fix(storage): harden Save against partial writes

accepted:
fix(storage): keep the existing file when an upload fails partway

An upload that stopped halfway left a truncated file where the old one had
been. Save now writes to a temporary name in the same directory and renames
it into place after the data is on disk. A failed upload leaves the old file
untouched.
```

## Headings

The rule is [WCAG 2.2 SC 2.4.6](https://www.w3.org/WAI/WCAG22/Understanding/headings-and-labels.html)
at Level AA: **headings and labels describe topic or purpose**. WCAG is the
accessibility standard for the web, so this is not a style preference. 71.6 %
of screen reader users search a long page by its heading list before anything
else ([WebAIM survey 10](https://webaim.org/projects/screenreadersurvey10/),
2024), so a heading that does not label its section is an accessibility defect.

Write a heading that is all four, which is
[GOV.UK's own list](https://guidance.publishing.service.gov.uk/writing-to-gov-uk-standards/writing-guidelines/clear-structure/):

| property | means |
|---|---|
| descriptive | it names the thing under it, and never a category or a stage of your writing |
| front-loaded | the word a reader is scanning for is the first word |
| active | start with a verb where one fits |
| removable | the section still reads correctly with the heading deleted |

A heading is a statement, never a question. GOV.UK: a heading should not "be
questions", because "users want answers, not questions". Content Design London
gives the same reason: a question cannot be front-loaded.

Ask before you write one. Does the first word name the subject? Reading
only the heading, can a stranger say what is under it? Would a person answering
"what is in this section?" say it out loud that way?

These shapes fail the rule. Each one is an example of the failure:

| fails | why | write |
|---|---|---|
| `Key takeaways`, `Summary`, `Overview`, `Conclusion` | a category with no topic in it | the thing: `Cardinality limits`, `NATS verdict` |
| `Considerations`, `Notes`, `Additional context` | same | `Rejected`, `Open items`, `Failure modes` |
| `Introduction`, `Background`, `Getting started` | a stage of writing | the first section is the content, not a run-up |
| `Start here`, `What this implies for us` | the reader's position in the document | `Recommendations` |
| `How a failure becomes an answer` | a figure of speech. Idiom is hard to decode for a reader whose first language is not English | name what happens: `Where a fault code becomes an HTTP status` |
| `The baseline, measured`, `The envelope claim, tested` | reports what the writer did. The reader wants what was found | the finding, `The baseline is 443 lines`, or the plain noun, `The baseline` |
| `The tiers, and how many there should be` | a question with the question mark removed | `How many tiers there should be`, or two headings |
| `Fractal in four sentences`, `The schema, by its three files` | names the length or the layout of the prose, which the reader can see | the subject alone: `Fractal`, `The schema` |
| `Whether this tree has the sprawl` | a clause, not a statement. It withholds the answer the way a question does | the answer: `The tree has no sprawl` |
| `The code he criticizes` | a pronoun. A heading is read alone in the heading list, where no earlier sentence supplies the person | name the person or drop them: `The criticized code`, `legacy/fractal` |

These pass. Leave them alone:

- a list: `Discovery, distribution, provenance`
- a name followed by what it is: `app, the composition root`
- two imperatives that are the rule: `Marshal once, share the bytes`
- a claim a reader can check: `Why the graph comes before the hub`, `Where the codes are defined`

One departure from GOV.UK: GOV.UK asks that headings carry no
technical term. This project keeps the term, because the term is what a reader
searches for. See [ADR-20260826-shb](../../docs/decisions/20260826-shb-a-document-is-read-at-speed.md)
rule 3.

## Code comments

This code is public. A comment is read by a stranger who never saw the
conversation that produced it.

- English, always, whatever language the work was discussed in
- comment **why**, not what. The code says what it does
- the absolute minimum. A line that restates the line below it is deleted
- comment the non-obvious: a constraint, a measured limit, a workaround and the
  thing it works around, an invariant a reader would otherwise break
- one or two lines. A paragraph in a source file means the code needs the work,
  not the prose
- link the decision, do not restate it: `// see ADR-20260824-nyw` beats three lines of
  reasoning copied out of it

## A comment cannot be dated

[ADR-20260826-zbh](../../docs/decisions/20260826-zbh-a-comment-outlives-the-request-that-produced-it.md).
A comment is read on a day you cannot predict, so its truth may not depend on
when it is read.

Each record has one home. A comment goes with the code, a changelog entry with
the change, an ADR with the decision, a commit message with the moment, and a
citation with the history.

One question decides a sentence. Name what it compares the code to:

| it compares the code to | do |
|---|---|
| a past revision of this repository | delete the comparison |
| the mechanism that failed | keep it. `review alone missed this clause` |
| a state the running program reaches | keep it. `an instance that no longer exists` |
| a fact about printers, firmware or the world | keep it |

Give the reason and never its date. `This gate exists because review alone
missed the clause` is still true in five years. `Until now the clause had no
gate` is the same fact tied to a day, and it expires.

Name history through a handle that still resolves later: a commit hash, an ADR
number, an issue number, a released version. Check 15
refuses `until now`, `until this`, `previously`, `used to`, `this change`,
`this commit`, `the previous version` and `before this` with a noun after it.
It does not refuse `no longer`, `today`, `currently`, `the old` or `earlier`,
because in this tree those name a state of the running program and not a past
revision.

Write the plain verb instead of `used to`. That spelling carries two meanings,
the past habit and `employed to`, and a reader has to guess which.

Never write a comment that only makes sense to somebody who read the request:

| never | why |
|---|---|
| `// as requested, we now...` | there is no requester in a source file |
| `// this is the cost of not shipping X` | argues a decision at somebody who did not ask |
| `// NOTE: the user wanted this configurable` | names a conversation nobody else was in |
| an essay justifying an approach | that belongs in the commit body or an ADR |
| `// removed the old handler` | the diff shows it |

A commit body explains a change to a reviewer. An ADR explains a decision to the
project. A comment explains code to the next person editing it. Keep them in
their own places.

## Doc structure

Every document presents these in this order:

1. one-paragraph purpose
2. links (repository, manifests, dashboards, URLs)
3. commands table
4. env vars table (`var | required | default | description`)
5. key files table (`path | purpose`)
6. everything else

## Rules documents are rules

A standards document or an ADR is a set of rules a reader follows and a reviewer cites. It is not a record of how the rule was made.

- numbered imperative rules, one per line
- a `rejected` entry per alternative, enough that the option is not proposed
  again. An alternative whose reason fits a cell is a table row. One whose
  reason is an argument is a heading named for the option, so a reader links to
  the argument instead of repeating it
- the enforcement gate named, or an honest statement that there is none. One row
  per gate: a cell naming four tests is a list that lost its markup
- an `open items` list where the code does not yet follow it

The shape of those two sections is
[ADR-20260826-shb](../../docs/decisions/20260826-shb-a-document-is-read-at-speed.md) rules 13
to 15. A rejected option still carries the whole reason it lost, and that
reason is an argument, which does not fit a cell.

Keep the measurements and the reasoning in the commit message or the ADR. A standards document that reads like an explanation addressed to a person is wrong, however accurate it is.

## Never write

- `// removed X` next to deleted code. The diff shows it
- dead code kept "in case we need it". Git history has it
- commit messages like `update files`, `wip`, `final`, `cleanup`
- filler words: `please`, `kindly`, `as you can see`, `it should be noted that`
- sentences that only make sense inside the conversation that produced them. If a line would look odd to someone opening the document in a month ("we'll fix this later", "come back in a few weeks"), do not write it. Documents are read out of context by default

## Ending punctuation

No trailing periods on bullet items unless the bullet is a full sentence.

Example: `- run tests` not `- run tests.`

## Enforcement

`make style-check` refuses an em dash anywhere and an emoji anywhere. It also
refuses a comment describing what the code did before, a comment addressing the
reader, and a comment dated against the repository's own past. It runs from the
pre-push hook and in CI.

Check 16 holds the emoji rule above. It names each code point instead of
spelling it, the way check 1 names the em dash, and it carries no baseline. It
reads the markdown with fenced blocks removed, plus the same source files check
1 reads. So an emoji in a Go comment, a workflow's `echo` or a shell script is
refused too. A markdown fence may still quote one.

Check 17 holds the rule above about names and dates. It reads the changed
markdown files the way checks 12 to 14 do and holds each file to its count at
the base. The phrase list is in the script, one line per form.

`✓`, `✗` and `⚠` are not in it. Those are status markers, [ADR-20260826-gtv](../../docs/decisions/20260826-gtv-build-tooling.md)
rule 14 governs where a program may print one, and the records stating that rule
have to quote the character they forbid.

Check 4 sets a floor under the heading rule. It cannot hold the rule itself.
It catches only these templates:

- a category, or the reader's position in the document
- a figure of speech narrating a transformation
- the writer's activity added to the subject after a comma
- the prose's own length or layout as the subject
- a subordinate clause standing alone
- a third-person singular pronoun

Whether a heading describes the section under it is a judgement over two
texts, and no pattern makes that judgement. So a heading can pass check 4 and
still fail the rule, and a reviewer holds that half.
[ADR-20260826-gtb](../../docs/decisions/20260826-gtb-writing-that-reads-as-written.md) states
the rule.

Comment length, prose quality, heading case and the density of any construction
in a comment are deliberately not checked. A checker that passes bad prose
approves it, and length is the wrong measure: one twelve-line comment worth
reading beats three that are not.

[ADR-20260826-zbh](../../docs/decisions/20260826-zbh-a-comment-outlives-the-request-that-produced-it.md)
rule 7 holds the last of those against a measurement. Comments under
`internal/` use the contrast frame, the "X, not Y" sentence shape, at 33 times
the rate of the Go standard library, and it is still not capped. The reasons,
each measured:

- 32 of 32 sampled instances name the option they reject
- the median file carries 333 comment words, so a per-file rate has no
  denominator
- the cheapest way to pass a cap is to delete the rejected option

Rule 8 sends it to the writing review in
[adversarial-review](adversarial-review.md) instead.

## What good looks like

Every rule above says what not to write. These pieces in this repository say
what to write.

A page a person reads to make a decision follows one model. Each entry says what the thing is in plain words before it says what
is wrong with it. It gives the consequence as something a person using the
server would notice, not as a fact about the code. It names the options so a
reader can answer in one word. Identifiers, file paths and record numbers sit in
a footer the reader can ignore.

`docs/src/content/docs/writing-a-driver.md` is the model for a guide. Every
command in it was executed and its real output is quoted.

The habits that carry across both:

1. Open with the reader's question, not with your finding.
2. Name the thing in plain words before naming the problem with it.
3. End with what the reader can do next.

A page that needs a table of contents is answering too many questions at once.
Split it.
