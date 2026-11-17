# Fractal module

This module connects your server to a Prusa Fractal network. A Fractal network
is a tree of servers. Each server joins one server above it, and accepts other
servers and printers under it. The same program runs at every level.

An example: a school has a printer room on each floor, and each room has its own
server. The school has one more server, and both room servers join it. A student
opens the school's server and sees every printer in the building, with the ones
that are free. The student picks a printer on the second floor and presses
start. The school's server sends the command down to the room's server. The
room's server checks its own permissions before it starts the print. Each room
decides which printers it shows to the school's server. Prusa's cloud can be one
level higher, above many schools.

The module does two jobs on your server:

- it shows your printers to the server above, as Fractal interfaces that server
  can read
- it receives commands from the server above and sends each one to your server,
  which checks its own permissions before a printer receives the command

## Installation

Write the settings in a JSON file and give that file to the install:

```
prusa-connect module add github.com/prusa3d/fractal@1.0.0 -from <release> -settings settings.json
```

The first word is the module path and the version of the release, joined by
`@`. `-from` names the release, as a directory or a `.tar.gz` file. The
command refuses any other form. Add `-config <file>` when your configuration
file is not `config/config.yaml`.

Your server makes the namespace of the module from the module path, so
`github.com/prusa3d/fractal` gives
`com.github.aux.prusa3d.fractal`. The routes of the module are under
`/api/v1/modules/<namespace>`, here
`/api/v1/modules/com.github.aux.prusa3d.fractal`.

The command reads `module.yaml` from the release. It writes one declaration
under `resources.link.fractal`, with your settings inside it, into the record
your server owns.

Do not write a `config:` block for this module in your own configuration file.
Written before the install, it makes the install refuse the release. Written
after it, it hides the installed declaration and your server starts nothing.
`-settings` is the one way in.

### Let the module read your printers

The module reads your printers as `sink:fractal`, and that name holds nothing
until somebody gives it something. Until it does, the module sends no printer to
the server above and the server above shows an empty list.

Add `-approve` to the install and your server writes that grant for you:

```
prusa-connect module add github.com/prusa3d/fractal@1.0.0 -from <release> -settings settings.json -approve
```

`-approve` is you saying yes to what `module.yaml` asks for. The install
prints every grant it wrote. This module asks for `observe`, `operate` and
`read_storage`. The viewer role on the organization carries `observe` and
`read_storage`, so the install writes one grant:

```
org:local#viewer@sink:fractal
```

That one grant covers every printer of your server, including one you admit
tomorrow. It also lets the module list your configured resources and read the
files in your storages. `operate` needs a grant on each printer, which the
install does not write (see below).

A server without a `files` block reads no file in your storages. A server with
one uses three things the install asks for:

- `read_storage`, to list and read the folders a person shares, and to print a
  file from them. It reads nothing outside a shared folder and the path to it.
- the data directory, which `-approve` grants. The module keeps the list of
  shared folders there, the digests it computed, and the copies of files from
  the servers above.
- the TCP port `files` (8443 by default), where the servers below fetch the
  shared files. It needs `-approve-listen` as well. Add `-listen files=<port>`
  to grant another port, for example when two servers run on one machine.

The install of a server with a `files` block is then:

```
prusa-connect module add github.com/prusa3d/fractal@1.0.0 -from <release> \
  -settings settings.json -approve -approve-listen
```

Take the grant away and write it on a single printer instead when you want to
show that one printer and no other. A grant on a printer reaches that printer
and no resource of your server:

```
DELETE /api/v1/grants/org/local?relation=viewer&subject=sink:fractal

POST /api/v1/grants/device/{id}
{"relation": "viewer", "subject": "sink:fractal"}
```

`prusa-connect module remove` takes the grants of the install back with
the module.

### Let the server above send commands

A command from the network runs as `node:<name of the server directly above
this one>`. On a server under `brno` that name is `node:brno`. The server the
person was sitting at does not change it. That name holds nothing either, so
every command is refused until you write a grant:

```
POST /api/v1/grants/device/{id}
{"relation": "operator", "subject": "node:brno"}
```

Write it per printer, so the servers above reach the printers you chose. The
setting `command_principal` decides which name your server checks, and
Permissions for commands from the server above has the whole of it.

A print of a file from the network also reads the file from a storage, so on
the server with the printer the same name needs `read_storage` as well. The
viewer role on the storage kind carries it:

