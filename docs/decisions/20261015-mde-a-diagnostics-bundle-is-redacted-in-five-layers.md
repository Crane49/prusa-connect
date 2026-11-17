---
title: "ADR-20261015-mde: a diagnostics bundle is redacted in five layers"
description: "An org admin downloads one zip from Settings, or the browser sends it to the Prusa beta portal; the server builds it in memory, stores nothing, never sends it, and removes secrets and personal data in five layers."
date: 2026-10-15
status: accepted
tags:
  - adr
  - observability
  - security
  - api
---
# ADR-20261015-mde: a diagnostics bundle is redacted in five layers

- status: accepted
- date: 2026-10-15

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | ADRs: [20260824-kkr](20260824-kkr-repository-layout-and-package-boundaries.md)

A beta tester who files a ticket needs to send the evidence with it: the failing
request, recent server logs, the last crash and the shape of the install. The
product runs on a local network and sends nothing to us on its own. So the
tester attaches the file to the ticket, or presses Send to Prusa and the
browser uploads it. The consent tick before either is the consent.

## rules

1. `POST /api/v1/diagnostics` builds the bundle. Only an org admin may call it,
   because the bundle describes the whole install. The body is the browser's
   buffer as a JSON array. A body that is not one, or is larger than 256 KB,
   is left out and `manifest.json` says why. The response is a zip named
   `pcl-diagnostics-<version>-<UTC>.zip`. The server stores nothing.
2. `internal/app/diagnostics` owns the bundle and the redaction policy.
   Transport only calls the `Diagnostics` interface. The composition root reads
   the sources under the admin's identity and calls `diagnostics.Build`. Send to
   Prusa builds the bundle through the same route, so it holds the same files
   (rule 16).
3. The bundle holds `manifest.json`, `config.json`, `modules.json`,
   `printers.json`, `health.json`, `accounts.json` (a count), `logs.jsonl`,
   `frontend.jsonl` and, when one exists, `crash.log`. Every entry is dated
   with the time the bundle was built.
4. Five layers remove data. Each one catches what the one before it misses.

   1. The allow-list. Config, modules, printers, health and accounts are
      projected field by field from the domain types. A new field is absent
      until someone projects it. A module's settings in `modules.json` keep
      their keys and lose every value. Each value is replaced by its type
      (`string`, `number`, `bool`, `list of N`) or by `unset`. A module may declare an API
      key or a broker address as a plain string, so no value of one is
      trusted.
   2. Log keys. A key is kept, text-scrubbed, denied or hashed.
      - Kept: ids, counts, and enums such as `type`, `operation` and
        `trace_id`. Also `theme`, `skin` and `language`, and `blocked` and
        `instances`, which are kind:name instance refs that `modules.json`
        shows anyway.
      - Kept: `printer`, which is always a device id. A printer's name is
        logged as `printer_name` and is hashed.
      - Text-scrubbed: `reason`, `result`, `error`, `detail` and `stack` keep
        their text after layer 3.
      - Denied: `setup_code`, `fingerprint`, `username`, `subject`, `identity`
        and `gcode`.
      - Hashed: any other key, `connector` and `driver` among them.
   3. Value patterns. Every string left is scrubbed of URLs, email, IPv4 and
      IPv6 addresses, MAC addresses, bearer tokens, cookies, JWTs and absolute
      paths. A URL keeps its scheme, its host is hashed, and the rest is
      dropped. A host name is hashed where an error names one without a
      scheme: after `lookup`, before a port, or under a local-network domain
      such as `.local` or `.lan`. A quoted account name after `user` or `role`
      is hashed. `Basic`, `Token` and `Digest` credentials, long runs of
      letters and digits, and `password=` style pairs are scrubbed too.
   4. The known-secret sweep. The live secrets are removed as substrings from
      every file. They are the config secrets, the generated first-run setup
      code, and the `secret` and `url` option values of modules. An option
      that holds a block, such as webhook headers, gives every string in it. A
      module URL is swept with its paths, its query and its password. The
      sweep also runs on each string before layer 3, because a pattern that
      stops at `&` would otherwise leave the tail of a password behind. A
      secret shorter than 6 characters is not swept, because it matches
      ordinary text too often.
   5. The reader. The zip is a plain archive. Settings lists what it holds and
      says that sensitive details may remain after scrubbing. The download
      waits until the admin ticks that they agree to share the file with the
      Prusa team. The admin may open it before attaching it.

