---
title: "ADR-20260827-xca: a fleet says what is wrong with it"
description: "That a device record carries a closed issue reason and no sentence, that a fleet listing carries the rollup over the caller's whole authorized population, that an issue is read from what the process already holds, and that a departed device carries one issue."
date: 2026-08-27
status: accepted
tags:
  - adr
  - api
  - devices
  - observability
---
# ADR-20260827-xca: a fleet says what is wrong with it

- status: accepted
- date: 2026-08-27

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md), [ADR-20260826-c77](20260826-c77-one-name-for-one-thing-on-the-wire.md), [ADR-20260827-wgm](20260827-wgm-a-caller-may-ask-what-it-may-do.md), [ADR-20260826-5yj](20260826-5yj-a-device-is-the-object-and-a-printer-is-the-type.md)

A person loading a fleet page wants one thing first: which machines need
somebody. Every fact that answers it was already in this process and none of it
was on the wire. A printer waiting at a dialog looked exactly like an idle
printer. A printer no capability profile covered looked like one with fewer
features. A client told them apart only by fetching each device and reading its
state tree against paths a driver chose.

The shape that answers this badly is a second endpoint that re-derives the same
facts, or a per-device health call. Four hundred printers is the fleet this
product claims, and a call per printer is four hundred round trips and four
hundred statements for one page.

## rules

1. A device record carries an `issues` array. Each entry is a `reason` from one
   closed set and the `severity` that reason is declared with.
2. The array is ordered by severity, worst first, so a client showing one badge
   shows the first entry. An empty array is a device with nothing wrong, and it
   is an array and never an absent member.
3. An issue is an index into the record it sits on and never a second copy of
   it. `departed` says when a session ended, `link` carries the rates and
   `state` carries the dialog. A reason names which of them to read.
