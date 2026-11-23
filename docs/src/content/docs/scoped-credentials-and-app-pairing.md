---
title: Scoped credentials and app pairing
description: Limit a token to reading, operating or administering, and let an app ask a person for a token with a short code instead of a pasted secret.
audience: run
rewrite:
  describes: current
  implementedBy:
    - internal/authz/ceiling.go
    - internal/platform/auth/apppairing.go
    - internal/app/apppairings.go
    - internal/transport/http/httpapi/apppairings.go
    - internal/transport/http/httpapi/credentials.go
    - internal/platform/auth/jwtshape.go
---

An API credential is a bearer token that a script or an app sends with every
request. Each credential has scopes. A scope limits the token to a part of
what its person may do. A token never reaches more than its person. The
scopes only take away.

An app does not need a person to copy a token into it. It can ask for one with
a short code, and the person approves it in the browser. The flow is the
device authorization grant of RFC 8628, the same one a TV app uses to sign in
to a streaming service.

## The three scopes {#scopes}

| scope | the token may |
|---|---|
| `read` | see printers, prints, files, history and who did what. It changes nothing |
| `operate` | everything `read` may, and start, pause and stop prints, send commands of the operate class, and upload files |
| `administer` | everything its person may, including managing people, access, modules and this server, and the dangerous commands |

Each scope allows what the one before it allows, so a token names one. A
token minted with no scope can only read.

The server checks the scopes on every request, wherever it arrives. That is
the REST API, the event stream and a module's routes. It is also each call a
module makes for you while your request is open. A request that a scope leaves out is refused with
`403` and the code `core.auth/ACCESS_DENIED`, the same answer as a missing
grant.

Some acts take a sign-in as a person, whatever scope a token has: minting a
token, creating an account, writing a pairing and approving an app. A token
limited below `administer` may revoke itself, and no other token.

## Mint a token with a scope {#mint}

In the web app, open **Settings**, then **API credentials**, then
**New credential**. Pick the scope there.

Over the API, sign in and send the scope with the name:

```sh
curl -s -b cookies.txt -X POST http://localhost:8080/api/v1/auth/credentials \
  -H 'content-type: application/json' \
  -d '{"name":"kitchen display","scopes":["read"]}'
```

The answer carries the token once, with its `scopes` and its `origin`, which
is `mint` for a token a person made.

A token minted before scopes existed is limited to `read`. Mint a new one if a
script needs more.

## Pair an app {#pairing}

An app asks, a person approves, and the app collects its token. Nothing
secret is typed or pasted.

### 1. The app asks

The request takes no credential.

```sh
curl -s -X POST http://prusa-connect.local:8080/api/v1/auth/app-pairings \
  -H 'content-type: application/json' \
  -d '{"app_name":"kitchen display","scopes":["read","operate"]}'
```

```json
{
  "device_code": "Qm9vdGg3cGFpcmluZ0NvZGVGb3JUaGVTYW1wbGUwMDAw",
  "user_code": "BCDF-GHJK",
  "verification_uri": "http://prusa-connect.local:8080/pair",
  "verification_uri_complete": "http://prusa-connect.local:8080/pair?code=BCDF-GHJK",
  "expires_in": 600,
  "interval": 5
}
```

Keep `device_code` to yourself. Show `user_code` and `verification_uri` to the
person, or open `verification_uri_complete` for them, or draw it as a QR code.

### 2. The person approves

The person opens the page and signs in. The page shows the app's name, what
the scope it asked for lets it do, and the address it asked from. The person
approves exactly the scopes the app asked for, or denies it. When the link
carries the code, the page shows no field for it. A narrower scope can be
approved only through the API.

The name is what the app says about itself, and nothing checks it. A person
should approve only an app they started themselves.

### 3. The app polls

Poll every `interval` seconds:

```sh
curl -s -X POST http://prusa-connect.local:8080/api/v1/auth/app-pairings/token \
  -H 'content-type: application/json' \
  -d '{"device_code":"Qm9vdGg3cGFpcmluZ0NvZGVGb3JUaGVTYW1wbGUwMDAw"}'
```

| answer | meaning | what to do |
|---|---|---|
| `400` `core.auth/APP_PAIRING_PENDING` | nobody has decided yet | poll again after the interval |
| `400` `core.auth/APP_PAIRING_SLOW_DOWN` | you polled too soon | wait five seconds longer from now on |
| `200` with a `token` and an `access_token` | the person approved | keep the token, and stop polling |
| `400` `core.auth/APP_PAIRING_DENIED` | the person denied it | stop, and tell the person |
| `400` `core.auth/APP_PAIRING_EXPIRED` | ten minutes passed | ask again |
| `400` `core.auth/APP_PAIRING_UNKNOWN` | the code is used or was never issued | ask again |

The token speaks for the person who approved it, with the scope they
approved. It lives ninety days. Its `origin` is `app_pairing`, so the person
sees it as a paired app among their credentials and can revoke it there. When
the token is refused with `401`, ask again.

The answer also carries `access_token`. It is the same token in the shape of a
JWT, three parts separated by dots. The second part names the person in `sub`,
the expiry in `exp` and the token's id in `jti`. The third part is `token`
itself. It is not signed. The server reads only the third part and holds the
first two to the token it names. Send it as `Authorization: Bearer`. The
server never reads it from a cookie.

### A page inside the app {#page-session}

A page the app shows, such as the Connect tab of PrusaSlicer, must never get
the token: a script on the page could send it elsewhere. Before the app
shows a page, it opens a page session with the token:

```sh
curl -s -X POST http://prusa-connect.local:8080/api/v1/auth/app-pairings/session \
  -H 'authorization: Bearer <token or access_token>'
```

The answer sets the session cookie to a page session, and carries the same
cookie in `cookie`: `name`, `value`, `path`, `expires`, `http_only`,
`same_site` and `secure`. An app that does not read `Set-Cookie` puts that
cookie into its web view itself. The cookie is HttpOnly and SameSite=Strict,
so no script reads it and no other site sends it.

A page session speaks for the same person with the same scope. It lives at
most an hour, and ends at once when the person revokes the app. Open a new
one before `expires_at`, or after the page meets a `401`. A token in a
cookie, or any other kind of credential, is refused with
`core.auth/PAGE_SESSION_REFUSED`. One token keeps four live page sessions,
and opens thirty from one address in ten minutes before it gets
`core.auth/PAGE_SESSION_LIMITED`.

### The limits {#limits}

| limit | value |
|---|---|
| how long a pairing waits | ten minutes |
| how often an app polls | every five seconds, longer after a slow down |
| pairings waiting at once on the whole server | 32 |
| pairings one address may ask for | five in ten minutes |

Each step writes one line to the server log at info, with `audit=true`: the
request, the approval or the denial, and the collection.

## List and revoke paired apps {#revoke}

`GET /api/v1/auth/credentials?origin=app_pairing` lists the apps a person
paired. `DELETE /api/v1/auth/credentials/{credential}` revokes one, and the app
is refused on its next request. In the web app, a paired app carries the
**Paired app** badge under **Settings**, **API credentials**.
