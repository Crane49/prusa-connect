---
title: "ADR-20261008-hna: an operator names the module of a local release"
description: "core.modules_local maps a module path to a release directory on this server, and the Extensions page offers each as a local install."
date: 2026-10-08
status: accepted
tags:
  - adr
  - extensions
  - api
---
# ADR-20261008-hna: an operator names the module of a local release

- status: accepted
- date: 2026-10-08

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | ADRs: [20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md), [20261005-64j](20261005-64j-a-deployment-may-read-a-module-catalogue-from-a-marketplace.md)

A built release directory holds `module.yaml`, the bundle and, for a service
module, its binary. Its manifest carries no module path: the namespace is
derived from the path an installer names, so a directory alone cannot say what
it installs as. Offering one on the Extensions page therefore needs the
operator to name the path.

## rules

1. `core.modules_local` maps a module path to a release directory on this
   server. Each key must derive a namespace, and each directory must be
   non-empty; the configuration is refused otherwise.
2. The page's module index lists one row per entry, with the facts of the
   directory's `module.yaml`, in module path order. It is read on each listing;
   nothing watches the directory.
   - **Amended by [ADR-20261019-h76](20261019-h76-an-operator-may-switch-installed-modules-off.md) rule 6.** While
     `core.modules_enabled` is `false` the server reads no directory and
     lists no row.
3. A row from `index.json` for the same module path wins over the entry, and the
   entry wins over a marketplace row for it, as an `index.json` row does.
4. A directory that cannot be read is skipped and logged. One bad entry does
   not empty the listing.
5. Installing is unchanged: `from` is the directory, and the digest is recorded
   as for any local install.

## consequences

- A rebuilt directory with a higher version shows as an update.
- The server scans nothing, so no module appears that the operator did not
  name.
