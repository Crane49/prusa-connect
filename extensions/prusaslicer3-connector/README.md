# PrusaSlicer 3 Connector

This module lets PrusaSlicer 3 use this server the way it uses Prusa Account
and Prusa Connect. In PrusaSlicer, open **Preferences**, then **Services**, set
the mode to offline and type the address of this server. The person then signs
in, sees the printers of this server in the Connect tab and sends a print from
the print dialog, as they do with the cloud.

The module answers the slicer's requests with the cloud's bodies, status codes
and error bodies. Only the paths differ. The slicer signs in through the app
pairing of this server, not through this module. It shows a code, the person
approves it on this server's `/pair` page, and the slicer collects a
credential. The host does the rest of the work: it resolves the person from
that credential, lists the printers and prints the file. The module never
calls Prusa Account or Prusa Connect. [ADR-20261112-2eb](../../docs/decisions/20261112-2eb-prusaslicer-3-reaches-this-server-through-a-compatibility.md)
has the rules.

## The requests, in the order they run

Steps 1 to 3 are the core's app pairing
([ADR-20261025-tpp](../../docs/decisions/20261025-tpp-a-credential-carries-scopes-and-an-app-asks-a-person-for-one.md),
[ADR-20261112-nay](../../docs/decisions/20261112-nay-an-app-pairing-credential-also-travels-in-a-jwt-shape.md)).
The other paths are under the module's mount, `/api/v1/modules/<namespace>`.

| step | request | cloud request | reply |
|---|---|---|---|
| 1 | `POST /api/v1/auth/app-pairings` with no credential, `app_name` and `scopes: ["operate"]` | `account.prusa3d.com/o/authorize/` | `device_code`, `user_code`, `interval` |
| 2 | the person opens `/pair?code=<user_code>` in a browser, signs in and approves | the sign-in page | nothing for the slicer |
| 3 | `POST /api/v1/auth/app-pairings/token` with `device_code`, every `interval` seconds | `/o/token/` | `token` and `access_token`, the same credential bare and in a JWT shape |
| 4 | `GET /account/api/v1/me/` with `Authorization: Bearer <access_token>` | `/api/v1/me/` | `id`, `public_username`, `avatar_small` |
| 4a | `GET /account/api/v1/me/avatar.png?i=<initials>&c=<colour>` with no credential | the `avatar_small` picture | a PNG of the person's initials |
| 5 | `GET /connect/slicer/v2/printers` | `connect.prusa3d.com/slicer/v2/printers` | `{"printers": [...]}` |
| 5a | Set as current on a printer, inside PrusaSlicer only, posts `SELECT_PRINTER` with the `uuid` of step 5 | Set as current in the Connect web | the slicer selects the printer and opens its Slicing tab |
| 6 | the print dialog `/m/<namespace>/select-printer?embed=1` posts `PRINT` and `CLOSE_DIALOG` to the slicer | `/slicer-select-printer` | the slicer starts the upload |
| 7 | `POST /connect/app/users/teams/{team}/uploads` | the same path | the upload, with `id` |
| 8 | `PUT /connect/app/teams/{team}/files/raw?upload_id=<id>` with the file | the same path | the file. The host starts the print |

The slicer sends `access_token` as `Authorization: Bearer` on steps 4 to 8.
Before it shows a page, it opens a page session at
`POST /api/v1/auth/app-pairings/session` and puts the HttpOnly cookie the
answer carries into its web view. The pages never receive the credential.
The credential lives ninety days, and the person sees it as a paired app
under **Settings**, **API credentials**, where they may revoke it. Revoking it
ends every page session it opened. A refused credential sends the slicer
back to step 1.

Step 4a is a public route: the host passes it on with no credential, because
PrusaSlicer downloads `avatar_small` without one. The operator approves the
route at install. `avatar_small` is a reference relative to the me address,
such as `avatar.png?i=AS&c=fa6831`, because the module does not know the
server's address. The me address ends in a slash, so PrusaSlicer resolves it
to `<mount>/account/api/v1/me/avatar.png?i=AS&c=fa6831`. The picture is an
opaque square of 128 by 128 pixels in the colour `c`, with the initials `i` in
white, and PrusaSlicer rounds it.
The initials are the first letters of the first two words of the person's
name, and the colour comes from the person's id, from a list of eight. The
route refuses with 400 any query that is not one or two letters and a colour
of that list. The query alone decides the picture, so the route shows nobody
more than its address says. A name with no letter gets no `avatar_small`.

Step 8 is a staged route: the host writes the file into the module's data
folder. The operator approves the route at install with a limit of 1024 MiB,
and may change the limit on the extension's Uploads tab. Step 7 refuses a file
over that limit with `PAYLOAD_TOO_LARGE`. Without the approval the route is
not staged, the module never gets the file, and step 7 refuses every print
with `BAD_REQUEST` and a reason that asks for the approval.

## Where each shape comes from