```
POST /api/v1/grants/resource_kind/storage
{"relation": "viewer", "subject": "node:brno"}
```

Without it, a `PrintFile` with a `fractal` URL is refused with `0x2011`, "this
server did not allow that call". The answer does not say which permission is
missing.

The install does not write this one. `module.yaml` asks for `operate` as well as
`observe`, and the install says so and leaves it. A module holds the viewer role
on the organization and no role above it, so nothing can hand a module the whole
fleet to drive. You write `operate` on each printer you choose.

## Configuration

`module.yaml` declares every key the module reads. Your server refuses a key
that `module.yaml` does not declare, before it starts the module, and the error
message names the key.

You must write `node_id` and `data_dir`. The module does not start without
them, and the error message names the missing key. Every other key has a
default, and the table below lists it.

Inside a nested block such as `accept`, you write only the keys you want to
change. A list replaces the default list whole.

### Join a server above

```json
{
  "node_id": "floor-2",
  "data_dir": "/var/lib/prusa-connect/fractal",
  "join": {
    "address": "nats://school.example:4222"
  }
}
```

`join.address` is the address of the server above. Write it as a host and a
port, `school.example:4222`, or with the scheme in front,
`nats://school.example:4222`. Both reach the same server.

`node_id` is the name of this server in the network. Two servers that join the
same server cannot have the same name.

`data_dir` is the directory where the module stores its key, the servers it
admitted, and commands that wait for delivery. The module creates it with
permission `0700`.

### Accept servers below

```json
{
  "node_id": "school",
  "data_dir": "/var/lib/prusa-connect/fractal",
  "accept": {
    "advertise": "tls://school.example:7422",
    "client_listen": "0.0.0.0:4222",
    "leaf_listen": "0.0.0.0:7422",
    "ask_first": true
  },
  "shadow": {
    "patterns": ["*"]
  }
}
```

`advertise` is the address this server gives to the servers that join it. They
connect to that address, and connect to it again after the connection drops, so
it must be one they can reach from their network. Write it with the scheme in
front, `tls://school.example:7422`. The module refuses a value with no scheme,
such as `school.example:7422`.

`shadow.patterns` selects the interfaces of the servers below that this server
keeps a copy of. A `*` matches any characters, so `["*"]` keeps a copy of every
interface. When you leave `shadow` out, this server keeps no copy.

A server in the middle of a tree writes both `join` and `accept`.

### All settings

This table repeats the `options:` list of `module.yaml`. A test in this package
fails when a row differs from `module.yaml`.

| Key | Type | Default | What it does |
| --- | --- | --- | --- |
| `node_id` | string | required | The name of this server in the Fractal network, for example `brno`. |
| `data_dir` | string | required | The directory where the module stores its key, the servers it admitted and the commands that wait. |
| `identity` | secret | none | The private key of this server, as NKEY seed text. By default the module creates one in `data_dir`. |
| `join` | object | none | The server above this one. A server at the top of the network does not set it. |
| `accept` | object | none | Lets other servers and printers join this server. A server that accepts nobody does not set it. |
| `tls` | object | none | The TLS certificate of this server and the authorities it trusts. Without it, nothing is encrypted. |
| `files` | object | none | Turns on file sharing in the Fractal network. Its base_url is the files address that every grant carries. |
| `shadow` | object | `{}` | Selects which state of the servers and printers below is copied to this server. The default copies nothing. |
| `command_principal` | string | `node` | Whose permissions this server checks for a command from the server above: `node` or `self`. |
| `dedup_window` | duration | `5m` | How long the module remembers the id of a command, so a command that arrives twice runs once. |
| `credential_lifetime` | duration | `24h` | How long the credentials stay valid that this server issues to a server below it. |
| `heartbeat_interval` | duration | `5s` | How often this server sends a heartbeat to the server above. A heartbeat shows the sender is running. |
| `max_heartbeat_interval` | duration | `10s` | The longest heartbeat interval this server allows a server below it to use. |
| `liveness_multiple` | int | `3` | How many heartbeats in a row a server below may miss before this server marks it as disconnected. |
| `ack_wait` | duration | `30s` | How long the module waits for the receiver to confirm a command before it sends the command again. |
| `stream_max_age` | duration | `168h` | How long a command waits for a disconnected server before the module deletes the command. |
| `stream_max_bytes` | int | `268435456` | The most disk space, in bytes, for the waiting commands of one server. When it is full, a new command fails. |
| `max_payload` | int | `8388608` | The largest message the module sends or accepts, in bytes. The default is 8 MiB. |
| `poll_interval` | duration | `1s` | How often the module checks whether each server below is still connected. |
| `flush_interval` | duration | `500ms` | How often the module sends changed printer state to the server above. It sends the changes together. |
| `resync_interval` | duration | `30s` | How often the module lists all printers again, to find a change it received no event for. |

