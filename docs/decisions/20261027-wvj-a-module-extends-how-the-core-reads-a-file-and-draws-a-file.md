---
title: "ADR-20261027-wvj: a module extends how the core reads a file and draws a file page"
description: "A module reads files of the types it names under file_analysis for the core, with a read handle for that one file and a fallback to the core's own reader. A module reads a large file fast through storage.fetch. A module draws the body of a file's page in the file-view slot, and an entry of a file slot can name file_types. A bundle hands a component to another bundle under ui.exports and ui.imports."
date: 2026-10-27
status: accepted
supersedes: []
amends: [ADR-20260929-nqd, ADR-20260911-4p6]
tags:
  - adr
  - modules
  - extensibility
  - sdk
---
# ADR-20261027-wvj: a module extends how the core reads a file and draws a file page

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends: [ADR-20260929-nqd](20260929-nqd-the-module-slots-come-from-one-registry.md), [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md) rule 18 | Related: [ADR-20260827-7jn](20260827-7jn-the-plugin-wire-is-a-specification.md)

The core reads a print file itself. It finds the picture and what a printer
needs to print it, such as the nozzle and the filament. A richer format, such
as a 3MF project, needs more than that. Its plates, objects and settings are
the work of a module, not of the core. That module must be able to do four
things. It must answer the core's questions about a file of its type. It must
read a large file quickly. It must draw the body of that file's page. And
another module must be able to draw its components, such as a project view
inside a planning page.

None of this is about one format. The core names no format in these
mechanisms. A module names the file types it covers.

## rules

### the core asks a module about a file

1. A module that reads files for the core names the file types under
   `file_analysis.types` in its manifest. A file type is a lowercase
   extension with its dot, such as `.3mf`. A storage reports no media type,
   so a file is known by its name. The install records the list in the
   declaration.
2. The core asks the module's process, so a module with `file_analysis` runs
   one. The core publishes the call in `sdk/fileapi`.
3. Several modules may read files for the core, each for its own types. For
   one file the host asks the one serving module whose types match the
   file's name. Rules 25 to 28 keep it to one.
4. The host calls `file.analyze` on that module. The params are a read
   handle and the file's name, size and modification time. The
   answer is an `Analysis`: a title, up to four pictures, what a printer needs
   in the `storageapi.PrintInfo` shape, a list of facts and a list of objects.
   `sdk/fileapi` publishes the bounds, and both sides hold the answer to
   them. An answer over a bound is a failure.

### the read handle

5. A read handle is a random token the host makes for one call. It names one
   file of one storage, at the size and the modification time the host saw,
   and one module instance. It ends when the call ends.
6. `storage.stat`, `storage.read` and `storage.fetch` take a `handle` in place
   of a storage, a path and a principal. With a handle the host checks no
   permission. It reads that one file only, for that instance only. Any other
   handle, an ended handle, and a file that changed since the handle was
   made are refused with `ErrNoFile`.
7. So a module needs no `read_storage` to answer the core. It cannot list a
   storage or read another file through a handle.

### the answer, the cache and the fallback

8. The host keeps each answer in memory, keyed by the storage, the path, the
   size, the modification time, the module's namespace and its release. A new
   file version or a new release of the module is a new key. A failure is
   kept too. A call that ran out of time is not kept.
9. Two callers asking for one key wait for one call. At most two calls run
   at once for one module.
10. The listing asks for the picture and waits two seconds. A file's details
    and what a printer needs wait twenty seconds. The call itself runs for up
    to twenty seconds either way. A caller that stops waiting gets the core's
    own reader. The module's answer, when it arrives, is kept for the next
    caller.
11. When no module reads the type, when the module is not serving, refuses,
    fails, answers out of bounds or runs out of time, the core reads the file
    itself. A person sees no error. The file entry names the module that
    answered under `analysis.provider`, and has no `analysis` when the core
    answered.
12. A module's answer replaces the core's own. Its picture is the file's
    picture, its `print` facts are the details and the needs, and its title,
    facts and objects are under `analysis`. The core decodes the picture
    again before it serves it, as it does for its own reader.

### reading a large file

13. `storage.fetch` sends one range of a file as data frames on the stream of
    the call, at most 16 MiB a call. Its params are those of `storage.read`.
    The answer comes after the bytes. It carries the size of the file and the
    number of bytes sent.
14. The SDK reads a file through `storageapi.Client.Open`. It answers a
    `File` with `ReadAt` and `Size`. A `File` is an `io.ReaderAt`, so a module
    hands it to `archive/zip` as it is.
15. `storage.read` stays, for a small read that fits in one control frame.

### the file page

16. `file-view` is a slot on the storage screen, in the area `file-body`. It
    is a section, handed the file reference. It is drawn in place of the
    part of a file's page that says what the file holds. The core keeps the
    header, the actions, the print button, the file's facts and its prints.
