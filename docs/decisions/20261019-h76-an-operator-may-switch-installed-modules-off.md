---
title: "ADR-20261019-h76: an operator may switch installed modules off"
description: "core.modules_enabled: false in the operator's configuration refuses every install, never starts or serves a module the server installed, and leaves the operator's own declarations running."
date: 2026-10-19
status: accepted
amends: [ADR-20260910-cqq, ADR-20260911-4p6, ADR-20261005-64j, ADR-20261008-hna, ADR-20261015-ejz, ADR-20261019-3ns]
tags:
  - adr
  - modules
  - security
---
# ADR-20261019-h76: an operator may switch installed modules off

- status: accepted
- date: 2026-10-19

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends: [ADR-20260910-cqq](20260910-cqq-graphql-and-the-old-schemas-are-modules.md) rule 2, [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md) rule 30, [ADR-20261005-64j](20261005-64j-a-deployment-may-read-a-module-catalogue-from-a-marketplace.md) rules 3 and 17, [ADR-20261008-hna](20261008-hna-an-operator-names-the-module-of-a-local-release.md) rule 2, [ADR-20261015-ejz](20261015-ejz-a-new-install-is-configured-by-a-wizard.md) rules 14 and 22, [ADR-20261019-3ns](20261019-3ns-a-module-declares-its-ports-and-gets-a-data-directory.md) rule 7

`core.modules_enabled: false` in `config.yaml` stops the server from
installing modules and from running the ones it installed. Modules the
operator declares in `config.yaml` keep running. Some deployments must run
only those. `core.marketplace.enabled: false` is not enough: it hides the
catalogue, but `POST /api/v1/modules` with `from` still installs a module. An
`asset` module, a folder of web files, then runs in the browser with the
session of the person signed in.

## rules

### what the key covers

1. `core.modules_enabled` in the operator's `config.yaml` is the key.
   Absent or `true` changes nothing. `false` switches installed modules off.
   The server reads it at start, so a change takes effect at the next start.
2. An installed module is an instance with runtime `service` or `asset` that
   the server wrote into `config.d/resources.json`, the file the server writes
   itself (the overlay). Only an install writes one: `module add`,
   `POST /api/v1/modules` or the web app. `resource.Declaration.Installed` is
   the one test for it.
3. An instance the operator declares in `config.yaml` is the operator's
   choice, and the key does not stop it, even a `service` or an `asset`.
   `builtin` and `script` instances are not modules and keep running too.
4. The operator decides. The first-run wizard never writes the key, and the
   overlay has no `core.*` key. A value in `config.d/setup.yaml` counts
   only where `config.yaml` leaves the key out, so a value the operator writes
   always wins.

### what the server refuses

5. `POST /api/v1/modules` and `module add` refuse with
   `core.resource/MODULES_DISABLED` (409) before they read or download
   anything. An update is an install and is refused the same way, and so
   is a change of a module's ports
   ([ADR-20261019-3ns](20261019-3ns-a-module-declares-its-ports-and-gets-a-data-directory.md)
   rule 7), which installs its release again.
6. `GET /api/v1/modules/index`, `GET /api/v1/modules/marketplace` and
   `PUT /api/v1/modules/marketplace` answer `MODULES_DISABLED`. The server
   reads neither the marketplace, nor the `index.json` file in the modules
   directory, nor the release directories `core.modules_local` names.
7. A `PUT` on a resource that switches an installed module from off to on is
   refused with `MODULES_DISABLED`. A write that leaves it as it is, switches
   it off or changes its settings is accepted. The web app sends `enabled` with
   every save, so a refusal of every write to an enabled module would block
   its settings.
8. The first-run wizard does not list installed modules. A saved choice that
   switches one on is skipped and logged. A choice that switches one off is
   applied, so the module stays off once modules are on again.

### what never runs and how the server reports it

9. The server treats an installed module as switched off whenever it applies
   the configuration, so the module never starts. As a second check,
   `Host.start` skips an installed module and logs a warning. An instance may
   be declared in both files, and then `config.yaml` wins. If the operator
   deletes it from `config.yaml` while the server runs, the server uses the
   installed copy from the next write through the API, and stops the running
   module then. After a restart the module does not start.
10. The server serves only a running module. Its UI bundle and its routes
    under `/api/v1/modules/<namespace>/` answer 404. A module may also serve
    a path outside `/api/v1/modules/`, such as `/graphql`. That path answers
    `MODULES_DISABLED`, marked as not retryable, and not the 503 of a module
    that may still start.
11. The resources listing shows the instance with state `disabled` and the
    condition `Started` false, reason `ModulesDisabled`. Readiness reports it
    as `disabled`. A restart answers `MODULES_DISABLED`. When it lists an
    installed module, `module list` starts with a line saying that installed
    modules are off. The listing gives the reason whatever the module's own
    flag says, because the key refuses every start. The start-up line and the module's own page promise that the module
    runs again with the key on, so both speak only of a module whose own flag
    is on.

### what still works and what the operator is told

12. Removal works: Remove in the web app, `DELETE /api/v1/modules/<namespace>`
    and `module remove`. The server deletes nothing by itself. An installed
    module keeps its files and records until the operator removes it.
