---
title: "ADR-20261027-3mk: Release checksums cover the plain bytes of every file"
description: "A release file is hashed as it is, by the release workflow and by the server, and no build carries a space a download server fills."
date: 2026-10-27
status: accepted
supersedes: []
amends: [ADR-20261025-4z5]
tags:
  - adr
---
# ADR-20261027-3mk: Release checksums cover the plain bytes of every file

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

A release publishes `SHA256SUMS`, a list of one SHA-256 digest per file, and a
signature over that list. The server checks a downloaded or uploaded binary
against it before it runs the binary. This record says what the digest covers.
It replaces rule 5 of ADR-20261025-4z5, which kept a space in the binary for a
download identifier outside the digest.

## rules

1. Hash a release file as it is, byte for byte, the same way `sha256sum`
   does. The release workflow, `offline-update-bundle`, staging, verification
   and the identity check before a swap use this one digest.
2. Build no space into a binary that a download server writes after the
   build. A changed byte is a different binary.
3. Report the same fields in `--version --json` for every copy of one
   release.

## enforcement

- `TestARealBinarySignedLikeAReleaseVerifiesAndApplies` signs a real linked
  binary the way the release workflow does, then stages, verifies and applies
  it.
- `TestARealBinaryInAnOfflineBundleKeepsItsIdentity` stages a real binary from
  an offline bundle and checks its identity before a swap.
- `make upgrade-process-e2e` signs each candidate with
  `cmd/sign-upgrade-checksums` over a plain digest and upgrades a running
  server with it.

## rejected

| option | why not |
|---|---|
| Normalize the identifier space before hashing | The marker text is in the binary twice, so the normalized digest never equals the signed one |
| Sign a second, normalized digest beside the plain one | Two digests for one file let the two checks disagree, and the Beta Portal no longer writes an identifier |
