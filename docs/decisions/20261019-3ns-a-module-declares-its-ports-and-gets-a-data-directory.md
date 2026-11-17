---
title: "ADR-20261019-3ns: a module declares its ports and gets a data directory"
description: "The needs block: a module lists the ports it listens on and asks for a writable directory; the operator approves the ports separately from the other requests, may move a port, and the host hands the module what was granted."
date: 2026-10-19
status: accepted
tags:
  - adr
  - modules
  - security
---
# ADR-20261019-3ns: a module declares its ports and gets a data directory

- status: accepted
- date: 2026-10-19

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on: [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md) rule 3, [ADR-20261007-br4](20261007-br4-an-operator-grants-a-module-room-for-its-records.md)

Some modules receive a protocol of their own from the network, such as
printer syslog over UDP. Some keep more data than the record store fits.
A module process can open a socket and write files anyway, because nothing
limits it. This record makes both a request the operator reads and approves.
The host then tells the module what it was granted. A network port is the
more dangerous request, so it has an approval of its own.

## rules

### what the manifest asks

1. A module asks in the `needs` block of `module.yaml`. The block has two
   keys, `listen` and `data`. Any other key is refused.
2. `listen` is a list. Each entry has a `name`, exactly one of `udp` or
   `tcp` with a port from 1 to 65535, and a `why` a person reads. A name is
   lower case letters, digits and hyphens, and is unique in the list. Two
   entries may not name the same network and port.
3. `data: true` asks for one writable directory. Absent or `false` asks for
   none.
4. A module that uses `needs` raises its `core` range to the first release
   that reads it, because an older server refuses the unknown key.

### what the operator approves

5. `data` is approved with `approve`, together with permissions, events and
   records.
6. Ports are approved with `approve_listen` over HTTP and `-approve-listen`
   on `module add`. Each needs `approve` too. An install with `approve` and
   without `approve_listen` grants everything but the ports, and lists each
   port as a request still open: `listen udp 13514 as syslog (printer log lines)`.
7. The operator may move a port with `listen` beside `approve_listen`, a map
   of name to port, or `-listen name=port,name=port`. A name the manifest
   does not list is refused, and so is a name given twice. The network stays
   the one the manifest named.
   The web app's install dialog shows a number field per port once the
   ports are ticked. The Ports tab of an installed extension changes them
   later by installing the same release again, and `module.listening` on the
   resources listing answers the ports an instance holds now.
   - **Amended by [ADR-20261019-h76](20261019-h76-an-operator-may-switch-installed-modules-off.md) rules 5 and 15.** While
     `core.modules_enabled` is `false` that install is refused and the Ports
     tab is locked.
8. The declaration records the grant as `service.listen`, a map of name to
   `udp/<port>` or `tcp/<port>`, and `service.data: true`. The operator may
   edit either in the declaration. The module reads the new values at its
   next start.
9. A port the manifest asks for and the operator was not shown refuses an
   approved install with `MODULE_REQUESTS_CHANGED`, as any other request
   does (ADR-20261005-64j rule 10). A marketplace listing names the ports in its
   `listen` field and the directory in `data`.
10. An update keeps a port grant whose name, network and `why` are the ones
    the operator approved last, with the port the operator chose. A new name,
    a new network or a new `why` is a new request. It needs `approve_listen`
    again, and the install lists it as new. A port the operator grants for
    the first time is listed as new too, even when the release before already
    asked for it.

### what the host gives the module

11. The host sends the grant in `plugin.configure`, in the `grants` object
    of `wirerpc.Settings`. `grants.listen` maps each granted name to its
    network and port. `grants.data` is the absolute path of the directory.
    A module reads them with `wirerpc.ConfigureGranted`.
12. The data directory is `module-data/<instance>` beside the modules
    directory. The host creates it with mode `0750` before it starts the
    module. At every start it takes the group's write and everybody else's
    access away from a folder that has them, unless the server may not
    change that folder. A folder or an `.owner` file that is a link is
    refused. An update
    and a removal leave it in place (ADR-20260827-rwq rule 7). The host writes the
    module's type, its namespace and manifest name, to a `.owner` file in the
    directory, and the module leaves that file alone. A directory whose
    `.owner` names another module is refused with
    `RESOURCE_MODULE_DATA_OWNED`, and the module does not start. The
    operator removes the folder or moves it away. So a module installed later
    under the same instance name never reads data another module left
    behind. A directory with no `.owner` is taken by the module that starts
    on it.
13. A module listens on the ports it was granted and on no other. A module
    that was granted no port runs without the listener and says so in its
    own status.

### which ports an install refuses

14. An install checks every port it would grant before it stops the running
    module or writes a file. It refuses with a usage error, which HTTP
    answers as `REQUEST_INVALID`:
    - two names on one network and port
    - a port below 1024. Only the system administrator may open such a
      port, and the server does not run as one. The operator moves the port
      with `listen`
    - a port another instance's declaration grants
    - a TCP port the server itself listens on, from `core.api.addr` and
      `core.observability.addr`
15. The ports of the MQTT broker and of a printer listener are settings of
    their resources, and an install does not read them. Such a collision
    shows when the module starts and cannot open its port.

## enforcement

| rule | gate | state |
|---|---|---|
| 1, 2, 3 | `sdk/manifest` needs tests: unknown key, bad port, repeated name, both networks | exists |
| 5, 6, 7 | `internal/app` module listen tests, over the command and over HTTP | exists |
| 8 | `internal/app/config/install` listen tests, a grant and a refusal at load | exists |
| 9 | `internal/app` module listen tests, a listing without the port | exists |
| 10 | `internal/app` module listen tests, an update that adds a port, a changed `why`, a port first granted on an update | exists |
| 11 | `sdk/wirerpc` settings tests, grants over the wire | exists |
| 12 | `internal/app/resourcehost` data directory tests: made, refused to another module, mode set on a folder that exists | exists |
| 13 | none. The host does not stop a module from opening another port | planned |
| 14 | `internal/app` module listen tests: two names on one port over the command and over HTTP, a port below 1024, a port in use | exists |
| 15 | none. A collision with a resource's port shows when the module starts | planned |

## rejected

| option | why not |
|---|---|
| ports as ordinary options | the operator would approve permissions and never see that a module opens the network |
| one approval for everything | a port is reachable from the network, so it is the riskier request and is asked on its own |
| the host opens the socket and passes it to the module | Go cannot pass a socket to a child on Windows, which is a production platform |
| the data path in an environment variable | the settings already carry everything the host tells a module, and one channel is simpler to test |
| the data directory under the module's release folder | an update and a removal delete that folder |

## open items

- the host does not enforce rule 13. Landlock can limit TCP ports on Linux 6.7
  and later, and has no rule for UDP
- an install does not read the ports of the MQTT broker or of a printer
  listener (rule 15)
- an extension installed from a local folder, which no index offers, has no
  release the web app can install again. Its ports change through the command
  or the declaration