13. At start the server logs one `WARN` line that names each installed module
    that would start if modules were on, as `kind:name`, which
    `module remove` takes. The line says how to
    remove them and how to start them again. A module switched off in the
    overlay is not named, because it would not start anyway. With no module
    named, the server logs one `INFO` line that says modules are off.
14. Setting the key back to `true` and restarting starts them again as
    declared.
15. `GET /api/v1/ui` answers `modules`, true when installed modules are on.
    While it is false the web app shows no catalogue, no local release rows,
    no marketplace badge or settings, and no install or update. An installed
    module shows a locked switch, locked ports and Remove, and its own pages
    say why they do not open.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `TestInstalledModulesAreOnUnlessTheFileSwitchesThemOff`, `internal/app/config` | exists |
| 2, 3 | `TestOnlyAModuleTheServerWroteIsInstalled`, `internal/app/resource` | exists |
| 3, 9, 11, 14 | `TestAnInstalledModuleNeverStartsWhileModulesAreOff`, `internal/app/resource` | exists |
| 4 | `TestTheWizardLeavesTheModulesKeyOut`, `internal/app` | exists |
| 5 | `TestNothingIsInstalledWhileModulesAreOff`, `internal/app` | exists |
| 5, 12 | `TestModuleAddIsRefusedAndRemoveWorksWhileModulesAreOff`, `internal/app` | exists |
| 6 | `TestTheCatalogueIsNotReadWhileModulesAreOff`, `internal/app` | exists |
| 6 | `TestTheMarketplaceAnswersModulesDisabledWhereNoneIsNamed`, `internal/app` | exists |
| 7 | `TestOnlySwitchingAnInstalledModuleOnIsRefused`, `internal/app/resource` | exists |
| 8 | `TestTheFirstRunLeavesAnInstalledModuleOffWhileModulesAreOff`, `internal/app` | exists |
| 8 | `TestTheFirstRunStillSwitchesAnInstalledModuleOff`, `internal/app` | exists |
| 9 | `TestAModuleThatMovesFromTheFileToTheOverlayIsNotStarted`, `internal/app/resource` | exists |
| 9 | `TestAHeldModuleIsNotStoppedAgainOnEveryReload`, `internal/app/resource` | exists |
| 9 | `TestTheHostStartsNoInstalledModuleWhileModulesAreOff`, `internal/app/resourcehost` | exists |
| 3, 9 | `TestTheHostStillStartsWhatTheOperatorDeclaresWhileModulesAreOff`, `internal/app/resourcehost` | exists |
| 9 | `TestThePlaneHoldsAnInstalledModuleWhileModulesAreOff`, `internal/app` | exists |
| 9, 10, 11 | `TestARunningServerStartsNoInstalledModuleWhileModulesAreOff`, `internal/app` | exists |
| 10, 11, 13 | `TestAModuleInstalledEarlierIsNotServedOnceModulesAreOff`, `internal/app` | exists |
| 10 | `TestARootMountSaysModulesAreOff`, `internal/app` | exists |
| 10 | `TestAPrefixOfAModuleTheOperatorSwitchedOffAnswersModulesDisabled`, `internal/transport/http/httpapi` | exists |
| 11 | `TestAnInstalledModuleIsNotRestartedWhileModulesAreOff`, `internal/app/resource` | exists |
| 11 | `TestTheCommandsSpeakOnlyOfWhatIsHeld`, `internal/app` | exists |
| 12 | `TestAnInstalledModuleIsRemovedWhileModulesAreOff`, `internal/app` | exists |
| 13 | `TestTheWarningLeavesOutAModuleSwitchedOffInTheOverlay`, `internal/app` | exists |
| 13 | `TestTheServerSaysModulesAreOffWhenNoneIsInstalled`, `internal/app` | exists |
| 15 | `TestTheUISettingsSayWhetherModulesAreOn`, `internal/transport/http/httpapi` | exists |
| 15 | `TestTheAppIsToldWhetherModulesAreOn`, `internal/app` | exists |
| 15 | `frontend/src/app.svelte.test.ts` | exists |
| 15 | `frontend/src/lib/screens/extensions.svelte.test.ts` | exists |
| 15 | `frontend/src/lib/screens/extension-detail.svelte.test.ts` | exists |
| 15 | `frontend/src/lib/screens/module-page.svelte.test.ts` | exists |
| the key | `make config-reference-check` | exists |
| the operations | `make openapi-check` | exists |
| the compatibility of the API | `make api-check` | exists |
| the fault code | `make error-codes-check` | exists |

## rejected

| option | why not |
|---|---|
| one check in `Host.start` only | the listing would say `pending` without a reason, and an instance moved from the file to the overlay would keep running |
| switch off every module, the operator's own too | the operator chose those in `config.yaml`. The key stops what a person installs through the server |
| delete installed modules at start | it cannot be undone and deletes the module's records |
| answer `InstallNotConfigured` | it means the build has no installer, so an operator would look for a broken build |
| a key the web app or the overlay may set | a key a signed-in person can set back to `true` protects nothing |
| reuse `core.marketplace.enabled` | it hides the catalogue only, and an install with `from` still installs |

## open items

- A browser tab that loaded a module before the restart keeps running it
  until the page is reloaded. The next load gets 404.