A duration is a number with a unit, such as `500ms`, `30s`, `5m` or `24h`.

A server with no server above it uses its `node_id` as its whole path in the
tree. `identity` is a seed in the NKEY text format of NATS. When you leave it
out, the node writes a new seed into the data directory at the first start and
reads it from there afterwards. The smallest `liveness_multiple` is 2, and the
interval it counts is `max_heartbeat_interval`.

### Settings inside `join`, `accept`, `tls`, `files` and `shadow`

| Key | What it is |
| --- | --- |
| `join.address` | The address of the server above, where this node sends its request to join. It is the `client_listen` port of that server. |
| `join.credential` | The token this node sends with its request to join. Only a server above that checks tokens needs it. |
| `accept.advertise` | The address this server gives to the servers that join it, such as `tls://school.example:7422`. Required under `accept:`. |
| `accept.client_listen` | The address and port where this server receives requests to join. Leave it out to use `127.0.0.1:4222`. |
| `accept.leaf_listen` | Where the servers below keep their connections open. Leave it out to use the port of `client_listen` plus 1000. |
| `accept.verification` | `tofu`, the default, trusts the key a server presents the first time. `jwt` checks a signed token. |
| `accept.jwks_url` | The address of the public keys that check a token under `jwt`. It is required under `jwt`. |
| `accept.ask_first` | When true, a server this one has not seen before waits until a person approves it. |
| `accept.max_pending_nodes` | The most servers that may wait for approval at the same time. Leave it out to use 256. |
| `accept.pending_expires` | How long a server that waits for approval stays in the list. Leave it out to use 24h. |
| `accept.max_joins_in_flight` | The most requests to join this server checks at the same time. Leave it out to use 8. |
| `accept.joins_per_second` | The most requests to join this server accepts per second. Leave it out to use 4. |
| `tls.cert_file`, `tls.key_file` | The certificate this node presents and its private key. |
| `tls.ca_files` | The certificate authorities this node uses to check the server above. |
| `files.base_url` | The https address where the servers below fetch files from this server. Required under `files:`. |
| `files.allow_cleartext_parent` | When true, this server takes a server above whose files address is `http`. Both ends then decide that TLS is off. |
| `files.rescan` | How often this server scans its shared folders again. Leave it out to use 60s. |
| `files.relay_timeout` | How long a request to the server above may take before this server answers 502. Leave it out to use 30s. |
| `files.proof_tolerance` | How far the time in a proof may be from the clock of this server. Leave it out to use 60s. |
| `files.max_resumes` | How often this server resumes one cut transfer. Leave it out to use 5. |
| `files.fetch_attempts` | How often this server fetches a file for one print. Leave it out to use 3. |
| `files.start_deadline` | How long a print may wait for its file before this server drops it. Leave it out to use 10m. |
| `files.cache_max_bytes` | The most bytes the copies of files from above take. Leave it out to use 10 GiB. |
| `shadow.patterns` | Interface names to keep a copy of, on every node below. A `*` matches any characters. |
| `shadow.interfaces` | Single interfaces to keep a copy of. Each entry names a `node`, an `interface` and an `instance`. |

`files.relay_timeout` also bounds a pause in the middle of a file, from the
server above or from a server below. An idle connection to or from this server
stays open for that long too.

Under `accept.verification: jwt` a server that asks to join must send a signed
token in `join.credential`. This server fetches the public keys from
`accept.jwks_url` and checks the signature, the expiry time and the Node ID the
token names. Write `accept.jwks_url` as well, or the module refuses the
settings at the start.

A server with a `files` block needs two grants. The operator approves the
port `files` with `approve_listen` and the data directory with `approve`.
Without either, the module refuses to start and names the one missing.
`files.base_url` must be on the port granted as `files`. An `https` address
also needs `tls.cert_file` and `tls.key_file`, and the module serves the
files with that certificate.

