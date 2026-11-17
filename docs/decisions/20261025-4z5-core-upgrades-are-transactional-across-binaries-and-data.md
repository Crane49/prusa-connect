---
title: "ADR-20261025-4z5: Core upgrades are transactional across binaries and data"
description: "The rules that keep an upgrade and its rollback recoverable across every supported deployment."
date: 2026-10-25
status: accepted
supersedes: []
amends: []
tags:
  - adr
---
# ADR-20261025-4z5: Core upgrades are transactional across binaries and data

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

## rules

1. Treat the binary, configuration, database schemas, installed modules, and
   upgrade journal as one upgrade transaction.
2. Write each journal transition atomically before starting the work it names.
3. Resume a prepared upgrade and roll back an interrupted replacement before
   an ordinary server start opens a database.
4. Verify a release checksum with an Ed25519 key trusted by the operator before
   executing any file from that release.
5. Keep the download fingerprint outside the bytes covered by a release
   checksum.
6. Refuse a schema whose compatibility floor is above the version understood
   by this build.
7. Accept ledger rows from a later compatible schema only when their recorded
   version is above every migration shipped by this build.
8. Apply every migration of one database under one lock and in one
   transaction.
9. Rehearse migrations by running that transaction and rolling it back.
10. Take and verify a backup before changing live data or replacing a binary.
11. Restore configuration, databases, modules, and the previous binary
    together when a trial start fails.
12. Keep the previous binary until the replacement passes its readiness gate.
13. Refuse an immediate upgrade while a printer is working unless an
    administrator asks to pause the printers and continue.
14. Let an administrator schedule an upgrade for the first time every printer
    is idle.
15. Block an upgrade that makes a required module incompatible and warn for an
    optional module.
16. Let a writable folder installation replace its own binary with atomic
    renames on one filesystem.
17. Put a self-managed systemd binary below its state directory.
18. Treat containers and printer builds as externally managed: they prepare,
    check, back up, and restore data, while their platform replaces the
    executable.
19. Permit an unsigned release only through an operator-owned configuration
    file, never through the HTTP API.
20. Keep five successful backups by default and let the operator choose
    another directory and retention count.
21. Plan the module releases an upgrade installs with the module resolver, as
    one plan with the core, the way
    [ADR-20261027-e6k](20261027-e6k-a-module-depends-on-a-versioned-contract-and-not-on-another.md)
    rules 28 to 32 say.
22. Take an offline bundle through the API as the request body and verify it
    as it arrives. It has the upload bound and the sliding upload deadline of
    every other file upload, and it is never held in memory whole.

## enforcement

- Migration tests run each ledger against SQLite and Postgres, including two
  concurrent migrators and a rehearsal that leaves no row behind.
- Upgrade state tests interrupt every journal transition and prove that the
  next start reaches either the old committed state or the new committed state.
- Release tests reject a changed checksum, an unknown key, an incompatible
  platform, and an unsigned upload under the shipping configuration.
- API and journey tests cover immediate, forced, scheduled, externally managed,
  successful, and rolled-back upgrades.
- `TestAnOfflineBundleIsStagedFromTheStreamedBody` and
  `TestAnOfflineBundleOverTheUploadBoundIsRefusedAsTooLarge` hold rule 22.

## rejected

| option | why not |
|---|---|
| Down migrations | An older binary cannot read later data. A rollback restores its snapshot. |
| Replace a binary before taking a backup | A failed migration would leave neither version with data it can open. |
| Run an unverified binary to ask for its version | Version output is executable code and crosses the trust boundary too early. |
| Let the server control Docker or the printer operating system | A Docker socket grants host control, and the printer updater owns its executable and rollback slot. |
| Keep a permanent supervisor process | Printer builds cannot afford the second resident process, and a trial parent is needed only during replacement. |
