---
title: "ADR-20260923-xcq: every suite a change can reach runs, in lanes that report alone"
description: "Which jobs of ci.yml a pull request runs, how ci.yml splits its gates into jobs, why a failed gate never hides the next one, and which job gives the result of a run."
date: 2026-09-23
status: accepted
tags:
  - adr
  - enforcement
---
# ADR-20260923-xcq: every suite a change can reach runs, in lanes that report alone

- status: accepted
- date: 2026-09-23

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

On a pull request, CI runs every suite the change can reach. A push to `main`,
the nightly run and a run a person starts run every suite. This record says how
a pull request finds the suites it can reach, and how to read the result. A job
that runs many gates in a row stops at the first red one, and the gates after
it report nothing. So the gates are split into jobs that run side by side. A
failed gate never stops the gates after it. One job, `CI OK`, gives the result
of the whole run.

## rules

1. On a pull request, run each job whose inputs the change touches, and
   skip the others, on the condition that no gate gets weaker. Anything a change
   can affect is still checked. The rules below keep that true.

   `CI / What the change touches` runs `scripts/ci-areas.sh` over
   `git diff` between the pull request's merge commit and its base. The
   script answers in areas:

   | area | the paths |
   |---|---|
   | `go` | the Go modules, and every file outside them that Go code reads |
   | `web` | the client packages, the web app, and every file they read |
   | `docs` | `docs/` and every Markdown file, except the changelog fragments |
   | `changes` | the changelog fragments under `docs/changes/` |
   | `removed` | a deleted or renamed path, because a link in the documentation can name any file |
   | `sign_in` | a path that reaches the sign-in at an identity provider |
   | `all` | a path every job reads, or a path no rule names. Every area is then true |

   A job in `pathGatedJobs` in `internal/arch/ci_ok_test.go` skips when none
   of its areas changed:

   | job | runs when the change touches |
   |---|---|
   | `Backend / Lint`, `Backend / Security scan`, `Backend / Error codes` | `go` |
   | `Backend / Model and firmware matrix`, `Backend / Build` | `go` |
   | every `Backend / Unit tests` job, `Backend / Event sink adapters` | `go` |
   | `Backend / Verify generated code` | `go` or `docs` |
   | `Backend / Client packages through a registry`, `Docker / Hardened image` | `go` or `web` |
   | `Web / Client SDK and library`, `Web / Embedded binary`, `Web / Journey` | `go` or `web` |
   | `Web / Example modules`, `Web / Filament module` | `go` or `web` |
   | `Web / Component library lint`, `Web / Component library tests` | `web` |
   | `Web / Application` | `web` |
   | `Documentation (Starlight)` | `go`, `docs`, `changes` or `removed` |
   | `Web / Sign-in at <provider>` | `sign_in` |

   These rules hold the design:

   - an area holds every file its code reads, wherever the file lives.
     `docs/errors/` is in `go` and `web`, because Go code and the client
     package's tests read it. `internal/arch/ci_areas_test.go` reads the
     paths that Go code, the web packages and the documentation build name.
     It refuses a path the script puts in another area
   - a path no rule names runs everything. So does a change to `ci.yml`, the
     script, the `Makefile`, `scripts/`, `go.mod`, `go.sum`, `.nvmrc`,
     `VERSION`, a `package.json`, a lockfile, a fixture under `docker/`, a
     compose file or a `.gitignore`. The sign-in lane's own script and its
     three provider fixtures are the one narrow exception, because no other
     job reads them
   - the paths job, `Repository / Commit, text and workflow gates`,
     `Backend / Go build cache` and `CI OK` run on every change. The
     repository job holds the commit message, changelog, writing style,
     Makefile and document number gates, actionlint over every workflow,
     and the architecture tests. Each one takes seconds, and a change to
     any file can break it
   - a job skips only on a pull request. Every other event checks every
     area, so a coupling the areas miss is found within a day
   - a job's condition carries `!cancelled()` and runs it on every event but
     a pull request. A failed paths job then stops no job on a push, and on
     a pull request `CI OK` fails on it
2. Give a job the gates of one toolchain or one part of the tree. The web gates
   are eight jobs with no `needs` between them: `Web / Client SDK and library`,
   `Web / Component library lint`, `Web / Component library tests`,
   `Web / Application`, `Web / Embedded binary`, `Web / Example modules`,
   `Web / Filament module` and `Web / Journey`. A part whose gates take minutes one after the other is
   split further, so no job waits on gates it does not need.
3. Let no gate hide another. In a job with more than one gate, every step after
   the first gate carries `if: ${{ !cancelled() }}`. A step that reads the
   output of an earlier step also requires that step's `outcome` to be
   `success`.
