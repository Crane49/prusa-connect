---
title: "ADR-20261027-e6k: modules depend on each other as Composer packages do"
description: "A module's require, suggest and conflict follow Composer, with the core as a platform package. One resolver in internal/app/moduledeps plans every install, update, install from the lock and core upgrade, the lock records what it resolved, and the module commands follow Composer's."
date: 2026-10-27
status: accepted
supersedes: []
amends: [ADR-20260910-74m, ADR-20260827-2bp, ADR-20261027-pq5]
tags:
  - adr
  - modules
  - extensibility
  - sdk
---
# ADR-20261027-e6k: modules depend on each other as Composer packages do

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends: [ADR-20260910-74m](20260910-74m-a-module-declares-what-it-depends-on.md) rules 2 to 5, 7, 13 and 15, [ADR-20260827-2bp](20260827-2bp-a-module-travels-as-a-verified-artefact.md) rule 7, [ADR-20261027-pq5](20261027-pq5-a-module-calls-the-routes-of-another-module.md) rules 1 and 3

One module often needs another. A print planner needs a slicing module, and
a slicing module reads projects with a 3MF module. Two records disagreed
about how a module says so. ADR-20260910-74m rule 5 allowed a version range
on another module, and ADR-20260827-2bp rule 7 refused one.

We follow Composer, because its model works in practice.
Its schema, its constraints, its stabilities, its lock and its commands are
the model. Where this server differs, the section on differences says how and
why.

## rules

### the manifest

1. A module names its dependencies in Composer's three maps:

   ```yaml
   require:
     core: ">=0.9"
     core-state.get: "*"
     com.github.aux.prusa3d.threemf: "^1.2"
   suggest:
     com.github.aux.prusa3d.parts: "Shows the parts of a plan"
   conflict:
     com.github.aux.prusa3d.old-planner: "<2.0"
   ```

2. A dependency names a module by its namespace, never a kind of thing. There
   is no `provide` and no `replace`: one module never stands in for another.
3. `core` is the core as a platform package, as `php` is in Composer.
   `core-<capability>` is a wire capability the core serves, as `ext-json`
   is in Composer, and takes `*` only.
3a. A core upgrade is the update of the `core` platform package. It is one
    plan with the releases of the modules it needs updated beside it, as
    `composer update` with a new `php` is.
4. `suggest` names a module and the reason. It installs nothing and checks
   nothing.
5. `conflict` refuses to install a module beside a release of another module
   its constraint holds, in either order.
6. `calls` names the modules whose routes this module calls with
   `routes.call`. Each one is under `require` or `suggest`.
7. A Go package of another module compiled into this one is a Go dependency
   in the module's own `go.mod`. The manifest does not name it.

### constraints and stability

8. A constraint has Composer's meaning:

   | constraint | holds |
   |---|---|
   | `1.2.3` | that release |
   | `>=1.0 <2.0` or `>=1.0, <2.0` | both bounds |
   | `!=1.2.5` | every release but that one |
   | `1.2.*` | `>=1.2 <1.3` |
   | `1.0 - 2.0` | `>=1.0 <2.1` |
   | `~1.2` | `>=1.2 <2.0` |
   | `~1.2.3` | `>=1.2.3 <1.3.0` |
   | `^1.2` | `>=1.2 <2.0` |
   | `^0.3` | `>=0.3 <0.4` |
   | `^1 \|\| ^2` | either alternative |
9. A release's stability comes from its version: `dev`, `alpha`, `beta`, `RC`
   or `stable`. A release on a marketplace's beta channel whose version names
   no prerelease is beta.
10. `core.modules_minimum_stability` is Composer's minimum-stability, and is
    `stable` by default. A constraint flag such as `^1.2@beta` lowers it for
    one module, and so does a prerelease the constraint writes out.
    `core.modules_prefer_stable` is Composer's prefer-stable.
11. The release of a module's major is its compatibility signal: a module
    that breaks how other modules talk to it raises its major.

### one resolver

12. A server has one installed release of each module.
13. One resolver plans every change: `moduledeps.Resolve` in
    `internal/app/moduledeps`. It takes what is installed, what the index
    offers, the lock and the settings, and answers the releases to install in
    order, a module before the modules that require it. It changes no module
    the request leaves fixed.
14. The index is the index file in the modules directory, the release
    directories in `core.modules_local`, and the marketplace when it is on.
15. A plan that cannot be met is refused. `MODULE_DEPENDENCY_CONFLICT` names
    who needs what, such as `planner needs threemf ^1.2, slicer needs threemf
    ^2.0`. `MODULE_DEPENDENCY_NOT_OFFERED` names a module no index offers.
    That is the answer on a server with no index.

### the commands

