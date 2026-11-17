# Prusa Connect extension SDK

Import this module to extend Prusa Connect. It holds the contracts and
nothing else: no application code, no server, no database.

It is a Go module of its own, and it depends on the standard library alone. So
adding it to your project pulls in nothing, and the server's own dependencies
never reach your build.

```bash
go get github.com/prusa3d/prusa-connect/sdk
```

## What you can write

| you want to add | implement | example |
| --- | --- | --- |
| support for another printer's firmware | `connectorapi` | the xBuddy driver Prusa printers speak |
| a camera | `cameraapi` | an MJPEG or RTSP stream |
| somewhere to keep files | `storageapi` | a directory, an S3 bucket |
| a way for people to log in | `authapi` | OpenID Connect, LDAP |
| something that reacts to events | `eventsapi` | publish to MQTT, Kafka, a webhook |
| long-term metrics and history | `historyapi` | a time-series database |

Those six are the extension points. The rest of the module is what they are
built from:

| package | what it is |
| --- | --- |
| `extension` | how an extension registers itself, and what it must declare |
| `event` | the event envelope, CloudEvents 1.0, and the cursor a reader resumes from |
| `fault` | the errors every surface returns, as RFC 9457 problem+json |
| `wire` | the frames between the server and an extension in its own process |
| `wirerpc` | the request and response protocol carried on those frames |
| `stateapi` | a key/value store the server keeps for your extension |
| `busapi` | how your extension publishes an event of its own |
| `routesapi` | how your extension serves HTTP under its own path |
| `diagnosticsapi` | how your extension adds files to the diagnostics bundle an admin downloads |
| `printcompat` | whether a print file fits a printer, judged the way the printer's firmware judges it before a print |
| `uiapi` | how your module hides a page of its own while one of its features is switched off |
| `scriptapi` | the event and message shapes a small script reads |
| `authapi/oidctoken` | ID token and JWKS reading, shared by every OIDC provider |
| `fractal` | the Fractal link protocol: how one site joins the site above it |

## Writing one

Every contract has a conformance suite beside it, in a subpackage named after
it. `connectorapi/connectorapitest` is the one for printer drivers. Run it
against your implementation and it tells you which clause you broke, in words.

An extension registers itself once, from an `init` function:

```go
//nolint:gochecknoinits // registration happens before main; see ADR-20260824-nyw.
func init() { connectorapi.Register(Type, doc(), New) }
```

You never write a namespace. The server takes it from the Go module the calling
package belongs to, so nobody can register under a name they do not own. A name
can be claimed once, and a second claim is refused with both module paths named.
The descriptor you pass carries a summary, a description, a version, a contract
version and a stability level, and the published extension reference is
generated from it.

Start here:

| you are writing | read |
| --- | --- |
| a printer driver | [Writing a printer driver](../docs/src/content/docs/writing-a-driver.md) |
| a first module, end to end | [Write your first module](../docs/src/content/docs/writing-your-first-module.md) |
| an extension in its own process | [Writing a module](../docs/src/content/docs/writing-a-module.md) |
| not sure which you need | [Extension concepts](../docs/src/content/docs/extension-concepts.md) |

`make scaffold-driver` writes a compiling driver, its descriptor and its
conformance test, so you can start from something that runs.

## Joining a tree of sites

`fractal` is not an extension point. It is a protocol, and it is here so that a
node or a tool speaks it without this server. `Dial` is a child's half and
`Listen` a parent's; each holds a key, runs the connections and reports every
node it has met, with the round trip it measured. The package comment is the
specification, written so an implementation in another language can be built
from it.

## Versions

This module is at `0.3.0`, and a release tags it `sdk/vX.Y.Z`. Go needs the
directory as a tag prefix to find a module in a subdirectory.

While it is `0.x` any release may change a contract. From `sdk/v1` on, the
exported surface only grows: CI compares each release against the last one and
fails a build that removes or changes anything.

## The decisions behind this

- [ADR-20260824-gfr](../docs/decisions/20260824-gfr-the-extension-sdk-is-a-separate-module.md):
  why this is a separate module with no dependencies
- [ADR-20260825-rre](../docs/decisions/20260825-rre-one-registration-rule-and-one-descriptor.md):
  why every extension point registers the same way
