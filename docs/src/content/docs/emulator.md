---
title: Printer simulator
description: Virtual Prusa printers written in Go. They register and speak the same WebSocket protocol as real xBuddy firmware, so you can develop and test with no hardware.
audience: extend
rewrite:
  describes: current
  implementedBy:
    - internal/printer/simulator
    - tools/printersim
    - tools/camerasim
    - cmd/demo
---

The simulator is a Go package in this repository. One simulated printer
registers the way real firmware registers, dials the server over a WebSocket,
reports temperatures, progress and state, and answers commands. Go is its only
requirement, so you can build features and run tests with no physical machine.

## The fastest way to see a fleet

`make demo` builds and starts the server with four printers already dialled
in: three xBuddy printers and one Klipper machine. Ctrl+C stops everything.

The demo keeps every printer printing, with the odd cancelled or failed print,
and starts with 21 days of recorded history. One printer fails about a quarter
of its prints.

```bash
make demo
```

Flags go through `DEMO_ARGS`:

```bash
make demo DEMO_ARGS="-printers 8 -flap 0 -cameras=false"
```

| flag | does |
|---|---|
| `-printers` | how many xBuddy printers dial in. Default 3. The Klipper machine is there as well |
| `-flap` | how often one printer goes away and comes back. `0` keeps the fleet connected. Default one minute |
| `-cameras` | start a simulated camera for each printer. Default on |
| `-print-gap` | the wait between prints, which also makes each print take seconds. Default `0`, the demo's own pace |
| `-transfer-rate` | how many bytes a second each printer fetches a file at. Default `0`, as fast as the server serves it |
| `-listing-delay` | how long each printer takes to list a folder, answering no other command meanwhile. Default `0`, at once |
| `-addr` | the address the core binds and the printers dial. Default `127.0.0.1:8080` |
| `-config` | the configuration file the demo lays its own settings over. Default `config/config.yaml` |
| `-dir` | where the configuration and the database go. Default a temporary directory that is removed |
| `-examples` | where the packed example modules and extensions are. Default `bin/demo-modules`, which `make demo` writes |
| `-log-level` | `debug`, `info`, `warn` or `error`. Default `error` |
| `-tls-cert`, `-tls-key` | a certificate and key as `core.api.tls` takes them. With both, the demo serves HTTPS |

## The two example modules the demo offers

`make demo` builds `examples/spools` and `examples/nozzle-watch` into release
archives under `bin/demo-modules` first, and writes an index beside the modules
directory the run declares. `GET /api/v1/modules/index` then answers one row per
example, and the extensions page installs either one off your own machine. No
network, and no release on anybody's forge.

Building the two bundles needs pnpm. On a machine without it the demo prints one
line, starts anyway, and offers nothing. The archives are rebuilt only when a
source file is newer than them, so a second `make demo` starts at once.

To pack them without starting a server:

```bash
make demo-modules
```

## The extensions the demo offers

`make demo-modules` also runs `scripts/pack-demo-extensions.sh`. It builds every
extension in `extensions/` into a release under `bin/demo-modules/extensions`,
such as Parts, Fleet stats and Prusa printer logs. A release with a Go half is laid out
by `extensions/scripts/pack-release.sh`, the script the extension gates and the
server image use. The demo offers each one under
`github.com/prusa3d/<folder>`, the module path the server image uses.

Parts keeps its catalogue in the record backend. The demo declares one, a
SQLite file `records.db` in its own directory, which goes when the demo stops.
It does not when your configuration file declares a `db` resource, because a
server with two record backends refuses every module state call.

This step is best effort. An extension that does not build prints a `WARN`
line with the end of its build log, and the demo does not offer it. The demo
starts anyway. No CI job runs this step, so a core change never waits for an
extension to build. The extensions have their own gates; see
`extensions/README.md`.

## A fleet against a server you already run

`tools/printersim` connects to a server that is already up, so you can restart
the server and watch the printers reconnect.

```bash
make run
go run ./tools/printersim -url http://127.0.0.1:8080 -model MK4 -count 5
```

A server that has not recorded how a new printer joins asks first, so each
simulated printer waits until you approve its code on the fleet page. To let
them in on arrival, set `mode: none` under `core.connectors` in the server's
configuration, or choose that in the first run.

| flag | does |
|---|---|
| `-url` | the server to connect to. Default `http://localhost:8080` |
| `-model` | the printer model. Default `MK4` |
| `-models` | print every model and exit |
| `-firmware` | the firmware label to report. Default the profile's floor |
| `-serial` | the serial number prefix. Default `SIM` |
| `-count` | how many printers to run. Default 1 |
| `-cameras` | serve a simulated camera for each printer. Default on |
| `-idle` | telemetry cadence while idle. Default 250 ms |
| `-printing` | telemetry cadence while printing. Default 1 s |
| `-print-time` | how long a print takes before it finishes. Default 5 minutes. `0` prints until it is stopped |
| `-duration` | stop after this long. `0` runs until interrupted |
| `-v` | log every session and every refusal |