16. `module add <path>@<constraint>`, Composer's `require`, installs a module
    and the modules it lacks, and keeps every other module as it is. With no
    `-from`, the index names the release. The page does the same through
    `POST /api/v1/modules/plan` and the install with `dependencies`. One
    approval covers every request of every module in the plan, all or
    nothing. The page sends each module of the plan back with its digest,
    the requests it showed for it and whether its ports were approved. Each
    module the plan adds is held to those requests, as the module the person
    picked is held to its own. An approved plan that showed nothing
    for a module is refused with `MODULE_REQUESTS_CHANGED`. A plan that moved
    between the two, a module, a release or its digest, is refused with
    `MODULE_PLAN_CHANGED`. A module installed at a release outside the range,
    or switched off, does not meet a requirement, so the resolver is asked.
17. `module update [<module>...] [-with-dependencies]` resolves the named
    modules again, or every one, and writes the lock.
18. `module install` installs exactly the releases the lock names, from the
    source and digest it records, and resolves nothing. A release installed
    under the same version from other bytes is installed again, and a
    release on this server's filesystem is checked against the digest first. It refuses a lock
    that does not satisfy its own modules or the core, and a server that has
    modules the lock does not name.
19. `module remove` refuses a module another installed module requires, with
    `MODULE_REQUIRED_BY`, which names the dependants. There is no force: the
    operator removes the dependant first. Switching a module off is refused
    the same way while an enabled module requires it. A suggestion blocks
    nothing.
19a. The modules an install added for others that nothing requires after a
    removal are named, by the command and by
    `GET /api/v1/modules/removals/{namespace}`. They are removed in the same
    operation only when the operator says so, with `-with-unused` or
    `with_unused=true`.
20. `module why <module>`, `module why-not <module> <version>` and
    `module outdated` answer what Composer's commands of the same names
    answer. The API serves the same three under `/api/v1/modules/why/`,
    `/api/v1/modules/why-not/` and `/api/v1/modules/outdated`.

### the lock

21. `modules.lock.json` beside the overlay is Composer's composer.lock. It
    records every installed release with its source and digest. It records
    whether the operator asked for it, with which constraint, and its
    require, conflict and core constraint. Every command and every API call that changes the installed
    modules writes it. An API call checks first that it can, and refuses
    with `MODULE_LOCK_NOT_WRITTEN` before anything is written when it cannot.
    A write that still fails after the change answers the same code and says
    that the change stands, and never a success.
22. Its `content-hash` is taken over what the operator asked for and the two
    stability settings, as Composer's is over composer.json.
23. A module a request does not update keeps the release the lock names when
    it still fits.

### start, stop and restart

24. A reconcile starts a module after the modules it requires, and a
    dependant whose dependency is still starting waits inside its own start
    budget (ADR-20260828-dse).
25. When a required module stops serving, the host restarts each running
    dependant, which waits as pending with `DependenciesSatisfied` false and
    reason `DependencyMissing`. When it serves again, the dependant is retried
    at once.
26. A cycle of requires is refused with `DependencyCycle`
    (ADR-20260910-74m rule 14).

### routes.call

27. `routes.call` names the namespace of the module called, and the host
    refuses a module the caller names under neither `require` nor `suggest`.

## core upgrades and the transaction

28. Preparing a core upgrade resolves its module plan with
    `moduledeps.CoreUpgrade`, and the candidate version is the core. The
    releases a signed offline bundle carries are added as an install adds a
    release. Every installed module the candidate leaves without a fitting
    release is resolved again, with every module that requires it. The
    installed releases are offered beside the index, so a module keeps its
    release where it still fits, and an offline server needs no index for that.
29. A bundle whose releases the plan cannot meet is refused at preparation,
    whole. Without a bundle, a refused plan does not refuse the upgrade. Each
    module the plan leaves without a release is reported `disabled` with the
    resolver's reason. An instance the server needs to start that would stop
    serving blocks the upgrade. That flag is the resource's `required`, and the
    report calls it `needed_to_start`, apart from the modules one module
    requires.
30. A release the plan takes from the index is downloaded at preparation into
    the upgrade's staging directory and held to the digest the index names. The
    Beta Portal's own rule decides what carries the pairing. The release is
    recorded in the journal beside the bundle's extensions. Preflight takes the
    ordered list, and refuses one that drops or changes an extension the bundle
    staged.
31. Apply installs the plan's releases after the backup and before the swap,
    in the plan's order, so a module is installed after the modules it
    requires. Each records the source, digest, constraint and pulled flag the
    lock carries, and the lock is written once they are in.
32. `modules.lock.json` is in the backup with the overlay, so a trial that
    fails restores the lock and the declarations it names together.

## where this server differs from Composer

