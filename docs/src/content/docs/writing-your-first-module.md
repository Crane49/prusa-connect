---
title: Write your first module
description: Build a module with the Go SDK that counts telemetry samples, run it against simulated printers, and read the count over the API.
audience: extend
rewrite:
  describes: current
  implementedBy:
    - sdk/busapi
    - sdk/routesapi
    - sdk/wirerpc
    - examples/service-plugin
    - examples/spools
    - examples/nozzle-watch
    - cmd/demo
---

You have run `make demo` and want to add something of your own. This page
builds a module that counts the fleet's telemetry samples and answers the
count on one route. Every command was run in this order, and the output is
what it printed.

A module is a separate program the server starts and supervises. It talks to
the server over its own stdin and stdout, receives the name of whoever is
calling, and is handed no secret. It serves routes under its own namespace at
`/api/v1/modules/<namespace>/`.

You need Go 1.26 and a checkout of this repository. The SDK is not published
yet, so `go.mod` points at the checkout with a `replace` line that goes when
it is released.

## Copy the example and name it

Copy `examples/service-plugin` to a directory of your own, outside the
checkout, and make it a module:

```bash
cp -r prusa-connect/examples/service-plugin sample-counter
cd sample-counter
rm fleet.go openapi_test.go
go mod init github.com/acme/sample-counter
```

Write `go.mod` so it requires the SDK from your checkout:

```go
module github.com/acme/sample-counter

go 1.26.11

require github.com/prusa3d/prusa-connect/sdk v0.0.0

replace github.com/prusa3d/prusa-connect/sdk => ./.core/sdk
```

The module path decides the namespace. `github.com/acme/sample-counter`
becomes `com.github.aux.acme.sample-counter`: the host reversed, the label
`aux`, then the rest of the path.

## Count the samples and answer on one route

Replace `main.go` with this. `wire.Handshake` is the first line the host
reads and `wirerpc.NewConn` is the session. `busapi.Subscribe` reads the
event stream, `routesapi.Serve` serves an ordinary `http.Handler`, and
`routesapi.Describe` attaches the OpenAPI document.

```go
// Command sample-counter counts the telemetry samples the fleet reports and
// answers the count at GET /count under its own namespace.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

const (
	name    = "sample-counter"
	release = extension.Version("0.1.0")
)

//go:embed openapi.json
var openAPI []byte

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

// run writes the handshake line on stdout, then answers the host's calls
// until the host closes stdin.
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	line, err := wire.Handshake{
		Protocol: wire.ProtocolMajor,
		MinMinor: wire.MinMinor,
		MaxMinor: wire.MaxMinor,
		Address:  wire.StdioAddress,
	}.Line()
	if err != nil {
		return err
	}

	if _, err = os.Stdout.WriteString(line); err != nil {
		return err
	}

	serves := wirerpc.Methods{}

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{Role: wirerpc.RoleChild, Methods: serves})
	if err != nil {
		return err
	}

	var seen atomic.Int64

	serves[wirerpc.MethodInfo] = func(context.Context, wirerpc.Call) (any, error) {
		return routesapi.Describe(wirerpc.Info{Name: name, Version: release, Contract: wirerpc.Contract}, openAPI)
	}
	serves[wirerpc.MethodShutdown] = func(context.Context, wirerpc.Call) (any, error) {
		return nil, nil
	}
	serves[routesapi.MethodServe] = routesapi.Serve(routes(&seen))

	go count(ctx, conn, &seen)

	return conn.Serve(ctx)
}

// count subscribes to the telemetry type as the module itself and adds up the
// envelopes in every window. A refused subscription is retried after a second.
func count(ctx context.Context, conn *wirerpc.Conn, seen *atomic.Int64) {
	params := busapi.SubscribeParams{Types: []string{event.TypeDeviceTelemetrySampled}}

	for ctx.Err() == nil {
		err := busapi.Subscribe(ctx, conn, params, func(window busapi.Window) error {
			seen.Add(int64(len(window.Telemetry)))

			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: subscribe: %v\n", name, err)
		}

		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
		}
	}
}

// routes is the module's HTTP surface. The host mounts it under
// /api/v1/modules/<namespace>/ and names the caller it authorized.
func routes(seen *atomic.Int64) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /count", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"samples":  seen.Load(),
			"asked_by": routesapi.PrincipalOf(r),
		})
	})

	return mux
}

// stdio is the channel the host handed this process.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
```

The host coalesces telemetry into windows, one envelope per changed key, so
the count is envelopes and not raw readings.

