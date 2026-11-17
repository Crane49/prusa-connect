---
title: "ADR-20260925-gv7: the printer build is a profile beside the normal build"
description: "How the binary for a printer that ships with Prusa Connect is built and shipped, which defaults it changes, how modules run on it, and the parts it may leave out once the owner confirms each one."
date: 2026-09-25
status: accepted
tags:
  - adr
  - build
  - memory
  - printer
---
# ADR-20260925-gv7: the printer build is a profile beside the normal build

- status: accepted
- date: 2026-09-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260826-gtv](20260826-gtv-build-tooling.md), [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20260901-7t4](20260901-7t4-build-time-adapter-composition.md)

A printer that ships with Prusa Connect gives it 15 to 20 MB of memory. Every build
measured in earlier research uses 44 to 52 MB of resident memory on Linux. The dummy
password hash and the API document are made only when they are needed. A build
with both changes ran five printers under a hard limit of 28 MB. At 20 MB it
spent most of its time reading its own code back from disk. This record adds
a printer build that changes defaults and may leave parts out. The normal
build keeps everything it has today. The owner confirms each part the printer
build leaves out. Rules 27 to 30 hold his answers, and the section at the end
lists what is still open.

## rules

### how the printer binary is built

1. Build the printer binary with `make build-printer`. It writes
   `bin/server-printer`.
2. Keep every other build as it is. `make build`, `make build-full` and
   `make build-minimal` keep everything they carry today and read no printer
   default.
3. Build the printer binary with the tags `full` and `printer`. Add the tags
   that `cmd/detect-build-tags` finds in the printer configuration file.
4. Give each part the printer build leaves out its own tag, whose name starts
   with `no`, such as `nowebrtc`. List those tags in one Makefile variable,
   `PRINTER_TAGS`.
5. Add a tag to `PRINTER_TAGS` only after the owner confirms that part. The
   comment beside the tag names the rule of this record that leaves it out,
   such as `ADR-20260925-gv7 rule 27`. The list holds the tags of rules 27 to 29.
6. Let the `printer` tag change default values only. It removes no code and no
   route.
7. Build the printer binary static, with `CGO_ENABLED=0` and
   `-ldflags="-s -w"`, for linux on arm64. Add an architecture when the printer's
   operating system names one.
8. Embed the web app in the printer binary the same way the normal build embeds
   it. The printer serves the same app as every other build.
9. Make the web app smaller for every build at once. Do not make a second,
   smaller app for the printer. Earlier research is the example. A smaller
   History chart makes every page smaller on every build.
10. Give each printer target its own row in `scripts/check-binary-budget.sh`.
11. Publish the printer binary with each release as
    `prusa_connect_printer_aarch64`, beside the other release files.
12. Ship the printer configuration as `config/examples/config.printer.yaml`.
    The printer's operating system installs it together with the binary.

### what the printer build does differently

13. Give every cache and buffer that can grow past 1 MiB a setting. Its default
    in a normal build is today's value. The `printer` tag sets a small default.
14. Bound the print file preview cache with a setting. Its normal default is
    today's 32 MiB. Its printer default is 1 MiB.
15. Set `persistence.max_open_conns` to 4 and `persistence.max_idle_conns` to 2
    in the printer configuration. The normal defaults stay 16 and 8.
16. Add a setting for the SQLite page cache of one connection. Its normal
    default is SQLite's own 2 MiB. Its printer default is 512 KiB, so four
    connections hold at most 2 MiB.
17. Make the argon2id memory and the number of passes settings. The normal
    defaults stay 19 MiB and 2 passes. The printer defaults are 7 MiB and 5
    passes, which OWASP lists as equal in strength.
18. Read the configured argon2id values in a new hash, in the dummy hash for
    an unknown account and in `NeedsRehash`. A hash stored with other values is
    replaced at the next login.
19. Add a setting for how many password hashes may run at once. The normal
    default stays 4. The printer default is 1, and a login past the bound
    waits within its own request deadline.
20. Add `core.memory_limit`. When it is set, the core calls
    `debug.SetMemoryLimit` with it at start and logs the limit. When it is not
    set, Go keeps its own default, which reads `GOMEMLIMIT`.
