---
title: "ADR-20261027-5m4: a service module runs under a memory budget"
description: "How a budget is resolved, how the host compares the sum with a pool and only warns, how it watches and kills a module over its budget, when it switches a module off, and what the budget cannot stop."
date: 2026-10-27
status: proposed
amends: [ADR-20260828-dse]
tags:
  - adr
  - extensions
  - supervision
  - modules
---
# ADR-20261027-5m4: a service module runs under a memory budget

- status: proposed
- amends [ADR-20260828-dse](20260828-dse-a-modules-start-budget-is-thirty-seconds.md) rule 5.
  The ceiling is per instance and has a default
- date: 2026-10-27

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260828-dse](20260828-dse-a-modules-start-budget-is-thirty-seconds.md) rule 5 | Builds on [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md), [ADR-20260827-7jn](20260827-7jn-the-plugin-wire-is-a-specification.md)

A service module is a child process of the server. Nothing limits its memory.
One module that leaks can make the kernel kill the server, and every other
module goes down with it.

This record gives each service module a memory budget and warns when the budgets
do not fit in the machine. It stops a module that goes over and switches off a
module that keeps failing. It also says what the budget cannot stop.

## rules

### the budget

1. The effective budget of an instance is the first of these that is set: the
   operator's override for that instance, the module author's recommendation,
   the server default. The status reports which one applies: `operator`,
   `author` or `default`.
2. The server default is 128 MiB.
3. The author recommends a budget with `needs.memory_mib` in `module.yaml`. The
   manifest checks its range the way it checks `records_mib`.
4. The operator override is stored per instance as `memory_mib` on the resource
   spec. Zero or absent means no override, so a release that lowers its
   recommendation lowers the budget of every instance with no override.
   Unlike `records_mib`, the recommendation is not copied into a grant at
   install.
5. A module installed without a recommendation gets the default. A module that
   needs more is stopped and switched off by rules 24 and 25 until the operator
   sets a budget.
6. A release whose recommendation is larger joins the same diff as any other
   release that asks for more, and raises `MODULE_REQUESTS_CHANGED`. The host
   compares the new recommendation with the recommendation of the release
   before it and with the ceiling the operator approved. It does not compare it
   with the effective budget, so a larger recommendation is shown even when an
   operator override already covers it.

### the pool

7. The pool is the memory the budgets share. It is `core.modules_memory` in
   `config.yaml` when set. Otherwise it is the smaller of physical memory and
   the container limit, minus the reserve.
8. Detection uses no new dependency. Linux reads `Sysinfo`. It then reads the
   cgroup v2 `memory.max` along the path in `/proc/self/cgroup`, and takes the
   smallest value of the path and its parents. It then reads cgroup v1
   `memory.limit_in_bytes`, where a value near 2^63 means no limit. macOS reads
   `hw.memsize`. Windows calls `GlobalMemoryStatusEx`. `/proc/meminfo` is not
   used, because the shipped unit hides it with `ProcSubset=pid`.
9. The reserve is kept for the server: 256 MiB or 25 % of the detected memory,
   whichever is larger. The server computes the pool and the reserve once, at
   start. The over-commit log of rule 13 and the endpoint of rule 11 carry the
   figures.
10. When detection fails and `core.modules_memory` is not set, the pool is
    unknown, not zero. The client has no pool to compare with, and rule 13 logs
    no over-commit.

### the warning

11. The pool only warns. Four actions can make the sum of the effective
    budgets of the enabled service modules pass the pool. They are switching a
    module on, installing it switched on, raising its budget, and upgrading to
    a larger recommendation. Each one still goes through. A read-only endpoint,
    `GET /api/v1/modules/memory`, reports `pool_bytes`, `committed_bytes` and
    `source` (`detected` or `configured`). The client compares them with the
    budget of the module and shows a warning. The fault
    `MODULE_MEMORY_POOL_FULL` does not exist. A module offer carries
    `process`, which is false for a release that runs no process. The client
    shows no budget for such a release and counts nothing for it.
12. A module that is switched off counts nothing.
13. Start never refuses. Every module that is enabled starts, whether it comes
    from the overlay or from `config.yaml`. An over-commit is logged at start
    as a warning with the promised and the pool figures.

### the watchdog

