---
title: "ADR-20261023-7r3: Fractal files are shared folders fetched over HTTPS"
description: "The Fractal module implements the file distribution extension of 2026-10-20: a server publishes the folders a person shares, a node fetches a file from its parent over HTTPS with a signed proof, keeps the copy, and prints a fractal URL. The core still runs without Fractal."
date: 2026-10-23
status: accepted
supersedes: []
amends: [ADR-20261020-tca, ADR-20261019-3ns]
tags:
  - adr
  - federation
  - fractal
  - storage
---
# ADR-20261023-7r3: Fractal files are shared folders fetched over HTTPS

- status: accepted
- date: 2026-10-23

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20261020-tca](20261020-tca-a-fractal-node-keeps-a-shadow-of-the-nodes-above-it.md) rule 22, [ADR-20261019-3ns](20261019-3ns-a-module-declares-its-ports-and-gets-a-data-directory.md) rule 4 | Builds on [ADR-20261020-tca](20261020-tca-a-fractal-node-keeps-a-shadow-of-the-nodes-above-it.md), [ADR-20260921-wkf](20260921-wkf-fractal-is-a-module-and-the-protocol-is-in-the-sdk.md), [ADR-20261019-3ns](20261019-3ns-a-module-declares-its-ports-and-gets-a-data-directory.md) | Needs [ADR-20261023-sns](20261023-sns-a-module-lists-a-storage-and-owns-one-the-core-prints-from.md), [ADR-20261026-mrj](20261026-mrj-a-link-may-ask-about-a-server-it-reported.md) | Reads with [ADR-20261026-kfh](20261026-kfh-a-fractal-node-sees-every-drop-of-a-leaf-connection.md)

