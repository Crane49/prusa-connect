---
title: Storage
description: The server-side storages the core serves at /api/v1/storages, the sdk contract a provider implements, and the storage devices a printer reports.
audience: run
rewrite:
  describes: current
  implementedBy:
    - internal/transport/http/httpapi/storagefiles.go
    - sdk/storageapi
    - internal/connector/xbuddy
---

"Storage" here covers two separate things. Keep them apart.

| | Lives on | What it is |
|---|---|---|
| **The server's own storages** | the machine running the server | files the server keeps on its own disk, which you browse over the API and send to any printer |
| **A printer's storage devices** | the printer | the mounts a printer reports, a USB stick or its internal disk. They form the first segment of every printer file path |

## The server's own storages

A storage is an instance under `resources.storage`, keyed by a name you
choose, like every other resource. The setup wizard declares one,
`storage:local`, for the files you upload. You can declare as many as you
like, in your file or in the web app, and each one is a place you upload to
and print from.

```yaml
resources:
  storage:
    local:
      type: filesystem
      config:
        root: ./storage
```

Every type that ships keeps files in a folder on the computer that runs the
server:

| Type | What it is |
|---|---|
| `filesystem` | a folder you keep files in, and the one the wizard declares. Created when missing, readable only by the server's user |
| `local` | the same, with a label of its own for the page, in `config.name` |
| `temporary` | the same, in a folder the system is free to clean, for files nobody expects to keep |

