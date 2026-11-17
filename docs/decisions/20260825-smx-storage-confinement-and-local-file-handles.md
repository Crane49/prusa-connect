---
title: "ADR-20260825-smx: storage confinement and local file handles"
description: "A storage provider resolves every caller-supplied name through an os.Root and hands back open handles instead of paths, and the three escapes that remain are named rather than claimed closed."
date: 2026-08-25
status: accepted
tags:
  - adr
  - storage
  - security
---
# ADR-20260825-smx: storage confinement and local file handles

- status: accepted
- date: 2026-08-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Proposal decisions: S1 to S6

The contract used to hand a caller a path. `LocalFilePath` checked a name,
returned the string, and the caller opened that string a second time, following
whatever had been put there in between. The check and the use were two
separate resolutions of the same name, which is the shape of every
time-of-check-to-time-of-use hole.

The replacement is not a better check. It is handing back the object: the
provider resolves the name under a directory handle and opens it in the same
step, so there is no name left for anybody to redirect. A compiled-in extension
runs in the host's own address space with the host's uid, and this is the only
thing that bounds where it can reach.

## rules

1. A storage provider resolves every caller-supplied name through an `*os.Root`
   opened on its store. Not `filepath.Join` onto a root, not a prefix check
   afterwards: neither can see a link.
2. The root handle is opened per operation and checked against the identity the
   provider recorded at construction. A store path that no longer leads to that
   directory is refused, not followed.
3. A name that does not resolve inside the store reports `ErrInvalidPath`, the
   same error a name that is simply absent reports. Reporting them apart would
   tell a caller which names in a store point out of it.
4. The contract hands back open handles, never paths. `OpenLocal` returns a
   `LocalFile` and `CreateLocal` returns a `LocalWriter`. No method returns a
   path for a caller to open.
5. `LocalWriter.Name` is for a log line and for `storageapi.RemoveStaged`.
   Opening it re-creates the second resolution rule 4 exists to remove.
6. A caller that holds the provider deletes through the provider.
   `RemoveStaged` is for a caller that kept the name and holds no provider, and
   it bounds the delete to one entry of one opened directory.
7. A write lands under a temporary name in the destination's own directory and
   is renamed into place once the bytes are flushed. The error from `Close` is
   checked, because on FAT and on USB media out-of-space is reported there and
   nowhere else.
8. The upload allowlist is applied by `Save` and by `Rename`. `CreateLocal` does
   not apply it: it stages content the caller has already accepted under a name
   the caller chose.
9. Every provider, ours and a community one, runs
   `storageapitest.RunProviderContract`. A provider declaring Browse and Write
   declares `Subject.Root` or `Subject.NoFilesystemRoot`, so a run cannot skip
   every security case and still count as a pass.
10. Confinement is claimed only where it is enforced. The three escapes under
    `limits` are named in the code that cannot close them and are never
    described as closed.

## limits

`os.Root` confines path resolution. These three are not path resolution, so no
confinement API sees them. Each was reproduced against the shipped provider on
Linux before it was written down.

| escape | what happens | why it is not closed |
|---|---|---|
| hardlink in the store | `Open` on a hardlink whose inode lives outside the store reads that file | a hardlink is a second name for one inode, not a step in resolution. Refusing one means refusing every file with `st_nlink > 1`, which is ordinary on a deduplicated or rsync'd tree |
| bind mount in the store | `Open` reads and `Save` overwrites the file the mount exposes | `os.Root` resolves with `openat(O_NOFOLLOW)` per component and never calls `openat2`, so no `RESOLVE_*` flag is available through it. `RESOLVE_NO_XDEV` would refuse the traversal, and reaching it means a raw `openat2` path that exists on Linux only, leaving Windows and macOS on a weaker confinement than the one the contract promises |
| recycled inode at the store path | the store directory is removed and remade, the new directory takes the same inode number, and rule 2's identity check passes | identity is device plus inode, which is what `os.SameFile` compares and the only identity a filesystem offers portably. Closing it needs the handle to be held for the provider's lifetime, which trades this for a store that keeps serving a directory an operator has already replaced, with nothing reported |

