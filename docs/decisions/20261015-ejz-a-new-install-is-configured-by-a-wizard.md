---
title: "ADR-20261015-ejz: a new install is configured by a wizard"
description: "How a person who downloaded one binary configures the server in the browser on its first run: the wizard reads the configuration contract from the server, writes a machine-owned file under config.d, and the server restarts itself on that file."
date: 2026-10-15
status: accepted
tags:
  - adr
  - first-run
  - configuration
---
# ADR-20261015-ejz: a new install is configured by a wizard

- status: accepted
- date: 2026-10-15

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

A release is a set of binaries, one for each operating system. A person
downloads one, starts it and opens the browser. Nobody in that group writes a
`config.yaml` first, so the server they get runs on the defaults: no storage
for files, every key at the value a developer chose. The first run today asks
only for the identity decision and the administrator.

The first run is now a wizard. It asks the person for every choice the
configuration offers, in a short basic path or a full advanced path. It
writes the answers to a file the server owns, and the server then restarts on
that file. Until the wizard is done, the server serves nothing else, which is
the gate [ADR-20260929-a4j](20260929-a4j-a-new-install-asks-for-a-one-time-setup-code.md)
already holds.

## rules

### when the wizard is shown

1. The wizard is the first-run screen. It is drawn while the first run is
   pending, which is while `auth.mode` is absent from the machine-owned overlay.
   A missing `config.yaml` is still a valid start: the server starts on the
   defaults and serves the wizard.
2. An install that has recorded its first-run decision never shows the wizard
   again. A later change goes through the settings pages or the operator's
   own file.
3. The setup code of ADR-20260929-a4j is asked on the first screen after the welcome,
   before any other choice. Every rule of ADR-20260929-a4j stays in force.

### where the answers are written

4. The wizard's configuration is written to `config.d/setup.yaml`, beside the
   overlay `config.d/resources.json`. The server never writes the operator's
   `config.yaml`. That rule is G2 of the proposal, and this record keeps it.
5. The loader reads `config.d/setup.yaml` first and the operator's file over
   it. A key the operator's file sets wins. Mappings merge key by key. A list
   or a single value is replaced whole.
6. `config.d/setup.yaml` is held to the same rules as the operator's file. An
   unknown key is refused with the path of that file and the line. It holds
   the `core` and `persistence` blocks only. An instance the wizard declares,
   the storage, the history store and the MQTT sink, goes through the resource
   store into the overlay, as an instance declared in the browser does.
7. The server writes `config.d/setup.yaml` in the first run and at no other
   time. An operator may edit or delete it. Deleting it returns the server to
   the defaults, not to the wizard: the first-run decision stays recorded.
8. The file starts with a comment that says the wizard wrote it, on which
   date, and that a key in `config.yaml` overrides it.

### the contract the wizard reads

9. The wizard reads its fields from `GET /api/v1/setup/configuration`, the
   operation `get-setup-configuration`. It is open while the first run is
   pending, like `get-setup`, and answers 409 `core.auth/SETUP_CLOSED` after.
10. The answer is built from the configuration structs, `config.Keys` and
    `config.Default()`, the same three sources the reference page in
    `config.md` is generated from. The frontend holds no list of keys.
11. Each key in the answer carries its dotted path, its group, its level
    (`basic` or `advanced`), its type and its default. It also carries its
    allowed values when it has a closed set, its sentence from `config.Keys`,
    and whether the operator's file already sets it.
12. A key the operator's file sets is shown locked, with its value. A secret
    key shows only that it is set.
13. The answer leaves out a key whose part this binary was built without. A
    wizard only offers what the running binary can run.
14. The answer leaves out the keys that decide where the server writes its own
    files: `core.overlay`, `core.modules`, `core.authz.database`,
    `core.auth.accounts` and `core.auth.setup_code`. Changing them in the
    wizard would move the file the wizard writes into. It also leaves out
    `core.authz.org_admin_handover`, which matters only to an integrator who
    runs several organisations through the grant API.
    - **Amended by [ADR-20261019-h76](20261019-h76-an-operator-may-switch-installed-modules-off.md) rule 4.** The
      answer also leaves out `core.modules_enabled`, so the wizard never
      decides whether installed modules run.
