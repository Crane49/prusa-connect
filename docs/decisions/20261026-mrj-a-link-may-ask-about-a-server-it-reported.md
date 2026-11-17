---
title: "ADR-20261026-mrj: a link may ask about a server it reported"
description: "A link module may ask access.check about node:<id> for a server it reported with link.changed, so the Fractal module refuses a print of a fractal URL before it answers, as a print of a path is refused."
date: 2026-10-26
status: accepted
supersedes: []
amends: [ADR-20260929-f4n]
tags:
  - adr
  - authorization
  - federation
  - modules
---
# ADR-20261026-mrj: a link may ask about a server it reported

- status: accepted
- date: 2026-10-26

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260929-f4n](20260929-f4n-a-module-may-ask-about-a-person-who-is-away.md) rule 9 | Builds on [ADR-20261023-7r3](20261023-7r3-fractal-files-are-shared-folders-fetched-over-https.md), [ADR-20260921-wkf](20260921-wkf-fractal-is-a-module-and-the-protocol-is-in-the-sdk.md)

A server above calls `PrintFile` on a printer of this server. With a `file`
URL, the module sends a command as `node:<id of the server above>`, the host
checks that principal, and a principal without the right is refused with
`0x2011` at once. With a `fractal` URL, the module answers first and calls
`device.print` only after the fetch. So the same principal got `OK`, and the
print failed later, after a transfer nobody could use.

The module could not ask before it answered. `access.check` answers only
about the module itself and about a person it served
(ADR-20260929-f4n rule 9). A link already calls the fleet as `node:<id>` for a
server it reported (ADR-20260921-wkf rule 13). Asking about that same
principal reads no part of the graph the link cannot already use.

## rules

1. A link may ask `access.check` about `node:<id>` for a server it reported
   with `link.changed`. That is the set of servers the host accepts as the
   principal of a fleet call from that link. The host reads the graph for it
   as it does for a person the module served.
2. Any other `node:<id>`, and `node:<id>` from an instance that is not a link,
   stays `ErrUnknownSubject` (ADR-20260929-f4n rule 9).
3. The action stays held to the instance's `permissions` list
   (ADR-20260929-f4n rule 7).
4. Before it answers a `PrintFile` with a `fractal` URL, the Fractal module
   asks the two questions `device.print` asks for a print from a storage.
   They are `operate` on the printer and `read_storage` on the storage kind. It asks
   about the principal `command_principal` names. A no is answered `0x2011`,
   as a print of a path is. The module names `access.check` in `requires`.
   The host decides from its own graph, and the module only asks it and
   passes the answer on. So ADR-20260921-wkf rule 14 still holds: the module
   checks no permission itself.
5. Any error from `access.check` leaves the decision to `device.print`, which
   asks it again after the fetch. That covers a host older than this record
   and a refusal of the instance's ceiling (rule 3). The module logs that it
   could not ask.

## enforcement

| rule | gate | state |
|---|---|---|
| 1, 2 | `TestALinkAsksAboutAServerItReported` in `go test ./internal/app/resourcehost/` | exists |
| 4, 5 | `TestPrintFileAsksTheHostFirst` in `cd packages/fractal && go test ./bridge/` | exists |
| 4 | `TestTheManifestAsksForFiles` in `cd packages/fractal && go test .` | exists |

## rejected

| option | why not |
|---|---|
| a dry run flag on `device.print` | a new field on a published contract, for a question `access.check` already answers |
| answer `OK` and let the print fail after the fetch | a server above cannot tell a refusal from an outage, and the fetch moves a file nobody may print |
| ask about any `node:<id>` | a link would read the grants of servers it does not carry, which ADR-20260929-f4n refused for this reason |
| refuse the print when the host cannot answer | a server older than this record would refuse every print of a `fractal` URL |