Replace `openapi.json` with the document for the one route. The path is
relative to the mount. Every operation needs an `operationId`, a `summary`
and a `description`, or the host refuses the module at start.

```json
{
  "openapi": "3.1.0",
  "info": {
    "title": "sample-counter",
    "version": "0.1.0",
    "description": "The one route sample-counter serves. The path is relative to the mount the host assigns."
  },
  "paths": {
    "/count": {
      "get": {
        "operationId": "read-count",
        "summary": "How many telemetry samples the module has seen",
        "description": "The number of device.telemetry.sampled envelopes the module has read since it started, and who the host says is asking.",
        "responses": {
          "200": {
            "description": "The count.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "samples": {"type": "integer", "description": "Envelopes seen since the module started."},
                    "asked_by": {"type": "string", "description": "The principal the host authorized."}
                  },
                  "required": ["samples", "asked_by"]
                }
              }
            }
          }
        }
      }
    }
  }
}
```

## Build it and lay out the release

`prusa-connect module add` installs a release. A release is one
directory: the manifest at its root, one binary per platform named
`<name>_<GOOS>_<GOARCH>` and `.exe` on Windows only, and a `checksums.txt` the
install verifies. A module that draws in the browser puts its built bundle
under `ui/` beside them. This one has no browser half, so its release is the
manifest and the binary.

Write `module.yaml` at the root of your module:

```yaml
name: sample-counter
kind: sink
license: MIT
version: 0.1.0
contract_major: 1
openapi: openapi.json
platforms:
  - windows_amd64
permissions:
  - observe
```

`name` decides the file name of the binary and the instance the install
writes. `permissions` is the ceiling on what the module may do as itself, and
the subscription above takes `observe`. There is no namespace key. The host
derives the namespace from the module path you install from, so no module can
name another module's address.

Build the binary into a release directory, copy the manifest beside it, and
write the digests:

```console
$ go mod tidy
$ go build -o release/sample-counter_windows_amd64.exe .
$ cp module.yaml release/
$ cd release && sha256sum module.yaml sample-counter_windows_amd64.exe > checksums.txt && cd ..
```

`module add` records the digest of every file it installs. Every build moves
that digest, so you never copy one into a declaration yourself.

## Install it

Write `demo.yaml` beside the module. It says where the server keeps the record
of what is installed, where the installed files go, and where the authorization
graph is. The server writes all three, and you write none of them:

```yaml
core:
  overlay: C:/Users/you/sample-counter/demo/resources.json
  modules: C:/Users/you/sample-counter/demo/modules
  authz:
    database: C:/Users/you/sample-counter/demo/config.d/authz.db
```

All three paths are under the demo directory, which is the directory `-dir`
names further down. The demo keeps its own record, its own files and its own
graph there whatever this file says. So the install has to name the same
overlay and modules paths, or the demo reads none of what the install wrote.
With the graph path, `module add` and `module remove` write the module's
grants into the demo's own graph. Without it they write another
graph, and the demo corrects its own graph at its next start. It writes the
grants each installed module recorded, and withdraws every module grant none
records.

Now install the release. The argument is the module path and the version, and
the module path is what the namespace is built from:

```console
$ prusa-connect module add github.com/acme/sample-counter@0.1.0 -config demo.yaml -from release -approve
installed com.github.aux.acme.sample-counter/sample-counter as sink:sample-counter
  files       C:\Users\you\sample-counter\demo\modules\sample-counter
  namespace   com.github.aux.acme.sample-counter
  checksums   2 files verified against checksums.txt
  granted     org:local#viewer@sink:sample-counter for observe. The viewer role also carries view_resource, read_storage
  enabled     yes

The record is written to C:/Users/you/sample-counter/demo/resources.json. No core is answering on :8080, so the next start reads it.
```

`-approve` is you saying yes to what the manifest asks for. It writes the
permissions into the declaration, grants the module what those permissions
name, and switches the instance on. Without it the declaration is written
switched off, nothing is granted, and the command prints what the module asked
for so you can read it first.

The grant is the line that begins `granted`. A module reads the printers of its
server as its own principal, here `sink:sample-counter`, and that principal
holds nothing until a grant is written for it. One grant on the organization
covers every printer this server has, including one it admits tomorrow.

Read the rest of that line too. A grant is a role, and a role carries several
actions. The viewer role on the organization also lets the module list this
server's configured resources and read the files in its storages. Write the
grant on one printer instead and it reaches that printer alone.

