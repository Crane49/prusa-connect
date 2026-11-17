---
title: The API stays compatible
description: A change that breaks the HTTP API is caught by a check before it merges.
audience: build
rewrite:
  describes: current
  implementedBy:
    - tools/apicompat
    - internal/transport/http/httpapi
---

The HTTP API is described by an OpenAPI document that is generated from the
code. A check compares that document with a recorded baseline and names
every change as additive, neutral or breaking.

Every action the server can take is reachable through the API. The web app
talks to the same API as any other client.

## Why is it this way?

People build integrations on the API. A field that disappears breaks them
without warning. A check that reads the whole surface notices what a
reviewer would miss.

## What does it mean for my change?

Add fields and routes. Do not rename or remove one that is published. Run
`make api-check` and read what it reports.

If a feature works in the web app, it works over HTTP. A command line tool
or an MCP tool can sit beside the API and never replaces it.