4. An issue is read from what this process already holds for a device it has
   been dialled by. Nothing on the path from a reason to a client may query
   anything, so a fleet's worth of issues costs the statements the listing
   already cost. See [the cost](#the-cost).
5. A device record carries no sentence. The summary, the description and what a
   person does about it belong to the vocabulary, which a client reads once and
   a translator translates. An English sentence repeated on four hundred records
   is bytes no translator can reach.
6. `detail` is the exception, and it carries only what the machine itself said.
   It is the machine's vocabulary rather than this API's, so it is shown to a
   person and never branched on, exactly as `machine_reason` is.
7. A fleet listing carries the rollup of issues over every device the caller may
   see, whatever page was asked for and whatever filter was applied. A badge
   that counted one page changes when somebody turns to page two.
8. The rollup publishes a count for every reason this build knows, including
   the ones at zero. A client reading a key it expects never has to tell zero
   from absent.
9. The rollup counts only what the caller may see. It is computed over the rows
   the authorized walk produced, so it discloses nothing the listing does not.
10. A filter that narrows the listing to the devices carrying an issue moves the
    items and the total, and never the rollup. A client filtering to the
    problems still learns how large the fleet behind them is.
11. A departed device carries exactly one issue, that it has departed. The rest
    of its record is the last reading this server took. Reporting an hour-old
    dialog as work waiting for somebody sends a person to a machine that is
    switched off.
12. The reasons are a vocabulary in [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md)'s
    sense, with every obligation that carries: a static descriptor table, a gate
    running both directions, a generated page and an endpoint under
    `/api/v1/vocabularies/`.
13. Every entry in that vocabulary says what a person does about it. A reason a
    reader cannot act on is a badge with nothing behind it, and the generator
    refuses one.
14. The `/dialog/` subtree of the state tree is reserved. It is the one part of
    that tree the host reads rather than passes through. `/dialog/id` says a
    dialog is up. `/dialog/title`, `/dialog/text` and `/dialog/code` are what
    the machine displayed. A driver that raises a dialog addresses it there.
15. A severity belongs to a reason and never to one device. The projection reads
    it from the descriptor table, so the value on the wire cannot disagree with
    the value the vocabulary publishes.

## the cost

This is a measurement. `TestTheFleetListingCostsNoStatementPerPrinter` counts
the statements a wrapped `database/sql` driver carried to SQLite while the
listing answered. The fleets are 1, 100 and 400 printers, a tenth of each
stopped at a dialog:

```text
1 printers,   a page and the rollup:          1 statements
100 printers, a page and the rollup:          1 statements
400 printers, a page and the rollup:          3 statements
400 printers, only the printers with issues:  3 statements
400 printers, the far end of the fleet:       3 statements
```

The listing walks the authorized directory in windows of 200 rows, so the cost
is one statement per window and one more for the window that comes back short.
It does not move with the number of devices that are wrong, with the page asked
for, or with the filter. The gate is written against the window and not against
those five numbers. A walk that grew a statement per device then fails at every
size, and not only at the size somebody remembered.

## enforcement

| rule | gate |
|---|---|
| 1, 2, 15 | `TestEveryIssueTheDevicePlaneRaisesIsInTheClosedSet` in `internal/app`, over the wire and not the table |
| 2 | `TestTheTableIsInDescendingSeverityOrder` in `internal/kernel/deviceissue` |
| 4 | `TestTheFleetListingCostsNoStatementPerPrinter` in `internal/app` |
| 7, 9, 10 | `TestTheIssueRollupCountsOnlyThePrintersTheCallerMaySee`, `TestTheIssueFilterNarrowsThePageAndNotTheRollup` |
| 9 | `TestACallerGrantedNothingLearnsNothingFromTheRollup` in `internal/app` |
| 11 | `TestADepartedPrinterCarriesOnlyTheDepartedIssue` in `internal/app` |
| 12, 13 | `make device-issues-check`, both directions, plus `go test ./cmd/generate-device-issues/` |
| 14 | `TestAPrinterWaitingAtADialogNeedsAnOperator`, which raises a dialog on the reserved paths |
| 1 to 15, end to end | `TestTheFleetListingReportsADialogAsAnIssueOverHTTP` in `internal/app` |
| the link reasons | `TestAPrinterSpeakingAnUnreadableDialectIsReportedAsAnIssue`, over real frames |
| 3, 5, 6, 8 | none. Whether a member restates another is a judgement over two texts |

## rejected

| option | why not |
|---|---|
| a second endpoint answering what is wrong with the fleet | it re-derives facts the listing already produced, and the two answer differently the moment one of them changes |
| a health call per device | four hundred round trips for one page, and the fan-out cost falls on whoever is loading a screen |
| the rollup over the current page only | a badge that changes when somebody turns to page two is a badge people stop trusting |
| an English sentence on every issue | it cannot be translated and it is repeated once per device. The vocabulary is fetched once and cached |
| reusing the error-code vocabulary for the reasons | an error code is what a request failed with, and a printer at a dialog is no failed request |
| reusing `condition.Type` and `condition.Reason` | that set is open by design, so a client could not branch on it and no gate could close it |

### A severity carried per device rather than per reason

A per-device severity would let one deployment call a filament runout a warning
and another call it an error, which is the configurable-vocabulary shape the
core does not have. It also gives a client two places to read one fact, and the
one it reads is whichever the wire happened to carry. The severity is declared
beside the reason and the projection reads it from there.

### A threshold on the link rates

`frames_refused` and `telemetry_dropped` are raised on any rate above zero over
the window the reading was measured across. The rate already carries that
window, so a refusal an hour ago decays out of it. A number picked in this
document would be a guess about a dialect nobody has met. A deployment that
finds the threshold wrong has the rate on the same record and can set its own.

### An issue derived from the device's own state string

`/state` carries what the firmware calls itself, and that is the driver's
vocabulary. A core rule reading `ATTENTION` would put one dialect's words in a
closed set the core owns. A second driver spelling it differently would then go
unread, or force the set open. The dialog subtree is read instead, because the
host already treats a dialog as first class: it holds a queue slot that cannot
be evicted.

## open items

- resource instances carry conditions and a derived state, and nothing joins
  them with device issues. A client that wants everything wrong on one screen
  reads `/api/v1/printers` and `/api/v1/resources` and merges them. A third
  answer covering both would re-derive what those two already produce, which
  rule 4's reason refuses. So the join belongs to whatever renders the screen,
  until a client exists that cannot do it. The row is on
  the old backlog
- no issue carries when this server first observed it, except a departure, which
  carries it under `departed.at` already. The device plane records no transition
  time for a dialog or for a link going quiet, so "broken for an hour" cannot be
  shown or sorted on. The row is on
  the old backlog
- the reasons are raised for printers only. `kind` is already on every device
  record, and a camera or a node that needs attention has nowhere to say so
