---
title: Beta Portal machine update API
description: The versioned minimal JSON contract Prusa Connect expects from a configured online update server.
audience: build
rewrite:
  describes: current
  implementedBy:
    - internal/upgrade/machine.go
---

This page defines the client contract implemented by Prusa Connect and served
by the Beta Portal for entitled testers.

An operator may point `core.upgrade.server` at another implementation. The
shipping default is `https://beta.prusa3d.com`. HTTP is accepted only for a
literal loopback host so an implementation can be exercised with a local test
server.

## Pairing

An organization administrator starts OAuth 2.0 device pairing through
`/api/v1/server/beta-portal-pairing`. Prusa Connect creates a P-256 key. It
keeps the private key, device code and access token in a private, atomically
replaced file, `beta-portal-pairing.json`, in the folder of the file
`core.overlay` names. A file that release 0.0.1-alpha.7 put in `config.d`
beside the configuration file stays in use there. The local API returns only the pairing state, user code,
verification address, expiry and safe account metadata.

The client id is `device-connect-local` and the scope is `device`. Connect
Local sends a fresh DPoP proof with every portal request. Access-token
protected requests carry:

```http
Authorization: DPoP <access-token>
DPoP: <proof>
```

The proof binds the token to the local private key. It includes the request
method and target URI, a unique id, its issue time and the token hash. Connect
Local sends authorization only to the exact origin of `core.upgrade.server`.
Catalogue and artifact redirects must stay on that origin and receive a new
proof for their target URL. A redirect or catalogue artifact URL on another
origin is refused before a request is sent there.

Disconnect first asks the portal to revoke the token and then clears the local
token and rotates the key. If the portal is unavailable, Prusa Connect still
clears the local authorization and reports that remote revocation was not
confirmed. An administrator can then remove the device in the portal.

## List releases

```http
GET <base>/api/connect-local/v1/core/releases?current=1.2.3&channel=stable&platform=linux_amd64&asset=prusa_connect_amd64
Accept: application/json
Authorization: DPoP <access-token>
DPoP: <proof>
```

The query members are:

- `current`: the running semantic version
- `channel`: `stable` or `beta`; the Beta Portal uses `beta` for eligible
  alpha and beta pre-releases
- `platform`: the Go operating system and architecture separated by `_`
- `asset`: the exact executable filename this build can run

A successful response is a JSON document no larger than 4 MiB:

```json
{
  "schema": 1,
  "releases": [
    {
      "version": "1.3.0",
      "name": "Prusa Connect 1.3.0",
      "notes": "Release notes.",
      "published_at": "2026-10-27T12:00:00Z",
      "prerelease": false,
      "binary": {
        "name": "prusa_connect_amd64",
        "url": "/api/connect-local/v1/core/artifacts/1.3.0/prusa_connect_amd64",
        "size": 73400320
      },
      "sums": {
        "name": "SHA256SUMS",
        "url": "/api/connect-local/v1/core/artifacts/1.3.0/SHA256SUMS",
        "size": 512
      },
      "signature": {
        "name": "SHA256SUMS.sig",
        "url": "/api/connect-local/v1/core/artifacts/1.3.0/SHA256SUMS.sig",
        "size": 128
      }
    }
  ]
}
```

`schema` must be `1`. Each accepted release has the requested binary, a
checksum manifest and its detached signature. Artifact URLs may be relative to
the configured base URL or absolute on the same origin. The client applies its
own semantic-version and channel policy even when the service already filtered
the answer.

## Download an artifact

Prusa Connect sends `GET` to each accepted artifact URL with
`Accept: application/octet-stream` and a fresh proof for that URL. A successful
answer is `200 OK`. The declared size and bytes read must both fit the client's
download limit. Catalogue reads time out after 30 seconds; artifact reads time
out after 10 minutes.

No response body from an error is included in a client-facing error or log.
The service may use ordinary HTTP status codes; this version of the contract
does not define an error JSON shape.

The client implementation lives in `internal/upgrade/machine.go`.