5. Config values are reduced by type. A string becomes `set` or `unset`, which
   covers file paths and the marketplace URL. An address becomes its port. A
   list, and any map the config does not document, becomes a count. Tunables,
   switches and enums keep their value.
6. A connector instance name is chosen by the operator, so it is hashed
   wherever it appears. It appears in the `connectors` keys of `config.json`,
   the rollups in `health.json`, the `driver` of each printer in
   `printers.json`, and the `connector` and `driver` log keys. One salt gives
   one hash in every file, so the files still match each other. The dialect
   it runs (`type`, such as `prusalink`) is kept in clear.
7. A frontend field that holds free text (`message`, `scope`, and the text
   keys of layer 2 such as `error` and `reason`) is scrubbed by layer 3. Any
   other string field, and every string in a list field, is hashed unless it
   is on the whitelist (`operation`,
   `method`, `traceId`). An error `code` is kept only when it has the shape of
   a server error code. Each entry's `route` is the router template of the
   screen it was written on, such as `/printers/:id`, as the app sends it. The
   server checks only its shape, static segments and `:params`, and hashes
   anything else. A lowercase address has that shape too and would be kept.
   An error's `params` keep a value only for a key that names a machine
   identifier, such as `verb`, `argument`, `field`, `state`, `kind` or `code`.
   The keys are listed twice, in `frontend/src/lib/client.ts` and in
   `logs.go`. The value is kept only when it is 1 to 64 letters, digits and
   `_.:/-`. The browser sends any other key by
   name alone, and the server hashes any other value it is sent.
8. Hashing is an HMAC with a random salt per bundle. The same value gives the
   same hash inside one bundle, so log lines still match each other. Two
   bundles cannot be linked.
9. Module stderr is opt-in, through a checkbox or `?modules=true`. A module can
   print anything. Split lines are joined again per instance before layers 3
   and 4 run. A log message is cut to 8 KB after the sweep and before the
   patterns, whose cost grows with the text; a joined line stops growing at
   twice that.
10. A crash stack goes to `crash.log` next to the overlay, through
    `debug.SetCrashOutput`. The bundle carries the last 64 KB of it, from the
    first whole line, so a token cut by the 64 KB mark is not carried. At
    start a longer file is cut back to that tail.
11. The browser keeps the last 200 entries at info and above in memory, after
    its own pass of the layer 3 patterns (`frontend/src/lib/log/index.ts`).
    Each move to another screen is an info entry in scope `nav` carrying the
    route template, so the buffer holds the path the person took. Clicks,
    their text and successful calls are not recorded.
    "Copy browser details" puts them on the clipboard after that pass alone,
    because the copy never reaches the server. An inner error boundary around
    each screen keeps navigation alive when a screen fails. The outer boundary
    is the last resort and offers the download too.
12. The first-run setup code stays in the log on purpose, as the fallback for a
    banner that cannot be written. The bundle removes it through rule 4.
13. The server writes one INFO line, `operation refused`, for every operation
    it answers with a 4xx other than 401. The line holds only `operation`,
    `status`, `code` and `trace_id`, where `operation` is the operation id and
    never the path. So a refusal the browser reports has a server-side line
    under the same trace id. A 401 is left out because a page polling an expired session would
    fill the buffer. A 5xx keeps its own single ERROR line.