14. Every two seconds, for each running service child, the host reads `RssAnon`
    from `/proc/<pid>/status` and sums it over the child's process tree. The
    host starts the child in a session of its own and reads `/proc/*/stat`.
    It starts from every process in the child's process group or session, so
    a process whose parent exited is still counted, even in another group.
    From there it follows the parent links, so a descendant that called
    setsid is still counted while its parent lives.
    macOS may use `ps -A -o pid=,ppid=,pgid=,rss=`. It uses the group and
    the parent links alone, because macOS is for development.
    Windows sums the private bytes of every process in the job object.
15. When the figure cannot be read, the status says unknown, never 0.
16. At 80 % of the budget or more, the status carries a `near budget` flag. The
    host does nothing else.
17. Over the budget, the host acts as `on_over_memory` says (rule 33). With
    `stop`, it records the stop result `over-memory` with the used figure and
    the budget, and then kills the tree.
18. A change to a running module's budget restarts it. The module learns its
    budget only at start, from `PCL_MEMORY_BUDGET_BYTES` and `GOMEMLIMIT`, and
    the slicer bounds each run from it (rule 32). A change to
    `on_over_memory` or `switch_off` alone applies at once, with no restart.
19. The host keeps the last hour of samples for each instance in memory. A
    server restart clears them.

### the launch

20. A service child runs in a session of its own, so it leads its process
    group. A stop kills every process the walk finds (rule 35). On Windows the
    child runs in a job object that kills its processes when the job closes.
    The job sets `JOB_OBJECT_LIMIT_JOB_MEMORY`, which is the one hard kernel
    limit in this record. The limit is the budget plus 25 %. A module that
    goes over its budget is then stopped by the watchdog with the result
    `over-memory`, before the job limit makes it fail on its own. A module
    set to `warn` is the exception: nothing stops it before the job limit
    does, so it stops there. The job limit is the last bound. The host kills
    a child only through `Child.Kill` in `wirehost`. `Child.Kill` holds the
    lock of the reap and does nothing once the child is reaped, so it does
    not signal the group of a leader that is already reaped. It cannot stop
    a process the walk found from exiting before the signal arrives, and the
    kernel may reuse that process's id.
21. The child's environment is an allowlist: `PATH`, `HOME`, `TZ`, `LANG`,
    `LC_*`, `TMPDIR`, `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` and their
    lower-case forms, and `PCL_MEMORY_BUDGET_BYTES` (rule 31). On Windows
    `TEMP`, `TMP`, `USERPROFILE`, `APPDATA` and `LOCALAPPDATA` pass too, because
    the Go runtime reads its temporary and home directories from them. No other
    variable passes. The host adds `GOMEMLIMIT`, which is 85 % of the effective
    budget, and sets `PCL_MEMORY_BUDGET_BYTES`, which is the budget itself in
    bytes.
22. On Linux the host writes `800` to `/proc/<pid>/oom_score_adj` after the
    start, so the kernel picks a module before the server. It is best effort. A
    failure is logged.
23. On Linux the server becomes a child subreaper with `PR_SET_CHILD_SUBREAPER`
    and reaps orphans. Killed grandchildren then do not stay as zombies when the
    server is PID 1 in a container. One reaper serves a process. It is built at
    the entry of the process and passed down, because two reapers collect each
    other's children. The reaper collects only zombies outside the server's own
    process group. So the host must not start, in its own group, a child that
    daemonises: the zombies of its orphans are never collected.

### the stop and the switch-off

24. A stop has one result from this closed set: `over-memory` (with used and
    budget), `exited` (with the code), `signal` (with the name),
    `stopped-by-operator`, and `switched-off-by-host`. `not-responding` is
    reserved for hang detection. The host reaps the child before it reports, so
    the result carries the real code or signal. A watchdog sets its result
    before it kills.