The module reads the shared folders and prints a file with the permission
`read_storage`. The identity named by `command_principal` needs it on this
server too, as it needs `operate` on the printer.

## Network ports

A server that accepts others opens two NATS ports. `client_listen` receives the
requests to join. `leaf_listen` is where the servers below keep their
connections open. The defaults are `127.0.0.1:4222` and `127.0.0.1:5222`, which
only programs on the same machine can reach. Write addresses such as
`0.0.0.0:4222` and `0.0.0.0:7422`, so servers on the network can connect.

A server with no `accept:` block opens neither NATS port. Nothing outside the module
connects to it: it sends its request to join to the server above, and it keeps
that connection open from its own side. It binds one port on `127.0.0.1`, which
the operating system picks and only this module uses. So you can run two such
servers on one machine, and neither takes a port you need.

A server with a `files:` block also opens the port `files`, whether or not it
accepts others (see [Installation](#installation)).

### Servers in containers

On one Docker network the servers reach each other by service name, and neither
NATS port is published on the machine. A server below writes the service name
of the server above in `join.address`, and the server above writes its own
service name in `accept.advertise`:

```json
{"join": {"address": "nats://hq:4222"}}
```

```json
{"accept": {"advertise": "nats://hq:7422", "client_listen": "0.0.0.0:4222", "leaf_listen": "0.0.0.0:7422"}}
```

Publish ports on the machine only when a server outside that network joins.
Such a server needs both NATS ports: `client_listen` for its request to join,
and `leaf_listen` for the connection it keeps open. `advertise` is then the
address of the published `leaf_listen` port as those servers see it, which is
the address of the machine and not of the container. `module.yaml` declares the
port `files` and not the NATS ports, so you publish both NATS ports in the
compose file yourself.

## Read the servers below over HTTP

This module serves its routes under the mount your server gave it,
`/api/v1/modules/<namespace>`. Your server is the only door to them: it decides
who is calling, and takes `view_resource` on this link before a GET and
`operate_resource` before the POST. A person who may not operate this link
sends no command.

`GET /nodes` lists every server and printer below this one, with its place in
the tree and whether the values it reported are current.

`GET /nodes/{path}` lists the interfaces of one of them and of the servers
below it, each with its instance, its status, when it was last updated and the
property values. `limit` says how many interfaces to answer, 100 by default and
500 at most. The answer is never larger than one mebibyte, and it says
`truncated` when it left entries out.

`POST /nodes/{path}/interfaces/{interface}/{instance}/methods/{method}` sends
one command down the tree. The body is the parameters as a JSON array, in the
order the method declares them, and no body is a call with no parameters. The
answer carries the result, or the error with the code of the Fractal
specification. The server that receives the command checks its own permissions
again, for `node:<name of the server directly above it>`.

Both GETs read the copy this server keeps of the state below it, which
`shadow.patterns` selects. A server that keeps no copy answers an empty list.

## Share a folder with the servers below

A server with a `files` block shares folders of its storages with every server
below it. A shared folder is shared with everything in it. The root folder of
a storage cannot be shared, and a folder inside a shared one may be shared too.

`PUT /files/shares/{storage}/{path}` shares the folder `path` of the storage
`storage`, and `DELETE` on the same address stops sharing it. Both take
`operate_resource` on this link. `GET /files/shares` lists the shared folders
and takes `view_resource`. The module keeps the list in its data directory.
A `PUT` also takes `read_storage` on the storage, checked for the person who
sends it, and is refused with `SHARE_DENIED` without it. A `PUT` for a storage
this server does not declare, or for a path that is not a folder, is refused
with `SHARE_INVALID`.

A server below that already holds a copy of a file keeps it after you stop
sharing the folder. A server without a `files` block answers these routes with
`FILES_OFF`.

## Browse the shared folders of the servers above and below

A server with a `files` block reads the folders the servers in its line share:
every server above it and every server below it. It reads no folder of a server
in another branch of the tree.

`GET /files/stores` lists the content stores of those servers, one for each
storage that shares a folder or did share one. A storage keeps its content
store after you stop sharing its last folder, with an empty listing at its
root. Each entry names the server, the storage as its
instance, and `rootHash`, the identity of the listing at the root of that
storage. `limit` and `after` work as on `GET /nodes/{path}`.

`GET /files/stores/{node}/{instance}/{identity}` reads one listing: the name,
the kind, the length and the identity of each file and folder in it. Start
with the `rootHash` and read a folder by the identity of its entry. A listing
of a server above comes over HTTPS through the server directly above, and one
of a server below comes from the `List` method of its content store.

Both take `view_resource` on this link. A node outside the line is refused
with `STORE_OUTSIDE_LINE`, a listing the server does not publish with
`LISTING_NOT_FOUND`, and a listing this server could not read now with
`LISTING_UNAVAILABLE`. A server without a `files` block answers these routes
with `FILES_OFF`.

## Approve, reject or revoke a server

With `ask_first: true`, a server that asks to join waits for you. Open **Links**
in the web interface. Find the row marked pending under this server and press
approve.

Reject removes a row that waits. The name is then free, so another machine can
ask for it. Use it for a name you do not want to admit.

Revoke removes the approval of a server that already joined. Its open
connection closes at once, and its next request to join is refused. The row
stays in the list, marked revoked. This server deletes its copy of the state of
the revoked server, and of every server under it, and the commands that wait
for the revoked server.

The server below learns of the revocation only at that next join. It joins
when it renews its credentials, when it starts, and when this server answers
its request for a file with `401`. Until then it keeps its copies of files from
above. Then it drops them, logs that, and stops dialing this server. It keeps
serving the servers below it.

The server below writes the revocation to `refusal.json` in its data directory,
and it never asks this server again, also after a restart. It does the same for
a rejection, a name collision and a refused join token, and for a grant it
refused because another server above sent it. To let it ask again, stop its
module, delete that file and start the module. A refused join token
also ends when the module starts with another `join.credential`.

Revoke blocks that name on this server. The same machine can ask to join again
under another name.

The waiting list holds `accept.max_pending_nodes` rows, 256 by default, and
this server accepts `accept.joins_per_second` requests each second, 4 by
default. Any machine that reaches the join port can put a name in that list, so
a row nobody decided about is removed after `accept.pending_expires`, 24 hours
by default. Without that a full list would refuse the server you are waiting
for.

## Links page

- the name of this server in the network, and its path in the tree
- the server above: its address, and whether the connection is open, lost or
  refused. For a refused connection the page shows the code and the message. It
  also shows when the connection last opened
- every server below: its name, whether it is pending, admitted or revoked,
  whether its connection is open, and when its last heartbeat arrived
- every port the NATS server of this module opened. The port `files` is not
  in this list

The page shows no round-trip time. A Fractal heartbeat goes one way, from a
server to the server above it, so there is no round trip to measure.

## What this module has handled

The links page and `GET /api/v1/links` show two tables of counts for this
module. One table counts, for each kind of message, how many the module
handled, passed on and threw away. The other counts how many it threw away for
each reason. A pair that counted nothing is left out, and the counts start at
zero again every time the module starts.

They are not on your server's metrics page. Your server writes every series
there from a table compiled into it, so no module can add one.

## Events about the servers under this one

The links page shows where every server stands right now. It does not say who
approved which server and when. An operator who sends events to a webhook, to
Kafka or to a history store needs that record, so your server publishes one
event per thing that happened:

- `com.prusa.connect.link.node.approved` and `...revoked` name the person who
  decided, the link and the server. Your server writes them on the route the
  person called, after this module confirmed the decision. This module cannot
  publish them, so nothing below can forge a person.
- `com.prusa.connect.link.node.joined` names a server your site had no record
  of, with its place in the tree and its public key.
- `com.prusa.connect.link.node.lost` and `...returned` name a connection that
  closed and one that opened again. The server stays in the list either way.
- `com.prusa.connect.link.node.denied` names the server above refusing this
  site's request to join, with both identifiers for the refusal.

Your server works the last four out by comparing the reading this module sends
with the reading before it. So this module makes no extra call, and it cannot
report the same server joining twice. The first reading after a restart
publishes nothing, because there is no earlier reading to compare it with.

## What a failure is called

Every failure a person can see carries a code. A code is an identifier such as
`com.prusa.fractal/JOIN_DENIED`. The English sentence goes out beside it, so a
page with no Czech or Polish sentence for that code still shows something.

A refusal of the connection to the server above carries two numbers, and they
mean different things. `code` is the protocol's own number, which the server
above chose. `fault_code` is this module's code, which the page keys its own
sentence by. This server can refuse the grant of the server above: for a
protocol version or a `files` entry it does not support, or because another
server above sent it. It also refuses a grant with another Node Path or with a
missing field. Then `code` is absent and `fault_code` is
`GRANT_REFUSED`. Look at the settings of this
server and of the server above. This server keeps the refusal in
`refusal.json`, as it keeps a revocation, and serves the servers below. Once
the settings are fixed, stop its module, delete that file and start the module.

A `refusal.json` that this server cannot read keeps it from asking the server
above, as a revocation does. Then `fault_code` is `REFUSAL_UNREADABLE`, and
the module log has the error. Stop the module, delete that file and start the
module.

Two more places show a code. A link that refuses to start shows why on its
instance. The reasons are a key the settings do not have and a length of time
written in a format this module does not read. They are also a directory it
cannot write, a TLS file it cannot read, and an address it cannot take. An
approve or a revoke that fails answers with the code as well: a server this
link has no record of, or a decision it could not write down.

A method call from the server above is answered with a Fractal error code and
not with one of these. That is the protocol's own set of numbers, and
`packages/fractal/bridge/codes.go` is the whole table this module sends from.
Each row names the base code of `fractal.md` it refines and the English
sentence that goes with it.

## Printer data sent to the server above

Each printer appears as a set of Fractal interfaces. The instance name of an
interface is the identifier your server gave that printer. A print head and a
slot of a multi-material unit add `_` and their number, counted from 1, such
as `<identifier>_2`. The interfaces come from the Prusa interface
specification. They report the printer's identity, its
firmware, its state and speed, the axes and the bed, and each print head. They
also report each slot of a multi-material unit, and the current print job. The
filament a print has used has a value only from a printer whose driver reports
it, and a Prusa printer's driver does not.

When a printer does not report a reading, the property is `null`. This module
does not provide the chamber, the controller board temperature or resin
printers, because no printer sends those readings to this
server. `packages/fractal/bridge/mapping.json` names the reading each property
comes from. It also gives a reason for every property this module does not
provide.

## Permissions for commands from the server above

When the server above sends a command, for example "pause this print", the
module passes it to your server. Your server checks permissions before the
printer receives it. The setting `command_principal` selects whose permissions
your server checks.

With the default, `node`, your server checks the permissions of the server
above. Your server knows it as `node:<name of the server above>`, for example
`node:school`. That node has no access to any printer at first. An
administrator of your server grants it access, printer by printer, the same
way as for a person.

With `self`, your server checks the permissions of this module. Write
`"command_principal": "self"` in the settings file.

`node` and `self` are the only values. Your server refuses any other value when
it starts the module, and the message names the key and the two values.

A print of a file from the network is a `PrintFile` with a `fractal` URL. It needs
`operate` on the printer and `read_storage` on the storages. The module asks
your server about both before it answers. When the principal lacks either, it
refuses the print at once, as it refuses a print of a file on the printer.

A `PrintFile` starts a print only on a printer whose `state` is `READY`. A
printer that reports IDLE is on and nobody has marked it ready, so the module
shows it as `BLOCKED` and refuses the print with `0x2013`. Send `SetReady` to
that printer first, the same way as `PrintFile`.

Every other method follows the state rules of the Prusa interface
specification in the same way. A call in a state the specification forbids,
such as `Home` while the printer prints, gets `0x2013`, and the printer
receives nothing. `mapping.json` lists the states for each method.

The file lands in the folder `usb/` of the printer under the `name` of the URL.
When the printer already holds a file of that name, the file lands under the
name with a hyphen and the first 12 characters of its digest, such as
`cube-7FcFH9hw1lY1.bgcode`. Your server starts a copy it sent there itself
with no transfer, as long as neither side changed since. A restart of your
server forgets what it sent. When the printer holds a file under both names that
your server cannot take for this one, the print does not start. The log line
then says the printer holds a file at both. Delete one of them from the printer
and print again.

### What operate_resource on this link lets a person do

Know this before you grant it. A person with `operate_resource` on this link
can send a command to every printer in the tree below this server. The call
goes down the tree as this server, so the name of the person stops here. A
server below sees the server directly above it and never the person.

Each server below decides for itself what that name may do, with its own
`command_principal` and its own grants. So a server below can refuse a command
this server sent. The grant here is still the widest one on this page: it
covers the whole tree, and not one printer.

In both cases the module itself needs the `operate` permission. You approved
`observe` and `operate` when you installed the module. If you remove `operate`
from the module's entry, your server refuses every command from the network.
The module still sends printer state to the server above.