15. The level and the group of a key are fields of `config.Key`. A key with no
    level is `advanced`.

### confirming

16. `POST /api/v1/setup` takes an optional member `configuration`, a mapping
    from a dotted path to a value. Without it, the call does what it does
    today and the server does not restart.
17. With it, after the setup code is accepted, the server merges the mapping
    under the operator's file and runs the same validation a start runs. A
    refusal names the dotted path, and nothing is written.
18. Then the server writes `config.d/setup.yaml` and `config.d/first-run.json`
    through `internal/app/atomicfile`. The second file holds the identity
    mode, the admission choice, the module switches and the administrator. It
    is written with mode `0600`.
19. The administrator's password is written to `config.d/first-run.json` only
    as a hash, made with the password cost of the new configuration. The
    password is never written in clear.
20. The call answers `202` with `restarting: true`. The server then drains as
    it does on `SIGTERM` and starts again.
21. The server starts again as the same binary with the same arguments. On
    Linux and macOS it replaces its own process. On Windows it starts a new
    process on the same console and exits.

### the start after confirming

22. A start that finds `config.d/first-run.json` applies it in the order
    `Record` uses: the modules, the administrator, the admission choice and
    the mode last. It then deletes the file.
    - **Amended by [ADR-20261019-h76](20261019-h76-an-operator-may-switch-installed-modules-off.md) rule 8.** While
      `core.modules_enabled` is `false`, a choice that switches an installed
      module on is skipped and logged.
23. The administrator is created through the same account store that
    `create-first-account` uses, under the id of `core.principal`.
24. A start that fails while `config.d/first-run.json` is present renames
    `config.d/setup.yaml` to `config.d/setup.yaml.refused`, writes the error
    into `config.d/first-run.json` and starts again once on the previous
    configuration. The first run is then pending again and the wizard shows
    the error on its summary.
25. That restart prints a new setup code, as ADR-20260929-a4j rule 3 says. The wizard
    asks for it again.

### the page

26. The wizard is a full page in the frame of the sign-in page, the target
    design of earlier research. It is not a dialog.
27. The first screen welcomes the person and links to the documentation in a
    new tab. The second says what the wizard does and asks for the setup code.
    The third asks for the basic or the advanced path.
28. The basic path asks only the keys at level `basic` and the first-run
    choices. The advanced path asks every key, grouped by its group.
29. The last screen is a summary of every value that differs from the
    default, and every first-run choice. Confirm sends the call of rule 16.
30. The draft is kept in `localStorage`, so a reload loses nothing. The
    password and the setup code are kept in `sessionStorage` only: they
    survive a reload and are gone when the tab is closed.
31. After the call, the page waits for the server. It reads `get-health` and
    then `get-setup` until the first run is closed, for at most 90 seconds.
    It then signs in with the password it holds, clears both stores and opens
    the fleet.
32. When the configuration moves the API to another address, the page waits
    on the new address and then opens it.
33. When the server does not answer in time, the page says so and tells the
    person to read the window or the log the server writes to. When the server
    comes back pending with an error, the page shows the summary with that
    error.

### what the wizard offers beside the keys

34. A build names where its documentation is read, in the build variable
    `DOCS_URL`, stamped like the version. `{version}` in it is replaced by the
    version. It is the beta portal while the project is private. The wizard,
    each field's information and the app's footer link there, and nowhere
    else.
35. The folder for uploads is checked on the server, behind the setup code,
    before the person moves on: `check-setup-storage` writes one file and
    removes it, and creates no folder. The confirm checks it again.
36. The encryption step has a switch for the web app and one for the
    printers. A printer speaks TLS 1.2 with one ECDSA suite and the web app
    TLS 1.3, so either switch gives printers a listener of their own. The
    step asks the address printers reach the server at with both switches
    off too, and refuses a loopback name. `core.frontend.printer_address`
    names that address with the printers' listener port, or the web app's
    while printers have none. An operator file that declares how printers
    connect leaves the key unwritten. `create-setup-certificates`
    makes an ECDSA authority and two leaves under `config.d/certs`, and the
    page hands the authority over as `connect.der`. Printer encryption sets
    `core.frontend.printer_custom_ca`, which makes the add-printer page write
    `custom_cert`.