25. A number of failures inside a sliding window switch the instance off. The
    server default is 3 failures in 1 hour, set by `core.modules_switch_off`
    with `failures` and `window` in `config.yaml`. The operator overrides
    either value per instance with `switch_off` on the resource spec. A value
    the operator does not set follows the server. `failures` is 0 to 100. With
    0 the host never switches the instance off for failing. An optional
    instance whose start keeps failing still gives up after an hour and five
    attempts, with the result `give-up`, and with a high limit the give-up can
    come first. `window` is 1 minute to 24 hours. An edit applies to the
    running instance. A limit at or below the failures already counted
    switches it off at the next failure. A shorter window drops older
    failures at once. A longer window does not bring back failures already
    dropped. The memory status
    reports the values in force and whether they come from the operator or
    the server default. Any failed stop or start counts: a crash, an
    `over-memory` or `not-responding` stop, and a failed start. A start the
    host refuses because a dependency is not serving yet, its record backend
    or a module it requires, is the instance waiting and does not count. It
    stays on the retry ladder. The watchdog
    never stops a `warn` module (rule 33), so `warn` alone never counts. A
    restart the operator asks for, including a restart after a budget change,
    neither counts nor resets the count. An install or an update keeps the
    operator's `switch_off`, as it keeps `memory_mib` and `on_over_memory`.
26. For an instance in the overlay, the host writes `enabled: false` and a
    record of the switch-off (the reason, the time and the stops) into the
    overlay. The write is a host-originated store method that skips the
    authorization check and keeps the commit and ETag path.
27. An instance declared in `config.yaml` cannot be written through the API.
    The host holds it off in memory until the server restarts. The instance's
    memory status shows that in the field `held_until_restart`.
28. The operator switches the module on again with the same switch, and that
    clears the record and the count. The host never switches on a module the
    operator switched off.

### the ceiling

29. The budget is a watched limit. It is not a kernel limit, except on Windows.
    A process whose parent exited, as after a double fork, stays in the group
    and is still counted. A process that leaves the group with `setsid` is still
    counted and killed while its parent lives (rule 35). It is lost once its
    parent has exited. Memory in shared memory or in a `memfd` is not in
    `RssAnon`. Only a delegated cgroup closes both. The shipped unit
    (`ProtectControlGroups=true`), Docker and Kubernetes do not delegate one to
    the server. The kernel's OOM killer, helped by rule 22, is the last bound.

### the amendment

30. This amends [ADR-20260828-dse](20260828-dse-a-modules-start-budget-is-thirty-seconds.md)
    rule 5. The ceiling is per instance, and it has a default: the budget of
    rules 1 and 2. Rule 5's sentence "a fixed default ceiling loses" is
    replaced. A default that an operator can override per instance, and that
    follows the author's recommendation, does not charge a small machine for a
    large one. Rule 5 names `launch.go:312`. The line is `start` at
    `internal/extension/wirehost/launch.go:583`.

### the host is the one authority

31. The host is the one place a module's budget is decided. A module learns it
    from `PCL_MEMORY_BUDGET_BYTES` and offers no option of its own that
    duplicates it.
32. A module that bounds its own children, such as the slicer, derives that
    bound from its budget, so its own stop fires before the host's.
33. The instance field `on_over_memory` says what the host does over the budget.
    `stop` is the default. It kills the tree and counts towards the switch-off
    of rule 25. An empty value means `stop`. `warn` keeps the module running,
    marks the status, and counts nothing. The kernel OOM killer with
    `oom_score_adj` 800 stays the last bound.
34. The pool is advisory (rule 11). Clients read it from
    `GET /api/v1/modules/memory`, and nothing is refused for it.
35. A host stop kills every process the measurement walk finds, including
    descendants that lead their own process groups.

## enforcement

