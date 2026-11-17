# @prusa/connect

A typed client for the Prusa Connect HTTP API. You give it the address
of your server and a token, and every operation the server publishes is a
method on the client, typed from the server's own OpenAPI document.

It has no runtime dependency. It uses `fetch` and `AbortSignal` and nothing
else, so it runs in a browser, in Node 22 and later, in Deno and in Bun.

## Listing printers and sending a command

```ts
import { PrusaConnect, ApiError } from "@prusa/connect";

const local = new PrusaConnect({
  url: "https://printers.local:8080",
  auth: "pcl_...", // or an async function that returns the current token
});

const page = await local.listDevices({ query: { limit: 100 } });
for (const device of page.items) {
  console.log(device.id, device.state);
}

try {
  await local.sendDeviceCommand({
    path: { device_id: "p4f2" },
    body: { verb: "core.command.pause_print" },
  });
} catch (err) {
  if (err instanceof ApiError) {
    console.log(err.status, err.code, err.message);
  }
}
```

Every method takes a request object and an optional `AbortSignal`. The request
object carries `path`, `query`, `header` and `body`, each present only when the
operation declares it. A refused request throws an `ApiError` carrying the
HTTP status, the server's error `code`, the `params` the code names and the
`trace_id` an operator can find in the log. A request that got no answer at
all throws a `NetworkError`; a `GET` is sent again a few times before that, a
`POST` never.

## Reading the event stream

```ts
const events = local.events({ types: ["com.prusa.connect.device.presence.changed"] });

let seen = 0;
for await (const event of events) {
  console.log(event.type, event.id);
  if ((seen += 1) === 10) break;
}
```

`events()` reads `GET /api/v1/events` over `fetch` with the same bearer token
as every other call, so it works where `EventSource` does not. Each event
carries the envelope's `type`, its place in the log as `id`, the payload, and
the whole envelope. When the connection drops, the subscription waits out the
server's `retry` hint, reconnects with `Last-Event-ID`, and carries on from
where it was; the loop sees no gap. Leaving the loop, calling `close()` or
aborting the `signal` ends the iteration with no error. When the server ends it,
the iteration throws an `ApiError` carrying the status: 401 for a token
revoked or expired, or whatever a refused reconnect answered. A loop that
threw logs in again rather than reconnecting.

## From a page the core serves

A page the core serves signs in through the login at `/api/v1/auth/login` and
holds no token. The credential is the session cookie the login set. It is
HttpOnly, so no script reads it, and the browser attaches it to a request to
the page's own origin on its own. Give the client `{ kind: "session" }` and no
`url`:

```ts
const local = new PrusaConnect({ auth: { kind: "session" } });

const page = await local.listDevices({ query: { limit: 100 } });
for await (const event of local.events()) {
  console.log(event.type);
}
```

In this mode the client sets no `Authorization` header, sends every request
to a relative path such as `/api/v1/devices`, and passes
`credentials: "same-origin"` on every fetch, the event stream included. The
core reads a bearer header before the cookie, so a client that sent both
would be judged by the header. A 401 means the session ended: send the person
to the login rather than reconnecting.

### One stream for every tab

A browser opens at most six connections to one host over plain HTTP, and an
event stream keeps one of them for as long as its page is open. With a stream
per tab, six tabs take every connection, and no other request of any tab gets
one. `shareEvents` makes all the tabs share one stream:

```ts
const local = new PrusaConnect({ auth: { kind: "session" }, shareEvents: true });
```

One tab reads `GET /api/v1/events` and posts each event on a BroadcastChannel,
and the other tabs listen there. The channel is named after the person signed in,
and after the server when the client names one, so the tabs of another person
never hear it. When the tab
that reads closes, another tab opens the stream again from the last event it
heard. Each `events()` call still gets only the types and origins it asked
for, and a stream the server ends still throws in every tab. Two things
differ: an origin the person may not observe is not refused, and the shared
stream uses the shortest `interval` any subscription asked for. A browser
without BroadcastChannel reads one stream per tab.

## Where the code comes from

| file | written by |
|---|---|
| `src/generated/core-api.ts` | `openapi-typescript`, from the server's OpenAPI document |
| `src/generated/operations.ts`, `errors.ts`, `events.ts`, `vocabulary.ts` | `cmd/generate-client-sdk` in the server's repository |
| the rest of `src/` | a person, and edited like any other source |

The version number is the server's. Package `0.4.0` is generated from server
`0.4.0`, so pin the one your server runs.

## Building it here

```sh
make generate-client-sdk   # regenerate from the committed contracts
make client-sdk-check      # the generated files match, the package builds, the tests pass
```

Inside this directory, `pnpm run build` writes `dist/` and `pnpm test` runs
the tests with Node's own test runner.

`make client-sdk-install-check` publishes the packed package to a local
registry and installs it into the three consumer projects under
`testdata/consumers`: a plain Node program, a Vite page and a Svelte component.
The Node one runs against `cmd/demo` and has to list a device and read an
event, and `publint` and `@arethetypeswrong/cli` read the tarball itself.