17. An entry of a slot handed a file, which is `file-view`, `file-action` and
    `print-section`, may name `file_types`. The entry is shown only for a
    file of one of those types. An entry with no `file_types` is shown for
    every file. A slot handed something else refuses the key.
18. The page draws each state of a view. No view for the file's type draws
    the core's part. A view whose bundle is still loading draws a
    placeholder. A bundle that was refused, threw, or loaded without
    registering the view draws the core's part, with one line saying the
    module's view is not available. A registered view draws the view.

### one bundle draws another bundle's component

19. A bundle names the components it hands other bundles under `ui.exports`,
    each by a `name`. A component's props are part of what the module
    publishes, and follow its version as its routes do.
20. A bundle names the components it draws under `ui.imports`. Each entry is
    the `module` that exports it, by namespace, and the `name`. The module
    names that module under `require` or `suggest`, which gives the
    releases it works with, and a manifest that does not is refused.
21. The exporting bundle calls `host.register.component(name, component)`.
    A name its manifest does not export is refused.
22. The importing bundle calls `host.components.load(module, name)`. The host
    checks that the import is declared, and that the exporting module is in
    the listing and exports that name. It loads the exporting bundle if it
    has not run, and answers the component once the bundle registered it.
    Anything else rejects with `ComponentUnavailable`, whose `reason` says
    which check failed.
23. The component runs in the one Svelte runtime of the page, with the props
    and the callbacks the importing bundle passes. It reaches the server
    with the person's session, as every bundle does. Importing grants no
    permission.
24. `ModuleComponent` in the library draws a placeholder while the component
    loads, the component once it is there, and the importing bundle's
    fallback when it is not available.

### two modules never claim one file type

25. Two enabled modules never read files of one type for the core, and two
    enabled modules never draw a file view for one type. A module may read
    a type another one draws. A file-view entry with no `file_types` draws
    for every file, so it shares every type with any other file view.
26. An install, an update and switching a module on are refused with
    `core.resource/MODULE_FILE_TYPE_CONFLICT` (409) when the change would
    break rule 25. The detail names both modules, the use and the shared
    types. A module installed switched off claims nothing until it is
    switched on. A conflict that was there before the change, which a file
    the operator wrote can hold, does not refuse a change to a third module.
27. The install records the types under `module.file_types` and
    `module.view_types` in the declaration. An index row or a marketplace
    release may carry `file_types` and `view_types`. The install plan lists
    under `conflicts` each enabled module that the release, or a module it
    adds, would conflict with. The Extensions page shows them before the
    install, and the refusal after it.
28. When a file the operator wrote declares two enabled modules that read
    one type, the host asks neither, logs it, and the core reads the file
    itself.

## enforcement

| rule | gate |
|---|---|
| 1, 2 | `TestAModuleReadsFilesOfTheTypesItNames`, `TestAFileAnalysisBlockIsHeldToItsShape` in `sdk/manifest` |
| 4 | `TestAnAnalysisIsHeldToItsBounds` in `sdk/fileapi` |
| 5 to 7 | `TestAHandleReadsOneFileForOneInstance` in `internal/app` |
| 3, 8 to 12 | the analysis tests in `internal/app`, such as `TestAModuleAnalysesAFileForTheCore` |
| 13, 14 | `TestFetchReadsAFileThroughAStream` in `sdk/storageapi` |
| 16, 17 | `TestASlotHandedAFileTakesFileTypes` in `sdk/manifest` |
| 18 | `file-detail-screen.svelte.test.ts` and the stories of the file page |
| 19, 20 | `TestABundleExportsAndImportsComponents`, `TestAComponentEntryIsHeldToItsShape` in `sdk/manifest` |
| 21 to 24 | `components.svelte.test.ts` in `packages/svelte/src/slots` |
| 25, 26 | `TestSwitchingOnAModuleThatReadsATypeAnotherReadsIsRefused` and two cases beside it in `internal/app/resource` |
| 27 | `TestAPlanNamesTheModulesAReleaseConflictsWith` in `internal/app` |
| 28 | `TestTwoModulesThatReadOneTypeAreNeitherAsked` in `internal/app/resourcehost` |

## rejected

| option | why not |
|---|---|
| the module asks for `read_storage` to answer the core | it could then read every file of every storage, for a question about one |
| the bytes of `storage.read` as base64 in bigger pieces | a control frame is at most 1 MiB, and base64 costs a third more |
| a component in an iframe | a renderer per frame on a small computer, and no shared theme or library |
| a custom element per component | the element loses the shared library and its tokens |
| an import map entry per module | the map is fixed when the page loads, so an install would need a reload |
| a component exported from the core library | the core would then carry a module's API in its own versions |
| the module whose namespace sorts first answers a shared type | nobody chose it, and a module added later takes a type over |
| the file view beside the core's part, not in its place | a project view and the core's facts would say the same thing twice |

## open items

- a listing does not redraw a picture when a late answer from a module
  arrives. The next view of the page shows it
