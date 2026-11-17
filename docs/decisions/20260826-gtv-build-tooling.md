---
title: "ADR-20260826-gtv: build tooling"
description: "That the build stays a single GNU make 3.81 Makefile, what a target is named, how it announces a missing dependency, and what bare `make` does."
date: 2026-08-26
status: accepted
tags:
  - adr
---
# ADR-20260826-gtv: build tooling

- status: accepted
- date: 2026-08-26

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) · Layout: [ADR-20260824-kkr](20260824-kkr-repository-layout-and-package-boundaries.md) · Style: [writing-style](../../.agents/rules/writing-style.md)

Bare `make` prints help, which rule 2 states, and does not run the pre-commit
gate. A stranger's first command in a fresh clone should tell them what to
type. It should not spend several minutes on a build, a frontend type check, a
lint and two test runs they did not ask for. Everything else here follows from
measurement.

## rules

1. Keep the build in one root `Makefile`. It shells out to `scripts/` for anything that is a program rather than a call, and it does not grow a `make/` fragment directory, a second include, or a Go rewrite while it stays under roughly 1,500 lines. Grafana ships the same tool at the same size and is readable.
2. Set `.DEFAULT_GOAL := help`. Bare `make` on a fresh clone prints what to type and builds nothing. Today the first target is `pre-commit`, so bare `make` runs a build, a frontend type check, a lint, unit tests and integration tests.
3. Use only features GNU make 3.81 has. `.ONESHELL`, `.SHELLFLAGS` and `.RECIPEPREFIX` arrived in 3.82 and are **silently ignored** by 3.81, so a recipe written against them runs one shell per line, drops the variables it set, and reports success. `.DEFAULT_GOAL` and `MAKEFILE_LIST` are safe.
4. Write `"$(MAKE)"` with the quotes, never bare `$(MAKE)`. On the Windows machine this repository is developed on, `MAKE` expands to `C:/Program Files (x86)/GnuWin32/bin/make` and the unquoted form is a shell syntax error, so every recursive target fails before doing any work. Never replace `$(MAKE)` with a literal `make`: that discards the binary make was invoked as.
5. Keep every `awk`, `sed` or `grep` recipe on a single line. A multi-line recipe works only while every continued line ends in a backslash and every continuation begins with a tab. A space-indented continuation reports `missing separator`, and a dropped backslash reports the shell's `unexpected EOF while looking for matching '`. Neither message names the command that caused it.
6. Document a target with `## description` after the colon, and group targets with a `##@ Group name` heading above them. `help` renders those and nothing else. Annotate the targets a person needs, not all of them.
7. End the help output with the line that points at the full target list, so an unannotated target is discoverable: help is a curated subset, and without that line it drifts the same way a hand-written list does.
8. Name a target domain first: `build-`, `test-`, `docs-`, `generate-`, `certificate-`, `server-`, `release-`, `clean-`.
9. Put a required tool **in the target name** only when producing that tool's artefact is the point of the target. `docker-check` and a compressed image build qualify. A test that happens to start a container does not.
10. State every other requirement in the `##` description and in the `##@` group. A description says the tool by name: `## (docker) build the image and prove it is hardened`, `## (pnpm) build every embedded frontend variant`.
11. A target whose dependency is absent names the tool in one line before it does anything else, and names the way around it where there is one. `_build-cross-full` is the model: it says Docker is required, says why (CGO plus glibc), and points at `VARIANT=minimal`.
12. Never turn a missing dependency into a warning that lets the target report success. `install-deps` degrading a failed `pip install` into `(optional)` and `status` printing `No active connections` when `lsof` is absent are both this defect.
13. Use `printf` for any string carrying an escape. `echo "a\nb"` prints the backslash under Git for Windows `sh` and prints a newline under a `sh` that expands escapes.
14. Write status markers in plain ASCII (`OK`, `FAIL`, `WARN`). The UTF-8 glyphs `✓`, `✗`, `⚠`, `→` and `─` render as mojibake through GnuWin32 make on Windows.
15. Mark a target that cannot run on every supported platform in its description, and say which platform. `status` needs `lsof`; a compressed cross-build needs a docker daemon and a host-path bind mount.
16. Keep `.PHONY` correct. Every phony target appears once, no name appears that is not a target, and no target goes unnamed. When this rule was written the declaration named two targets that did not exist and omitted three that did. `make makefile-check` holds all three directions.
17. Delete a target that nothing calls and that no person uses as an entry point. A duplicate of another target is always deletable.
18. Extract a recipe into `scripts/` once it stops being a call and becomes a program. Four recipes are over thirty lines today.
19. Never repeat a recipe. Two targets differing by one flag take one recipe and a variable.
20. The Makefile obeys `writing-style.md`, including the em dash rule. The exemption in that document covers banners and section separators, not punctuation.

## enforcement

| rule | gate | what it refuses |
|---|---|---|
| 2, 6, 7 | `make help` | nothing. Help renders what is annotated, and an unannotated target is invisible rather than refused. This is the weakest gate in the table and rule 7 is the mitigation |
| 3, 4, 5, 13, 14 | `make makefile-check` | one check per rule, written out below the table |
| 8, 9, 10, 11, 12, 15, 18, 19 | review | judgement calls a reviewer points at. None of them is mechanically decidable |
| 16 | `make makefile-check` | a `.PHONY` naming something that is not a target, a target `.PHONY` does not name, and a name listed twice |
| 17 | review | a caller search over `.github/`, `.githooks/`, `docs/`, `scripts/` and the Makefile's own prerequisites, before any deletion |
| 20 | `make style-check` | an em dash, a dated comment, a comment written as history |