| Composer | here | why |
|---|---|---|
| `composer.json` beside the lock | the operator's constraints live in the declarations, and the lock carries them | there is no project file; a server is the project |
| `install` removes packages the lock does not name | `module install` refuses and names them | the server never removes a module the operator installed without asking |
| `remove` drops unused dependencies itself | they are named and removed only with `-with-unused` | the operator may use such a module directly |
| a platform package matches by version | a prerelease of the core matches as its release unless the constraint writes a prerelease of it | a beta core runs modules built for that release |
| `require` takes any package name | a module namespace, `core` or `core-<capability>` | modules are named by namespace |
| `suggest` and `conflict` with no other effect | `calls` reaches only a required or suggested module | a call to an unnamed module has no known release |
| `dev-<branch>` versions | not read | a release is a published artefact with a digest |
| `update` keeps a package the repositories no longer list | `module update` refuses it with `MODULE_DEPENDENCY_NOT_OFFERED` | a release the server cannot fetch again cannot come back from the lock |
| the lock pins a package by version and reference | the plan and the lock pin each release by version and digest | an index may point a version at other bytes after a person approved it |

## the Go API

`internal/app/moduledeps` is the resolver every caller uses: an install, an
update, an install from the lock and a core upgrade.

| name | what it does |
|---|---|
| `Resolve(Request) (Plan, error)` | plans a request against what is installed, the index, the lock and the settings |
| `CoreImpact(installed, available, lock, core, settings) (Plan, error)` | plans a core upgrade: modules the new core breaks and the modules that require them |
| `CoreUpgrade(Request, core) (Plan, error)` | `CoreImpact` with the releases the upgrade brings added, the installed releases offered beside the index |
| `CoreBroken(installed, core) []string` | the modules a core version leaves without their core constraint, and every module that requires one |
| `ReadLock`, `WriteLock`, `LockOf`, `LockPath`, `ContentHash` | the lock |
| `Lock.Check`, `Lock.Fresh`, `Lock.Installed`, `Lock.Locked` | the lock against itself and the server |
| `Why`, `WhyNot`, `OutdatedOf` | the three questions |
| `InstalledOf([]resource.Declaration)` | the installed modules as the resolver reads them |

## enforcement

| rule | gate |
|---|---|
| 1 to 6 | `TestAManifestDeclaresItsModules`, `TestADependencyIsHeldToItsShape` in `sdk/manifest` |
| 8 | `TestAConstraintReadsAsComposerReadsIt`, `TestAConstraintComposerRefusesIsRefused` |
| 9, 10 | `TestAReleaseCarriesItsStability`, `TestStabilityIsHeldToTheMinimumAndTheFlag` |
| 3, 3a | `TestTheCoreIsAPlatformPackage`, `TestACoreUpgradeUpdatesTheModulesItBreaks` |
| 5 | `TestAConflictRefusesARelease`, `TestAConflictRefusesTheTwoTogether` |
| 13, 15 | `TestTheNewestReleaseInEveryRangeIsChosen`, `TestTwoRangesNoReleaseMeetsAreRefused` |
| 15 | `TestAModuleTheIndexDoesNotOfferIsRefused`, `TestAPlanBacksOutOfAChoiceALaterOneContradicts` |
| 16 | `TestAnInstallAddsTheModulesItRequires`, `TestAPlanThatMovedIsRefused` |
| 17, 20 | `TestAnUpdateTakesTheNewestReleaseAndWritesTheLock`, `TestWhyWhyNotAndOutdated` |
| 18, 21 to 23 | `TestTheLockReproducesTheInstalledModules`, `TestTheLockRoundTripsAndKnowsWhenItIsStale` |
| 19 | `TestARemovalADependantNeedsIsRefused`, `TestSwitchingOffThroughTheResourceAPIIsRefused` |
| 19a | `TestARemovalNamesTheModulesItLeavesUnused`, `TestUnusedModulesGoOnlyWhenAsked` |
| 24, 25 | `TestADependantStartsAfterItsDependency`, `TestADependencyThatStopsTakesItsDependantsDown` |
| 27 | `TestTwoModulesCallEachOther` |
| 28 | `TestACoreUpgradeTakesTheReleasesItBrings`, `TestCoreBrokenFollowsRequiresUpward` |
| 28, 29, 31 | `TestACoreUpgradeResolvesTheReleasesItBrings`, `TestUpgradeImpactsFollowTheModulePlan` |
| 32 | `TestUpgradeBackupProtectsTheModulesLock` |
| 30 | `TestAReleaseWithAnotherDigestIsRefusedBeforeItIsUnpacked` holds every download to its digest; review holds the staging |
| 2, 7, 11, 12, 14 | none. Review holds them |

## rejected

| option | why not |
|---|---|
| contracts with their own majors that any module provides | a dependency names the module, and its release is the signal |
| `provide` and `replace` | one module never stands in for another |
| several releases of one module side by side | a module has one namespace, one mount and one set of records |
| a forced removal that switches the dependants off | the operator removes the dependant first, and sees it go |
| npm's `~`, which reads `~1.2` as `<1.3` | Composer's reading is the model |
