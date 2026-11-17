---
title: "ADR-20261026-ydf: the slicer extension slices with the public PrusaSlicer"
description: "extensions/slicer runs the public PrusaSlicer command line as a separate, bounded process, installs it from the GitHub releases or an operator manifest with checked digests, and offers a slicing API whose jobs check the person's permissions when they are taken and again before they read and write."
date: 2026-10-26
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - modules
  - extensions
---
# ADR-20261026-ydf: the slicer extension slices with the public PrusaSlicer

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Related: [ADR-20261026-c7m](20261026-c7m-a-module-writes-a-file-into-a-storage.md), [ADR-20261026-w6a](20261026-w6a-a-3mf-project-is-read-by-a-core-reader-and-one-dialect-per.md)

A person with a 3MF project, or a program on the network such as a printer
or another module, needs G-code without a desktop slicer. The extension under
`extensions/slicer` takes a job, runs the command line of the public
PrusaSlicer on this server, and writes the G-code into one of the server's
storages. Its README describes the routes, the settings and the manifest.
This record holds the rules a change to it must keep.

## rules

### the slicer and where it comes from

1. The module runs only the public PrusaSlicer, as released on
   github.com/prusa3d/PrusaSlicer, or a build an operator made from that
   public source. It never ships a slicer inside its own release.
2. A version is offered from three sources. They are a manifest an operator
   names in `manifest_url`, the releases of github.com/prusa3d/PrusaSlicer
   from the GitHub Releases API, and the executables in `local_binaries`. A build
   in the operator's manifest replaces the GitHub build of the same version
   on the same platform.
3. A manifest build names its version, channel, operating system, processor,
   https address, SHA-256, archive kind and the executable's path inside the
   archive. A manifest with one wrong entry is refused whole.
4. The GitHub releases offered are the versions from 2.7.0 up to, and not
   including, 4.0.0. 2.7.0 is the first release that writes binary G-code
   and the printer metadata the module reads a result from. A version 4 may
   change the command line or the project format. Drafts and versions
   outside the range are not shown. A release in the range without a build
   for this host is shown, greyed out, with the reason, and cannot be
   installed.
5. The build for a host is chosen by the asset's name. macOS takes the
   universal `.dmg`, and Windows x86-64 takes the `.zip`. Linux takes a
   `.tar.bz2` before an AppImage, and GTK3 before GTK2. The executable is found in the unpacked
   build by its name.
6. The release list is read when the module starts and every six hours,
   with the ETag of the copy kept on disk. That copy is offered while GitHub
   is out of reach. An administrator may ask for a new reading at most once a
   minute. A GitHub token is a secret setting, and no route reads it.
7. A download is checked against the SHA-256 that GitHub publishes for the
   asset, or that the manifest names. A download that does not match is
   deleted and never unpacked. A redirect is followed only to another https
   address. A GitHub asset without a published SHA-256 is installed only when
   the administrator's request confirms it with `unverified`, and is refused
   with `slicer/UNVERIFIED` otherwise.
8. An unpacked build is run once with `--help`, and a build whose banner
   names another version than it was offered as is removed.
9. Several versions are installed side by side, each in its own folder. A
   version a job is slicing with is not removed.
10. An operator may name executables already on the machine in
    `local_binaries`. They are listed by the version their banner names, and
    the module never removes them.
11. A final release newer than the default version is announced to the page.
    The module never installs a version by itself.
12. Installing, removing, choosing the default version and asking for a new
    reading of the releases take `configure_resource` on the sink kind. The
    module asks it with `access.check` while the request is open, so a token
    reaches it only with the `administer` scope. A local binary, a manifest
    and a token are settings an operator writes, and no route sets them.

### which slicer a job gets

13. A job that names a version gets that version. A job that names none gets
    the pinned version, else the newest installed final release, else the
    newest prerelease.
14. A project whose 3MF `Application` metadata names PrusaSlicer of major
    version N gets a slicer of major version N or newer, whatever was named or
    pinned. A job that cannot have one fails with `slicer/NO_SLICER`.

### running the slicer

15. The slicer is a separate process in a process group of its own, started
    with an argument list and never through a shell.
16. Its environment holds `PATH`, a locale of `C` and a home in the job's own
    folder. Nothing else of the server's environment reaches it.
17. `--datadir` points it at an empty folder, so it slices with the settings
    saved in the project.
18. A run is bounded by `timeout_minutes`. On Linux and macOS it is also
    bounded by the memory one run may hold, read from the process group twice
    a second. A bound stops the whole group. The host's memory budget sets that
    figure (ADR-20261027-5m4).
19. Its output is read line by line, each line cut at 2 KiB, and only the
    last 60 lines are kept. They are the job's `error.log`.
20. The exit status is not trusted. A run succeeds only when the output file
    exists and is not empty, and its first bytes decide whether it is binary
    or text G-code.
21. The job's folder is deleted when the job ends, whatever the outcome.

### jobs and permissions

22. A job is taken while the person's request is open. The module reads the
    source in the person's name, and asks whether the person may write into
    the target storage.