4. Write a make target that runs several gates so it runs every gate and names
   each failure at the end. `make frontend-check` is the example.
5. Read `CI OK` as the one result of a run. It needs every other job in
   `ci.yml` and runs under `if: always()`, because GitHub reads a skipped
   job as a pass. It fails when the run was cancelled, and when a job it
   needs failed or was cancelled. It also fails when a job skipped where
   nothing explains the skip. A race job runs on the nightly schedule and on
   a push to `main`. The paths job runs on every event. A job in
   `pathGatedJobs` may skip on a pull request only when the paths job
   answered `false` for each of its areas. A missing answer counts as a
   change. The sign-in at the providers runs on the nightly event too. Every
   other job carries `if: github.event_name != 'schedule'`.
6. Run the race detector as shards, on a push to `main` and on the nightly
   schedule, never on a pull request. Each cell runs
   `make test-shard SHARD=<name> RACE=1`, so the race lane and the unit lane
   read one package list. `Backend / Race (sdk and packages)` runs the sdk
   module and every module under `packages/` on the same events.
7. Give every job in `ci.yml` a `timeout-minutes` of three times its observed
   90th percentile, rounded up to five and never under ten. A job with no
   measurement yet gets sixty.
8. Let a new commit on a pull request cancel the run over the commit before it.
   A push and the nightly run group by commit and always run to the end.

## enforcement

| rule | gate |
|---|---|
| 1 | `internal/arch/changefilter_test.go`, `internal/arch/ci_areas_test.go` and `internal/arch/ci_ok_test.go` |
| 5 | `internal/arch/ci_ok_test.go` and `internal/arch/ci_ok_script_test.go` |
| 7 | `TestCIOKNeedsEveryJobAndEveryJobHasATimeout` checks that a timeout exists, and review checks its size |
| 2, 3, 4, 6, 8 | none. Review holds them |

## rejected

| option | why not |
|---|---|
| `paths:` under `on:` in `ci.yml` | the workflow does not start, so no `CI OK` reports, and nothing tells a skip from a pass |
| a path list per job | the lists drift from each other and from the code. One script and a test that reads the code give one answer |
| every suite on every pull request | on 50 merged pull requests, 13 percent of the job minutes ran suites the change could not reach |
| a change-detection action | `git diff` over two commits answers it, and each action is a supply chain entry |
| the commit, text and architecture gates behind an area | they read every file and the commits, so no area holds their inputs, and they take seconds |
| a skip on a push to `main` | the push finds what the areas miss, and what broke `main` before a pull request |
| `re-actors/alls-green` for the result job | a third-party action for two lines of expression, and each action is a supply chain entry |
| built packages shared between jobs as artifacts | it needs a `needs` graph to save one or two minutes, and a red build then stops every job after it |
| a cache for the Playwright browsers | Playwright's CI documentation says a cache restore takes about as long as the download |
| `playwright install --with-deps` | the runner image carries Chromium's libraries already, and apt upgrading them took up to eleven minutes |
| a composite action for the shared setup steps | four plain steps per job are easier to read than one more file. Review it again if the copies drift |
| one race job over the whole tree | it finishes after every other job, and a failure names a package and not a shard |

## the Go build cache

Compiling is most of a Go job on a runner's two processors. setup-go keeps one
entry per `go.sum`. On main it held 152 MB, the dependencies and little of this
module. `Backend / Go build cache` runs `make go-cache-warm`, and a push
to main writes what it compiled once a day. Every Go job restores the newest
entry with `cache: false` on setup-go, and rebuilds what changed since. On
pull request 377 the Error codes job went from 5.5 to 2.3 minutes and the
unit shards from over three to about two and a half. `Backend / Build` keeps a
cache of its own, because it cross-compiles for targets no other job builds.

## open items

- The areas are coarse. A change to Go code runs every web job that starts a
  core, and a change to the web packages runs the image build. Finer areas
  save more, and each needs its own read test.
- `Backend / Unit tests (internal)` runs the architecture tests again on a
  change to Go. They take about twenty seconds.

- A required check needs a ruleset or branch protection. The GitHub plan of the
  organisation offers neither for a private repository, and the rulesets API
  answers 403. When the plan allows it, `CI OK` becomes the one required check.
- The organisation runs at most twenty jobs at once, and a run of this file
  starts more than thirty. A job past the twentieth waits for a runner, so a
  new job costs its setup in runner time even when it finishes beside the
  others. Measured on pull request 377: 80 job minutes, twenty at once, a
  wall time of 7.8 minutes with no job over 6.2.
