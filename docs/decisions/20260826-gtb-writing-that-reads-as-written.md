---
title: "ADR-20260826-gtb: writing that reads as written"
description: "Six constructions that mark this corpus as machine-written, each stated as the repair the writer owes and each gated. Rule 6 adopts WCAG 2.4.6 and GOV.UK for headings rather than banning shapes. Plus the fenced-example convention that lets a rules document quote what it forbids, and the baseline ratchet that adopts a rule the tree already fails."
date: 2026-08-26
status: accepted
tags:
  - adr
  - documentation
  - writing-style
  - enforcement
---
# ADR-20260826-gtb: writing that reads as written

- status: accepted
- date: 2026-08-26

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Extends [writing-style](../../.agents/rules/writing-style.md)

A reader finds machine-writing in this documentation without tooling. Four
families so far, and the most recent was a heading in an onboarding page that
turned an HTTP response into a metaphor. Earlier research
measured all four against 3.4 M words of human design documents and found the
signal is not any single phrase. Our score on the published excess-vocabulary
list is the lowest of five corpora, three of them human, while the spread of
marker rates across our documents is a third of theirs. Every document sounds
the same, and that is what a reader notices first.

So the rules below are budgets and repairs, and none of them is a banned word.
A rule that changes the spelling produces the next formula. A rule that names
what the formula skipped changes the sentence.

Rule 6 was itself a ban list once, and it lost. Four heading templates were
named one at a time, a ban was written for each of the first three, and the
next document arrived with a fifth. So rule 6 no longer says what a heading may
not be. It adopts a published standard that says what a heading is, and the
shapes are demoted to examples of failing it.

## rules

1. When a sentence sets one option against another, name the losing option and
   say why it lost in the same paragraph. Where the reason is not available,
   state the choice flatly and drop the contrast. A file uses the frame at most
   six times per thousand words.
2. Deliver a count where it is announced: a colon and the items, a list, a
   table, or one heading per item within four lines. Where the items are named
   in another document, link there and drop the count.
3. A sentence that says who does not decide says what does. Keep the form where
   the absence is itself the finding, at most twice per thousand words.
4. A paragraph opening with a bolded phrase that acts as a heading becomes a
   heading or loses the bolding, at most five and a half times per thousand
   words. A heading enters the table of contents, takes an anchor and has to be
   specific enough to grep for; a bolded lead-in avoids all three.
5. No run of twelve or more words appears twice in one document. Say it once
   and link to it. Quoting one source twice is not writing it twice, so
   quotation marks exempt a span.
6. A heading describes the topic or purpose of what is under it, and the word a
   reader is looking for comes first. This is the published standard named in
   `the standard rule 6 adopts` below, and not a preference this project
   invented. In practice a heading is descriptive, front-loaded, active and
   removable, and it is a statement, because a question cannot be front-loaded
   and the reader came for the answer. A heading naming what the writer did
   instead of what the reader will find is an uninformative heading. So is a
   noun phrase carrying a second heading after a comma, which defers the answer
   the way a question does.
7. A document that quotes a construction it forbids puts the quotation in a
   fenced block, and every prose gate skips fenced blocks. A marker a gate
   scans as bytes in a file format that has no fence is named by its code point
   and never spelled. The files a gate does not read are its own source and its
   baseline, it prints both names on every run, and a missing one fails the run.
8. A page a program writes names its generator and says `Do not edit`, both on
   one line, in its first twenty lines, and checks 4 to 9 skip it. Both halves
   on one line, because `do not edit` on its own is ordinary prose about a
   generated directory and matching it alone excused two hand-written pages
   nobody chose to excuse. A page without that line is prose and is held to all
   of it. Checks 1 to 3 read it anyway, because a generator that emits an em
   dash is a defect in the generator.
9. A check that the tree already fails ships as a baseline ratchet and never as
   a warning. `scripts/writing-style.baseline` carries one line per file per
   check, the gate fails when a count rises, and it fails when a count falls
   until the line is corrected. A file with no line is held to zero.

## the standard rule 6 adopts

Rule 6 is an adoption. Every line of it comes from one of these.

