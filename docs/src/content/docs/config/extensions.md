---
title: "Extensions"
description: "Where the Extensions page finds modules, where the server installs them, and how to switch installed modules off."
audience: run
rewrite:
  describes: current
  implementedBy:
    - internal/app/config
    - cmd/generate-config-reference
---

Modules add pages, cards and services to the server. The Extensions page lists
the ones on the Prusa marketplace, and installs a module with one click.
`core.marketplace.enabled` turns the listing off, and the server then fetches
nothing from it. `core.marketplace.url` names a marketplace of your own.

`core.modules_enabled: false` switches off every module installed through the
server: from the Extensions page, the API or `module add`. The server then
refuses every install, and it starts none of those modules. A module you write
under `resources` in `config.yaml` still runs, and an installed module can
still be removed. Restart the server after you change the key.

To keep a module running while `core.modules_enabled` is `false`, declare it
yourself:

1. While the key is still `true`, install the module. Copy its directory out of
   the modules directory, `config.d/modules/<name>` by default, to a directory
   of your own.
2. Note its declaration in `config.d/resources.json`. Then remove the
   installed copy with `prusa-connect module remove <kind:name>`. The
   removal also takes back the grants the install wrote. A module that acts
   as itself, with `permissions` in its declaration, needs them written again
   with `POST /api/v1/grants/org/local`, as
   [writing your first module](../writing-your-first-module.md#mint-a-token-and-read-the-count)
   shows.
3. Write the declaration under `resources` in `config.yaml`, with its paths
   pointing at your copy, as
   [the declaration an operator writes](../writing-a-module.md#the-declaration-an-operator-writes)
   shows.
4. Set the key to `false` and restart the server.

The other keys say where installed modules live and how large a module's
settings may be. See [Extensions](../extensions.md).

## How much memory a module may use

Each service module runs under a memory budget. The budget is `memory_mib` on
the module's resource when you set it, then the module's own recommendation,
then 128 MiB. The Memory tab of a module's page on the Extensions page edits
the budget too; saving restarts a running module, and "Use the recommendation"
clears it.

The budgets of all enabled service modules are meant to fit in one pool. Set the pool
in bytes with `core.modules_memory`. Without the key, the pool is the machine's
memory, or the container's limit when that is smaller, less a reserve for the
server. The reserve is a quarter of the memory, and at least 256 MiB. A machine
with 256 MiB or less therefore has a pool of zero. Set `core.modules_memory` to
give modules more.

The pool is a warning, not a limit. The server installs, switches on, raises
and upgrades a module whatever the pool holds, and at start it starts every
enabled module. `GET /api/v1/modules/memory` reports the pool and the sum of
the budgets of the enabled modules.

The Extensions page shows the memory a module will ask for before you switch
it on, and who set the figure: you, the module's author or the server default.
A pool that the budgets pass shows a warning there, and you can still switch
the module on.

The listing shows the pool as a bar: the memory the enabled budgets add up to,
against the pool. A bar over the pool is a warning and nothing more. Each
service module has a memory cell in MiB. Its failures show as
`failures_in_window` together with the window that is configured for it.
Modules the server switched off are in a block of their own, with the reason
for each. The server also sends a bell notice when it switches a module off.

The server reads each module's memory every two seconds. A module runs in a
session of its own, and the figure adds up every process the server finds
under it. A module over its budget is stopped and started again, and the stop
kills every one of those processes. On Linux the server finds them by the
session, on macOS by the process group and the parent links. When the module's
`on_over_memory` is `warn`, the server keeps it running instead, logs a warning
and sets `status.memory.over_budget`. On Windows a `warn` module still stops
when it passes the hard limit of its job, which is its budget plus 25 %. If the
system itself kills the process for memory, that counts as a crash. A module
that fails three times within one hour
is switched off, whatever the reason: over its budget, a crash, or a failed
start. A restart you ask for does not count, and neither does a start that
waits for its database or for a module it requires that is not serving yet.
The server keeps retrying that start and starts the module once the
dependency serves.

Set other numbers for every module with `core.modules_switch_off`:
`failures` from 0 to 100, and `window` from `1m` to `24h`. With `failures: 0`
the server never switches a module off for failing. A module whose start keeps
failing still gives up after an hour and five attempts, as any optional
resource does. With a high limit that can come first. Set them for one module with `service.switch_off` on its
resource, or under Automatic switch-off on the module's Memory tab. A value you
leave out there follows the server.

```yaml
core:
  modules_switch_off:
    failures: 5
    window: 6h
```

- An installed module is switched off in `config.d/resources.json`, with a
  record of the stops. It stays off after a server restart.
- A module you declared in `config.yaml` is held off until the server
  restarts. The server never writes to your file.

To switch a module on again:

1. Read why it stopped in `status.memory.switched_off` of
   `GET /api/v1/resources/{kind}/{name}`.
2. When the stops say `over-memory`, raise its `memory_mib`, or its Memory
   budget on the module's page of the Extensions page.
3. Switch it on on the Extensions page, or write `enabled: true`. That clears
   the record and the count. A module declared in `config.yaml` runs again when
   the server restarts.

## Watching a module

The page of a service module has a Memory tab, a Log tab and a Restart button
in its header.

The Memory tab draws a chart of the module's memory over the last hour. A line
marks the budget and a marker marks each stop. Where the server could not read
the memory, the line has a gap. Under the chart, a list shows the recent stops
and why each happened.

The Log tab shows the newest 200 lines or 64 KiB of the module's standard
error, whichever is smaller. The text is not redacted, so it can hold anything
the module prints. You need the `configure_resource` grant on the module's
kind to read it. A person with the grant sees the tab, and a person without it
gets a refusal. The same data is at `GET /api/v1/resources/{kind}/{name}/log`,
and the memory history is at `GET /api/v1/resources/{kind}/{name}/memory`.

Restart stops the module and starts it again. It does not count as a failure.

A module's state names the reason for it. A module that did not answer the
server's checks shows "Hung and was restarted".

The server keeps the history and the log in memory. A server restart clears
both.

## Safe mode

A module can break the server so badly that you cannot open the page that
fixes it. Safe mode starts the server once with every module off.

Start the server with `-safe`, or ask for a restart in safe mode under
Settings → Server → Overview in the web app. That restart writes the marker
`config.d/safe-mode.json` beside the overlay. The next start finds the marker,
deletes it and runs in safe mode. If the server cannot delete it, the log names the file. Delete it
yourself, or the start after that is in safe mode too.

An organization administrator can ask for the restart with
`POST /api/v1/server/restart` and the body `{"mode":"safe"}`; `{"mode":"normal"}`
restarts normally. The restart stops every printer stream, upload and module,
and the call answers 202 before the server goes down. `GET /api/v1/ui` answers
`safe_mode` with the reason and the time while the server is in safe mode. It
never names who asked.

In safe mode no `service` or `asset` module starts. That includes the modules
you declared in `config.yaml`. `builtin` and `script` instances keep running.
An install, a catalogue read and switching a module on answer `SAFE_MODE`.

These still work:

- removing a module
- editing a module's settings
- editing `memory_mib`, `on_over_memory` and `switch_off`

Safe mode writes nothing to the overlay. A module the server switched off
stays off, with its alert, until you switch it on in a normal run. The failure
count of every module is 0 after any server restart. The Log tab and the
memory chart are empty, because no module runs.

While safe mode is on, every page of the web app shows a banner. Its Restart
normally button, and the same button under Settings → Server → Overview, leave
safe mode. Both appear only for an organization administrator. The Extensions
page says that safe mode is on, and the Restart button of an extension is off.

To leave safe mode, restart normally in the web app, or restart the server
without `-safe`. A restart the server performs never keeps `-safe`.

A restart you ask for goes on even when a module does not stop in time. The
server logs a warning and starts again without waiting for it.

On Windows under a service manager, the new process looks like a stop to the
manager. Start the service again yourself there.

## Keys

<!-- generated by make generate-config-reference for every key the core reads. Do not edit: change the struct and its row in config.Keys or resource.Keys, then regenerate. -->

### core

Every key has a working default, so a file with no `core:` block starts a server. A key this build does not read is refused at startup, with the line it is written on.

| key | type | default | required | what it does |
|---|---|---|---|---|
| <a id="key-core-modules"></a>`core.modules` | text | `config.d/modules` beside your file | no | The directory the server and the module add command install a module into. |
| <a id="key-core-modules-local"></a>`core.modules_local` | mapping | empty | no | Release directories on this server the Extensions page offers to install, each under the module path written as its key. |
| <a id="key-core-modules-minimum-stability"></a>`core.modules_minimum_stability` | text | `stable` | no | The lowest stability of a module release an install takes: dev, alpha, beta, RC or stable, as Composer's minimum-stability. A constraint such as ^1.2@beta lowers it for one module. |
| <a id="key-core-modules-prefer-stable"></a>`core.modules_prefer_stable` | true or false | `false` | no | Take the most stable release a constraint allows before the newest one, as Composer's prefer-stable. |
| <a id="key-core-modules-enabled"></a>`core.modules_enabled` | true or false | `true` | no | Write false to refuse every module install and to start no module the server installed. Modules declared in this file still run. Read at start. |
| <a id="key-core-marketplace-enabled"></a>`core.marketplace.enabled` | true or false | `true` | no | Whether the Extensions page reads the marketplace catalogue. Write false to read none, and the server then fetches nothing from it. |
| <a id="key-core-marketplace-url"></a>`core.marketplace.url` | text | `https://plugin-marketplace.prusa3d.com` | no | The marketplace whose module catalogue the Extensions page lists and installs from. An https address; http only for a loopback host. |
| <a id="key-core-marketplace-timeout"></a>`core.marketplace.timeout` | duration | `10s` | no | How long one read of the marketplace catalogue waits. |
| <a id="key-core-module-settings-bytes"></a>`core.module_settings_bytes` | number | `4096` | no | The largest size, in bytes, of one record that the settings pane of a module writes. At most 65536. |
| <a id="key-core-modules-memory"></a>`core.modules_memory` | number | - | no | The bytes the budgets of all enabled service modules must fit in, at least 67108864. When absent, the machine's memory less a quarter, keeping at least 256 MiB. |
| <a id="key-core-modules-switch-off-failures"></a>`core.modules_switch_off.failures` | number | `3` | no | How many failed starts or stops inside the window switch a service module off, from 0 to 100. 0 never switches one off. A module may set its own. |
| <a id="key-core-modules-switch-off-window"></a>`core.modules_switch_off.window` | duration | `1h` | no | How far back failed starts and stops count towards switching a service module off, from 1m to 24h. A module may set its own. |

<!-- end generated -->
