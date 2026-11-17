---
title: "ADR-20261030-7e2: Safe mode restarts the server once with every module off"
description: "How an operator starts the server once with every module off, what that run clears and keeps, and who may ask for it."
date: 2026-10-30
status: accepted
supersedes: []
amends: [ADR-20261019-h76, ADR-20261015-ejz, ADR-20261027-5m4]
tags:
  - adr
  - extensions
  - modules
  - operations
---
# ADR-20261030-7e2: Safe mode restarts the server once with every module off

- status: accepted
- amends [ADR-20261019-h76](20261019-h76-an-operator-may-switch-installed-modules-off.md)
  rules 1, 3, 4, 11, 13 and 15. A start can hold modules off without the key
- amends [ADR-20261015-ejz](20261015-ejz-a-new-install-is-configured-by-a-wizard.md)
  rule 21. A restart can drop an argument
- amends [ADR-20261027-5m4](20261027-5m4-a-service-module-runs-under-a-memory-budget.md)
  rules 13 and 27. Start can refuse a module, and the hold has a second source
- date: 2026-10-30

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20261019-h76](20261019-h76-an-operator-may-switch-installed-modules-off.md), [ADR-20261015-ejz](20261015-ejz-a-new-install-is-configured-by-a-wizard.md) and [ADR-20261027-5m4](20261027-5m4-a-service-module-runs-under-a-memory-budget.md)

A module can break the server so that the operator cannot open the page that fixes it. The operator then needs one start with every module off, without editing `config.yaml`. This record rules how that start works. It also rules what the start changes and what it leaves alone.

## rules

### entering safe mode

1. Safe mode starts in two ways. The flag `-safe` starts one process in it. The call `POST /api/v1/server/restart` with `mode: safe` writes the marker `config.d/safe-mode.json` beside the overlay and restarts the server. Nothing else starts it. The server never enters safe mode on its own.
2. The start that finds the marker deletes it before it serves. Safe mode therefore lasts for one process. A restart that the server performs drops `-safe` from its arguments. This amends rule 21 of ADR-20261015-ejz: the new process has the same binary and the same arguments, without `-safe`.
3. The server carries safe mode as `config.Core.SafeMode`, a field with the tag `yaml:"-"`. It is not a key. No file sets it, and the operator cannot write it into `config.yaml`.

### what safe mode holds off

4. In safe mode the run behaves as if `core.modules_enabled` were `false`. This amends rule 1 of ADR-20261019-h76: the key is no longer the only source of the hold.
5. Safe mode also holds off the `service` and `asset` instances that the operator declares in `config.yaml`, for this run only. This amends rule 3 of ADR-20261019-h76. An asset bundle declared there can break the page that holds the exit button.
6. `builtin` and `script` instances keep running. They are not modules.
7. This amends rule 13 of ADR-20261027-5m4. Start never refuses a module in normal mode. In safe mode no module starts, whether it comes from the overlay or from `config.yaml`.
8. No module runs in safe mode, so the host starts no memory watch and no hang watch. The module log and the memory samples live in memory. The Log tab and the memory chart of the module that broke the server are empty.

### what safe mode keeps and clears

9. Safe mode never writes the overlay. The record `switched_off` and the overlay value `enabled: false` stay through safe mode and after it. The module stays off until the operator switches it on in a normal run.
10. Switching a module on is refused in safe mode with the fault `SAFE_MODE`. The operator may remove a module, or edit `memory_mib`, `on_over_memory` and `switch_off`, because those are settings writes. The server keeps them.
11. `core.modules_switch_off` is a configuration key. Safe mode does not touch it.
12. This amends rule 27 of ADR-20261027-5m4. The hold of an instance declared in `config.yaml` is in memory and ends when the server restarts. A start into safe mode begins with an empty hold, and safe mode holds the instance instead. The field `held_until_restart` is false for the whole run. A normal restart starts the instance again.
13. `failures_in_window` lives in the memory of the process. It is 0 after any server restart, and in safe mode it stays 0. Safe mode does not reset the count for an operator. Rule 25 of ADR-20261027-5m4 speaks of a restart of one module, not of the server.
14. The pool of ADR-20261027-5m4 still counts every enabled service budget in safe mode. Rule 12 of that record says a module that is switched off counts nothing, and "switched off" means `enabled: false`, not held. The bar shows what a normal start will commit.

### what the operator sees

15. Safe mode has its own condition reason `SafeMode` and fault `SAFE_MODE`. The listing, the readiness and the refusals use them instead of `ModulesDisabled`. This amends rule 11 of ADR-20261019-h76.
16. The start-up log line names safe mode and says how to leave it. This amends rule 13 of ADR-20261019-h76.
17. `GET /api/v1/ui` answers `modules: false` and `safe_mode` with the fields `reason` and `since`. This amends rule 15 of ADR-20261019-h76. It never names who asked for the restart.
18. The overlay never writes `core.modules_enabled`, and the first-run wizard never writes it. Rule 4 of ADR-20261019-h76 holds. The marker is a file of its own and is not a `core` key. The overlay still has no `core.*` key.
19. The web app shows a banner in safe mode. The switched-off alerts and the pool bar of the extensions page stay as they are, and the header Restart of a module is disabled while modules are off.

### asking for a restart

20. The restart uses the maintenance surface that exists: `POST /api/v1/server/restart` beside the other `/api/v1/server/*` calls. It takes the organization admin role, as `requireMaintenance` already demands for the diagnostics bundle and for backup and restore.
21. A restart cuts every printer stream and every module. A grant on one resource kind is not a grant on the server. Under open access the role is everyone on the network, the same people who may already remove modules.
22. The server logs the restart at `INFO` with the principal. The marker holds no principal.
23. A restart that the server was asked for goes on even when the plane did not stop within its budget. A module that hangs on stop must not block it. The server logs the wedge at `WARN`.
24. A restart for an upgrade or for a restore keeps the clean stop. Those two touch the database and the files.
25. Ceiling: on Windows under a service manager, the new process looks like a stop to the manager. This record documents the case and does not fix it.

## enforcement

- `go test ./internal/app/... ./internal/app/resource/ ./internal/app/resourcehost/ ./internal/transport/http/httpapi/` covers the flag, the marker, the hold, the refusal of a switch-on, the maintenance gate and the restart after a wedge.
- `make error-codes-check` fails when `SAFE_MODE` is missing from the generated list.
- `make docs-check` and `make style-check` cover the configuration page and this record.

## rejected

| option | why not |
|---|---|
| Set `core.modules_enabled: false` and restart | It needs a file edit, and it does not hold off modules declared in `config.yaml`. |
| Enter safe mode on its own after repeated crashes | The server would change its behaviour without the operator knowing. The operator decides. |
| Clear `switched_off` and the failure count in safe mode | Safe mode is for diagnosis. It would hide the module that failed. |
| A new restart interface and a new `Config` field | The `/api/v1/server/*` maintenance surface and its admin gate already exist. |
| The role `configure_resource` for the restart | A restart cuts every printer stream, so it needs a grant on the server. |
| Wait for a hanging module before the restart | A wedged plane would hold the restart, and the operator would have no way out. |
