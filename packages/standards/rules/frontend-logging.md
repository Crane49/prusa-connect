---
paths:
  - "**/*.ts"
  - "**/*.svelte"
  - "**/*.svelte.ts"
---

# Frontend logging

The browser logger, `src/lib/log` in the frontend when this repository has
one. Browser consoles appear in screenshots, screen shares and bug reports,
so the console is a leak channel. Treat every log line as if a customer will
read it.

## The logger, never console

- `console.*` is an ESLint error everywhere except inside `src/lib/log/`, the transport itself. `no-debugger` and `no-alert` are errors too
- import `{ log }` from `$lib/log`. A feature takes a scoped child once and reuses it: `const fleetLog = log.scope('fleet-store')`
- structured calls: `log.warn('refresh superseded', { deviceId })`, a message plus a context object, never string concatenation
- levels: `error` (broken, someone acts), `warn` (degraded, fallback active), `info` (rare lifecycle facts), `debug` (dev diagnosis), `trace` (per-event verbosity)

## Cost discipline

Below-threshold calls must stay cheap. A context that is expensive to build
takes the lazy form, so it is never constructed when suppressed:

```ts
fleetLog.trace('rows replaced', () => ({ count: rows.length, ids: rows.map(r => r.id) }));
```

## Never log

- PII: emails, names, addresses, phone numbers
- credentials: tokens, `Authorization` headers, cookies, session ids
- whole API response objects, forbidden always. A response carries customer data by design. Log the two fields the message needs (`{ status, path, count }`), never the payload
- for recurring context shapes, `redact(ctx)` from `$lib/log` drops the sensitive fields: email, token, authorization, name and friends, by key substring, case-insensitive. Dropped, never replaced with a marker

## Runtime level

The app is a static build, so `PUBLIC_*` env vars are inlined at build time
and useless for this. The level rides the same runtime file as the backend
URL: `static/config.js` sets `window.__PRUSA_CONNECT_LOG_LEVEL__`. The
process that serves the frontend writes that file from `LOG_LEVEL` at start,
so one build serves any environment. Defaults: `debug` in dev, `warn` in
production. `setLogLevel()` validates, and garbage keeps the current
threshold.

## Trace ids, later

The context object is an open `Record<string, unknown>`, so a `traceId`
attaches when the core's trace id reaches the browser. Do not build
correlation plumbing before that exists.