Ctrl+C stops the fleet. There are no PID files and no log files to clean up:
one process runs the whole fleet.

Every simulated printer starts with a standard 0.4 mm nozzle on each head.
To change it, send `set_tool_nozzle_diameter`, `set_tool_high_flow` or
`set_tool_hardened` from the printer's command panel or through the API. The
printer then reports the new nozzle, as a real printer does after a nozzle
swap. A restart of the fleet puts the standard nozzle back.

## The camera on each printer

Every simulated printer gets a simulated camera. It is a small HTTP server that
draws a picture and answers two paths. They are the same two a real ESP32-CAM
answers: one still image, and a stream of JPEG frames as
`multipart/x-mixed-replace`. That stream is what an `img` tag in a browser
plays.

Each frame carries five things, so you can tell a live view from a frozen one
at a glance:

- the printer's name
- what the printer is doing, such as `PRINTING 42%`
- a bar that fills as the print advances
- a coloured block that slides across the frame, and a background that shifts
  colour every frame
- the time the frame was drawn

`make demo` adds one camera per printer for you and prints the URL of each
live view. It uses the same two URLs an operator gives a real camera, so
nothing about the path is a shortcut:

```bash
curl -H "Authorization: Bearer $TOKEN"   http://127.0.0.1:8080/api/v1/cameras/cam-demo-01/snapshot --output frame.jpg
```

`tools/printersim` holds no operator credential, so it serves the cameras and
prints the two URLs of each rather than adding them. Add one with a `PUT` to
`/api/v1/cameras/<name>`, giving it the printer it watches and those two URLs.

Turn them off with `-cameras=false` on either command.

## What a G-code line comes back with

Send a line to a printer and read the `answer` field:

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json'   http://127.0.0.1:8080/api/v1/devices/$DEVICE/gcode -d '{"lines":["M105"]}'
```

| line | answer |
|---|---|
| `M105` | the nozzle and bed temperatures with their targets, such as `ok T:215.0 /215.0 B:60.0 /60.0` |
| `M114` | where the head is, with the stepper counts, then `ok` on the next line |
| anything else | `ok` |

Raw G-code takes more than an operator's permissions. It takes a grant naming
the verb, or ownership of the machine, so a caller who can command a printer in
every other way is still refused here.

Real Buddy firmware answers a G-code line with an acknowledgement and no text
at all. The reply above is the simulator being more generous than the machine,
because a person debugging at a bench needs a reading to look at. On a real
printer the `answer` field is absent.

## Which models it can be

Run `go run ./tools/printersim -models` to print the list. The simulator holds
no model of its own. Every model, firmware floor and capability comes from the
capability profile data the server itself reads. A model added there is a model
the simulator can be on the next build.

## What it does on the wire

- the registration handshake: `POST /p/register`, then a poll for the token,
  then the WebSocket
- nine-byte framing, JSON frames and G-code frames, and the event replies each
  one takes
- telemetry, faster while printing than while idle
- job state, including the refusals real firmware makes when a command arrives
  in the wrong state
- `BUSY` while homing and while running a bed mesh
- file transfers: block requests, a transfer resumed across a session, and the
  short-name collision a FAT filesystem produces
- storage devices with a free-space figure, so a write can be refused for the
  reason a real one is

Two things it does not copy, on purpose:

- reconnect backoff is a fixed 100 ms rather than the firmware's 0.1 s to 60 s,
  so a test that restarts the server does not wait a minute
- there is no client-side ping, because nothing in the server depends on one

It is also more generous than a printer in one place. A G-code line comes back
with the reply a serial port would print. Real firmware sends no such reply.
The section below says what each line answers with.

## Faults you can ask for

A test drives these as method calls on a `Printer`. None of them is a
probability and none is timed off a clock the test does not own, so a run is
the same every time.

| method | what the server sees |
|---|---|
| `GoOffline`, `GoOnline` | the printer drops and refuses to redial, then comes back and drains what the host queued |
| `SetResponseDelay` | slow answers, so a timeout and a breaker are reachable without waiting for a real one |
| `RemoveStorage` | a USB stick pulled out mid-job. Paths on it are refused and `INFO` stops listing it |
| `InsertStorage` | a device appears |
| `SetFreeSpace` | a full disk |
| `RaiseDialog` | the printer asks a question and waits for an answer |

## Where the gates drive it

The lane that gates a pull request runs the same package over `net.Pipe`
inside a `testing/synctest` bubble, because a real socket leaves virtual time.
`tools/printersim` and `make demo` are the manual lanes over a real socket.

`cmd/fleetload` drives the same package as a load harness. When you need the
firmware itself rather than a model of it, `make firmware-up` boots Prusa's own
MK4 firmware under an emulated board against a running server.