23. A worker slices the job as the module. Before it reads the source and
    before it writes the G-code, it asks `access.check` whether the person may
    still read and write. A person who lost the permission gets a failed job
    and no file.
24. The G-code reaches the storage through `storage.write`. A file of the same
    name stops the job with `slicer/OUTPUT_EXISTS`, unless the job asks to
    overwrite it.
25. A person sees and cancels their own jobs. A person with
    `configure_resource` on the sink kind sees and cancels every job. Any
    other person gets the same answer as for a job that does not exist.
26. A job that was waiting or running when the module stopped is failed with
    `slicer/INTERRUPTED` at the next start.

### part of a project

27. Slicing a chosen bed, or chosen build items, is a rewrite of the project
    before the slicer runs, with the reader of the threemf module
    (ADR-20261026-w6a, ADR-20261028-jae). This module links that reader as
    a Go package and has no 3MF reader of its own.
28. Every job is rewritten, a whole one too, so the command line slices the
    bed the job names and not one the project saved as chosen.
29. A selection the project does not have fails the job with
    `slicer/SELECTION_INVALID`. A whole job with nothing to print fails with
    `slicer/NOTHING_TO_PRINT` before the slicer runs.
30. A result names the build item and the instance behind each labelled
    object with `Project.MatchLabels` of the threemf reader. A label it cannot
    match carries no item.
31. A project too large for the 3MF reader is sliced as saved when the job
    is whole, with a warning, and fails when the job names a selection.

32. A project is sliced only when the threemf reader detects its PrusaSlicer 2 or
    PrusaSlicer 3 dialect. Any other fails with `slicer/NOT_PRUSASLICER` before a
    slicer runs, and the message names the program that saved it. Another
    slicer keeps its settings and its plates in files PrusaSlicer does not
    read, so its project would slice with settings nobody chose.
    A project too large for the reader is the one exception, as rule 31
    says: nothing in it is read, so nothing names its program.

## enforcement

| rule | gate |
|---|---|
| 2, 3 | `internal/catalog` tests: a manifest with one wrong entry, the operator's build replacing the GitHub one |
| 4, 5 | `internal/catalog` GitHub tests over a recorded release list: the range, drafts, the asset for each host |
| 6 | `internal/feed` tests over a stand-in API: the ETag, the copy on disk, a refused reading |
| 7, 11 | `internal/slicers` tests: an install without a digest is refused until confirmed, a newer final release is announced |
| 7, 8, 9 | `internal/install` tests: a download that does not match, an archive that climbs out, a build that names another version |
| 12, 25 | `main_test.go`: only an administrator changes the slicers, a person sees their own jobs |
| 13, 14 | `TestTheSlicerForAJob` in `internal/install` |
| 15, 18, 20 | `internal/runner` tests over a stand-in slicer: the bounds, a run that writes nothing, shell characters in a name |
| 22, 23, 24 | `internal/jobs` tests: a person who lost the write, a file that exists, the refusals at submit |
| 26 | `TestAJobRunningAtAStopIsFailed` |
| 27, 28, 29 | `internal/project` tests: the chosen bed and items are the ones bed 0 prints, a selection the project lacks |
| 29 | `TestABedTheProjectDoesNotHaveFailsTheJob` |
| 30 | `TestTheLabelsOfASlicedBedNameTheirItems` |
| 31 | none. Review holds it |
| 32 | `TestAProjectPrusaSlicerDidNotSaveIsRefused`, `TestAProjectOfAnotherSlicerFails` |
| 16, 17, 19, 21 | none. Review holds them |

`TestTheRealSlicerSlicesASmallProject` in `extensions/slicer` runs the real PrusaSlicer 2.9.6 and
3.0.0-alpha12 on macOS. It needs the network, so no gate runs it.

## rejected

| option | why not |
|---|---|
| a list of tested releases carried inside the module | a new PrusaSlicer release would need a new module release |
| offer only releases with a published SHA-256 | every release before mid-2025 has none, and with them every Linux build GitHub published |
| install a newer release by itself | a new version may slice a project differently, so an administrator chooses when |
| install the Linux Flatpak | it needs Flatpak on the server and installs system-wide, outside the module's data directory |
| ship a slicer inside the module's release | a release would be hundreds of megabytes for each platform, and one version only |
| trust the exit status of PrusaSlicer | a bed with nothing on it exits with 0 and writes no file |
| read the project and write the G-code as the person | the person's request has closed by the time a worker runs |
| a route that sets a local binary's path | it would let a token run any program on the server |
| slice part of a project with command-line options | the PrusaSlicer command line slices bed 0 alone and has no option to leave an object out |
| a memory bound through rlimit | Go sets no rlimit on a process it starts, and one set on the module would bound the module too |

## open items

- Windows has no memory bound, and the module's tests have not run the
  Windows builds
- GitHub publishes Linux builds only up to 2.8.1 for x86-64 and 2.7.4 for
  arm64. A Linux server that needs a newer version needs a local binary, or a
  manifest of builds its operator hosts
