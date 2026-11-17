---
title: "ADR-20260929-a4j: a new install asks for a one-time setup code"
description: "How the first run proves that the person recording it can read the server's log: a one-time code printed at start, set in the configuration file for a scripted install, limited per address and gone once the first run is done."
date: 2026-09-29
status: accepted
tags:
  - adr
  - security
  - auth
  - first-run
---
# ADR-20260929-a4j: a new install asks for a one-time setup code

- status: accepted
- date: 2026-09-29

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

A new install serves nothing but the first run until somebody records what it
does about identity. The server listens on every interface, so without a
check the first machine on the network to call `POST /api/v1/setup` becomes
the administrator, or makes the server open to everybody. The first run now
asks for a code that the server prints in its own log. Only a person who can
read that log, on the box itself or through `docker logs` or `journalctl`, can
finish the first run.

## rules

### the code

1. While the first run is open, the server makes a setup code at start: 12
   symbols of Crockford's base 32 alphabet, drawn with `crypto/rand`, which is
   60 random bits. It is written in three groups of four, like
   `K7QD-3M9X-TP2A`.
2. The server prints a code it made in a `WARN` log line under the key
   `setup_code`, and in a block on standard error. The block is printed
   whatever the log level and format say, so the code is always in
   `docker logs` and `journalctl`.
3. The code lives in the process memory and nowhere else. A restart before
   the first run is done makes and prints a new code, and the old one stops
   working. A restart is also how a person gets a code back when the log has
   rotated past it.
4. `core.auth.setup_code` sets the code for a scripted install, such as a
   Docker, Kubernetes or Ansible deployment. The value holds only printable
   ASCII characters and spaces, checked before rule 6 changes it. After rule
   6, it has 12 to 64 characters and at least 6 different ones. The server
   then makes no code, prints none, and names the key in its log line
   instead of the value.

### the check

5. `POST /api/v1/setup` takes the code in the body field `code`. A missing or
   wrong code answers 403 with `core.auth/SETUP_CODE_REFUSED`, and nothing is
   recorded: no mode, no account, no module switched.
6. Both codes are changed the same way before they are compared. Lower case
   becomes upper case, hyphens and spaces are removed, the letter O becomes
   the digit 0, and the letters I and L become the digit 1. The SHA-256
   digests of the two are compared with `crypto/subtle`, so the time a
   comparison takes says nothing about how much of the code was right.
7. The code works once. When a decision is recorded, the server drops the
   code from memory, and the next call answers `core.auth/SETUP_CLOSED`. Only
   one decision is recorded at a time, so a second caller with the same code
   meets the closed first run.
8. An install whose decision is already written asks for no code. That is an
   install with an issuer, an account, or a principal the operator chose with
   no password login, as the first run settles it at start. An install with
   `core.auth.local` and no account stays in its first run, which refuses mode
   `open` and creates the administrator behind the code.

### wrong codes

9. One client address may send 5 wrong codes in 15 minutes, counted from its
   first wrong code. After that, every call from it answers 429 with
   `core.auth/SETUP_CODE_LIMITED`. The message names the wait in minutes, and
   `Retry-After` gives it in whole seconds. The right code is refused too
   until the window is over. A restart clears every count.
10. The client address is the one the login limit reads: the peer, or the
    forwarded address behind a proxy listed in `core.trusted_proxies`. The
    whole address is the key, for IPv6 too, so one host cannot use up the
    attempts of the others on its network. A loopback address is never
    counted or refused, so the owner at the machine is never locked out.
    Wrong codes from loopback are logged at most once a minute, with how many
    came since the last line. The first request from loopback that carries
    `X-Forwarded-For` or `Forwarded` logs one warning: a proxy on this machine
    forwards requests, and `core.trusted_proxies` does not list it.
11. The server counts at most 1024 addresses at once, which bounds a caller
    with many addresses to 5,120 guesses in 15 minutes. A new address past
    that is refused as limited, with the wait until the oldest address leaves
    the table. An address already counted is never dropped to make room,
    because that would give it new attempts. A full table is logged at most
    once a minute. Rule 18 bounds what one IPv6 network may take of it.
12. Each wrong code is logged at `WARN` with the address and its count. An
    address that reaches the limit is logged once more. No log line holds the
    code that was sent or the code the server holds.

### where the code appears

13. No operation returns the code. `GET /api/v1/setup`, the answer of
    `POST /api/v1/setup` and every refusal leave it out.
14. The web app's first-run screen asks for the setup code before the
    account. A sentence under the field says the code is in the server log,
    read with `docker logs` or `journalctl`. It also says that a restart
    prints a new code when the log no longer shows it. A code set in the
    configuration is not printed, and the sentence says so.
15. No setting turns the code off. An operator may choose the code, and the
    first run always asks for one.
16. No environment variable and no command-line flag carries the code. The
    configuration file is the only place to set it.

### the account path

17. `POST /api/v1/auth/setup` creates the first administrator only with the
    same code, while this server's own account store holds no account. The
    server prints the code at start in that state too. Once an account
    exists, the code is gone.

### one network

18. One IPv6 /64 takes at most 16 of the addresses rule 11 counts. A new
    address of a /64 that already holds 16 is refused as limited, with the
    wait until that /64's oldest address leaves. So one host cannot fill the
    table and lock out every new address, such as the owner coming from a
    Docker bridge.

## enforcement