14. The bundle is an optional part of the binary, behind the `diagnostics`
    build tag. `core.diagnostics.enabled` implies the tag and is on by
    default, so the full and printer builds carry the bundle. The minimal
    build never does. Without the tag, or with the key off, the route answers
    404, no log ring or crash file is kept, and `GET /api/v1/ui` reports
    `diagnostics: false`. The app then hides the Settings entry, the button
    on a crashed page and the report link. A deployment with no sign-in
    makes everyone on the network an admin, so there the key is off unless
    written as `true`.
15. The server builds one bundle at a time and answers a second request
    meanwhile with 429 `core.validation/DIAGNOSTICS_BUSY`. The build stops
    when the request is cancelled. The log ring keeps the newest 2000 records
    and at most 2 MB of their text, because 2000 module lines of 8 KB each
    would hold 16 MB.
16. Send to Prusa is a second action beside the download, and the browser does
    the upload. The server never calls out.
    - The same consent tick gates it, because sending is sharing.
    - The click opens an empty tab first, so a popup blocker does not stop it.
      The browser then builds the bundle through rule 1, with the same modules
      choice.
    - It posts the zip to `{send_url}/api/diagnostics?product=connect-local&version=<version>`
      with `Content-Type: application/zip` and no credentials.
    - The portal answers 201 with the address of its page for the upload. The
      tab opens that page with `intent=new` or `intent=existing`. A refusal
      closes the tab and is shown on the Diagnostics page.
    - `send_url` is `core.diagnostics.send_url`. It is
      `https://beta.prusa3d.com` when the key is absent, and an empty value
      hides the button. `GET /api/v1/ui` serves it as `diagnostics_send_url`.

## enforcement

| gate | what it holds |
|---|---|
| tests in `internal/app/diagnostics/bundle_test.go` | a golden test plants secrets in every source and asserts that none is in the zip, with module output on and off |
| tests in `internal/app/diagnostics/scrub_test.go` | each value pattern, and the known-secret sweep |
| tests in `internal/app/diagnostics/logs_test.go` | the four outcomes for a log key, the keys kept in clear, and the params and route a browser entry keeps |
| tests in `internal/transport/http/httpapi/diagnostics_test.go` | a caller who is not an org admin is refused |
| tests in `internal/transport/http/httpapi/diagnostics_nodiagnostics_test.go` | a build without the tag serves no route and reports `diagnostics: false` |
| tests in `internal/app/config/build/tags_test.go` | the minimal variant leaves the tag out whatever the file says |
| tests in `internal/transport/http/httpapi/problem_test.go` | a refusal writes one line with four attrs, a 401 none, a 5xx its own one |
| tests in `frontend/src/lib/log/index.test.ts` and `client.test.ts` | the route stamp, the navigation entry and the params the browser keeps |
| tests in `packages/svelte/src/data/services/send-diagnostics.test.ts` | the upload request, the refusals, and a page outside the portal refused |
| tests in `frontend/src/lib/send-diagnostics.test.ts` | the tab opens before the build, gets the intent, and closes on a failure |
| tests in `internal/app/config/diagnostics_test.go` | the default portal, the empty value, and an address the browser cannot post to |

## consequences

- Free text with no recognisable shape, such as a printer's custom name inside
  a wrapped error, can pass layers 2 to 4. Layer 5 is the mitigation, and
  the operator's review is part of the design.
- A new domain field is absent from the bundle until it is projected.
- Send to Prusa needs a browser that reaches the portal. A server with no
  internet access still sends, as long as the admin's browser has it.
- The portal must accept a cross-origin POST from any origin, because every
  install has its own address.

## rejected

| option | why not |
|---|---|
| Sentry or GlitchTip | it needs a reachable DSN and reports by default, and the product never phones home |
| uploading from the server | the product never calls out on its own, and the server may have no internet |
| module output always included | it is the dirtiest source, so the admin opts in |
| a fixed salt | two bundles from one install could be linked |
| copying config and printers, then removing fields | a new field would leak until someone noticed |
