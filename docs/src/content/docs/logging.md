---
title: Logging
description: Where the core writes its log, the two formats and four levels it takes, the sampling that keeps a debug loop from filling a disk, and the two counters that make sampling visible.
audience: run
rewrite:
  describes: current
  implementedBy:
    - internal/app/logging.go
    - internal/platform/observability/logging.go
---

The core writes one structured log to stderr. Two keys control it, and there is
nothing else to configure.

```yaml
core:
  log:
    level: info   # debug, info, warn or error
    format: text  # text or json
```

Stderr and not stdout, so a process whose stdout is a pipe still logs somewhere
an operator can see. Docker and Kubernetes collect it as it is, and no rotation
setting is involved.

## Levels

| Level | Use |
|---|---|
| `debug` | detail you turn on to follow one printer |
| `info` | what the process did |
| `warn` | something is wrong and the operation continued |
| `error` | an operation failed |

A level the core does not know is refused at load, naming the key and the value.
There is no `fatal`: a failure that ends the process is an `error` line and a
non-zero exit.

## Formats

`text` is `slog`'s key-value handler and is what a person reads at a terminal.
`json` is one object per line and is what a log collector parses. Nothing else
changes between them.

## Why a debug loop does not fill the disk

Every line passes through a counting handler before it is written, and that
handler samples **below `info` and nowhere else**. Per message per second, the
first 10 lines pass, then one in 100.

A warning or an error is rare by construction and is the line somebody is
reading, so neither is ever sampled. A debug line inside a loop over four
hundred printers is the one that fills a disk, so that is the one sampling
catches. The shape is zap's.

Sampling nobody can see is worse than no sampling, so both halves are counted:
`pcl_log_lines_total` counts what was written and `pcl_log_lines_dropped_total`
counts what sampling discarded, each by level. An operator reading a debug
volume that fell by two orders of magnitude can tell a quiet process from a
sampled one. See [Metrics](./metrics.md).

## Writing a log line

The core has no package-level logger. Every part of it is handed one `*slog.Logger`
by the composition root. Nothing reads
a global and nothing writes one.

Use structured fields, never an interpolated message:

```go
// Good. The fields are searchable, and the message is a constant.
logger.Info("command sent", "printer", printerID, "command", cmdID)

// Bad. Nothing can group these lines, and the sampler cannot count them.
logger.Info(fmt.Sprintf("command %d sent to printer %s", cmdID, printerID))
```

A constant message matters twice over. The sampler keys its per-second table on
the message, so an interpolated one is a new key every line and defeats it.