| rule | gate | state |
|---|---|---|
| 1, 2, 4 | `TestBudgetOf` in `internal/app/resource` | exists |
| 1, 2, 4 | `TestALoweredRecommendationLowersABudgetWithoutOverride` in `internal/app` | exists |
| 3 | `TestANeedsBlockRecommendsAMemoryBudget` in `sdk/manifest` | exists |
| 6 | `TestARaisedMemoryRecommendationIsAChangedRequest` in `internal/app` | exists |
| 7, 9, 10 | `TestPoolOf` in `internal/app/resource` | exists |
| 8 | `TestCgroupLimit` in `internal/kernel/sysmem` | exists |
| 14, 15 | `TestMemoryCountsADescendantThatLeftTheGroup` in `internal/kernel/procgroup` | exists |
| 14, 15 | `TestAnUnreadableSampleIsUnknownAndKillsNothing` in `internal/app/resourcehost` | exists |
| 16 | `TestNearBudgetIsFromEightyPercentAndAbsentWhenUnknown` in `internal/app/resourcehost` | exists |
| 17 | `TestAModuleOverItsBudgetIsStoppedAndSaysWhy` in `internal/app/resourcehost` | exists |
| 18 | `TestABudgetEditReachesTheWatchWithoutARestart` in `internal/app/resourcehost` | exists |
| 18 | `TestABudgetEditRestartsAndAnOnOverMemoryOrSwitchOffEditIsInPlace` in `internal/app/resource` | exists |
| 19 | `TestTheRingKeepsOneHour` in `internal/app/resourcehost` | exists |
| 20 | `TestKillEndsTheWholeGroup` in `internal/kernel/procgroup` | exists |
| 20 | `TestAStoppedModuleLeavesNoProcess` in `internal/app/resourcehost` | exists |
| 20 | `TestAReapedChildIsNotKilledAgain` in `internal/extension/wirehost` | exists |
| 21 | `TestChildEnvKeepsOnlyTheAllowlist` in `internal/app/resourcehost` | exists |
| 23 | `TestTheReaperCollectsAnOrphan` in `internal/kernel/procgroup` | exists |
| 23 | `TestTheReaperLeavesAnOwnedChildToWait` in `internal/kernel/procgroup` | exists |
| 24 | `TestAChildThatExitsReportsItsCode` in `internal/extension/wirehost` | exists |
| 24 | `TestAKilledChildReportsTheSignal` in `internal/extension/wirehost` | exists |
| 24 | `TestARecoverableStopCarriesItsResult` in `internal/extension/supervisor` | exists |
| 25 | `TestTheThirdFailureInTheWindowSwitchesOff` in `internal/extension/supervisor` | exists |
| 25 | `TestTheFailureLimitIsTheCountThatSwitchesOff` in `internal/extension/supervisor` | exists |
| 25 | `TestNoFailureLimitNeverSwitchesOff` in `internal/extension/supervisor` | exists |
| 25 | `TestSwitchOffOf` in `internal/app/resource` | exists |
| 25 | `TestASwitchOffOfZeroFailuresIsKept` in `internal/app/config` | exists |
| 25 | `TestAnUpdateKeepsOnOverMemoryAndTheSwitchOff` in `internal/app` | exists |
| 25 | `TestAFailedStartCounts` in `internal/extension/supervisor` | exists |
| 25 | `TestAStartWaitingOnADependencyDoesNotCount` in `internal/extension/supervisor` | exists |
| 25 | `TestAnOperatorRestartNeitherCountsNorResets` in `internal/extension/supervisor` | exists |
| 25 | `TestTheThirdOverMemoryStopSwitchesTheModuleOff` in `internal/app/resourcehost` | exists |
| 26 to 28 | `TestTheHostSwitchesAnOverlayModuleOffAndItStaysOff` in `internal/app/resource` | exists |
| 26 to 28 | `TestTheHostHoldsAnOperatorFileModuleUntilRestart` in `internal/app/resource` | exists |
| 26 to 28 | `TestSwitchingBackOnClearsTheRecord` in `internal/app/resource` | exists |
| 5, 11 to 13, 22, 29 to 35 | none possible, or the figure is a constant. Review | review |

## rejected

| option | why not |
|---|---|
| a fixed budget for every module | a module that needs more is stopped, and a module that needs less holds memory it never uses |
| the budget as a number the author declares and the operator cannot change | the operator knows the machine and the author does not |
| the operator figure copied into a grant at install, as `records_mib` is | a release that lowers its recommendation would not lower the budget |
| RSS as the measured figure | it counts file pages the kernel can drop, so a module reads larger than it is |
| `RLIMIT_AS` | a Go child that does nothing reserves 5.53 GB of address space |
| the host refusing to start modules that no longer fit | a server restart would switch off modules the operator turned on |
| a fixed window and count for the switch-off | a module that fails rarely but for a known reason is switched off, and the operator has no way to keep it running |
| a delegated cgroup as the only mechanism | the shipped unit, Docker and Kubernetes do not delegate one, so most installs would have no budget |

## open items

- A cgroup path (rule 29) is the only full bound. It needs the unit to set
  `Delegate=yes`, and that is a decision about the shipped unit.
- The walk of `/proc/*/stat` costs about 6 ms per sample with
  about 510 processes, and 0.07 ms with about 10 (`BenchmarkMemoryWalk`, Linux
  Docker on an Apple M-series Mac). The cost grows with the process count, and
  the sample interval is two seconds because of that cost.
