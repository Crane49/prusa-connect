# Prusa printer logs

This module keeps the log lines and the metrics your Prusa printers send, on
this server and nowhere else. A printer with Buddy firmware sends them when
its Metrics & Log setting is on. When a printer misbehaves, they say what it
did. A person reads and filters them on the Prusa printer logs page, and the
module adds the newest of them to the diagnostics bundle a support ticket
carries.

The module ships two programs. This directory holds the Go program, which
receives what the printers send and serves the routes listed below. `ui/`
holds the browser bundle, which draws the page and the Logs tab on a
printer's page.

## Links

| what | where |
|---|---|
| what a printer sends, and how to tell printers apart | [What a printer sends](#what-a-printer-sends), below |
| the ports and the data directory | [ADR-20261019-3ns](../../docs/decisions/20261019-3ns-a-module-declares-its-ports-and-gets-a-data-directory.md) |
| the files in the diagnostics bundle | [ADR-20261019-nrq](../../docs/decisions/20261019-nrq-a-module-adds-files-to-the-diagnostics-bundle.md) |

## Commands

| command | does |
|---|---|
| `make -C extensions check EXT=buddy-debug-logs` | test both halves, pack a release, install it into a demo and read back a UDP line |
| `make test` | build, vet and test the Go half, in this folder |
| `pnpm run check` in `ui/` | lint, type-check and test the browser half in Chromium |
| `pnpm run build` in `ui/` | build the bundle into `ui/dist/` |

## What a printer sends

Both streams are UDP datagrams in the syslog format, sent to one host:

| stream | port | what |
|---|---|---|
| log lines | 13514 | one line per datagram, with a level and the firmware's component, cut at 200 bytes |
| metrics | 8514 | many points per datagram, such as temperatures, fan speeds and errors |

The printer names itself by the MAC address of its Ethernet port. The module
matches that MAC to a printer by the network the printer reported when it
connected. A printer on Wi-Fi alone reports no Ethernet MAC, so its datagrams
are matched by the address they came from. A MAC matches a printer even while
it is away from the server, because it may still send its logs. An address
matches only a connected printer, because a printer that is away may have
lost its address to another one. When two printers report one MAC or one
address, the connected one wins, and two of the same kind match neither. A
pair matched once is kept, so it survives a new address. A pair matched by
the address alone is written to disk at the next read of the printers, at
most once a minute. A MAC that matches no printer is listed on the page, and
a person can match it to a printer on the page.

The printer sends no debug lines, and a datagram lost on the network is not
sent again. The sequence number of each line shows a gap where lines were
lost, and a small number after a restart of the printer.

## Anybody on the network can send a line

UDP carries no proof of who sent a datagram, and the printer signs nothing.
Anybody who can reach the two ports can send a line in the name of any MAC,
and the module keeps it like a line from a printer. Treat the lines as a
hint about what a printer did, never as evidence. The page shows the
address each line came from, and the Senders tab says the same.

The module limits what one sender can do:

- a datagram larger than 2 KiB is unreadable. The firmware cuts a log line at
  200 bytes, or 128 bytes on firmware 6.4, and a metrics datagram at 1024
  bytes
- a datagram whose HOSTNAME is not a MAC such as `10:9c:70:2a:bb:cd` is
  unreadable
- a metrics datagram with more than 128 points, or a point with more than 32
  fields, is unreadable
- every control character in a line is written as `\xHH`, so a line cannot
  add a second line to a download or move a terminal's cursor
- the list of senders holds 1024 MACs. A new MAC takes the place of the
  unmatched one seen longest ago. When there is none, it takes the place of
  the MAC matched by its address alone seen longest ago, because a sender can
  forge an address. A MAC matched by its own MAC or by a person stays

Keep the two ports closed to networks you do not trust.

## Who sees which records

A person who may view this module sees the records of the printers they may
see, and no others. For each request the module asks the server which
printers that person may see, as that person, and answers only their
records. This holds for the log lines, the metrics, the senders and the
download.

A MAC matched to no printer could belong to any printer. Its records go only
to a person who may see every printer the module reads. Deleting the kept
records removes those of every printer, so it takes such a person too. While
the module knows no printer, nobody counts as such a person.

The diagnostics bundle is built by an admin, and it carries the records of
every printer.

## Setting a printer up

On the printer: **Settings, Network, Metrics & Log**. Switch on `Enable
Metrics`, write this server's address as `Host`, and leave the ports at 8514
and 13514. The Host takes at most 20 characters, so write the address and not
a long name.

On the page: the setup button sends the printer the same settings as the
G-code `M334 <host> <metrics port> <syslog port>`. A stream that is off is sent
port 0, which switches that stream off, so the log lines and the metrics are
switched on and off apart. The printer then shows `G-CODE METRICS CONFIG
CHANGE` and changes nothing until a person presses Yes on its display. The
module never answers that question for them. Sending raw G-code takes a grant
of its own on the printer, so a person without it sets the printer up on its
display.

## Installing it

The module listens on two UDP ports, so its install asks for two approvals:

```console
$ prusa-connect module add github.com/prusa3d/buddy-debug-logs@0.1.4 -from ./release -approve -approve-listen
```

`-approve` grants the data directory and lets the module read the printers.
`-approve-listen` grants the ports. `-listen syslog=5514,metrics=5515` grants
other ports, and the printer is then set up with the same ones. Without the
port approval the module runs, keeps nothing new, and its status says why.

In Docker, publish both ports from the container, as UDP:

```yaml
ports:
  - "13514:13514/udp"
  - "8514:8514/udp"
```

A container that publishes them under other ports on the host tells the
printer the host's ports, which the setup route takes as `syslog_port` and
`metrics_port`.

## Configuration

| key | type | default | summary |
|---|---|---|---|
| `retention_hours` | int | `24` | How many hours a log line or a metric point is kept. |
| `max_mib` | int | `128` | The disk room both streams may take together, in MiB. The log lines keep a quarter of it. |

A record is removed when it is older than `retention_hours`, or when its
stream is over its room, the oldest first. The log lines keep a quarter of
`max_mib`, because a printer sends far more metric points than lines and the
lines are what a person reads first.

## Where things are kept

| what | where |
|---|---|
| log lines | `syslog/` in the data directory, one JSON record per line |
| metric points | `metrics/` in the data directory, the same way |
| the MACs matched to printers | `sources.json` in the data directory |

A new file starts each hour, or when the current one is full. Its name is a
number that grows with each new file, then the hour, so the files sort in
the order they were written even when the clock moves back.

The data directory is `module-data/buddy-debug-logs` beside the server's modules
directory. An update and a removal of the module keep it.

## The routes

| route | does |
|---|---|
| `GET /status` | the ports, what arrived, the room taken |
| `GET /sources` | every MAC that sent something, and its printer |
| `PUT /sources/{mac}` | match a MAC to a printer, as a person chose |
| `GET /logs` | one page of log lines, newest first, by printer, level, component, time and text |
| `GET /metrics/names` | the metrics a printer sent |
| `GET /metrics/series` | one metric as points for a chart |
| `GET /export` | one page of a download as text |
| `POST /devices/{device_id}/setup` | ask a printer to send, with M334 |
| `POST /data/purge` | remove every kept record, or those older than a number of hours |

`openapi.json` describes each of them.

## The diagnostics bundle

An admin can include the files from extensions in the bundle. This module
then adds `status.json` and `sources.json`. Next come the newest log lines,
one file per printer as `syslog/<printer>.log`. Last come the newest metric
points, as `metrics/<printer>.lp`. A printer is named by its device id. A MAC
matched to no printer is named by an alias such as `unassigned-3f2a9c01b4d7`.
In `sources.json` the alias stands beside the MAC, which the server replaces
in the bundle. Unreadable datagrams go to `unreadable`. A file name never
holds a MAC or an address. The server takes at most 8 MiB from one module, so
a bundle carries the newest lines. The lines in each file are newest first. The
server removes addresses, MACs and secrets from every line before it writes
the file.

## Key files

| path | purpose |
|---|---|
| `main.go` | the handshake, the method set and the wiring |
| `internal/datagram/` | reads the two datagram formats |
| `internal/receiver/` | listens on the granted ports |
| `internal/sources/` | matches a MAC to a printer |
| `internal/store/` | the files in the data directory, and the retention |
| `internal/query/` | the filters, the series and the download |
| `internal/collect/` | the pages of the diagnostics bundle |
| `internal/routes/` | the HTTP handlers and the error codes |
| `client/` | the typed client, generated from `openapi.json` |
| `ui/src/index.ts` | registers the page and the Logs tab on a printer |
| `ui/src/*.svelte.ts` | the stores: status, log lines with the live mode, metrics, setup, download |
| `ui/src/views/` | the page, the printer tab, the setup dialog and the guide |
| `ui/src/locales/` | the texts, in English and Czech |
| `scripts/check.sh` | the end-to-end gate |