| rule | gate |
|---|---|
| 1 | `TestTheServerMakesALongRandomCode` in `internal/app` |
| 2, 4 | `TestAMadeCodeIsPrintedAndAConfiguredOneIsNot` in `internal/app` |
| 2, 5, 6, 7, 12, 13 | `TestTheFirstRunTakesTheCodeFromTheLog` in `test/e2e` |
| 3 | `TestARestartBeforeTheFirstRunPrintsANewCode` in `test/e2e` |
| 3 | `TestARestartBeforeSetupPrintsANewCode` in `internal/app` |
| 4 | `TestAConfiguredSetupCodeIsAskedForAndNeverPrinted` in `test/e2e` |
| 4 | `TestASetupCodeOfYourOwnIsHeldToTheLengthOfAMadeOne` in `internal/app/config` |
| 5 | `TestADecisionWithoutTheCodeRecordsNothing` in `internal/app` |
| 6 | `TestTheRightCodeIsAdmittedAndAnyOtherIsRefused` in `internal/app` |
| 6 | `TestTwoSpellingsOfOneCodeReadTheSame` in `internal/app/config` |
| 7 | `TestTheCodeWorksOnce` in `internal/app` |
| 7 | `TestOneDecisionIsRecordedAtATime` in `internal/app` |
| 8 | `TestASettledInstallTakesNoCode` in `internal/app` |
| 8 | `TestAPasswordLoginWithNoAccountStaysPending` in `internal/app` |
| 8 | `TestAPasswordLoginInstallIsSetUpBehindTheCode` in `test/e2e` |
| 9 | `TestWrongCodesAreLimitedPerAddress` in `internal/app` |
| 9 | `TestFourWrongCodesDoNotRefuseTheRightOne` in `internal/app` |
| 9 | `TestWrongSetupCodesAreLimited` in `test/e2e` |
| 9 | `TestALimitedFirstRunSaysWhenToTryAgain` in `internal/transport/http/httpapi` |
| 10 | `TestEachIPv6AddressIsCountedAlone` in `internal/app` |
| 10 | `TestLoopbackIsNeverRefusedForWrongCodes` in `internal/app` |
| 10 | `TestWrongCodesFromLoopbackAreLoggedOnceAMinute` in `internal/app` |
| 10 | `TestAnUnlistedProxyIsWarnedAboutOnce` in `internal/app` |
| 10 | `TestAForwardedRequestFromLoopbackIsMarked` in `internal/transport/http/httpapi` |
| 11 | `TestAFullTableRefusesANewAddress` in `internal/app` |
| 18 | `TestOneIPv6NetworkTakesAtMostSixteenSlots` in `internal/app` |
| 12 | `TestAWrongCodeIsLoggedWithoutTheCode` in `internal/app` |
| 14 | the first-run screen tests in `packages/svelte/src/shell/first-run` |
| 17 | `TestTheAccountPathTakesTheSetupCode` in `internal/app` |
| 17 | `TestTheAccountPathAsksTheFirstRunForTheCode` in `internal/transport/http/httpapi` |
| 15, 16 | none. Review holds them |

## rejected

| option | why not |
|---|---|
| a setting that turns the code off | it opens the gap again, and a scripted install already sets its own code under rule 4 |
| an environment variable | the core reads one file and no environment variable, and a second place to configure it is a second place to look |
| a command-line flag | every user on the machine reads the arguments of a process |
| keeping the code in a file, as Jenkins keeps its first admin password | a secret on disk that nobody deletes. A new code at each start keeps the log the only place to look |
| listening on loopback until the first run is done | a headless Raspberry Pi or NAS has no browser on it, so the person could never reach the page |
| showing the code on the printer's screen | a server on a NAS or in a container has no screen |
| six digits | 20 bits. A limit per address cannot keep that safe from a network of many addresses |
| the login limit's growing wait | it only makes a caller wait. Many parallel calls each wait alone, so more connections give more tries |
| counting an IPv6 client by its /64 | every host on a home network shares one link-local /64, so one host would lock out the rest |
| counting loopback like any address | a host on the network could then lock out the owner at the machine |
| settling a password login with no account as `required` | the web app then shows only sign-in, and the account path is the only door |
| a global limit over every address | anybody on the network could lock the owner out of the first run by sending wrong codes |
| the code in a header | the page already sends a body, and one place for the whole decision is simpler to document |

## sources

- [Jupyter Server security](https://jupyter-server.readthedocs.io/en/latest/operators/security.html):
  the server prints a token in its log at start, and a browser needs it to
  connect
- [Unlocking Jenkins](https://www.jenkins.io/doc/book/installing/linux/#unlocking-jenkins):
  the setup wizard asks for a password printed in the log and written to a
  file
- [NIST SP 800-63B](https://pages.nist.gov/800-63-3/sp800-63b.html) section
  5.2.2, which `internal/transport/http/loginguard` already follows for
  password logins

## open items

- an install that recorded `required` before it had an account shows only
  the sign-in page. Whether the web app should offer the account path there
  is an open question
- behind a reverse proxy on the same machine that is not listed in
  `core.trusted_proxies`, every client has the loopback address of the
  proxy, so none is counted. The 60 random bits of the code are then the
  only limit. The log flood this could cause is fixed by rule 10: the wrong
  codes make one line a minute, and a warning names the missing proxy
- only English carries the first-run texts. The other languages show the
  English until a translator adds `firstRun.code`, `firstRun.codeWhere` and
  `firstRun.missingCode`
- rule 6 holds by construction. No test measures the time a comparison takes