`make makefile-check` refuses six things. A 3.82-only special target, for rule
3. A bare `$(MAKE)`, and a recipe re-entering the literal `make`, for rule 4.
Three continuation defects, for rule 5: one whose next line has no tab, a quoted
program split across one, and one flattened into a single line, which make runs
because the shell reads the leftover tab as whitespace. An `echo` carrying a
backslash escape, for rule 13. And any byte outside ASCII except U+2500 inside a
comment, for rule 14.

Rule 20 reaches these files through `SEARCH` in `scripts/check-writing-style.sh`,
which names `Makefile` and `Dockerfile*`. Checks 1, 2 and 15 hold all four.

Every check in `scripts/check-makefile.sh` carries an arming floor, and every
floor is per file as well as tree-wide. A tree-wide count alone stays over its
floor on the second Makefile while the first contributes nothing, which is the
failure `TestEveryGuardedStorageDirectoryIsScanned` refuses in `internal/arch`.

The honest summary: **rules 1, 2, 6 and 7 gate themselves by existing, rules 3,
4, 5, 13, 14, 16 and 20 are gated, and the nine judgement rules are held by
review.** Earlier research says why review is the accepted cost here. The
alternatives replace a review burden with an install burden.

## rejected

| option | why not |
|---|---|
| `just` | another binary a contributor installs before the first build, which is the exact friction `_ensure-pnpm` exists to remove for pnpm. Its own claim against make, "no need for `.PHONY` recipes", is a fair point (rule 16 exists because this file's `.PHONY` is wrong in five places) and is not worth the dependency |
| Task, with its embedded `mvdan/sh` | the strongest alternative, and it genuinely removes the Windows shell problem rule 4 works around. Rejected because rule 4 costs thirteen quote characters, and because every gate, hook and CI job in this tree already calls `make`. Revisit if a second shell portability defect of this size appears |
| Mage, or a Go program under `tools/` | the build tool would need the toolchain it is bootstrapping. Hugo's magefile is 8 KB against this file's 30 KB of behaviour, so the two are not comparable |
| no build tool, `go` plus a CI file, as Caddy does | this repository has a frontend, a docs site, six generated vocabularies, a swagger step and cross-compilation. Caddy has none of them |
| a hand-written list of commands in `help` | drifts. `tools/printer-emulator/Makefile` already has one and this repository has a standing rule against hand-maintained copies of generated state |
| a `make/` directory of fragments | gives separation the file does not need and adds `include` ordering as a new failure mode. Rule 1 names the line count that would reopen it |
| `.ONESHELL` plus `.SHELLFLAGS := -eu -o pipefail -c` | the recommended modern shape, and unusable here. Both are ignored without a message on GNU make 3.81, so a recipe relying on `set -e` would pass CI and misbehave on the developer machine |
| overriding `MAKE` to the bare name `make` | works on Windows and discards the binary make was invoked as, so a machine where GNU make is `gmake` re-enters the wrong one |
| renaming `server-up`, `logs` and `status` to say docker | they do not use docker. `server-up` runs `./bin/server` as a host process and writes `.server.pid`; `logs` tails `server.log`; `status` reads that pid file. What they need is a completed `make build`, which rule 10 covers |
| Traefik's `sed -n 's/^#?//p' \| column -t` help | `column` is present in Git Bash and absent from many containers. `awk` is present everywhere `make` is |
| annotating all 88 targets | help becomes a wall nobody reads. Rule 6 says annotate what a person needs |

## open items

- rules 3, 4, 5, 13, 14, 16 and 20 are closed by `scripts/check-makefile.sh` and by `Makefile` and `Dockerfile*` in `SEARCH`. Two live defects fell out of the first red run. The two `$(shell awk '\` blocks that opened this file were a quoted program split across four continuations each. `build-minimal-compressed` split a `sh -c` script the same way. All three are one line now
- rule 14's conversion was 69 lines. `-` is written for `->`, `...` for the ellipsis, and `OK`, `FAIL` and `WARN` padded to six columns for the three status glyphs. U+2500 stays inside a comment banner. `writing-style.md` names that banner as the house standard for a config file, and no recipe prints one
- measured under GNU Make 4.3 on Linux on 2026-08-31, against the Windows and GNU Make 3.81 numbers the rules were written from. Bare `make` prints 35 lines of help in 14 ms at exit 0, so rules 2 and 7 hold. The file carries 123 target headers, 22 of them annotated under 5 `##@` groups, and the rejected table's figure of 88 targets is the one number that has drifted. All 18 `$(MAKE)` sites are quoted and none is bare, and `make docs-check` re-enters three of them and runs to the end, so rule 4 costs nothing here. Neither Makefile names `.ONESHELL`, `.SHELLFLAGS` or `.RECIPEPREFIX`, so no recipe depends on a feature 4.3 has and 3.81 ignores, which is rule 3 read from the other side. `make makefile-check` passes in 200 ms and `make hooks-check` in 95 ms. Rule 1 is the one rule the file fails on either platform, at 1,774 lines against a cap of roughly 1,500
- `tools/printer-emulator/Makefile` obeys these rules as of 2026-08-31. It carries `.DEFAULT_GOAL`, the one-line `awk` help over `##` and `##@`, and the full-target line rule 7 asks for. Each of its ten targets has a `##` description naming python. `REQUIRE_PYTHON` is the rule 11 guard, and `RESOLVE_NAME` collapses the resolution `start` and `stop` repeated. Rule 8 is the one rule it still fails. `install`, `list`, `add`, `remove`, `start` and `stop` are the words `emulator.py` takes and `README.md` documents, so renaming them was rejected for the reason `server-up` was
