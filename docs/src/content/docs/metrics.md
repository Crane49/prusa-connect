---
title: Metrics
description: The Prometheus exposition at GET /metrics, the pcl_ catalog behind it, and the per-printer budget that decides what may carry a printer label.
audience: run
rewrite:
  describes: current
  implementedBy:
    - internal/platform/observability
    - internal/transport/http/httpapi/metrics.go
---

The core serves its Prometheus exposition at `GET /metrics`, in the Prometheus
text format, and in OpenMetrics when the scraper asks for that. It is on the API
port by default, where a scrape signs in, and `core.observability.addr` moves it
to a port of its own that asks for nothing.

## What a scrape needs

| what | value |
|---|---|
| path | `/metrics`, on the API port or on the port `core.observability.addr` names |
| identity | on the API port, the same as any other operation. An unauthenticated scrape gets 401 |
| grant | on the API port, telemetry on every device this server has seen |
| switch | `core.observability.enabled`, read when the server starts. Off, the path answers 404 and names the key |
| build | the `observability` tag. Without it the path answers 404 whatever the key says |

On the API port the endpoint is authorized, because the exposition names every
printer this deployment has seen and counts them. The printer listing refuses
that same fact to a caller holding no grant. Serving it openly there would
answer through the scrape path what the API answers carefully.

A build with no metrics client answers 404 rather than an empty 200, and so
does a build with the client while `core.observability.enabled` is false. An
empty exposition and a healthy process with nothing to report look the same,
so a scraper would alert on the wrong thing.

Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: 'prusa-connect'
    scrape_interval: 15s
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
```

## A port of its own

A collector running beside this server usually holds no account on it, so it can
hold no grant either. Minting a credential for one is a secret to store, to
render into the deployment and to rotate, and its expiry shows up as a scrape
target failing 401.

Set `core.observability.addr` and the exposition moves to that port instead:

```yaml
core:
  observability:
    enabled: true
    addr: ":9090"
```

The port asks for no credential. Publish it to your collector and to nothing
else, the way etcd, CoreDNS and ingress-nginx are scraped. On Kubernetes that is
a `ClusterIP` Service the ingress does not route to; the container port itself is
reachable from any pod in the cluster unless a NetworkPolicy says otherwise.

Three things follow from setting it:

- **The API port answers 404 at `/metrics`**, and its OpenAPI document names no
  such operation. One port serves the exposition, never both.
- **That port serves the exposition only.** Every other path on it answers 404,
  so publishing it does not publish this server's API.
- **The labels do not change.** The document is still the whole fleet, one
  series per printer. What moved is the boundary, from a grant to an address.

The server refuses to start when `addr` is set and `core.observability.enabled`
is false, and when the address is one the API listener or a printer listener
already answers on.

## Where the names are defined

Every name starts with `pcl_` and every one is declared in
`internal/platform/observability/catalog.go`. That file is the list, and this
page does not copy it. A copy drifts, and the declaration carries the help text
a scraper already shows you.

| set | holds | grows with |
|---|---|---|
| `Core()` | connections, commands, events, HTTP requests, log lines | drivers, sinks and routes |
| `Fleet()` | one series per printer: up, info, last frame, connected since, errors | the size of the fleet |
| `Resource()` | calls, latency, breaker state and panics per supervised instance | configured instances |

`TestEveryCatalogFamilyHasAWriter` in `internal/app` refuses a family that
nothing in the process records. A name with no producer is worse than a missing
one: an operator points a dashboard at it and the empty panel looks healthy.

## Why only six metrics carry a device label

`DefaultFanoutBudget` is 6, and that is the whole per-device allowance. A
metric carrying `device_id` costs one series per device, so the seventh metric
is the next series per machine in the fleet. A deployment that wants one raises
the budget on purpose.

Every metric in `Fleet()` is a counter or a gauge for the same reason. The same
question asked with a histogram is one series per device per bucket, so a
histogram may not carry `device_id` at all.

## What the retired server published

Nothing. The overlap between the two name sets is zero. The `serve` server
published `websocket_*`, `api_*`, `queue_*` and `commands_*` under its own
`observability:` config block, and the core reads none of that block. A
dashboard built against those names draws nothing once the core is what runs.

`internal/arch/testdata/legacy-metric-names.tsv` captures the 18 legacy names,
their instruments, their labels, and which of them ever reached a scrape. It
was read from a real run and not from the source. `docs/CHANGELOG.md` records
the change as a loss and not as a migration.