`module list` shows what is installed, and `module remove` takes the
declaration, the files and these grants away.

A core that is already running does not read the record. It reads it at its own
start, so restart it, and the command says which of the two happened. Run the
same line again after a rebuild: it replaces what it installed, and on Windows
the core has to be stopped, because a running module holds its binary open.

## Start the demo

`make demo` starts the server with four simulated printers and your module in
one process. Point it at the file you wrote and at the directory the install
used, so it reads the record `module add` left there. Its log is quiet unless
you ask for it:

```bash
make demo DEMO_ARGS="-config C:/Users/you/sample-counter/demo.yaml -dir C:/Users/you/sample-counter/demo -log-level info"
```

The module's launch is in the log:

```
level=INFO msg="the child process is serving" kind=sink resource=sample-counter resource=sink:sample-counter
  path=C:\Users\you\sample-counter\demo\modules\sample-counter\bin\sample-counter_windows_amd64.exe
  plugin=sample-counter plugin_version=0.1.0 contract=1.0.0 wire_minor=0
  mount=/api/v1/modules/com.github.aux.acme.sample-counter
level=INFO msg="the module's OpenAPI document is served" kind=sink resource=sample-counter resource=sink:sample-counter
  path=/api/v1/modules/com.github.aux.acme.sample-counter/openapi.json paths=1
```

Once the printers are connected it prints the account to sign in with. The
password is new on every run:

```
Sign in as the demo account. Its password is new on every run:

  username  demo
  password  qsjphyacz2gosnkva22a
```

Leave it running.

## Mint a token and read the count

The demo serves a login, so a request with no credential is refused with
`core.auth/TOKEN_INVALID`. Log in as the printed account and mint a token.
The demo prints both of these lines with the password filled in, so you can
copy them from its terminal:

```console
$ curl -s -c cookies.txt -X POST http://127.0.0.1:8080/api/v1/auth/login -H 'content-type: application/json' \
    -d '{"username":"demo","password":"qsjphyacz2gosnkva22a"}'
{"expires_at":"2026-09-17T10:25:00.3922436+02:00"}

$ curl -s -b cookies.txt -X POST http://127.0.0.1:8080/api/v1/auth/credentials -H 'content-type: application/json' \
    -d '{"name":"sample-counter guide"}'
{"id":"STTmTqUm4tEFVfJORGKSkY","name":"sample-counter guide","subject":"user:operator","created_at":"2026-09-10T10:25:00.4475361+02:00","expires_at":"2026-12-09T09:25:00.4475361+01:00","token":"pcl_STTmTqUm4tEFVfJORGKSkY_abEW4e9a6ft2HJGriETLQPM4M3HAq7wiZUmQIdFH4xL2tCY7j"}
```

The token is shown once. Put it in `TOKEN` and read the count:

```console
$ curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/api/v1/modules/com.github.aux.acme.sample-counter/count
{"asked_by":"user:operator","samples":325}

$ curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/api/v1/modules/com.github.aux.acme.sample-counter/count
{"asked_by":"user:operator","samples":650}
```

It counts, because `-approve` granted `sink:sample-counter` the fleet at the
install. The permissions list says what the module may ask for, and the grant
says what it receives. Take the grant away and the module receives nothing:

```console
$ curl -s -H "Authorization: Bearer $TOKEN" -X DELETE \
    'http://127.0.0.1:8080/api/v1/grants/org/local?relation=viewer&subject=sink:sample-counter'

$ curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/api/v1/modules/com.github.aux.acme.sample-counter/count
{"asked_by":"user:operator","samples":650}
```

The number stops moving within a second. Write the grant again to start it:

```console
$ curl -s -H "Authorization: Bearer $TOKEN" -X POST http://127.0.0.1:8080/api/v1/grants/org/local \
    -H 'content-type: application/json' -d '{"relation":"viewer","subject":"sink:sample-counter"}'
{"object":"org:local","relation":"viewer","subject":"sink:sample-counter"}
```

## Where to look when it does not work

The server log is the first place, and under the demo that is
`DEMO_ARGS="-log-level info"`. A wrong digest fails the instance alone:

```
level=WARN msg="resource failed to start" kind=sink resource=sample-counter attempt=1 class=permanent
  error="core.internal/RESOURCE_PLUGIN_CHECKSUM: the artefact does not match its recorded checksum: ...
```

Whatever the module writes to stderr lands in the same log. Without `observe`
under `permissions` in the manifest, or without `-approve` on the install, the
module says so every second:

