---
title: "ADR-20261103-qag: The system may answer for the host name"
description: "On a machine where another mDNS responder answers for the host name, the server announces only its services, names the host that responder holds, and leaves the addresses to it."
date: 2026-11-03
status: proposed
supersedes: []
amends: [ADR-20260916-b3x]
tags:
  - adr
  - discovery
  - mdns
---
# ADR-20261103-qag: The system may answer for the host name

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260916-b3x](20260916-b3x-instances-announce-themselves-on-the-network.md) rules 7, 11, 13 and 23

The server's responder answers for `<hostname>.local` with the machine's
addresses, probes for that name and renames it on a conflict
(ADR-20260916-b3x rule 23). Many Linux machines already run a responder of
their own, such as Avahi, which does the same for the same name. The two
then claim one name with different records. One of them renames the machine
to `<hostname>-2.local`, and every other service the machine announces moves
with it. Such a machine has to switch discovery off, which takes away the
announcement to slicers and the list of the printers nearby.

The host name and the addresses are the machine's, not the server's. The
service records are the server's. They can be split, as long as each SRV
names the host the other responder really holds. That name is not always the
one the operating system reports. The other responder renames the machine
when another machine holds the name. It can also start before the machine's
name is set.

## rules

1. `core.discovery.host_records` says who answers for this machine's host
   name. `own` is the default, and then the server answers for the host name
   as ADR-20260916-b3x rule 23 says. `system` says that another responder on
   this machine answers for it. Any other value is refused at startup. It is
   the fourth key of the block in ADR-20260916-b3x rule 7, beside `enabled`,
   `services` and `hidden`. The values match the startup line of rule 7, and
   leave room for a later value that finds the other responder by itself.
2. With `system`, the server announces only its service records: PTR, SRV
   and TXT. It sends no A or AAAA record, and it answers no question for the
   host name.
3. With `system`, each SRV names the host the other responder holds. The
   server asks for the reverse name of each address it announces on. It asks
   with its first probe, and again after the interfaces change. The PTR of
   that reverse name gives the host. An address record does not, because an
   alias can carry the same address, and the reverse name points at one host
   (RFC 6762 section 4). Only a response sent from an address of this machine
   counts. A name that differs from the current one only in case is the same
   name. When the name changes, the SRV records take it and are announced
   again. Until such a record arrives, they carry the host name the operating
   system reports.
4. The source address is the only check, and it is not proof. A machine on
   the link that forges this machine's address, or a responder on this
   machine that reflects other machines' answers, can move the name. Such a
   machine can already announce a service of its own under any name, so this
   gives it nothing new, and the server does not try to prevent it.
5. With `system`, the server never probes for the host name, never claims
   it and never renames it. A record another machine sends for that name is
   not a conflict. The instance names stay the server's, and
   ADR-20260916-b3x rule 23 still applies to them.
6. `core.discovery.enabled` still turns the announcer and the listener off
   together (ADR-20260925-jne rule 8), and `core.discovery.hidden` still
   hides the server. `host_records` changes only which records go out.
7. The startup line of ADR-20260916-b3x rule 11 says who answers for the host
   name, as `host_records=own` or `host_records=system`. The default gets
   `host_records=own`, and the words of the line stay as they are. The
   addresses it names are this machine's, and with `system` the other
   responder announces them. When the server takes a new name, it logs the
   old one and the new one.
8. The host that `Announcement` returns under ADR-20260916-b3x rule 13 is the
   one the SRV records name, and the addresses are this machine's, as in
   rule 7.

## enforcement

- Rules 2 and 5, in `internal/discovery`:
  - `TestASystemHostAnnouncesOnlyTheServices`
  - `TestASystemHostKeepsItsNameWhenAnotherAnswersForIt`
  - `TestASystemHostLeavesTheAddressQuestionsToTheSystem`
- Rules 3 and 8, in `internal/discovery`:
  - `TestASystemHostAsksWhichNameHoldsItsAddresses`
  - `TestASystemHostAsksAgainWhenTheInterfacesChange`
  - `TestASystemHostFollowsTheNameTheSystemHolds`
  - `TestASystemHostLearnsFromItsLinkLocalAddress`
  - `TestASystemHostDoesNotSwingToAnAlias`
  - `TestASystemHostReadsItsNameWithoutCase`
  - `TestASystemHostIgnoresTheNameOfAnotherMachine`
- Rule 1: `TestTheHostNameIsTheServersUnlessTheFileLeavesItToTheSystem` in
  `internal/app/config`.
- Rule 7:
  - `TestASystemHostSaysWhenItTakesANewName` in `internal/discovery`
  - `TestASystemHostIsWhatTheStartupLineSays` in `internal/app`, which also
    shows the key reaching the announcer. It runs where `PCL_NETWORK_TESTS`
    is 1, as CI sets it.
- Rule 4 has no gate. It says what the server does not try to prevent.

## rejected

| option | why not |
|---|---|
| register the services through the system responder, over its own API | the server would depend on one responder and one bus, and it runs on machines that have neither |
| listen without announcing | the listener alone lists the printers nearby, but slicers no longer find the server |
| announce under a host name of the server's own | the machine then answers to two names, and a client lists it twice |
| a key that names the host | the other responder renames the machine on a conflict, and a fixed name then points at another machine |
| a side process that announces through the system responder's tool | the hidden switch stops working, and the TXT keys `id` and `version` come from outside |
