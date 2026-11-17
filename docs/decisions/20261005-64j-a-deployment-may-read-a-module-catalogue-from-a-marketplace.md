---
title: "ADR-20261005-64j: a deployment may read a module catalogue from a marketplace"
description: "The Prusa Research marketplace, or the one core.marketplace.url names, is read by default and merged into the module index, and an install the catalogue names is downloaded over https and refused unless its digest matches."
date: 2026-10-05
status: accepted
tags:
  - adr
  - extensions
  - api
---
# ADR-20261005-64j: a deployment may read a module catalogue from a marketplace

- status: accepted
- date: 2026-10-05

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | ADRs: [20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)

Until now the modules a deployment could install came from one file,
`index.json` in the modules directory, which an operator or the demo wrote by
hand. A person who wants a module first has to find its release somewhere,
copy it next to the server and run `module add`. A marketplace lists modules
from many authors, reviews each release before it is published, and can say
which release fits this build and this machine. A module is native code with
no sandbox, so the server must install exactly the bytes the marketplace
reviewed and nothing a caller substituted.

## rules

1. The marketplace is read by default, at `https://plugin-marketplace.prusa3d.com`.
   `core.marketplace.url` names another one. `core.marketplace.enabled: false`
   is the operator's opt-out, and nothing is fetched then. The URL is https; a
   loopback host (`localhost`, `127.0.0.1`, `::1`) may be http, for local
   development.
1a. The server fetches nothing at start and on no timer. It reads the
   catalogue only when the module index is asked for and installs only when an
   administrator asks. A request carries the core version and the platform, and
   no credential and no instance id.
2. The server reads `GET <url>/api/connect/v1/extensions?core=<version>&platform=<goos>_<goarch>`
   with its own version and platform, within `core.marketplace.timeout`, and
   keeps the answer for a short time. The version is the whole version, with
   its prerelease and without its build metadata, and a build whose version is
   not a semantic version leaves `core` out. The index lists a release only
   when the core is in its core range by the rule the install uses. It reads
   version 1 of the marketplace's API, and v1 only grows compatibly; a breaking
   change is a new path. A refresh that fails serves the copy it kept, and a
   marketplace that never answered serves no rows. Neither is an error of
   `GET /api/v1/modules/index`. The catalogue is paged: `next` is a path and a
   query resolved against the page that named it, or a whole address. Either
   way it stays on the marketplace's origin and under the catalogue's path, the
   walk reads at most 20 pages, and it spends one timeout and one byte budget.
3. `GET /api/v1/modules/index` lists the local index rows first, with the
   releases an operator names
   ([ADR-20261008-hna](20261008-hna-an-operator-names-the-module-of-a-local-release.md)), then the
   marketplace rows whose module path no local row names. Each row says its
   `origin`, and an installed module carries `installed_version`.
   - **Amended by [ADR-20261019-h76](20261019-h76-an-operator-may-switch-installed-modules-off.md) rule 6.** While
     `core.modules_enabled` is `false` the index answers `MODULES_DISABLED`
     and reads nothing.
4. `POST /api/v1/modules` with no `from` installs the release the marketplace
   row for that module and version names, from its `source` and against its
   `sha256`. A module or version the catalogue does not offer is refused. A
   caller cannot point the server at another host this way.
5. Every https download is hashed while it is written. A release whose digest
   differs from the one the request or the catalogue states is refused with
   `core.validation/MODULE_DIGEST_MISMATCH` before it is unpacked.
6. A download follows at most five redirects, and only to https or to a
   loopback http address. Plain http to any other host is refused. After the
   name is resolved, a download never connects to a loopback, private,
   link-local, unspecified or multicast address. There is one exception: a
   marketplace configured on loopback over http, for local development.
   Loopback may then be reached on any port, because the marketplace serves
   its releases from a storage beside it. The local network stays shut. The
   download reads no
   proxy from the environment, because a proxy would carry the request past
   this check.
7. Installing a release over an installed module stops the running process
   before its files are replaced, and the instance starts on the new release.
8. A release may carry `checksums.txt.sig`, the author's signature over
   `checksums.txt`. The install does not read it and does not require it to
   be listed.
9. A `from` that is a URL takes `sha256`, because nothing else pins the bytes.
   A path on this server keeps its `checksums.txt` check.
10. An install with `approve` grants nothing the operator was not shown. For a
   marketplace install, the manifest's permissions and events must be among
   the ones the catalogue lists for the release. `check_absent_people` must be
   listed when the manifest asks it. `records_mib` must not exceed the listed
   one. A request may carry `expect`, what the page showed, and the
   manifest is held to it the same way. Anything beyond is refused with
   `MODULE_REQUESTS_CHANGED` (409) before anything is written. An install
   without `approve` grants nothing and is not held.