```
level=INFO msg="sample-counter: subscribe: bus.subscribe: busapi: the instance may not subscribe as that:
  sink:sample-counter takes observe, and the instance declares no such permission for its own work"
  resource=sample-counter
```

`GET /api/v1/resources/sink/sample-counter` answers the instance as JSON, with
its state and these three fields:

```
"runtime":"service","namespace":"com.github.aux.acme.sample-counter",
"openapi_url":"/api/v1/modules/com.github.aux.acme.sample-counter/openapi.json"
```

That last address is your document with the path rebased under the mount,
which is what a client generator reads.

## A module that adds a page

The counter above answers on a route. A module can also draw: add an item to
the sidebar, and a whole screen behind it, on the same component library the
core's own pages are built from.

`examples/spools/` is that module, built and proved end to end. It tracks
filament spools per device, and it is five files longer than the counter:

| file | what you write in it |
| --- | --- |
| `module.yaml` | the `ui` block: the bundle's entry, the library range, and the one page under `contributes.pages` |
| `ui/vite.config.ts` | one call to `moduleBuild()` from `@prusa/connect-ui/build` |
| `ui/src/index.ts` | `defineModule((host) => host.register.page('spools', SpoolsPage))` |
| `ui/src/spools-page/spools-page.svelte` | the page, on the library's `PageLayout` and primitives |
| `ui/src/styles.css` | four lines that give the bundle its own Tailwind. [Styling a module](./styling-a-module.md) says why |

The page reaches the module's own routes through `host.client`, which is the
app's SDK with the session the browser already holds. A typed client for those
routes comes from the module's own fragment:

```bash
go run ./cmd/generate-client-sdk -module \
  -document examples/spools/openapi.json \
  -namespace com.github.aux.acme.spools \
  -name spools-client -out examples/spools/client
```

`bind(host.client)` then gives one method per operation, each sending the
browser's cookie to the rebased path under the module's mount. The page holds
no token, names no address and calls no `fetch`.

Build both halves and run them:

```bash
bash scripts/check-example-spools.sh
make demo DEMO_ARGS="-config bin/spools/config.yaml -dir bin/spools/demo"
```

The script builds the Go half and the bundle, records the digest of both, and
installs the module with `prusa-connect module add`. One command and one
declaration: the process and the bundle travel in one release under one
manifest, and the listing shows one line. The script prints the `make demo`
line above when it passes, with the configuration it wrote.

Sign in, and **Spools** is in the left rail under **Extensions**. It opens
`/m/com.github.aux.acme.spools/spools`, which is the page in
`ui/src/spools-page/`, drawing the shelf from the module's own route with the
session the browser already holds. `examples/spools/README.md` says what the
module still lacks.

## A module that adds a section to a printer's page

A module can also add to a screen the app already owns, and that costs no
process at all. `examples/nozzle-watch/` is one browser bundle, one `asset`
declaration, and a section at the foot of a printer's Status tab showing the
nozzle temperature and its target. It asks the core for nothing, because the
fleet is already on the host object the bundle receives, and the app keeps that
one fleet current from the event stream.

```yaml
name: nozzle-watch
kind: asset
license: MIT
version: 1.0.0

ui:
  entry: ui/index.js
  library: ">=0.0.1 <1.0.0"
  contributes:
    device_section:
      - id: nozzle
        title: Nozzle temperature
```

The same `check-example-spools.sh` installs it. Open a printer from the
printers page and the section is under the core's own status card, with
**Nozzle temperature** as its heading. The heading is the manifest's, so an
operator reads it before switching the module on and the running code cannot
change it.

A module that adds nothing a person can see is still worth looking for on
**Settings**, **Extensions**. Every installed module with a browser half is
listed there, and a module the app refused to run is named with the reason and
its code.

## Read these next

| page | covers |
|---|---|
| `examples/spools/README.md` | the worked module with a page: both halves, the five files, and what is missing |
| [Writing a service module](./writing-a-module.md) | the reference: records, publishing events, the host's limits and failure shapes |
| [More room for records](./writing-a-module.md#giving-a-module-more-room-for-its-records) | asking for more than 8 MiB of records, and granting it at install |
| [`extension`](./reference/go-sdk/extension.md) | the package a module is built on: each exported identifier, its declaration and its comment |
| [The plugin wire protocol](./plugin-wire-protocol.md) | the frames and calls, for a module in another language |
| [The extension host](./internals/the-extension-host.md) | how the host launches, supervises and stops a module |