The Fractal specification has an extension that moves files down a tree of
servers. A file is named by the hash of its bytes. A node publishes folders,
and a node below fetches a file from its parent, which asks its own parent
when it does not hold the file. This record says how `sdk/fractal` and the
module `packages/fractal` implement the version that the section
[the version this record implements](#the-version-this-record-implements)
lists, and how the product owner's rules R1 to R17 shape it. That section also
explains the terms. This first part covers one path: a server shares a folder, and a
printer on a server below prints a file from it.

## rules

### the specification and the core

1. `sdk/fractal/files` and `packages/fractal/files` implement the version of
   the file distribution extension of 2026-10-20. When they move to a newer
   version, a new record that amends this one lists what changed, as the last
   section here does.
2. Where the product owner's rules R1 to R17 and the extension both speak, a
   rule here follows both. Where only the rules speak, it follows them.
3. A server runs the extension when its module configuration has a `files`
   block, and not otherwise. The core runs the same with the module disabled,
   and nothing in `internal/`, `adapters/`, `cmd/` or `compose/` imports
   `sdk/fractal`. The changes to the core are those of ADR-20261023-sns and
   ADR-20261026-mrj, and none of them names Fractal.

### the protocol package

4. `sdk/fractal/files` holds what both ends of the extension must agree on.
   That is the identity in text and in bytes, the `fractal` URL and the
   listing in deterministic CBOR. It also holds the rule for names, the
   `files` join entry and the definition of `ContentStore1`. It reads no disk
   and opens no connection.
5. It uses the standard library alone (ADR-20260824-gfr). It writes its own CBOR for
   the four types a listing uses: an unsigned integer, a byte string, a text
   string and an array, each in its shortest form.
6. It checks every part of the rule for names except Normalization Form C. The
   module checks that with `golang.org/x/text`, a new dependency of the module
   that enters as the rules for the supply chain say. The package's
   documentation says so.
7. A reader refuses a listing or a join entry whose version it does not
   support, and acts on no part of it. A listing that is not in the
   deterministic form, or whose entries are not sorted and unique, is refused
   too.

### the join

8. A server that runs the extension puts a `files` entry in every grant it
   issues: `version: 1` and an `http` part with `version: 1` and the
   `baseUrl` of rule 26.
9. A server that runs the extension refuses a grant with no `files` entry, or
   with a version it does not support. It refuses one with no `http` part, an
   `http` version it does not support, or a `baseUrl` with a query, a
   fragment or a scheme other than `https`. It takes `http` only when
   `files.allow_cleartext_parent` is true, which is false by default. The refusal is the one the
   specification gives a grant of an unsupported protocol version: no
   connection, a log line and no retry. The module stays up without a
   connection until it is started again, because the host starts a module
   that ended again, and that would retry.
10. A server drops the `files` entry of its former parent, and its downward
    cache, when its Node Path changes and when its parent answers
    `ERR_JOIN_REVOKED`. It does both before it opens the next connection.
11. A server that runs the extension selects `ContentStore1` of every node
    above it in its ancestor shadow, and of every node below it in its
    descendant shadow. It does so whatever `shadow.patterns` selects besides.

### sharing (R5, R6, R7)

12. A person shares a folder of a server storage with a switch, and everything
    in the folder is shared. The root folder of a storage cannot be shared.
    A folder inside a shared one may be shared too, and the result is the
    union.
13. The switch is a route of the module on the server that owns the folder,
    which the host serves only to a person with `operate_resource` on the
    module. No other server can reach it. Sharing also takes `read_storage`
    on the storage, which the module checks as the person by listing the
    folder.
14. The module keeps the shared folders in a file in its data directory. It
    writes a temporary file and renames it, so a crash leaves the old set or
    the new one.
15. Unsharing a folder publishes a new root hash at once. A response the
    module is sending at that moment is finished. A request that arrives after
    it is answered under rules 22 and 31. It ends with a `404` from the top of
    the tree, unless a server between keeps a copy under rule 16.
16. A copy another server already holds stays, and that server prints it and
    serves it as it would any other copy (R7, rules 21 and 37).

### publishing

17. A server storage that has had a shared folder is one `ContentStore1`
    instance, named by the storage with each `-` written as `_`. An
    instance id follows the Identifier grammar, and a storage name holds no
    `_`. A storage where nothing was ever shared
    has no instance, so no node below learns its name. A storage whose last
    shared folder was unshared publishes the empty listing, because an
    instance is never withdrawn. `rootHash` is `null` until the first scan
    ends, and for a storage the operator removed while the module runs.
    `mediaPresent` is true, `removable` false, and `capacity` and `free` are
    `null` (R4).
18. The root listing holds the path to each shared folder and no other folder
    above it (R5). A shared folder's listing holds all of its files and
    folders. A link, a special file and the store's own `.pcl-meta` folder
    are not published.
19. A name that is not in Normalization Form C is converted. A name that
    converts to one the folder already has gets a suffix that makes it
    unique, such as ` (2)` before the extension. A name that still breaks the
    rule is left out. The module logs each of them.
20. A file's identity comes from the digest `storage.list` or `storage.stat`
    returns, which ADR-20261023-sns rule 3 gives only when it is not older than the
    file. A file with none is read through `storage.read` and hashed by the
    module. The module keeps the digests it computed, keyed by storage, path,
    size and time of change, in its data directory.
21. An object the server publishes is answered with a retention of unlimited,
    so a server below may keep its copy after the folder is unshared (R7).
22. The module answers `/objects/` from its storages only for an identity
    reachable from the current root hash of one of its instances, and from its
    copies only under rule 37. It never answers from a digest it keeps for a
    file that is not shared (R2). Any other identity is relayed under rule 31,
    or gets `404` where the server has no parent. `List` answers
    `ERR_OBJECT_NOT_FOUND` for it.
23. The module scans the shared folders and the path to them, and nothing
    beside them. It scans at start, after a change to the shared folders, and
    every `files.rescan` (60 seconds by default). A scan lists every shared
    folder and every folder inside one. It reads a file only when neither the storage
    nor the digests of rule 20 give a digest for its size and time of
    change. A scan that fails keeps the last root hash, and a root hash is
    published only for a whole tree. A file the module cannot read is left
    out of its listing with a warning, as a name is under rule 19.
24. `List` answers a listing the instance publishes as base64url text, which
    the extension allows where the binding carries no bytes. It answers
    `ERR_OBJECT_NOT_FOUND` for any other identity, and `ERR_INVALID_PARAMS` for
    one that is missing or malformed.

### the HTTPS resource

25. The module asks for a TCP port named `files` in `needs.listen` and for its
    data directory. It names `storage.list` in `requires` instead of raising
    its `core` range, so a server without it refuses the module
    (ADR-20261023-sns rule 5). This amends ADR-20261019-3ns rule 4 for this
    module, because a range above 0.0.1 would refuse the alphas of 0.0.1. A
    server too old for `needs` still refuses the unknown key, as rule 4 says.
    It serves `GET` and `HEAD {base}/objects/<identity>` on that port, over
    TLS 1.3 with the certificate and trust anchors of its TLS options.
26. `files.base_url` is the address the module puts in the grant. It must be
    an absolute `https` URL with no query and no fragment, on the granted
    port, and `http` is allowed only under rule 9. A server with a `files`
    block and no grant for the port `files` refuses to start, and names the
    port.
27. A request is checked in this order: the `Fractal-Proof` header, then that
    its key is an admitted child that is not revoked. Then comes the method,
    where any but `GET` and `HEAD` gets `405`, and then the identity, the
    `Range` and the lookup. A proof must have `typ: fractal-proof+jws` and
    `alg: EdDSA`, and a signature that verifies against its `nkey`. Only then
    is the payload read: a JSON object with an integer `iat` within
    `files.proof_tolerance` (60 seconds by default) of the server's clock.
28. The identity is taken from the request target as it arrived, with no
    percent-decoding and no resolving of dot-segments. A query is ignored.
29. The module writes ranges itself and does not use `http.ServeContent`. It
    sends `Accept-Ranges: bytes`, and answers `bytes=a-b` and `bytes=a-` with
    `206` and the whole length in `Content-Range`. It answers any other form
    with `400`. It answers any `Range` on an empty object with `200`, and
    otherwise a start at or past the end with `416`. It ignores `If-Range`.
30. An answer is `application/octet-stream` with no content coding. It
    carries no validator, never `304`, and no `Fractal-` header the binding
    does not name. Every failure carries `Cache-Control: no-store`, and a
    `401` carries `WWW-Authenticate: Fractal-Proof`. An object the module
    publishes but cannot read now gets `503` with a `Retry-After`. So does a
    file whose length or time of change is not the one the last scan read,
    until a scan publishes its new identity.

### relaying

31. A server that neither publishes an object nor may serve a copy of it asks
    its parent, with a proof of its own. So does a server asked with
    `Cache-Control: no-cache`, which does not answer from its copies. It
    passes the answer down as the bytes arrive. It passes `Range` and
    `Cache-Control: no-cache` up, and `206`, `Content-Range` and the
    retention directives down, unchanged. A server with no parent answers
    `404`.
32. A server maps its parent's failure by the extension's table. `400` and
    `401` become `503` with a `Retry-After` of minutes. `404` and `416` pass,
    `502` and `503` pass with their `Retry-After`, and anything else becomes
    `502`. A parent it cannot reach within `files.relay_timeout`, or an answer
    it cannot use before it has written its own, is `502`. A `Retry-After` the
    server sets is an HTTP date. One from the parent passes unchanged, and a
    parent of this implementation sends an HTTP date too. A response it
    cannot finish is cut short, never padded. A parent that sends nothing more of a body for `files.relay_timeout`
    cuts the transfer, and so does a caller that takes nothing more of an
    answer for that long. A connection to or from the files server that
    waits that long for its next request is closed.

### fetching and the downward cache

33. The client signs a new proof for every request, checks the parent's
    certificate and host, follows no redirect, and dials `http` only under
    rule 9. A `200` to a request with `Range` replaces what it holds. A `206`
    must start where it asked and end within the range. The length is the
    whole length in `Content-Range`.
34. The client resumes a cut transfer at most `files.max_resumes` times (5 by
    default). The retention of an object it assembled is the most
    restrictive one its answers carried.
35. The client checks the hash of what it received. A copy that does not
    match is dropped, and the client asks once more with
    `Cache-Control: no-cache`.
36. The downward cache is the folder `print/` of the module's data directory
    (ADR-20261019-3ns rule 12, ADR-20261023-sns). A fetched object with a retention of
    unlimited is kept there under its identity alone. The `name` of a URL is
    never part of the key. The cache keeps no object with any other
    retention. It deletes the copy when the transfer ends, and the print
    fails (rule 51).
37. The module serves its children only the copies it keeps under a retention
    of unlimited, with the directives of unlimited. It prints from any copy
    it holds.
38. When the cache passes `files.cache_max_bytes` (10 GiB by default), the
    copies used least recently go first. A copy a print is reading or a child
    is fetching stays.
39. At start the module checks every copy against its identity and deletes a
    copy that does not match.

### printing a fractal URL

40. The `url` of `PrintFile` and of `UpgradeFromFile` always has a scheme. A
    file the printer already holds is named by a `file` URL with no authority
    or `localhost`, such as `file:///usb/benchy.bgcode`. The printer gets its
    path without the leading slash and percent-decoded, here
    `usb/benchy.bgcode`. A `file` URL with an empty path, a `..` segment, a
    percent-encoded `/`, a query or a fragment gets `0x4016`. `PrintFile` also
    takes a `fractal` URL. A value with no scheme, or any other scheme, gets
    `0x4015`, and the printer is not asked. `UpgradeFromFile` answers `0x4015`
    for a `fractal` URL too. This amends ADR-20261020-tca rule 22.
41. Before it answers, the module parses the URL, and answers `0x4016`, a
    refinement of `ERR_INVALID_PARAMS`, when it is malformed. It answers
    `0x2013`, a refinement of `ERR_DENIED`, when the printer is not `READY`.
42. It then checks that it publishes the object, holds a copy, or has a parent
    with a `files` entry. When none holds, it answers `0x4044`, a refinement
    of `ERR_OBJECT_NOT_FOUND` that says the server has no way to the object.
    The check proves only that there is somewhere to ask. It then asks the host
    whether the principal may print, and answers `0x2011` on a no
    (ADR-20261026-mrj).
43. It then answers, and leaves the state of the printer alone. A file the
    server publishes is printed from its storage with `device.print`, with
    no copy. Any other file is fetched into the downward cache and printed
    from there (ADR-20261023-sns).
44. The file lands in the folder `usb/` of the printer and takes the `name`
    of the URL. A `name` that breaks the rule for names, Normalization Form C
    included, is ignored. So is one that does not end in `.gcode` or
    `.bgcode`. So is one that holds a control character or one of `\:*?"<>|`,
    which the FAT file system of a USB drive refuses. With no usable `name`,
    the file is named `fractal-` and the first 12 characters of the digest in
    its identity. It ends in `.bgcode` when its first bytes are `GCDE`, and
    in `.gcode` otherwise. `device.print` may answer that the printer
    already holds the `name` (`deviceapi.ErrNameTaken`). Then the file is
    sent again as the `name` with a hyphen and the same 12 characters before
    its extension, so `cube.bgcode` becomes `cube-7FcFH9hw1lY1.bgcode`. The
    core starts a copy it sent to a path itself, unchanged since, with no
    transfer (ADR-20261023-sns rule 11). A file at a path that the core does
    not take for this one is never printed, because nothing checked that it
    is the object. A print whose every path is taken fails under rule 47.
45. A fetch that fails after the answer is retried where the extension says it
    may be, at most `files.fetch_attempts` times (3 by default), honouring
    `Retry-After`. A `404` ends it at once, and the next print starts from the
    current root hash.
46. `files.start_deadline` (10 minutes by default) bounds the time from the
    answer until the core begins to transfer the file to the printer: the
    fetch and the `device.print` call. A print that has not reached the
    transfer by then is dropped, and the printer stays as it was (R13, Not
    started). Once the core transfers the file, nothing drops the print,
    because the core offers no way to cancel a transfer.
47. A print that fails or is dropped ends with one log line, and a count kept
    by outcome inside the module. The line names the identity, the printer,
    the node that called `PrintFile` and the reason. The reason is the text
    of the refusal `device.print` gave. A failure inside the host, which
    `deviceapi` gives no code, reads only "the host could not answer", and
    the host's own log holds the cause. A print whose every path is taken
    names those paths before that text.

### browsing the stores of the line (R1, R2)

48. The module answers `GET /files/stores` with every `ContentStore1` in its
    ancestor and descendant shadows. It answers
    `GET /files/stores/{node}/{instance}/{identity}` with one listing as
    JSON: the name, the kind, the length and the identity of each entry. The
    host serves both to a person with `view_resource` on the module, as it
    serves `GET /nodes`. A listing of a node above is fetched over HTTPS
    from the parent, as a print fetches a file, and may come from the
    downward cache. A listing of a node below is read with `List` on its
    instance. Any other node, this server's own included, is refused with a
    `404`. The list of stores comes in pages, read with `limit` and `after`.
    A listing this server cannot read now gets `503`.

### a 401 from the parent

49. After a `401` from its parent, the client logs its own clock beside the
    `Date` of the answer when the two differ by more than
    `files.proof_tolerance`. It then runs the join exchange once, within
    `files.relay_timeout`, and asks again with a new proof. A failed exchange
    leaves the `401`. A fetch fails on it, and a relay maps it by rule 32.
    The `Date` of the parent's answer stands in for the time source the
    extension names, because a server has no other one to check against.
50. The client runs that exchange at most once per `files.proof_tolerance`,
    counted from the end of the last exchange that ran. An exchange the
    request cancelled does not count. A request sent before an exchange
    ended waits for it, and asks again when it was granted, so requests
    refused at the same time share one exchange. Inside that time any other
    `401` stays a `401`. A clock that is off stays
    off for longer, and a parent that revoked this server denies every
    exchange, so asking more often would only load the parent. A denial with
    `ERR_JOIN_REVOKED` drops the `files` entry of the parent and the downward
    cache (rule 10), and the server asks that parent nothing more.

### a retention other than unlimited

51. The module does not print an object its parent gave a retention other
    than unlimited. The fetch ends as a failed print with the log line and
    the count of rule 47, and the reason names the retention. Nothing goes
    to the printer, and the copy is deleted. The extension requires a node
    to discard such an object when its retention ends, and the printer
    keeps the file it prints, where the module cannot discard it. Deleting
    the printer's copy when the print ends would need the module to follow
    every job and would remove a file a person sees on the printer. Only a
    parent of another implementation sends such a retention, because this
    one answers every object with unlimited (rule 21).

## enforcement

| rule | gate | state | what it refuses |
|---|---|---|---|
| 1, 2 | none. A reviewer checks the last section against the version and the rules | | |
| 3 | `TestTheCoreImportsNoFractal`, in `go test ./internal/arch/` | exists | a package of the core that imports `sdk/fractal` |
| 4, 7, 8, 9 | `cd sdk && go test ./fractal/files/` | exists | a change that breaks a test of the rule, and a conformance row with a missing test |
| 5 | `make sdk-check` | exists | an import outside the standard library in `sdk/` |
| 5 | the vectors of the extension, in `cd sdk && go test ./fractal/files/` | exists | a listing whose bytes or identity differ from the extension's examples |
| 6, 9 to 51 | `cd packages/fractal && go test ./...` | exists | a change that breaks a test of the rule, and a conformance row with a missing test |
| 6 to 51 | none. Review checks that a rule with no conformance row has a test | | |
| 13 | the host's permission check on module routes, and `TestShareRouteRefusals` in `cd packages/fractal && go test .` | exists | a share by a person without `read_storage` on the storage |
| 22 | `cd packages/fractal && go test ./files/`, on a server with no parent: an unshared file, and a file after unsharing | exists | an answer other than `404` |

## consequences

- a server that runs the extension refuses a parent that does not. A network
  turns the extension on at every server, from the top down
- an operator approves a port and a data directory for the module, which it
  did not ask for before
- a person who shares a folder shares it with every server below, including
  servers admitted later, and cannot take back a copy one of them holds
- a kept copy is not a file of the server that holds it. No page lists it
  until a page for kept copies exists
- every print of a file from above reads it through every server in between
  once. A second print below a server that kept the copy goes no higher
- a server that prints a `fractal` URL needs the port `files`, and a TLS
  certificate for an `https` base URL, even when no server is below it
- a server below a parent of another implementation that gives a retention
  of none or of a date prints none of that parent's files (rule 51)
- a started print holds its copy for 24 hours at most, if the core never
  reports the end of the transfer. This bound is not a setting
- a transfer cut after its folder is unshared cannot resume, so its print
  does not start. R7 says a print already sent still finishes. That holds for
  every transfer that is not cut, and for every print from a copy already
  fetched. Keeping the folder published until such transfers end would break
  R7's "hides the folder at once", and was not chosen

## rejected

### publishing every storage, or the storages an operator lists

Fewer parts, but R5 shares a folder and never a whole storage, and the product
owner agreed R5.

### a retention of none

Exact while no server keeps a copy, but R7 keeps a copy after unsharing, which
needs a retention that allows it.

### keeping copies in a storage of type `temporary`

The core already prints from any storage, but a copy of a file from `hq` would
show as a file of `brno` in the storage API, against R15. ADR-20261023-sns
rejects it for the same reason.

### serving a removed object for a while after unsharing

A transfer cut after unsharing could then resume, as R7 wants for a print
already sent. But the server would serve what it no longer publishes, which
the extension's reachability does not allow.

### `http.ServeContent` for ranges

It sends validators, answers `304`, and accepts range forms the HTTP binding
refuses.

### fetching straight from the node that publishes

The extension leaves it out. A node holds the admissions of its own children
only, so a node further up cannot check the caller or a revocation below it.

### a Unicode normalization in `sdk/`

Large tables to keep in step with Unicode, for one check the module makes with
a library.

## open items

- the NATS ports of `accept` are options of the module and are not granted,
  which ADR-20261019-3ns rule 13 asks. A record that moves them into
  `needs.listen` closes it

## the version this record implements

The extension is the version the Fractal team published on 2026-10-20, in two
documents: `fractal-files.md` and its HTTP binding, `fractal-files-http.md`.
The product owner's rules R1 to R17 are in
[the product owner's rules](#the-product-owners-rules).

These terms come from the extension:

| term | meaning |
|---|---|
| object | a sequence of bytes that never changes, named by the hash of its bytes |
| identity | the hash of an object with the name of its algorithm, such as `sha256:yvWz…` |
| blob | an object that holds the content of one file |
| listing | an object that holds the entries of one directory, encoded as CBOR |
| content store | a place a node publishes its own files from. One `ContentStore1` instance stands for it |
| root hash | the identity of the listing at the root of a content store |
| downward cache | the objects a node obtained from a node above it |
| retention | how long the receiver of an object may keep it: none, until a time, or unlimited |
| relay | a node that asks its parent for an object it does not hold and passes the answer down |

### what the extension asks of a node

| subject | where | what a node must do |
|---|---|---|
| identity | `fractal-files.md`, Object Identity | write an identity as text in a URL and in a path, and as bytes in a listing. Refuse any other form |
| URL | `fractal-files.md`, The URL | read `fractal:///sha256:<digest>`, refuse a percent-encoded path, and use `name` only to label the file or tell its format |
| listing | `fractal-files.md`, The Listing | encode a directory as deterministic CBOR, entries sorted by the bytes of their names |
| names | `fractal-files.md`, Names | publish names in UTF-8 and Normalization Form C, with no `/` and no U+0000, never `.` or `..` |
| join entry | `fractal-files.md`, Join Data | put a `files` entry with an `http` part in every grant, and refuse a grant without one |
| `ContentStore1` | `fractal-files.md`, Interface | expose one instance per content store in both directions, and never withdraw one while the node runs |
| ancestor shadow | `fractal-files.md`, Interface | select `ContentStore1` of every node above |
| `List` | `fractal-files.md`, Methods | answer a listing the store publishes, or `ERR_OBJECT_NOT_FOUND` |
| serving | `fractal-files.md`, Serving | serve what the node publishes, or answer that it cannot serve now |
| relaying | `fractal-files.md`, Relaying and Caching | ask the parent for an object it neither publishes nor holds. With no parent, answer that it is not available |
| caches | `fractal-files.md`, The Downward Cache | serve the children from it within the retention, and discard both caches on a new Node Path or a revocation |
| retention | `fractal-files.md`, Retention | keep an object no longer than its retention, and pass on no longer a retention than it received |
| proof | `fractal-files-http.md`, The Ed25519 Proof | sign a JWS with `typ: fractal-proof+jws` for every request, and check the caller's admission before anything else |
| resource | `fractal-files-http.md`, The Resource | answer `GET` and `HEAD /objects/<identity>` with the bytes, and never negotiate a content coding |
| ranges | `fractal-files-http.md`, Ranges | serve `bytes=a-b` and `bytes=a-`, refuse other forms with `400`, and send the whole length in `Content-Range` |
| directives | `fractal-files-http.md`, Caching | send the retention as `Cache-Control` or `Expires`, and pass the received directives down unchanged |
| failures | `fractal-files-http.md`, Failures | answer `400`, `401`, `404`, `416`, `502` and `503`, and map a parent's failure by the relay table |

The extension leaves four things to the node. They are how a file enters a
store, metadata such as dates and previews, paging of a large listing, and a
view of a transfer in progress.

### the product owner's rules

A node's line is the node itself and every node above and below it. In the
tree `hq` with `praha` and `brno` below it, and `zabovresky` below `brno`, the
line of `brno` is `hq`, `brno` and `zabovresky`, never `praha`. A rule marked
(user interface) says what a page shows, and no code of this record keeps it.

Seeing files:

- R1. I see every file of my own node.
- R2. Of other nodes in my line I see only shared folders. A folder that is
  not shared never leaves its node. Other nodes cannot browse it, download its
  files or learn what it holds, unless one is printed (R14).
- R3. I see nothing of a node outside my line.
- R4. Other nodes see a storage's name, but not its size or free space.

Sharing:

- R5. I share a folder with a switch, and everything in it is shared. Other
  nodes see its full path, but nothing else of the folders above it. A whole
  storage cannot be shared.
- R6. Only a person on the node that owns a folder can share or unshare it.
- R7. Unsharing hides the folder at once. Copies already downloaded stay, and
  a print already sent still finishes.
- R8. Other nodes can only print from a shared folder, not change it.

Printing:

- R9. A file prints on its own node, and on the nodes below it when it is
  shared. It never goes up, or to another branch.
- R10. I am offered only the printers of my node and the nodes below it.
- R11. A node accepts prints from every node above it, without asking.
- R12 (user interface). Files on a printer's USB or flash show only on its
  "Printer storage" tab, to its node and the nodes above, never in Storage.
  They print only on that printer.
- R13 (user interface). A print to another node shows "Sent", then how it
  ended. "Printing" means it started, and "Refused" that the printer was not
  ready. "Not started" means the printer took it but did not start. "No
  answer" and "Not started" ask the person to check the printer.
- R14 (user interface). Printing shows the file's name and thumbnail to the
  printer's node and every node above it, even from a folder that is not
  shared.

Showing files:

- R15 (user interface). Every file shows its node, storage and path:
  `hq · Local disk · /projects/fixtures`.
- R16 (user interface). Only files of my own node show when they changed.
  Other nodes do not send dates.
- R17 (user interface). When a node below me goes offline, its files and
  printers stay, greyed out with "node offline since 14:02", and cannot be
  printed. I cannot see whether a node above is offline, so printing its file
  ends as "No answer".

### what the product owner's rules add

The rules describe what a person sees and prints. The extension does not
contradict any of them. Each row says where the rule lands.

| rule | what it says | where it lands |
|---|---|---|
| R1, R2, R3 | a node sees all its own files, shared folders of the nodes in its line, and nothing outside its line | the extension's reachability gives the line. Sharing decides what leaves the node |
| R4 | other nodes see the name of a storage, not its size or free space | `capacity` and `free` of `ContentStore1` are `null`, which the extension allows |
| R5 | a folder is shared with all it holds. Others see its full path, no other folder above it, and never a whole storage | the root listing holds only the path to each shared folder |
| R6 | only a person on the node that owns a folder shares it | the switch is a route of the module on that server |
| R7 | unsharing hides the folder at once. Copies already downloaded stay | a new root hash at once, a retention of unlimited, and a kept copy prints |
| R8, R9 | other nodes print from a shared folder and change nothing. A file prints on its own node and below it | files travel down only, as the extension says |
| R10, R11 | a node is offered the printers of its own node and the nodes below, and accepts a print from above without asking | the existing Fractal RPC |
| R12 | the files on a printer's USB drive show on its own tab and print only on that printer | not file distribution. No Prusa interface lists a printer's files yet |
| R13 | a remote print shows Sent, then Printing, Refused, Not started or No answer | a user interface, later. `PrintFile` refuses a printer that is not ready, which the specification asks too |
| R14 | the name and preview of a printed file show to the nodes above | metadata, which the extension leaves out. Later |
| R15 | every file shows its node, storage and path | a copy of a file from `hq` kept on `brno` must not look like a file of `brno` |
| R16 | only files of a node's own show dates | a listing carries no dates |
| R17 | a node below that goes offline is greyed out, and a print to a node above that is offline ends as No answer | a user interface, later |

A file is fetched hop by hop, each request signed by the node that sends it,
instead of by a link the owning node signs. A node in between can read what
passes, as the extension's hop-by-hop model says.

### moving to a newer version

1. Compare the new version of both documents with the version this section
   describes.
2. Find each changed sentence in the conformance tables under
   `sdk/fractal/files` and `packages/fractal/files`.
3. Change the test first, then the code, then write the record that amends
   this one.
