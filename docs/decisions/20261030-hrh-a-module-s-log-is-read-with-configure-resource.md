---
title: "ADR-20261030-hrh: A module's log is read with configure_resource"
description: "Where the host keeps the standard error of a service module, who may read it, and why the page shows it without redaction."
date: 2026-10-30
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - extensions
  - modules
  - access
---
# ADR-20261030-hrh: A module's log is read with configure_resource

- status: accepted
- relates to [ADR-20261015-mde](20261015-mde-a-diagnostics-bundle-is-redacted-in-five-layers.md)
  rule 9, which stays as it is
- date: 2026-10-30

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Relates to [ADR-20261015-mde](20261015-mde-a-diagnostics-bundle-is-redacted-in-five-layers.md) rule 9

An operator who sees a module fail needs to read what it printed. The host
already reads the standard error of every service module. This record rules
where the host keeps it and who may read it.

## rules

1. The host keeps the standard error of a service module per instance, in
   memory. It keeps the newest 200 lines or 64 KiB, whichever is smaller.
2. `GET /api/v1/resources/{kind}/{name}/log` serves those lines.
3. Reading the log takes `configure_resource` on the kind. A caller who may
   see the instance and holds no such grant gets 403, and the answer names the
   grant.
4. A caller who cannot see the instance gets the not-found answer, in the same
   bytes as for a missing instance.
5. The lines are served as the module wrote them, with no redaction. The reader
   already holds the grant that rewrites the settings of the module and starts
   it.
6. Nothing of the log is written to disk. A server restart empties it.

## enforcement

The store tests in `internal/app/resource` cover rules 3 and 4: an operator
without the grant gets 403, and a hidden instance gets the not-found answer.
The handler tests in `internal/transport/http/httpapi` cover the route of
rule 2. The host tests in `internal/app/resourcehost` cover the bounds of
rule 1.

## rejected

| option | why not |
|---|---|
| redact the lines as the diagnostics bundle does | a bundle leaves the machine, and this page does not. The reader can already run the module |
| `view_resource` for the log | a module can print a secret from its settings, and a viewer may not read those settings |
| keep the log on disk | the log serves a failure in progress, and a file needs a size bound and a cleanup of its own |
