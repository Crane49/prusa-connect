---
title: Bench preflight
description: What to do about each check in scripts/bench-preflight.ps1, and what a printer on a direct ethernet cable needs.
audience: build
rewrite:
  describes: current
  implementedBy:
    - scripts/bench-preflight.ps1
---

A printer plugged straight into a Windows machine fails in ways that all look
the same from the printer's side, which is nothing happening. The preflight
checks each cause separately and names the one that is wrong. This page says
what to do about each answer it gives.

## Run the preflight

| command | does |
|---|---|
| `make preflight` | run every check against port 8080 |
| `make preflight PREFLIGHT_ARGS="-Port 80"` | run them against the port shipping firmware dials |
| `make preflight PREFLIGHT_ARGS="-AdapterName 'Ethernet 2'"` | use the adapter the cable is in, as `Get-NetAdapter` names it |
| `make preflight PREFLIGHT_ARGS="-Fix"` | create the inbound firewall rule, in an elevated shell |
| `powershell -ExecutionPolicy Bypass -File scripts/bench-preflight.ps1` | the same checks without make |

`make preflight` names which interpreter it took before it runs anything. It
prefers `pwsh` and falls back to Windows PowerShell. A machine with neither
fails and says so, because a preflight that did not run has proved nothing.

Each check prints one marker and then what it observed.

| marker | means |
|---|---|
| `OK` | the check observed the thing, and the line says what it saw |
| `FAIL` | the check observed something wrong, and the line says what to do |
| `WARN` | this shell could not observe the thing at all |

`WARN` is not a pass. The script exits 1 for a `WARN` as it does for a `FAIL`,
because a check that could not run has proved nothing.

## Key files

| path | purpose |
|---|---|
| `scripts/bench-preflight.ps1` | the checks |
| `config/config.yaml` | `core.api.addr` and `core.connectors.<name>.config.listener.addr`, the two listeners |
| `examples/prusa_printer_settings.ini.example` | the file a printer reads from a USB stick |

## Ethernet link is down

The adapter reports `Disconnected`. Seat the cable at both ends, power the
printer on, and give the link a few seconds. If the printer is on and the link
stays down, the cable or one of the two sockets is dead.

`Get-NetAdapter` lists every adapter with its name and status. Pass the right
one with `-AdapterName` when the cable is not in the adapter called `Ethernet`.

## Adapter has no IPv4 address

