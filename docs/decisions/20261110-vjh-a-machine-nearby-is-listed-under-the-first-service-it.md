---
title: "ADR-20261110-vjh: a machine nearby is listed under the first service it announces"
description: "A machine that announces a server and PrusaLink on two ports of one host is one entry of the printers nearby, the server, so it is not offered as a printer to add."
date: 2026-11-10
status: proposed
supersedes: []
amends: [ADR-20260925-jne]
tags:
  - adr
  - discovery
  - mdns
---
# ADR-20261110-vjh: a machine nearby is listed under the first service it announces

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260925-jne](20260925-jne-printers-find-each-other-and-a-person-pairs-them.md) rule 5

A printer can run a Prusa Connect server of its own beside its PrusaLink.
There the system responder announces PrusaLink on port 443, and the server
announces `_prusa-connect._tcp` on port 8090. Both name the same host.

The list of printers nearby made one entry for each instance name, host and
port. Such a printer was two entries. The web app left out the server and
offered the PrusaLink entry under Add printer. Such a printer joins another
server as a node, through a pairing, so the guide behind that button is the
wrong way in.

The machine says what it offers by the services it announces. The listener
picks which of them the machine is listed under.

## rules

1. The listener asks for its service names in order of preference:
   `_prusa-connect._tcp` first, then `_prusalink._tcp`. `config.NearbyServices`
   holds that order.
2. Entries whose services name one host, compared without case, are one
   machine. `GET /api/v1/discovery/nearby` returns only the entries of that
   machine that answer under its first service, and leaves out the others.
3. The machine this server runs on is not listed. Its own records still rank
   its host, so the PrusaLink a system responder announces beside the server
   is left out with it. No driver has to report anything for that.
4. A machine that announces only PrusaLink is listed as a printer, whatever
   runs it. A printer whose own server is off is offered under Add
   printer, as any other PrusaLink printer.
5. The web app keeps leaving out an entry that announces
   `_prusa-connect._tcp` (ADR-20260925-jne rule 6). So a machine that runs a
   server does not show in Printers nearby at all. A pairing is where such an
   entry belongs.

### edge cases

- A server that is switched to hidden announces nothing. Another server then
  hears only the PrusaLink of that machine, and offers it under Add printer.
- A machine that runs a server and an unrelated PrusaLink, such as a
  Raspberry Pi that also drives an older printer, is listed only as the
  server. That printer is not offered under Add printer, not even on the
  server of that machine.

## enforcement

- Rules 1 and 2: `TestAServerComesBeforePrusaLinkNearby` in
  `internal/app/config`, and `TestAMachineIsListedUnderTheFirstServiceItAnnounces`,
  `TestAServerHidesNoPrinterOnAnotherMachine` and
  `TestAServerBesideASystemResponderIsListedOnce` in `internal/discovery`.
- Rule 3: `TestTheMachineThisServerRunsOnIsNotListed` in `internal/discovery`.

## rejected

| option | why not |
|---|---|
| a `join=node` key in the TXT record of the printer's PrusaLink | the printer would decide how another server treats it, and every new way to join would need a new value |
| leave both entries and let the web app pair them by address | the API would still serve one machine as two, and every client would repeat the rule |
| a fixed list of server services in the listener | the order of the names it asks for already says which comes first |