These three are assessed against a single-tenant appliance, which is the
deployment earlier research assumes. A multi-tenant host does not get its isolation
from this layer.

## enforcement

| rule | gate |
|---|---|
| 1, 6 | `internal/arch`'s `TestStorageResolvesOnlyThroughARoot`, which refuses a path-taking `os` call in a storage package or in either staged-delete site, against a reviewed allow-list that fails when it goes stale |
| 1, 2, 3 | `storageapitest` `LinkEscape`, `PathEscape` and `legacy/platform/storage`'s `TestLocalProvider_RefusesReplacedRoot` |
| 3 | `PathEscape` requires every returned `FileInfo.Path` to be relative to the store, and `StatMissing` requires absence and escape to share one error |
| 4, 5 | the compiler: `LocalFilePath` is not in the contract |
| 6 | the arch gate above, which is what stops a staged delete returning to `os.Remove`, plus `TestRemoveStagedRefusesANameThatIsNotAnObject` for the guards. `TestRemoveStagedUnlinksALinkRatherThanItsTarget` pins the behaviour and does not discriminate the implementation: `os.Remove` unlinks a leaf link too |
| 7 | `storageapitest` `SaveIsAtomic` |
| 8 | `storageapitest` `ExtensionRejected` and `RenameExtensionRejected` |
| 9 | `TestPathEscapeCatchesAnUnconfinedProvider` runs the published suite in a second process against a provider that resolves with `filepath.Join`, and fails when the suite passes it |
| 9 | `internal/arch`'s `TestEveryStorageProviderRunsTheContractSuite`, which fails a provider that runs no contract suite |

The two rule 9 gates divide the claim. `TestPathEscapeCatchesAnUnconfinedProvider`
proves the suite can catch an unconfined provider, and says nothing about
whether a given provider meets it. `TestEveryStorageProviderRunsTheContractSuite`
finds every directory whose code calls a registrar, so no provider stays
outside the suite.

Every gate above was watched fail on a deliberate mutation before it was trusted
green.

## rejected

| option | why not |
|---|---|
| `filepath.Join` onto a root, then a prefix check | `Clean` folds `..` before anything touches the disk and cannot see a symlink. This is the defect the appendix records as finding 6 |
| `LocalFilePath` returning a validated path | two resolutions of one name with a writable window between them. It is the hole this record exists to close |
| one `*os.Root` held for the provider's lifetime | it closes the recycled-inode window and opens a worse one: a store directory an operator moves or replaces keeps serving the old inode silently, and on Windows the open handle blocks the replacement |
| a raw `openat2` with `RESOLVE_NO_XDEV` | Linux only, and the contract would then promise a confinement two of the three shipping platforms do not have |
| refusing to traverse any symlink above the store root | `/tmp` and `/var` are symlinks on macOS, so every cleanup on a developer machine would fail closed for no attacker |
| `chattr +i` on an empty mountpoint | an operator action on a host, not something a build can gate. It belongs with removable media |
| dropping `LocalWriter.Name` | the connector holds the name across a transfer it does not own, and has no provider available. Removing the field moves the problem rather than solving it |

## open items

- `legacy/connector/xbuddy/connection.go:1034`,
  `legacy/printer/state/printer_state.go:276`, `:1299` and `:1337` hold an
  absolute path across a transfer and delete it with `RemoveStaged`, which bounds
  the delete to one directory entry but resolves the directory itself through the
  operating system. Closing it means handing the connector the provider and the
  relative name, which changes `legacy/connector/connector.go`,
  `legacy/printer/state/transfer_registry.go` and
  `legacy/transport/httpapi/handlers.go`.
- `docs/decisions/README.md` indexes ADR-20260824-kkr to ADR-20260824-dym only.