| source | what rule 6 takes |
|---|---|
| [WCAG 2.2 SC 2.4.6](https://www.w3.org/WAI/WCAG22/Understanding/headings-and-labels.html), Level AA | "Headings and labels describe topic or purpose" |
| the same, on length | "A word, or even a single character, may suffice" |
| [GOV.UK, clear structure](https://guidance.publishing.service.gov.uk/writing-to-gov-uk-standards/writing-guidelines/clear-structure/) | headings are "descriptive", "frontloaded", "active", "removable" |
| the same, on questions | headings should not "be questions", because "users want answers, not questions" |
| [Readability Guidelines](http://readabilityguidelines.wikidot.com/headings), Content Design London | "statements rather than questions in headings and putting the keyword first" |
| the same, on cleverness | "Play-on-words, idioms and colloquialisms can all be hard to decipher" |
| [Home Office, screen readers](https://ukhomeoffice.github.io/accessibility-posters/screenreaders) | do "write descriptive links and headings", never "uninformative" ones |
| [Home Office, autistic spectrum](https://ukhomeoffice.github.io/accessibility-posters/autism) | do "write in plain language", do not "use figures of speech and idioms" |
| [ISO 24495-1:2023](https://www.iso.org/standard/78907.html), clause 5.2.4 | "Use headings to help readers predict what comes next" |
| [WebAIM survey 10](https://webaim.org/projects/screenreadersurvey10/), 2024 | 71.6 % of screen reader users search a long page by its headings first |

This is why rule 6 is not a matter of taste. Most screen reader users reach a
section through the heading list before trying anything else, so a heading that
does not label its section is an accessibility defect. WCAG 2.4.6 is a Level AA
conformance criterion, and UK public sector accessibility regulations bind to it
through EN 301 549.

One departure, one extension and one caution on the citation:

- GOV.UK also asks that a heading carry no technical term. This project does the
  opposite and follows [ADR-20260826-shb](20260826-shb-a-document-is-read-at-speed.md) rule 3:
  the audience tier decides which terms may stand undefined, never how hard the
  English is. `envelope`, `intersection` and `sdk/eventsapi` are what a reader
  greps for.
- Content Design London's own site went offline in 2026 and its successor wiki
  is behind a proof-of-work wall. The link above is the archived alpha, which
  says so itself. Quote it as archived.
- No source rules against a noun phrase with a comma tail by name. Rule 6
  extends the anti-question reason both GOV.UK and Content Design London give,
  because `The tiers, and how many there should be` is a question with the
  question mark filed off. That extension is this project's and is not quoted
  from anywhere.

## enforcement

`make style-check` runs every check from the pre-push hook and in CI. Checks 1
to 3 predate this record.

| rule | check | what it flags today |
|---|---|---|
| 1 | 8, a per-file rate over `rather than` and the bare comma form, cap 6.00 per thousand words, files of 800 prose words or more | 8 files, from `00-requirements.md` at 9.07 down to `19-legacy-behaviour-contract.md` at 6.06 |
| 2 | 5, an awk lookahead for a count with no colon, list, table or heading within four lines | 3 lines, all in earlier research |
| 3 | 9, a per-file rate over the absent-agent family, cap 2.00 per thousand words | 3 files |
| 4 | 6, a per-file rate over paragraph-opening bold, cap 5.48 per thousand words, the human control's 99th percentile | 9 files, the worst at 32.01 per thousand words |
| 5 | 7, a maximal repeated-phrase scan at twelve words, one file at a time, outside fences, headings, blockquotes, table rows and quotation marks | 76 repeats across 17 files, the longest 30 words |
| 6 | 4, three heading templates: a category or the reader's position, a narrated figure of speech, and the writer's activity as a comma tail | 0, after the sweep this record ordered |
| 7 | the fence filter every markdown check reads, and a guard that fails when the baseline file is missing | 0 |
| 8 | the `generated by ... do not edit` filter, which the six generators already write on one line | 6 generated pages skipped by checks 4 to 9 |
| 9 | the ratchet in `ratchet()`, which fails on a rise, on a fall and on a line matching nothing | 53 baselined lines |

Every one of those was watched fail on a document written to break it before it
was trusted green: an em dash in prose, an HTML comment about what the code
returned before, a comment addressing the reader, two empty headings, a count
followed by four lines of prose, a twelve-word run repeated, and a 900-word file
over each of the three rate caps. The same probe inside a fenced block produced
no hit, which is rule 7 working. That pass found two defects, and they are the
reason it is worth naming. Check 5 stopped looking after two blank lines
anywhere in the lookahead. Check 7 dropped every repeat longer than twelve
words, so the 30-word and 28-word repeats now in the baseline were invisible to
it.

Check 4 was rebuilt when rule 6 was rewritten and watched fail again on a fresh
probe of ten headings. The six that must fail did: `Start here`, `How a failure
becomes an answer`, `The baseline, measured`, `The envelope claim, tested`, `The
three options, weighed` and `The decision trace, counted`. The same six inside a
fenced block produced no hit. The four that must pass did: `One rule, checked at
the edge`, `Discovery, distribution, provenance`, `app, the composition root`
and `Why the graph comes before the hub`. The first of those four is why the
participle list is closed: a tail describing the code is not a tail describing
the author, and a pattern that cannot tell them apart would have taken a heading
that other files link to by its anchor.

## rejected

| option | why not |
|---|---|
| a banned-word list from the popular AI-tell articles | measured in earlier research: our rate on Kobak et al.'s 407 annotated style words is the lowest of five corpora, and `delve`, `intricate`, `seamless`, `pivotal`, `meticulous` and `showcase` are zero, combined, in 471,634 words. The list bans words this project does not write and misses every construction it does |
| ban the contrast frame outright | 2,144 occurrences and none of 30 sampled had an empty second half. A ban would produce worse prose, and `writing-style.md` states its own rule in the form it would ban. The density is the tell, so rule 1 is a budget |
| ban the absent-agent family | *"a rule nobody has seen fire"* is the clearest way to say that an untested gate is untested, and firmware this project does not update on its own schedule is a fact about the product. Budget only |
| a word-substitution table, one banned phrase to one approved swap | a swap table mints the next formula, and somebody greps for that one in six months. The repair is to know the construction and rewrite the sentence |
| gate the tricolon | 1.3x against the Kubernetes control, and no pattern separates a rhetorical triple from a list of three real things. This repository has many of the second |
| gate the paragraph-final summarising sentence | measured below every human control. Folklore |
| gate `which is the` at 199 occurrences | 18.5x the control and it looks bad, but the sampled instances are restrictive relatives doing real work. Deleting one costs a clause and gains nothing |
| gate exact reuse across documents | 514 instances, and 30 of 30 sampled were an ADR quoting its own rule or a generated page agreeing with its companion. Rule 5 is within-file only |
| a document length cap | the median is ordinary and only the tail is long, because the subject is |
| require a heading's nouns to appear in its own section | executed, then dropped: scored over every heading in `docs/`, it puts `Review log` and `Data loss and escape` in the same band as the headings this record exists to remove. It separates nothing, so rule 6 gates the templates and states the reach the check does not have |
| ship rule 2 as a warning, per earlier research | a warning is how a gate dies. Tightening the lookahead to accept a colon, a list, a table or a heading dropped it from 17 lines to 3, all three of which announce a count and deliver prose |
| a per-file exemption list in the gate | a file-level opt-out is invisible and nothing fails when it goes stale. `--exclude-dir=rules` was one, and it is deleted: `.agents/rules/` is now scanned and is over the rule 1 budget in two files |
| an inline `style-check: ignore` marker | it needs parsing in nine checks, and a marker a writer can type anywhere becomes the way to silence the gate |
| put a quoted marker in an inline code span | measured: check 1 does not tokenise, so a code span does not help. A fence does, and an example that drifts out of its fence starts failing, which a stale exemption cannot do |
| rewrite the corpus to a threshold | 471,634 words. A rule nobody complies with is worse than no rule, which is what rule 9 exists to avoid |
| a heading rule this project writes itself | tried and lost. Four heading templates were named one at a time, a ban was written for each, and the next document invented the fifth. 482 of 3,466 headings carried a comma and the shape repeated. A ban list can only hold what somebody already named |
| keep rule 6 as the list of banned heading shapes | the shapes are now examples under a rule, and the rule is WCAG 2.4.6. A citation survives an argument and a preference does not, and 71.6 % of screen reader users reach a section through the heading list, which makes this conformance and not taste |
| GOV.UK's rule that a heading carries no technical term | our reader greps for `envelope`, `intersection` and `sdk/eventsapi`. ADR-20260826-shb rule 3 already settles it: the audience tier decides the terms, never the English |
| a question as a heading | GOV.UK and Content Design London both refuse one, for the same reason. A question cannot be front-loaded, and the reader came for the answer. Existing questions are left where they stand |
| a maximum heading length | no source in the adopted set states one. WCAG 2.4.6 says the opposite, that "a word, or even a single character, may suffice". Nielsen Norman's 40 to 60 characters is a page title and a search-ranking figure |
| gate the comma in a heading | 482 of 3,466 headings carry one and most are a list, an appositive or a pair of imperatives. The comma is not the defect. Check 4 reads the tail |

## open items

- `.agents/rules/engineering-standards.md` and `go-standards.md` are over the
  rule 1 budget, and earlier research measures the whole directory running
  the contrast frame at 5.3 and the absent agent at 0.87 per thousand words,
  both above `docs/`. The rule files seed two of the four families and are about
  50 edits. They bind every running agent, so the edit is a task on the old backlog and
  not a side effect of this record.
- Check 4 holds a floor under rule 6 and cannot hold the rule. Whether a
  heading describes the section beneath it is a judgement over two texts. The
  sweep that rewrote rule 6 changed 222 headings across 69 files, and the
  templates could see 59 of them. The other 163 were a comma tail that is not a
  reporting verb, a relative clause, a tail that names a notation, or a phrase
  that reads as written. A reviewer holds that half and no gate is proposed for
  it.
- `scripts/writing-style.baseline` is down to one line, `test/README.md` on
  check 7. The 53 lines this record shipped with were cleared by the sweeps on
  the old backlog and by the heading sweep.
- Rule 6 reaches a file name as well as a heading, and no file name in the
  tree carries a banned form. `docs/src/content/docs/plugin-architecture.md`
  is settled: its title is now `Writing an extension`.
- Comments under `internal/**` run the contrast frame at 4.64 per thousand
  words against the Go standard library's 0.16, the largest ratio earlier research
  found. `writing-style.md` states in writing that comment prose is not gated,
  and this record does not overturn that.
- Rule 5 fires on a status stamp repeated across parallel findings in one list,
  which `the old backlog` carried twice for two different escapes. Rewriting the second
  was an improvement, and the next instance may not be.
- The ratchet compares counts exactly, so two changes made at the same time
  against one document both have to touch `scripts/writing-style.baseline`. That
  is a conflict on a number and it is the price of a ratchet that cannot go
  stale.
