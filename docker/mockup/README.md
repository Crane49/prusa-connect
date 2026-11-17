# The Fractal mockup

Four real Prusa Connects on one machine, joined into one Fractal tree.
Nothing here is a drawing or a stub: every server is this repository's own
binary with the Fractal module installed, and the traffic between them is the
protocol's own.

The picture is the school from the module's README. A school has a printer room
on each floor, each room has its own server, and the school has one more server
above them.

| server | what it is |
| --- | --- |
| `hq` | the top of the tree. It accepts servers under it and asks a person before it admits one |
| `brno` | under `hq`. It accepts servers under it, admits them with no person, and keeps a copy of the state of everything below it |
| `workshop` | under `brno`, with two fake printers |
| `garage` | under `hq`, with two fake printers |

A message from `workshop` to `hq` crosses `brno`, so the mockup shows a server
in the middle and not one hop.

## Start the servers

```
make mockup-up
```

It builds one image and starts four containers from it. The image carries the
demo binary with the web app inside it, the shipping binary and the Fractal
module laid out as a release.
Each container installs that release at its start with the shipping binary's
own command:

```
prusa-connect module add github.com/prusa3d/fractal-link@1.0.0 \
    -config /etc/prusa-connect/core.yaml -from /release -approve -approve-listen \
    -settings /etc/prusa-connect/<server>.json
```

The settings of each server are the file named by `-settings`, and they are in
`config/` beside this file. Each one has a `files` block, so the servers share
folders with the servers below them over plain HTTP on the compose network.
`-approve-listen` grants the port the module serves those files on. The install writes one declaration with those
settings into the machine-owned overlay on the container's volume.

## Open a server

Each server's web interface is on the loopback address:

| server | address |
| --- | --- |
| `hq` | http://127.0.0.1:18080 |
| `brno` | http://127.0.0.1:18081 |
| `workshop` | http://127.0.0.1:18082 |
| `garage` | http://127.0.0.1:18083 |

The demo makes a new password for the `demo` account on every run and prints it
once in the container's log:

```
docker compose -f docker-compose.local.yml logs hq | grep password
```

Every port reads from the root `.env`: `PCL_MOCKUP_HQ_PORT`,
`PCL_MOCKUP_BRNO_PORT`, `PCL_MOCKUP_WORKSHOP_PORT` and
`PCL_MOCKUP_GARAGE_PORT`.

The two NATS ports of a server that accepts others are not published. A server
below reaches the one above it by service name on the compose network, which is
what the `advertise` setting of each server names.

## Click through Storage

```
make mockup-seed
```

The seed makes the mockup ready for a person. `hq` admits `brno` and `garage`,
`hq` shares `projects/fixtures` and `workshop` shares `jigs`, each with one
G-code file, and `brno` gets `orders/2026` for the Shared switch. It prints the
address and the password of every server.

Open Storage on `brno` to see both sides: its own folders with the Shared
switch, and the folders `hq` above it and `workshop` below it share.

The seed admits the servers that `make mockup-check` expects to find pending.
Run the check on a fresh mockup, and the seed after it.

## What the check proves

```
make mockup-check
```

It drives the four servers over their own HTTP APIs and fails with a plain
sentence when a step does not happen in time. It reads nothing out of a log but
the demo password, and it runs no command inside a container.

1. all four servers answer and the demo account signs in
2. `hq` holds `brno` and `garage` waiting for a person, each with its public key
3. approving both through `POST /api/v1/links/fractal/nodes/{id}/approve`
   connects both
4. `brno` admitted `workshop` with no person, because `ask_first` is false there
5. `workshop`'s place in the tree is `hq/brno/workshop`
6. the links body of `hq` has its place in the tree, both listeners and the
   counters
7. the install on every server granted its link the printers of that server,
   and a printer of `workshop` then appears at `hq` with its state and its
   nozzle temperature
8. a command sent from `hq` is refused while `node:brno` holds no grant on that
   printer, and is no longer refused once `workshop` writes one
9. stopping `brno` makes `hq` report it as not alive, and starting `brno` again
   brings it back
10. revoking `garage` at `hq` ends its connection, and a restart does not get it
    back in

A whole run:

```
The Fractal mockup, driven through the API of each server.

OK   all four servers answer and the demo account signs in
OK   hq holds brno and garage waiting for a person, with the public key of each
OK   hq approved brno and garage through POST /api/v1/links/fractal/nodes/{id}/approve
OK   brno and garage are admitted and their connections to hq are open
OK   brno admitted workshop with no person, because ask_first is false there
OK   workshop's place in the tree is hq/brno/workshop, so a message to hq crosses brno
OK   the links body of hq has its place in the tree, both listeners and the counters
OK   the install on brno, garage and workshop granted each link the printers of its own server
OK   hq lists workshop under GET <mount>/nodes, the route that shows the servers below
OK   hq reads a printer of workshop: its interfaces, its state and the temperature of its nozzle
OK   a command sent from hq is refused, because node:brno holds no grant on that printer of workshop
OK   the same command is no longer refused for want of a grant, once workshop grants node:brno the operator role
OK   hq reports brno as not alive after brno stopped
OK   hq reports brno as alive again after brno started
OK   revoking garage at hq ended its connection
OK   garage did not get back in after a restart, because hq revoked it

OK   16 claims held
```

## Look at a printer of workshop from hq

The module serves its own routes under the mount the host gave it,
`/api/v1/modules/com.github.aux.prusa3d.fractal-link`:

```
GET  <mount>/nodes
GET  <mount>/nodes/{path}
POST <mount>/nodes/{path}/interfaces/{interface}/{instance}/methods/{method}
```

The first two read the copy `hq` keeps of the state below it. The third sends a
command down the tree.

## The two grants each server holds

A module reads the printers of its own server as `sink:fractal`. That name
holds nothing on its own, so without a grant the module sends no printer upward
and `hq` shows an empty list. The install writes that grant: `-approve` is the
operator saying yes to the permissions the manifest asks for, and the module is
granted what those permissions name. One grant on the organization covers the
whole fleet of that server:

```
org:local#viewer@sink:fractal
```

`make mockup-check` reads it back over `GET /api/v1/grants/org/local` and fails
when a server does not hold it.

The viewer role on the organization carries `observe` on every printer, and
`view_resource` and `read_storage` on every resource kind. So it lets the module
list the configured resources of its server and read the files in its storages
as well. The install prints that on the line it writes.

A command that reaches `workshop` runs as `node:brno`, the server directly
above it, whatever server the person was sitting at. Nobody installed that
server here, so an operator writes its grant, per printer:

```
POST /api/v1/grants/device/{id}
{"relation": "operator", "subject": "node:brno"}
```

The check sends the same command before that grant and after it, so the run
shows the refusal and then its end.

## Stop the servers

```
make mockup-down
```

It removes the four containers, the four volumes and the network, and keeps the
image.
