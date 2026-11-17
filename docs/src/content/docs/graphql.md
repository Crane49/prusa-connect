---
title: GraphQL
description: The server answers GraphQL at /graphql, in the same vocabulary as its REST API. How to send a query and a mutation, what an error looks like, and how to switch the surface on and off.
audience: extend
rewrite:
  describes: current
  implementedBy:
    - packages/graphql
    - sdk/deviceapi
    - sdk/routesapi
---

The server answers GraphQL at `/graphql` once the GraphQL module is installed
and switched on. Post a query there and you get the same devices, jobs and
files the REST API serves, under the same names.

```sh
curl -X POST http://localhost:8080/graphql \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"query":"{ devices { items { id name state } } }"}'
```

```json
{
  "data": {
    "devices": {
      "items": [
        {"id": "p3f2a1c9b", "name": "Core One", "state": {"/state": "PRINTING"}}
      ]
    }
  }
}
```

You can also put the query in the address, which is what a link or a browser
address bar can hold:

```sh
curl -G http://localhost:8080/graphql --data-urlencode 'query={ devices { total } }'
```

## Signing in

The same way as the REST API. Send the session cookie the browser already has,
or an `Authorization: Bearer` header. The server works out who you are before
the query runs, and every device the query reads is one you are allowed to see.
Two people running the same query get different answers, because the server
answers each of them for themselves.

A token never goes in the query string.

## What you can ask for

Seven fields. Five read and two change something.

| field | answers |
|---|---|
| `devices(limit, offset, tag, labels)` | one page of the printers you may see, as `items`, `total`, `limit` and `offset` |
| `device(device_id)` | one printer |
| `device_jobs(device_id)` | what a printer is printing now and what it printed last |
| `device_files(device_id, path)` | one directory of a printer's own storage |
| `device_storages(device_id)` | the storage devices a printer reported |
| `send_device_command(device_id, verb, args)` | sends one command to one printer |
| `approve_registration(code)` | lets a printer that is waiting join the server |

Every field is one REST operation, every type is one REST schema, and every
member keeps the name the REST API gives it. `time_remaining_ms` is
`time_remaining_ms` in both. Nothing is renamed on the way through.

The schema is generated from the API document the server publishes. A test
fails when the two stop agreeing, so a field cannot quietly describe an older
version of the API.

## Sending a command

```graphql
mutation {
  send_device_command(
    device_id: "p3f2a1c9b"
    verb: "core.command.set_nozzle_temperature"
    args: {nozzle_temperature: "215"}
  ) {
    ok
    machine_reason
  }
}
```

A printer that took the command answers `ok: true`. A printer that refused
answers `ok: false` with its own words in `machine_reason`. That is an answer
and not an error: the command reached the machine and the machine replied.

## What an error looks like

A refusal goes in the `errors` list, and it carries the same error code the
REST API would have returned.

```json
{
  "data": {"device": null},
  "errors": [
    {
      "message": "deviceapi: no such device: p0000",
      "path": ["device"],
      "extensions": {
        "code": "core.printer/NOT_FOUND",
        "type": "https://beta.prusa3d.com/connect-local/docs/error-codes/#core-printer-not-found",
        "params": {"device_id": "p0000"},
        "retryable": false
      }
    }
  ]
}
```

Branch on `code`. It is the same identifier the REST API puts in its problem
document, so a client that already translates error codes into messages needs
no second set of them. `type` opens the page that explains the code.

The HTTP status is 200 for any query the server could run, even one whose
fields were all refused. That is how GraphQL works. A request that was not a
query at all, such as a body with no `query` in it, answers 400 with a problem
document instead.

## There are no subscriptions

The schema has no `subscription` type and nothing is planned for one. For live
data, read the event stream at `GET /api/v1/events`. See [SSE events](sse-overview.md).

## Switching it on and off

GraphQL runs as a module, out of the server's own process. The shipped
`config/config.yaml` declares it and leaves it off, because no release carries
the module yet. Install the module's release first:

```sh
prusa-connect module add <module path>@<version> -from <release>
```

Then set `enabled` on its resource row in `config/config.yaml` and restart:

```yaml
resources:
  sink:
    graphql:
      enabled: true
```

With `enabled: false`, `/graphql` answers the same way any address the server
does not serve answers.

You can also switch it while the server runs, with no restart:

```sh
curl -X PUT http://localhost:8080/api/v1/resources/sink/graphql \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"enabled": true}'
```

At start the file decides. A row left `false` in `config/config.yaml` comes back
off on the next restart, whatever the running server was last told.

`GET /api/v1/resources/sink/graphql` says whether it is running.

## Reading the schema

The module ships `schema.graphql` beside its binary, with the description of
every field and every member taken from the API document. Any GraphQL client
can also ask the server itself:

```sh
curl -X POST http://localhost:8080/graphql \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"query":"{ __schema { queryType { fields { name } } } }"}'
```

There is no explorer page. Use whichever GraphQL client you already have.

## What is not there

The REST API has 181 operations and this surface answers seven of them. The
missing ones fall into three groups, and none of them is an oversight:

- accounts, grants, resources and modules. A module must not be a second way
  into the settings that decide what the module itself may do
- anything that moves file content. Uploads and downloads go through the REST
  API, which streams the bytes
- anything the module cannot reach. A module reads printers through a private
  channel to the server, and that channel reaches printers alone

For any of those, use the [REST API](reference/api/devices.md).