| shape | what it follows |
|---|---|
| me reply | the fields PrusaSlicer 3 reads from the Prusa Account me reply |
| printer list | the printer list PrusaSlicer 3 reads from Prusa Connect, with the model names it matches its printer profiles by |
| upload and file | the requests PrusaSlicer 3 sends to register an upload and send the file, and the cloud's replies to them |
| error body | the cloud's refusal shape, `{message, code}` with the extra fields of the error, which PrusaSlicer 3 shows to the person |

## What differs from the cloud

- A request the server refuses before the module runs gets the server's
  problem document, not the cloud's error body. That is 401 for a missing or
  ended credential, 403 from the extension's access check, 413
  `BODY_TOO_LARGE`, 503 `MODULE_UPLOADS_BUSY` and 507 `MODULE_STORAGE_FULL`.
- The core has no print queue. An upload must ask for an immediate print, with
  `set_ready` and the first position. Any other position, or `wait_until`, is
  refused with `BAD_REQUEST` and a reason.
- Prusa Account answers `avatar_small` as the address of a photo the person
  uploaded. This module answers a picture of the initials, at a relative
  address.
- A file is always sent to a printer. An upload without `printer_uuid` is
  refused.
- Step 7 asks the host whether the person may operate the printer, with
  `access.check`, and refuses a person who may not with `FORBIDDEN`, before
  the slicer sends the file. The operator approves that question at install,
  as `checks: operate`. Without it, the print in step 8 is the check.
- Step 7 refuses a printer that is not `READY`, `IDLE`, `FINISHED` or
  `STOPPED` with `BAD_REQUEST` and a reason that names its state.
  `PRINTER_NO_TRASFER` is only for a printer that takes no file.
- The module moves the uploaded file into `print/` of its data folder and
  prints it with `delete_source`, so the server deletes the file once the
  print has settled
  ([ADR-20261123-vvb](../../docs/decisions/20261123-vvb-the-core-deletes-a-file-a-module-hands-it.md)).
  A file whose send failed stays, and the module removes it after a day. It
  sweeps when it starts and every hour.
- A file the printer already holds is printed under a free name, such as
  `benchy[1].bgcode`, and the reply to step 8 names that file.
- The cloud pushes each printer's state to the slicer over MQTT. This module
  runs no broker. In offline mode PrusaSlicer reads the printer list again
  every 30 seconds and takes the state from there. A broker would be one more
  port open on the server, and a port on the server's host receives the
  cookies a browser holds for that host
  ([ADR-20261112-2eb](../../docs/decisions/20261112-2eb-prusaslicer-3-reaches-this-server-through-a-compatibility.md)).

## Settings

The module has no settings. The largest file is the limit the operator
approves for the upload route, on the extension's Uploads tab.

## Commands

| command | does |
|---|---|
| `make -C extensions check EXT=prusaslicer3-connector` | lint the Go half, then run the end-to-end gate below |
| `bash extensions/prusaslicer3-connector/scripts/check.sh` | the end-to-end gate alone |
| `make test` | build, vet and test the Go half, in this folder |

## The end-to-end gate

`scripts/check.sh` builds both halves and checks the typed client against
`openapi.json`. It runs the browser half's checks in Chromium and builds its
Storybook. It packs the release and installs it into a demo server with
`module add`. Then it drives the demo the way
PrusaSlicer 3 does, twice: once over HTTP, and once over HTTPS. For HTTPS it
makes a CA and a certificate for `127.0.0.1` with `openssl`.
`scripts/e2e` is the program that sends the requests. It checks the way through the server and one refusal in each part.
The other refusals are the Go packages' tests. Each run:

1. adds four people: alice views every printer, bob operates every printer,
   carol may observe the XL alone, and dave no printer. Carol and dave are
   also granted operate on the module, as they hold no role
2. signs each one in as the slicer: the pairing request with no credential,
   the person's approval with their session, the collection and me. A device
   code collected twice is refused. Alice's `access_token` then opens a page
   session as the slicer does, and the cookie its answer carries reads the
   printer list and the core's devices alone. The credential in the `auth.access_token` cookie is refused, and
   her bare token still answers me. Every other call sends `access_token` as
   the bearer. For each person the gate resolves `avatar_small` against the
   me address and downloads the PNG with no credential
3. reads the printer list as each person, and holds it to the cloud's shape:
   `model` and `base_model`, and a feeder only on the printer with an MMU
4. sends an upload request as carol for the XL, which she may observe and
   not operate, and reads the cloud's 403 body back before any file is sent
5. prints a 1.5 MiB file on the staged route, waits until the transfer to the
   printer is done, and reads the printer list until it shows `PRINTING`

Everything goes under `bin/prusaslicer3-connector`. The demos take the first free
ports from 18598 (HTTP) and 18643 (HTTPS).
`PRUSASLICER3_CONNECTOR_PORT` and `PRUSASLICER3_CONNECTOR_TLS_PORT` move them. The program sends its HTTP requests itself, because the curl of macOS
speaks no TLS 1.3.