11. The instance name and the module's directory come from the manifest's
   name. A release whose name a declaration of another type holds, or a module
   of another kind already holds, is refused with `MODULE_NAME_TAKEN` (409),
   from the command and the API alike.
12. An approved install records what the manifest asked in the declaration's
   `module.asked`. An approved update grants each request the declaration
   still holds plus each request the old ask did not have, which the answer
   lists in `added`. A request the operator took away stays away. A
   `records_mib` that differs from the old ask is the operator's, and is kept
   unless the request names one. A declaration with no recorded ask grants
   what the manifest asks.
13. A release names its `channel`, stable or beta. A release with no channel is
   stable. A row offers the newest stable release. The index leaves out a row
   that offers a beta for a module installed from the stable channel. A beta is
   then never offered as an update the operator did not choose. The declaration
   records the channel in `module.channel`.
14. `marketplace.reachable` is true only when the last read succeeded. A copy
   served after a failed refresh is `stale: true`, with `problem` saying why.
15. A download that fails answers `MODULE_DOWNLOAD_FAILED` (502) with one
   sentence whatever happened, so the answer reveals nothing about the address.
   The log says which status or error it was.
16. A row's `page_url` is kept only when it is https, or on the configured
   loopback marketplace's own origin.
17. `PUT /api/v1/modules/marketplace` takes the admin role on the
   organization, because the switch decides where every administrator's
   installs come from. Reading the switch and the index takes
   configure_resource on a resource kind.
    - **Amended by [ADR-20261019-h76](20261019-h76-an-operator-may-switch-installed-modules-off.md) rule 6.** While
      `core.modules_enabled` is `false`, reading and switching the marketplace
      answer `MODULES_DISABLED`.
18. A row says whether its requests are complete in `requests_known`. A
   marketplace row is. A local `index.json` row is when it states any of
   `permissions`, `events`, `check_absent_people` or `records_mib`, or when
   its source is a release directory on this server, whose manifest is read.
   A row pointing at an archive or an address is not, because reading it
   would unpack or download a release on every listing. A page then installs
   without `approve`, reads `requests`, and approves with them as `expect`.

## enforcement

| rule | gate |
|---|---|
| 2, the cache and a failed refresh | `internal/app/marketplace_test.go` |
| 2, the pages | `internal/app/marketplacepages_test.go` |
| 3, 4, 5 | `internal/app/marketplace_test.go` |
| 6, the redirects | `internal/app/modulefetch_test.go` |
| 6, the addresses one dial reaches | `TestADownloadReachesNoLocalAddress` in `internal/app/marketplaceconsent_test.go` |
| 6, loopback beside a local marketplace | `TestLoopbackIsReachedOnlyBesideALocalMarketplace`, same file |
| 7 | `internal/app/moduleupgrade_test.go` and `internal/app/modulefetch_test.go` |
| 9 to 16, 18 | `internal/app/marketplaceconsent_test.go` |
| 17 | `TestOnlyAnOrganizationAdminSwitchesTheMarketplace` in `internal/transport/http/httpapi/modulemarketplace_test.go` |
| the shape of the marketplace's answer | `TestTheMarketplacesDocumentedAnswerIsDecoded`, over `internal/app/testdata/marketplace/` |
| the key, the operations and the fault codes | `make config-reference-check openapi-check api-check error-codes-check` |

## rejected

| option | why not |
|---|---|
| the browser reads the marketplace and posts the URL | the page could then install any URL, and a marketplace on the internet cannot reach a server on a home network anyway |
| fetch the catalogue on every request | a slow marketplace would make the settings screen slow, and it would be asked once per person who opens it |
| verify the author's signature in the core | the core holds no author keys yet; the digest pins the reviewed bytes, and a signature check can follow |
| a separate update verb | an install over an installed module is already an update, and a second verb would duplicate its checks |
| read the manifest of a local archive for the index | it unpacks up to 256 MiB on every listing; the two-step install covers it |
| configure_resource on one kind for the marketplace switch | an administrator of one kind would decide where every other administrator's installs come from |
| only the local marketplace's own port on loopback | its releases come from a storage on another port, so no local install could finish. The operator runs that marketplace |

## open items

- a marketplace on a private network address over https cannot serve downloads, because the address check refuses the local network
- a deployment that reaches the internet only through a proxy cannot download a release
