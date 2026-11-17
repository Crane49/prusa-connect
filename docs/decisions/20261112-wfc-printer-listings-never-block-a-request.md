---
title: "ADR-20261112-wfc: printer listings never block a request"
description: "The route that lists a printer's folder answers at once from memory, a read runs in the background and says when it lands, and the printer's own file events patch the one folder they name."
date: 2026-11-12
status: proposed
supersedes: []
amends: []
tags:
  - adr
  - printer
  - files
---
# ADR-20261112-wfc: printer listings never block a request

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260826-ezr](20260826-ezr-the-file-vocabulary-is-dialect-neutral.md), [ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md), [ADR-20260828-rp4](20260828-rp4-a-published-event-type-declares-its-payload.md), [ADR-20260901-atx](20260901-atx-durable-command-records.md), [ADR-20261001-pdr](20261001-pdr-a-listing-says-how-it-may-be-filtered.md), [ADR-20261023-a52](20261023-a52-the-tabs-of-a-browser-share-one-event-stream.md)

A Prusa printer on Buddy firmware answers one command at a time, and an MK4S
takes 12 to 15 s to list a USB root of about 300 files. The route that lists
a folder used to wait for that read, up to the 30 s request budget. A large
stick passed the budget, and the page got a fault with no way to retry. A send
also paid for its own relistings: about 35 s for a 42-byte file.

A read of a printer's folder is slow and cannot be made fast. So a request
must not wait for it, and the server reads ahead of the person. The printer's
own events keep what is held current, as in Prusa Connect. It sends an `INFO`
when a USB drive goes in or out, a `FILE_INFO` for a file created, and a
`FILE_CHANGED` for any other change (firmware `planner.cpp`, `ChangedPath`).

## rules

1. `GET /api/v1/devices/{id}/files` never waits on the printer for longer
   than `listingGrace` (1 s), and only for a read it started with nothing
   held. It answers from memory with `stale`, `reading` and `reading_since`,
   or with an empty listing that says `reading: true`.
   `GET /api/v1/devices/{id}/storages?refresh=true` waits the same grace for
   the printer's answer and then answers the storages held. An answer that
   lands later is published as `storages.changed` when it differs.
2. A read that failed or did not finish is held for its folder and answered as
   `fault`. A read that did not finish answers with the retryable code
   `core.printer/LISTING_UNFINISHED`. Retry is `refresh=true`, which starts or
   joins a read and answers at once. A caller that asks again gets the same
   held answer until the next event, not a new read.
3. A read that begins, one that lands (`failed` true or false), and a folder
   this server patched or forgot each publish
   `com.prusa.connect.device.files.changed` (state class). The payload names
   the device, the storage-prefixed folder and whether a read is on the wire
   or has failed. It carries no rows; the page reads the held listing.
4. A listing that landed is held for `listingHold` (60 s) whatever the byte
   bound says, so the page told it landed finds it. An oversized one is held
   for that window only.
5. A write this server makes, and a change the printer reports, patch the one
   folder they name, and do not drop every listing of the printer. The
   printer names an entry by its 8.3 path; each segment is matched to the held
   rows of the folder above it, and a folder not held is not patched. A
   `FILE_CHANGED` with `rescan` reads its held folder again in the background.
   A report that describes a row as it is held changes nothing, so a
   thumbnail read does not refuse a read in flight. A send that landed adds or replaces the
   entry, as the printer describes the file or, when it does not, as sent. A
   send refused before any byte (a name already taken) forgets its
   folder, since the printer's content is no longer known to match. The
   whole-device drop stays for link lost, retire and a storage that went away.
6. The landing readback of a send asks the printer for the file itself first
   (2 s). It falls back to the folder readback when the answer does not prove
   the landing. A question that runs out of its 2 s finishes the send with a
   warning and no folder readback. The verdicts of ADR rules on the readback
   (BUG-014) do not change.
7. An idle printer reads the root of each storage it reports, once, in the
   background. There is one trigger: the xBuddy driver signals its storages on
   the first `INFO` of each connection. A printer that prints is never warmed.
8. Background printer work yields to a person. A read that revalidates held
   rows, a warm-up read and a thumbnail read run under
   `lane.Background` (`internal/kernel/lane`). The xBuddy driver keeps one
   queue in the order commands were asked, takes a person's command first, and a background
   command already on the wire runs to its end. It sends no
   background command while a transfer is open or within 250 ms of a person's
   command. The next step of a person's flow, such as a send's offer after its
   name check, then does not wait behind a read. A person who joins a read that waits in the background promotes it,
   which clears its mark where it stands, so it is queued once and sent once.
   A driver without priorities ignores the mark (ADR-20260826-ezr).
9. A client that waited on a folder reads it again after a gap in the event
   stream (ADR-20261023-a52), since an event may have been missed in the gap.
   The page waits for `files.changed` and does not poll.
10. The event goes through the vocabulary, the generator and its checks
    (ADR-20260827-qvk, ADR-20260828-rp4), and is visible to whoever may observe
    the device. Reading the listing still takes operate.
11. A folder is read again only when nothing is held for it, on Retry or
    Refresh (`refresh=true`), and on a `rescan`. The page opens a folder
    without refresh and loads the storages without asking the printer. A
    change made on the printer's own screen reaches no client, so the Files
    tab offers Refresh.

## enforcement

`internal/app/devicefiles_test.go` pins the answer at once, the held failure
and the grace, and `devicefilewatch_test.go` the printer's changes applied
under long names. `internal/connector/xbuddy/files_internal_test.go` pins how
a `FILE_CHANGED` is read. `sdk/connectorapi/lane_test.go` and
`internal/connector/xbuddy/lanes_test.go` pin the queue order, promotion and
the hold around a person's flow. The vocabulary, OpenAPI, SDK and compat checks fail
when the event or the new fields drift from the generated files.

## rejected

| option | why not |
|---|---|
| Raise the request budget | A stick that is bigger still passes it, and the page still waits with no retry. |
| Delay warm-up or hide the printer until it is read | A delay moves the risk. Hiding blocks every command. Now a command waits behind one read at most. |
| Ask again from the page until the rows come | Every ask is a request and a held answer is the same each time. The event says when to read. |
| Drop all listings of a printer after a write | A send then costs a 12 to 15 s root read. One folder is patched instead. |
| Warm up on three triggers (arrival, storages change, link back) | The first `INFO` of a connection already covers all three, so one trigger is enough. |
| Read the folder again on every visit, inside a window of freshness | A send waits behind each read, and the printer's events already say what changed. |
| Read the whole tree on connect, as Prusa Connect does | A full USB drive holds the printer for minutes. The roots are read, and a folder when it is opened. |
| Two queues, a person's and a background one | A promoted command sat in both, and its leftover copy was told it was never sent. |