37. A key names the switch that turns it on in `config.Key.DependsOn`. The
    wizard draws it under that switch while the switch is on, and does not
    write it while the switch is off.
38. The wizard keeps each printer's history by default, in a SQLite store of
    its own under `config.d`, and declares an MQTT sink when the person names
    a broker.
39. The web app can be switched off on the advanced path. The summary then
    says the page is gone after the restart, and the wizard ends without the
    fleet.
40. Every field carries a sentence of the wizard's own and, behind an
    information button, the reference's sentence, the key and a link to the
    configuration page. The wizard has a language and a theme switch of its
    own, and the language picked becomes the web app's default unless the
    person sets another.

41. The documentation has a section **Configuration** right after the
    overview. A page says how the configuration works, and each part of the
    server has a page with what the part does and a table of its keys.
    Every key has an anchor of its own, and `config.PageOf` names the page.
    The wizard links each field to that row, and to the guide its key's
    `See` names, and each step to the page of its part.

## enforcement

| rule | gate |
|---|---|
| 5, 6 | `internal/app/config` tests: a key in each file, a refused key names `setup.yaml` and its line |
| 10, 13, 14 | `internal/app` test: every path in the answer is a path the reference page lists, and the excluded keys are absent |
| 15 | `cmd/generate-config-reference` refuses a level other than `basic` or `advanced` |
| 17, 18 | `internal/app` test: a refused value writes no file |
| 19 | `internal/app` test: `first-run.json` holds no byte of the password |
| 22, 24 | `internal/app` test: a start with the file applies it, and a start that fails rolls back |
| 30 | `packages/svelte` test: `localStorage` never holds the password or the code |
| 35 | `internal/app` test: a read-only folder is refused, and the check creates nothing |
| 36 | `internal/app` test: both leaves verify against the authority, are ECDSA, and carry an address as IP and DNS |
| 41 | `internal/app/config` test: every key's page exists |
| 41 | `cmd/generate-config-reference` test: every key is on exactly one page, and a `See` names a page that exists |
| 37 | `cmd/generate-config-reference` refuses a `DependsOn` that names no key |
| 37 | `packages/svelte` test: a key under an off switch is not sent |

## rejected

| option | why not |
|---|---|
| the server writes `config.yaml` when it is missing | every shipped deployment mounts that file read-only, so the wizard would never be shown there. It also breaks G2 |
| show the wizard when `config.yaml` is missing | the same reason. The pending first run is already the gate, and it works on every deployment |
| exit after confirming and let the supervisor start the server | a binary started from a terminal or a double click has no supervisor, and systemd does not restart an exit 0 |
| create the administrator before the restart | the wizard may choose another database, so the account would be written where the new server never looks |
| keep the password in `localStorage` for the reload | the repository's own rule in `credentials.svelte.ts`. `sessionStorage` survives the reload and dies with the tab |
| a list of fields written in the frontend | it drifts from the structs. The reference page already proves the structs can describe themselves |

## sources

- an earlier design proposal G2 and G6, on the machine-owned overlay
  and on the reconcile path
- earlier research,
  one configuration source
- earlier research
  section 3, and earlier research,
  the design of the first-run page
- [ADR-20260929-a4j](20260929-a4j-a-new-install-asks-for-a-one-time-setup-code.md), the setup
  code

## open items

- The beta portal keeps one documentation bundle per product. A link for one
  version needs the portal to keep a bundle per release
- The MQTT sink sends no Home Assistant discovery messages. A person adds
  each sensor in Home Assistant

- The default `-config` path is relative to the working directory. A binary
  started by a double click on macOS runs in the home folder, so it writes
  `~/config/config.d/`
- The published address of the documentation that the welcome screen links to