[Extensions](./extensions.md#storage-backends) lists the settings of each
type.

### A folder on a NAS or another disk

A storage reads and writes a folder through the operating system. The folder
can be anywhere the system mounts: a second disk, or a network share from a
NAS over SMB or NFS. Mount the share first, so it is there before the
server starts, and give the mounted folder as `root`. The server's user needs
to write there. The setup wizard checks that it can, before you go on.

### More providers are coming

A storage that is not a folder, such as Amazon S3, Google Drive or Nextcloud,
needs a provider of its own. None ships yet, and they are planned. Until then,
a provider can be written against the published contract: see
[Writing a storage provider](#writing-a-storage-provider).

### The HTTP surface

Every route is under `/api/v1/storages`, and `{storage}` names one storage.

| Method and path | Does |
|---|---|
| `GET /api/v1/storages` | list the storages and what each one's type can do |
| `GET /api/v1/storages/{storage}/files` | list one directory |
| `GET /api/v1/storages/{storage}/content` | download a file, with range requests |
| `PUT /api/v1/storages/{storage}/content` | upload a file |
| `DELETE /api/v1/storages/{storage}/files` | remove one entry, a directory recursively |
| `POST /api/v1/storages/{storage}/deletions` | remove several entries in one request |
| `POST /api/v1/storages/{storage}/folders` | make a folder |
| `POST /api/v1/storages/{storage}/moves` | move or rename an entry |

The interactive reference at `/docs` on a running server carries the parameters
and the error codes for each.

### Upload allowlist

Each storage carries an `allowed_extensions` allowlist, applied on upload and on
rename.

| The key is | Then |
|---|---|
| omitted | the default list applies: `.gcode .bgcode .gco .g .stl .3mf .obj .bbf .png .jpg .jpeg .webp .gif` |
| `[]`, an explicit empty list | the allowlist is off and any extension is accepted |
| a list of your own | only those, matched without case, with the leading dot |

### Path safety

Every path a storage handles is relative to that storage's root, uses `/` as the
separator, and never begins with `/`. The empty path is the root.

A provider treats every path as untrusted. A traversal attempt resolves inside
the root and can never reach a host file outside it, and the local provider
refuses to delete, rename or overwrite its own root.

`OpenLocal` and `CreateLocal` exist for the same reason. Each resolves the name
and opens the object in one step, under the provider's own confinement, so the
caller holds the object rather than a name it has to resolve a second time. A
link planted at that name afterwards has nothing left to act on.

### Writing a storage provider

The contract is `sdk/storageapi`, a module of its own, so a provider can live
outside this repository. A provider is Go code built into the server binary.
It cannot be installed as a module into a running server, because the module
protocol carries no storage. A provider implements `storageapi.Provider`: `Name`,
`DisplayName`, `List`, `Stat`, `Open`, `Create`, `Delete`, `DeleteMany`, `Mkdir`
and `Capabilities`. `Rename`, `DownloadURL`, `OpenRange`, `OpenLocal` and
`CreateLocal` are optional interfaces beside it. Read the
declarations in `sdk/storageapi/provider.go` rather than a copy here, which
would go stale.

`Capabilities` returns a `Caps` value, and it is what stops a caller offering an
operation that cannot work.

| Field | True when |
|---|---|
| `Browse` | `List`, `Stat` and `Open` return real data |
| `Write` | `Create`, `Delete`, `DeleteMany` and `Mkdir` change the store |
| `DownloadURL` | a relative path resolves to a URL a client can fetch itself. A download is then answered with a redirect to it |
| `LocalFile` | `OpenLocal` and `CreateLocal` return open, confined handles |

Register a type from an `init()`:

```go
//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() {
    storageapi.Register("mytype", doc(), func(spec storageapi.ProviderSpec) (storageapi.Provider, error) {
        return newMyProvider(spec)
    })
}
```

`doc()` returns an `extension.Doc`: a summary, a description, your version, the
contract version `storageapi.Contract`, and a stability level. It is what the
published reference prints for your type, and a registration without it is
refused.

A `build.yaml` names the core and your provider's module, and
`make compose-build BUILD_FILE=<your build.yaml>` builds a binary that offers
your type. Nothing in the core has to change.
`examples/adapters/storage-memory` is a complete provider with its own
`build.yaml`, and [Writing a driver](./writing-a-driver.md) walks through the
same build for a printer driver. Configuration refuses a `type` the binary
does not carry.

Every provider passes the same published conformance suite,
`storageapitest.RunProviderContract` in `sdk/storageapi/storageapitest`. We run
it against the built-in provider and you run it against yours. Both pass it or
neither ships.

## Printer storage devices

A printer reports its own storage devices in the `storages` array of every
`INFO` event, refreshed from telemetry. Each device becomes a `connectorapi.Storage`.

| Field | Meaning |
|-------|---------|
| `mountpoint` | Firmware path, e.g. `/usb`, `/local` |
| `name` | Mountpoint without the leading slash (`usb`, `local`), see below |
| `type` | Device type reported by firmware (`USB`, `LOCAL`, …) |
| `free_space` | Bytes free |
| `read_only` | Whether writes are rejected |

### Storage-prefixed path namespace

Throughout `prusa-connect`, printer file paths are **relative and storage-prefixed**: the relative namespace is simply the firmware path without its leading slash, so the first path segment names the storage device.

| Firmware (on the wire) | Relative (everywhere else) |
|---|---|
| `/usb` | `usb` |
| `/usb/foo.gcode` | `usb/foo.gcode` |
| `/local/sub/foo.gcode` | `local/sub/foo.gcode` |

Translation happens **only** at the xBuddy connector boundary:

- **Incoming** (`SEND_FILE_INFO`, `FILE_CHANGED`): the leading slash is stripped, so the rest of the codebase stays connector-agnostic.
- **Outgoing** (path-bearing commands): the leading slash is restored. A path whose first segment names a known storage (or the default storage) is prefixed as-is; a **storage-less** path (a bare filename, for example) is resolved under the configured **default storage** (`core.connectors.<name>.config.default_storage`, default `/usb`) so older callers keep working. This fallback also applies before the first `INFO` event has revealed which storages exist.

### Virtual root and the file tree

The per-printer file tree has a **virtual root** whose top-level children are the printer's storage devices, each a folder keyed by its name (`usb`, `local`), carrying the device metadata (mountpoint, type, free space, read-only). The `INFO` event mirrors the reported devices into this tree so the folders exist before any per-storage file listing arrives. Syncing **merges** rather than replaces: a late-arriving `INFO` never wipes a file listing that landed first, and devices no longer reported are dropped.

Loading files requests each reported storage root by name; if no storages are known yet, it falls back to a single root request that the connector resolves to the default storage, so single-storage printers work unchanged.

### Listing a printer's files

A printer on Buddy firmware answers one command at a time, and an MK4S takes 12 to 15 seconds to list a USB root of about 300 files. So the Files tab never waits for the printer. It shows at once what the server already holds for the folder, and the server reads the folder from the printer in the background.

- Rows you already saw show at once, with a small "updating" marker while a new read is on the wire. Nothing jumps when it lands.
- A folder never read yet shows "The printer is reading the USB drive. This can take a while." with the seconds since the read began, and turns into rows by itself when the read lands. A second tab counts from the same moment.
- A read that failed or did not finish shows the error with a Retry button. Retry starts one new read.
- A change you make through this server (send, delete, new folder) shows at once, and the server does not read the whole drive again.
- A change made on the printer's own screen, or by a program that does not use this server, shows the next time you open the folder. The printer does not tell the server about its own changes.
- Right after the printer connects, the server reads the top folder of each drive, so the first visit is quick. For the seconds that takes, a command for a different folder or drive, or a movement, can wait behind that one read. The command button shows that it is waiting, and the printer shows that it is reading the drive. A printer that is printing is never read ahead.

### Read-only devices

An upload whose target path names a device the printer reports as `read_only` is rejected. A storage-less path (resolved to the connector default) and uploads sent before any `INFO` event has arrived pass through.
