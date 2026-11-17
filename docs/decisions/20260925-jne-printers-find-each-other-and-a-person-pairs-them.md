---
title: "ADR-20260925-jne: printers find each other, and a person pairs them"
description: "How printers on one network list each other, how a person confirms that two of them trust each other, how they form a fleet under a top the person picks, and the per-printer setting that hides a printer."
date: 2026-09-25
status: proposed
amends: [ADR-20260916-b3x]
tags:
  - adr
  - discovery
  - mdns
  - fractal
  - security
---
# ADR-20260925-jne: printers find each other, and a person pairs them

- status: proposed
- date: 2026-09-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Supersedes in part [ADR-20260916-b3x](20260916-b3x-instances-announce-themselves-on-the-network.md) rule 7 | Builds on [ADR-20260921-wkf](20260921-wkf-fractal-is-a-module-and-the-protocol-is-in-the-sdk.md), [ADR-20260925-gv7](20260925-gv7-the-printer-build-is-a-profile-beside-the-normal-build.md)

Several printers on one network each run Prusa Connect. Each printer lists the
others it hears, with nothing for a customer to set. Being on that list grants
nothing. A person pairs two printers by comparing six digits shown on both web
pages, and only then do they trust each other. The paired printers form a
fleet under one printer the person picks. A per-printer setting hides a printer
from the list and from pairings it did not start.

## rules

### how a printer finds the others

1. Every printer announces itself as ADR-20260916-b3x rules 1 to 6 say. The TXT record
   keeps the four keys of ADR-20260916-b3x rule 4. No key names a fleet or its top.
2. The core also listens for `_prusa-connect._tcp` and `_prusalink._tcp`. The
   listener lives in `internal/discovery` beside the announcer and shares
   its socket.
3. Keep what the listener hears in memory only, at most 256 entries. An entry
   holds the instance name, its addresses, the port and the `tls` key. It
   keeps at most 8 addresses, each for as long as the record that carried
   it. It shows the first IPv4 one in address order, so a machine that
   answers on several networks keeps one link. The announcer leaves out a
   point-to-point interface and an IPv4 address that is its network's own.
   It also leaves out the interfaces Docker makes for containers: `veth…`,
   `docker0` and `br-` with a 12-digit network id. A bridge such as `br0`
   is kept. The announcer and the listener use the same interfaces. Joins
   that fail for the first time share one warning, so a host with many
   interfaces logs one line.
4. Drop an entry when its mDNS record expires or its printer sends a goodbye.
   The listener and the announcer take a packet only from the link, as RFC
   6762 section 11 says. A packet sent to `224.0.0.251` or `ff02::fb` is on
   the link whatever its source, because no router forwards it. A packet sent
   to an address of this machine needs a source on the link. That is an IPv6
   link-local address, or an address inside a prefix of the interface the
   packet arrived on. The destination is read from the IPv4 and IPv6 control
   messages. A system that reports no destination, which is Windows, is held
   to the second rule.
5. `GET /api/v1/discovery/nearby` returns the list. It takes `view_resource`
   on the link kind, which every org viewer holds, so a caller with no
   credential gets nothing. An entry with an address a printer connected to
   this server comes from names that printer in `device_id`. A
   session reports that address through `connectorapi.ReportsPeer`. A
   printer that is away, or whose session ended, is named by the address it
   last came from, until a connected printer comes from that address. The
   host keeps that address in memory. The id is
   left out for a caller who does not hold `observe` on the printer.
6. The web app shows the list on the printers page, under the listing and
   under an empty fleet. It shows only the entries with no `device_id`, because
   a printer of this server is already in the listing. Each entry links to that
   printer's own page and to the guide that adds a printer. It leaves out an
   entry that announces `_prusa-connect._tcp`, because another Prusa Connect
   server is not a printer to add. A pairing is where such an entry belongs. The list draws nothing while it is empty or
   refused with 403, and it stops asking after a 403. A caller who may not
   observe a connected printer gets no `device_id` for it, so the list shows
   that printer to them too. Its note therefore says the printers are not
   among the caller's printers, never that they are not connected. The
   printer's own page does not show the list, because a person looking for
   printers to add does not look there.
7. A printer on the list is a name and an address. It gets no access, and no
   pairing starts because a printer was found.
8. `core.discovery.enabled: false` stops the announcer and the listener
   together.

### how two printers agree to trust each other

9. Two printers trust each other only after a person pairs them. Either printer
   may start a pairing, from its web page or through its API.
10. The person who starts a pairing holds `operate_resource` on the link of the
    printer they are signed in to. The person who accepts it holds
    `operate_resource` on the link of the other printer.
11. The two printers exchange their Fractal public keys over TLS. Each side
    sends a hash of a random value before the value itself. Earlier research lists
    the steps.
