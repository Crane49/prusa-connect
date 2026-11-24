---
title: "ADR-20261124-3wt: a storage upload takes the bound an operator sets and an upgrade bundle keeps its own"
description: "The largest file a storage takes is core.api.max_upload_size, and an offline upgrade bundle keeps a fixed bound of its own."
date: 2026-11-24
status: proposed
supersedes: []
amends: [ADR-20261025-4z5]
tags:
  - adr
---

# ADR-20261124-3wt: a storage upload takes the bound an operator sets and an upgrade bundle keeps its own

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20261025-4z5](20261025-4z5-core-upgrades-are-transactional-across-binaries-and-data.md)

The largest file a storage takes was one number, 200 MiB, in the core and in
the web app. A self-hosted server prints files larger than that, and has no
proxy whose limit the number had to match. This record makes the storage
bound a key and keeps the upgrade bundle off it. It replaces the second
sentence of ADR-20261025-4z5 rule 22, which gave a bundle "the upload bound
of every other file upload".

## rules

1. `core.api.max_upload_size` is the largest file, in bytes, one upload to a
   storage takes. It defaults to 200 MiB and is at least 1 MiB and less than
   4 GiB, the first length a transfer to a printer cannot carry.
2. An offline upgrade bundle has a fixed bound of 200 MiB and the sliding
   upload deadline of every other file upload. The key does not move it, so
   an operator who lowers the key can still upgrade.
3. A storage upload or a bundle whose `Content-Length` is over its bound is
   refused before the body is read. A storage upload is refused with
   `UPLOAD_TOO_LARGE`, which names the key, and a bundle with
   `BODY_TOO_LARGE`. A body of no stated length is refused when it crosses
   the bound, with the same code.
4. `GET /api/v1/ui` reports the storage bound as `max_upload_bytes`. A page
   refuses a larger file before it sends it, and sends any file while it
   does not know the bound.

## enforcement

| rule | gate |
|---|---|
| 1 | `TestValidateNamesTheKeyItRefused` in `internal/app/config` |
| 1 | `TestTheUploadBoundTakesItsRangeAndDefaults` in `internal/app/config` |
| 2 | `TestALoweredUploadBoundStillTakesABundle` in `httpapi` |
| 3 | `TestAnUploadDeclaredOverTheBoundIsRefusedUnread` in `httpapi` |
| 3 | `TestAnUploadOfNoDeclaredLengthIsRefusedAtTheBound` in `httpapi` |
| 3 | `TestAnOfflineBundleDeclaredOverItsBoundIsRefusedUnread` in `httpapi` |
| 4 | `TestTheUISettingsReportTheUploadBound` in `httpapi` |
| 4 | the storage store tests in `packages/svelte` |
| 4 | `files.test.ts` in the parts module |

## rejected

| option | why not |
|---|---|
| a bundle takes the larger of 200 MiB and the key | see [a bundle on the key](#a-bundle-on-the-key) |
| one bound for storage uploads and bundles, with a floor on the key | a floor high enough for a bundle stops a lower storage bound |
| a bound with no key, raised for everybody | a person behind a proxy waits out an upload the proxy refuses |

### a bundle on the key

A release binary is 51 to 57 MiB, and the binary budget gate keeps it there.
A bundle has room under 200 MiB, so tying it to the key buys nothing.
