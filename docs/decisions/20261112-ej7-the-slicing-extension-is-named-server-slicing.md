---
title: "ADR-20261112-ej7: The slicing extension is named server-slicing"
description: "The extension that slices 3MF projects on the server is named server-slicing, in its folder, its module path and its error codes, so its name says what it does next to the prusaslicer3-connector module."
date: 2026-11-12
status: proposed
supersedes: []
amends: [ADR-20261026-ydf]
tags:
  - adr
  - modules
  - extensions
---
# ADR-20261112-ej7: The slicing extension is named server-slicing

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20261026-ydf](20261026-ydf-the-slicer-extension-slices-with-the-public-prusaslicer.md), the folder it names and the prefix of every error code in its rules

A module named `prusaslicer3-connector` is planned. It lets the desktop
PrusaSlicer 3 talk to this server. Next to it, an extension named `slicer`
looks like the slicer program, or the link to one. The extension slices
projects on this server. It runs PrusaSlicer today and may run other
slicers, such as OrcaSlicer, later. So its name says what it does, and
the program it runs is named in its summary.

## rules

1. The extension is in `extensions/server-slicing`. Its manifest `name` is
   `server-slicing`, its title is Server slicing, and its module path is
   `github.com/prusa3d/server-slicing`. Its namespace and its
   marketplace id are `com.github.aux.prusa3d.server-slicing`.
2. Every error code of the extension starts with `server-slicing/`. Where
   ADR-20261026-ydf names a code that starts with `slicer/`, the code is the
   same name with the `server-slicing/` prefix, such as
   `server-slicing/UNVERIFIED` for rule 7.
3. Where ADR-20261026-ydf names `extensions/slicer`, it means
   `extensions/server-slicing`.
4. The sidebar page of the extension is titled Slicing. Its path stays
   `slicer`.
5. The new namespace makes the extension a new module for a server that has
   the old one. Such a server installs it again, with a new data folder, and
   removes the old module. The marketplace takes the new id only with its
   own trusted publisher, as `extensions/README.md` describes.

## enforcement

- Rule 1: `TestTheManifestNamesWhatTheCodeIs` in `extensions/server-slicing`
  holds the manifest to the name in `main.go`. `scripts/check.sh` there
  installs the module under the new module path and reaches it under the new
  namespace.
- Rule 2: `openapi_test.go` in `extensions/server-slicing` holds every code
  the routes answer with to the codes `openapi.json` lists, and the gate
  above checks a refusal by its `server-slicing/` code.
- Rules 3, 4 and 5 have no gate. They name a folder, a page title and what
  an operator does.

## rejected

| option | why not |
|---|---|
| keep the name `slicer` | next to `prusaslicer3-connector`, it looks like the slicer program |
| `prusaslicer` | the extension may run other slicers later |
| `slicing` | it does not say that the slicing runs on this server, which tells it apart from the desktop slicer |
| keep the `slicer/` prefix on the error codes | a code then names a module that has another name |
| keep the old module path under the new folder | the namespace comes from the module path, and a path that differs from the folder misleads a reader of the code |