12. Both printers derive the same six digits from the two keys and the two
    random values. The code uses `crypto/ecdh`, `crypto/ed25519` and
    `crypto/sha256` and adds no dependency.
13. Both web pages show the six digits and the other printer's title. The page
    of the printer that did not start the pairing also shows accept and
    decline.
14. The person accepts on the web page or the API of the printer that did not
    start the pairing. The printer's own screen shows nothing for a pairing.
15. A pairing that gets no answer expires after 5 minutes.
16. A printer holds at most 16 pairings that wait for an answer. It takes at
    most 4 pairing requests a second.
17. A declined or expired pairing leaves nothing stored on either printer.

### the routes of a pairing

18. `POST /api/v1/links/{name}/pairings` starts a pairing with the address of
    another printer. It takes `operate_resource` on the link.
19. `GET /api/v1/links/{name}/pairings` lists the pairings that wait, with
    their six digits. It takes `view_resource` on the link.
20. `POST /api/v1/links/{name}/pairings/{id}/accept` and
    `POST /api/v1/links/{name}/pairings/{id}/decline` answer one pairing. Each
    takes `operate_resource` on the link.
21. `POST /api/v1/links/pairing-exchange` receives the messages of the other
    printer. It asks for no credential, because the other printer holds none
    yet. It refuses a body over 4 KiB.
22. The printer that starts a pairing sends every exchange message, and the
    other printer only answers.
23. The core hands each exchange message to the link module through
    `sdk/linkapi` and reads nothing inside it. The module holds the keys and
    computes the digits.

### how a fleet is formed

24. A fleet is the Fractal tree of ADR-20260921-wkf with one level. Every printer in it
    joins the top directly, so each keeps the one link of ADR-20260921-wkf rule 11.
25. The top is a printer the person picks. When neither printer in a pairing
    is in a fleet, the person who starts the pairing picks which of the two is
    the top.
26. When one printer of a pairing is in a fleet, the other joins that fleet's
    top.
27. Refuse a pairing between printers of two different fleets. The message
    says to take one printer out of its fleet first.
28. After a pairing is accepted, each printer stores the other's key. The
    printer below then joins the top with that key.
29. The printer that becomes the top opens its join ports on the addresses of
    the printer, so the members on the network reach it.
30. The top admits a joining printer whose key it stored in a pairing, with no
    second approval. The module setting `accept.verification` takes a new
    value, `paired`, which admits only those keys.
31. A fleet uses `paired`. `tofu` and the `ask_first` approval of ADR-20260921-wkf
    rule 19 stay for a link an operator sets up in the module settings.
32. A pairing grants the top a role on every printer of the server that
    joins, as the principal `node:<the top's Node ID>`. The role is `viewer` or
    `operator`. Viewer lets the top show the printers. Operator also lets the
    top start, pause and cancel prints.
33. The Fractal module setting `pairing.role` is the server-wide default,
    `viewer` or `operator`. Its default is `viewer`.
34. The person at the printer that joins picks the role for that pairing. The
    accept route takes a `role` field, and the start route takes one when the
    joining printer starts the pairing. Without the field the pairing uses
    `pairing.role`.
35. The default is `viewer` because it grants the least. Another printer
    starting prints on this one is a choice a person makes.
36. The role stays an ordinary grant. A person changes it later through
    `/api/v1/grants`, as `packages/fractal/README.md` says.
37. `GET /api/v1/links/{name}` also answers whether this printer is in a fleet
    and the address of its top. It takes `view_resource` on the link, as
    before.
38. The top sends every member the keys of the other members over the paired
    link.
39. A person makes another member the top from the web page of any member.
    Every member then joins the new top with the key it already holds.
    Nothing picks a new top by itself.
40. A person signs in to the top to see the whole fleet. An account belongs to
    one printer and does not travel down the tree.

### the hidden printer

41. Every printer has a setting, `hidden`. A new printer is visible unless
    its configuration file says otherwise.
42. `core.discovery.hidden` in the configuration file sets the value a printer
    starts with. Its default is `false`. The printer configuration of
    ADR-20260925-gv7 rule 12 may set it, so a printer model can ship hidden.
43. Visible is the default because a customer then finds a printer with
    nothing to set. Being found grants nothing, under rule 7.
44. A customer's switch is stored in the machine-owned overlay,
    `config.d/resources.json`, beside the first-run decision. Once it is
    there, it wins over `core.discovery.hidden`. A change applies at once,
    with no restart. A build older than the switch refuses the file while it
    carries the `discovery` key, so going back to such a build needs the key
    removed from the file first.
45. `GET /api/v1/discovery/visibility` reads the setting and takes
    `view_resource` on the link kind. `PUT` on the same path changes it and
    takes `configure_resource` on the link kind, which every org
    administrator holds.
