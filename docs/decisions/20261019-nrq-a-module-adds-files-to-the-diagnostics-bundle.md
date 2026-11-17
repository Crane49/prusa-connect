---
title: "ADR-20261019-nrq: a module adds files to the diagnostics bundle"
description: "A module that serves diagnostics.collect hands the host text files in pages; the host scrubs them, bounds them and writes them under modules/<kind>.<name>/ in the bundle when the admin includes extension files."
date: 2026-10-19
status: accepted
amends: [ADR-20261015-mde]
tags:
  - adr
  - modules
  - observability
  - security
---
# ADR-20261019-nrq: a module adds files to the diagnostics bundle

- status: accepted
- date: 2026-10-19

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends: [ADR-20261015-mde](20261015-mde-a-diagnostics-bundle-is-redacted-in-five-layers.md) rules 2 and 3

A module can hold the best evidence of a fault. The printer logs module keeps
the log lines a printer sent in the last day. The diagnostics bundle of
ADR-20261015-mde holds only what the core knows. This record lets a module add text
files to the bundle. The core asks for them, scrubs them and limits their
size, and the admin decides whether they go in.

## rules

1. A module that adds files names `diagnostics.collect` in `Info.Serves`. A
   module that does not name it adds nothing and is never called.
2. The host calls it while a bundle is built, once per page, on each running
   instance that serves it. The params are `{"cursor": "<text>"}`, empty on
   the first call. The answer is `{"files": [{"name", "text"}], "cursor"}`.
   An empty cursor in the answer is the last page.
3. A file name is a relative path of letters, digits, `.`, `_`, `-` and `/`,
   at most 128 characters, with no `..` segment. A segment may not end with
   `.` or name a Windows device, such as `con` or `aux.log`, so a bundle
   unpacks the same on every system. Text with the same name on
   a later page is appended to that file. An entry with empty text adds no
   file. A page holds at most 512 entries. The host refuses a new name that
   differs from an earlier one only in case. It also refuses a name that uses
   an earlier file as a folder, as `a/b` does after `a`, and the other way
   round. Opened on a disk, such a name would overwrite a file.
   `manifest.json` counts the refused entries as `refused`.
4. The host asks one module, writes its files and only then asks the next,
   so a bundle holds one module's files at a time. It writes them under
   `modules/<kind>.<name>/`, such as `modules/sink.buddy-debug-logs/`. Neither
   part holds a `.`, so two instances never share a folder. `manifest.json`
   lists each module it asked, the files it wrote and their sizes, and a
   `truncated`, `failed` or `skipped` reason where one applies. A truncated
   module also names the `limit` it reached: `size`, `files`, `pages` or
   `time`.
5. The host takes at most 8 MiB, 1024 files and 64 pages from one module.
   Each new file name costs its length plus 128 bytes of the 8 MiB, so a
   module cannot fill memory with names. The host stops at the limit and
   records `truncated`. A module puts what matters most first, which for a
   log is the newest lines.
6. Each call has the wire's 10 second deadline. One module has at most one
   minute and all modules together at most three minutes. When the request
   has a deadline, the modules have at most half of the remaining time, so
   the bundle is still built and sent. A module stopped by its time keeps
   what it gave and is recorded as `truncated` by `time`. The modules after
   the three minutes are recorded as `skipped`. The host ends a call for
   the bundle's time with a cancel, never a deadline, so the module's
   breaker does not count it. A module that fails or times out on its own
   is recorded as `failed` with its fault code. In every case the bundle is
   still built. When the bundle's request is cancelled, the bundle stops.
7. Every line passes layer 3, the value patterns, and layer 4, the
   known-secret sweep, of ADR-20261015-mde rule 4. Then it is cut to the length of a
   log line. Layer 2 does not apply, because the text has no log keys.
   Layers 3 and 4 also check each part of a file name. A part they would
   change, such as a MAC or an address, becomes `h-` and a hash of it, with
   a short extension kept. The zip and `manifest.json` use the same hash.
8. The files go in only when the admin includes them: `extensions=true` on
   `POST /api/v1/diagnostics`. The Diagnostics page offers the choice ticked,
   next to the module output choice.
9. `GET /api/v1/diagnostics` answers `extensions`, the titles of the running
   modules that serve `diagnostics.collect`, so the page names them. It takes
   the admin role on the organization, because the list names installed
   modules. `GET /api/v1/ui` takes no credential and so never names them.

## enforcement

| rule | gate | state |
|---|---|---|
| 2, 3 | `sdk/diagnosticsapi` tests: pages, names, the cursor, the entries on a page | exists |
| 3 | `internal/app/diagnostics` extension tests: a name that clashes in case or as a folder is refused | exists |
| 4 | `internal/app/diagnostics` extension tests: two instances of two kinds with one name keep their own folders | exists |
| 4, 7 | `internal/app/diagnostics` extension tests: a secret, an address and a MAC in a file and in a file name | exists |
| 7 | `internal/app/diagnostics` extension tests: a long line is cut after the sweep | exists |
| 5 | `internal/app/diagnostics` extension tests: the size, file and page limits, entries with no text | exists |
| 6 | `internal/app/diagnostics` extension tests: a module that fails keeps what it gave | exists |
| 6 | `internal/app/diagnostics` extension tests: the minute, the three minutes, half the request's time | exists |
| 6 | `internal/app/diagnostics` extension tests: a module out of time sees a cancel, not a deadline | exists |
| 6 | a module that hangs on one call is cut by the supervised call deadline. No test holds it | planned |
| 8, 9 | `internal/transport/http/httpapi` diagnostics tests: the choice, the list, a caller who is not an admin | exists |

## rejected

| option | why not |
|---|---|
| one answer with every file | a wire frame is at most 1 MiB, and a day of logs is larger |
| a route the host calls as the admin | a route answer is also at most 1 MiB, and the admin's identity would let the module decide what the admin sees |
| the module writes into its data directory and the host reads it | the module could not tell when a bundle is wanted, and a module without a data directory could add nothing |
| module files always included | they come from code the core does not review, so the admin decides, as for module output |
| every module asked before the zip is written | the bundle would hold 8 MiB from every module at once |
| a deadline for the bundle's time | the supervised call counts a deadline it did not set as the module's failure, against its breaker |
