---
title: "ADR-20261112-4ds: the core can serve the time on the local network"
description: "The core carries a small SNTP server on UDP port 123, off by default, switched by core.time_server.enabled and by a switch in the web app that applies at once."
date: 2026-11-12
status: proposed
supersedes: []
amends: []
tags:
  - adr
  - discovery
  - config
  - security
---
# ADR-20261112-4ds: the core can serve the time on the local network

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260916-b3x](20260916-b3x-instances-announce-themselves-on-the-network.md), [ADR-20261019-3ns](20261019-3ns-a-module-declares-its-ports-and-gets-a-data-directory.md), [ADR-20260925-jne](20260925-jne-printers-find-each-other-and-a-person-pairs-them.md)

A device that cannot reach the internet still needs the right time. Some
devices stop working without it and report nothing. Such a device often
asks for the time on UDP port 123 and ignores a port written after the
address. A time server on any other port never hears it. A module may not
listen below port 1024 (ADR-20261019-3ns rule 14), and the core may. So the
core serves the time itself.

SNTP is the simple form of NTP (RFC 4330): one 48-byte request and one
48-byte answer. The server answers with the clock of the computer it runs on.

## rules

### what the core serves

1. `internal/timeserver` answers SNTP requests on UDP port 123, on every
   IPv4 interface. It uses only the standard library. It opens no IPv6
   socket, because on macOS a dual-stack socket opens beside another
   program's IPv4 socket on the same port and then hears no IPv4 request.
2. It answers a request of 48 bytes or more in client mode, and ignores any
   other datagram. The answer is never longer than the request. It answers no
   source that a client never sends from: port 0, a multicast address, the
   broadcast address 255.255.255.255 or 0.0.0.0. A subnet's own broadcast
   address still passes, because refusing it needs each interface's netmask.
3. It answers at stratum 10, the level of a local clock with no source above
   it, with the reference id `LOCL`. A client refuses stratum 0 and 16.
4. Every build carries it. There is no build tag, because a tag takes code out
   of the OpenAPI document, the lint and the tests.

### how it is switched

5. It is off by default. `core.time_server.enabled` in the configuration file
   sets the value a server starts with.
6. The switch on Settings, Server, Network and `PUT /api/v1/time-server`
   store the choice in the machine-owned file and apply it at once, with no
   restart. A stored choice wins over the configuration key, as it does for
   the hidden setting (ADR-20260925-jne rule 55).
7. Reading the switch takes `view_resource` on the link kind, and changing it
   takes `configure_resource` on the same kind, as for the hidden setting.

### when it cannot serve

8. A port that will not open never stops the server from starting. The switch
   stays on, `GET /api/v1/time-server` answers `state: failed` with the
   reason the operating system gave, and the server logs one warning.
   Switching on again tries the port again.
9. A machine-owned file that cannot be read at start leaves
   `core.time_server.enabled` in charge, with one warning in the log.
10. Port 123 is below 1024. On Linux outside a container, opening it needs
    the `CAP_NET_BIND_SERVICE` capability on the process or a lower
    `net.ipv4.ip_unprivileged_port_start`. The shipped systemd unit grants
    the capability as an ambient capability. Inside a container the
    runtime sets the value to 0, as for a printer listener on port 80. The
    port then has to be published. macOS and Windows have no such limit. An NTP server
    already running on the machine, such as `ntpd`, holds the port, and the
    state then says so.

11. No module process gets that capability. At start the core clears its
    ambient capability set, so a child inherits nothing, and it keeps the
    capability in its own effective set. Before it replaces itself with a new
    run, as a restart does, it raises the capability in the ambient set
    again, so the new run can open the port too. The test of this rule skips
    where the process may not raise the capability.

## enforcement

| rule | gate | state |
|---|---|---|
| 2, 3 | `internal/timeserver` tests: the answer, the packets left unanswered, the sources refused | exists |
| 5, 6, 9 | `internal/app` time service tests: the key, a stored choice, a refused write, a file that cannot be read | exists |
| 6 | `internal/app/resource` tests: the switch written and read back, the other members of the file kept | exists |
| 7 | `internal/transport/http/httpapi` time server tests: a viewer reads and is refused a change, an administrator changes it | exists |
| 1, 8 | `internal/timeserver` tests: a port in use reports failed, also when another program holds it on IPv4, and stop clears it | exists |
| 10 | none. No test opens port 123 | planned |
| 11 | `internal/kernel/procgroup` Linux test: a child gets no ambient capability, and the raise before exec reaches it | exists |

## rejected

| option | why not |
|---|---|
| the time server in a module, on a port above 1024 | a device that asks on port 123 whatever port it is told never reaches it |
| the time server in a module, on port 123 | ADR-20261019-3ns rule 14 refuses a module port below 1024 |
| a build tag for it | a tag hides code from the OpenAPI document, the lint and the tests |
| on by default | it opens a port to the whole network, and most networks already have a time server |
| a full NTP server that follows an upstream source | an upstream source needs the internet this network does not have |