Windows waits about a minute after the link comes up and then assigns itself an
address in `169.254.0.0/16`. Wait that minute, or set a static address as
[addresses a direct cable needs](#addresses-a-direct-cable-needs) describes.

The `OK` line names how the address arrived, which is the answer to whether a
DHCP server exists on this cable.

| the line says | means |
|---|---|
| self-assigned | no DHCP server answered, and Windows chose the address itself |
| handed out by a DHCP server | something on this cable serves DHCP |
| set manually on this adapter | a static address, which is what a bench cable usually wants |

## Printer does not answer on the cable

Nothing has spoken on that adapter, so Windows holds no neighbour entry for it.
Either the printer is off, or it has no address, or its address is outside this
machine's subnet.

The check reports the second case as a subnet mismatch and prints both
addresses. That is the shape a missing DHCP server takes: this machine
self-assigns in `169.254`, the printer holds an address from an old network, and
neither can reach the other. Give both ends a static address in one subnet.

## Firewall admits no rule for the port

Windows Firewall is on for the Domain, Private and Public profiles by default,
and a direct cable usually lands on Public. Inbound is blocked there, so the
printer's connection is dropped before the core logs anything.

Run the script again with `-Fix` in an elevated shell. It creates one rule:

```
New-NetFirewallRule -DisplayName "prusa-connect bench (TCP 8080)" `
    -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8080 -Profile Any
```

A `WARN` on this check means no rule was found and the rule list came back
short. Reading every inbound rule needs administrator rights, so a shell
without them cannot separate a missing rule from an unreadable one. Re-run in
an elevated shell to settle it.

The check counts only a rule open to every program. Windows ships many inbound
rules whose port is `Any` and whose program is one named binary, and none of
those admits the core. On the machine this was written on, 71 rules match TCP
8080 that way, and every one belongs to Wi-Fi Direct printing, Remote
Assistance or a store app.

## Nothing listens on the port

Build the binary and run it:

```
make build
.\bin\server.exe core --config config\config.yaml
```

The startup line names the address it bound and the health URL:

```
level=INFO msg=serving api=[::]:8080 health=http://[::]:8080/api/v1/health
```

## Listener is bound to loopback

The socket answers every request from this machine and is invisible on the
cable. Set `core.api.addr` to `":8080"` in `config/config.yaml`. An address of
`"127.0.0.1:8080"` binds loopback alone.

## Container publishes the port

`docker-compose.local.yml` publishes a core of its own on 8080. A printer
pointed at this machine reaches that one rather than the one you started.
Nothing about it looks wrong: the printer connects, `/api/v1/health` answers
200, and the `/dev` page you are watching stays empty.

A container started from an older image is worse, because it runs the shipping
server: the printer connects there too and no `/api/v1` path answers at all.

Stop the container, or move the core to a port nothing publishes:

```
docker compose -f docker-compose.local.yml stop prusa-connect
```

The service is named `prusa-connect` and the container is named
`prusa_connect`. `docker compose ... stop server` fails with
`no such service: server`.

## Wrong server answers on the port

Something is listening, it is reachable, and `GET /api/v1/health` did not
return 200. Whatever holds the port is not the core. Find its owner:

```
Get-NetTCPConnection -State Listen -LocalPort 8080 |
    ForEach-Object { Get-Process -Id $_.OwningProcess }
```

A port published by Docker Desktop is held by `com.docker.backend` or
`wslrelay` rather than by the container itself, so those names on the port mean
a container answers.

## Connector does not answer the printer wire

`POST /p/register` returned a status and no `Code` header. The core is running
and no printer can register with it.

The API and the printer wire are different surfaces. `/api/v1` is served by the
API, and `/p/register` is served by a connector. A core with no xBuddy
connector mounted answers the first and not the second, and `/api/v1/health`
stays green throughout.

Read the log line the core prints at startup:

```
level=INFO msg="connector mounted" connector=xbuddy type=core/xbuddy routes="[/p/register /p/ws]"
```

`routes` is the answer. A core mounting only the reference dialect prints
`routes=[/connectors/lab/*]`, and nothing serves `/p/register`.

Leave `core.connectors` out of the configuration. A file that names no
connector mounts the shipping xBuddy dialect, so no deployment with printers in
the field has to be edited.

## Addresses a direct cable needs

A cable between two machines carries no router, so nothing hands out an
address. Static addresses on both ends are the option that depends on nothing:

| end | set |
|---|---|
| this machine | `New-NetIPAddress -InterfaceAlias Ethernet -IPAddress 192.168.77.1 -PrefixLength 24` |
| the printer | Settings, Network, Ethernet: address `192.168.77.2`, mask `255.255.255.0` |

Then the printer's INI names this machine:

```ini
[service::connect]
hostname = 192.168.77.1
port = 8080
tls = false
```

`port` is the core's API port while the connector's `listener.addr` is empty,
because printers dial the API listener then. Set that address and the printer
dials that port instead. [Printer Configuration](./printer-configuration.md)
covers the INI file and the menus that load it, and
[Custom Certificates](./certificates.md) covers `tls = true`.

Remove the static address afterwards:

```
Remove-NetIPAddress -InterfaceAlias Ethernet -IPAddress 192.168.77.1 -Confirm:$false
```

## The reply that names which server answered

The core serves `/api/v1`. The shipping server has no path under it and answers
404 there, so one request settles which of the two holds the port:

```
curl http://127.0.0.1:8080/api/v1/health
```

| reply | which server |
|---|---|
| `{"status":"ok","version":"..."}` with status 200, `application/json` | a core |
| `404 page not found` with status 404, `text/plain` | the shipping server |

It says which server, not which process. The container runs a core as well, so
a 200 does not prove the answer came from the one you started. Read the version
in the reply, or stop the container and ask again.

This is the last check the preflight runs, so a green run has already asked.

## What the printer is told

Three keys, and nothing else. The firmware dials `POST /p/register`,
`GET /p/register` and `GET /p/ws` at whatever host it is given.

| key | set to |
|---|---|
| `hostname` | this machine's address on the cable, which the preflight prints |
| `port` | the core's API port, or the connector's `listener.addr` port when that is set |
| `tls` | `false` unless a certificate is configured. See [Custom Certificates](./certificates.md) |

`token` is ignored on the shipping defaults. The connector runs with
`mode: none` and `auto_register: true`, so any printer that dials is admitted
and given a token of its own. No operator approves anything.

## Prove the printer wire before the printer arrives

The two registration requests are plain HTTP, so this machine can make them
itself. A green run here says the core answers what the firmware asks for, and
leaves only the cable and the printer's own configuration in question.

```powershell
curl.exe -s -D - -o NUL -X POST http://127.0.0.1:8080/p/register `
    -H "Content-Type: application/json" `
    -d '{\"sn\":\"BENCH-0001\",\"fingerprint\":\"AAAABBBBCCCCDDDDEEEEFFFF00001111\",\"printer_type\":\"7.1.0\",\"firmware\":\"6.4.0-0\"}'
```

It answers `200` and a `Code` header. Poll with that code:

```powershell
curl.exe -s -D - -o NUL -X GET http://127.0.0.1:8080/p/register -H "Code: <the code>"
```

| answer | means |
|---|---|
| `200` with a `Token` header | approved. This is the shipping default |
| `202` | somebody has to approve it. `auto_register` is off |
| `404` | no registration answers to that code |
| `503` with `Retry-After` | the registration table holds 400 entries an operator approved |

A `404` on a code that worked earlier means the registration was dropped to make
room. The core keeps 400 of them, and the log names each one it drops:

```
level=WARN msg="a registration was dropped to make room for another" connector=xbuddy dropped=... fingerprint=...
```

A printer that collected its token is not dropped while any registration nobody
completed is still in the table. So a bench printer that finished the two
requests above keeps its code. Raise
`core.connectors.<name>.config.max_registrations` for a fleet above 400.

Measured against this binary: `Code: 5GUC3V4UX5`, then `Token: RV7U7FV4ZY7TR5LFMLFG`.

A path beside those two is refused, and so is any write the frontend would
otherwise answer with a page. Measured: `POST /p/nothing` and `GET /p/nothing`
both return `404 core.validation/ROUTE_NOT_FOUND` as
`application/problem+json`, and so does `POST /`.

Still read the `Code` header and never the status. One case remains open. A
core running a connector that is not the xBuddy dialect reserves that dialect's
own prefix nowhere. `GET /p/register` on such a core therefore reaches the
frontend and answers `200` with the page. A `200` with no `Code` header in it is that
case.

## Signs that the printer reached the core

The log records the admission at `INFO`, naming the printer, its model and its
serial:

```
level=INFO msg="a printer was admitted" printer=... model=... serial=...
```

The developer view at `http://127.0.0.1:8080/dev` lists the printer under
`printers`, with its capability count and its session. The page is described in
[Developer view](./developer-view.md).

Open `/dev` and not `/`. The frontend at `/` is the one written for the
shipping server. It asks for `/api/printers`, `/api/events` and eight more
paths under `/api`, and this core publishes none of them. Measured:
`GET /api/printers` answers `404 core.validation/ROUTE_NOT_FOUND`. The page
loads and stays empty, which reads exactly like a printer that never arrived.

`GET /api/v1/devices` returns the same fleet as JSON. It takes the configured
principal while `core.auth.issuer` is empty, so a bench run needs no login.

## Drive the printer from the bench

Four requests read the status, list the files, start a print and cancel it.
Every one takes the printer id that `GET /api/v1/devices` returned.

In PowerShell, write `curl.exe`. Plain `curl` is an alias for
`Invoke-WebRequest`, which takes different arguments and prints a different
answer.

Escape every quote inside a JSON body with a backslash. PowerShell removes an
unescaped one before `curl.exe` sees it, and the server answers 422 naming the
body it received:

```
"detail":"validation failed invalid character 'v' looking for beginning of
object key string (body: {verb:core.command.stop_print})"
```

Set the id once:

```powershell
$id = (curl.exe -s http://127.0.0.1:8080/api/v1/devices |
    ConvertFrom-Json).items[0].id
```

### Step 1, read the status

```powershell
curl.exe -s http://127.0.0.1:8080/api/v1/devices/$id
```

The `state` member is the printer's own tree. `/state` holds the word the
machine reports, such as `IDLE` or `PRINTING`.

A printer that has just arrived carries an empty tree. It fills on the first
telemetry frame, which took 230 ms against a simulated CORE One. Ask for one
instead of waiting:

```powershell
curl.exe -s -X POST http://127.0.0.1:8080/api/v1/devices/$id/commands `
    -H "Content-Type: application/json" `
    -d '{\"verb\":\"core.command.send_state_info\"}'
```

### Step 2, list the files

The storages come first, because an empty path lists the devices themselves
and every other path hangs off one of them:

```powershell
curl.exe -s http://127.0.0.1:8080/api/v1/devices/$id/storages
curl.exe -s "http://127.0.0.1:8080/api/v1/devices/$id/files?path=usb"
```

The listing is answered from the machine, one directory per request. Take the
`path` of an entry whose `folder` is false and print that.

### Step 3, start a print

```powershell
curl.exe -s -X POST http://127.0.0.1:8080/api/v1/devices/$id/commands `
    -H "Content-Type: application/json" `
    -d '{\"verb\":\"core.command.start_print\",\"args\":{\"path\":\"usb/tangle.bgcode\"}}'
```

`path` is the storage-prefixed spelling the listing returned, with no leading
slash. The driver rewrites it to the firmware's own absolute form.

### Step 4, cancel the print

```powershell
curl.exe -s -X POST http://127.0.0.1:8080/api/v1/devices/$id/commands `
    -H "Content-Type: application/json" `
    -d '{\"verb\":\"core.command.stop_print\"}'
```

`/state` settles at `STOPPED`. A 200 alone says the queue took the command, so
read the state to know the machine acted on it.

### The four answers, as measured

`TestTheBenchWalkthroughReadsListsPrintsAndCancels` in `test/e2e` runs these
four steps in order against the binary this repository builds, with a
simulated CORE One on a real socket. Every command above was also typed into
Windows PowerShell against that binary on port 8080. Both observed:

| step | answer |
|---|---|
| read the status | `model=CoreOne firmware=6.4.0-0 connected=true`, then `/state=IDLE` |
| list the storages | `{"mountpoint":"/usb","name":"usb","type":"USB","free_space":1073741824}` |
| list `usb` | `usb/examples` as a folder, and `usb/tangle.bgcode` at 1048576 bytes |
| start the print | `{"ok":true}`, and `/state` becomes `PRINTING` |
| cancel the print | `{"ok":true}`, and `/state` settles at `STOPPED` |

### Watch the printer while you touch it

The event stream is the fastest way to see what the machine reports as it
happens. Leave it open in one window and press buttons on the printer:

```powershell
curl.exe -s -N -H "Accept: text/event-stream" http://127.0.0.1:8080/api/v1/events
```

The first line is `retry: 2000`, which says the stream is open and nothing has
happened yet. Each frame after it is one CloudEvent. Measured:

```json
{"specversion":"1.0","id":"01M10GRRE3XCAEVMWFZHD1RVRF",
 "source":"urn:prusa:site:e2e/device/0c95d38a-4e71-4b06-9f28-73a1c5e04b6f",
 "type":"com.prusa.connect.device.telemetry.sampled",
 "subject":"device:0c95d38a-4e71-4b06-9f28-73a1c5e04b6f",
 "data":{"path":"/state","value":"IDLE"}}
```

The `type` names what happened, and [Event Types](./event-types.md) lists every
one. A printer arriving, going unreachable and leaving each carry their own,
so a stream that goes quiet is a printer that stopped reporting.

### Refusals the printer's own state explains

The four steps above are in the order a machine takes them. Out of that order a
printer refuses, and the refusal is the correct answer rather than a broken
connector. Ten conditions read that way:

| you sent | the machine's condition | confirmed |
|---|---|---|
| `core.command.start_print` | a job is already running or paused | yes |
| `core.command.pause_print` | nothing is printing | yes |
| `core.command.resume_print` | nothing is paused | yes |
| `core.command.stop_print` | there is no job | yes |
| `core.command.set_printer_ready` | a job is on the bed | yes |
| `core.command.set_idle` | a job is on the bed | yes |
| `core.command.dialog_action` | the display shows no dialog | yes |
| `core.command.dialog_action` | `dialog_id` names a different dialog | yes |
| `core.command.dialog_action` | `button` is not one the dialog offers | yes |
| `core.command.cancel_printer_ready` | a job is on the bed | **no** |

Each answers `200 {"ok":false}` with the machine's own word in
`machine_reason`. The simulator refuses all ten as well, so the walkthrough
rehearses against the same rules the bench meets.

The confirmed column comes from the nine refusals earlier research read out of the
retired Python emulator, which copied them from the firmware. That set accepts
the last row rather than refusing it. The simulator refuses it because a machine
that is printing is not in the ready state, so there is nothing to cancel. That
is a guess, and the bench is where it is settled: send it during a print and
read what comes back.

**Record what the machine writes in `machine_reason`.** The simulator answers
`INVALID_STATE`, `NO_DIALOG`, `INVALID_DIALOG_ID` and `INVALID_BUTTON`. Those
four are this project's own words. They were invented because no recording of
the firmware's own words exists. The retired Python emulator carried free text
instead, such as `Already printing`, `No print to pause` and `No buttons`. Only
one of the two can be right. The strings you read at the bench settle it, and
they are what `internal/printer/simulator/commands.go` has to learn.

To reach the three dialog rows, put a dialog on the display first. Pulling the
filament during a print raises one, and `/dialog/id` on the state tree is where
it appears. The reply names that id and one of the buttons the display offers:

```powershell
curl.exe -s -X POST http://127.0.0.1:8080/api/v1/devices/$id/commands `
    -H "Content-Type: application/json" `
    -d '{\"verb\":\"core.command.dialog_action\",\"args\":{\"dialog_id\":\"1\",\"button\":\"Retry\"}}'
```

`/dialog/code`, `/dialog/title`, `/dialog/text` and `/dialog/buttons` carry what
the display says. They arrive once, with the event that raised the dialog, and
telemetry then repeats the id alone. A reader that missed that event knows a
dialog is waiting and not what it asks, so read the four paths before answering.

### Answers that are not what you asked for

| answer | means |
|---|---|
| `200 {"ok":false,"machine_reason":"..."}` | the printer refused, in its own words. Read `machine_reason` |
| `400 core.printer/VERB_UNKNOWN` | this build has never heard the word. `START_PRINT` is the shipping server's spelling |
| `409 core.printer/CAPABILITY_NOT_SUPPORTED` | the word is real and this printer has no such verb |
| `409 core.printer/SESSION_ENDED` | the printer left. It stays readable and reaches nothing |
| `404 core.printer/NOT_FOUND` | no printer of that id, or none this caller may see |
| `422 core.validation/REQUEST_INVALID` | the body did not parse. In PowerShell that is usually the quoting above |
| a request that never answers | the printer is away and the command is queued. It drains when it comes back |

A `CAPABILITY_NOT_SUPPORTED` problem carries `verbs`, which is this printer's
own list as a JSON array, and `verb_count`. Asking a CORE One for a verb
only the iX takes answered 409 with 45 verbs, `start_print` among them.

The commands are the verbs, and the state tree is the readings. The printer
payload also carries `link`, which counts frames this build could not read.
Anything but zero there means the printer is connected and speaking a dialect
this build does not follow.

## Printer matched no capability profile

Read `profile_matched` on the admission line. It is the first thing to check
after a printer arrives, and it is not what a person expects to be wrong:

```
level=INFO msg="a printer was admitted" printer=... model=CoreOne serial=... capabilities=85 profile_matched=true
```

`false` there means the model the printer reported is spelled differently from
every model the profile file carries. A `WARN` beside it names both sides, so
the diagnosis is on the line:

```
level=WARN msg="no capability profile covers this printer, holding it to what this driver's profiles cover" model=Voron firmware=2.4.0 serial=SN-1 covered="[MK4 MINI]"
```

`covered` is the file's own list. This build ships thirteen models, and the
Core One family is spelled `CoreOne`, `CoreOneL`, `CoreOne_INDX` and
`CoreOneL_INDX`. `GET /api/v1/devices/{device_id}` carries the same field.

A spelling does not have to be one of those thirteen. Case, spaces, dots,
hyphens and underscores are ignored, so `Core One`, `core-one` and `COREONE`
all reach `CoreOne`. The device type code reaches it too, and that is what a
printer sending no `User-Agent-Printer` header is filed under. `7.1.0` is
`CoreOne`, `7.10.0` is `CoreOne_INDX`, `8.1.0` is `CoreOneL` and `8.10.0` is
`CoreOneL_INDX`. A printer matched under a spelling the file does not use says
so on its own line:

```
level=INFO msg="this printer reported a model spelling the profile file does not use" model=7.1.0 profile_model=CoreOne firmware=6.4.0 serial=SN-1
```

An unmatched printer is not refused. It falls to the coverage tier, which is
the union of every model in the file, so it is offered more verbs than its own
machine has. The symptom is a refusal from the printer rather than from this
server: a `200` with `"ok":false` and a `machine_reason`, where a matched
printer would have answered `409 core.printer/CAPABILITY_NOT_SUPPORTED`.

The connector is not broken when this happens. Record the exact `model` string
the printer reported, then add it to
`internal/printer/profile/aliases.json` as one row:

```json
{ "spelling": "what the printer sent", "model": "CoreOne", "source": "the bench, 2026-08-28" }
```

`model` has to be one of the thirteen the file carries, and `source` has to say
where the spelling was seen. A row missing either is refused at startup, and so
is a spelling that already reaches another model.

## What the preflight does not prove

| not proved | why |
|---|---|
| that the printer self-assigns an address when no DHCP server answers | it depends on firmware this machine cannot ask. Static addresses on both ends avoid the question |
| that the firewall would pass a packet | no rule list is a decision engine. The check reads the rules, and only a real connection proves the outcome |
| that a port speaking TLS carries the core | the health probe speaks plain HTTP, so a listener with the connector's `listener.tls` set reports `WARN` |