46. The settings page of the web app has a switch for it.
47. A hidden printer stops announcing every service of
    `core.discovery.services` and every record a module added.
48. When a printer becomes hidden, it sends a goodbye for each record, as
    RFC 6762 section 10.1 says. Other printers then drop it from their lists
    at once.
49. A hidden printer answers `POST /api/v1/links/pairing-exchange` with 404.
    No other printer and no phone can offer it a pairing.
50. A hidden printer can still join a fleet. Its administrator starts the
    pairing on its own page with the address of the top. A person at the top
    accepts it.
51. A hidden printer that is in a fleet stays in it. Hiding a printer does not
    take it out of a fleet.
52. A hidden printer still lists the printers it hears. It listens without
    asking, because a question carries its address to every machine on the
    network.
53. A hidden printer still serves its web page and its API at its address. The
    setting stops the printer being found. The login still protects it.
54. On a server that drives several printers, `hidden` hides the server,
    because the server is what announces itself. To keep one printer out of a
    fleet's view, remove the `sink:fractal` grant on that printer.

### what changes in ADR-20260916-b3x

55. ADR-20260916-b3x rule 7 is superseded in part. `core.discovery.enabled: false`
    switches the announcer off for the whole install, from the file. `hidden`
    switches it off from the web app and the API. The block gains a third key,
    `core.discovery.hidden`, under rule 42. Every other rule of ADR-20260916-b3x
    stands.

### where the code runs

56. The listener, the list and `hidden` are part of the core. They work on
    every build with no module.
57. Pairing and the fleet need the Fractal module. The printer build runs that
    module only after the owner confirms it under ADR-20260925-gv7 rule 25.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | the TXT-source gate of ADR-20260916-b3x rules 4, 5 and 17 | owed, the old backlog |
| 2 to 4 | tests in `internal/discovery` that hear a record and its goodbye, and drop a packet from off the link | exists |
| 5, 18 to 21, 45 | `make api-check` | exists, once the routes are in the document |
| 5 | a test in `internal/app` that names a printer by its address, and one in `httpapi` that leaves the id out | exists |
| 6 | component tests of the panel and of the printers page | exists |
| 11, 12 | a test in `packages/fractal` where a relay swaps the keys and the digits differ | owed |
| 15, 16 | a test in `packages/fractal` that fills the waiting list and waits out the expiry | owed |
| 24 to 30 | `make mockup-check`, with a pairing between two containers | owed |
| 32 to 36 | a test in `packages/fractal` that accepts with each role and checks the grants written | owed |
| 42, 44, 47, 48 | a test in `internal/app` that sets `hidden`, and one in `internal/discovery` that hears the goodbye | exists |
| 49 | a test of the exchange route on a hidden printer | owed, with the route |
| the rest | none. Review holds them | none |

## rejected

| option | why not |
|---|---|
| a fleet id or a top flag in the TXT record | anyone on the Wi-Fi reads it, and it names the printer worth attacking |
| a printer joins the fleet as soon as it is found | anything on the network can announce itself with any name |
| a button pressed on both printers within a time window | a neighbour on shared Wi-Fi can press in the same window |
| approval with no digits to compare | it trusts whichever key arrives first, so a man in the middle passes |
| the same Prusa account on both printers as the only way | it fails offline, and many school printers have no account |
| a code typed from one printer into the other | it needs a PAKE library, and typing on a touch screen is slow |
| the digits confirmed on the printer's own screen now | that needs a dialog from the firmware team, which waits |
| every printer holding the whole fleet's state | it costs memory on every printer and needs merge rules for the queue |
| an elected leader | an election is hard to test and gains little on a desk of printers |
| `hidden` on by default | a customer would have to find a setting before any printer finds another |
| `operator` as the default role | another printer could start prints on this one before a person chose that |
| an election of a new top | it is hard to test, and a person picks the top anyway |
| `hidden` as a key in the configuration file only | a customer changes it from the web app or a phone, never in a file |
| `hidden` also refusing the API | a login protects the API. Hiding is about being found |

## open for the owner

1. The Fractal module runs a NATS server in its own process. Earlier research
   measured about 10 MiB at idle on a desktop. The owner either accepts that
   cost after a measurement on the board, or asks for a lighter transport.
2. Rule 14 confirms a pairing on the web page, because the printer's own
   screen waits. The owner decides when the firmware team is asked for the
   dialog.

## open items

- `internal/discovery` lists the printers nearby and hides a printer. Nothing
  under `sdk/` or `packages/` pairs yet.
  The old backlog are
  the work
- the memory of the core's mDNS responder is not measured yet. Earlier research
  measured only the library it replaced, on a desktop
- a phone needs a way to pin each printer's self-signed certificate, and no
  record says how
- `sdk/linkapi` has no method for a pairing yet, so rule 23 needs a change to
  `sdk/` under ADR-20260827-qvk