21. Set `core.memory_limit` in the printer configuration. Its value is 12 MiB
    until the board is measured, because earlier research builds its estimate for
    20 MB on that value.
22. Treat the printer values in rules 14 to 21 as starting values. The
    measurement on the printer's board may change them in
    `config/examples/config.printer.yaml` and in the `printer` defaults. The
    commit that changes one names the measurement.

### how modules run on the printer

23. Run a module on the printer as its own process, as ADR-20260827-wmv rules 3 and 5
    say for every build. Each one costs about 4.4 MB, which earlier research
    measured for the filament module at idle.
24. Keep the module host in the printer binary. A person may still install a
    module on the printer and pay its memory.
25. Declare a module in the printer configuration only after the owner confirms
    that module for the printer.
26. Build a feature that ADR-20260827-wmv rule 2 places as an adapter into the printer
    binary with `make compose-build`, under ADR-20260901-7t4. It then runs in the
    server's own process.

### what the printer build leaves out

27. Leave the API reference page out of the printer build with the tag
    `noreference`. The printer does not serve `/docs` or its script.
    `/openapi.json` stays, so phone apps and generated clients keep working.
28. Leave the developer tools out with the tag `nodevtools`. The printer then
    has no `/dev` page, no driver list and no raw command operation.
    `make api-check` reads the document of the normal build only. A test
    holds the printer build's document to it, without the developer
    operations.
29. Leave the capability profiles of other printer models out with the tag
    `noothermodels`. The build keeps only the profile of the model that
    `PRINTER_MODEL` in the Makefile names.
30. Keep WebRTC in the printer build, because without it a browser cannot show
    the live view of an H.264 camera.

## enforcement

| rule | gate |
|---|---|
| 2 | `TestOnlyThePrinterTargetBuildsWithPrinterTags` in `internal/arch` |
| 6 | `TestOnlyTheBuildProfileReadsThePrinterTag` in `internal/arch` |
| 5 | `TestEveryPrinterTagNamesItsRule` in `internal/arch` |
| 10 | `make binary-check`, whose `arm64:printer` row builds with the printer tags |
| 12 | the strict loader refuses a key the build does not read |
| 27 | `TestThePrinterBuildAnswersNotFoundAtTheReference` in `httpapi`, which `make test-unit` runs with the printer tags |
| 28 | `TestThePrinterBuildServesNoDeveloperRoute` in `httpapi` |
| 28 | `TestThePrinterBuildPublishesTheNormalDocumentWithoutTheDeveloperOperations` in `httpapi` |
| 29 | `TestThePrinterBuildLoadsOneProfile` in `internal/printer/profile` |
| 13 to 21 | `make config-reference-check`, which fails when the reference and the settings differ |
| the rest | none. Review holds them |

## rejected

| option | why not |
|---|---|
| small defaults in the normal build too | a desktop or a server has the memory, and the normal build keeps what it has |
| an agent decides what the printer build leaves out | the owner confirms each part |
| `GOMEMLIMIT` in the service environment only | an operator can check a limit the log states. Earlier research asked for a key |
| the web app as files on disk beside the binary | it saves 2.4 MiB of binary and no resident memory, measured. the printer's operating system ships more files |
| one process that hosts every module | a fault in one module stops every module, and the wire has no such host today |
| a module compiled into the printer binary | ADR-20260827-wmv rules 3 and 5 refuse it for every build |
| leaving out huma now | a rewrite of the HTTP layer for 2.9 MiB of heap and 2.1 MiB of code, estimated |
| replacing SQLite now | saves about 1 MB of data and 2.5 MB of code, measured. Cgo ends the static binary |

Huma and SQLite come back after the measurement on the printer's board.

## open for the owner

The printer build runs no module process until the owner confirms that module.
Each module is one confirmation, and rule 23 gives its cost.

The shipped configuration declares GraphQL and the older Prusa Connect shapes.
The filament and queue modules are being built. Without a confirmation, the
printer does not offer the feature of that module.

## open items

- Earlier research measured on amd64 under WSL2 with simulated printers. The printer values wait for a run on the printer's board.
- How the printer's operating system enforces the budget is not known. A cgroup limit counts code pages, and a figure for resident memory alone does not.
