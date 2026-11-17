# Changelog

Notable changes, newest first. One bullet per change, written for somebody
reading release notes rather than a diff.

Versions follow [semantic versioning](https://semver.org/). Both this
application and the `sdk` module are `0.x`, so the version number promises no
compatibility and the entry is the upgrade notice. The `sdk` module versions
separately; an entry an extension author must read starts with `[sdk]`.

How an entry is written and what earns one: `.agents/rules/changelog-discipline.md`.

News that no release has shipped yet waits in [`docs/changes/`](changes/README.md), one
file per change, and the release files it here under its own version.

## 0.1.0-beta.1 - 2026-11-17

### Security
- Go 1.26.11 now builds the server and every container image, and it fixes GO-2026-6859 in `net/http`. Building from source needs Go 1.26.11

### Changed
- An install that cannot grant a permission says the module may still ask whether a person holds it.
- Printers nearby lists a machine that runs a Prusa Connect server once, as that server. A printer that runs a server beside its PrusaLink is no longer offered under Add printer.
- The camera beside a running print on a printer's Now tab shows its last picture and plays its live view only when you press play. It says it is live only while it plays, and the larger camera window is gone.
- The camera card of a dashboard and the Cameras view of the printer list show the whole picture and play the live view, as a printer's Cameras tab does. The camera card's Picture setting is gone.
- What a camera serves, and the addresses of a generic or MJPEG camera, now show beside its picture in the Cameras tab. The Edit button is gone.
- The server log says at INFO when a printer's connection ends, and why.
- The primary button, the selected item and checked controls show white text on the brand orange, as in the Prusa Design System, and darken on hover. The PRO skin takes the design system's Connect colours in the same way.
- The fleet severity filter has no Needs hands tier: a paused printer is under Needs attention
- Checkboxes, radios, switches and sliders paint the skin's on colour. Segmented controls and tabs paint its selected colour, and progress bars its progress colour. The Slicer and PRO skins show their own colours there.
- The Official badge on Extensions is outlined, so the orange fill on a row is the Install button alone.
- The systemd unit lets the server open a port below 1024, for the time server on UDP port 123. The modules the server starts do not get that right.
- GET /api/v1/devices/{device_id}/files no longer waits for the printer. It used to wait on a folder it held nothing for, and on `refresh=true`, and failed with 409 or 504. It now answers 200 within about a second with the rows this server holds. While the printer reads the folder, the answer carries `reading: true` and `reading_since`, and `items` is empty if nothing was held. A read that failed answers 200 with `fault`, such as `core.printer/LISTING_UNFINISHED` for a read the printer did not finish. `refresh=true` starts a new read and answers the held rows at once. A client that checks only the status code reads an empty folder as an empty drive. Check `reading` and `fault`, wait for `com.prusa.connect.device.files.changed` for that folder, then ask again without `refresh`.
- GET /api/v1/devices/{device_id}/storages?refresh=true waits at most about a second for the printer. It used to wait for a busy printer and fail with 504. It now answers 200 with what the printer last said. A later answer that differs is published as `com.prusa.connect.device.storages.changed`, so a client that needs the current drives listens for that event.
- The container image carries the slicing extension in `/opt/modules/server-slicing`, with the module path `github.com/prusa3d/server-slicing`. It was `/opt/modules/slicer` with `github.com/prusa3d/slicer`, and a `core.modules_local` entry that names that pair finds no folder. Replace the entry with the new pair and switch the extension on again. It is a new module with a new data folder, so install the PrusaSlicer versions again.
- [sdk] The `type` of every error response now points to `https://beta.prusa3d.com/connect-local/docs/error-codes/` instead of `https://prusa-connect.org/docs/error-codes/`, a domain Prusa does not own. The anchor after `#` is unchanged. A client that compares the whole `type` updates the base, or compares `code`, which is the stable identifier.
- Cameras are devices kept in the server's database, and no configuration file declares them. A camera used to be a `camera` resource: a `resources.camera` block of `config.yaml`, or an entry the interface wrote into `config.d/resources.json` through `/api/v1/resources/camera/<name>`, which now answers 422. Add, change and remove one with `PUT` and `DELETE /api/v1/cameras/<name>`, and read the settings with `GET /api/v1/camera-settings`; each camera has its own version to quote. The first start of this version moves the cameras of both files into the database with their grants and logs each one. A `resources.camera` block left in `config.yaml` after that start is logged and not read, so delete it. A backup of the database carries the cameras, and a copy of the files does not.
- A camera's status events, `com.prusa.connect.resource.state.changed` on its own origin, reach whoever may see the camera. A grant on `resource_kind:camera` is still what lets a person change cameras.
- [sdk] `CameraSettings` from `@prusa/connect-ui/data` reads and writes cameras through `listCameraSettings`, `putCamera` and `deleteCamera`, so the client it is given must carry them. `declaredInFile` is gone, because no camera is declared in a file.
- The Extensions page lists only the official marketplace extensions until an organization administrator switches on Show community extensions in the marketplace settings. Installing or updating an unverified extension asks the person to accept the risk first, and every row shows its trust badge.
- [sdk] A `module.yaml` must name the licence of the module under `license`, free text of at most 200 characters such as `MIT`. The server refuses to install or start a module whose manifest has none, so add the key to your manifest before you update the server.
- [sdk] A `module.yaml` is refused when its `license`, or the `why` or `sends` of an `egress` entry, holds a control or invisible character. Such a character is a terminal escape, a reversal of the writing direction or a zero-width space.

### Added
- [sdk] The tool tree carries the material package instance a driver reports for a tool: its uuids, type, colour, full weight and profile temperatures, in `Tool.Reported`.
- [sdk] `connectorapi.ValidColor` says whether a tool's colour is #rrggbb or #rrggbbaa, and `ReadTools` reads no other colour.
- The PrusaLink-compatible xbuddy driver publishes the material package instance a printer reports for a tool, so the tool tree carries it. A spool the printer stops reporting leaves the tool tree.
- [sdk] A module reads whether a device was removed or registered again under a new id, in `deviceapi.Device.Retired`.
- [sdk] A module lists the actions it only asks access.check about under checks: in module.yaml, and the install withholds nothing for them. Materials asks so whether a person may operate a printer. The operator approves each one at install, as `check <action>`.
- `core.discovery.host_records: system` lets another mDNS responder on the machine, such as Avahi, answer for its host name. The server then announces only its services, so discovery can stay on there without the machine being renamed to `<hostname>-2.local`.
- Storage has a Shared column on a server with the Fractal module and its `files` block, and its switch shares a folder with the servers below. A card under it lists the folders that the servers above and below share.
- [sdk] A module may name `qr-code` as the icon of a page or an action. The set a manifest may choose from is closed, and until now it held no glyph for a code a camera reads.
- [sdk] A module can hand a JPEG to a push camera with `camera.accept`. The bytes travel on the call's own stream, and the call needs `operate` on the camera, the permission the HTTP push route takes.
- A generic camera type takes the pictures a camera sends, or fetches them from an address, and can also stream over RTSP or MJPEG.
- A printer's page has a Cameras tab with one card per camera. It shows the camera's last picture and plays its live view, MJPEG or H.264, when you press play, and it adds, changes and removes cameras.
- [sdk] A module can draw a section for each camera in the Cameras tab, with the new `camera-section` slot.
- [sdk] A module can offer camera types with `camera_types` in its manifest. A camera of such a type runs on the core's generic camera, and the module feeds it its pictures with no grant. The module can draw its live view in the new `camera-view` slot.
- The camera listing names each camera's type, and `GET /api/v1/camera-types` lists the types a new camera may be declared as. The Cameras card offers them.
- [sdk] The event `com.prusa.connect.camera.frame.received` tells a reader that a camera was handed a new frame.
- [sdk] `printcompat` now warns when a file for a Core One with INDX does not name the INDX lock feature. Firmware 6.6.2 and newer stops at a warning before such a print. An INDX printer on older firmware now reports the feature as unsupported
- [sdk] `printcompat` now lists the nozzle cleaner check as unverified on every Core One with INDX. The firmware refuses each print until the cleaner is calibrated, and the printer does not report whether it is
- [sdk] `printcompat` now lists the abrasive filament check of firmware 6.8.0 and newer as unverified in one case. The file names a filament type that is no firmware preset, and the nozzle is not reported as hardened. Such a filament may be one a person defined on the printer as abrasive, and the printer does not report that setting
- The web app can be installed from the browser, on a phone or a handheld scanner too. It serves `/manifest.webmanifest` and an icon, so an installed copy opens full screen. A browser installs it only from a page whose certificate it trusts.
- [sdk] An entry of `device_card`, `device_section`, `device_tab` or `device_action` in module.yaml may carry `devices: {resident: true}`, and it then shows only on the printer this server runs on. `GET /api/v1/resources` returns the block on the entry.
- [sdk] `SlotOutlet`, `SlotActions` and `moduleTabs()` from `@prusa/connect-ui/slots` take the device record, and show an entry that carries a `devices` block only for a record that meets it. An app built on the library must pass the record, or such an entry shows on no printer.
- A file dropped on a printer's page asks whether to print it now or only copy it to the printer. Print now sends the file and starts it once it lands, and it goes on when the page is closed. It is offered for one file on a printer that can start a print now; otherwise the dialog says why, in the words the print dialog uses.
- The server can answer the time on UDP port 123, so a camera or another device on a network with no internet can set its clock. It is off by default, the switch under Settings, Server, Network or `core.time_server.enabled` turns it on, and the page says why when the port does not open.
- A printer's dialog that comes with a code alone is named as Prusa Connect names it, such as File error after an upload to an MK4 is stopped. The words and the buttons under them can be translated, and the words the printer sends itself are kept.
- [sdk] connectorapi.FileChange is one entry of a device's file tree that the device said changed.
- The event com.prusa.connect.device.files.changed says when a printer folder starts being read and when its listing lands or changes.
- A printer's Files tab opens on rows already read, because the server reads each drive's root when the printer connects or a drive is put in.
- The demo takes `-listing-delay`, how long each simulated printer takes to list a folder. It answers no other command meanwhile, as a real printer does, so `-listing-delay 13s` shows what a full USB drive costs.
- The server image carries the Fractal module under `/opt/modules/fractal`, so a container can join a Fractal network with no separate build of the module. It needs its settings at install, through `prusa-connect module add -settings`, as [running in a container](src/content/docs/container.md#bundled-extensions) says.
- Removing a printer with `DELETE /api/v1/devices/{device_id}` also removes the cameras that watch it, so none keeps running with no printer page to be seen on.
- `PUT /api/v1/modules/marketplace` takes `community`, and the marketplace object it and `GET /api/v1/modules/index` answer carries it. It says whether an organization administrator chose to list the community extensions, those whose trust is not `official`, and it is false until one does.
- [sdk] A `module.yaml` can list under `egress` every connection the module makes outside Prusa Connect. Each entry names the host or the option that holds it, the ports, whether the process or the page connects, why, and what it sends. `module add` prints each one.
- Before an extension is installed or updated, the Extensions page shows its licence and every connection it makes outside Prusa Connect. A release that does not declare its connections is shown as one that says nothing about them.

### Fixed
- A module that publishes events keeps running after its settings change. The rewritten declaration put the namespace in front of each event type a second time, and the module was stopped.
- An approved update keeps the events a module was allowed to publish. It dropped them when the release asked for the same events as before.
- The printer search finds a printer by the model name the pages show, such as "Core One" for a Prusa CORE One L. A model a driver declares is found by its name too. Before, only the model code the printer reports matched.
- An RTSP camera that sends its first packets before it answers PLAY, such as a Buddy3D, now plays. The packets that came first are dropped.
- A pushed camera's new picture shows on the page when it arrives. Before, the page asked for it twice a second.
- An update, online or from an offline bundle, is no longer refused because the release's module wire contract changed, such as from 1.0.0 to 1.1.0. The server being updated runs this check, so 0.0.1-alpha.7 still refuses 0.0.1-alpha.8, and its binary has to be replaced manually.
- An online update that fails verification, or whose migration rehearsal fails, now ends with the reason on the Updates page. Stage another update right away, without cancelling the failed one first.
- A server now reads the version report of a release that adds fields to it, where it refused that release before.
- An update whose verification or migration rehearsal was stopped, by a closed page or a server restart, now continues from that step at the next Run preflight and prepare.
- [sdk] `device.print` of a file the printer already holds fails with `deviceapi.ErrPrintRefused`, code 3010, when the printer refuses to start it. A module was answered as if the print had begun. The message carries the printer's own reason where it gave one.
- [sdk] `device.print` of a file the printer already holds fails with `deviceapi.ErrPrintFailed`, code 3011, when the printer takes the start command and then fails it. A module read that the host could not answer. The printer may have begun, so a module reads its state before it starts the print again.
- The web app's left menu now reads Czech when the browser or the language setting asks for Czech. The menu entries, the Settings sections, the menu buttons, the theme choice and the connection status were English before.
- A printer's page offers Print again only while the printer still holds the file, and otherwise says the file is no longer on the printer. A stopped upload left the button pointing at a file the printer never kept, and the printer refused it.
- [sdk] A transfer's `bytes_sent` counts a byte the printer asks for again once, so a transfer's progress no longer passes 100 %. A printer that started a file over on each connection was shown at over 1000 %.
- A print start or a firmware flash sent while a printer's file pictures load no longer runs out of time. The server asks a printer for one picture at a time, so a command waits behind one picture at most.
- A confirmation that named a long file, such as Delete on a printer's files, drew the name and the buttons past the edge of its box. The name now breaks onto more lines and the box holds it.
- The confirmation for Reset printer, Stop print and Remove printer closes once the act is confirmed. It used to stay open until Cancel was pressed, even after the printer had restarted.
- Jobs no longer asks the printer for a print's picture that its file list already gave. Each visit used to ask once per print, one at a time, so the pictures came in slowly.
- Files and Jobs no longer ask the printer again for a picture they already read when the printer gives a file's time a second apart in two answers.
- A printer on the Prusa Connect SDK, such as the Prusa Pro HT90, lists its files and answers commands instead of failing with an unexpected answer
- A storage other than the default one, such as the local storage of the Prusa Pro HT90, lists its files instead of reporting them not found
- A dialog a printer raises with its text alone, such as a door opened on the Prusa Pro HT90, is shown on the printer's page
- The Prusa Pro HT90 is drawn as itself rather than as an unknown printer
- A printer's page names its model by its product name, as Prusa Connect does, such as Original Prusa MK4 or Prusa CORE One, where it showed what the printer sent
- Settings › People › Add person says a password needs at least 8 characters while a shorter one is typed, where the Add person button stayed disabled without a reason
- Settings › Server › Health says All printers away for a connector none of whose printers is connected, where it said Most printers away
- The printer menu on the Printers page offers Pause only while the printer prints, and Stop only while it prints or is paused
- On a wide screen a setting's label and its value sit side by side, where Settings › Account and Network put them at opposite edges
- Adding a printer names its model, such as HT90 or MK4S, where it showed what the printer sent, such as 6.1.0 for the Prusa Pro HT90
- A HEAD request for a stored file answers with its headers without reading the file
- With the sidebar collapsed, the Settings menu closes when another page opens, and its current entry stays highlighted under the pointer
- Settings › Server › Overview no longer says there is no newer release when the server could not check, such as before it is paired with the Beta Portal
- A file opened in Storage offers Download in its menu
- Coming back to a printer's Files tab opens the drive itself again, instead of sometimes a folder inside it that could read "This folder is empty"
- A toast stays on screen for 8 seconds instead of 4, long enough to read a command result or a reason
- A command that took a second or longer gives its time in seconds, such as "Took 18 sec" rather than "Took 18,845 ms"
- A module's menu item runs for the printer or file it was chosen for. Before, when the page showed another printer or file before the module had loaded, the item ran for that one.
- A toast waits while the browser tab is in the background, so a result that lands meanwhile is still there on return
- A command that took a minute or longer gives its time in minutes
- A server that cannot open its port no longer prints a setup code. A second copy started while another one held the port printed a code that the running copy refused with `core.auth/SETUP_CODE_REFUSED`.
- A file on its way to a printer can be stopped from the printer's Now tab, as on its Files tab
- A finished or stopped printer counts as Done in the dashboard summary, as Prusa Connect counts it, and no longer as needing attention
- Links reach 4.5:1 against the page. They take a darker step of the brand colour, where the brand orange read 3.32:1 on white.
- Every link, row and card reached with the keyboard shows the same focus ring, the footer links and the navigation rail included.
- The hint lines in settings take the small type step, one step under the title they explain.
- A pressed primary button darkens further instead of lightening, so its white text stays readable while held.
- The download button in Add printer fits a 320px screen, and the footer links are 24px tall.
- A file on its way to a Prusa printer no longer reads 100 % within a second and stays there for minutes. A Buddy board asks for the end of a G-code file first, and the progress counted how far into the file the bytes sent so far reached. It now counts the bytes of the file the printer has been sent, each once.
- A read-scoped API token no longer sees operate actions and verbs in the device list. `GET /api/v1/devices` gives each printer only the `actions` and `verbs` the token's scopes allow.
- The Remove dialog says an extension's database stays, and Databases no extension uses lists it without a reload.
- A camera with no still image shows its live view, or a short hint, instead of a broken picture, and is no longer polled for one.
- A command button shows a spinner while its command is under way.
- The switch that hides the server from the local network no longer jumps back when a read sent before the change answers after it.
- Beep on a Prusa printer plays a clear tone instead of a click.
- A switch now shows whether it is on or off by more than colour: off is an outline, and on is a filled track with a tick on its thumb.
- An extension installed or updated in Safari no longer shows "Importing a module script failed" until the page is reloaded. The browser now loads each release of an extension under its own address and tries once more when a load fails.
- Settings > Server > Overview now says when it could not read whether you may restart the server. It offers Try again instead of an empty Restart card.
- The Files tab of a printer shows the rows the server holds at once and updates them when the printer's read lands.
- The Files tab says when the printer is reading its drive, counts the seconds, and offers Retry after a failed read.
- A send, a delete or a new folder on a printer no longer makes the Files tab read the whole drive again.
- A file sent to the root of a printer's USB drive finishes seconds sooner, because the printer is asked about the file instead of the whole folder.
- Sending several files to a printer in a row no longer waits behind a background read of its drive between them.
- Opening a printer folder no longer has the printer read it again, so a send started meanwhile does not wait 12 to 15 s behind that read. The Files tab offers Refresh for a change made on the printer's screen.
- A file uploaded, deleted or created on a printer through another client, such as PrusaLink, shows in the Files tab without a read of its folder. The server applies the printer's own file events.
- File pictures load when their row comes into view and two at a time, so a busy printer no longer stalls the rest of the page.
- The print dialog shows how far a file has got on its way to each printer, then says when the printer writes it to its drive.
- A print's page no longer shows a serial number and firmware version under Origin. It showed the printer's current values, which can differ from the ones the print ran on, and the server does not record those.
- A person signed in through Synology SSO was named by their email address. The name is now read from the `username` claim, which Synology SSO sends in place of `preferred_username`.
- Settings > Account offered a password change to a person signed in at an OpenID Connect provider, who has no password on this server. The page now shows their name and hides the password change.
- On a phone, the menu now closes when a page in it is tapped, and the bottom bar stays on screen on a long page
- The `install.sh` that `make new-module` writes runs pnpm from inside each project, so corepack uses the pinned pnpm version. It failed with `ERR_PNPM_BAD_PM_VERSION` when corepack defaulted to another major version.
- Under the systemd unit from `prusa-connect.service`, the server now announces itself over mDNS on its network addresses. The first-run page and the log banner now offer those addresses too
- A typo in the configuration file now stops the server with the key and its line alone. The message no longer starts with "recover interrupted backup restore"
- The setup wizard stays on its page after the restart when the server is reached through another port, such as a published container port or a proxy. It went to the port the server listens on, which the browser could not reach.

## 0.0.1-alpha.8 - 2026-11-06

### Security
- The Fractal module is built with `golang.org/x/crypto` v0.56.0 and `github.com/klauspost/compress` v1.18.7, which fix GO-2026-6355, GO-2026-6354, GO-2026-6303 and GO-2026-5841. None of them was reachable from the module's code.
- A Fractal server refuses a join credential at the second its `exp` claim names. It accepted the expired credential for that one second.
- The NATS permissions of a Fractal link allow only the JetStream streams of that link. They allowed every stream and consumer in the domain of the server below, and acknowledgements on any stream.
- [sdk] `join.Parent` keeps a node revoked when an operator revokes it while its join request is checked. It granted that request and admitted the node again until a restart, and a Fractal server did the same for a revocation just after the checks.
- [sdk] `join.Parent` no longer loses a revocation that is saved while the first connection of that node is saved. The connection undid the revocation in memory and in the admissions file, and the node could join again.

### Removed
- The slicer's `memory_mib` setting is gone. A slicer run is now bounded by the memory budget of the module, and you set `memory_mib` on the module's resource instead. An install or an update drops the old key from the slicer's settings by itself. A slicer declared in `config.yaml` keeps the key and fails to start with `RESOURCE_PLUGIN_SETTINGS` until you delete it there.
- The Fractal settings `shadow.max_interfaces` and `shadow.max_props_bytes` are removed, and a server keeps every report from the servers below. A configuration that still sets either key is refused, so delete both keys before the upgrade.
- [sdk] The limits of `shadow.Store` are removed: `Config.MaxInterfaces`, `Config.MaxPropsBytes`, `Store.MaxInterfaces`, `Store.MaxPropsBytes`, `DefaultMaxInterfaces`, `DefaultMaxPropsBytes`, `LimitError`, `ErrTooManyInterfaces`, `ErrPropsTooLarge`, `OutcomeRefused` and `Result.Err`. A program that set or read them deletes those lines, because `ApplyREP` now applies every report it selects.

### Changed
- A file changed in place in a disk storage, by a copy or an editor, no longer reports the SHA-256 of its earlier content. The storage API and `storage.stat` leave the digest out until the server writes the file again.
- A Fractal server takes the file of `PrintFile` and `UpgradeFromFile` only as a URL with a scheme, so a server above sends `file:///usb/benchy.bgcode` where it sent `usb/benchy.bgcode`. A value with no scheme now gets the error `0x4015` and never reaches the printer. A server of an earlier release reads `file:///usb/benchy.bgcode` as an unsupported scheme, so update every server of the network together.
- Modules installed before this release run under the 128 MiB default. A module that needs more is stopped and switched off after the upgrade until you set its `memory_mib`.
- A module's process now gets only a short list of environment variables: the path, the home and temporary folders, the time zone, the locale and the proxy settings, plus `PCL_MEMORY_BUDGET_BYTES`. No other variable of the server reaches it.
- A restart the server was asked for now happens even when a module did not stop in time.
- The server settings no longer show the Updates and Backups tabs where `core.upgrade.enabled` is false, and the overview names the version without offering an update.
- A module that shows more than one page in the sidebar now gets one row under its title, with its pages listed below it. The row opens the first page, and the phone menu lists the pages under the module's title.
- Lists, cards, tabs, links, spinners and page buttons look the same on every screen.
- Every listing puts its search, filters, sort and view switch in one toolbar that wraps the same way at every width.
- Spinners stand still when the system asks for reduced motion.
- [sdk] The module lint preset `@prusa/frontend-standards/eslint/module` refuses a raw control such as `<button>`, `<input>` or `<select>` and names the primitive to use. An element no primitive covers takes an `eslint-disable-next-line svelte/no-restricted-html-elements -- reason` comment above it.

### Added
- [sdk] A new `sdk/fractal/files` package reads and writes the identity of a Fractal file and the `fractal` URL that names it. It uses the standard library alone, checks every part of the rule for names except Normalization Form C, and `files.ParseURL` leaves out a `name` that breaks the rule.
- [sdk] `files.Listing` encodes a directory listing in the deterministic CBOR of the Fractal file distribution extension, and `files.DecodeListing` reads one. The reader refuses a listing of any version but 1, and any form but the shortest.
- [sdk] `files.ParseJoinEntry` reads the `files` entry of a Fractal grant, and `files.Definitions` holds the `ContentStore1` interface for `schema.Load`. A grant whose base URL is not `https` is refused unless cleartext is allowed.
- [sdk] A module lists one folder of a server storage with `storage.list`, under the permission `storage.read` takes. A page holds at most 1000 entries in the order of their names, and a module names `capability: storage.list` in `requires`.
- [sdk] A storage provider sets `FileInfo.Irregular` on a link or a special file it lists, and `storage.list` leaves such an entry out.
- [sdk] A module with a data directory prints a file from its own `print/` folder by naming the storage `module:data` in `device.print`. Nobody else sees that folder, and a second print of the same unchanged file onto the same name sends nothing while the server runs.
- [sdk] `device.print` refuses a print onto a name the printer already holds with `deviceapi.ErrNameTaken`, code 3009, where a module got a failure of the host before. Nothing is sent, so the module may print the file under another name.
- A Fractal server with a `files` block in its settings offers the file extension in every grant, and refuses a server above that does not offer it. `files.allow_cleartext_parent` lets it take a server above whose files address is `http`.
- The Fractal module shares a folder of a server storage with the servers below with `PUT /files/shares/{storage}/{path}`, and stops with `DELETE` on the same address. `GET /files/shares` lists the shared folders, a share takes `read_storage` on its storage, and the root folder of a storage cannot be shared.
- A Fractal server with a `files` block publishes each storage that holds a shared folder as a `ContentStore1` content store. The servers below see only the root hash of the shared folders and the path to them, and the content store stays after its last folder is unshared.
- The Fractal module asks for the TCP port `files` (8443 by default) and a data directory. It runs on any server that offers `storage.list`, an alpha of 0.0.1 included, once the operator grants both.
- A printer on a Fractal server prints a file a server above shares, named by a `fractal:///sha256:...` URL in `PrintFile`. Other servers fetch it over HTTPS from the server above and may keep a copy, dropped with one log line on a revocation or a move in the tree.
- A Fractal server that passes a file from the server above to a server below logs one line at info with its identity and the bytes sent. When the server above answers with a failure, such as `404`, the line gives that status instead.
- A printer on a Fractal server prints a `fractal` URL even when it already holds a file of that name. The file then lands under its name plus the first 12 digest characters, such as `cube-7FcFH9hw1lY1.bgcode`, and nothing prints when both names are taken.
- A Fractal server does not print a file the server above gives a retention other than unlimited, and logs the print as failed with the retention it got. Only a server of another implementation sends such a retention.
- A fetch for a `fractal` URL is tried again up to `files.fetch_attempts` times. A print that has not started within `files.start_deadline` is dropped, and every print that does not start leaves one log line.
- The Fractal module lists the content stores of the servers above and below with `GET /files/stores`, and reads one listing of them with `GET /files/stores/{node}/{instance}/{identity}`. A server with a `files` block keeps the content stores of every server below in its copy of their state.
- [sdk] A link module may ask `access.check` about `node:<id>` for a server it reported with `link.changed`. Any other server is still refused as a subject the host did not serve.
- A Fractal server refuses a `PrintFile` with a `fractal` URL at once when the server above may not operate the printer or read the storages.
- A Fractal server that the server above answers with `401` for a file joins the server above again and asks once more. It does so at most once per `files.proof_tolerance`, logs both clocks when they differ by more than that, and stops dialing a server above that revoked it.
- [sdk] `Joiner.Renew` of `sdk/fractal/join` keeps the credentials of a membership the node already holds current. Unlike `Joiner.Run`, it does not join at once.
- [sdk] A module may recommend a memory budget with `needs.memory_mib` in `module.yaml`, from 16 to 8192 MiB, and reads the budget it got from `PCL_MEMORY_BUDGET_BYTES`.
- Each service module now has a memory budget of 128 MiB unless its author or the operator sets one. The operator sets it with `memory_mib` on the module's resource, and `core.modules_memory` sets the pool that the budgets are compared with. A new `memory_mib` restarts the module.
- The pool only warns. Installing, switching on, raising or upgrading a module is never refused because the budgets pass the pool, and the Extensions page shows the warning. `GET /api/v1/modules/memory` reports the pool and the sum of the budgets.
- The Extensions page shows the memory a module will ask for before you switch it on, and installing or upgrading a module asks again when a new release recommends more.
- A service module that uses more than its memory budget is stopped and restarted. Set `on_over_memory: warn` on the module's resource to keep it running and only log a warning. On Windows the module still stops at its hard limit of the budget plus 25 %. The server reports how much memory each module uses and why it last stopped.
- A service module that fails three times within an hour is now switched off, and it stays off across restarts until the operator switches it on again. A module declared in `config.yaml` is held off until the next server start. `core.modules_switch_off` sets other numbers for every module, and `switch_off` on a module's resource or its Memory tab sets them for one module. `failures: 0` never switches a module off.
- `make new-module` asks how much memory the module's process needs.
- [sdk] `connectorapi.ListingRefused` and `connectorapi.ErrListingRefused` report a device that refused to say what is at a path. A driver that refuses a listing raises them, and any other error still reaches a caller as 500 `core.internal/UNEXPECTED`.
- A Fractal server reports the speeds of the print fan and heatbreak fan of a printer's head, where it reported null.
- A Fractal server reports which slot of a printer's multi-material unit is loaded, in `activeSlot` of `FDMPrinter1`, where it reported null.
- A Fractal server reports the nozzle diameter and high-flow flag of a printer's head, which it reported as null.
- A Fractal server reports the `uuid` of a printer in `Device1`, where it reported null.
- A Fractal server shows `FDMPrintJob1`, whose `filamentUsed` is the filament a print has used. It has a value only from a driver that reports it, such as the Moonraker example for Klipper, and never from a Prusa printer.
- A Fractal server shows each print head of a printer with more than one, such as an XL, as its own `FDMPrinterHead1`, numbered from 1. `LoadFilament`, `UnloadFilament` and `Extrude` on a head the printer has not picked get `0x2015`.
- A Fractal server shows each slot of a printer's multi-material unit as a `FilamentSlot1` with the filament loaded in it, numbered from 1.
- A module can show the Prusa Connect app's materials icon beside its page in the navigation.
- The server image carries the materials extension under `/opt/modules/materials`, which keeps the OpenPrintTag material catalogue and the spools in stock.
- Every print that ends keeps how it ended, as `outcome` on `GET /api/v1/jobs`, on `GET /api/v1/devices/{device_id}/jobs/history` and on the jobs read: finished, failed, cancelled or unknown. A print that ended before this release reads unknown.
- `GET /api/v1/jobs` takes `from` and `to`, which narrow the page to the prints that ended inside them, so a report reads a period of the whole fleet in one paged request.
- [sdk] `deviceapi.Job` carries `Outcome`, the same word, on `device.jobs` and `device.job_history`.
- [sdk] Every module built with this SDK answers plugin.ping, the lifecycle contract is 1.1.0, and a module built against it needs a server of this release or later.
- A service module that stops answering for about 45 seconds, or whose calls time out for two minutes, is restarted with the reason not-responding. The stop counts toward switching it off.
- The API serves a service module's memory over the last hour with its recent stops, and its recent standard error to callers who may configure it.
- A service module's page on the Extensions page charts its memory over the last hour, with its budget and its recent stops.
- A module's page has a Log tab with its recent standard error, for people who may configure the module, and a Restart button.
- The Extensions listing shows the memory pool as a bar, each module's memory in MiB, and the modules the server switched off.
- The bell notices you when the server switches a module off, and a module's state now gives a reason, such as "Hung and was restarted".
- The `-safe` flag starts the server once with every module off.
- In safe mode modules declared in config.yaml are held off too, and a request that would start a module answers SAFE_MODE.
- `POST /api/v1/server/restart` restarts the server normally or in safe mode, for an organization administrator.
- `GET /api/v1/ui` answers `safe_mode` while safe mode is on.
- Settings → Server → Overview has Restart in safe mode, and every page shows a banner with Restart normally while safe mode is on.
- A Fractal server that revokes a server below deletes its copy of that server's state and the commands that wait for it.
- [sdk] `node.Node.ChildRevoked` deletes the shadow of a node below that this node revoked, and `fractaltest.Recorder` records the call for `Recorder.Revoked`. A transport calls it after `ChildLost`.
- `GET /api/v1/ui` reports in its new `upgrades` field whether the server prepares upgrades.
- [sdk] `connectorapi.Dialect` has `Resident`, for a driver that reaches only the machine the server runs on. The host then refuses to retire that driver's devices.
- [sdk] The event stream sends `com.prusa.connect.stream.origin.withdrawn` when a reader loses sight of an origin it followed, such as a removed extension, a removed printer or a lost dashboard.
- The file lists of the storage page and of a printer tick a range of files on a shift-click. A box in the table header ticks every file on the page, and "Select all" ticks every file of the folder or search.
- A printer read through `GET /api/v1/devices` and `GET /api/v1/devices/{device_id}` lists in `verbs` every command the caller may send it. A command such as `core.command.flash` is listed only for the printer's owner or a caller given it by a grant, since no role gives it.

### Fixed
- `GET /nodes` of the Fractal module names the place in the tree the server holds now. It named none for a server that joined after it started, and the former place after a move.
- A Fractal server that refuses the grant of the server above, for a protocol version or a `files` entry it does not support, stays unconnected until restarted. Its uplink then carries the code `GRANT_REFUSED` and a reason that names the cause, where it ended and asked the server above again and again.
- [sdk] A node of `sdk/fractal/node` answers every RBR, the request for a full state report, also while its work queue is full. RBR messages that wait for an answer collapse into one full report.
- A Fractal server no longer misses the state of a server below it that has just connected. It asked for that state before the server below listened, and the request was lost.
- A Fractal server below another one stops at once. It waited up to 2 seconds for a read of the requests sent to it from above.
- A Fractal server keeps its link to the server above when it starts or restarts. It joined again right after it connected, and the grant of that join closed the link it had just opened.
- A Fractal server stops at once while it waits for a server above that accepts a connection and never answers. It waited up to 15 seconds.
- A Fractal server sees every break of the connection of a server below it, however short, and asks that server for its full state when it is back. It missed most breaks that lasted about one second.
- A Fractal server passes a change of its printers' state to the servers above within about a second. It passed it only every 30 seconds, so a `PrintFile` sent right after `SetReady` was refused with `0x2013` by a printer that was ready.
- Stopping a module now also stops the processes it started, and the server no longer hangs on shutdown when one of them is still running.
- `GET /api/v1/devices/{device_id}/files` now answers 409 `core.printer/LISTING_REFUSED` with the printer's own reason when the printer refuses to say what is at a path. A printer in ERROR refuses every listing, so opening its Files tab answered 500 `core.internal/UNEXPECTED`, which looked like a fault in this server.
- Settings → Server no longer counts retired printers. A connector serving one live printer is now reported as healthy, where before it was reported as failing because of registrations the Printers page does not list.
- A new API credential says under the field what is wrong with it: a missing name, or a lifetime outside 1 to 365 days. The Create button was disabled instead, with nothing saying why.
- Closing the "New API credential" dialog now clears what was typed in it.
- The print dialog's Print button is now disabled when no ticked printer can start a print. It stayed enabled next to the line saying no printer can start one.
- A file thumbnail no longer opens its preview when a dialog or a redrawn row arrives under a pointer that has not moved. A print dialog opened under a resting pointer was covered by the preview of the file it was about to print.
- A printer question the firmware sends with no text now says the question did not arrive and gives the printer's code for it. Somebody away from the machine is no longer asked to answer a question they cannot read.
- A printer question's button labels are shown as words, not in the firmware's `Purge_more` spelling.
- The History tab chart legend leaves out a reading remembered from an earlier version that nothing can draw. Such a reading also no longer makes the next reading you turn on replace the chart instead of joining it.
- A Fractal server reports the nozzle temperature and target of a printer's head, which were always null.
- A Fractal server reports the target of a bed or nozzle heater that is off as null, as the vocabulary says, where it reported 0.
- A Fractal server reports the filament loaded in a printer's head, or in the multi-material unit slot loaded into it, where it reported null.
- A printer that is offline, or that the server no longer lists, shows as `BLOCKED` to a Fractal server above, which then cannot start a print on it.
- A printer with a dialog over a running print, such as at a filament runout, shows as `PAUSED` to a Fractal server above, where it showed `BLOCKED`.
- A Fractal server refuses a method call in a state the vocabulary forbids, such as `Home` while the printer prints, with `0x2013`, and sends nothing to the printer. It refuses `LoadFilament` of `NOTHING` with `0x4013`, and an `Extrude` with no temperature to heat to with `0x2014`.
- A Fractal `Extrude` with no feedrate moves the filament at 27 mm/min, where the printer refused it. A heater target or a feedrate with a fraction is rounded to a whole number, where the printer refused it.
- A Fractal server reports the job id, progress and print times of a printer as null while no print runs, where it kept the values of the last print.
- The links page shows when the server last heard from each Fractal server below it, where it showed the time of the last change of the link, usually the join.
- A Fractal `LoadFilament` of `TPU` loads what a Prusa printer calls `FLEX`, where the printer stopped and asked on its screen which filament to load.
- A Fractal change of several printer properties at once, such as `targetBed` and `flow`, sends them in the order of their names. The order, and so the result, changed from one request to the next.
- A Fractal `Home` with every axis false succeeds and moves nothing, as the vocabulary reads, where it was refused as a call with a missing parameter.
- A Fractal server above holds the latest value of a printer property after it asks for the full state. A change report sent at the same time could leave an older value there.
- A Fractal call for a printer that this server stops holding while the call runs gets `0x4042`, the code for a printer that is gone. It got `0x4043`, the code for a printer this server never held.
- `backup restore` run from another directory than the server's put the databases into that directory, and the server kept its migrated ones. A backup now records each database by its full path, as it records the files, so a restore puts it back where the server reads it.
- A history query for a window older than the store keeps no longer opens its circuit breaker. A page that asked for a few such windows took history away from every caller, who then read `core.internal/UNEXPECTED` from `GET /api/v1/history/facts`. While a breaker is open, history reads now answer 503 with `core.internal/RESOURCE_BREAKER_OPEN`, which a caller may retry.
- On Windows, every backup failed with "Access is denied", so an update could not start either. A backup no longer asks Windows to flush a folder, which it refuses.
- Settings, Backups asks about PostgreSQL databases outside the backup only when there are such databases.
- A printer folder that took longer than 30 seconds to list never opened, because each retry from the page asked the printer to start the listing again. A retry now waits for the listing the printer is already sending, and the next visit shows it at once.
- A Fractal call that expired, or that names no server below, gets its error answer after a failed send of that answer. The server took the call as done and the caller got no answer.
- A Fractal server logs a message it drops for an unsupported protocol version, a sender that fails the check or a receiver it cannot reach as an error. It logged a warning.
- A Fractal server that the server above denies logs the denial with its code. A wait for an operator and a busy server above are warnings, and any other denial is an error.
- A Fractal server that the server above rejects or revokes keeps serving the servers below, and asks again only after an operator deletes its `refusal.json`. It restarted and asked again immediately.
- A Fractal server with servers below it refuses a grant from a different server above, serves the servers below without a link above, and keeps that refusal across restarts. It restarted and asked that server again immediately.
- [sdk] `join.Joiner` sends no further join request after it refused a grant, and sends one request at a time when `Join` and `Run` overlap. `JoinerConfig.OnGrant` now runs during that one request, so it must not call `Join`.
- A Fractal server that refuses a grant for an unsupported protocol version, a `files` entry or a missing field serves the servers below and keeps the refusal across restarts. It stopped serving them, and a restart asked again.
- A Fractal server above keeps the newer value of a printer property that changed while this server read the full state for it. It could keep the older value until the next change.
- [sdk] `join.Parent` grants one Node ID per identity and one identity per Node ID also when it checks several join requests at the same time. Requests that overlapped could give one identity two Node IDs, or one Node ID two identities.
- The Fractal setting `accept.max_joins_in_flight` takes effect: a server checks that many join requests at the same time and answers the rest as busy. It checked one at a time and kept the rest waiting.
- A Fractal command or result that waits for a disconnected server is no longer deleted when its queue reaches `stream_max_bytes`. The queue refuses the new one, and the waiting ones stay.
- A Fractal server counts a server below as lost when its heartbeats stop, also while its state reports still arrive. The links page shows the time of the last heartbeat.
- [sdk] `join.Parent` keeps one Node ID per identity when it cannot issue the credentials of a new node. That identity could ask for a second Node ID and held both after a restart.
- Behind a reverse proxy, the web app stayed offline after a short server restart until the page was reloaded. While the server is down, a proxy answers 502, 503 or 504. The event stream of the web app and of the TypeScript client now reconnects after such an answer, the same as when the server gives no answer at all.
- A file sent to an idle Prusa printer with a screen no longer fails with `core.printer/TRANSFER_NOT_ON_STORAGE` when it arrived whole. The printer opens the file in its print preview, and the transfer now finishes with a warning that the file cannot be deleted or replaced until that screen is closed.
- A Prusa printer's refusal to delete or replace a file it still holds from a transfer now says to close the print preview or the end-of-print screen on the printer.
- A server with `core.auth.issuer` set and `core.principal` left at `operator` now logs a warning at start-up, because nobody who signs in at the provider administers it. The warning also says the local login is refused, and the sign-in log line now names the `subject` that `core.principal` takes.
- Opening the Print dialog no longer covers it with the enlarged picture of the file. The dialog now starts on the first printer that can take the print, and the picture opens only on hover, a tap or the Tab key.
- With the sidebar collapsed, the Settings icon opens a menu of every settings section, where it led only to Account. Any other sidebar item with sections does the same.
- A button that shows only an icon, such as the theme switch, shows its name in a tooltip on hover and on keyboard focus.
- A module is no longer switched off at start-up while its database, or a module it requires, is still starting. Such a start now keeps retrying and does not count towards `core.modules_switch_off`.
- The server no longer stops at start-up on a read-only configuration folder when `core.overlay` and `core.authz.database` point outside it, as in the Kubernetes manifest. A new Beta Portal pairing sits beside `core.overlay`, and one that release 0.0.1-alpha.7 kept in `config.d` stays in use while the server can use it.
- `core.upgrade.enabled: false` now switches upgrades off: the server prepares no upgrade, keeps no backup of its own and writes no Beta Portal pairing. The first start with upgrades off cancels a scheduled upgrade, and the server version and the restart keep working.
- Removing a printer that runs this server brought it back at once under a new id, with no name, no tags and no grants. `DELETE /api/v1/devices/{device_id}` now refuses such a printer with 409 and `DEVICE_RESIDENT`, and its device record has `resident: true`.
- The printer card offered Remove printer to every reader, and the printer page offered it for the printer that runs this server. Both now offer it only to a reader who may administer the printer, and for a printer that runs this server the printer page says why it cannot be removed.
- Settings > Server > Network lists the announced IP addresses one per line, wrapping long IPv6 addresses, instead of one line that runs off the page.
- A Prusa printer that upgrades its firmware shows the new version on its page and in `GET /api/v1/devices/{device_id}` once its `INFO` reports it. The printer reconnects once, and commands queued for it at that moment fail with `PRINTER_CHANGED`.
- A printer that has reported before and is not reporting right now no longer says it has never reported to this server.
- On a bridge network, the Overview card for the network says "Inside the container only" in a warning tone instead of a green "Announced". The network tab no longer says that slicers on the network can find the server.
- The notifications bell shows a printer issue as the sentence from the device-issue vocabulary, not as its raw code, and links to the printer. A reason the vocabulary lacks gets a plain sentence.
- [sdk] The API reference page leaves its row of document links out when the address carries `embedded=1`. A browser on plain HTTP does not say a page is framed, so the frame showed a `Core API` link that opened a page refusing the frame.
- Settings > People masks the password typed into the Set a password dialog, as the Add person dialog does.
- On a Docker host the server no longer announces itself on the bridges and container cables Docker makes. Failed mDNS joins now log one warning, not one per interface.
- Clicking the option already picked in a one-of-several switch, such as High flow or Hardened under a printer's Nozzles, keeps it picked and sends no command.
- The result of the Hardened switch reads "Set tool hardened", after the switch, where it read "Set tool hardness".
- Every list with a search or filters offers a clear button in the search box, Clear filters while a filter is on, and Clear search and filters when nothing matches.
- An extension offered from a local index now shows the ports, the data folder and the database it asks for before you install it.
- The install dialog names the extension's data folder and where it is, and the install and Remove dialogs say that removing the extension keeps that folder.
- After you install, remove, switch on or off, update or configure an extension, the page shows what the server now holds, even after an error or while another read runs.
- Removing an extension or a printer, or losing access to a dashboard, now shows in every open tab without a reload, and its open page says it is not here.
- A printer that did not answer the start of an upload in time could stay on "Downloading 0 %" and resume that download after every restart. The server now tells the printer to stop the download, so the printer takes the next file.
- Flashing a firmware file from a printer's file list answered "Unknown command" and left the printer on its old firmware. The server now sends the flash in the form the printer reads, and does not send it while the printer is printing.
- A search in the storage page clears the ticked files, so Download and Move act only on files the reader can see. A press on a printer's file tile no longer ticks it, as on the storage page.
- Download on ticked storage files says how many files arrive, as a folder has none to serve. A bulk delete on a printer counts the files and the folders it takes.
- On a server that asks nobody to sign in, nobody could flash, update or rename a printer, because a printer that registers on its own had no owner. Such a server now makes `core.principal` the owner of each printer without one when the printer connects, including printers it already knows.
- Once a server that was open turns on sign-in, a printer `core.principal` owns moves to a new id, without its history, when someone else approves it. Withdraw that owner first to keep the id.
- A printer's Files tab no longer offers Flash to a person the server would refuse it, such as an administrator who does not own the printer. The other printer screens show a command the person may not send as unavailable.
- `GET /api/v1/grants/subject/{subject}` no longer lists a command such as `core.command.flash` that the server refuses the person. A grant for such a command reaches nobody who lacks the class it needs, and `verbs` on a printer now says the same.
- Signing out of a server with local accounts now ends the session or token the request carries. Before, only the browser's cookie was dropped, and a copy of it kept working until it expired.
- The host database, which holds the accounts, credentials and grants, is now readable by the server's own user only, with the two files SQLite keeps beside it. A file an earlier version left readable to others is narrowed when the server starts.
- The SQLite files of the history and records stores are now readable by the server's own user only, like the host database.
- Cancel on Updates and the cleanup after an upgrade now remove a staged release only inside the staging folder, never another folder a damaged journal names.
- A refused delete or replace of a file a Prusa printer still uses now says the printer kept the file, not "File is being transferred", which testers mistook for success.
- A print that has ended now publishes one job.settled event, even when the printer reports that print again before it goes idle.
- Stopping an upload to a Prusa printer no longer leaves a job that never printed, and the Jobs list no longer offers Print again for it.
- [sdk] The built-in upload allowlist now holds `.bbf`, the extension of Prusa firmware files, where it held `.bff` by mistake. A `.bbf` upload to a storage no longer answers 415, and a storage that sets its own `allowed_extensions` lists `.bbf` instead of `.bff`.
- A Prusa firmware image (`.bbf`) can now be uploaded to a printer from the Files tab and the Now tab. It lands at the top of the USB stick, where Flash can use it.
- A Prusa printer with no tool selected no longer shows a flow of -100% on its Controls tab, and its flow is shown as unknown. An MMU with no filament loaded leaves no tool selected, and Buddy firmware then reports a flow of 0.
- Refresh on the Prusa printer logs page now shows the log lines and metrics that arrived since the page was opened, and it leaves live mode as it was.
- A printer that comes back on another firmware or as another model is counted once in `pcl_printer_info`, under its new labels. The series under the old labels stayed until the server restarted.
- On a printer's Now tab, the "Finishes" time is counted from when the time left arrived. A page opened before the print started counted it from when the page was opened, so the time read early until a reload.
- A sink hears `printer.state_changed` when a printer's state changes, not on every telemetry frame. A webhook, broker or script sink was told the same state about every two seconds, so a script that alerted on `ERROR` alerted again on each frame.
- On Printers, the retired list is headed "Retired printers" and offers no "Add printer". It was headed "Printers" like the fleet, and a printer added from it joined the fleet.
- A server started before its Postgres accepts connections waits up to 45 seconds for it, and logs each try. It exited on the first refused connection, so a server started together with its database by compose or Kubernetes restarted in a loop until the database was up.
- A file sent to a Buddy printer reports its progress as it goes, every 64 KiB. The printer asks for the whole file at once, and progress was reported only when that request was done, so a send stayed at 0 % until the file had arrived.
- In the setup wizard, a configuration file that gives the printers' listener a certificate shows "Encrypt printer connections" on, and the summary names printers under Encryption. Both said encryption was off, although the server started with it on.
- A printer of this server that is switched off and on again is no longer offered under Printers nearby while it reconnects. `GET /api/v1/discovery/nearby` names a printer that is away in `device_id` by the address it last connected from.
- The restart after a first-run wizard that chose open mode no longer asks for a first administrator. It printed a setup code, and the web app showed a setup banner, although an open server has no administrator. `GET /api/v1/auth/info` answers as on any open server.
- The Add printer page no longer says this server refused a printer once a code is announced or approved after the refusal. A printer that is still refused shows the refusal again on its next call.
- A server whose machine refuses every mDNS packet it sends now says so. The log warns once and names the macOS Local Network permission. `GET /api/v1/discovery/visibility` answers `announcement: "failed"` instead of `"announcing"`.

## 0.0.1-alpha.7 - 2026-10-29

### Security
- The server and every container image are built with Go 1.26.10, which fixes GO-2026-6809 in `crypto/x509`. Building from source needs Go 1.26.10
- Login attempts from one address now take turns, so opening many connections at once no longer gets more password guesses past `POST /api/v1/auth/login`. Attempts that arrive while the table of recorded accounts or addresses is full are now held too.
- Every login attempt that is held or refused, and every wrong password, is now written to the server log as a warning with the username and the client address. A run of refused attempts from one address is written at the first, second, fourth and each doubling refusal, with the count so far, and the password is never logged.
- `POST /api/v1/auth/login` now answers `429` with the new error code `core.auth/LOGIN_LIMITED` and a `Retry-After` header when the client address already has too many attempts waiting. No password is checked for such an attempt, and one sent after the given wait is let through.
- A correct password no longer resets the login count of the client address it came from. The address goes back to the shortest wait and keeps its queue. Logging in to one account between guesses at another no longer gets around the limit.
- A module route no longer receives the cookie or token of the person who calls it, and a module can no longer set cookies or the server's security headers. A call from one module to another passes no credential either. See [ADR-20261028-7be](decisions/20261028-7be-a-module-route-sees-only-the-headers-the-host-passes.md).

### Removed
- The device-model vocabulary no longer lists Klipper in a build without the moonraker driver, and its published set moves to version 2.0.0. A build that composes the driver serves the row, with a drawing, from the driver's own declaration.
- A printer's state no longer carries `/material`, `/temperature/nozzle`, `/temperature/nozzle_target` or `/active_tool`. A machine with one nozzle reports it as head `0` of the tool tree, at `/tools/0/nozzle_temp`, `/tools/0/nozzle_target_temp` and the head's material. The printer's first report after the upgrade removes the old paths.
- `active_tool` is no longer in `tools` of `GET /api/v1/devices`. Each tool carries its own `active`.
- [sdk] `connectorapi.MaterialPath`, `NozzleTempPath`, `NozzleTargetTempPath` and `ActiveToolPath` are removed. A reader reads the tree, and `ToolTree.Active` derives the tool printing now from each tool's `active` member.
- The MQTT topics `data/material`, `data/temp/nozzle/current`, `data/temp/nozzle/target` and the three `data/slot/...` topics are gone from the topic list. The core never published them, and the tool topics below replace them.

### Changed
- The setup wizard says that the network discovery switch finds the printers that have PrusaLink turned on.
- The list of printers nearby moved from the Info tab of a printer's page to the Printers page, under the listing. It leaves out the printers you already see here and other Prusa Connect servers, and links each printer to the Add a printer guide.
- The Fractal module answers `PrintFile` and `UpgradeFromFile` with invalid params when the file is a URL with a scheme, such as `http://`, and sends the printer nothing. The code is `0x4015`, a refinement of `ERR_INVALID_PARAMS` of its own, and the message asks for a path on the printer. A path on the printer's own storage, such as `usb/benchy.bgcode`, works as before.
- [sdk] The filament material enumeration of the Prusa interface files in `sdk/fractal/prusa` is named `FDMMaterial`, as the Fractal specification names it, and no longer `FDMaterial`. Code that looks the enumeration up by name in the registry those files load uses the new name.
- [sdk] An interface definition file must state its exposure, and `sdk/fractal/schema` no longer loads a file without one. Add `"exposure": "upward"` beside `"interface"` and a node reports the interface to the node above, as before. The other values are `"downward"` and `"both"`. `node.Instance` has an `Exposure` field, and `Register` refuses an instance whose exposure differs from its definition.
- [sdk] A Fractal node answers a call to an interface instance or a method it does not have with `fractal.CodeObjectNotFound` (`0x4040`, `ERR_OBJECT_NOT_FOUND`). It gives this answer before it checks the parameters, where it answered `ERR_INVALID_PARAMS`. An owner with no interface definition returns that code for a method it lacks, as `fractaltest.StateOwner` now does.
- The Fractal module answers a call for a printer it does not hold, or no longer holds, or for a method the printer lacks, with a refinement of `ERR_OBJECT_NOT_FOUND`. The three codes are `0x4043`, `0x4042` and `0x4041`. A server above that matched `0x4022`, `0x4021` or `0x4011` has to match the new numbers.
- The Fractal module signs its join request with the header `typ: fractal-join+jws`, and denies a join request without it with `ERR_JOIN_REJECTED`, which the server that asked does not retry. A server on an earlier release can no longer join an updated server above it. An updated server still joins an earlier one. Update a network from the servers furthest from its top server to the top server last.
- [sdk] `fractaltest.Engine` has the methods `ParentConnected` and `ParentLost`, and `fractaltest.Link` calls them on the engine below when it connects, restores and cuts. An engine a test passes to `fractaltest` needs both, which `node.Node` and `fractaltest.Recorder` already have.
- [sdk] `node.Owner.Snapshot` returns the time the owner determined the values, never the time a value last changed. An owner that returns values it holds returns zero, and the node stamps the report with its own clock.
- A file now leaves the uploads list as soon as it ends, whether it finished, failed or was cancelled, and there is no button to clear it manually.
- A printer's tools move from `/slot/<n>` in the state tree, counted from 1, to the tool tree under `/tools/<id>`, counted from 0 as G-code counts them, and `/slot/active` becomes `/active_tool`. A client that read `/slot/2/temperature` reads `/tools/1/nozzle_temp`, and a page labels the heads `T0`, `T1`. The old `/slot` paths go with the printer's first report after the upgrade.
- The `tool` argument of `core.command.set_nozzle_temperature`, `core.command.set_tool_nozzle_diameter`, `core.command.set_tool_hardened` and `core.command.set_tool_high_flow` now counts print heads from 0. It took 1 to 5 before and takes 0 to 7 now, the head id under `/tools/<id>` in the printer's state. An API client that sent these commands sends one less than it did, and the eighth head of an INDX can now be set.
- A file dropped on a printer page is no longer kept in the server's storage. It goes up as a temporary copy that the server deletes once the file is on the printer.
- `/tools/<id>/nozzle_target_temp` is 0 while that heater is off, where the path was removed, and `nozzle_target_temp` in `tools` of `GET /api/v1/devices` reads `null` then, as the architecture writes it. An absent target is a printer that did not say.
- [sdk] The tool a G-code `T<n>` selects moves from `/tool_mapping/<n>` to `/gcode_tool_map/<n>`, and `tool_mapping` in `tools` of `GET /api/v1/devices` is now `gcode_tool_map`. `connectorapi.ToolMappingPath`, `SplitToolMappingPath` and `ToolTree.Mapping` are now `GCodeToolMapPath`, `SplitGCodeToolMapPath` and `ToolTree.GCodeToolMap`.
- [sdk] `connectorapi.Feeder` carries `SlotCountKnown`. A feeder whose slot count nobody reported is unknown, and the API leaves it out where it read it as no feeder.
- The header lists every file on its way to a printer the person may read, whoever sent it and from whichever browser. A file somebody else sent says so, and a person who may stop it can stop it there.
- [sdk] `printcompat.PrinterFromState` reads a printer's tools from the tool tree only. A printer that reports no tree has no tools.
- A chart of a printer's nozzle draws `/tools/0/nozzle_temp`, the first head. Readings stored under `/temperature/nozzle` before the upgrade are not drawn.
- The printers page, the controls and the readings beside a printer's name show the nozzle of the head printing now, and of head `0` when none is picked.
- The nozzle-watch example module reads the first head's nozzle at `/tools/0/nozzle_temp` and `/tools/0/nozzle_target_temp`.
- The setup wizard titles its MQTT section MQTT, and its example broker address is `tcp://broker.local:1883`.
- Every API credential now has a scope, `read`, `operate` or `administer`, and reaches only what that scope allows. A credential minted before this release can only read, so mint a new one with `operate` or `administer` where a script needs more. `POST /api/v1/auth/credentials` takes `scopes`, and a mint that names none gets `read`.
- A credential whose scope stops short of `administer` may revoke itself and no other credential.
- The database records the scopes of every credential, so a build older than this release refuses to open it. Roll back from a backup taken before the upgrade.
- A core update and the extensions it needs are one plan, as in Composer: preparing resolves the extensions again with the new core version. An extension the new core would leave behind is updated beside it, from the offline bundle or the module index.
- A bundle whose extensions do not fit together, or with what the server keeps, is refused at preparation. The extensions an update installs go in after the modules they require, and the modules lock is backed up and rolled back with the rest.
- Preparing an update that installs extensions is refused when the server cannot write `modules.lock.json`, as an install is.
- Installing or updating an extension asks with the primary button, and removing one keeps the red button. `ConfirmDialog` takes `variant: 'confirm'` for an act that loses nothing.
- In the update preflight, the flag that a module must serve for the server to start is now `needed_to_start`, and a module the plan updates reads `updated`.
- Settings, Server opens on an Overview tab with one card for each part of the server. The cards show its status, its printers, its version and any newer release, its last backup and its network announcement. Each card opens the tab that says more.
- The Network settings moved to a Network tab in Settings, Server, and the old address leads there. The tab now names what this server announces on the local network: its name, its `.local` address and port, its IP addresses and its mDNS services. It also warns when a container's bridge network keeps the announcement from every other machine.
- The Health tab in Settings, Server, names each printer that is away and links to its page, with the time it was last seen. It explains that an away printer is usually switched off and that nothing on the server needs a restart. It also lists the optional instances that are not serving. The connector verdicts read All connected, Some printers away and Most printers away.
- The printer Files tab and the Storage page list the newest files first, after the folders, instead of by name.
- The web app draws printers with a 3D printer icon instead of a paper printer.
- [sdk] A module depends on other modules by a range of their release versions. The manifest follows Composer, with `require:`, `suggest:` and `conflict:` maps. In `require:`, `core` is the server's version and `core-<method>` a capability. The `requires:` list and the top-level `core:` key are gone.
- [sdk] A constraint has Composer's meaning, so `~1.2` now means `>=1.2 <2.0`. A release has a stability, `core.modules_minimum_stability` and `core.modules_prefer_stable` choose among them, and `^1.2@beta` takes betas of one module.
- [sdk] `calls:` in a manifest names modules that the manifest also names under `require:` or `suggest:`, and `routes.call` refuses any other module.
- A file's page is now a header and tabs: Contents, Prints and Details. The size and the date sit under the name, and where the file is and its checksum moved to Details. The page no longer says a file needs slicing, and it has no Prints tab where this server lists no prints.
- On an update that finished or rolled back, Settings, Updates names the button that clears it from the page Dismiss instead of Cancel update, because it changes nothing installed. A scheduled update still offers Cancel update.

### Deprecated
- [sdk] `fractal.CodeNotFound` is deprecated in favour of `fractal.CodeNodeNotFound`, the same code `0x4020`, which `Code.String` now renders as `ERR_NODE_NOT_FOUND`.

### Added
- [sdk] A session tells the host which address its device connects from through `connectorapi.ReportsPeer`. The xBuddy connector implements it.
- `GET /api/v1/discovery/nearby` names the printer of this server that connects from an entry's address in `device_id`, for a caller who holds observe on that printer.
- [sdk] A Fractal grant carries the join data of the extensions the parent offers, in `fractal.JoinGrant.Extensions`, which `join.ParentConfig.Extensions` sets. A joining node keeps the entries of the extensions named in `join.JoinerConfig.Extensions` in `join.Membership.Extensions`, and `join.JoinerConfig.CheckExtensions` can refuse the grant before the node connects.
- [sdk] `fractal` has the messages AREP, ALOS and ARBR, and `shadow.Store` keeps the state of the nodes above a node with `Rebase` and `MarkStaleAbove`. A selection entry with no instance id now selects every instance of its interface, where `shadow.New` refused it before.
- The Fractal module carries the state a server shares with the servers below it, and the notice that a server lost the server above, on `fractal.down.arep.<id>` and `fractal.down.alos.<id>`. A server asks the servers above for that state on `fractal.up.arbr` each time its connection to the server above opens. An engine the binding drives needs the methods `ParentConnected`, `ParentLost` and `ForgetAncestors` of `natsbind.Engine`.
- [sdk] `fractaltest.Recorder` counts `ParentConnected` and `ParentLost` calls, and `ParentConnections` and `ParentLosses` return the counts. Its `ForgetAncestors` does nothing, because a recorder keeps no ancestor shadow.
- [sdk] A Fractal node sends an interface instance exposed downward to every node below it with AREP, also from the root. It keeps what the nodes above report in its ancestor shadow, `node.Config.Ancestors`, read with `Node.Ancestors`. It drops an RPC, an AREP or an ALOS from the node above whose sender is not a node above it.
- [sdk] A Fractal node asks the nodes above for their full state when its transport calls `Node.ParentConnected`, and on `Node.ParentLost` marks its ancestor shadow stale and tells the nodes below. `node.Config.ARBRWindow` and `node.Config.ARBRMinInterval` pace those requests, and `Node.ForgetAncestors` discards the ancestor shadow of a node whose admission was revoked.
- A build downloaded from the beta portal carries an identifier of that download. `--version` and the start-up log show it, and a build from anywhere else shows nothing.
- While `core.modules_enabled` is false, the Extensions page shows no catalogue, no marketplace and no install, and says why.
- `GET /api/v1/ui` answers `modules`, false while `core.modules_enabled` is false.
- While `core.modules_enabled` is false, the server starts and serves no module it installed, and lists each as `disabled` with the reason `ModulesDisabled`. Modules declared in `config.yaml` still run.
- `core.modules_enabled: false` in `config.yaml` refuses every module install with `MODULES_DISABLED`, from the web app, the API and `module add`, and the server reads no module index or marketplace. Removing an installed module still works.
- [sdk] A driver declares the device models it reaches in `connectorapi.Dialect` `Models`, and `GET /api/v1/vocabularies/device-models` lists them beside the Prusa models under the driver's own dialect. The server leaves out a model that two drivers declare, or that names a Prusa machine, logs an error at start and lists the model under `refused`.
- When the web app has no drawing of a printer's model, the printer icon shows the model's name from the server instead of "Unrecognised printer model". That is most often the name the model's driver declared.
- [sdk] A driver may declare a drawing of a model in `Artwork`, served at `GET /api/v1/vocabularies/device-models/{model}/artwork`. The web app shows it for a model it has no drawing of its own for.
- `uploadStorageObject` and `postStorageObject` in `@prusa/connect` take an `onProgress` callback. It reports the bytes sent as a file goes up, so a page can draw a progress bar for an upload instead of a bar stuck at zero.
- The Storage page shows how much of an upload has gone up, and lets one on its way be cancelled.
- Every page now lists the files dropped on a printer page until they reach the printer, with their progress and a link to the printer. One still going up can be cancelled, and closing the tab while one is going up asks first.
- The uploads list names each file's printer, and a printer's own page now shows a file going up to the server before it moves on to the printer.
- A file still on its way to a printer when the page that sent it is reloaded is listed again on every page until it lands.
- The demo takes `-transfer-rate`, the bytes a second each simulated printer fetches a file at. A file on its way to a printer then stays on screen long enough to watch.
- `GET /api/v1/jobs` lists the prints of every printer the caller may observe, running and ended, newest first, in pages by cursor. `phase` narrows it to running or ended prints and `device_id` to one printer.
- A finished or failed print in the bell opens that print's page.
- A printer's page shows the picture of the file it is printing beside the print's name, as its Files tab and last prints already do
- `POST /api/v1/devices/{device_id}/files` and `POST /api/v1/devices/{device_id}/print` refuse a file sent to a name the printer already holds with `core.printer/FILE_NAME_TAKEN`. The same request with `replace: true` deletes the printer's file and sends the new one
- The print dialog and a printer's page ask before they replace a file the printer holds under the same name
- `GET /api/v1/devices/{device_id}/storages` takes `refresh=true` to ask the printer what drives it has now, and answers `reported`, which tells a printer with no drive from one that has not answered yet. Every answer that changes the list is published as `com.prusa.connect.device.storages.changed`.
- [sdk] A module can declare `file_action` to add an item to the menu of each file on the Storage page and on a file's own page. The host runs it with the file's reference, `<storage>:<path>`, which `parseFileRef` from `@prusa/connect-ui/slots` splits.
- [sdk] A module can declare `print_section` to add a section to the print dialog of a file in storage, handed the file's reference. `printDialogContext()` gives the section the printers ticked in the dialog and a `close()`, and a manifest may name the icon `calendar-clock`.
- [sdk] A module can declare `list_people` to list the people the server holds with `access.people`, each by subject, name and kind, once the operator approves it at install. A server without local accounts answers an empty list.
- [sdk] A print carries the objects a printer can skip one by one, as `object_count` and `objects`, on `GET /api/v1/devices/{device_id}/jobs`, the job history, the device listing, `device.jobs` and `device.job_history`. A finished print keeps the set it ended with, so a module can read which objects it skipped.
- An operator hides a module's item in the file menus and its section in the print dialog from the extension's Where it shows tab. In `hidden_contributions` the entries are `file_action/<id>` and `print_section/<id>`, and a dashboard card cannot be hidden.
- [sdk] `DialogContent` takes a `size` of `md`, `lg` or `xl`, and the panel grows to 32, 42 or 56 rem on a screen that has the room. A module's own width classes do not reach a dialog, which is drawn outside the module's slot.
- [sdk] A print file's details carry the slicer profiles it was sliced with, as `print_profile`, `printer_profile` and `filament_profile`, and `high_flow` for a high flow nozzle. They are on the storage file routes and on the module storage wire.
- A module may ask for a database of its own with `needs.database: true`, and the operator chooses where it goes. It is a SQLite file in the module's data directory, a Postgres database the operator names, or one this server makes on its own Postgres server with `persistence.postgres.module_admin_dsn`.
- [sdk] `wirerpc.Grants` carries `Database`, the engine and the connection string of the database the operator granted the module, and `manifest.Needs` reads `needs.database`.
- [sdk] A module that asks for a database of its own opens and migrates it with the new `sdk/moduledb` package. A new SQL file or Go step is named by the UTC date and time it was written, such as `20261027T093015_note_tags.sql`. One upgrade applies every migration the database lacks in one transaction.
- [sdk] `wirerpc.ConfigureReporting` lets a module answer `plugin.configure` with the schema version of its own database.
- An update installed over HTTP is kept only when the new release starts. One that does not start, such as one whose database migration failed, is taken back to the release it replaced and answered with `MODULE_UPDATE_ROLLED_BACK`.
- The resources listing shows the schema version of a module's own database under `module.database.schema`, and an install answers `schema_version` and the `migrations` its start applied.
- A removed module's own database stays with its data until the operator purges it, and `GET /api/v1/modules/databases` and `module list` show it. `DELETE /api/v1/modules/databases/{kind}/{name}` and `module purge` drop a database this server made, delete a SQLite file and forget one the operator named.
- An update that moves a module's database to another place keeps the old one with its data and lists it beside the removed ones.
- Installing a module again with the database mode `provision` gives its role a new password and keeps its data.
- [sdk] A module's `core` range that names a prerelease of the core's own release, such as `>=0.0.1-alpha.7`, now tells that release's prereleases apart.
- `persistence.postgres.module_admin_dsn` is refused at start unless it is a `postgres://` URL with a host and a database, and on a server whose `persistence.type` is `sqlite`.
- The extension install dialog lists a database of its own among a module's requests and lets the operator choose where it goes.
- The Extensions page lists the databases extensions left behind and purges one after the person types its instance name. An extension's page shows the schema version of its database.
- [sdk] A running module can hide its own pages and other entries with `ui.hide` from `uiapi`, and an open web app takes them out of the sidebar at once. `GET /api/v1/resources` carries the list as `ui.hidden`, and each change is published as `com.prusa.connect.resource.ui.changed`.
- The demo takes `-print-gap`, the wait between a printer's prints, and every print then takes seconds, a print a module starts on an `-idle` fleet too
- An administrator can give or block one person's access to one printer from the web app, on the printer's Printer tab or on the person's own page in Settings, People
- `GET /api/v1/grants/subject/{subject}/direct` lists the grants written for one person on every printer you administer, a block included
- Settings, People has a search box, and a person's username opens their page
- A printer's state now carries its tools as a tree of heads and the feeder slots on them, under `/tools/<id>`. An id such as `0` is the first head and `0.2` is the third slot of the MMU in front of it, and there is no fixed number of tools. Each tool carries its nozzle features, its material, its temperatures and fans, and whether it is enabled. The names are those of the public Prusa architecture. `/tool_count` is how many heads there are, and `/active_tool` is the tool that prints now.
- [sdk] `connectorapi` names the tool tree paths, the `ToolID` spelling, and `ReadTools`, which reads the tree back and maps a print file's `T<n>` onto a tool.
- The Prusa driver maps today's firmware onto the tool tree. An XL is five heads, an INDX eight heads, and an MK4S with an MMU3 one head with five slots. The driver also reads firmware that sends the tree itself.
- The Moonraker driver reports each Klipper extruder as a head counted from 0, with the nozzle diameter its configuration names and its temperatures.
- A printer's state carries its build volume at `/build_volume/x`, `/y` and `/z`, in millimetres. The Prusa driver reports it for each model it knows, and the Moonraker driver reads it from the Klipper configuration.
- [sdk] `connectorapi` names the build volume paths a driver reports.
- [sdk] `storageapi.PrintInfo` carries what each filament of a print file needs, under `Filaments` from `T0`, with the bed, the object area and the G-code checks.
- [sdk] The new `printcompat` package says whether a print file fits a printer, the way the printer's firmware judges it before a print, and whether it fits the build volume.
- A printer record from `GET /api/v1/devices` carries `build_volume` and `tools`, the printer's tool tree keyed by ids such as `0` and `0.2`. A printer that is away shows what it reported last.
- Each tool in `tools` keeps the names and nesting of the public Prusa architecture, such as `material_package_instance.package.material.abbreviation`. A disabled tool, no feeder and nothing loaded each read `null`, and a member the printer did not report is left out.
- The `print` member of a stored file's details carries what each filament needs under `filaments`, from `T0`. It also carries the bed, the area the objects cover, the top layer height and the G-code checks.
- The Info tab of a printer shows a Tools card with each head, its nozzle, the filament loaded or nothing loaded, the slots of its feeder and the build volume. A print file's page lists what each filament needs and what the printer checks before it prints.
- [sdk] The TypeScript client takes a `shareEvents` option. With it, all the tabs of a browser that talk to one server as one person read a single `GET /api/v1/events` stream, which one tab reads and passes to the others
- The demo offers the first-party extensions Parts, Fleet stats and Prusa printer logs on its extensions page, and skips one that does not build with a warning
- The History tab of a printer's page lists the files this server sent to the printer. Each row shows the state the send ended in, the bytes that reached the printer, and why a send was cancelled or failed
- [sdk] `connectorapi.NozzleTempPath`, `connectorapi.NozzleTargetTempPath` and `connectorapi.MaterialPath` name `/temperature/nozzle`, `/temperature/nozzle_target` and `/material`, the values of the tool printing now. A reader that needs the value of one tool reads `/tools/<id>`.
- On a Klipper machine, `core.command.set_nozzle_temperature` takes the `tool` argument, the head counted from 0: 0 heats `extruder`, 1 heats `extruder1`. Without it, the command heats the extruder printing now, where it always heated `extruder`.
- [sdk] `oidctoken.Verify` checks a token against a `Policy` of accepted algorithms, a key set and a client secret, and `oidctoken.Accepted` builds the list from a discovery document. A `KeySet` reads EC and Ed25519 keys as well as RSA, and `KeySet.Key` returns the key for an algorithm.
- `core.auth.provider_name` names the OpenID Connect provider on the sign-in button. Without it the button names the issuer's host, as in Sign in with sso.example.com, where it said Sign in with oidc.
- The printer page has a Save to storage switch under the drop zone. On, the file is also kept in the storage and folder picked there.
- `PUT` and `POST /api/v1/storages/{storage}/content` take `temporary=true`, which writes the file as a temporary copy to send to a printer. The server deletes the copy once the file lands or its transfer is stopped, one hour after a failed send, or at the next start.
- Each tool in a printer's state carries `active`, as the public Prusa architecture marks it. A head is active while it is picked, and a slot while it is loaded into its head, even a head that is not picked. The Prusa driver reads it from firmware 8.0 and from the slot block of earlier firmware. The Klipper driver reads it from the extruder printing now. `/active_tool` is derived from these members, and each tool in `tools` of `GET /api/v1/devices` reports its own `active`.
- [sdk] `connectorapi.ToolActive` names the member, `connectorapi.ActiveToolOf` derives the active tool from the members, and `Tool.HeaterOff` reads a target of 0.
- Each feeder in a printer's state names its family and its kind at `feeder/base_model` and `feeder/type`, beside its model. It reports what its multi-material unit is doing at `feeder/mmu_progress_code` and `feeder/mmu_command_in_progress`. The Prusa driver reads them from firmware 8.0, and the unit's progress and command from the slot block of earlier firmware.
- Each tool reports its odometers at `odometer_id`, `odometer_e` and `odometer_weight`, as firmware 8.0 sends them: the filament extruded since the last reset, and a number that changes at each reset.
- `tools` in `GET /api/v1/devices` carries these in each tool and its `feeder` object, under the public Prusa architecture's names.
- The Tools card of a printer names the model of its feeder and says what the multi-material unit is doing. It shows idle, or the command the unit runs and the step it is at.
- [sdk] `connectorapi` names the members `FeederBaseModel`, `FeederType`, `FeederMMUProgressCode`, `FeederMMUCommandInProgress`, `OdometerID`, `OdometerE` and `OdometerWeight`, and `ToolTree` reads them.
- `GET /api/v1/transfers` lists every file on its way to a printer the caller may observe, whoever started it. Each row names the printer and says whether the caller started it and may stop it. A caller who holds `view_audit` on that printer is also told who started it.
- [sdk] An event sink that implements `eventsapi.SampleObserver` is handed the live state of the devices it may read. It gets one `Samples` per device per delivery window, and the whole state tree once when it can first read a device.
- The MQTT sink publishes each head and each feeder slot of a printer under `v1/devices/printers/{printerId}/data/tools/{toolId}/`, with the tool tree's ids such as `0` and `0.2`. A head has `active`, `material`, `temp/nozzle/current` and `temp/nozzle/target`, and a slot has `active` and `material`; the retained topics are filled once when the sink is granted the printer.
- An MQTT tool topic carries `0` for a heater that is off, and an empty payload for a value that is not known. A disabled tool, or one that left the tree like the slots of an MMU switched off, gets an empty payload on every topic, which deletes the retained value.
- `GET /api/v1/health` returns `instance_id`, a random id the server writes into its database on the first start and keeps across restarts and address changes. The mDNS record of every service the server announces carries the same id as the `id` key, so a client knows the server again at a new address.
- [sdk] The event stream publishes `com.prusa.connect.device.cameras.changed` on a device when the cameras `GET /api/v1/cameras?device=` lists for it change, so a client reads the cameras again then instead of on a timer. The frame names the device only, reaches whoever may see that device, and goes out at most once every 5 seconds per device. Images, live views and uploads never travel on the event stream.
- [sdk] A driver declares the states its devices report, with the verbs each state accepts, in `Dialect.States`, and a numeric argument declares what it counts in, in `Value.Unit`. `connectorapi.Register` refuses a state set that lists the verbs of some states and not others.
- `GET /api/v1/vocabularies/device-states` lists the states of every connector type the server runs, with the verbs a device accepts in each state, where it answered an empty list before. Each entry names the connector instances it covers, so a client finds a device's states by the `driver` the device record names.
- `GET /api/v1/devices/{device_id}/capabilities` serves every verb a device resolved to with the name, type, range, default, allowed values and unit of each argument. A client builds a control from it, and a value inside the range passes the check the server makes before the printer is reached.
- An app can ask for a credential with a short code that a person approves on the page `/pair`, as RFC 8628 describes. The app calls `POST /api/v1/auth/app-pairings`, the person approves its scope or a narrower one, and the app collects its token from `POST /api/v1/auth/app-pairings/token`. A pairing waits ten minutes, a code works once, and each step writes an audit line to the log.
- `GET /api/v1/auth/credentials` shows the `scopes` and the `origin` of each credential, and `?origin=app_pairing` lists the apps a person paired.
- The API credentials page of the web app lets a person choose the scope of a new credential and marks the apps a person paired. The page `/pair` shows what an app asks for, and approves or denies it.
- The setup wizard opens any page you have already reached in one click, from the steps on the side or with a new Back to summary button. A jump forward stops at the first page whose answer is no longer valid.
- add the foundations for signed transactional automatic upgrades.
- add backup create, list, and restore commands for upgrade recovery.
- add signed upgrade preparation with trial startup and automatic rollback.
- automatically roll back an upgrade interrupted during binary swap or trial startup.
- publish signed upgrade metadata and enable managed systemd binary replacement.
- add authenticated update and backup orchestration with safe offline apply, restore, scheduling, history, and progress events.
- keep the durable upgrade journal outside restored payloads so interrupted rollback can resume safely.
- accept the intentional forced stop of a healthy Windows trial process.
- block an upgrade before shutdown when its candidate drops a capability required by an installed extension.
- Settings can verify, rehearse, schedule and safely roll back signed core updates, and can create, inspect, restore and retain consistent backups.
- Settings separates online portal releases from offline signed bundles, shows extension compatibility before an update, and exposes prepare, apply, progress, failure and rollback states. A tester pairs the server to the Beta Portal with a short-lived code; access tokens and proof keys never reach the browser.
- Docker, Kubernetes and printer-managed installations can prepare and validate updates while leaving executable replacement to their platform.
- Organization administrators can configure one private Beta Portal entitlement credential for online core updates and same-origin marketplace requests, while API reads expose only its status and suffix. This provisional transport is superseded by device pairing below.
- Organization administrators pair the server to the Beta Portal with a short-lived code, and every online update request is bound to the server private key. They can disconnect it locally even while the portal is unavailable; the UI then tells them when remote revocation still needs attention.
- The Beta Portal pairing status says what a pending pairing waits for, the tester's confirmation or the Prusa team's approval, and gives the portal address with the code filled in.
- Settings, Updates walks you through Beta Portal pairing step by step. It opens the portal with the code already filled in and says when it waits for you or for the Prusa team. When paired, it names the account. It also explains an expired or declined code, a portal it cannot reach and a wrong server clock, each with the next thing to do.
- Starting a Beta Portal pairing on a paired server is refused with 409. A damaged pairing file is set aside so the server still starts, and a wrong server clock is reported as `core.resource/BETA_PORTAL_CLOCK_SKEW`.
- Signed offline update bundles can carry one core executable and verified extension release archives with strict platform, compatibility, path, size and digest checks.
- Signed offline update bundles atomically apply one core executable and verified extension release archives under the same backup, candidate trial and rollback. A failed extension install, migration or start restores the previous core, module files, declarations, grants and protected module databases together.
- Settings, Updates lists what each extension in an offline bundle asks for, and marks new requests and older versions. Apply then waits until the operator approves them, and only then sends `approve_extensions`.
- When a rolled-back update leaves PostgreSQL databases for the operator, Settings, Updates names each one in a warning on the update and in its history entry. The warning asks the operator to restore them from their own backup.
- When the update server refuses the release list because this server asked too often, Settings, Updates says how long to wait. It sends no new release request until that time has passed.
- A paired server tells a revoked pairing from a suspended access by the reason the Beta Portal names, and keeps the pairing while the access is suspended.
- Settings, Updates checks a paired server's pairing with the Beta Portal when the page opens and every minute after, and says when the tester's access is suspended.
- The add-printer page offers connect.der for download when printers trust this server's own authority.
- [sdk] A module can write a file into a storage with `storage.write`, sending the bytes on the call's own stream. The call takes `write_storage`, and the storage publishes the file only when every byte has arrived.
- The server image carries the slicer extension under `/opt/modules/slicer`, which slices 3MF projects into G-code with a PrusaSlicer you install or name.
- The API reference page shows a module's routes at `/docs?module=<namespace>`, in the same renderer as the core's own document. A page of this server may frame it, so a module's page can show its routes in place. At the top of `/docs` a row of links moves between the core's document and each module document you may read.
- [sdk] A module may call the routes of another module with `routes.call`, after it names that module under `calls:` in its manifest and the operator approves the install. A call runs as the person the module is serving, held to their permissions, or as the module itself.
- An approved install grants a module `write_storage` on the storage kind, because no role on the organization that a module may hold reaches it. `read_storage` is still granted on the organization. A module that writes files then needs no grant that the operator writes through the API.
- Installing a module adds the modules it requires from the index and the marketplace, the newest release that fits every installed module. `POST /api/v1/modules/plan` lists them first, the install takes the approved list as `dependencies`, and `module add` prints what it added. A conflict is refused with `MODULE_DEPENDENCY_CONFLICT`, which names who needs what.
- A module another installed module requires cannot be removed or switched off, and the refusal `MODULE_REQUIRED_BY` names the modules that require it. When a removal leaves modules an install added and nothing needs any more, `module remove -with-unused` and `DELETE /api/v1/modules/{namespace}?with_unused=true` remove them too, and `GET /api/v1/modules/removals/{namespace}` lists them first.
- `module install` installs exactly what `modules.lock.json` names, `module update` takes the newest releases every constraint allows, and `module why`, `module why-not` and `module outdated` answer what Composer's commands of the same names answer. The API serves the last three under `/api/v1/modules/why/`, `/api/v1/modules/why-not/` and `/api/v1/modules/outdated`.
- The server starts a module after the modules it requires. It stops a dependant while a module it requires is down, and starts it again once that module serves. Every change of the installed modules writes the resolved releases and where they came from to `modules.lock.json` beside the overlay, and a later install prefers those releases.
- The Extensions page lists the extensions an install also adds, with the release and the requests of each, before the person approves it. An extension shows what it depends on in a Dependencies tab. An extension others require cannot be removed or switched off from the page, and a removal offers to remove the extensions it leaves unused.
- The Dependencies tab of an extension shows why it is installed and how its release stands against the index. It also lists each suggestion with its reason, each conflict with the release installed, and the core version it needs.
- [sdk] A module can fill a new `file-view` slot on a file's page. An entry of `file-view`, `file-action` or `print-section` can name `file_types` to show only for files of those types. The view is drawn in place of the part of the page that says what the file holds, and the page keeps its header, its actions and its print button.
- [sdk] A module's bundle can hand a component to other modules' bundles under `ui.exports`, and draw a component of another module under `ui.imports`, which names the module and the component.
- [sdk] A module can read files of the types it names under `file_analysis` for the core. The core asks it for a file's picture, details and what a printer needs, with a read handle that reads that one file only. The core reads the file itself when the module fails or does not answer in time.
- [sdk] A module reads a large file through `storage.fetch` and `storageapi.Client.Open`, which send the bytes as a stream. A 100 MiB file reads about 50 times faster than through `storage.read`.
- Two enabled modules can no longer read files of one type, or draw a file view for one type. An install, an update or switching a module on that would allow it is refused with `MODULE_FILE_TYPE_CONFLICT`, which names both modules and the types. The install plan lists the conflict under `conflicts` before the install.
- `GET` of one file in a storage carries `analysis`: the module that described the file, the title, the facts and the objects it named.
- The server image carries the 3MF projects extension at /opt/modules/threemf. Name it in core.modules_local to offer it, and the page of a .3mf file then shows the project with its beds, objects and settings.
- A module reads what is wrong with a printer, and the dialog it waits on, from the printer record, and the event com.prusa.connect.device.issues.changed says when either changes. See [ADR-20261029-sax](decisions/20261029-sax-a-module-reads-what-is-wrong-with-a-device-and-the-dialog.md).
- `POST /api/v1/storages/{storage}/deletions` removes several files and folders from a storage in one request. A list with a path that leaves the storage or names its root removes nothing, and a batch where some entries stayed answers `core.storage/DELETE_INCOMPLETE`.
- A download from a storage whose `download_url` is true is answered with a 307 redirect to the URL that storage hands out. The file then no longer passes through this server. The disk storage hands out no URL and is served as before.
- A print file's answer and a module's stat of it carry the objects the slicer labelled. Each has the id a printer cancels it by, its name and its outline on the bed.
- [sdk] A module can add a tab to a file's page in the new `file-tab` slot. An entry of `file-action` with `primary: true` is a button in the header of the file's page, and stays a menu item in the listing.
- A file's page has a Print button for a file a printer takes as it is. It opens the same print dialog as a row of the storage listing.
- The print dialog reads in Czech.

### Fixed
- A Prusa Connect server on several networks was listed twice among the printers nearby, and one of the two rows looked like a printer to add. It is one row now.
- The server announced itself over mDNS on VPN tunnels and with network addresses some virtual bridges carry, so a client could list it at an address nothing opens. It now announces only addresses on a local link that another machine can open.
- A server on several networks moved between its addresses on the list of printers nearby from one read to the next. Its row now keeps one address, and a printer connected to this server is recognised on any address it announces.
- The Fractal module refuses to start when `accept.advertise` has no scheme or no host, such as `hq:7422`. With such a value, the servers below redialed the listen address after a drop.
- A Fractal server below another reconnects only to the leafnode address `accept.advertise` names on the server above. Behind NAT or a port forward, it no longer redials the unreachable listen address after a drop.
- A full state report the Fractal module sent for a printer carried the time of the printer's newest reading, so `updated_at` on the server above could stay behind the report. The report now carries the time the module read the state, and `updated_at` moves forward with each full report.
- The Printers page hid the Retired printers button once every printer was removed, so the removed printers could not be seen. The button now stays in the page header while the list is empty.
- A dialog naming a long file, such as Rename, ran its title and text field past the dialog's edge. The name now wraps and the field stays inside the dialog.
- A `core.principal` changed after the first start, as the setup wizard does when you sign in at your own OpenID Connect provider, got no administrator rights. The next start now makes that principal the administrator, once.
- Signing out on a server that signs you in at an identity provider showed the username and password form until the page was reloaded. It now shows the button for the provider.
- `module remove` refused the `kind:name` spelling that its own error for an ambiguous name lists, and after a removal it pointed at the install call. It now takes `kind:name` and names `DELETE /api/v1/modules/<namespace>`.
- The Extensions page offered Remove for a module that `config.yaml` declares, and the server refused it every time. Remove is now disabled there, in the row's menu and on the module's page.
- An instance switched off in `config.yaml` or `config.d/resources.json` was started after its `type` was edited there and the next write through the API made the server read the files again. It now stays off, and one that ran is stopped.
- A Prusa printer refused a command sent while it was still running an earlier G-code command, such as Beep pressed after Home. The server now holds the next command until the printer reports the G-code finished, for at most a minute.
- The Add a printer guide drew a Prusa printer as an unrecognised one, because it looked the drawing up by the model's family. It now draws the printer's own model, also when the printer reports a code in place of a model name.
- Mesh bed leveling on a Prusa printer with several tools, such as the CORE One INDX, homed with no tool picked up. The server now homes the printer fully first, as Home does, which picks up a tool, and then levels.
- A file sent to a printer through `POST /api/v1/devices/{device_id}/files` or
  `POST /api/v1/devices/{device_id}/print` keeps going when the page that sent
  it is reloaded or closed. It runs until the file arrives, the stop route ends
  it, or `core.api.stream_timeout` passes, and the printer no longer throws away
  a file a page left part way.
- `POST /api/v1/devices/{device_id}/print` now starts the print even when the
  page that asked for it closes while the file moves to the printer.
- An upload through `PUT` or `POST /api/v1/storages/{storage}/content` that
  takes longer than ten seconds no longer fails with 504. The upload is bounded
  by `core.api.stream_timeout` and ends when the client goes away.
- A finished print read 99 per cent on the printer page, the Jobs page and the job page. It now reads 100, and a stopped or failed print keeps the figure it stopped at.
- The Jobs page listed only finished prints, and only of the first 25 printers, so a farm that was printing read "No jobs yet". It now lists running prints beside finished ones across the whole fleet, and follows prints as they start and end.
- A printer's Now tab listed at most one finished print, and said the printer had printed nothing while its first print was running. It now lists the last three, each print on the printer page opens its job page, and the link under them opens the printer's prints on the Jobs page.
- Printing a file from a storage a second time on the printer it was sent to failed, because the printer refuses a file under a name it holds. The print now starts the copy already there when this server sent that same version of the file and the copy has not changed since
- A folder with a long name on a Prusa printer's USB drive listed its files under the folder's short 8.3 name, such as `SUMMER~1`. They are now listed under the long name, and a file sent into such a folder is found there when its transfer ends
- The setup wizard refused "Sign in with your own provider" at Confirm and asked for a username and a password. `POST /api/v1/setup` now takes a required mode with no account when the configuration names `core.auth.issuer`, and the administrator is the subject given in `core.principal`.
- A USB stick put in or pulled out of a printer after it connected went unnoticed until the printer reconnected. Listing the printer's files from the root now asks the printer again, unless the server holds a listing of the root and `refresh=true` is not set.
- The printer page dropped its upload area without a word when the printer had no USB drive. It now says to put one in, with a button to ask the printer again, and the print dialog marks such a printer as unable to take the file.
- A print file sliced for several filaments now reports the filament the whole print uses, where it reported the share of the first filament.
- An extension that requires the record store is no longer shown as ready on a server that declares no record backend, such as one set up by the first-run wizard. It waits with `RESOURCE_RECORDS_UNSERVED`, starts by itself once a backend under `resources.db` serves, and the Extensions page says why.
- On a server with no record backend, an extension that calls the record store now reads that the server serves no such method. It used to read a failure it could not tell from a broken backend.
- A server on a prerelease, such as `0.0.1-alpha.6`, no longer lists a marketplace release whose core range it is outside, such as `>=0.0.1-alpha.7`, which the install then refused. The server sends its whole version as `core` to the marketplace and holds each listed release to the rule the install uses.
- The web app no longer stops loading when six or more tabs of one server are open over plain HTTP. Each tab used to hold an event stream of its own, and those streams took all six connections a browser opens to one host. Now all the tabs of a browser share one stream, and module pages read it too
- A resource instance whose type this build does not have now fails at once with `core.internal/RESOURCE_UNKNOWN_TYPE`, and its message lists the types this build has. The resources API refuses to create such an instance with status 422.
- A module no longer logs the warning `a declared sink is receiving no events` at every start. A sink whose type cannot receive events still logs it.
- Setting the nozzle diameter, the hardened nozzle or the high-flow nozzle of a tool on a Prusa printer now reaches the printer. It is sent under the key the firmware reads, on firmware 8 and on the firmware before it. A setter sent before the printer's first INFO after connecting is refused with `core.printer/VERB_NOT_ALLOWED_IN_STATE` and the state `UNKNOWN`, and may be sent again a few seconds later.
- A Prusa printer's tool tree no longer keeps what the printer stopped reporting. The slots of an MMU that is switched off leave `/tools`. So does a nozzle feature, a feeder model or a material that the printer's next description of its tools leaves out.
- A head that a Prusa printer on firmware 8 reports as switched off in its telemetry no longer keeps its last temperatures and fan speeds in `/tools`.
- [sdk] A module answer over the 1 MiB frame limit was never sent, so the caller waited for its deadline and an HTTP request ended in a 504. The answering side now logs it to `wirerpc.Options.Logger` and sends `wirerpc.CodeResultTooLarge` (-32001), so the HTTP request ends in a 500 at once.
- A module installed with `module add -approve` against the file the demo starts on had no grant in the demo, so it saw no printer. At each start the demo now makes the module grants in its own database match the installed modules, which also withdraws the grant of a removed module.
- On a Klipper machine with more than one extruder, `/temperature/nozzle` and `/temperature/nozzle_target` are the readings of the extruder printing now, where they were always those of `extruder`. A machine with one extruder no longer repeats them under `/tools/0`, and `/tools/<id>/nozzle_target_temp` is removed while that heater is off.
- The printer page shows a row per head for a printer whose tool tree holds more than one head, whatever its model. It decided by the model before.
- Each head on the printer page shows the target it reports at `/tools/<id>/nozzle_target_temp`. A head that was not printing showed a target of 0.
- The nozzle controls on the printer page start from the diameter, high-flow and hardened nozzle each head reports under `/tools/<id>/features`.
- The printer page and the printer list show the material of the tool printing now, from the tool tree. A printer with a feeder shows the slot it prints from.
- The nozzle-watch example module reads the nozzle diameter and the loaded material of the first head from the tool tree. It read paths no driver writes.
- After the setup wizard restarted the server on your own OpenID Connect provider, Go to the fleet asked for a username and password until you reloaded. It now shows the button that signs you in at the provider.
- A person whose OpenID Connect subject is not a short lowercase id signed in at the provider and was then refused here. Such a subject, like the default one of authentik, Auth0 or Dex, now gets a stable user id made from a hash of the issuer and the subject. The setup wizard takes the administrator's subject as the provider shows it.
- A sign-in at an OpenID Connect provider failed after the person signed in there whenever the provider signed its ID token with anything but RS256. The server now takes every algorithm the provider lists and can check, including ES256 and HS256 with the client secret, which an authentik provider with no signing key uses.
- A sign-in at an OpenID Connect provider that failed after the person signed in there ended on an error document and logged nothing. It now returns to the sign-in page with a sentence about what went wrong, and the server log has the cause.
- A person who signed in at an OpenID Connect provider was shown by a user id instead of a name. The page now shows the name and the address the provider sends, read from the ID token or, as Authelia needs, from the userinfo endpoint.
- Every request that carried no credential, such as a page asking who is signed in before anybody has, was logged as a warning about a malformed token. It is now logged at debug, and a credential that was really malformed is still a warning.
- The beta portal's downloads page offered no `prusa-connect.service` and no `config.yaml`, which the page "Install from a release" tells you to install. Both are now listed there under "Files for every platform", and the install page checks a download against the checksum the downloads page shows in place of `SHA256SUMS`.
- The non-Prusa machine in `make demo` reports its one head like every other printer: `tools.tool_count` is 1 and head `0` is a print head, enabled and active. Before, its record had a nozzle reading but no tool count and no tool type.
- On a Prusa XL that reports the numbered slot block, a head that was put down kept the nozzle target it had while picked. That firmware sends a target for the picked head only, so `nozzle_target_temp` is now absent for every other head, which means the printer did not say.
- The setup wizard's address printers use now takes an optional port, such as `192.168.1.24:8481`, and writes it into `core.frontend.printer_address`. A server in a container published under another port no longer gives printers the port it listens on inside the container.
- A failed automatic upgrade now restores protected directories such as `config.d/modules` exactly, removing files and directories created only by the failed version while leaving data outside those protected paths untouched.
- Failed upgrades now restore PostgreSQL module databases through a verified staging database while preserving ownership, access, comments and settings.
- Automatic upgrades now accept a version-stamped candidate at apply time after its signed normalized fingerprint passed staging.
- Data created by published alpha releases can migrate to a current release without an already-applied migration being rejected as changed
- An update now checks a release binary against the plain SHA-256 digest the release publishes, so a signed release passes its check. The binary no longer carries a download identifier, and `--version --json` no longer reports `download_fingerprint`.
- A failed upgrade now finishes its rollback when a PostgreSQL database is backed up outside the server. It restores the files and the previous binary, names the database to restore in `external_restore` and in the log, and the server starts again.
- An update no longer fails when the server keeps its upgrade files on a different filesystem than its binary. The verified binary is copied beside the old one before the swap, and a rollback replaces it in one step there too.
- On Windows an update keeps a complete copy of the old binary at `.previous` before it replaces the binary. When an interrupted update left no binary at the service path, the next start restores that copy.
- A PostgreSQL backup or restore during an update no longer puts the database password on the command line, where every local user could read it. `pg_dump`, `pg_restore` and `psql` receive it in `PGPASSWORD`.
- An online update now streams the release binary to a private file instead of holding it in memory. It refuses a file whose size differs from the size in the release catalogue.
- `core.upgrade.allow_unsigned` now applies only to a release an operator uploads. A release the server downloads from its update server always needs a trusted signature.
- A 32-bit ARM server now names its platform `linux_armv7`, or `linux_armhf` for an ARMv6 build, so the Beta Portal finds its builds. A Beta Portal request now gives up after 30 seconds.
- The server keeps the release list from the update server for five minutes per pairing. Opening the update page and staging a release no longer use up the update server's per-hour limit. When the update server still refuses for asking too often, the API answers `UPGRADE_SERVER_LIMITED` with status 429 and the wait in `Retry-After`.
- A crash while the server recorded a finished upgrade no longer breaks the upgrade history for good. The history file is replaced in one step, and a last line cut short by an older crash is ignored.
- An offline bundle that fails verification no longer deletes the files of the last finished update.
- A backup manifest names the migration a server must know to open each SQLite database in `floor_migration`, and reads migration numbers from the migration names.
- A failed upgrade now also restores the files modules keep in `config.d/module-data`, so a trial that migrated a module's own files is undone with its database.
- Preparing an offline bundle now lists what each bundled extension asks for, what is new against the installed release, and whether it is older, in `extension_reviews`. Applying refuses a new request or an older extension until the operator sends `approve_extensions`, and grants no more than the review shows.
- When the Beta Portal refuses the server's access token, the pairing shows as revoked in Updates and asks for a new one. A refusal caused by a wrong server clock is corrected and the request is sent once more.
- A marketplace other than the Beta Portal can no longer make the server send its Beta Portal pairing, by redirecting to the portal or by listing a download there.
- A Beta Portal answer can no longer set a clock correction over one day or a `Retry-After` wait over one hour. Only a refusal of the token in use corrects the clock.
- Settings, Updates reads the Beta Portal pairing again when the release list fails. A pairing the portal refused during that read now shows as revoked without a page reload.
- Settings, Updates no longer offers Apply on an update that already finished or rolled back, and offers Prepare only before the rehearsal.
- When the update server refuses this server's pairing, the release list answers `core.resource/BETA_PORTAL_PAIRING_NEEDED` with status 409 instead of saying that the Beta Portal did not answer.
- Online and offline updates offer alpha and beta releases, and the Updates page has no channel to choose. `core.upgrade.channel` and the `channel` query of the release list are gone, so a server on an alpha installs the next alpha.
- When a server cannot replace its own executable, as in a container, the Updates page says why next to Apply. It also says how to install the new version instead.
- A failed update that rolled back starts the previous version again instead of leaving the server stopped. The trial start of a new version gets the address flags the server was started with.
- Deleting a backup or cancelling a prepared update no longer reports a JSON error in the browser, and the deleted backup leaves the list at once.
- When printers share the web app's listener, the setup wizard now gives them the port through which the browser opened it. A person no longer has to enter a container's published web port.
- The setup wizard suggested a storage folder beside the configuration folder, such as /etc/storage under the systemd service, which the server cannot write. It now suggests storage in the folder the server starts in, /var/lib/prusa-connect/storage for the service.
- The systemd unit lets the server write its setup files under /etc/prusa-connect/config.d, so the setup wizard can finish under the service.
- When the server cannot reach the extension marketplace or a sign-in provider, the message names the cause, such as a DNS failure on this host.
- The Docker compose file gives the server's volumes fixed names, so a compose manager that renames the project still finds the existing data.
- The temperature chart on a printer's History tab labels its axis in whole steps of 5 °C, and small sensor noise no longer stretches the line across the whole chart.
- A print cancelled at the printer before it started printing, such as at the filament check, stayed in the print history as printing. It is now recorded as stopped, with the outcome cancelled.
- A long dashboard name made the page wider than a phone or tablet screen, so the whole page scrolled sideways. The name now ends in an ellipsis, and resting the pointer on it shows the whole name.
- On a printer's page at phone width, Pause and Stop drew over the name of the file being printed. A long name now gives up its middle, and the buttons move to a line of their own below it.
- Loading or unloading filament from the printer page heats the nozzle first on Buddy printers. Before, the printer showed the loading screen and waited with a cold nozzle.
- A Buddy printer that was sending telemetry went offline every minute or two when it answered the server's ping late. Any message from the printer now counts as proof that it is there, so only a printer that sends nothing for the whole read deadline is dropped.
- Two servers starting at the same time against one fresh history or module records database no longer fail to start. The schema setup now waits for the other server to finish, on SQLite and on PostgreSQL, and a failed setup leaves the database as it was.
- A fractal node asks for a full state report again after its link reconnects, and the request is no longer lost. Before, a node could keep stale state of the node below until the next change.
- A new API credential's token is no longer shown again after you leave Settings → API credentials and come back, as the warning beside it says.
- The History tab chart now lists every reading the printer reports, drawn or not, and turns readings on and off without a page reload. Readings in one unit draw together, and choosing one in another unit draws it in their place.
- The copy buttons now work when Prusa Connect is opened over plain `http://` on its network address.
- Sending a file to a printer no longer answers `sending` for a transfer that has finished. A progress report that arrived just after the end turned the finished transfer back into a running one.
- The webhook sink no longer counts a delivery the endpoint received as failed. Another part of the server that closed its idle connections at the same moment could break the request after the endpoint had answered.
- A `.bff` printer firmware file can be uploaded to a storage and sent to a printer. The built-in upload allowlist refused it with 415, and a storage that sets its own `allowed_extensions` must add `.bff` itself. The file is never offered as a print.
- A Buddy printer that took longer than a minute to list a folder lost its connection every minute and the folder never loaded. A printer that is still sending a listing now keeps its connection however long the listing takes.
- Slow uploads to a storage no longer block file lists, downloads, deletes and sends to a printer from that storage. Uploads now take at most 6 of the 8 slots a storage has for work running at once.
- [sdk] A read on a `wirerpc.Stream` that the reader or the peer ended could miss the last bytes the peer sent before the end. The read now hands out every buffered byte first and reports the end after them.
- The Extensions page scrolled sideways once modules were installed, because the words beside a module's switch never wrapped. They now wrap in the row, and on a narrow window the source column and then the author column give way.
- A confirmation longer than the window, such as an install that adds many extensions, put its title and its buttons out of reach. Its body now scrolls, and the title and the buttons stay in view.
- A file uploaded to a printer from its Files tab now shows in the open folder without leaving and returning.
- A page that drew a strip of tabs further down, such as the 3MF view, jumped to that strip when it opened. The strip now scrolls on its own to the open tab, and the page stays where it is.
- Resetting a printer reported "the printer did not answer" even when it had restarted, because it drops the connection as it reboots. A reset now succeeds when the connection ends after the command is sent.
- A temperature typed between the slider steps stays as typed, and an editor opened on such a target no longer rounds it to the nearest step.
- An installed extension's title is a link to its page in the narrow list too, not only in the table.
- After the first-run wizard the log no longer warns that the server has no administrator. The administrator the wizard named exists by then.
- Saving an extension's ports, data folder or other grants on its settings stopped the extension and left it stopped, so its page was gone after a reload. The extension now restarts with the new grants.
- A refused update step, such as Apply on an update that already finished, said that the request did not match the API schema. It now answers `core.resource/UPGRADE_CONFLICT` with status 409, and a bundle or backup that fails a check answers `core.resource/UPGRADE_REFUSED` with status 422, each naming the reason.
- Settings, Updates kept showing an applied update as rehearsed, with Apply offered, after the server restarted on the new version. The page now reads the update again when the server comes back, and shows it committed or rolled back.
- Settings, Updates asked every server to confirm that its PostgreSQL data is backed up, and let Apply go ahead without it. It now asks only a server with a PostgreSQL database that the update's backup does not hold, and keeps Apply disabled until it is confirmed. `GET /api/v1/server/version` says so in `postgres_backup_ack_required`.
- A server that a published alpha release had set up no longer refuses its records and its history after an upgrade. A comment in their first migration was edited after the release, which changed the checksum the database had recorded.
- With `core.observability.addr` set, the API address answers `/metrics` with 404 instead of the web app's page. A scraper pointed at the wrong port now reports the target as down.
- [sdk] A module's styles no longer reach the markup of another module. One module's `.flex-col` stacked the settings of the 3MF view in one centred column. A bundle now narrows its styles to its own markup once the host serves it, and a component one module draws from another keeps its own styles.

## 0.0.1-alpha.6 - 2026-10-19

### Added
- The server image carries the Prusa printer logs extension in `/opt/modules/buddy-debug-logs`. Name it in `core.modules_local` to offer it, and publish its UDP ports.
- An installed extension that listens on the network has a Ports tab in Settings, Extensions, where you allow its ports or change their numbers. `GET /api/v1/resources` answers the ports an instance holds as `module.listening`.
- The install dialog on the Extensions page lets you choose the number of each port an extension listens on, once you allow the ports.
- Settings, Diagnostics offers to include the files installed extensions keep, such as the log lines your printers sent, ticked by default and naming the extensions.
- The install dialog on the Extensions page lists the network ports and the folder an extension asks for. It grants the ports only when you tick Allow it to listen on these ports.
- `POST /api/v1/diagnostics?extensions=true` adds the files installed extensions offer to the bundle, under `modules/<kind>.<name>/`. They are scrubbed like the server's own logs, file names included, and each extension adds at most 8 MiB and 1024 files. `GET /api/v1/diagnostics` names the extensions that offer any.
- [sdk] A module adds text files to the diagnostics bundle by serving `diagnostics.collect` through `diagnosticsapi.Serve`, one page at a time.
- A Prusa printer's network is on its state tree under `/network`: the MAC and IPv4 address of its Ethernet and Wi-Fi interfaces and its host name. The printer reports them when it connects. An interface that is down has no entry.
- A module that listens on a network port is installed without the port until you approve it apart, with `-approve-listen` on `module add` or `approve_listen` in `POST /api/v1/modules`. `-listen` and `listen` move a port, and a module that asks for a data directory gets one under `module-data` beside the modules directory.
- [sdk] A module lists the ports it listens on and asks for a data directory in a `needs` block in `module.yaml`, and reads what the operator granted with `wirerpc.ConfigureGranted`.

### Changed
- The documentation links in the first-run wizard and the app open the documentation of the version you run, so a later release that moves a page does not break them.

### Fixed
- A printer still working on an earlier command refused a new one with the message that it gave no reason. The message now says the printer is busy with another command, so wait for it to finish and send the command again.
- A screen could stop drawing when it showed an error or a text that no translation holds. It now shows the text's key, as intended.
- The first-run wizard could not be confirmed when the configuration file set the printers' address or a sign-in key. The wizard sent the key anyway, and the server refused the whole answer. It now leaves out a key the file sets and names the ones whose value differs. A sign-in or certificate setting that differs from the file stops the wizard until that step matches.

## 0.0.1-alpha.5 - 2026-10-19

### Security
- A printer removed with `DELETE /api/v1/devices/{device_id}` kept its token, so it connected again and joined as a new printer without an approval. Removing a printer now takes its token and approved fingerprint back, so outside automatic admission it has to be added again, including a printer an earlier build removed.

### Removed
- The USB token tab on the Add a printer page is gone, and so is every mention of a token in the settings file it writes. A printer gets its token from the server when you choose Add Printer to Connect on it and its code is approved.

### Changed
- The Add a printer page is a guide in four steps: prepare the settings file, set up the printer, approve it or watch it join, and see it connected. It shows each printer the moment it asks, says why a printer was refused, and opens help after a quiet wait.
- The Add a printer guide lists its steps down the side of a wide page, and a narrow page shows the step number with a bar. The settings file and its values are behind one disclosure, and the printer step shows the menu to open on the printer.

### Added
- A release can sign the macOS binaries with a Developer ID and have Apple
  notarize them, so macOS opens a downloaded binary without asking to allow it.
  Its notes say whether it did; without the signing secrets it goes out
  unsigned as before.
- Settings, Diagnostics has a Send to Prusa button beside the download. After the same consent tick, the browser uploads the bundle to the Prusa beta portal and opens the portal's page. There you sign in and attach it to a new or an existing report. The server sends nothing itself. `core.diagnostics.send_url` names the portal, `https://beta.prusa3d.com` by default, and an empty value hides the button. `GET /api/v1/ui` serves it as `diagnostics_send_url`.
- The bell lists a printer waiting for approval, a printer approved here that has connected, and a printer the server refused. Each notice links to the page that deals with it, and a waiting printer also raises a toast outside the Add a printer page.
- `GET /api/v1/ui` answers `printer_admission`, which is `approval` when a person approves each new printer and `automatic` when every printer that asks is approved at once. With several connectors it is `automatic` only when all of them approve on arrival.
- A printer the server refuses on `/p/ws` is published on the event stream as `com.prusa.connect.device.registration.refused`, at most once a minute per printer. The event names the connector, the reason, and whether that connector asks a person to approve printers. It carries the model and firmware the printer reported, but never its fingerprint in clear or its network address.
- [sdk] A driver can take back the credential it issued for one device through `connectorapi.ForgetsDevices`, and the host calls it when that device is removed. The xBuddy connector implements it.
- [sdk] A driver tells the host about a device it turned away through `connectorapi.ReportsRefusals`, and says whether it approves every registration on arrival through `connectorapi.ReportsAdmission`. `event.RegistrationRefused` is the payload of the refused event.
- The server logs a warning when it refuses a printer's websocket with 401 or 403, at most once a minute per printer. The line gives the reason, the printer's fingerprint, its network address and the admission mode, and says how a printer gets a token this server accepts.

### Fixed
- A screen reader announced each select in the web app as a button, named by its label alone and not by the value it shows. A select is now a combobox. The temperature chips on a printer's page left out the reading they show, and now read it after what pressing them does.
- A module release archived with macOS `tar` installs: the `._` files it adds
  are skipped instead of refused as files `checksums.txt` does not list.
- The settings file on the Add a printer page carried `token = <TOKEN>`, and the page said a printer without a token still connects. A printer with a token the server did not issue is refused, and one with no token sends nothing. The file now carries no token. The steps say to choose Add Printer to Connect on the printer and approve its code.
- On a server that approves every printer on arrival, a new printer was also announced as waiting for approval, with `com.prusa.connect.device.registration.announced`. Such a code is no longer announced, and its approved event is its only event.
- On a server that approves every printer on arrival, an approval put no `com.prusa.connect.device.registration.approved` event on the stream, so a page could not learn the new printer's device id. The event is now published for that approval too.
- A printer that chose Add Printer to Connect appeared on the Add a printer page only after a reload. The page now shows it as soon as it asks, and removes it once somebody approves or rejects it.
- A printer that dialled `/p/ws`, `/p/register` or `/p/auth` where no printer connector serves them got the web app's page and showed `Srv error`. The server logged nothing. It now answers 404 and logs a warning naming the path, at most once a minute.
- The Storage page offers a print button only on G-code files, so a 3MF project or any other file can no longer be sent to a printer from there.
- A file that did not reach a printer is announced with the reason the server recorded. Before, the announcement omitted the reason, so a file the printer refused for its type seemed to be a broken upload.
- On a CORE One or an XL, the up arrow of the Z controls moved the bed down. On these printers the arrows now move the bed in the direction they point, and their labels say the bed moves.
- A printer that dialled before the first run had recorded how a new printer
  joins was let in without anybody approving it. That covered a printer
  connected while the setup questions were still open, and one connected after
  "ask me first" was chosen but before the server was restarted. Such a server
  now holds every new printer at the door until a choice is recorded, so the
  printer shows its code and waits for approval.

## 0.0.1-alpha.4 - 2026-10-17

### Changed
- The server refuses to start when `core.frontend.printer_address` names `localhost`, a `127.x` address, `::1` or `0.0.0.0`, which a printer cannot dial. Set it to the address printers reach the server at, or remove it, in `config.yaml` or `config.d/setup.yaml`.

### Fixed
- The setup wizard now asks the address printers use even with encryption off, so the add-printer page no longer writes `hostname = localhost` for a server set up from `localhost`. The add-printer page warns when it would hand printers a loopback address.

## 0.0.1-alpha.3 - 2026-10-16

### Changed
- This release does not upgrade a database 0.0.1-alpha.2 or an earlier build made. The server refuses to start on one and names a table it found there. Delete the database `core.authz.database` names, or its tables on PostgreSQL, and the history store's and the module store's files. Delete the file `core.overlay` names too, which holds the first run's answers, so the setup wizard runs again. Then register the printers again.

### Fixed
- A websocket handshake on `/p/ws` with no `Fingerprint` header was admitted as
  a printer named after its network address. A browser tab or a port scanner
  then appeared in the fleet as a new device on every connection. Such a
  handshake is now refused with 401 in every admission mode, and `GET /p/auth`
  refuses it the same way.
- The upload zone on a printer's page offered `.3mf`, which the printer refuses.
  Such a file was saved on the server, failed with "Unsupported file type" and
  never appeared on the printer, and the zone now turns it down before upload.

## 0.0.1-alpha.2 - 2026-10-16

### Security
- An HTTP/2 client could crash the server through a race in the header encoder (GO-2026-6617). The server is built with `golang.org/x/net` v0.60.0, which fixes it.
- A module install fetches a release only over https, and every redirect it
  follows is held to the same rule. A release whose bytes do not match the
  `sha256` the request or the marketplace states is refused with
  `MODULE_DIGEST_MISMATCH` before it is unpacked.
- A module download never connects to this machine or the local network, checked
  after the name is resolved, except to loopback when the marketplace itself is
  configured on loopback. A failed download answers `MODULE_DOWNLOAD_FAILED` (502) with the
  same detail whatever the address answered.
- `POST /api/v1/modules` with a URL in `from` requires `sha256`.
- An install with `approve` is refused with `MODULE_REQUESTS_CHANGED` when the
  release asks for more than the marketplace lists for it, or more than the
  request's new `expect` states. Nothing is granted that the operator was not
  shown.
- A release whose name another module already holds is refused with
  `MODULE_NAME_TAKEN` instead of replacing that module's files and declaration.
- `PUT /api/v1/modules/marketplace` takes the admin role on the organization
  instead of configure_resource on one resource kind.
- A module release that carries `checksums.txt` is refused when it holds a file
  that list leaves out. `module add` and `POST /api/v1/modules` both refuse it,
  so no file is installed that nothing checked.

### Changed
- The web app loads the setup wizard and its texts only while the server is not set up, so every page after the first run fetches about 78 kB less. `SetupWizard` moved from `@prusa/connect-ui/shell` to its own subpath, `@prusa/connect-ui/setup-wizard`.
- `/metrics` follows `core.observability.enabled` when the server starts. A release binary, which carries the metrics client, answered every scrape before; it now answers 404 until the key is true, so set `core.observability.enabled: true` to keep scraping it.
- The page of a print opens for every print the server holds, however long ago it ran. It reads the print by its id, where it walked the printer's five most recent pages of history and gave up on an older print.
- [sdk] A print's `id` is a UUID instead of `<device id>:<job number>`. The printer's job number is the new `device_job_id` on `GET /api/v1/devices/{device_id}/jobs`, the job history, the job on each printer in the listing, the `device.job.started` and `device.job.settled` events and `deviceapi.Job`. A client that read the printer or the job number out of the id reads `device_id` and `device_job_id` instead, and reads one print by its id at `GET /api/v1/jobs/{job_id}`. A link to a print saved before the upgrade no longer resolves.
- A database this release has upgraded cannot be opened by an older server. The upgrade deletes the prints stored before it, because their ids named the device and the machine's job number, and no release shipped those ids. A print a machine is running across the upgrade is recorded again from its next reading. The history keeps the facts of the deleted prints under their old ids, which `GET /api/v1/jobs/{job_id}` does not answer.
- Building from source needs Go 1.26.9.
- A module's pages and panels appear and disappear without a reload as it is installed or removed. Another open tab shows an install at once and a removal once it is shown again.
- `core.device_listing_cache_bytes` bounds the printer folder listings the server
  keeps in memory, across every printer. It defaults to 4 MiB, and to 256 KiB on
  the printer build.
- A printer's Files tab opens a folder it showed before at once.
  `GET /api/v1/devices/{device_id}/files` answers a repeat request from the last
  listing with `stale: true`, and `refresh=true` asks the printer again.
- After a printer lists a folder, the server fetches the thumbnails of its print
  files one at a time while the printer is idle, so they show without waiting.
- Thumbnails of files on a printer survive a restart. The server keeps them in
  `config.d/printer-thumbnails/`, and drops the oldest first.
- `core.device_thumbnail_disk_bytes` bounds the printer thumbnails kept on disk.
  It defaults to 64 MiB, and to 4 MiB on the printer build.
- `core.device_thumbnail_prefetch` and `core.device_thumbnail_disk` turn the
  background thumbnail fetch and the disk copy off. Both are on by default and off
  on the printer build.
- The web app calls what the Extensions page installs an extension, on that
  page, in the command palette and on an extension's own page.
- Updating a module with `approve` keeps what the operator decided: a permission
  or an event they took away stays away, and a records room they set is kept.
  The answer's new `added` lists what the new release asks for that the old one
  did not.
- The module marketplace status reports `reachable: false` and `stale: true`
  when the index serves a copy kept after a failed refresh.
- The module index never offers a beta release as the update of a module
  installed from the stable channel, and each marketplace row says its `channel`.

### Removed
- `core.mqtt.enabled`, `core.mqtt.broker.enabled` and `core.docs.enabled` are gone, because none of them changed what a running server does. A file that still names one is refused at startup as a key this build does not read. To send the printers' state to an MQTT broker, such as Home Assistant's, declare a `resources.sink` row of type `mqtt`. The build links the documentation, and `make docs` builds the site.

### Added
- The setup wizard offers sign-in at your own OpenID Connect provider on both paths, with the administrator named by their `sub` at the provider, which it writes as `core.principal`. It reads the provider's discovery document before it restarts, and refuses an issuer it cannot read or a subject this server cannot name.
- `core.auth.client_secret` sets the secret a confidential OpenID Connect client authenticates the code exchange with, which Google and a new Keycloak or Authentik client need. The wizard writes `config.d/setup.yaml` readable by its owner only when it holds a secret.
- `POST /api/v1/setup/oidc` tries a sign-in provider behind the setup code without signing anybody in, and reports whether the discovery document, the client id and secret, and the redirect address are taken. The wizard's provider step has a button for it.
- The documentation has a Configuration section right after the overview, with one page per part of the server and an anchor for each key. Its first page says how the configuration works.
- The footer's Support link opens the assistant on the beta portal, and the footer no longer links the repository.
- The home dashboard offers to add the first printer while the server has none, above the dashboard itself.
- The setup wizard's storage step says a folder on a NAS works, that more storage providers such as Amazon S3, Google Drive and Nextcloud are planned, and links the guide to writing one. The Storage page describes the `filesystem` type the wizard declares.
- Each field of the setup wizard links to its own row in the new Configuration section of the documentation. It also links to the guide about its part, and each step links to the page of its part. `get-setup-configuration` carries `reference` and `guide` for every key.
- The setup wizard keeps each printer's history by default. `POST /api/v1/setup` takes `history` and declares a `history:past` SQLite store in `config.d/history.db`, kept for 2, 7 or 30 days or for good.
- The setup code banner a new install prints names the addresses to open the wizard at. They are this machine and its addresses on the local network.
- `core.frontend.printer_custom_ca` makes the add-printer page write `custom_cert = 1`, so a printer trusts the `connect.der` on its USB stick. The setup wizard turns it on when it encrypts the printers' connections. `GET /api/v1/ui` carries it as `printer_custom_ca`.
- The setup wizard encrypts the web app, the printers' connections, or both. `POST /api/v1/setup/certificates` makes an ECDSA authority and two certificates under `config.d/certs`, and `POST /api/v1/setup` takes `printers` to give printers a TLS 1.2 listener of their own.
- Every build links its documentation at the address the build variable `DOCS_URL` names, the beta portal while the project is private. `{version}` in it is replaced by the version. `GET /api/v1/setup` and `GET /api/v1/ui` carry it as `documentation`.
- `POST /api/v1/setup/storage` checks, behind the setup code, whether the server can keep uploads in a folder, and creates nothing. `POST /api/v1/setup` takes `mqtt` and declares a `sink:mqtt` instance for that broker, and refuses a storage folder the server cannot write.
- A new install opens a setup wizard in the browser. It asks the basic or every setting, keeps the answers across a reload, explains each key, speaks seven languages, and restarts the server on confirm.
- `GET /api/v1/setup/configuration` lists the keys the first run may set, behind the setup code in `X-Setup-Code`. `POST /api/v1/setup` takes `configuration` and `storage`, writes `config.d/setup.yaml`, answers 202 and restarts the server, which then records the decision and creates the administrator.
- The server reads `config.d/setup.yaml` under `config.yaml`, and a key in `config.yaml` wins. The first-run wizard writes that file, and an operator may edit or delete it.
- An org admin downloads a redacted diagnostics zip from Settings → Diagnostics, or from a crashed page, and attaches it to a support ticket. It holds recent logs, the last crash, module and printer state and the browser's errors. The server scrubs the secrets, addresses and names it recognises, and the download waits for the admin to agree to share the file. Module output is included only when ticked. `POST /api/v1/diagnostics` builds one bundle at a time, answers 429 `DIAGNOSTICS_BUSY` meanwhile, and keeps nothing.
- The diagnostics bundle is built into the full and printer builds and left out of the minimal one. A server with no sign-in leaves it out unless `core.diagnostics.enabled` is written as `true`, and `false` leaves it out of a full build too. Then `GET /api/v1/ui` reports `diagnostics: false`, so the app hides the download.
- `core.frontend.support_url` sets where the web app's Support link points, such as a beta ticket portal. Empty keeps the assistant on the beta portal. `GET /api/v1/ui` serves it as `support_url`.
- `POST /api/v1/modules` takes the module's settings in `settings`, the same
  keys `module add -settings` reads from a file. A module with a required
  setting, such as the address of the system it sends to, is then installed
  and started in one call.
- An operator hides one place an extension draws in, such as its line on every printer card, from the extension's Where it shows tab. The list is `hidden_contributions` on the instance, settable in the YAML file too; a hidden page loses its menu entry and still opens by link.
- Each extension on Settings → Extensions has one on/off switch. Its title opens its own page with its state, what its release asks for, and a tab per settings pane it draws.
- An extension's options are edited on its page, in the Settings tab. Each value is checked against its type before saving, and a stored secret is kept unless replaced. A save from a stale page is refused and says so.
- A module instance read from `/api/v1/resources` carries `module.asked`, what its install granted, and `module.title`. `module.asked` is absent when the install was not approved.
- [sdk] A module's `module.yaml` may name a `title`, the name a person reads. The Extensions page shows it, and the manifest's `name` where it is absent.
- `core.modules_local` names built module releases on this server, and the Extensions page offers each to install.
- The server image carries the Parts and Fleet stats extensions in `/opt/modules/parts` and `/opt/modules/fleet-stats`. Name them in `core.modules_local` and the Extensions page offers them.
- `/api/v1/dashboards` stores the dashboards the home page draws: list, make,
  read, change against the revision it was read at, delete, and choose the
  default with `PUT /api/v1/dashboards/default`. Who sees one follows the
  organization roles on the new `dashboard` resource kind.
- `core.dashboards.enabled` switches the dashboards off, and `make build` then
  leaves them out of the binary. It defaults to on, the printer profile turns it
  off, and `GET /api/v1/ui` says which with `dashboards`.
- [sdk] `com.prusa.connect.dashboard.changed` reports a dashboard made, changed
  or deleted, and the param key `limit_dashboards` is published.
- The home page is a dashboard of cards: printers, prints that end next, a
  camera, a temperature chart, a clock and notes. You arrange them on a grid,
  share a dashboard by its address and show it on a wall screen with
  `?kiosk=1`. With no dashboard, the home page draws one from your printers.
- [sdk] A module adds cards to the dashboards with `contributes.cards` in its
  manifest and `register.card` in its bundle. The core draws each card's
  settings from its schema.
- [sdk] `host.pageHref(path)` answers the address of one of the module's own
  pages, so a card can link to it without knowing the namespace.
- The Extensions page opens the install question for an extension a link
  names, which is how the marketplace's Install in Prusa Connect button reaches
  this server. The question lists every request the install grants, and the
  answer says what was withheld or added.
- Each row of `GET /api/v1/modules/index` carries the release's `events`,
  `check_absent_people` and `records_mib` beside `permissions`, and
  `requests_known` says whether they are complete. A local `index.json` row may
  state them, or the server reads them from a release directory it points at.
- The print dialog sends one file to several printers at once: tick each
  printer, and the dialog stays open on any that did not start, saying why.
  A printer of another model than the file was sliced for is marked.
- The storage page renames a file or a folder from its row, keeping a file's
  extension.
- The web app signs in through the identity provider when the server uses one
  (`GET /api/v1/auth/info` answers `redirect: true`). The sign-in shows a single
  "Sign in with <provider>" button instead of the password form, and returns you
  to the page you were on afterwards.
- The printer's Commands tab now offers `set_token`, which no other screen
  reached.
- The last prints on a printer's page show when each print started and ended,
  beside how long it took.
- Expanding a printer's camera on the printer page plays the live stream when the
  camera serves one, instead of the last still frame.
- `core.frontend.printer_address` sets the origin printers dial, and the
  add-printer page writes it into `prusa_printer_settings.ini` instead of the
  address the page was opened on. `GET /api/v1/ui` serves it as `printer_address`.

- The printer page's Status tab shows a question the printer is waiting on, with
  its title, text and buttons. A caller who may operate the printer answers it
  from there.
- The add-printer page downloads `prusa_printer_settings.ini` as a file, beside
  the button that copies it.
- [sdk] A module may ask for more room for its records than 8 MiB with
  `records_mib` in its manifest, from 1 to 1024 MiB. An approved install writes
  it as `service.records_mib`, and `module add -approve -records-mib <n>` or
  `records_mib` in `POST /api/v1/modules` grants another amount.
  `GET /api/v1/state/namespaces` answers each module's `limit`, and the
  `state.usage` call answers what a module holds against its limit. A record
  backend now takes the limit in `stateapi.Backend.Records` and answers
  `Usage`. `stateapi.MaxNamespaceBytes` is renamed
  `DefaultNamespaceBytes`.
- A print file that is only on a printer now shows its thumbnail in the
  printer's Files tab. `GET /api/v1/devices/{device_id}/files/thumbnail?path=`
  serves the picture the printer reads out of the file, and answers 404 with
  `core.storage/NO_THUMBNAIL` when the file carries none.
- [sdk] `connectorapi.File` has a `Preview` field, the encoded picture a
  device read out of a print file, filled when a listing is of that one file.
- `core.marketplace.url` names a marketplace whose module catalogue
  `GET /api/v1/modules/index` lists after the local index.
  `POST /api/v1/modules` with no `from` installs the release that marketplace
  offers for the module and version.
- The server reads the module catalogue of the Prusa Research marketplace by
  default, and `core.marketplace.enabled: false` switches it off. It reads only
  when the module index is asked for, and sends its version and platform.
- The Extensions page lists every module this server has installed or can
  install in one table, searched by name and narrowed by source and status.
  It installs, updates and removes them after a confirmation that names what
  each asks for.
- The Extensions page narrows its listing by the marketplace's trust level too.
  An extension Prusa Research publishes carries an Official badge drawn apart
  from the others.
- The marketplace can be switched off and on again from the Extensions page,
  through `PUT /api/v1/modules/marketplace`, with no restart. Switched off, the
  server fetches nothing from it and an install that names no `from` is
  refused with `MARKETPLACE_DISABLED`.
- The demo takes `-marketplace` on its command line, so
  `make demo DEMO_ARGS="-marketplace http://localhost:4455"` lists and installs
  modules from a marketplace running on the same machine.

### Fixed
- Remove printer in a printer card's menu on the Printers page did nothing and sent no request. It now asks to confirm and then retires the printer with `DELETE /api/v1/devices/{device_id}`.
- A long file name in the Storage tile view is clipped in the middle inside its tile instead of running over the tiles beside it.
- The Jobs page and a job's own page show the picture inside the printed file,
  read from the printer when this server did not send the file.
- A print that reuses a job number the printer issued before, after a firmware reset or when its counter wraps, is recorded as a print of its own. Before, the server reported no current print while the printer printed it, and the print never reached the history.
- [sdk] The events conformance suite fails a sink whose `Close` does not
  return in every case that closes it, instead of hanging the test binary
  until its deadline.
- An install that cannot write into the modules directory answers
  `MODULE_INSTALL_FAILED` (500) and names the directory, instead of
  `REQUEST_INVALID` as if the request were wrong.
- The server image carries the root certificates, so it verifies an https
  marketplace or OIDC provider instead of refusing every certificate.
- A server whose version is no semantic version, such as a build stamped with
  a commit, reads the marketplace catalogue without naming its version. The
  marketplace refused such a read, so the Extensions page listed nothing.
- A printer's page kept showing the prints it had when it was opened, so a
  print that ended meanwhile never appeared under "Last prints" until a
  reload. The page now reads the printer's prints again whenever one starts or
  ends.
- Installing another release over an installed module stops the running
  process before its files are replaced, and the module starts again on the
  new release.
- Two module installs or removals sent to `POST /api/v1/modules` or
  `DELETE /api/v1/modules/{namespace}` at once take turns, instead of racing
  over the same module's files.
- A removal through `DELETE /api/v1/modules/<namespace>` now waits for an
  install of the same module to finish, and takes back the grants of the
  declaration it finds then.
- Two installs of the same module through `POST /api/v1/modules` now run one
  after the other, so neither plans its grants from a declaration the other is
  replacing.
- A module reinstalled with `POST /api/v1/modules` or `module add` now keeps
  its stored settings when none are sent. A credential sent back as `xxxxx`
  keeps the stored credential, as it does with put-resource.
- A reinstall keeps a stored setting only when the new manifest declares it
  with the same type, and keeps nothing when the module path changes. A stored
  credential is no longer shown in plain text after a reinstall that retyped or
  removed its option.
- A reinstall through `POST /api/v1/modules` that would take a grant back is
  now refused before it writes anything when the caller lacks the admin role on
  the organization. An approved install that records a permission or
  `check_absent_people` now needs that role too, even when no grant is written.
- The answer to `POST /api/v1/modules` now says in `state` whether the module
  started, and gives the cause in `reason` when it failed. `running` is no
  longer true for a module that failed to start, such as one missing a
  required setting.
- An install through `POST /api/v1/modules` with `approve` now writes the same
  grants as `module add -approve`, so a module that asks for observe reads the
  printers. The grants are written as the caller, who needs the admin role on
  the organization, and `approved` lists only what a grant was written for.
- A printer whose driver panics while reporting what the printer has now
  connects with the commands its profile allows.
- A printer at a connector endpoint, an address this server dials, now lists
  its files and previews and takes uploads and file and folder changes.
  Before, `/api/v1/devices/{device_id}/files` answered
  `CAPABILITY_NOT_SUPPORTED` (409), the device's `capabilities` listed
  commands the printer's driver had not reported, and the printer showed as
  connected when its driver could not reach it.

## 0.0.1-alpha.1 - 2026-10-02

### Added
- The web app returns you to the page you were on after your session expires.
  A 401 from any call, or the event stream ending with `auth.expired`, shows the
  sign-in with a notice, and signing in again opens the same address. Only an
  address on this server is followed.

- [sdk] The TypeScript client takes an `onUnauthorized` option, called when the
  core answers any call with 401.

- A printer's Controls page sets the nozzle diameter, high flow and hardened
  nozzle of each toolhead, and changes the printer's hostname. Each control
  shows only on a printer that accepts its command.

- A command sent from a printer's page ends in one notification: done, refused
  with the printer's reason, or outcome unknown, with dropped or replaced
  parameters and the time taken. Each send carries its own idempotency key, so
  a request repeated while the first one runs reaches the printer once.

- The API credentials page asks how many days a new credential stays valid,
  from 1 to 365 and 90 by default.

- The last print on a printer's Info page shows when it started and when it
  ended, and an error message shows its trace ID with a copy button.

- The storage page has a New folder button on a storage that accepts writes. It
  creates the folder inside the folder you are looking at.

- The printers page has a Retired printers button. It lists the printers an
  operator removed and those registered again. Each shows why it was retired.
  Its page keeps the Info and History tabs and offers no commands.

- A printer card on the printers page names the severity of its first issue. It
  shows the sentence and the action from the device-issues vocabulary. For a
  reason the vocabulary does not list, it shows the machine's own words.

- The History tab of a printer lists every command stored for it: who sent
  it, when, how it ended and how long it took. Opening a row shows the
  command from request to outcome.

- Settings has a Server pane that says whether the server is ready and how
  the printers behind each connector are doing.

- The bell in the header lists finished and failed prints, printers that stop
  answering and new printer issues while the app is open. A failed print also
  raises a toast.

- `make demo` keeps its printers printing, with the odd cancelled or failed
  print, and starts with 21 days of recorded print history.

- The printers page searches by name, model or serial, sorts by any order the
  server offers, and filters by the states and models present in the fleet. A
  strip above the list counts the printers with errors, warnings or notices,
  and a click lists only those.

- `GET /api/v1/devices` describes how the fleet may be filtered and sorted.
  `filters` lists each filter with the values present among the printers you
  may see and how many each would list. `sorts`, `sort` and `order` say which
  orders exist and which was applied, and the issue summary adds
  `by_severity`.

- The printer page has a pencil beside the printer's name that renames it.

- A Prusa XL with two to five toolheads is drawn with that many heads.

- The MK3.5, MK3.9, CORE One L and the INDX models are drawn with their own
  picture instead of the generic printer, as are the HT90 and the MMU and
  enclosure variants.

- On a tool changer, the nozzle temperature chip in the printer page header
  lists every toolhead with its material and temperature. Picking one sets
  that toolhead's target, and each row of the Temperatures controls' Tools
  block sets its own toolhead.

- `core.command.set_nozzle_temperature` takes an optional `tool`, 1 to 5, on
  the Prusa XL and the INDX models, and heats that toolhead alone. Without it
  the command heats the active toolhead, as before.

- The nozzle temperature editor on a tool changer marks the active toolhead
  and names it on the target field, such as `Target for T2`.

- On a Prusa XL or an INDX tool changer, the printer page's Temperatures
  controls list every toolhead with its material and temperature. The header
  names the active tool, such as `T2`.

- `GET /api/v1/vocabularies/device-models` marks the Prusa XL and the INDX
  models with `tool_changer: true`, since each of their slots is a toolhead of
  its own. The set's format is now 1.1.

- A Prusa XL's device state carries each toolhead under `/slot/<n>/`, with
  its `temperature`, `material`, `fan_print` and `fan_hotend`, and the active
  tool as `/slot/active`. A printer with an MMU reports its feeders there.

- [sdk] A storage provider can implement the optional
  `storageapi.ExtensionPolicy` interface to report the upload allowlist it
  enforces.

- `GET /api/v1/storages` returns `allowed_extensions` for each storage, the
  extensions an upload to it accepts, where an empty list means any. The
  Storage page's upload zone offers only those files and names them.

- The footer links to the server's API reference at `/docs`. `GET /api/v1/ui`
  returns the page's path as `api_reference` and omits it in a build that
  leaves the page out, where the link is not drawn.

- `GET /api/v1/principal` also returns `display_name` and `username` when the
  caller is a local user account. The user menu shows that name instead of the
  account's subject, such as `user:operator`.

- The metrics can be served on a port of their own, which
  `core.observability.addr` names. That port asks for no credential, so publish
  it to your collector only; the API port then answers 404 at `/metrics`.

- The nozzle-watch example now adds a line to each printer card, a tab and a
  menu item to each printer's page, besides its Status-tab section.

- `make new-module` offers the printer listing line, the printer page tab, the
  Status-tab section and the printer action menu, grouped by where they
  appear.

- [sdk] A module can declare `device_action` to add an item to the action
  menu of each printer's page. The host runs it with the printer's id and
  shows a failure as a toast.

- [sdk] A module can declare `device_tab` to add a tab of its own to each
  printer's page, routed at `/printers/<id>/<namespace>.<id>`.

- [sdk] A module can add one line of its own to each printer's card in the
  Printers page listing, under `ui.contributes.device_card` and
  `host.register.deviceCard`. The line is cut to one line rather than
  wrapping or growing the card.

- Each release now carries its source code as
  `prusa-connect-<version>-source.tar.gz`, listed in `SHA256SUMS`. A tag
  with a hyphen, such as `v0.1.0-beta.1`, is published as a prerelease, and the
  release notes carry that version's section of this changelog.

- A server that finds no configuration file now logs a warning naming the path
  it read. It also names any `config.yaml` it found in the folder it started in
  or beside the binary, which is where a file unpacked in the wrong place is.

- Each release now carries `config.yaml`, the configuration to start from, and
  `prusa-connect.service`, a systemd unit for running the server as a
  service. The unit runs it as a user of its own, with its files under
  `/var/lib/prusa-connect`.

- Each release now carries the full build for Windows and macOS, on Intel and
  on ARM, beside the Linux binaries, and `SHA256SUMS` lists them too. They are
  not signed, so Windows SmartScreen and macOS Gatekeeper warn before the first
  start.

- The answer to `POST /api/v1/modules` lists in `withheld` each permission an
  approved install could not give, and `approved` lists the events it gave.

- The answer to `POST /api/v1/modules` lists in `approved` what an install
  made with approve gave the module: each permission, and
  `check_absent_people`.

- [sdk] `access.check` refuses an object type longer than 64 bytes with
  `ErrInvalidQuestion`, as it refuses a long subject, action or object id.

- An operator can let a module ask whether a person may still do something
  while that person is away, by approving `check_absent_people` on install. A
  print queue then keeps printing after a restart, and the server still reads
  the person's current rights for every question and answers no for a deleted
  person.

- [sdk] A module's `module.yaml` may ask for `check_absent_people: true`, and
  `sdk/accessapi` refuses with `ErrTooManyQuestions`, code 6003, when a module
  asks about people it is not serving too often.

- The Info tab of a printer's page lists the printers nearby, each with a link
  to its own page. A new Network room in Settings has the switch that hides
  this printer from them.

- A server lists the printers it hears on the local network at
  `GET /api/v1/discovery/nearby`, and `PUT /api/v1/discovery/visibility`
  hides it from them at once. `core.discovery.hidden` sets whether a server
  starts hidden, and a choice made through the API wins over it. The choice
  is kept under a `discovery` key in `config.d/resources.json`, and a build
  older than this one refuses that file until the key is removed.

- The server announces itself on the local network over mDNS, so a slicer
  on the same network finds a running instance without a typed address.
  `core.discovery` switches it and lists the names it announces under;
  inside a container the start-up log says the address reaches no other
  machine and names the setting that changes it.

- A Tags page lists every tag the printers carry and the printers under each
  tag. Its button for a tag opens the Printers page filtered to that tag. The
  Labels page and its setting are gone.

- The Printers page can be filtered by tags. The server answers which printers
  carry every chosen tag, and the filter is offered once some printer carries a
  tag.

- A printer's Info tab shows the printer's tags. A person who may administer the
  printer adds a tag by typing it and removes one with its button. A tag the
  server refuses is shown with its error code, and the list shown returns to
  what the server holds.
- `make build-printer` builds `bin/server-printer`, a static Linux arm64 binary
  that runs on the printer's own board. It carries the full build and the web
  app, and no other build changes.

- The printer build leaves out the API reference page, so `/docs` answers 404
  on a printer. `/openapi.json` still serves the API document for phone apps
  and generated clients.

- The printer build leaves out the developer tools page at `/dev` and the
  operations under `/api/v1/dev/`. The API document of the printer build does
  not list those operations.

- The printer build knows the capabilities of one printer model, the one
  `PRINTER_MODEL` names when it is built. A printer of another model that
  connects to it gets only the capabilities its driver declares for every
  model.

- Each release publishes `prusa_connect_printer_aarch64`, the printer
  build for the printer's own ARM64 board. It runs with
  `config/examples/config.printer.yaml`, and a `SHA256SUMS` file beside it
  holds the checksum of every binary in the release.

- The example configuration `config.printer.yaml` runs the server on a
  printer. It sets a small database pool and a memory limit of 12 MiB, and it
  starts no module.

- The SQLite history and record stores take the option `page_cache_bytes`,
  which sets how much memory each connection may use for its page cache. Zero
  keeps SQLite's own size, and the Postgres stores refuse the option.

- The key `core.auth.password_hashes_at_once` sets how many password hashes
  may run at the same time. The default stays 4, and the printer build runs
  one. A login past it waits within its request timeout.

- The keys `core.auth.password_memory_bytes` and `core.auth.password_passes`
  set the memory and the passes of one argon2id password hash. The defaults
  stay 19 MiB and 2 passes, the printer build uses 7 MiB and 5 passes, and a
  value below the OWASP settings is refused. A password stored at other values
  still works and is stored again at the next login.

- The key `core.memory_limit` sets a soft memory limit for the server, in
  bytes. The server logs the limit at start, and the key wins over
  `GOMEMLIMIT`.

- The key `persistence.sqlite.page_cache_bytes` sets how much memory each
  connection to the SQLite database may use for its page cache. The default
  stays the size SQLite picks, 2000 KiB. A server on Postgres refuses the key.

- The key `core.preview_cache_bytes` sets how much memory the preview pictures
  of print files may use. The default stays 32 MiB.

- A module `make new-module` writes outside a checkout installs its UI
  packages from the local registry `make registry-up` starts.

- `make new-module` can start the module it wrote, in the demo or the real
  app, on a free port with the module's dev server in front of it, and
  `go run ./cmd/new-module -start demo -dir <folder>` starts it later. The
  install shows one line instead of what the build prints, and the generated
  `install.sh` installs without starting a demo, into the real app's data
  with `TARGET=app`. Ctrl+C stops both and ends the run with exit status 0.

- `make new-module` can make a new private GitHub repository for a module and
  push the first commit to it, with `-github` or by default when `gh` is
  logged in.
  A push or a `gh repo create` that fails no longer fails the run. The module
  stays written and committed, and one warning line gives the reason and the
  command that tries again.

- A module `make new-module` writes is a git repository with one commit.
  `-remote` adds a remote and pushes that commit, and `-git=false` leaves git
  out.

- `make new-module` checks the tools the module's halves need before it asks
  anything: Node for a UI and Go for a process. It also checks that pnpm is
  installed at all, because pnpm switches itself to the version the generated
  `ui/package.json` pins. A missing or too-old Node or Go stops it, with one line saying how
  to get it.

- A printer's page carries a History tab. It shows the measurements of the
  last two hours as a chart that continues into the live readings, and the
  events of that window as a log. The chart opens on the two temperatures, names each
  line and its unit, and remembers which readings a reader turned on. Both
  halves come from the history store, so a deployment that declares none is
  told so rather than shown an empty page.

- The Details card on a printer's page carries a `Rename printer` control. It
  writes what every listing calls the machine, over
  `PUT /api/v1/devices/{device_id}/name`, which until now no screen reached.

- A printer's file, a recent print and a print's own page draw the plate the
  file was sliced from, where this server sent that file to the printer: the
  transfer record is what names both the copy on this server and where it
  landed on the machine. A file carried over on a USB stick, and one whose
  copy here carries no picture, keeps its glyph. Nothing is matched by file
  name, which would draw another model's plate without saying so.

- A command the printer would not run raises a toast saying so. The refusal
  was written only into the command log on the Advanced tab, so a control
  pressed on the Now or Controls tab read as a control that does nothing.

- The page of a file in Storage shows the picture the slicer wrote into it and
  what the slicer set: the estimated time, the filament, the layer height, the
  nozzle, the material and the printer model it was sliced for. The page held
  a permanent `Not built yet` in place of all of it, although
  `GET /api/v1/storages/{storage}/stat` has answered with them all along.

- A file reaching a printer raises a toast naming it, and so does one that
  failed on the way. The transfer's own row used to sit under the listing
  reading `Done` until the page was reloaded. Now only a transfer still
  moving, or one that ended badly, keeps a row. Moving files on the Storage
  page raises a toast too.

- The Storage page ticks rows. A selection can be downloaded, one file at a
  time as the browser saves them, and moved into another folder through a
  picker that walks the storage one folder at a time. Download needs the
  storage to allow browsing and the move needs it to allow a rename, so a
  storage offering neither shows neither.

- A folder row in the file listing of `GET /api/v1/storages/{storage}/files`
  carries `entries`, how many entries the folder holds directly, and the
  Storage page states it in place of a size. A folder this server could not
  read carries no such field.

- Settings › Appearance holds a fold, `Default view per listing`, that says
  which shape each listing opens in: the printers, the jobs, the files, the
  material catalogue, the labels, the API credentials and the people. The
  toggle on every screen still switches at any time, and a listing a reader
  has already switched keeps their own choice. The picks are stored per
  browser, under `preferences.defaultViews`.

- `docker/server/Dockerfile` takes `--build-arg CONFIG_FILE=` and `--build-arg
  EXTRA_BUILD_TAGS=`, the two knobs `make build-config` already had, so an image
  can be built with a part the checked-in configuration leaves off without
  editing that file. `--build-arg EXTRA_BUILD_TAGS=observability` builds an
  image that serves `/metrics`.

- A person can send a file from the Storage page to a printer. The picker
  offers the printers whose bed is free, says what each of the others is
  doing, and opens the printer's page once it takes the file.

- The Jobs page links each print to a page of its own at `/jobs/{id}`. It reads
  the print from the printer the address names, and says so when a print is
  older than the pages one visit reads.

- A customer can see a picture of the model before printing it. `GET
  /api/v1/storages/{storage}/thumbnail?path=` serves the thumbnail PrusaSlicer
  wrote into a `.gcode`, `.bgcode` or `.3mf` file. It answers 304 to a client
  that sends back the ETag or the Last-Modified it already holds. A file that
  carries no picture answers 404 with `core.storage/NO_THUMBNAIL`.

- `GET /api/v1/storages/{storage}/stat` now carries a `print` object. It holds
  the estimated print time, the filament, the layer height, the nozzle
  diameter, the material, the printer model and the size of the picture. The
  file listing does not carry it, because reading it opens the file. A client
  asks per file it is about to show.

- [sdk] A module reads the same `print` object from `storage.stat`, so it does
  not have to parse a print file itself. `storageapi.FileInfo` carries a new
  optional `Print` field of the new type `storageapi.PrintInfo`.

- `prusa-connect module add` takes `-settings <file>`, a JSON file with the settings of the module. The install writes them into the same record as the rest of the declaration. Settings written in the operator's own file hide that record, and the server then starts nothing.

- An operator can remove a server that waits for approval under a link, with `POST /api/v1/links/{name}/nodes/{id}/reject`. The name that server asked under is free afterwards. It takes the grant approving takes.

- `make mockup-check` drives four servers joined by the Fractal module and fails with a plain sentence when a step does not happen. `make mockup-up` starts them, each with the module installed by `prusa-connect module add`.

- [sdk] A link module can remove one server that waits for approval, through
  the new `link.reject` call of `sdk/linkapi`. The name it asked under is then
  free, so another machine can ask for it.

- The Fractal module removes a server that waited for approval longer than
  `accept.pending_expires`, 24 hours by default. Any machine that reaches the
  join port can put a name in the waiting list, and a full list would refuse
  the server an operator is waiting for.

- The Fractal module joins the server above when the address of that server
  names a scheme, such as `nats://hq:4222`. A join request that does not reach
  the server above writes the address and the error to the log, and the links
  page shows the same reason under the connection.

- The Fractal module shows a printer to the network under its identifier with
  the hyphens removed. A printer identified by a UUID now reaches the servers
  above. A second printer whose identifier gives a name
  another printer took is shown to nobody, and the module writes which one.

- A person at the top server can read the printers of the servers below it and
  send one a command, through the routes the Fractal module serves.
  `GET /nodes` lists the servers and printers below. `GET /nodes/{path}` lists
  the interfaces of one of them with their values, and a POST on a method of
  one of those interfaces sends the command down the tree.

- A module now has a dev loop: `pnpm dev` in its `ui/` serves it from source
  in front of a running core, with hot replacement. A module's
  `vite.config.ts` takes `moduleDev()` from `@prusa/connect-ui/build`
  for it.

### Fixed
- A module run under its dev server (`new-module -start`) no longer restyles
  the app around it. Its stylesheet is scoped to the module's own markup, as in
  its built bundle. Before, a module's `.hidden` could hide the app's sidebar.

- The issue summary on `GET /api/v1/devices` counts every non-retired printer
  you may see whatever filter narrows the page, as its documentation said;
  `state`, `model` and `search` used to shrink it.

- The printers, jobs, storage and home pages list every printer on a server
  with more than fifty, where they listed only the first fifty.

- A long list of accepted file types under the upload zone wraps onto new
  lines instead of running out of the zone.

- A failed or stopped upload stayed in the printer page's Transfers list for
  good, and every one from the printer's past was listed again on each visit.
  The list now keeps only the transfers that moved while the page was open.

- The server gives the memory of a password check back to the operating
  system within a second of a login, a setup or a password change. A server
  with five printers grew from about 45 MB to 100 MB after one login, and kept
  that memory for one to two minutes.

- A print of a long-named file from a USB stick showed the short FAT name,
  such as `1A_PET~7.GCO`, as the current print and in Last prints. The long
  name is shown now.

- The page footer shows the version the server runs: the release tag on a
  release, the commit on any other build. It used to show `0.1.0-dev`.

- The Table view on the Jobs page stays a table on a narrow screen and scrolls
  sideways. It used to turn into the Rows view below about 1350 pixels, so
  switching between the two changed nothing.

- Buttons that look like badges or tabs, such as the temperature and material
  chips on a printer's page and the page's tabs, now show a pointer cursor on
  hover, so they read as something to click.

- The demo no longer announces itself on the local network when it serves on
  a loopback address, which is its default, and its camera views stay on
  loopback too. On Windows the firewall no longer asks to allow it.

- Sending a file to a printer works on a deployment that terminates the
  printers' TLS itself. The xBuddy `chunk_write_ceiling` default is 1011 rather
  than 4096, which puts exactly 1024 bytes on the connection per write and so
  one 1024-byte TLS record. Buddy firmware compiles mbedTLS with a 1024-byte
  record buffer and refuses anything longer fatally, dropping the connection
  mid-transfer; it never advertises the limit, so a host has to know it. The
  shipping server never met this because nginx terminates its printers' TLS and
  reads an upgraded connection in 1 KiB buffers. Transfers are unaffected on a
  deployment behind such a proxy, and no faster there: the bound is the
  printer's own USB write.

- [sdk] A module's stylesheet no longer breaks the host's layout: its
  utilities apply only inside the module's own markup. A module made before
  this needs only a rebuild.

- An upload is bounded at 200 MiB, and the same 200 MiB everywhere: the core's
  own limit, the number the web app refuses at before sending, and the
  `proxy-body-size` the Kubernetes manifests give nginx. The core used to
  accept just under 4 GiB while a deployment's ingress took nginx's 1 MiB
  default, so anything over a megabyte was refused by the proxy with the core
  never seeing it, and the web app offered to send files far larger than either
  would take.

- The release notes no longer promise MQTT autodiscovery or a Home Assistant
  integration, which no release carries. They list the file for each machine
  and what each build contains.

- Every binary now reports its version in its startup log, at `/api/v1/health`
  and for the new `--version` flag, where a released binary reported `(devel)`.
  A release reports its tag, and a `make` build from a checkout reports
  `VERSION` and the commit, such as `0.1.0+3835f5f6e0a1`.

- A server built from a prerelease tag such as `v0.1.0-beta.1` now installs a
  module whose core range is `0.1.0`, `>=0.1.0` or `^0.1.0`. The range check
  reads a prerelease core as its release, where it refused every such module.

- The usage text the binary prints for `-h` or for a word it does not know now
  names the `module` commands and `-version`.

- A module that asks `access.check` about a person it served is told no once
  that person's account is deleted, even when the grants written for them
  remain.
- A request whose client hung up before the answer is recorded as
  `core.validation/REQUEST_ABANDONED` with status 499. It was logged at ERROR
  as `core.internal/UNEXPECTED` and counted as a 500, though the server had not
  failed.

- The authorization vocabulary now names the actions each role really grants.
  The viewer, operator, manager and owner descriptions named actions the server
  no longer checks. The API description of the grant listing named one too.
- A stored password hash with no passes, no lanes or a cost over the upper
  limits is refused as unreadable. Such a row stopped the server at login, or
  made every login against it take as much memory as the row named.

- Two MQTT sinks with no `client_id` no longer get the same client id when the
  server starts them together. The broker disconnected one of them each time
  the other connected.

- The line in notifications that counts the printers waiting opens the
  Printers page.

- The tag filter on the Printers page is kept in the address, such as
  `/printers?tag=workshop`. A note names the chosen tags on every layout of
  the page, with a button that shows every printer again.

- A tag written on a printer's Info tab is added to the list the server holds
  at that moment. A tag another person added meanwhile stays, and an edit
  queued behind a refused one still goes through.

- A bookmark to `/settings/users` keeps its query and its fragment when it
  opens the People room.

- The Settings room for accounts is called People, as in the rest of the
  interface, and its address is `/settings/people`. A bookmark to
  `/settings/users` opens the same room.

- Loading filament on a Prusa printer sends the filament type you picked, and
  the printer loads it without asking when it knows that exact name. An unknown
  name like petg makes it ask on its screen, and a name over seven characters or
  holding a character outside letters, digits, dash and underscore is refused.

- After you change your own password, the Printers list keeps updating. The
  server ends the event stream the old session opened, and the app opens it
  again with the new session instead of showing an error.

- When somebody else signs in from another tab, the Printers list drops the
  printers it held instead of carrying on under the new sign-in.

- When somebody else signs in from another tab, the open screen is mounted
  again for them and the event stream starts again under their sign-in.

- The component library's fleet store opens its event stream again after a
  password change only while the same person is signed in. Given no session,
  it shows the error and opens nothing.

- Right after an upgrade, `admin reset-password` says to start the new server
  once before it runs, instead of telling the operator to use another version.

- Changing your own password keeps you signed in. The answer from
  `POST /api/v1/auth/accounts/{account}/password` carries a new session cookie,
  because the change ends every session, the one it was sent with included.

- Two changes of one password that both knew the old password can no longer
  both succeed. The one that finds the password already changed is refused as
  a wrong current password, which now answers `401 core.auth/LOGIN_REFUSED`
  instead of a server error.

- `admin reset-password` refuses a database that another version of the
  program has set up, and says to use the server's version. It no longer
  changes the database layout under a running server.

- A demo run over a kept directory ends the sessions and API tokens the
  previous run handed out, when it replaces the demo password.

- A module's `install.sh` and `go run ./cmd/new-module -start demo` work from
  a checkout whose path holds a space.

- When `make new-module` made the GitHub repository but the push to it failed,
  the command it prints to try again is the push, not a second
  `gh repo create` that GitHub refuses.

- A module `make new-module` writes passes its own `pnpm check` in `ui/`. It
  lints, formats and type-checks against `@prusa/frontend-standards`.

- The History tab on a printer's page reads its window when the tab opens,
  and asks only for the readings the chart draws. Live readings no longer run
  past the edge of the chart, and a server that keeps only the events or only
  the measurements shows the half it keeps.
- Renaming, labelling and removing a printer wait for the server's answer,
  and a refusal is shown on the card that sent it, by its error code. A
  person the printer's record does not allow to administer it is offered
  none of the three, and an empty name takes the operator's name off.
- A printer's Files tab reads the folder on screen again when a file sent
  from this server lands in it. A folder expanded in the tree is read again
  too, and so is any folder a delete or a new folder changed, while its rows
  stay on screen. Deleting several files sends them one after another and
  names any the printer refused.
- The temperature editors on a printer's page work in Fahrenheit when the
  reader chose it, as the readings beside them already did. A printer whose
  load command takes no filament type offers one `Load filament` button in
  place of the type picker.
- A command the printer ran without one of its arguments raises a toast
  naming that argument. A refused command names the error by its code, with
  the printer's own words under it.
- A file on a printer shows the picture of the newest transfer that wrote
  it, and only when that transfer finished. A failed transfer, or an older
  file sent to the same path, no longer lends it a picture.
- Opening a printer's page no longer raises a toast for every transfer the
  printer ever had. Only a transfer that ends while the page is open raises
  one.
- A module's page in the rail shows the icon its manifest names, from the list
  the component library publishes. A name not on the list shows the puzzle
  piece.
- A module's page sees the printers when a person opens it straight from its
  address. The app now reads the printers for every module page.
- A module's settings panel sees the printers when a person opens Settings,
  Extensions straight from its address.
- `make` and the image build refuse a word in `EXTRA_BUILD_TAGS` that is not
  a build tag of this tree, and name the tags it knows.
- `View all jobs for this printer` on a printer's page opens the Jobs page
  with that printer's prints only. `Open this file` on a print's page opens
  the file on this server whose send to the printer last finished before the
  print started, and is left out when this server did not send it.
- A search in Storage starts on the first page of its results, and opening
  another folder clears it. The folder trail stays in view while you search.
- Moving several items in Storage counts only the items that moved. Each item
  the server refused is named, and its error stays on the page.
- The page of a file in Storage names its storage in the address, so a server
  with two storages shows and deletes the file the reader opened. The folder
  link on that page returns to the folder, and the storage page keeps the
  folder in the address, so Back and a reload return to it. A bookmark to the
  old file address opens the file on a server with one storage, and the
  storage page on a server with more.
- Print again on the Jobs page and on a print's own page opens the print
  dialog on the printer that ran the print. The dialog refuses a printer that
  is busy or still has a part on its bed, and shows the error code of a
  refused start.
- The Printers page lists the printers again at every Home page setting. The
  setting decides only which page the app opens on, and Back no longer returns
  to the address that sent you there.
- Sending a large file to a printer no longer fails at thirty seconds with the
  file on the machine. `[sdk]` The generated client put its own 30-second
  deadline on every call, including `POST /api/v1/devices/{id}/files`, whose
  answer waits for the printer to report the file arrived. The document now
  marks such an operation `x-long-running` and the client leaves its deadline
  off it.

- A transfer whose caller went away is recorded as cancelled and not as
  failed. The file may well be on the machine by then, and a row reading
  `Failed` sends a person to send it a second time.

- A transfer the printer refused says what the printer said. `File already
  exists` is what a person acts on; the row carried a chain of wrappers ending
  in `the device refused the transfer`, and the machine's own words reached
  neither the row nor the log.

- Pointing at a file in the Storage listing highlights that file. The stretched
  link on a file's name had no row to measure itself against, so its hit area
  covered the whole table and the last row lit up instead.

- A detail page's heading clips a long name in the middle and keeps its end,
  the version suffix and the extension a reader tells two files apart by. A
  slicer's file name is one unbroken word, which nothing wrapped, so it pushed
  the state badge onto a line of its own and stretched the header.

- The shapes under `Default view per listing` stand in a row at the size of
  their own drawings. Each one filled the width of its column and they stacked
  one under another, which made the fold several screens tall.

- A printer holding no print draws no progress bar. The bar had no reading to
  draw, so it rendered as a full pulsing track beside the words `No job
  running`.

- The Now tab says a printer has never printed, on the card its prints would
  fill. The block was simply absent, leaving the space beside the upload box
  blank.

- A folder made on a printer's Files tab appears as soon as the printer
  confirms it, and a deleted file disappears the same way. The listing only
  caught up on a page reload.

- On an instance holding two printers, the switcher above a printer's page
  draws each machine with its picture and its state beside the name, in two
  halves of equal width. It was a row of bare names, and it reached assistive
  technology as a tab strip controlling a panel that does not exist.

- A screen with nothing to list says so on the same card its rows would have
  filled. The Jobs, Printers, Storage and Material catalogue screens, and a
  printer's own recent prints, wrote that sentence onto the page background
  instead, so an empty screen read as a screen that had failed to draw.

- The Settings entry in the navigation opens its rooms and lands on Account.
  It folded its rooms away and marked none of them, so a reader arriving at a
  settings room by a link or a reload could not see which one they were in.

- The Account room of Settings is drawn at the width of every other room. It
  stopped two thirds of the way across the page while Appearance, API
  credentials and People filled it.

### Security
- Wrong setup codes from the machine itself are logged once a minute with a
  count. Each wrong code wrote a line of its own, and a flood could push the
  code out of the log. The first request from a proxy on the machine that
  `core.trusted_proxies` does not list logs a warning. One IPv6 /64 takes at
  most 16 of the 1024 addresses the setup code counts.

- A new install asks for a one-time setup code before anybody can record its
  first run. The server prints the code in its log at every start while the
  first run is open, and `core.auth.setup_code` sets one for a scripted install.
  No setting turns it off. `POST /api/v1/setup` without the code in its `code`
  field answers 403 `core.auth/SETUP_CODE_REFUSED`, where it recorded the
  decision before, and `POST /api/v1/auth/setup` asks for the same code while
  the server holds no account. With `core.auth.local` and no account, a server
  stays in its first run and refuses mode `open`, where it counted as set up
  before. After five wrong codes from one address in fifteen minutes, that
  address gets 429 `core.auth/SETUP_CODE_LIMITED`; the machine itself is never
  refused. The web app's first-run screen asks for the code.

- A module's browser files under `/api/v1/modules/{namespace}/ui/` are served
  with `Cache-Control: private, no-cache`. A browser asks the server before it
  uses a kept file, so the server checks the person's access to the module.

- `GET /api/v1/storages/{storage}/thumbnail` answers `Cache-Control: private,
  no-cache`, so a browser asks the server before it shows a kept picture. A
  person sharing a browser with someone else no longer sees their pictures
  from the cache without the server checking their permission.

- After a person signs out or another person signs in, the app forgets the
  modules it admitted for the first person. It also removes what those modules
  cached in the browser, aborts `host.signal` and every request of
  `host.client`, and opens the home page.

- After one person signs out and another signs in, the same browser tab no
  longer shows the first person's printers, print history, credentials or
  files. The app empties every list that person could read, and drops answers
  to requests made for them.

- A page left open while another tab signs in as someone else no longer
  receives that person's events after its connection drops. The TypeScript
  client's `events` takes a `principal` option and checks it before each
  reconnection.

- The TypeScript client takes a `signal` option, and `withSignal` answers a
  client whose every request ends when the signal aborts. A request made after
  that fails before it is sent.

- A password change also ends the tokens and pairings written down a chain
  that started with the person. A stolen session could pair an outside account
  to a colleague and use that token to write more.

- A Prusa Account token paired to a service account no longer counts as a
  sign-in. With a stolen session it could write more pairings and mint tokens
  that a password change could not find.

- A password change also ends every pairing the person wrote, whoever it
  names. With a stolen session, a pairing written for a service account let
  an outside account keep acting after the change.

- A person who signs in with a Prusa Account can administer this server again.
  Only acts that create or grant access need a sign-in. Listing accounts,
  pairings and tokens, and revoking a token or a pairing, work with an API
  token that carries the grant.

- A pairing saved by a session that a password change ended is refused. Such
  a pairing could let an outside account keep acting as the person.

- Administering accounts, pairings and other people's API tokens takes a
  signed-in session. A stolen administrator's token could create an account
  with a known password, reset one, or pair an outside account.

- A password change also ends the API tokens that person created for a service
  account, and every pairing that lets an outside account act as them. The
  shell reset prints both counts and asks the operator to review accounts and
  grants.

- Ending credentials on a password change no longer depends on the server's
  clock. A board whose clock steps back at its first time sync keeps its new
  sessions, and a sign-in with the new password during the change is kept.

- A sign-in or an API token mint that ran while a password was being changed
  no longer leaves a working credential behind. The change raises a counter
  on the account, and a credential from before it is refused.

- An API token older than 365 days is refused, whatever expiry its stored row
  names. This covers a token minted before the upper limit existed.

- Minting an API token at `POST /api/v1/auth/credentials` takes a signed-in
  session, and the expiry is at most 365 days away. A stolen token could mint a
  copy of itself that kept working after the original was revoked.

- A password change, an administrator's reset and `admin reset-password` end
  every session and API token the account holds. A token minted with a leaked
  password stayed valid after the change, and the shell reset ended nothing.

- A storage a module cannot open and one this server does not declare are one
  sentence to that module. This server's own file paths no longer cross to a
  module, and the reason reaches the server log at most once a minute for each
  module.

- A second file transfer to a printer that is already fetching one is refused
  with `core.printer/TRANSFER_BUSY`, and this server relays at most sixteen
  prints that nobody is waiting for. A print asked for over the module wire is
  answered while the file is still moving, so a module could ask again at once.

- A module reading a file or printing one is told the same sentence for every
  file it cannot reach, and the reason is in the server's log. The sentence
  said whether the storage is one this server declares, so a module could
  collect a deployment's storage names.

- A server that asks to join under a name another server is already waiting
  under is refused. In the ask-me-first mode the operator sees the public key
  that asked first, and his approval admits only that key. A key that asks
  later under the same name never replaces the record, so an approval cannot
  admit a machine the operator never saw. The limit on waiting servers counts
  only the ones no operator has approved.

- A setting a resource type declares as a credential is no longer returned by
  `GET /api/v1/resources` or `GET /api/v1/resources/{kind}/{name}`. Both routes
  returned the whole config block of an instance. So anybody who could view a
  resource saw its passwords, client secrets, tokens and connection strings.
  Only the credentials of a module were hidden. A type compiled into this
  binary had none hidden. Every such value is now `xxxxx` in the answer, at the
  top of the block, inside a nested block, and inside a block in a list. A
  password written into an address the type declares as a URL is replaced
  inside that address. No caller sees a credential, an administrator included.
  A write keeps what the caller could not see. `xxxxx` under a key the type
  declares as a credential means keep the stored value. So a client may read an
  instance, change one key and write the whole block back. Write the credential
  itself to change it. `xxxxx` sent for a credential the instance has none
  stored for is refused.

  An instance whose `type` this build has no implementation for, which is what
  a misspelt type name produces, has every value of its block replaced and its
  keys kept. Nothing can say which key of such a block is a credential, and the
  block is still returned to whoever may view the instance.

  The `header` block of a `webhook` sink is now declared a credential, whole. It
  is what carries an authorization token, and a block whose keys the operator
  chooses cannot be masked key by key. A read of that instance answers the
  marker in place of the block, so the header names are hidden too. To change a
  header, write the whole block again.

- An instance is refused at start when another kind is already running under
  its name. The permissions graph knows every module instance as
  `sink:<name>`, whatever its kind. So `sink: {audit: ...}` and
  `link: {audit: ...}` were the same principal, and a grant written for the
  sink also applied to the link. Rename one of the two instances. The log line
  and the status of the refused instance name both. A grant belongs to the
  name and stays after the instance is removed. So a new instance that takes
  the name of a removed sink also gets the grants of that sink.

- A subscription a module opens for a server that joined this one counts
  against the stream limit of the module as well as against the limit of that
  server. The event hub limits how many streams one principal may have open.
  A link module that opened each stream in the name of a different server
  never reached that limit. One link watches at most
  `core.events.max_streams_per_principal` servers at once, 64 by default.
  Raise the setting on a site where more servers than that join one link. A
  subscription a module opens for a person still counts against that person
  alone.

- A link module relays calls only for the servers it has told this server
  about. A link module connects this server to other servers, and it lists
  them in the link reading it sends to this server. A link that has just
  started relays no call until its first reading arrives. A link cannot use
  the grants of a server that another link connects. A reading that lists
  such a server is refused, and the link that sent it fails.

### Removed
- The interface no longer shows printer tags: the Tags page, the tags on a
  printer's Info tab and the tag filter on the Printers page are hidden. Tags
  set through the API stay on the printers.

- The component library's `MaterialSwatch` draws a colour and no photo. Its
  `preview` prop, the `photos` field and the `SwatchPhoto` type are gone.

- The link type `core/fractal` is gone from the server. A server joins a Fractal
  network through the Fractal module instead, which is installed with
  `prusa-connect module add` and declared under `resources.link`. A
  configuration that names `core/fractal` is refused at startup, and its keys
  `mode`, `parent`, `parent_key`, `listen`, `enrolment`, `key_file`,
  `heartbeat` and `window` are read by nothing. The links API, the links page
  and the `link.node.*` events are unchanged.

- The `make` targets `up`, `down`, `restart`, `release`, `build-aarch64`,
  `build-minimal-aarch64`, `build-minimal-compressed`,
  `build-minimal-compressed-docker`, `coverage-unit` and `storybook-build` are
  gone. Type `make server-up`, `make server-down`, `make server-restart`, `make
  release-patch`, `make build-cross ARCH=aarch64`, `make coverage` or `make
  client-ui-check` instead.

- A verb grant no longer stores a condition nothing evaluated, and `operator`
  is no longer a value of the marker that says who marked a verb dangerous.
  No code wrote the condition or that marker value, and no route could set
  them. `GET /api/v1/vocabularies/authorization` reports `host` or `adapter`
  for every marked verb. A migration drops three columns, so an earlier build
  refuses a database this build has migrated. Take a backup before the
  upgrade. The verbs a printer accepts and the people who may send them are
  unchanged.

- [sdk] `sdk/fractal` no longer exports `Dial`, `Listen`, `Link`, `FileKeyStore`
  or the join and welcome frames. That handshake was not the Prusa Fractal
  protocol and it has no replacement in the SDK. The package now holds the
  protocol the Fractal team specifies, listed under Added.

- The component library no longer publishes `./shell`, `./device`, `./forms`
  or `./screens`, and `./primitives` holds six components. Every component the
  React design system defines is written again from that specification, so the
  ones that did not match it go instead of being edited in place. An
  application or a module that imported one of those four subpaths has nothing
  to import until the new components land under `./primitives`. `applySkin`,
  the theme store and the `NavItem` type are on the package root now,
  `@prusa/connect-ui`.

- The component library no longer publishes `./skins/connect-green.css`, and
  the six candidate skin files are gone with it. A skin used to be one
  stylesheet imported on the line after the theme; it is now an attribute on
  the root element, and `tokens.css` holds all three skins itself. Call
  `applySkin('prusa' | 'slicer' | 'prusapro')` from `@prusa/connect-ui`
  in place of importing a file, and set `core.frontend.skin` to one of those
  three names.

### Changed
- `GET /api/v1/devices` orders printers by urgency unless `sort` names another
  field: errors first, then printers waiting for a person, then silent ones.
  It accepts `severity` to list the printers carrying an issue of that
  severity, and every order breaks ties by device id.

- The settings pages no longer draw a row of tabs under the title. The
  sidebar already lists every section and is now the only way between them.

- [sdk] `moduleBuild()` scopes a module's CSS to the module's own markup,
  whatever the module's `styles.css` says. A stylesheet that wraps
  `@tailwind utilities` in `@scope ([data-module])` drops that block, or the
  module's utilities stop applying.

- A print's page no longer has an Open printer button. The printer beside the
  print's name is larger and is the link to that printer's page.

- The printer listener reads its TLS certificate again when the certificate or
  the key changes, so replacing the pair a connector's `listener.tls` block
  names no longer needs a restart. It looks at most once a second. A
  replacement the firmware could not verify is logged, and the listener keeps
  the certificate it already serves. The API listener, `core.api.tls`, still
  reads its certificate once at startup.

- [sdk] A module's Status-tab section used to be declared under
  `ui.contributes.device_card` and registered with `host.register.deviceCard`.
  `device_card` and `deviceCard` now mean one line on each printer's card in
  the printer listing, drawn under its status and cut to one line. A module
  that still registers its Status-tab section this way now finds it in the
  listing instead. Move the entries to `ui.contributes.device_section` and
  call `host.register.deviceSection` to keep the Status-tab section.

- [sdk] A module.yaml that names a page icon outside the published list is
  refused at install, with the list in the message.

- The shipped `config/config.yaml` declares the GraphQL and the old Prusa
  Connect compatibility modules switched off, where a fresh install warned
  `RESOURCE_PLUGIN_MISSING` at every start. No release carries them yet; install
  one with `prusa-connect module add` and set `enabled: true` on its row.

- A first visit to the web app downloads about a quarter of the bytes it did.
  The server sends the app's scripts, styles and pages gzip-compressed to every
  browser, and the full binary is about 2 MB smaller.

- A client that does not accept gzip, such as curl, may get 503 with the code
  `core.internal/APP_FILE_BUSY` for a file of the web app. The server
  decompresses files for two such clients at a time, and a third waits up to
  two seconds for a free place.

- The material list now lives in the filament module, which draws it as a
  Materials page beside its Filament page. The web interface shows no Materials
  page of its own.

- Every page of the app loads 110 kB less JavaScript after gzip, so the first
  visit to a printer is faster.
- The History chart on the printer page writes its times in the language you
  chose, and a screen reader reads out the latest, lowest and highest value of
  each line. The value labels on the left of the chart are no longer cut off.
- A printer carries tags, plain words such as `workshop` or `night shift`,
  instead of key and value labels. Upgrading converts every stored label into
  a tag, so `site=brno` becomes `site: brno` and a label with no value becomes
  its key. `PUT /api/v1/devices/{device_id}/tags` replaces a printer's tags,
  and `GET /api/v1/devices?tag=workshop` lists the printers carrying that tag,
  ignoring case. A new tag is at most 64 characters, carries no comma, and a
  printer holds at most 32. Each broken rule has its own code, such as
  `core.validation/TAG_DUPLICATE`. The `labels` member, `PUT
  /api/v1/devices/{device_id}/labels` and the `labels` listing parameter still
  work, read and write tags, and are removed in a later release.

- [sdk] `deviceapi.Device` and `deviceapi.ListParams` carry `Tags`, and
  `Labels` on both is removed in a later release. The fault param keys `tag`,
  `limit_characters` and `limit_tags` are published for the tag error codes.

- The deprecated `labels` listing parameter refuses a term that is not a valid
  label key or value with `core.validation/REQUEST_INVALID`. A mistyped term
  does not answer with an empty fleet.

- A tag converted from a label keeps its full length, which can be up to 382
  characters. A tag write that sends it back unchanged is accepted, and the
  `tag` filter finds it.

- A tag that contains a character that shows nothing is refused with
  `core.validation/TAG_CHARACTER`. These are the characters Unicode lists as
  variation selectors or as ignorable, and the blank braille pattern.
  A tag with no visible character is refused with `core.validation/TAG_EMPTY`.

- A tag may hold an emoji or a word that needs a joiner, such as Persian. A
  joiner stands between two visible characters, and an emoji selector right
  after one. A tag with and without one is the same tag, both in a write and
  in the `tag` filter. A region flag, such as the flag of England, is
  accepted too.

- Reading tags through the deprecated labels view keeps one tag per key. A
  client that reads labels and writes them back removes the other tags, so it
  should move to `tags`.

- [sdk] The `com.prusa.connect.device.record.changed` event carries a `tags`
  list in place of the `labels` object, and names `tags` among its changed
  fields. The event-type set and `event.VocabularyVersion` move to version
  3.0.0.

- The server needs much less memory while it starts. It now renders
  `/openapi.json` and `/openapi.yaml` when a client first asks for one of them.

- The web interface no longer shows the Materials page or the setting that
  belongs to it. The material chip on a printer's page offers the filament
  types to load, and an unload.

- The print sheet on the Storage page decides which printers can take a file
  from the states this server declared. It reads them from
  `GET /api/v1/vocabularies/device-states`, and a kind that names no move for
  the start verb keeps the old rule of a free bed.

- [sdk] A module that asks `storage.read` for a directory is told the file is
  not there. `storageapi.ErrNoFile` is the sentinel, in place of
  `ErrUnreadable`, which a module waits on and asks for again.

- The role picked for a new person starts at operator. Most people added to a
  server run prints, and a reader who wants a weaker role changes it in the
  same panel.

- The button that folds the Settings rooms under a rail entry reads its label
  from the catalogue, so a reader sees it in their own language.

- The config block of an instance whose type is compiled into this binary is
  checked against the settings that type declares, before the instance is
  built. A key the type does not declare refuses the instance with
  `RESOURCE_PLUGIN_SETTINGS`. So does a required key nobody wrote. So does a
  value written in a shape the declared type cannot hold. Only that instance is
  refused. The server and every other instance keep running. Whether a typo was
  caught used to depend on the decoder the author of the type had written. So a
  mistyped key could leave a setting unwritten with no sign of it. An operator
  whose instance is refused reads the key named in the refusal and in the
  status of the instance, then corrects it or removes it. The settings each
  type accepts are the `Configuration` list on the extensions page.
  `GET /api/v1/vocabularies/extensions` serves the same list per entry, with a
  flag saying which keys are credentials. A `local` or `temporary` storage
  instance declares `path`, `name` and `allowed_extensions`, which it always
  read and never published. A camera's `printer`, `snapshot_every` and
  `snapshot_storage` stay legal in any camera block. The server reads those
  three and no camera driver declares them.

- [sdk] `connectorapi.Session.State` states what a driver must do with state
  patches the host is not reading, and `connectorapi.CountsLink` is the
  interface the host reads those counts through. A driver never waits for the
  host. It keeps a limited number of unread patches, drops the oldest patch
  when it reaches that limit, and counts the patches it dropped. `CountsLink`
  is optional. It replaces an interface the host checked for and the SDK never
  published, so a driver outside this repository can now report those totals.

- A refusal to send raw gcode names the machine as `device:<id>` in its
  `object` parameter, matching every other authorization refusal. It named it
  `printer:<id>`. The authorization graph has no object type `printer`, so an
  operator could not find the grant that the refusal asked for.

- The per-machine label on `/api/v1/metrics` is `device_id` and no longer
  `printer_id`. Every series of the fleet metrics has this label. A dashboard
  panel or an alert rule that matches on `printer_id` matches nothing after
  the upgrade, so edit it. The metric names are unchanged. The series that the
  retired server published keep the label `printer_id`.

- [sdk] A script sink reads `event.device_id` in place of `event.printer_id`.
  The variable `event.printer_id` does not exist any more. An expression under
  `resources.sink.<name>.script` that uses it produces an empty string, so
  edit every such expression before the upgrade. In the SDK,
  `scriptapi.Event.PrinterID` becomes `scriptapi.Event.DeviceID`.

- [sdk] The five printer-named event types are now device-named.
  `com.prusa.connect.printer.arrived`, `.departed`, `.presence.changed`,
  `.link.changed` and `.telemetry.sampled` become
  `com.prusa.connect.device.arrived`, `.departed`, `.presence.changed`,
  `.link.changed` and `.telemetry.sampled`. The old names are removed without
  a deprecation period, and the server publishes none of them. A subscriber
  that selects events by type, in its own code or with `?types=` on
  `/api/v1/events` or `/api/v1/history/facts`, changes the name. The payload
  of the four device events names the machine `device_id` and no longer
  `printer`. The published `event-types` vocabulary moves to 2.0.0. In the
  SDK, the five `event.TypePrinter...` constants become `event.TypeDevice...`,
  for example `event.TypePrinterArrived` becomes `event.TypeDeviceArrived`.

- [sdk] Every device event now names its origin `device:<id>` and no longer
  `printer:<id>`. A camera, a printer and a machine of a class that a module
  adds all use the origin kind `device`. A subscriber that filters with
  `?origins=printer:<id>` on `/api/v1/events` or `/api/v1/history/*` changes it
  to `?origins=device:<id>`. So does code that matches the `subject` member or
  the `source` URN of an event. A migration rewrites the stored history rows
  on both database engines, so a query over a period before the upgrade
  returns the same events. In the SDK, `event.KindPrinter` becomes
  `event.KindDevice`.

- [sdk] The second implementation of the plugin wire protocol is JavaScript for
  Node. It is in `examples/wire-protocol/` and uses no npm package. `make
  wire-check` runs it against the same test vectors as the Go implementation.

- A printer dialect describes itself. The device model vocabulary reads each
  dialect's brands, its setup step and the words for the first run's
  admission choice from the driver. So a composed dialect describes its own
  machines, and the core knows no dialect by name. The published default for
  the xBuddy `chunk_write_ceiling` option is 4096, the value the code used.

- The checks that open the web app measure the page it draws now, and the
  example modules and the SDK consumer draw the library's primitives. The
  layout-shift and contrast measures stay. The measures over screens that no
  longer exist are skipped until a screen returns.

- The component library draws the colours, the radii and the type and spacing
  rhythm of the Prusa design system, value for value. The theme also carries
  the three skins the design system ships: prusa, slicer and prusapro.

- MQTT, the broker inside the process and the metrics client are switched
  under `core:`, as `core.mqtt.enabled`, `core.mqtt.broker.enabled` and
  `core.observability.enabled`, where the server and `make build` both read
  them. A file naming the old top-level `mqtt`, `observability` or `fractal`
  block is refused at start with a message naming the key that replaced it.

- The MINI404 emulator launcher moved from `internal/printer/mini404` to `tools/mini404`, beside the other simulators.

- The device listener moved under the connector that serves it: write
  `core.connectors.<name>.config.listener` with `addr`, `tls` and
  `handshake_timeout`. A file naming `core.printer` is refused at start with
  a message saying where it went.
- `core.compat` is deleted. A compatibility layer is switched by its own
  resource row, `resources.sink.<name>.enabled`, and a file naming the old
  key is refused at start.

- The example profile `config/examples/config.memory.yaml` is renamed `config.core-only.yaml`, after what it is. The usage text no longer names a `serve` command, which no version of this binary accepts.

- The unit suite runs on CI as seven parallel shards, about five minutes instead of twenty, with the race detector on a nightly job and on main.

- Every English string the app shows follows one voice: sentence case, one
  word per thing across screens, a verb on every button, and numbers with
  their units.

- A printer's state and its connection show as a coloured mark and a word on
  one quiet chip. The printer stands on a shelf of one size on every fleet
  card, and only a printer that is printing keeps the orange button.

- Every primitive draws from the theme's named roles, and an empty page shows
  a drawing instead of a grey box. A card is told from the page by its tone,
  or by one edge where the ground is the same colour.

- The API baseline records the ui operation, the wider resource type bound
  and the module install answer's `running` and `restart` members.

- The extensions page is two tabs, Installed and Available. An installed
  extension shows its row at once, switches on and off, opens its own page,
  and can be uninstalled with a confirmation.

- The theme names what it draws: five text sizes, three weights, nine spacing
  steps, one radius per role and a five-tone surface ladder. A gate counts
  what each screen is made of and refuses a new radius, size, grey or primary
  button.

- The sidebar groups its pages under headings you can shut, gives each one
  a glyph and a search field. Ctrl+K opens a palette that finds a page, a
  printer by name, model or serial, or an action.

- The device-models vocabulary carries the connectors this build knows and
  the connector of each model, and the API baseline records today's
  additions.

- Nothing on a page moves when something loads or appears. The shell,
  Settings, Extensions and the fleet reserve their slots. A browser test lane
  holds every screen to a layout shift under 0.02, every press to its own
  region and every text pair to 4.5:1.

- Text takes its colour from the surface it sits on, and the primary button
  is the light orange with dark text, readable in every state and both
  themes. Six alternative primary colours ship as skin files.

- A dialog and a sheet appear on the frame after the click. The scrim is a
  plain wash instead of a blur, and a step changes with no remount. A dialog
  that needs data opens at once and fills in behind a skeleton. A closed menu
  leaves no node in the page.

- The Tools page picks a printer in a panel that shows each machine's
  picture, state and camera, with a search field and arrow keys, instead of
  a drop-down of names.

- A camera picture is drawn as a feed with its own strip, in a fixed box that
  never moves. The strip says whether it is live or a still frame, how often
  it refreshes and how old the frame is. The refresh follows the rate the
  camera declares, capped per page, and `GET /api/v1/cameras` rows carry
  `min_interval_ms` from the driver.

- Every drop-down is a combo box you can type into, with icons where the
  options have them. The language control in the sidebar lists each language
  in its own words.

- The component library's tiers decide which subpath a name is imported from,
  and several names moved. `@prusa/connect-ui/domain` now carries the
  injected-action contract (`CommandAction`, `CommandActions`, `PrintAction`,
  `PrintRequest`, `PrintOutcome`, `getCommandActions`, `setCommandActions`),
  the telemetry state paths and their two readers, `ReadonlyFleet`, and the
  principal (`ModulePrincipal`, `PrincipalClient`, `PrincipalGrant`,
  `readPrincipal`). They came from `./screens`, `./device` and `./data`.
  `@prusa/connect-ui/slots` now carries the module key-value store
  (`ModuleStore`, `createModuleStore`, `ScopedStore`, `ModuleStorage`,
  `ModuleStoreOptions`, `storeKeyPrefix`), which came from `./data`.
  `defaultCommandActions` and `startPrintAction` stay on `./screens`, and
  `createFleet` stays on `./data`. The root import keeps every name.

- `DeviceCard` no longer draws a module's `device-card` sections by itself. It
  takes them through a `sections` snippet, which the screen that mounts the
  card fills with the slot outlet. A page that renders the card outside this
  library's screens passes its own snippet to keep those sections.

- The module listing store no longer builds the sidebar's entries.
  `Modules.navItems` is gone; `SlotRegistry.sidebarItems()` from
  `@prusa/connect-ui/slots` builds the same list, and `withModuleNav` on
  the same subpath puts a module's entries after the application's own.

- A module's bundle no longer carries its own copy of the translation
  machinery, which takes the `nozzle-watch` example from 71 kB to 5 kB. The
  page's import map answers `@prusa/connect-ui/i18n` as well as the
  runtime, the library and the client SDK, so a module reads the application's
  translations instead of shipping a second copy.

- A module may name any Tailwind utility, not only the ones the application
  already uses. `@prusa/connect-ui/build` compiles the module's own
  stylesheet and puts the result inside the single file the manifest names, so
  the page loads no second stylesheet.

- One name for one thing on the wire, which ADR-0022 orders. A file entry's
  `modified` is an RFC 3339 string instead of an integer of Unix seconds. It
  is absent for a board that reported no time, rather than reading as zero.
  A transfer's `started_ms` and `ended_ms` are `started_at` and `ended_at`,
  RFC 3339 strings. A job's `time_remaining`, whole seconds, is
  `time_remaining_ms`. The developer command log's `elapsed`, a duration
  string, is `elapsed_ms`. The event stream's `?interval=` query parameter is
  `?interval_ms=`, with the same value and the same bounds.

- A refusal names things by one key each. A refusal about a resource instance
  carries one `resource` parameter, spelled `kind:name`, instead of `kind` and
  `name`. `core.validation/FRAME_NOT_AN_IMAGE` names the camera under
  `resource`. `core.validation/TOO_MANY_STREAMS` renames `limit` to
  `limit_streams`, `core.validation/ARGUMENT_OUT_OF_RANGE` renames it to `max`,
  and `core.printer/TRANSFER_TOO_LARGE` to `limit_bytes`. Every `core.printer`
  refusal names the machine `device_id`, never `printer_id`.

- The command that sets the heated bed is `core.command.set_bed_temperature`;
  its argument `bed_temperature` is unchanged. An event sink forwarding a
  printer state, a label change or a job writes lowercase member names such as
  `id`, `state` and `external_id`, and names the machine `device_id`. The
  Svelte icon for the heated bed is `BedIcon`.

- Rules, decision records, pages and research notes state each rule without
  saying who decided it or when. That record lives only in the owner decisions
  log, and check 17 of the writing-style gate refuses a new attribution.

- A module is installed with one command, and a module with a browser half is
  one line in the listing. `prusa-connect module add
  github.com/acme/spools@1.0.0 -from ./release` reads the release's
  `module.yaml` and verifies its `checksums.txt`. It copies the files into the
  directory `core.modules` names, and writes one declaration into the
  machine-owned overlay. That declaration carries both halves: the process
  under `service`, and the browser bundle beside it under `service.assets`.
  The bundle is served under the same namespace at
  `/api/v1/modules/<namespace>/ui/`, and one `enabled` flag covers both.
  `module list` says what is installed and `module remove` takes a module and
  its files away. `make new-module NAME=<name> REPO=<repository address>`
  writes a new module from a skeleton: UI only, process only, or both,
  chosen with `-kind`. Beside a checkout it writes to `../<name>` and links
  the checkout's packages.
  Anywhere else it writes to `./<name>` and asks for the published versions.
  It never writes into a folder that holds anything. It numbers what it
  prints from 1, prints paths from where it was run, offers to run
  `install.sh`, and ends with the next commands to copy. A core that is
  already running reads the declaration
  at its next start, and the command says so. The version after the `@` is a
  version range in npm's grammar, and so are the `core` and `ui.library` keys
  of a manifest. `^1.2.0`, `~1.2.3`, `1.2.x`, `1.2.3 - 2.0.0` and `^1 || ^2`
  are all accepted.

- The whole screens move into the component library: the fleet, a printer's
  page, the settings, the sign-in and the not-found page. A second application
  hosts them by setting one context and injecting what its buttons do, and
  keeps only its router, its session and its wiring.

### Added
- The web app is drawn against its design. Settings are one page of four rooms
  opening on Account, a print starts again from the list and from its own page,
  the printer’s page gains a drop zone and preheat presets by material,
  Appearance sets the clock, the temperature scale and how many rows a listing
  opens with, and the app opens on the page Appearance names.

- Six screens are drawn against the design they were specified from. The
  printer's info, commands and files tabs, the server's own storage, a file's
  page, a print's page and the materials catalogue were written from their
  port and their record rather than from the design, so each one had a shape
  of its own. They now carry the design's layout, its wording and its states.
  The left navigation shares the top bar's surface, as the design draws it.
  A print's page and a file's page hold their parts in one card instead of
  behind a tab strip. The design does that for a subject with three parts.
- A person can now watch every printer's camera on the printers page. The
  Cameras view drew a picture of each machine and asked the server for no
  frame. It now loads the latest still image of every printer that has a
  camera. A printer with no camera keeps its picture. A printer's
  Status tab draws its frame too, which it did not because the page never read
  the camera listing. The frames stop being asked for while the browser tab is
  hidden.

- A person can now see which material each printer reported and which labels
  the fleet carries, on the Materials and Labels pages in the application.
  Neither has a catalogue on this server. A material is a reading a printer
  sent, and a label is a key and a value on a device record. Both pages read
  the device listing. Renaming or removing a label is not offered yet.

- A person can now change their own password, create and revoke an API
  credential, and add or remove a colleague, from the Settings rooms in the
  application. Adding a person says under the role choice that a role is held
  over the whole server, so nobody grants access to every printer believing
  they granted access to one.

- A person can now see what every printer has printed, on the Jobs page in the
  application. This server serves a print history per printer and none across
  the fleet. The page reads one history per printer, for the first twenty-five
  of them. It says on the page when the fleet is longer than that.

- A person can now upload a file to this server, browse its storages and
  delete a file, from the Storage page in the application. The page lists every
  storage the server declares, walks its folders, and takes a file by drag or
  by a file picker. A file longer than the upload route accepts is refused
  before it is sent, so nobody waits out a four gigabyte upload to read the
  refusal.

- Signing in now opens the printers page. The root address of the application
  answered with the not-found screen, so the first page after a sign-in was
  blank until the person found the Printers entry in the rail.

- A filament module keeps a list of spools for this server and shows how much
  is left on each one. It serves `/spools` and `/materials` under the namespace
  an operator installs it at, and draws a Filament page in the application. It
  writes every spool into the server's record store, so a restart leaves the
  list as it was.

- A link now reports how many messages it has handled and how many it has
  thrown away, and `GET /api/v1/links` shows both. The Fractal module counted
  them and nothing read the counts.

- A webhook, a Kafka topic and the history store now receive one event per
  decision about a server under this site. Only the whole reading of a link was
  published before, so nobody had a record of who approved which server and
  when.

- [sdk] A module asks whether one principal may perform one action on one
  object with access.check. A print queue uses it to ask whether the person who
  submitted a job may still operate the printer, before it starts that job at
  night. The question is bounded on two sides. The action is one the instance's
  `permissions:` list names. The subject is one the server handed the instance
  on `routes.serve` since it started. An object this server does not have
  answers as one the subject reaches nothing on. A module names `access.check`
  in its `requires:` list.

- [sdk] A module reads a page of finished prints with device.job_history, and
  the HTTP job history route takes from and to. The wire call takes the device,
  a range of time, a page size and a cursor. A call naming no device answers a
  page over every device the caller may observe. The permission is
  resolved inside the query, so the page carries nothing of a device the caller
  reaches nothing on. `from` and `to` are two RFC 3339 instants bounding when
  the print ended, either one alone bounds that end only, and a `from` after a
  `to` is refused. A module names `device.job_history` in its `requires:` list.

- [sdk] A module reads a file in a server storage with storage.stat and
  storage.read. `stat` answers the file's length, when it last changed and the
  digest the storage kept. `read`
  answers one range of at most 512 KiB, so a module walks a large file with the
  offset and neither side holds it whole. Both take `read_storage` on the
  storage kind, which is what the HTTP storage routes take. A storage this
  server does not declare, a file it does not hold and a path that leaves the
  storage all answer one refusal. A module names `storage.stat` and
  `storage.read` in its `requires:` list, and `read_storage` in the
  `permissions:` list of its manifest when it reads files for its own work.

- [sdk] A module starts a print with `device.print` on the module wire. It takes
  the storage holding the file, the file's path inside it, and where the file
  lands on the printer, which is what `POST /api/v1/devices/{device_id}/print`
  takes. It needs `operate` on the printer and `read_storage` on the storage
  kind, so an instance installed without `read_storage` prints only what a
  machine already holds. The call answers once the transfer has started, and the
  server goes on relaying the file and sends the start command itself. A module
  names `device.print` in its `requires:` list.

- Two events say when a print begins and when it ends:
  `com.prusa.connect.device.job.started` and
  `com.prusa.connect.device.job.settled`, on the printer's own origin, once per
  print. The settled event carries the outcome as one of finished, failed,
  cancelled and unknown. Beside it are the machine's own last word, the last
  progress, how long the machine says it printed and how much filament it says
  the print used. Neither event names a person. The started event carries the
  `command_id` of the instruction that began the print, absent for a print
  started at the machine's own screen. Who authorized that instruction is in
  `com.prusa.connect.device.command.authorized`. A machine that loses its
  connection mid-print settles nothing until it comes back.

- [sdk] `sdk/event` carries `JobStarted` and `JobSettled` and the closed set of
  outcomes a module and an event sink decode them with. `sdk/connectorapi`
  states where a driver reports a print, and two paths are new: `/job/outcome`,
  where a driver maps its machine's own terminal word onto that set, and
  `/job/filament_used_mm`. Both are optional, and an absent
  `/job/filament_used_mm` means unknown and never zero. A driver that reports a
  print must report `/job/id`, deriving one where its machines count no jobs;
  the conformance suite checks it. The Moonraker example derives one from the
  file name and the minute the print began.

- An installed module is now on the screen. A page it declares is an entry in
  the left rail under Extensions, at `/m/<namespace>/<path>`. A device-card
  section is at the foot of a printer's Status tab. A settings panel is on
  Settings, Extensions, which also lists every installed module with a browser
  half. A module the app would not run is named there with its reason and its
  code, so a refusal is read on the screen and not in the browser console. A
  module that fails to load, or throws while it draws, costs its own page and
  never the rest of the app.

- [sdk] A driver can tell the server which commands its printer supports.
  A driver does this through the optional interface
  `connectorapi.ReportsProbe`. `GET /api/v1/devices/{device_id}` then lists
  only those commands. The server refuses any other command with
  `core.printer/CAPABILITY_NOT_SUPPORTED`, and the driver never receives it.
  The Moonraker example uses it: a Klipper printer with no heated bed no
  longer offers the bed commands.

- [sdk] A module reads its settings from the configuration file. `module.yaml`
  has an `options:` list. Each entry declares one key with its type, a
  sentence that describes it and its default. The server starts from the
  defaults and applies the `config:` block of the instance over them. A nested
  block is merged key by key, and a list is replaced whole. The server checks
  the result against the declared keys and sends it to the module process with
  the new `plugin.configure` call. The server does not start an instance whose
  block has a key the module does not declare, or lacks a required key. The
  error message names the key. A module author reads the settings with
  `sdk/wirerpc.Configure`.

- An instance whose `config` block a module will not read is refused with
  `RESOURCE_PLUGIN_SETTINGS`, and one carrying a block for a module that reads
  no settings with `RESOURCE_PLUGIN_UNCONFIGURABLE`. The server does not retry
  either start. The instance starts after the configuration file is corrected.

- A module's declared settings are on `GET /api/v1/resources`, each with its
  type, its default and its description. `prusa-connect module add`
  prints them at install. The API response shows `xxxxx` in place of the value
  of a setting the module declared as a secret, and never shows the default of
  a secret.

- A server above this one reaches printers here as `node:<id>`, and has no
  permission on any printer until an administrator grants one. A module
  installed as a `link` may send a command in the name of that principal. The
  authorization graph checks the command the way it checks a command from a
  person. Write the grant with `POST /api/v1/grants/device/{id}` and the
  subject `node:<id>`. The id is the one the links page shows for that server.
  A module of any other kind cannot act as a node.

- The permissions list on a link module now bounds what that link relays for
  the server above it, and not only what it does for itself. A link installed
  with `observe` alone sends printer state to the server above and sends no
  command to a printer, whatever the graph grants that server.

- A module's `requires` list is now enforced at install and at every start.
  An entry names a wire capability, a range of server versions, or another
  resource type. The install records the list in the declaration, and the
  server checks each entry before it starts the module. An instance whose
  requirement is not met does not start.
  `GET /api/v1/resources/{kind}/{name}` reports it with the condition
  `DependenciesSatisfied` and one of `RESOURCE_DEPENDENCY_MISSING`,
  `RESOURCE_DEPENDENCY_UNSERVED` and `RESOURCE_DEPENDENCY_CYCLE`. A module
  that waits for another resource type starts when an instance of that type is
  ready. `module add` prints the `requires:` list and refuses a module that
  this build cannot run.
  `GET /api/v1/resources` now carries a module's `requires` list beside its
  release.

- [sdk] `extension.Requirement` carries `Type` and `Core` beside `Capability`.
  A requirement sets exactly one of the three fields, and `Declare` refuses a
  requirement that sets none or more than one. `fault.ParamKeys` gains
  `requires` and `cycle`.

- The Fractal module makes this server a node of a Prusa Fractal network.
  It joins the server above it and accepts servers under it. It shows this
  server's printers to the network as the interfaces of the Prusa interface
  specification. A method the server above invokes becomes one command on one
  printer. This server checks that command against its own permissions.
  Install it with `module add`, enable it, and write its settings record. See
  `packages/fractal/README.md`.

- [sdk] A new `sdk/fractal/prusa` package embeds the Prusa interface files.
  The files define the same interfaces as `interfaces.md`. The tests of
  `sdk/fractal/schema` check them against that specification, and the Fractal
  module serves the same files.

- Every published vocabulary can mark an entry `deprecated` before it goes, and
  the marker names the release it shipped in and the entry to move to. The
  marker is the member `deprecated` in every vocabulary under
  `GET /api/v1/vocabularies/...`, and the generated reference pages print it.
  An entry is removed three minor releases after its marker shipped, and not
  earlier.

- [sdk] A new `sdk/vocabulary` package holds the `Deprecation` marker every
  published set uses, and `Window` says how many minor releases a deprecated
  entry stays published. `fault.Doc.Deprecating` marks an error code, and the
  descriptor of an event type, a capability key, a device state and an
  extension carries the same `Deprecated` field.

- The device-model vocabulary publishes a version and a format and has a
  reference page of its own. `GET /api/v1/vocabularies/device-models` answers
  `1.0.0` in format `1.0`, the page is at `/docs/device-models/`, and the
  vocabulary index points at it instead of at the capability page.

- A new packages/fractal module carries the Prusa Fractal protocol over NATS.
  It is a Go module of its own, so the core server does not import the NATS
  server or a JWT library. Every node runs an embedded NATS server with
  JetStream. A node joins the node above it through a bootstrap account that
  needs no credentials, and then receives a NATS account for that link.

- The links API reads a link of either runtime, and an operator can revoke a node.
  `POST /api/v1/links/{name}/nodes/{id}/revoke` removes a node that a link
  admitted. The caller needs the same grant as for approving a node. A link
  now also reports its path in the tree, the site above it with the state of
  that connection, and the network listeners it opened. When a link does not
  answer, the request fails with `core.internal/LINK_NOT_ANSWERING` and does
  not wait.

- A link may run as a module in a process of its own. The link kind now offers
  `runtime: service` beside `runtime: builtin`, so `prusa-connect module
  add` installs a module whose manifest says `kind: link`.

- [sdk] A new sdk/linkapi package is the contract a link module answers and the host asks.
  The host calls `link.status`, `link.approve` and `link.revoke` on the module.
  The module sends `link.changed` to the host when its state changes, and the
  host publishes that as an event.

- [sdk] A new sdk/fractal package carries the vocabulary of the Prusa Fractal protocol.
  It follows the Fractal team's specification for identifiers, Node IDs, Node
  Paths and shadow selection patterns, and it imports the standard library
  alone.

- [sdk] sdk/fractal decodes and validates the Prusa Fractal messages and error codes.
  The messages are `REP`, `DES`, `RPC`, `RES`, `LOS` and `RBR`, and the codes
  are `CodeDenied`, `CodeExpired` and the rest of the specification's list.

- [sdk] sdk/fractal carries the join exchange messages and the retry rule for a denial.
  `JoinRetry` says whether a denied node may ask again.

- [sdk] A shadow selection pattern in sdk/fractal matches a name carrying the wildcard character.

- [sdk] A new sdk/fractal/shadow package keeps one node's copy of the state of the nodes below it.
  It applies a REP message under the rules of the specification. When a node
  below disconnects, it marks the state of that node and of every node under
  it as stale. It tells the application which properties changed.

- [sdk] A new sdk/fractal/join package runs the join exchange for the server
  above and for a server that joins. The server above runs the checks of the
  specification on a join request, in the order the specification lists them.
  It keeps the list of servers it admitted in a file and can revoke one. Its
  authorization check may answer a denial with a more specific error code, for
  example one that says an operator has not decided yet. The server that joins
  decides what to do with a grant. It stores the bytes the transport sent with
  the grant in `Membership.Binding`, and it asks again before its connection
  credentials expire.

- [sdk] A new sdk/fractal/schema package reads an interface definition from a JSON file.
  It checks a DES against that definition and refuses an unknown, read-only or
  wrongly typed property, and it answers an RPC whose parameters do not match
  with `CodeInvalidParams`.

- [sdk] A new sdk/fractal/node package is the Prusa Fractal engine, with the
  contract a transport implements. One Node routes messages and keeps a copy of
  the state of the nodes below it. A method call addressed to a node that this
  node has no connection to is answered with `CodeNotFound`. A node never sends
  a state report for an interface with a timestamp earlier than the last
  report it sent for that interface. An application calls a method or requests
  a property change on any node below it, and implements the interfaces this
  node reports.

- [sdk] A new sdk/fractal/fractaltest package runs a Fractal network in one process and tests any transport.
  It has an in-memory transport and the conformance suite a transport must
  pass. It also has a table that names, for every MUST of the specification,
  the test that checks it. A runnable three-node example is under
  `examples/fractal-tree`.

- The settings hold a room for the people who can sign in. It adds an account,
  changes what one may do, and removes one, and it refuses to take the last
  administrator away or to change your own role.

- The settings hold a room for API credentials, where one is created, listed
  with the date it expires and the day it was last used, and revoked. A new
  credential is shown once and cannot be fetched again.

- The settings hold a room for your own account, where you change your
  password. This server records no display name and no email address for an
  account, and the room says so.

- The web app browses this server's own files. It walks the folders of every
  storage the server declares, takes uploads where the storage is writable, and
  deletes an entry after asking.

- The web app lists the materials the fleet reports, and opens one to see which
  printers hold it. This server publishes no material catalogue, so the screen
  says so instead of drawing a page of readings nobody answers.

- The web app lists the labels the fleet carries, as the key and value pairs
  they are on a printer, with the printers wearing each one. A pair can be
  renamed or taken off every printer that has it.

- The web app lists every print the server knows about, across the whole fleet.
  It sorts, searches and filters by printer and by how a print ended, and a
  printer the server could not read is marked unread, not treated as empty.

- A file has a page of its own, whether it sits on this server's storage or on
  a printer's own drive. It names the size, when it changed, the digest the
  storage kept, and offers printing, downloading and deleting where the thing
  holding it allows them.

- A print has a page of its own. It names the file, the printer it ran on, the
  state it ended in, when it started and ended and how long it took, and the
  path the printer holds it under.

- A printer's page lives at `/printers/<id>/<tab>`. An open tab survives a
  reload and can be sent to somebody, a bare `/printers/<id>` opens on Status,
  and a tab this build does not know falls back to it.

- The component library publishes `PrinterDetailScreen` on
  `@prusa/connect-ui/shell`. It is the frame of one printer's page: the
  trail back to the fleet, the printer's name and state, the readings beside
  them, a switcher when the server holds exactly two printers, and a strip of
  five tabs. An address this server holds no printer for says so rather than
  waiting for good.

- A printer's page has all five of its tabs: Status, Controls, Files, Info and
  Commands. Status draws the print, its progress, the finish time, the lifecycle controls and the camera's still
  frame. Controls sends heat, motion, speed and flow, and offers only the
  verbs the printer says it accepts. Files browses the printer's own drive and
  prints or deletes from it. Info carries the identity, the labels, the
  connection counters, the accepted commands, the issues and the prints it has
  run. Commands lists every verb including the refused ones with the reason,
  and holds a G-code console with the stored presets.

- `DetailHeader` draws a snippet beside the title, which is where a subject's
  state badge belongs. A long name still wraps and pushes the badge onto the
  next line rather than truncating.

- A printer tab this build draws no panel for says so instead of waiting for
  good. A skeleton that never resolves reads as a page that is broken.

- A printer address this server holds no printer for says so instead of
  waiting. Once the fleet has answered and the id is in no row, the record
  will never arrive.

- The component library publishes `DetailHeader` and `DetailTabs` on
  `@prusa/connect-ui/primitives`. Every screen about one subject, a
  printer, a job, a file or a material, draws the same subject block and the
  same tab strip out of these two instead of writing its own. A tab whose
  panel is not written yet draws whatever the screen hands the strip, so a
  screen ships its frame before its tabs.

- The web app has a page for adding a printer, at `/printers/add`, reached
  from `Add printer` on the printers page. It lists the printers waiting for
  approval, with the code and the fingerprint to check against each printer's
  own display, and admits or turns away each one. It also writes the
  `prusa_printer_settings.ini` that points a printer at this instance, filled
  in with this server's own address.

- The web app asks who you are, and shows you your printers. A browser holding
  no session gets a sign-in page; a server with nothing recorded yet gets a
  first-run page that creates the first administrator and signs you in as
  them. Signed in, the printers page lists the fleet worst state first, as
  cards, as a table or as a wall of camera tiles, and the side rail carries
  your name and the way out. The filters, the fields a row shows, the display
  mode and the page size are yours and are kept in this browser.

- The component library publishes both pages and the printers page as
  `SignInScreen`, `FirstRunScreen` and `PrintersScreen` on
  `@prusa/connect-ui/shell`. Each takes plain values rather than this
  application's stores, so a second application on another core draws the same
  screens from its own.

- The component library publishes the application frame as
  `@prusa/connect-ui/shell` and the route matcher as
  `@prusa/connect-ui/router`. The frame draws the header, the side rail,
  the page footer and the bar a narrow screen gets instead of the first two,
  and it never reaches the core, so an application hands it the navigation and
  the connection state and keeps its own route table.

- `tokens.css` carries the semantic colours a design reaches for by name.
  `selected` marks the current destination, `control-on` a switched-on
  control, `progress` work advancing. Each of the four notices gains a toned
  surface, and each of the ten printer states its own name. Every one is
  declared in both themes and in all three skins, so `bg-selected` and its
  neighbours compile in a consuming application.

- The connection state draws as a signal glyph rather than a coloured dot, so
  it reads without a legend, and the rail marks one row. A folder such as
  Settings stays unmarked while a room under it is the address.

- The component library publishes `@prusa/connect-ui/app.css`, the
  stylesheet an application imports. It carries the theme and the scan of the
  library's own components, so an application names no path into
  `node_modules`. A module keeps importing `./tokens.css`, the theme alone, and
  so keeps emitting only the utilities it names itself.

- `@prusa/connect-ui/slots` exports `NavGroup`, the heading a rail draws
  over a run of items that `NavItem.group` names by id. The package root
  exports `applyTheme(dark)` beside `applySkin`, which writes the class the
  theme matches, so a page with no theme control is still drawn on the right
  side.

- The application draws its pages inside the frame. The address bar follows
  the screen, so a copied address opens the same page and the back button
  works.

- The component library publishes its primitives again under `./primitives`,
  each one written from the React design system so the two match class for
  class. They arrive one at a time, so the subpath grows as the ports land.

- Every listing's toolbar ends with the same pair. It says which columns the listing shows, and which shape the listing is drawn in. Six screens had grown that pair on their own, in two orders and at two spacings.

- The component library exports a pager, a page trail, an alert and a clipped name. Pages are real links, so page three opens in a new tab, and a file name gives up its middle rather than the extension at its end.

- The component library exports a slider, a labelled slider field and a value editor. A caller says whether a slider reports one value or a range, so a range never arrives at a control drawn for one number.

- The component library exports a row's actions and a field picker. Every listing opens its row actions behind one glyph at the same 44 pixel size, and chooses its columns from a menu of real checkbox items.

- The component library exports a facet popover and a prompt dialog. A listing filters from a panel of values a reader ticks, and a new folder or a rename asks for its one name and nothing else.

- The component library exports a context menu and an alert dialog. An alert dialog keeps the reader in it: neither Escape nor a press beside the panel is a way out, so a question that matters is answered.

- The component library exports a toggle, a toggle group and a view switch. A listing offers its shapes as a radio group, so a reader hears one choice among several rather than a set of tabs with nothing behind them.

- The component library exports a collapsible. A trigger names the panel it opens, reports whether the panel is open, and a caller renders and turns its own chevron.

- The component library exports a checkbox. It reads ticked, clear or mixed, and the mixed reading is what a box over a set whose rows disagree shows.
- A gate builds and vets every module beside the core in its own folder, so an example a reader copies is one that builds. The four example and fixture modules now declare the Go version the sdk requires.

- The web page reads `core.frontend` before it paints. The title names the tab and the sidebar, the theme and the language are the opening choice until a person picks, and the skin loads its stylesheet.

- The demo takes `-password` on its command line, so the password is the same on every run. Without it a new one is printed on every start.

- A generated module builds its own Tailwind, and every text a generated
  component draws is a key in its own catalogue. `ui/src/styles.css` carries
  the four directives and `tailwindcss` is a dev dependency, so a module may
  name a utility the application does not already use. The catalogue is
  `ui/src/locales/en.json`, a component reads a key with `host.t`, and the
  build copies the catalogue beside the bundle's entry.

- `make new-module` writes one of three kinds of module, and only the
  contributions that were asked for. `-kind ui` gives the manifest
  `kind: asset` with no `contract_major`, no `platforms`, no OpenAPI fragment
  and no Go file. `-kind process` gives a Go process and a manifest with no
  `ui` block, and `-kind both` gives the two. `-contributes` takes `page`,
  `device-card` and `settings`, comma separated. One answer writes both the
  manifest's `contributes` block and the `host.register` calls in the entry,
  so a generated module cannot register what it never declared. Every module
  carries `.vscode/extensions.json` for the languages it holds.

- `make new-module` asks for what the flags did not carry when it is run on a
  terminal. It asks in numbered menus for the kind, the name, the repository
  address, what the module adds, where its folder goes and git. A line
  beneath each question says what the answer is used for. It prints the
  namespace as soon as the address is typed. It ends in a summary that offers
  write, change or cancel, and prints the flag command that writes the same
  module. With no
  terminal, `-name`, `-kind` and `-repo` are required, and a run without them
  names each one missing. `-repo` is the repository address the namespace is
  derived from.

- A generated module carries `install.sh`, which builds it, installs it and
  starts a demo showing it. It numbers its steps from 1, builds the bundle
  and the binary the module has, installs the release with
  `module add -approve`, and starts a server with the module on screen. What
  pnpm prints goes to a log, and the end of that log is shown only when a step
  fails. Running it again is the loop after a change, because every rebuild
  moves the digest the install recorded.

### Fixed
- A camera view on a busy server is answered instead of being refused. The
  timer that takes a camera back from a viewer who never dials now starts once
  the answer exists, and no longer closes a connection still being negotiated.

- The event that says a print started now always names the file being printed.
  A machine's reading arrives one value at a time, and a frame built while one
  was still arriving carried an empty `path` and an empty `display_name`.

- Writing or deleting under a path that climbs out of a storage answers 400
  with `core.storage/INVALID_PATH` again. It answered 404 with
  `core.storage/FILE_NOT_FOUND`, which sent a person looking for a file they
  were about to create. Reading such a path still answers 404.

- The log line for a recovered panic now carries the stack it happened on. A
  request handler and a printer driver relaying a print both wrote the panic
  value alone, which named nothing an operator could look at.

- Asking a storage about a file it does not hold answers 404 with
  `core.storage/FILE_NOT_FOUND`. It answered 400 with
  `core.storage/INVALID_PATH`, which sent a person looking for a typo in a
  path that was fine.

- A storage that is down tells a module to come back rather than that the file
  is not there. A module takes an absent file off its list, so a storage that
  stopped for a minute emptied that list.

- A printer driver that stops while relaying a print a module asked for no
  longer ends this server. The print runs with nobody waiting for it, so the
  failure reached the top of its own goroutine and took every printer with it.

- A module that walks a file piece by piece no longer makes this server parse
  the print file once for each piece. It asks for the length of the file, and
  the description a slicer wrote is read only by the call that answers it.

- A printer whose driver stopped in the middle of a transfer is free again
  straight away. Its transfer record was left in place, so every later file
  sent to that printer was refused as busy until this server restarted.

- A page cursor from an older build is refused with
  `core.validation/REQUEST_INVALID`. Such a cursor named an instant in 1970, so
  the page answered no prints and no cursor, which a client takes for the end
  of the history.

- `GET /api/v1/devices/{device_id}/jobs/history` skipped a print that started
  in the same millisecond as the last print of the page before it. The cursor
  that resumes a page now carries the whole start instant, so a client walking
  the pages reads every print.

- The Fractal link now sends a registered error code with every failure a
  person can see. A refused start, a refused join and a refused decision about
  a server carried an English sentence and no code. The links page had nothing
  to key a Czech or a Polish sentence by, so it could only show the English.

- A server above this one now receives its own error number for each way a
  printer command can fail. It read one number for eight different failures,
  with an English sentence beside it. So it could not tell them apart, and it
  could not show the reason in the language its operator reads.

- A stream that was open before a printer dialled in now carries that printer's
  telemetry. It carried the arrival and the record changes, and no readings
  until the reader reconnected. Each machine now keeps the newest reading of
  each telemetry path it reports, and a reader is handed that set the moment it
  is attached to the machine. A reading taken before the reader was granted
  sight of the machine is still not handed over.

- A telemetry event now carries the value this server kept, not the value the
  driver sent. The size limits applied only to the state the server stores,
  and the event had the value as the driver sent it. So every stream
  subscriber and every event sink received a value over the limit in full.
  When the server refuses to store a patch, it now publishes no event for it.

- The vendor, model, serial number and firmware version a driver reports about a
  machine are now cut at 256 bytes. The server cuts between two characters and
  adds a marker that shows the text was cut. It already did this for every
  other text a printer sends. A longer string went whole into the device
  record, into every log line about the machine and into a metric label.

- A command whose machine lost its connection after the command was sent now
  has `outcome_unknown` set on its record. The record said the server had seen
  the outcome. The driver cannot know whether the printer executed a command
  when the connection is lost after sending it. `GET
  /api/v1/commands/{command_id}` and the event that closes the command both
  report `outcome_unknown`.

- One number now governs how long an extension's process may take to start.
  The handshake and the first call each had a fixed limit of ten seconds, and
  no setting changed it. Both now use the start limit of the instance. An
  instance with no start limit gets ten seconds.

- The server now checks that a module process is the module that was
  installed. At start the server asks the process for its name, its release
  and its plugin contract. When one of them differs from the record of the
  install, the instance fails with
  `core.internal/RESOURCE_MANIFEST_DISAGREES` and the server does not use the
  process. An instance that was not installed with `module add` has no record
  of an install, so the server does not check it.

- Stopping the server no longer waits without a limit. At start, the server
  closes the commands and transfers that the previous run left open. That
  work now has a deadline, and the server waits for it to end before it
  detaches the history store the work writes into. A sink or a link that is
  still writing an event when the configured stop time runs out is abandoned,
  and a log line names it.

- A resource instance declared while the authorization graph could not be
  written was left in the configuration file and never started. Every later
  write was then decided against a configuration older than the file on
  disk. The server now keeps the declaration and starts the instance. It logs
  and reports the failed write, and the next reload writes every declared
  instance to the authorization graph again.

- A printer that was accepted and then never opened its session left the
  capability set resolved for it in memory for the life of the process. A
  printer that reconnects often, or a caller that presents a new fingerprint
  each time, made that memory grow without a limit. The server now drops the
  set one minute after it accepted the printer. A session opens much sooner
  than that.

- A request answered with the internal error code now leaves a log line
  carrying the same trace id the response does. The response gives the caller
  a trace id and hides the message and the cause. No log line had that trace
  id, so an operator could not find the error.

- A request carrying no credential no longer writes a warning. The line said
  the token was malformed where no token had been read. The readiness probe
  holds no credential and is read every ten seconds, so an idle server wrote
  six of these a minute. It is now a debug line saying no credential was
  presented. A credential a caller presents and the server refuses is still
  a warning, and what a caller is told is unchanged.

### Removed
- The Python printer emulator under `tools/printer-emulator/` is gone, and the
  simulated printers are Go. `make demo` starts the server with a fleet already
  dialled in, and `go run ./tools/printersim -url <server>` points a fleet at a
  server you are already running. `make install-deps` no longer installs a
  Python package.

- [sdk] The descriptor-less registration entry points are gone:
  `RegisterFactory`, `IsRegisteredType` and `BuildProvider` on `authapi`,
  `eventsapi` and `storageapi`. Register a type with `Register(name, doc,
  factory)`, which takes the descriptor the published reference is generated
  from, and ask the registry you build with `NewRegistry` rather than the
  package. The `sdk` module moves to `0.2.0` for the removal.

- The build no longer takes a private repository credential. `FRACTAL_GIT_CREDENTIALS`, the
  `fractal_git_credentials` BuildKit secret and the `GOPRIVATE` setting are
  gone from the Dockerfiles, the compose files, `.env.example` and the release
  scripts. Every module this repository requires is public, so a fresh clone
  builds every target and runs every suite with no token.

- The `sqlite` and `dragonflycache` build tags are gone. Neither gated any
  code. SQLite is compiled into every build behind no tag, so
  `persistence.type: sqlite` keeps working and implies no compiler argument.
  Remove either tag from a build command that still passes it.

- The server that came before this one is gone. The `serve` subcommand and the
  60,828 lines behind it are deleted, with everything that existed only for
  them. The binary now answers one server, and the six shipping targets are
  25.9 to 27.7 MB smaller. What the old server worked out is kept as frozen
  fixtures and in EP-20. An operator still writing
  `serve` gets the core's own usage error. A configuration block only the old
  server read is named in a warning at startup.

### Changed
- The developer view takes a permission of its own. `/dev`,
  `GET /api/v1/dev/connectors` and
  `GET /api/v1/dev/devices/{device_id}/commands` took any caller with an
  identity. They now take `develop_device` on the `driver` resource kind, so an
  org administrator reaches them on day one and a fleet viewer is refused with
  403. The authorization vocabulary is at 4.2.0, and
  `POST /api/v1/access/check` answers for the new action.

- The component library gains the menus, popovers, tooltips, sheets, toasts
  and the notification bell that Prusa Connect's pages use, plus the
  printer and telemetry icons, the heating display, the user menu and the
  phone bar. A module's page can use every one of them.

- The app uses real addresses. A screen has its own path, so a reload, a
  copied link and a middle click all open it, and an address this server has
  no screen for says so instead of showing the fleet.

- The component library ships compiled JavaScript and its `.svelte` files
  under its `svelte` export condition, so a consumer's dev server starts
  without excluding the package from dependency optimization.

- The web page the server carries is the Svelte one. The React interface is
  gone, and with it the two build variants the retired server embedded. `/`
  now serves the fleet, a printer's own page and the settings screen from the
  component library, and `make frontend-check` and `make frontend-build` are
  the commands that gate and build it.

- A local npm registry runs from compose for development. `make registry-up`
  starts it, `make registry-publish` puts both packages into it at a dev
  version, and `make registry-use` points any project on the machine at it.
  Nothing publishes to npm until the repository opens.

- The fleet, a printer's own page and the settings screen are drawn from one
  component library, and a module hangs its own sections and panes on them.
  The settings screen lists every installed module with its state and says
  why a module's bundle was refused, so "installed and not running" has an
  answer on screen.

- `examples/nozzle-watch` is a module that is one browser bundle and no
  process. It adds a nozzle-temperature section to every device card, read
  from the fleet the page already holds, and installs like any other module.

- A module can now add a sidebar item and a page of its own. `examples/spools`
  is the first extension with both halves: a Go process serving two routes and
  a bundle drawing them on the component library. The page reaches the routes
  through the client SDK and the browser's session, with no token in the page.

- A module's bundle shares the app's Svelte runtime, component library and
  client SDK through an import map in the page. `@prusa/connect-ui/build`
  gives a module author the whole Vite config that produces such a bundle, so
  it carries neither a second Svelte nor a second library.

- The full build serves its own web interface at `/`, and an address under it
  that names no file answers the same page, so a reload works. That build also
  names `core.feature.ui` in its capability vocabulary, which a module writes
  under `requires` when it adds to the interface.

- The core's own pages are being rebuilt on the Svelte component library.
  A sign-in that posts to `/api/v1/auth/login` and a fleet screen that reads
  `/api/v1/devices` and follows `/api/v1/events` are the first two. Every
  request goes through the published client SDK and the browser's own session
  cookie. The React app stays until the new one covers its screens.

- The component library gains a fleet store over the SDK and a slot registry
  held to a module's manifest. It also gains the host object a module bundle
  receives and the loader that admits one. A module's page, device card section or
  settings pane renders inside a boundary, so a throw stays inside it.

- The client SDK and the component library now live under `packages/` in
  this repository, not `clients/`. Nothing changes for a consumer: the two
  npm package names stay, and the Go SDK stays at `sdk/`.

- A module says what it draws in the `ui` block of its `module.yaml`, and
  `/api/v1/resources` publishes that block for an enabled and ready module.
  The listing carries the entry file's address, the library range the bundle
  was built against, and the pages, device-card sections and settings panes it
  may register. `sdk/manifest` reads the file. It refuses an entry that leaves
  the bundle, a range written in any other form, and a name that is not one
  label. A key it does not know is refused too.

- The component library gains its domain rules, its app shell and the four
  slot names, so a module's page decides a verb, a heater and a printer state
  the way the core's own page does.

- A module that ships only files for the browser is declared under the new
  `asset` resource kind, with the directory and the digest recorded over it.
  The server checks that digest at every start and serves the files at
  `/api/v1/modules/<namespace>/ui/`, where a request takes `view_resource` on
  the instance the way a module route does.

- The Svelte component library `@prusa/connect-ui` starts under
  `packages/svelte` with the design tokens, three primitives and browser
  tests. Six of its eight subpaths are empty until the screens and the slot
  registry land.

- A page the core serves can build the client with `auth: { kind: "session" }`
  and sign in with the session cookie instead of a token. No `Authorization`
  header is sent and every request carries `credentials: "same-origin"`, the
  event stream included.

- Every path and query parameter in `/api/v1/openapi.json` carries an example
  value, so a sample built from the document names a real device, credential
  or preset rather than `{device_id}`.

- `make fixtures-up` works on a host whose Docker refuses bind mounts,
  `make test-race-linux PACKAGES=<pkg>` runs the race detector on Linux, and
  `make docker-clean` removes what this project left behind.

- **`make demo` reads `config/config.yaml` and serves a login.** A module
  declared under `resources:` starts under the demo, and an origin listed in
  `core.api.allowed_origins` is honoured. The demo prints a `demo` account with
  a new password on every run, so `POST /api/v1/auth/credentials` mints a token
  instead of answering `CREDENTIAL_REFUSED`. A listing now takes that token,
  and `/dev` opens once the browser holds the session the printed line gives
  it.

- [sdk] A module that serves routes ships an OpenAPI fragment through
  `routesapi.Describe` or is refused at launch with
  `core.internal/RESOURCE_PLUGIN_UNDOCUMENTED`, and a module with no routes
  needs none.

### Added
- A request that waits too long is answered with
  `core.validation/REQUEST_TIMED_OUT` after `core.api.request_timeout`, thirty
  seconds by default, and the streaming and file routes carry
  `core.api.stream_timeout` instead. A command waits for the printer only as
  long as the connector's `command_deadline` allows, and a module that will
  not stop is killed on the host's own budget.

- The server answers GraphQL at `/graphql`, serving its own devices, jobs and
  files under the same names as the REST API, switched by
  `core.compat.graphql.enabled`.

- The web app is configured in `config.yaml`. `core.frontend` sets whether it
  is served and its title, language, theme and skin, and the page reads them
  from `GET /api/v1/ui` before it paints. `core.docs.enabled` decides whether
  the documentation site is built. `core.compat` names the GraphQL and
  old-REST compatibility layers, both declared and switched on in the shipped
  configuration. A layer that is switched on and not installed answers 503
  at its address instead of 404.

- The component library ships a Storybook of every component, with a story
  beside each one and a switch for the theme and the brand skin. `pnpm run
  storybook` opens it, and the library gate builds it.

- Adding a device starts by asking which machine you have, one card per
  connector the server carries, Prusa first. A machine this server calls is
  added from the dialog with an address and an optional key. A device page
  names its connector, and the demo fleet includes a Klipper printer.

- Every vocabulary read answers `vocabulary`, `version` and `format` beside
  its entries, and the fault parameter keys are a published closed set in
  `docs/errors/error-codes.json` under `param_keys`. A gate over the OpenAPI
  document refuses a `printer_id`, an instant that is not a date-time string,
  a duration key without `_ms`, and a paging list without its four members.

- The docs site walks through the demo. One page starts `make demo` and signs
  in. It then goes through the fleet, one printer, a print, the Tools page,
  Fractal, Extensions, Settings and the first run, with a picture of each page.

- `make demo` builds the two example modules into release archives on your
  own machine and writes the index the extensions page lists from. A module
  installs from it with no network and no published release. The packing is one
  script the demo and the example gate share, and both `module.yaml` files
  carry a summary and a code owner, which is what the card shows.

- The first run sends the admission choice it asked for, and a run left
  halfway resumes at the step the server has not recorded yet. An extension
  card says the release, what it does and who answers for it, and an
  installed one says the same from its manifest. A card's print progress,
  state and file follow the machine instead of waiting for the event stream
  to reconnect. A printer's silhouette stands on a shelf of its own, so it
  reads on a dark card. The settings page says the name on your account, with
  the subject under it. Waiting for a device to call draws a segment crossing
  the track instead of a full orange bar, and a card's camera thumbnail shows
  the whole frame.

- The first run reports the steps it has taken. `GET /api/v1/setup` and
  `POST /api/v1/setup` carry a `steps` list naming the mode, the first
  administrator, the admission choice and each declared module. It is read
  from what is on disk, so a person who closes the browser halfway resumes
  where they stopped.

- The first run takes the admission choice. `POST /api/v1/setup` accepts
  `admission`, `ask` or `open`, and writes it into the machine-owned file
  beside the auth mode. The shipping xBuddy connector reads its own mode from
  it wherever the operator wrote none.

- A module listing says what a card needs. Every module row of
  `GET /api/v1/resources` carries the release, the summary and the author its
  manifest declared, and every row of `GET /api/v1/modules/index` carries the
  author beside the release.

- An administrator sets a person's role over the API. `PUT
  /api/v1/auth/accounts/{account}/role` takes `viewer`, `operator` or `admin`
  and replaces the role the account held, so making somebody else an
  administrator is one call. The change that would leave the server with no
  administrator is refused with `core.auth/LAST_ADMINISTRATOR`.

- The Tools page is for a developer. Pick a printer, send G-code lines and
  read each answer, or press a stored preset. The same page shows the command
  log newest first, the state tree moving by path, and the camera.

- The application draws every screen of the first-run to first-print
  journey. Add a device on the fleet opens the guided dialog, a device that
  was let in opens its own page, and Extensions lists the catalogue.

- The device card and the device page are redrawn around the printer. The card
  shows the model's silhouette, the name a person gave it, its state, the
  print in progress with both heaters, a camera thumbnail and one primary
  action. The whole card opens the page. The page opens on a hero with the
  silhouette, an inline rename, the model, firmware, serial and id, the camera
  beside the print controls. The fleet filters by state and issues, searches,
  and sorts by name, model or state.

- The first run is a guided page. It records whether a login is required,
  creates the first administrator, switches the declared modules on and shows
  a review before one call records the decision. A refusal keeps the form and
  shows the code beside the server's message.

- A printer is added from a guided dialog. The dialog waits for the machine
  to call this server, shows what it says it is, lets it in or turns it away,
  names it and opens its page. A refusal or a timeout shows the code with a
  help link.

- The Fractal page shows the link and the nodes it reaches. It says whether
  a link is declared and on, its mode, how it admits nodes, and this node's id
  and key. Each node is listed with whether it is alive, its last heartbeat
  and its round trip, and a pending node is approved from the table. A setup
  dialog ends on the configuration block to copy.

- A camera this server cannot dial sends its own picture.
  `PUT /api/v1/cameras/{camera}/snapshot` takes one JPEG, at most 8 MiB, and
  keeps it as that camera's most recent frame.
  `GET /api/v1/cameras/{camera}/snapshot` and
  `/api/v1/cameras/{camera}/stream` then serve it the way they serve a frame
  the server fetched. Declare the camera with `type: push`, whose `stale`
  option says how long one frame is served after it arrives, five minutes by
  default. The call takes `operate` on the camera, which a viewer does not
  hold, and a body that is not a JPEG is refused with
  `core.validation/FRAME_NOT_AN_IMAGE`.

- [sdk] The Fractal link protocol is a package of the SDK,
  `github.com/prusa3d/prusa-connect/sdk/fractal`. `Dial` opens a
  child's connection to one parent and `Listen` accepts children. Each holds an
  Ed25519 key, runs the signed join and the heartbeat, and reports every node
  it has met with the round trip it measured. The package comment is the
  specification, so a node in another language can be written from it. The
  `fractal` build tag is gone, because the package imports the standard library
  alone. `link: core/fractal` now resolves in every build. A build command, a
  Dockerfile or a CI job that still passes the tag should drop it.

- A module's settings pane keeps its own choices on the server.
  `GET /api/v1/modules/{namespace}/state` lists the keys the signed-in caller
  holds under that module, and `GET`, `PUT` and `DELETE` on
  `/api/v1/modules/{namespace}/state/{key}` read, write and remove one of them.
  The value is any JSON document, returned as the bytes that were sent, because
  the shape belongs to the module. A record is private to one person: only the
  caller who wrote it may read it. Reaching the family at all needs
  `view_resource` on the module instance. One record holds four kilobytes,
  which `core.module_settings_bytes` raises to at most 64 KiB, and a larger
  body is refused with that number in the message.

- Every simulated printer now comes with a simulated camera. `make demo` shows a
  live picture of each machine, with no hardware on the desk. The frames are
  drawn in the server. Each one carries the printer's name, what it is doing, a
  bar that fills as the print advances, a timestamp and something that moves, at
  five frames a second. They are served on the paths a real camera is served on:
  `GET /api/v1/cameras/{camera}/snapshot` and
  `/api/v1/cameras/{camera}/stream`. A page cannot tell one from a device.
  `make demo DEMO_ARGS="-cameras=false"` turns them off.
  `go run ./tools/printersim -cameras=false` does the same for a fleet pointed
  at a server you already run. The simulator also answers a G-code line the way
  a serial port does. That reply comes back in the new `answer` field of
  `POST /api/v1/devices/{device_id}/gcode`: `ok` for most lines, the
  temperatures for `M105`, the position for `M114`. A real printer sends no
  reply text, so the field is absent there.

- A page prints a file in two calls, installs a module without shell access,
  and shows what each person may do. `POST /api/v1/devices/{device_id}/print`
  takes a storage, a source and a path: it relays the file to the machine and
  starts the print, and answers the transfer and the printer's reply. The same
  call with a path alone prints a file the machine already holds.
  `POST /api/v1/modules` installs a module from a directory or a `.tar.gz` on
  the server, or from an https URL of one. It runs the same checks
  `module add` runs. `DELETE /api/v1/modules/{namespace}` removes it, and
  `GET /api/v1/modules/index` lists what can be installed, read from
  `index.json` in the directory `core.modules` names. Every row of
  `GET /api/v1/auth/accounts` carries `role` and `grants`, the org roles that
  account holds. The `type` URL on every failure now opens the error reference
  at that code's own section, such as
  `https://prusa-connect.org/docs/error-codes/#core-printer-not-found`.
  A screen that has just failed has a help link that lands somewhere.

- A site says what its Fractal link is doing, and two sites now connect to each
  other. `GET /api/v1/links` and `GET /api/v1/links/{name}` report each
  configured link's mode, how it admits nodes, its own node id and public key,
  and every node it has met. Each node carries whether it is alive, when its
  last heartbeat arrived and the round trip in milliseconds. A link with
  `mode: child` dials the address under `parent`, proves the key in `key_file`
  and is admitted; one with `mode: parent` accepts children on the address
  under `listen`. A parent set to `enrolment: explicit` holds each child until
  `POST /api/v1/links/{name}/nodes/{id}/approve` admits it. `make mockup-up`
  starts two of these servers in containers with fake printers on both, and
  `make mockup-status` prints what each one reaches.

- A new install says whether it needs a login before it serves anything else.
  `GET /api/v1/setup` reports `pending` while no decision is recorded, and
  `POST /api/v1/setup` records `open` or `required`. `required` creates the
  first administrator and answers with their session cookie; `open` serves
  every request as `core.principal`. Either call also enables or disables the
  declared modules it names. Until a decision is recorded, every other path
  answers 503 `core.auth/SETUP_PENDING`, while the health probe, the readiness
  probe and the page at `/` still answer. The choice is the `auth.mode` key in
  `config.d/resources.json`, and a second call answers 409. A server that
  already has an issuer, `core.auth.local`, accounts or a `core.principal` the
  operator chose has the decision written for it at its first start.

- A page adds a printer, names it, and reads the whole of what that machine has
  printed. A registration reaches the event stream as
  `com.prusa.connect.device.registration.announced`, `.approved` and
  `.rejected`. Each is published on the connector that holds the code, so a
  wizard waits for a machine instead of asking every second.
  `POST /api/v1/registrations/{code}/approve` answers the `device_id` it bound,
  so that wizard opens the device page without matching serial numbers.
  `PUT /api/v1/devices/{device_id}/name` writes what a person calls a machine,
  and the name survives the machine reconnecting.
  `GET /api/v1/devices/{device_id}/jobs/history` pages the prints that have
  ended, newest first, by cursor.
  `GET /api/v1/vocabularies/device-models` lists the models this build carries a
  profile for, each with the name the product is sold under and a key to map to
  an icon. `GET /api/v1/devices` takes `state`, `model`, `search`, `retired`,
  `sort` and `order`, so a fleet screen narrows and orders without reading every
  page. `DELETE /api/v1/devices/{device_id}` retires a machine somebody has
  taken away. It leaves the listing, its record and its prints stay readable
  under the id, and the same printer dialling in again is given a new one.

- The reference carries a Prusa Connect compatibility page. For every query,
  mutation and route of the old surface it says which operation of this API
  answers it and how each field and error code translates. A refused
  operation carries the reason.

- [sdk] A module reads what a device is printing and what it holds through
  `device.jobs` and `device.files`, on the permission rule the HTTP routes
  apply. No file content crosses the wire: a transfer stays an HTTP operation.

- A Prusa Account token opens the API and the event stream once an
  administrator has paired its subject at `POST /api/v1/auth/pairings`. An
  `auth:` instance of type `core/prusa-account` under `resources:` verifies
  the token offline against the issuer's published keys. An unpaired subject
  is refused with `core.auth/BEARER_UNPAIRED`.

- [sdk] A module's install block takes an optional `service.mount`, a root
  prefix such as `/graphql`, and the host serves the module's routes and its
  `openapi.json` there instead of under `/api/v1/modules/{namespace}/`. The
  same permission is asked and the same principal is handed on `routes.serve`.
  A prefix the core serves, one the router cannot route, or one another module
  holds is refused at load with `core.internal/RESOURCE_MOUNT_RESERVED`,
  `RESOURCE_MOUNT_INVALID` or `RESOURCE_MOUNT_CLAIMED`. Every instance in
  `GET /api/v1/resources` carries `mount` beside `namespace`.

- A `fractal` build accepts one `link:` instance of type `core/fractal` under
  `resources:`, which generates the node's key at first start, records the
  `parent` address and publishes `com.prusa.connect.link.state.changed`. It
  dials nothing yet, and a build without the tag refuses the type as unknown.
- `make docs-up` serves the documentation site from a container at
  `http://127.0.0.1:54322/docs/`, built the way it is deployed, and
  `make docs-down` removes it.

### Fixed
- A resource declaration accepted over the API now starts even when the
  caller stops waiting for the answer. Before, a client that gave up after
  the write left an instance the listing showed and nothing supervised until
  the next start.

- The demo repeats a camera declaration or a print start that timed out on a busy machine, instead of printing "no camera" and leaving the fleet still.

- A fresh clone builds and starts the demo with `make demo` alone. Before, packing the example modules failed because the client SDK they link to had not been built.

- The web page no longer preloads its font file, which a browser reported
  as unused on every load.

- A camera this server cannot read answers 502 with
  `core.resource/CAMERA_UNREACHABLE` on the snapshot, stream and WebRTC
  paths, instead of 500 with no code.

- A sign-in with a wrong password or an unknown username answers
  `core.auth/LOGIN_REFUSED`. The sign-in page says "The username or
  password is wrong" instead of the raw fault about an invalid token.

- The demo keeps its cameras when it is started again on the same folder. Before, every camera declaration was refused and each snapshot answered 500.

- Every printer card on the fleet draws the same rows at the same heights.
  The camera picture sits in the same place whether the printer is printing,
  idle or has no camera at all.

- An installed module's row can be switched on and off through the resources
  API. Its type may be as long as the grammar allows. A row with no settings
  answers an empty object, a put of that row is accepted, and the write keeps
  the artefact the module is launched from.

- Every request the client sends gives up after thirty seconds, and opening
  one folder after another no longer shows the rows of the folder you left.
  A camera's live view keeps playing past sixty seconds. Every database pool
  has a bound on its connections and, on Postgres, on how long a statement
  may run, settable under `persistence`.

- Installing or removing a module through the API takes effect at once. The
  module starts, its page and sidebar entry appear, and removing it clears
  them, with no restart of the server.

- A first run that lets everybody in lands on the fleet. The page read the
  principal only when the answer said somebody signed in, so an open server
  stayed on the sign-in screen with no account to sign in with.

- Uninstalling a module that is running no longer half-succeeds. The server
  stops its process, waits for the files to go, and only then drops the
  declaration. A delete that fails leaves the module installed, and the same
  request finishes the job.

- Pressing Print no longer freezes the page. The screens asked the host for a
  printer's transfers store from their markup, which Svelte runs as a derived,
  and the store's first write was refused as an unsafe mutation. The store is
  now built before the markup reads it.

- The sidebar footer fits its column. Online, the language and Sign out were
  wider than the sidebar and Sign out was cut off.

- A browser that has never signed in is asked to sign in, not told that its
  credential was refused. The People pane draws on the settings page, and the
  count under a person's name says devices, because it counts cameras as well
  as printers.

- Buttons, cards, badges, sheets, menus, popovers and inputs take the radius
  tokens of the design system. They sat at two pixels while the newer forms
  and dialogs used the tokens, so one dialog drew three radii.

- A telemetry reading is rounded to two decimals. A position read
  `21.200000000000003 mm` where the floating point showed through; it reads
  `21.2 mm`.

- The composed-binary gates record the login mode before they read the
  server, so a fresh install no longer refuses them with `SETUP_PENDING`. The
  gate also fails on any answer other than 200 instead of reading a refusal
  as an empty document, and the composed binary takes the `.exe` suffix on
  Windows.

- [sdk] A module no longer deadlocks with the host when a data frame lands
  while the host is between reading its buffer and waiting for more. The
  host waited on data it already held and never granted the module more
  credit, and the module waited for that credit. The wait now re-checks the
  buffer under the lock it captures the signal with.

- **A transfer is reported done only once the file is on the printer's own
  storage.** Two files sharing one FAT 8.3 short name were both answered
  `state: done`. The printer listed only the first of them.
  `POST /api/v1/devices/{id}/files` now reads the destination directory back.
  For a file that is not there under the name it was sent to, it answers
  `409 core.printer/TRANSFER_NOT_ON_STORAGE`, carrying `path`, `expected_size`
  and `found`. A printer that cannot be asked for a listing still finishes the
  transfer, and the transfer carries a `warning` saying the file was not
  confirmed.

- [sdk] `connectorapi.Progress` carries `Warning` and `transferapi.Record`
  carries `Warning`, both for a transfer that finished unconfirmed, and
  `connectorapi.TransferNotOnStorage` is the code a driver reports for a file
  that is not on the storage under the name it was sent to.

- **A telemetry path that reports a word is stored and can be asked about
  again.** A printer's own `/state` and `/material` reached the event stream
  and no history store. A stored sample carried a number and had nowhere to put
  a reading like `PRINTING`. `GET /api/v1/history/samples` now answers
  that series, and a reading renders as `text` or as `value` and never as both.

- **A word-valued reading is stored clipped to 256 bytes.** The paths this half
  of the store exists for report a few characters. A driver reporting a
  filename at a state path would otherwise write it on every report. The
  detach line counts the clipped ones beside the untyped ones.

- [sdk] `historyapi.Sample` carries `Kind` and `Text` beside `Value`, and the
  history provider contract is `1.1.0`. A provider built against `1.0.0` still
  registers and keeps storing the numeric half; one built against `1.1.0` needs
  a host that serves it.

### Security

- **A driver declares its own verbs, and the core no longer has to be patched to
  add a device class.** Command dispatch refused any verb the vocabulary did not
  classify. The authorization vocabulary held a static table of the 59
  `core.command.*` verbs, so an adapter's own commands were registrable and
  undispatchable.
  The table now has a row per device kind: the printer's verbs are one kind, and
  every kind a build compiled in is another. `GET
  /api/v1/vocabularies/authorization` lists them all, with the class each verb
  takes and any marking that puts it out of reach of a role.

- **BREAKING. The device permissions are renamed and folded.** `view_telemetry`
  is now `observe`; `send_command`, `upload` and `cancel_job` fold into
  `operate`; `provision` is now `configure`; `manage` is now `administer`. No
  grant changes what it reaches: the operator level conferred all three folded
  actions already. A new action, `view_audit`, reads who authorized which
  command on a device, and it is conferred at manager and by nothing lower.
  `send_gcode` stops being an action and becomes a marking on the two raw gcode
  verbs. Anything naming an old action, in a policy, a script or a client, takes
  the new name.

- **Raw gcode, firmware flashing and the other irreversible verbs are conferred
  by no role at all.** They take a row naming the subject, the device and the
  verb. The row is written with `PUT /api/v1/grants/device/{id}/verbs`. Or they
  take ownership of the machine, which reaches every such verb on the device it
  names. A manager who
  could flash a printer yesterday cannot today without one of the two. The owner
  of a printer may now send it raw gcode, which a defect in the shipped mask had
  been refusing.

- **A rebuild cannot quietly lower what a declared verb takes.** This server
  records the class and the markings every declared verb was first seen with.
  The record is keyed by the module that declared it. A build that raises a
  class applies on its own. One that lowers a class, or drops a marking, is held
  and named in the vocabulary until somebody accepts it with
  `POST /api/v1/grants/kinds/{kind}/acceptances`. A device kind that reappears
  from a different module than first seen is refused outright.

- **[sdk] `connectorapi.RegisterKind` declares a device class and its verbs.**
  An adapter writes the declaration beside its driver registration. It names
  the kind, a display name, and each verb with the class it takes and any
  marking. The host
  derives the namespace from the module path, refuses a verb outside it, and
  computes the `opaque_payload` marking from the capability descriptor whatever
  the declaration says. `extension.KindDeviceKind` is the extension point.

### Added

- [sdk] `generate-client-sdk -module` writes a module's own typed client
  package from its OpenAPI fragment, with a `bind(client)` that attaches the
  module's operations onto a `@prusa/connect` client.

- [sdk] A module declares an OpenAPI 3.1 document for its routes, paths
  relative to its mount, as the optional `openapi` member of `plugin.info`,
  attached with `routesapi.Describe`. The host refuses one that does not parse,
  has an absolute path, or an operation missing `operationId`, `summary` or
  `description`; a module shipping none starts, its routes logged as untyped.

- **A running module's OpenAPI document is served at
  `GET /api/v1/modules/{namespace}/openapi.json`**, rebased under its mount, on
  the same `view_resource` permission as the module's routes. Every instance in
  `GET /api/v1/resources` now carries `runtime`, a module carries `namespace`,
  and a module that ships a document carries `openapi_url`. A client discovers
  enabled modules and their documents in one call.

- **A bearer token opens the event stream at `GET /api/v1/events`.** A
  script or the client SDK subscribes with the `Authorization: Bearer` header
  it calls every other operation with. A token revoked or expired under an
  open stream ends it with a `prusa-connect.auth.expired` frame within
  the keep-alive.

- **A signed-in person can mint a bearer token at
  `POST /api/v1/auth/credentials`.** It is shown once and carries a name and an
  optional `expires_at`, ninety days by default. A script presenting it is
  accepted wherever the session is on the HTTP API and speaks as the same
  principal. An administrator can mint one for a `service_account:` subject.
  `GET /api/v1/auth/credentials` lists name, creation, expiry and last use
  without the secret. `DELETE /api/v1/auth/credentials/{credential}` revokes
  one on the spot.

- **A page served from another origin can call the API from a browser once
  its origin is listed under `core.api.allowed_origins`.** The list is empty by
  default. A listed origin is answered with its own name,
  `Access-Control-Allow-Credentials: true`, and a preflight naming the methods
  and headers the API takes. An origin off the list is answered as before, and
  `*` is refused at startup.

- [sdk] A TypeScript client package, `@prusa/connect`, is generated
  from the core's OpenAPI document and registries under `packages/typescript`,
  with one typed method per operation. It takes a bearer token or a token
  provider, and a refused request throws an `ApiError` carrying the status,
  error code, params and trace id.

- [sdk] `events()` on the TypeScript client reads `GET /api/v1/events` over
  `fetch` with the bearer token and yields typed events. After a drop it
  reconnects with `Last-Event-ID` on the server's `retry` hint, and it ends
  with an `ApiError` when the server revokes the credential.

- **`GET /api/v1/grants/subject/{subject}` answers what one person holds**, per
  device and per kind, with the verbs their classes reach. An account listing
  now renders the subject a grant names, and `POST /api/v1/auth/accounts` takes
  an optional `role`, so one call makes a usable person.

### Security

- **`GET /metrics` now takes a grant, and a scraper that has none is refused
  with 403.** It took identity alone before. Every metric family carries a
  `printer_id` label, so any account that could log in read every device this
  server has seen. That is the fleet-shaped fact `GET /api/v1/devices` refuses
  to a caller holding no grant.

  What the grant is: telemetry on every device this server knows about. The
  answer is all or nothing, because a scrape is machine to machine. One
  exposition cannot be narrowed per caller without giving two scrapers two
  different series sets under one metric name. The per-device labels are
  unchanged.

  **A deployment with no `core.auth` block is not affected.** Every request
  there resolves to `core.principal`, which owns the graph and reaches every
  device. A self-hosted server with no login serves scrapes exactly as before.

  **If your scraper is being refused**, give its service account one org tuple
  rather than a grant per device:
  `POST /api/v1/grants/org/local {"relation":"viewer","subject":"service_account:scraper"}`.
  An org role reaches every device in the org, so it keeps working as machines
  arrive. A caller who reaches some devices and not all of them is refused. So
  is one who reaches none: a partial fleet is not a fleet.

  The refusal is 403 and not 404, so a scraper that lost its grant reads
  differently from a build compiled with no metrics client, which still answers
  404.

- A declared `auth`, `driver` or `db` resource instance is now refused at load
  instead of reported ready. Nothing on this server read one of those instances
  back. An operator who declared `auth: {idp: {type: core/oidc}}` and no
  `core.auth` block was shown `state=ready serving=true` on `/api/v1/resources`.
  Every request with no credential was answered as `user:operator`, the owner of
  the authorization graph. The declaration now fails that instance alone, the
  server serves the rest, and one warning names the reason. A deployment with
  `core.auth.local` or `core.auth.issuer` set was never open: those paths
  refused an unauthenticated request with 401 and are unchanged.

- `UPGRADE` and `FLASH` no longer accept a path that leaves the printer's
  storage. Both name a firmware file on the machine, and the shipping server
  checked a path only for a list of eight other commands, so
  `usb/../etc/passwd` reached the printer as `/usb/../etc/passwd`. Which
  commands the check covers is now read off the command definition, so any
  command that names a path answers to it. Both verbs already take the
  `provision` action, which an operator does not hold.

### Removed

- Home Assistant discovery is not published, and the core will never publish
  it. The retired server announced 23 entities on `homeassistant/#`, so an
  install moved onto the core finds no devices in Home Assistant. Each sensor
  has to be configured there against the `v1/` topics, which are unchanged.
  Serving discovery is a module's job rather than the server's, and no such
  module is built yet.

- Eleven metric families that nothing wrote are gone from `/metrics`. A scrape never carried
  any of them, so nothing an operator has can be reading one. A name a dashboard
  can be pointed at and never see is worse than an absence.
  They are `pcl_printer_frames_received_total`, `pcl_printer_frames_sent_total`,
  `pcl_printer_bytes_received_total`, `pcl_printer_bytes_sent_total`,
  `pcl_events_dropped_total`, `pcl_event_queue_depth`,
  `pcl_printer_job_progress_ratio`, `pcl_breaker_state`,
  `pcl_retry_attempts_total`, `pcl_retry_budget_exhausted_total` and
  `pcl_bulkhead_rejected_total`.

  Frames and bytes are counted inside a driver's socket, and no driver contract
  carries the count up. A figure taken above it would be a different number
  under the same name. The two event families belong to the event hub, which
  publishes no total anything outside it can read. The rest describe behaviour
  that is not built: a retry budget, a three-valued breaker state, and a job
  progress path that only one dialect spells. Each one is a row on EP-15 saying
  what has to exist for it to come back.

### Added

- `GET /api/v1/vocabularies/device-states` reports the states each device
  kind's adapter declared, with a display name for each and the verb that
  causes each move. It takes an identity where the other vocabularies do not,
  because its entries are what this deployment installed rather than what the
  build ships. It answers an empty list until an install places a declaration.

- [sdk] An adapter declares the states its device kind reports and the moves
  between them, as `connectorapi.StateSet`. `Problems()` is the same check the
  host runs when it places a declaration, so an author reads a refusal before
  they ship rather than at install.

- [sdk] `core.printer/VERB_NOT_ALLOWED_IN_STATE` is published in the error
  reference and on `/api/v1/vocabularies/error-codes`, for a verb a device has
  and the state it is in does not allow. No endpoint raises it yet, because the
  state a device reports is still free text this server does not read.

- A module's own approval of a device records the module instance as what
  admitted it and never mints a new device id. The registration's
  `admitted_by` reads `sink:<instance>`, no owner is written, and a machine
  this server already knows keeps the id it had. An approval a module makes for
  a person it is serving is that person's, as it would be at the API.

- `[sdk]` A module admits a device waiting to be let onto this server.
  `sdk/deviceapi` gains `device.approve`, which takes the code the device is
  displaying. It is a permission of its own. An instance granted every device
  still cannot admit one until an operator grants it `admit_device` on the
  driver resource kind, which is what the HTTP surface asks before it approves.
  Approving decides admission and does not open a connection, so the device
  joins the listing when it next dials.

- `[sdk]` A refusal the fleet raises reaches a module as the sentinel it
  branches on. An argument the verb does not take is `deviceapi.ErrArgument`,
  and a full command queue is `deviceapi.ErrBusy`. A verb the principal does
  not hold on a device they can read is `deviceapi.ErrDenied`. Each arrived as
  the host's one-sentence failure before.

- A module's own work runs inside a ceiling the operator writes at install.
  `service.permissions` on the instance block lists the actions the module may
  take as itself on the fleet and the event stream, such as `observe`, `operate`
  and `admit_device`. A `device.*` call or a `bus.subscribe` that names no
  person is refused unless its action is listed. The authorization graph then
  decides it for the instance's own principal, so a module reaches what it
  declared and what it was granted and never more than either. An instance
  block that lists nothing may do nothing as itself, which is the default. A
  name the graph does not hold fails the instance at load.

- `[sdk]` A module's fleet call and subscription carry the principal of the
  person it is serving. Every `device.*` params object and `bus.subscribe` take
  a `principal`, the same field `routes.serve` hands a module on the way in, and
  the host answers as that person. A principal the host did not hand the module
  in a request it is still serving is refused with `deviceapi.ErrDenied` or
  `busapi.ErrDenied`. A subscription opened for a person ends when their request
  does. `deviceapi.Client.Get`, `Command` and `Approve` take their params
  object whole, so a module written against the earlier signatures changes the
  call and nothing else.

- `[sdk]` A module subscribes to the event stream. `sdk/busapi` gains
  `bus.subscribe` beside `bus.emit`, so the bus is one contract with two
  directions: a module names what it wants and the host writes it a stream of
  coalescing windows. It observes what the subscription's principal was granted,
  the way a browser reading `GET /api/v1/events` does, and a restart resumes.

- `[sdk]` A module reads the fleet and drives one device. `sdk/deviceapi`
  carries `device.list`, `device.get` and `device.command`. A call that names no
  principal is resolved against the instance's own subject in the authorization
  graph. A module reads the printers an operator granted it and cannot widen
  that, and one granted nothing lists nothing.

- `GET /api/v1/events` takes a `types` parameter. A page can ask for the
  event types it renders instead of being sent every telemetry sample the
  caller may see. A type this build never publishes is not refused. It matches
  nothing, so a client written against a later release keeps working here.
  Omitting the parameter is unchanged and sends every type.

- The fleet screen shows every printer the caller may see. It read one page and
  printed a line saying how many printers it was not showing. On a fleet larger
  than the server's page size the table was never the fleet. The screen now
  follows the listing's `offset` until the pages cover the total. The line
  survives for the case where the walk stops early, and says that instead.

- `core.authz.org_admin_handover` decides how an administrator hands an
  organisation over. It is off, and off is what the server has always done. The
  admin role on an org cannot be written over the API. So an administrator who
  strands themselves is told to grant a successor first, and has no way to do
  it. The recovery is to change `core.principal` and restart. Turn the key on
  and the successor is granted over the API, then withdraws the departing
  administrator. That is the handover the refusal already recommends. The price
  is that an object's `org` edge stops being writable over the API. A caller who
  administers one org and manages one object of another could otherwise attach
  their own org to that object, and outrank the administrator it already had.
  Nothing legitimate writes that edge over the API: a device gets its org when
  it registers, and a resource kind gets its own at first start.

- `-printer-addr` overrides `core.printer.addr`, the way `-addr` overrides
  `core.api.addr`. A bench session that wants the device-facing listener on a
  second socket no longer has to edit the configuration file. Some keys are
  defaulted only for a deployment that binds that listener. A flag that turns
  it on now gets those too, so nothing is refused for a timeout nobody wrote.

- `GET /p/auth` answers again, on the frozen wire a Prusa printer dials. A
  machine presents the `Fingerprint` and `Token` headers it registered with and
  is told the id this server knows it by, which is the same id
  `GET /api/v1/devices/{device_id}` uses. A request missing either header is
  answered 401 and a credential this server did not issue is answered 403. So
  the path names no machine to a caller that cannot already reach it.

- A device carries operator labels. Every device record has a `labels` map, `PUT
  /api/v1/devices/{device_id}/labels` writes the whole set, and
  `GET /api/v1/devices?labels=site=brno` narrows the fleet listing to the
  machines carrying them. A label is the operator's own word for what a machine
  is for, beside the `kind` this server owns, and nothing in the authorization
  graph reads one. Writing takes `manage` on the device; reading takes what
  reading the device takes. The labels survive a restart.

- An operator can let a printer onto this server from the API. `GET
  /api/v1/registrations` lists the machines waiting, each with the code it is
  displaying and what it said it is. `POST /api/v1/registrations/{code}/approve`
  lets one in, so its next poll carries the credential it dials with. `DELETE
  /api/v1/registrations/{code}` turns one away and revokes what it held. Until
  now the only way to admit a printer was the connector's `auto_register` flag,
  which admits every machine that finds the port. All three take
  `configure_resource` on the `driver` resource kind, and no row carries a
  token.

- `GET /api/v1/events/stats` says how many event streams this server is serving
  right now. It answers the same `subscribers` member the retired server did, so
  a dashboard written against that endpoint reads this one. The number is over
  the whole process and names no printer and no caller, so any authenticated
  request gets the same answer.

- The stored past has an HTTP surface. `GET /api/v1/history/facts` searches the
  events this server kept, by origin, by event type and by time.
  `GET /api/v1/history/samples` reads the measurements the same way. Both hang
  under one device as well, at `/api/v1/devices/{device_id}/history/facts` and
  `/history/samples`. Paging follows an opaque cursor the provider mints. The
  answer is narrowed to what the caller may read, from the authorization graph
  the event stream narrows by. So the past of a printer a caller cannot see now
  is not readable either. A deployment that configures no history provider
  answers `501` with `core.resource/HISTORY_NOT_CONFIGURED`. An empty page would
  say nothing happened, and what is true is that nothing is kept.

- `make demo` now keeps history, in a SQLite file beside the configuration it
  writes. The four paths above answer on it, so somebody running the demo can
  search what the three simulated printers did.

- `[sdk]` `event.ForwardBytes` hands the next node the bytes that arrived, with
  the hop count and any remaining command time rewritten in place. Decoding an
  envelope into `event.Envelope` and marshalling it back silently drops every
  attribute the build predates, a newer node's or another author's. A forwarding
  path uses this, and keeps `Envelope` for routing.

- `/dev` is now a developer console rather than one page of printers. It covers
  five areas: the fleet, the event store, users and access, configuration, and
  the first run of a fresh install. It reads and writes nothing but the
  published `/api/v1` operations. Every request it makes is on a log at the foot
  of the page, with the server's own refusal beside it. So an engineer can
  exercise a feature without reaching for a terminal.

- The web interface opens on the fleet, listing every printer the caller may see
  with its model, its state and whether the server can reach it. It reads
  `GET /api/v1/devices` once and then follows `GET /api/v1/events`, so a printer
  going away or coming back moves on screen without a refresh.

- The web interface has a login. A server configured with an identity provider
  shows a sign-in screen instead of an empty table. It takes a username and a
  password where the provider accepts one, and sends the browser to the issuer
  where it does not. A session that has ended says so and offers the login
  again. A server
  with no identity provider is offered no login, because every request there is
  served as one subject: `make demo` still opens straight on the fleet.

- Cutting a release refuses while the changelog still files that release's news
  as pending. `scripts/release.sh` bumped the version and tagged without opening
  `docs/CHANGELOG.md`, so a tag could ship with its own entries under
  `## Unreleased`. It also refuses a newest version heading that is not the
  version being tagged.

- `make vuln-check` reports a known-vulnerable symbol reachable from either
  module's own code. The security scan in CI reads the root module alone, so
  `sdk/` had never been scanned. Needs `govulncheck` and network.

- A second printer dialect: `moonraker`, which reaches a Klipper machine through
  Moonraker. It is the first dialect in the shipping binary the server dials, so
  `make run` plus one address now produces a printer. Voron, Rat Rig and most
  converted Enders are Klipper machines.

  Declare it under `core.connectors` and give it an address:

      core:
        connectors:
          klipper:
            type: moonraker

      curl -X PUT -H 'If-None-Match: *' -H 'Content-Type: application/json' \
        -d '{"connector":"klipper","address":"voron.local","enabled":true}' \
        http://localhost:8080/api/v1/connector-endpoints/bench

  The machine is then listed at `/api/v1/devices` with its temperatures, its
  position, its progress and its state. It takes `core.command.pause_print`,
  `core.command.resume_print`, `core.command.stop_print`,
  `core.command.set_nozzle_temperature` and `core.command.set_heatbed_temperature`.
  What it offers is what the machine itself reports. A machine with no
  `pause_resume` section is offered no pause, and each heater's ceiling is that
  machine's own `max_temp`.

  Klipper reports no vendor, model or serial number. The endpoint options
  `vendor`, `model`, `serial` and `unit_uid` are where an operator writes them. A
  machine that names none is shown as `Klipper` and its Klipper hostname.

  Sending a file and starting a print are not built for this dialect. No Klipper
  machine has been reached with it yet, because every test runs against a fake
  written from Moonraker's published API.

- `make demo` starts this server with three simulated printers already connected,
  so somebody can see it working on a machine with no hardware. It needs Go and
  nothing else: no Python, no configuration file, no certificate, no account and
  no second terminal. One process holds the server and the fleet, so one Ctrl+C
  stops both, and it writes its database to a temporary directory it removes on
  the way out.

  The printers behave like printers. Each one registers over the xBuddy
  WebSocket protocol and reports telemetry. One is sent
  `core.command.start_print` and its progress climbs. One disconnects every
  minute and returns fifteen seconds later. The command prints the addresses to
  open once the fleet has arrived, and says that they only answer while it keeps
  running. `-printers`, `-addr`, `-flap`, `-log-level` and `-dir` change what it
  does.

- `[sdk]` A driver author can now read how much of a value survives.
  `connectorapi.RetainedTextFloor` is the number of bytes a host promises to
  keep of one machine reason, one state path, or the text inside one state
  value. It is a floor and not a limit. Nothing in the SDK checks it, no driver
  is refused for sending more, and a host cuts and marks the rest.
  The doc comments on `Result.MachineReason`, `Result.Raw`, `StatePatch.Path`
  and `StatePatch.Value` each say what happens to a longer value.

- A printer can no longer decide how much memory this server spends on it. A
  refusal reason, a dialog title or any other text a printer chooses is kept to
  32,768 bytes. A longer one is cut on a character boundary and marked with `…`.
  The whole of one printer's state tree is kept to 256 KiB. Over that, a path the
  printer has not reported before is refused. A path it is already reporting
  still takes new readings, so a machine cannot stop reporting its own state by
  describing itself too fully once.

  Before this, one command could answer a megabyte of text a printer chose, and
  one state frame could put a megabyte in the tree every subscriber reads. Four
  hundred printers each holding one such value retained 419 MB.

  `core.device_state_budget` raises or lowers the per-printer budget for a fleet
  that needs it. `pcl_printer_state_bounded_total` counts what was cut and what
  was refused, by driver. An operator can then see a printer doing this rather
  than measuring the heap. Nothing a shipping Prusa printer can display comes
  near either bound: the widest string in the eight firmware translation
  catalogues is 444 bytes.

- This server can now reach a printer that will not dial it. A Moonraker host, a
  Bambu printer in LAN mode and a serial port all listen and wait. A server that
  could only be dialled into could not see any of them. A connector whose
  dialect implements the outbound half is now given the addresses to open, and
  the printer it reaches is admitted and commanded like one that dialled in.

  The address list is the server's own, in the machine-owned overlay beside the
  resource instances. Nothing is typed under `core.connectors`, and adding an
  address restarts nothing. Reading the list takes `view_resource` on the driver
  kind and changing it takes `configure_resource`. A dial that fails is tried
  again with full jitter, from one second to a minute, with no limit on the
  attempts. The wait resets when a session is taken, and a session that ends is
  dialled again.

  The addresses are served at `/api/v1/connector-endpoints`. A printer is added
  with one PUT naming the connector and the address, listed with a GET, switched
  off by writing it again, and taken away with a DELETE. A write quotes the
  configuration's ETag, exactly as a resource instance does. The dial loop reads
  an accepted write and dials the address, with no restart.

- Keys under a module's own namespace now come back in the same order on both
  database engines. A deployment on PostgreSQL created with a language
  collation was listing them in that language's order, which puts `ab` before
  `A/b` and ignores where the separators fall. Every SQLite deployment listed
  them by bytes. The two key columns are pinned to byte order, so a module that
  pages through its own keys reads one answer wherever it runs.

- A module can publish an event that any subscriber can receive. A plugin
  declares which events it may emit in the operator's configuration, and the
  server writes the namespace itself, so a plugin cannot name a core event or
  publish into another plugin's namespace. A plugin with no declared events
  cannot emit at all.

- This server can keep a history of what its printers did. It is off by
  default. When on, it stores numeric measurements and durable events, and both
  survive a restart. A window the server did not see whole is recorded as
  incomplete rather than left as a silent hole. Nothing expires yet.

- `[sdk]` A service module can say that something happened. `sdk/busapi` carries
  one call, `bus.emit`, and a module names an endpoint and a payload on it. The
  operator names the endpoints under `service.events` in the instance's
  declaration, and the host publishes each one as its own event type: the
  module's namespace, then the name the operator wrote. A subscriber reads the
  frame on `GET /api/v1/events` like any other, and sees it if it may see the
  instance that emitted it.

  An instance whose declaration carries no `service.events` may publish nothing,
  and its calls are answered as a method the host does not serve. One payload is
  8 KiB, one instance publishes one event at a time, and eight emissions in
  flight is the limit. `docs/src/content/docs/writing-a-module.md` walks the
  whole path against a running server.

- A history store, off by default. Set `core.history.enabled: true` and the
  server keeps what it used to only broadcast: every measurement a printer
  reports becomes a sample named by its state path, and every durable event
  becomes a fact stored whole. Both survive a restart. A deployment that leaves
  the key alone opens no file, runs no migration and writes no row.

  On SQLite the store is a database of its own at `core.history.database`,
  never the authorization graph's file, because SQLite allows one writer per
  file and a flood of samples beside the graph made small authorization writes
  wait up to five seconds. On PostgreSQL the tables live in the database
  `persistence.postgres.dsn` names.

  Two limits worth knowing before turning it on. Nothing expires yet, and at 400
  printers reporting six values a second the store grows by about 8 GB a day. And
  a value that is not a number is not kept: a printer's state machine reports
  `PRINTING` as text, so the store can say when a job started and finished and
  cannot say what the machine was doing in between.

- [sdk] `sdk/historyapi` is the contract a history provider implements: `Caps`,
  `AppendSamples`, `AppendFacts`, `QuerySamples` and `QueryFacts`, over a
  `Sample` and a `Fact`. Five capability bits say which halves a provider
  serves, and a method whose bit is clear answers `ErrUnsupported`. An
  implementation reads one attribute of an event, its class, to know which
  append verb it reaches, so an event type the provider has never heard of is
  stored like any other.

- `local` and `temporary` join `filesystem` as storage types a `resources` block
  can declare. All three keep files in a directory on the server's own disk.
  `local` and `temporary` take a `path`, an optional display `name` and an
  optional `allowed_extensions` list; `filesystem` takes a `root` and the same
  list. Each instance is browsable and writable through `/api/v1/storages`.

  The published extension reference has documented `local` and `temporary` for
  some time and the binary refused them, because the older server claimed both
  names first. It no longer does.

### Changed
- The Go toolchain is 1.26.8, the newest release older than five days. The
  toolchain gate holds 1.27.1 back for the same reason as 1.27.0: the
  prebuilt linter refuses a tree targeting 1.27.
- [sdk] The module path is `github.com/prusa3d/prusa-connect`. An
  extension importing `github.com/prusa3d/prusa-connect/sdk/...` changes
  the import to `github.com/prusa3d/prusa-connect/sdk/...`. The old
  path no longer resolves, because the repository moved to the prusa3d
  organisation.

- The web interface serves the core at `/`. It served the application built for
  the server that is being retired, which reaches nothing under `/api/v1`. That
  application is still built and now answers at `/legacy/`.

- A printer dialling in no longer recompiles everybody's access to every other
  printer. A fleet coming back after a restart used to appear over tens of
  seconds, because each machine's registration rebuilt the whole permission
  table on the one writer SQLite has. A write now recompiles only what it moved.
  On a 400-printer site one arrival costs 0.38 ms instead of 51 ms, and the
  figure no longer grows with the fleet. Writing or withdrawing a grant on one
  printer costs 5 ms instead of 132. Nothing an operator sets changes, and every
  permission answer is the same answer.

- Listing the fleet costs a fraction of the memory it did. Each row copied the
  printer's whole capability descriptor twice and read a list of names off one
  copy and a single flag off the other. On a 400-printer site that was the
  largest allocation site in the server, 12 per cent of everything it allocated.
  The listing now reads both answers without the copy. Nothing on the wire
  changes: the same page came back byte for byte, and the whole fleet in 11 ms
  where it took 18.

- Two operation ids in the OpenAPI document are named after the device rather
  than the printer. `send-printer-command` is now `send-device-command`, and
  `get-developer-printer-commands` is now `get-developer-device-commands`. A
  generated client renames those two methods and nothing else: the paths, the
  request and the response are unchanged. Sending gcode keeps its printer name,
  because gcode runs on a printer and on nothing else.

- **The binary now runs the current server when no subcommand is given.** It
  used to run the retired one. Read this before upgrading: a deployment that
  starts `prusa-connect` with no word after it gets a different server on
  the next start.

  What answers changes. The current server publishes `/api/v1/...`,
  `/openapi.json` and the API reference at `/docs`. Every path the retired
  server published under `/api/`, `/auth/`, `/setup` and `/p/auth` now answers
  404. Its OpenAPI document declares 64 operations. Of those, 38 are served
  under a new name and 26 are served nowhere. The 26 are labels, jobs, API
  tokens, the MQTT token, registration approval, event statistics, command
  history by id, the transfer listing, printing straight from a storage, and
  deleting a printer.

  What is read changes. Only the `core`, `resources` and `persistence` blocks
  of `config/config.yaml` are read. Against the file this repository ships that
  leaves ten blocks unread, and each one is named in a warning at startup, so
  the log says what stopped running. `./data/state.db`, `./data/commands.db` and
  `./data/auth.db` are not opened, so accounts, jobs, labels and command history
  are not migrated. Create the first login again with
  `POST /api/v1/auth/setup`.

  What is listening changes. There is one listener, `core.api.addr`, and
  printers dial it. The MQTT broker on 1883 and 1884 is gone, and so is the
  printer-facing listener on port 80. A printer configured against port 80 is
  repointed, or the deployment gives it a socket of its own with
  `core.printer.addr`. A subscriber written against the retired server's MQTT
  topics reads nothing until it is repointed at the topics the mqtt sink
  publishes. The 17 OpenTelemetry metric names that server exported are gone and
  nothing aliases them; `/metrics` serves the `pcl_*` names instead. `/docs` is
  the API reference and no longer the documentation site, which is published on
  a host of its own.

  The retired server is still reachable by name. `prusa-connect serve`
  runs it, `serve admin create` is where the account bootstrap went, and the
  word prints a notice saying it is deprecated. It is removed in release 0.5.0,
  three minor releases after this one, and the container image and the
  Kubernetes manifests already name `core` and are unaffected.

- Publishing an event no longer allocates to check its type. The check split
  the name into a slice and searched a freshly built list of every core type,
  once per event. Both are gone and neither answer changed: 185 ns and 224
  bytes before, 63 ns and none after. That is about a fifth off what one
  printer measurement costs this server. A 400 printer site therefore spends
  less of its processor on validation, and gives its collector less to do.

- A printer dialling in now costs one authorization write instead of two.
  Registering the machine and placing it in its org were separate writes, and
  each one recompiles the whole permission projection. On a 400 printer
  deployment an arrival measured 133 to 159 ms before and 64 to 70 ms after. A
  fleet coming back after a restart is therefore visible in about half the
  time. The window between the two writes is gone as well: in it the printer
  was registered, in no org, and visible to nobody.

- The extension SDK no longer depends on anything outside the standard library.
  An extension author adds `sdk/` and nothing else. So an extension cannot
  inherit a version conflict from the server, and cannot be told to upgrade a
  library it never asked for. The legacy plugin contract that carried
  the last of those dependencies is gone; nothing outside `legacy/` used it. An
  import gate refuses the next one.

- [sdk] A module's calls into the host's record store are now bounded, and are
  refused before the module is ready. Two refusals join `sdk/stateapi`.
  `ErrNotReady` answers a call made while the host is still starting the
  module. `ErrBusy` answers a call made when the module already has as many
  state calls running as the host will run at once. Both name the instance, and
  `ErrBusy` names the width.

  A module that reads a record during its own start-up now gets `ErrNotReady`
  there, and succeeds once the instance is serving. That covers a call from a
  `plugin.info` handler and from a goroutine beside one. A module that fans a
  job out over its records retries the calls that come back `ErrBusy`. Neither
  refusal reaches the store, so nothing is half written.

- One grant on an organisation now reaches every printer in it. The grant
  surface accepts `org` beside `device` and `resource_kind`, so a fleet-wide
  tuple can be written and withdrawn over HTTP. Administering an organisation
  means holding `admin` on it; no new permission was added.

  Writing the `admin` role itself is refused for now. Inside an organisation it
  confers nothing its writer already lacks, and outside it would open a path for
  an administrator of one organisation to seize a printer in another.

- `make run` builds and starts the core with one command. It compiles the Go
  binary alone, with no swagger step, no pnpm, no docs site and no C compiler,
  and runs it in the foreground. Bare `make` now prints help instead of running
  the whole pre-commit suite.

  `make up` still starts the shipping server, and so does `make build && make
  up`. Use `make run` for the core, or
  `docker compose -f docker-compose.local.yml up -d prusa-connect`.

- A module serves its own HTTP routes, behind the same authorization as the
  rest of the API. A module writes ordinary paths and the host serves them under
  `/api/v1/modules/<namespace>/`. The namespace comes from the host, not from
  the module, and a second module claiming one is refused before its process
  starts.

  A caller needs `view_resource` on the module's instance to read and
  `operate_resource` to do anything else, granted the ordinary way. A module
  opens no socket of its own, so the server is the only door to it.

- One grant feeds an event sink every printer in the fleet, including ones
  admitted later. Writing `org:<your org>#viewer@sink:<name>` covers every
  printer in the organisation. A machine that dials in next week is covered the
  moment it is admitted, because the code that admits a device is the code that
  writes its organisation edge.

  A sink is also its own principal now. It used to be a service account of the
  same name, so a grant written for an integration nobody had built yet went to
  a webhook that was already running. Nothing else changes for an operator: a
  sink is granted where a service account is granted, and at viewer only.

- An event sink reads the bus as `sink:<name>` and no longer as
  `service_account:<name>`. The two were one principal holding one set of
  grants. A grant written for a service account somebody had named `audit` fed
  a webhook sink named `audit`. Every grant written to feed that sink was a
  grant the service account's credential would authenticate into. A
  deployment that granted a sink through `service_account:<name>` rewrites those
  grants under `sink:<name>`. The authorization vocabulary moves to `3.4.0`.

### Fixed

- A withdrawn grant could keep working on an open event stream. The server
  caches the generation of the authorization graph, and every subscriber's
  decision to re-read its permissions is taken off that number. Two writers
  finishing at the same moment could publish their generations in the wrong
  order, which left the cache one generation behind until somebody wrote to the
  graph again. The cache never moves backwards now.

- On PostgreSQL, two people withdrawing each other's grant at the same moment
  could leave an object nobody administers. Nothing could then put an
  administrator back. The rule that refuses a grant leaving the caller unable to
  administer an object read the graph before the two writers were serialised
  against each other. So each of them read a graph without the other's
  withdrawal in it, and both were allowed. Measured at 23 of 25 attempts. They
  are serialised from the start of the transaction now. SQLite was never
  affected, for a reason in the connection string rather than in the rule.

- `serve` sends a browser at `/` to `/legacy/`, where the retired server's own
  application is. `/` served the page that belongs to the current server, and
  that page calls versioned paths the retired server does not answer, so it
  loaded and then showed a refusal.

- A refused device name and a supervised resource's failure message are cut on
  a character boundary and marked with an ellipsis. Both were cut at a byte.
  A name or a driver error written in a language with multi-byte characters
  arrived carrying a replacement character where its last letter had been.

- A command whose caller has already given up is no longer sent to a Buddy
  printer. A cancelled request could still reach the machine and be reported as
  carried out, because the driver only read the cancellation while waiting for
  the answer. It reads it before anything goes on the wire.

- The MQTT sink's published reference now names the value its
  `default_action` option actually takes. It said the default was `publish`
  and that the choice was `publish` or `drop`. The sink accepts `forward` or
  `drop`. An operator who copied `publish` out of the reference got the
  instance refused at load, with a message naming two values the page never
  mentioned. Only the description changed, and no configuration behaves
  differently.

- Every printer of a fleet that dials in at once now announces itself to a
  reader who was already watching, and to the history store. Six simulated
  printers admitted inside two milliseconds put one
  `com.prusa.connect.printer.arrived` row in the store. Five browsers were left
  with a fleet listing that only a reload would correct. Admitting a printer
  moves the authorization graph a generation, so a site powering up moves it
  once per machine. What a reader was handed of a newly visible printer was
  bounded by the graph as it then stood, rather than by the write that made that
  printer visible. The bound is now per printer. A reconnecting reader is no
  longer told its place is gone because somebody else's grant changed while it
  was away. That used to discard any command result or audit record it had
  missed.

- A printer reporting an alpha firmware now matches the profile band an alpha
  belongs in. Buddy firmware spells a candidate build two ways, `6.2.0.alpha`
  and `6.4.0-alpha`, and the profile matcher followed only the second. It
  ordered `6.5.0.alpha` with the finished `6.5.0`. A firmware band ending at a
  release would then have offered an alpha build what that release added. Every
  shipped band is open at the top and starts below the floor, so no printer in
  the field matched differently. An operator writing a band with a ceiling would
  have met it on the first alpha. A fourth number, as in `6.4.0.1`, counts builds
  of a release rather than naming a candidate for one, and is still dropped.

- A scrape of a loaded server now describes the fleet. `/metrics` carried two
  families during a 400-printer run and both were about log lines, while the
  documented example promised `pcl_printer_up` with a printer id in it.

  The same run now answers eleven. Each printer reports whether it is up, what
  it is, when it last sent a frame and when it connected. Each driver reports
  how many of its printers are in each state, and how many it has offered. The
  process reports what build it is, what it has published on the bus, and how
  long it took over each HTTP request by route template. Commands, reconnections
  and the supervised-resource families answer once something has happened to
  report.

- A reader losing telemetry is no longer silent about it. The server counted the
  dropped frames and nothing read the count, so an operator watching a
  400-printer fleet saw a stream that looked healthy while most of it was gone.
  The server now writes a warning naming the reader, how many frames it lost,
  what its queue was holding and the bound that decided. It is written at most
  once every thirty seconds for one reader, because a queue over its bound is
  over it five times a second.

- A reader watching a whole fleet is given all of its telemetry. The per-reader
  buffer was 512 KiB, sized against a telemetry frame of about 200 bytes. The
  frame this server sends is 406 bytes. A 400-printer site reports 6,000 values
  in one window, so the buffer held about a fifth of that and dropped the rest.
  It is now 4 MiB, which covers the fleet this product claims.

- A server told to use PostgreSQL no longer warns at startup that it is ignoring
  the block that says so. `persistence:` is read by every build, and the list of
  blocks the core names as unread had left it out. The first line an operator
  saw was a warning about a setting the server was in fact acting on.

- The USB printer settings file this repository generates now names the port the
  server actually binds. `scripts/generate_printer_config.sh` read two keys from
  the retired server's part of the configuration, which this build does not
  decode. A
  printer given that file dialled a socket nobody was holding and said nothing
  about it. The script reads `core.printer.addr` and falls back to
  `core.api.addr`, which is where printers dial when the first is empty. It
  refuses to write a file at all when neither key names a port.

- A printer is matched to its capability profile on what it is, not on how its
  firmware spelled itself. Case, spaces, dots, hyphens and underscores are
  ignored, so `Core One` and `core-one` reach the `CoreOne` profile. The device
  type code reaches it too. A Core One reporting `7.1.0` now resolves 88
  capabilities. The coverage tier had offered it all 102, and the printer
  refused the extras itself. A spelling no rule reaches is one row in
  `internal/printer/profile/aliases.json`. The log names the reported spelling
  beside the profile it resolved to.

- A sink that cannot receive events says so instead of dropping them silently.
  A `runtime: service` declaration under `sink` is a module: it serves routes
  and keeps records, and is handed no events. The server now says so once and
  names what to write instead.

- A malformed key or a stripped nonce can no longer weaken a login. Four
  defects in the OpenID Connect path are closed: an unbounded key exponent, an
  unthrottled key refetch, held keys being replaced by an empty set, and a nonce
  check that was skipped when the nonce was absent.

- The grant example in the shipped `config/config.yaml` named the wrong object
  type and the wrong principal, so an operator who followed it wrote a grant
  nothing read and got a sink that received nothing, with no error anywhere. It
  now reads `POST /api/v1/grants/device/{printer}` with
  `{"relation": "viewer", "subject": "sink:audit"}`.

- A sink declared `runtime: service` is now left off the event bus with one
  warning naming the instance and the runtime to write instead, rather than
  being subscribed and failing on every event in silence. No contract reaches a
  child process yet, so such a sink never received an event; it reported ready
  throughout and nothing said otherwise.

### Added

- `GET /api/v1/state/namespaces` reports what each module is keeping: the
  namespace, how many records it holds and how many bytes those records count
  toward the 8 MiB one namespace may hold. No stored value is returned.

  A caller sees a namespace if they hold `view_resource` on the module instance
  that claims it, which is the permission that module's own routes already take.

- A subscriber is told when a device's record changes. A new event,
  `com.prusa.connect.device.record.changed`, carries the stored row and names
  which fields moved. It is published to the device's own origin, so it reaches
  exactly the readers who may already see that device.

  It fires only when one of eight reported fields moves, not on every telemetry
  frame, so a fleet at rest produces none. The state tree is not carried: every
  path in it already ships as telemetry.

- A restart no longer forgets the printers this server was serving. Every device
  keeps a record of its driver, identity, firmware, last state and why its last
  session ended. The fleet is listed the moment the server comes back, rather
  than one machine at a time as each dials in.

  A restored device is shown as away, never as reachable. The record holds no
  field for reachability at all, so nothing a restart reads can claim a machine
  is on the wire. A start after a crash says in the log how many sessions the
  last run left open. A command sent to a restored device is refused until it
  connects.

- One grant now feeds a sink the whole fleet. `org:local#viewer@sink:audit`
  covers every printer in the org, including one that dials in tomorrow. The org
  edge is written by the same code that admits a device. `sink` is the one
  non-human type an org role accepts, and it is accepted at viewer alone. A
  service account and a peer node stay out of every org role. A credential must
  not become fleet-wide by accident, and nothing can authenticate as a sink. The
  grant surface does not yet administer `org`, so this tuple has no
  operator-facing route. That is tracked on EP-04.

### Changed

- A resource type outside the core namespace is refused if it asks to run
  inside the server. A module runs in its own process, under
  `runtime: service`. A namespaced type declared `runtime: builtin` is now
  refused at three doors: the operator's file, the machine-owned overlay and
  the resource API. Each names the runtime to write instead.

  A deployment that declared such a type sees that one instance refused on
  upgrade, and the rest of the file served. It used to reach an unknown-type
  error at first construction, naming neither the key nor the runtime. See
  ADR-0034 rule 5.

### Changed

- A module runs in a process of its own and never inside this binary. An
  instance whose type carries a namespace the core does not hold is refused
  when it is declared as `builtin`, which is the default. The same refusal
  covers the operator's file, the machine-owned overlay and
  `PUT /api/v1/resources/{kind}/{name}`. It names the instance and
  nothing else: the process keeps serving and every other instance starts. A
  deployment that declared such a type was running an instance that resolved to
  nothing, because no build of this binary contains one. Declare it with
  `runtime: service` and the `service` block naming the artefact it ships.

- The API serves devices at `/api/v1/devices`. Every path under
  `/api/v1/printers` moves with it and `{printer}` becomes `{device_id}`, so a
  client reading the fleet, a printer's files or its cameras changes the path it
  calls. The file listing answers `items` where it answered `entries`. The
  printer layer keeps its names. A command is still a `PrinterCommand`, the
  verbs are still `core.command.*`, and the gcode schemas are unchanged. Those
  are about the machine rather than the object the server authorizes. The
  compatibility gate reports the move rather than refusing it, which is what
  `v1` at `reporting` is for.

- MQTT topics name devices. A subscriber matching `printer` in a topic segment
  changes the filter it subscribes with. The published extension reference is
  regenerated, so the topics it lists are the topics this build sends.

- A camera is a device, and whoever owns a printer can flash it. The
  authorization graph names its object type `device`, and the two tables behind
  it are renamed with it. A binary older than this one refuses a migrated
  database rather than reading half of it. Ownership is a relation rather than a
  role. It adds firmware and the command actions to whatever a principal already
  holds, stops short of administering grants, and is cut by a block.

- A camera that belongs to no printer can be granted and watched. A camera used
  to be reached through the printers a caller could already see, so a camera on
  its own reached nobody. It now holds a row of its own. A camera bound to a
  printer still inherits from it and gains nothing it did not have.

### Added

- `[sdk]` The `sdk` module gets a version of its own. `sdk/VERSION` reads
  `0.1.0` and `scripts/release.sh` cuts `sdk/vX.Y.Z` beside the application's
  `vX.Y.Z`, so an extension author writes a version in a `require` line instead
  of a pseudo-version over a commit hash. Go resolves a module in a subdirectory
  by a tag carrying that subdirectory as a prefix, which is why the tag reads
  `sdk/v0.1.0`. A release where nothing under `sdk/` moved cuts no sdk tag. The
  module stays below `v1` while `sdk/routesapi` exposes `gin.IRouter`, because a
  `v1` tag would pin this application's web framework into every author's module
  graph for the life of the major. `0.x` still promises no compatibility. See
  ADR-0033 rules 6 and 7.

- A script can publish an event. It emits one rather than only reshaping one on
  its way out. An operator
  who writes `endpoints: {bus: "event:com.example.fleet.observed"}` under a
  `runtime: script` sink gives that script a destination that is this server's
  own event stream. A script that returns `msg("bus", key, body)` puts a fact on
  the same bus every other producer publishes on, so every reader of
  `/api/v1/events` receives it, including a federated peer. The script names the
  endpoint key, the routing key and the body. The operator names the address and
  the event type, so neither of the two things a consumer routes on is the
  script's. A type under `com.prusa.connect.` is refused at start, because that
  namespace is closed and every type in it is a gated published identifier. The
  event carries the ref of the instance that emitted it as its source. A sink is
  never fed an event a sink emitted, so a script cannot trigger itself.
  A script still hears one fact, `printer.state_changed`, so an emission reaches
  a reader of the stream and not another sink. See ADR-0015 rules 26 to 29.

- A browser plays an H.264 camera. `POST /api/v1/cameras/{camera}/webrtc` takes
  an SDP offer as `application/sdp` and answers with one that already carries
  every ICE candidate. One request completes the negotiation, and there is no
  second channel to keep open. The server offers no ICE server of its own, so an
  air-gapped install works with no STUN and no TURN. An operator who needs
  one across a NAT names it. Nothing is decoded or re-encoded: the camera's own
  H.264 is repacketised into RTP, which is what lets a Raspberry Pi serve a live
  view at all. `adapters/camera/rtsp` is the driver that feeds it, for a camera
  that speaks RTSP, which is what the Prusa Buddy3D camera is. It opens one
  session per viewer and closes it when the viewer leaves, and it answers a
  Basic or Digest challenge. An optional `snapshot_url` lets the same camera
  fill the thumbnail grid. A camera that describes H.265 is refused with the
  codec named, because no browser matrix covers it and nothing here converts it.
  The camera listing gains `watch`, which says whether a camera answers this
  path. Every refusal is the refusal every other camera path gives, so a caller
  learns nothing about a camera they may not see. The binary grew by between
  3.5 and 3.9 MiB on every target, and
  `scripts/check-binary-budget.sh` holds the measurement.

- A server with no identity provider can create accounts and sign people in.
  Setting
  `core.auth.local: true` makes this server hold the accounts itself.
  `POST /api/v1/auth/setup` creates the administrator on the first run, and
  `POST /api/v1/auth/login` takes a username and password and sets the session
  cookie. `GET /api/v1/auth/info` says which login this deployment serves and
  whether it has been set up. All three take no credential, because nobody holds
  one before the first run.

  That first account takes the id `core.principal`. The authorization graph's
  first grant was already written for that subject, so the person who runs setup
  is the administrator and no second grant is written anywhere. They add people
  with `POST /api/v1/auth/accounts`. A new account holds no grant until one is
  written for it.

  Passwords are stored with argon2id at the OWASP profile of 19 MiB and two
  passes. The parameters travel inside the hash, so raising them re-hashes on
  the next login rather than locking anybody out. Ten wrong passwords on one
  account inside fifteen minutes refuse every attempt on it until the window
  drains. The window rolls rather than latching, so guessing at somebody cannot
  keep them out. An unknown username, a wrong password and a spent budget are
  one answer and cost the same time. Changing or resetting a password ends every
  session opened under the old one, on every transport.

  `core.auth.local` and `core.auth.issuer` cannot both be set: one deployment
  serves one identity provider. See `docs/src/content/docs/local-accounts.md`.

- `runtime: service` starts the extension it names. The host launches
  `<type>_<GOOS>_<GOARCH>` from the directory under `service.path`. It refuses
  that binary when the bytes do not match `service.checksum`, or when
  `service.contract_major` is not the wire this build serves. The process is
  then supervised like any other resource, so a child that dies is
  restarted and a child that is stopped is asked before it is killed.
  No contract publishes a method set yet, so a
  launched child answers `plugin.info` and `plugin.shutdown` and is asked nothing
  else.

- A fleet listing says which printers need somebody, in one request. Every
  printer on `GET /api/v1/printers` carries an `issues` array, worst first. The
  page carries the count of each reason across every printer the caller may
  see. So a screen loading a fleet already holds what it needs to raise a
  notification, with no call per printer. Six reasons ship. A printer waiting at
  a dialog, one that has gone, one that is not answering, one no capability
  profile covers, and two link faults. Setting `with_issues=true` narrows the
  page to the printers that need attention. It leaves the counts over the whole fleet,
  so the badge does not read zero once the filter is applied. The reasons are a
  closed set published at `GET /api/v1/vocabularies/device-issues`, which
  carries the sentence, the severity and what a person does about each one. A
  printer record carries the token and no English, so a client branches on it
  and a translator translates the vocabulary. Measured over 1, 100 and 400
  printers, the listing costs the same statements it did before.

- `[sdk]` `sdk/wirerpc` is the session an out-of-process extension speaks over
  the plugin wire: streams, JSON-RPC control objects, and a credit balance. It
  publishes the lifecycle method set every child answers, `plugin.info` and
  `plugin.shutdown`, and a write on a starved stream waits for the room the peer
  granted.

- The SSE event reference names `prusa-connect.auth.expired`. The server
  sends that event just before it closes a stream whose session token has
  expired. The published AsyncAPI document described the other ten event types
  and not this one. A client written from the reference had no way to know the
  event it must act on to authenticate again. The document and the generated
  page both carry it now. A divergence test refuses a document that describes a
  different set from the one the server publishes.

- A configuration block this build does not read is named at startup. The
  shipped `config/config.yaml` carries fourteen top-level blocks and the core
  decodes two, `core` and `resources`. The other twelve belong to the server
  that still runs by default, and an operator who configured MQTT or
  persistence against the core got neither an effect nor a warning. Each block
  the core does not read now produces one line at warning level, naming the key
  and the file it came from.

- Every printer record carries a `kind`. It is `printer` on every record this
  server produces. It is there so a client reading a fleet can tell one device
  type from another without a second request. Read it as an open value. A
  federated peer running a newer core may name a kind this build does not
  carry, and a client that meets one shows it rather than refusing the record.

- The API document names the browser session as a credential it accepts. It
  published one scheme, a bearer token, and said in prose that a same-origin
  browser rides a cookie instead. A reader generating a client could not see
  that, and the try-it control in the reference had no way to offer it. The
  document now carries a second scheme naming the session cookie, and the two
  are alternatives: either one authorizes a request, which is what the server
  has always accepted. The cookie's name is read from the login ceremony, so
  the document cannot name one the server does not set.

- Health and the six vocabulary operations publish the status they can fail
  with. They answered OpenAPI's `default` response, which names no status. A
  generated client got an untyped fallback there, where every other operation
  gave it a status to branch on. All seven now publish 500, which is what the
  panic recovery writes and the only failure any of them has.

- A printer row now carries what this caller may do to it, and
  `POST /api/v1/access/check` answers the same question for one object. Every
  printer in `GET /api/v1/printers` gains an `actions` member. It names the
  permissions the caller holds on that printer. Beside it sits an
  `authorization_epoch`, which moves whenever any grant is written. A client
  can now disable a control the caller may not use. It costs no extra request:
  the set comes from the row the listing was already filtered by. The check
  endpoint answers for an object that is not on a list. It always decides
  about the caller, so it needs no permission of its own. A body naming a
  `subject` is refused. An object this server does not have answers exactly as
  one the caller may not see, so this endpoint discloses no printer the
  listing hides. A review answer is a hint for drawing an interface. The
  server checks the permission again when the action is attempted.

- A Kubernetes manifest set under `deploy/kubernetes/` deploys one tenant
  instance. It is a Deployment, a Service, a ConfigMap holding the
  configuration file, and two persistent volume claims, applied with
  `kubectl apply -k deploy/kubernetes/ -n <tenant>`. The ConfigMap is mounted as
  a directory and read through `-config`, so it is not the `subPath` mount
  Kubernetes never updates. The Deployment stays at one replica and replaces
  rather than rolls, because the claims take one writer and the authorization
  graph is a single SQLite file. Nothing has applied it to a real cluster; see
  the Kubernetes page for what is proved and what is not.

- `[sdk]` The wire an out-of-process extension speaks is published as a
  specification, so an author in Python, Rust or TypeScript can implement it
  without reading our Go. The page at `/docs/plugin-wire-protocol` states the
  frame layout, the five frame kinds, the stream rules, the sizes a reader
  refuses on, credit, the handshake line and version negotiation.
  Beside it, `docs/wire/plugin-wire-protocol.json` carries 39 conformance
  vectors as the exact bytes an implementation is held to. A Python
  implementation in `examples/wire-protocol`, written from the page alone,
  passes every one of them. The Go implementation of the same page is
  `sdk/wire`, and where the two disagree the page is right. No host speaks
  this wire yet: an instance declared as `runtime: service` is still built in
  process.

- The API reference at `/docs` opens on a description of what the server is for.
  Its operations are listed in named groups, each with a description, and a
  request sent from the page reaches the origin the page was loaded from. Every
  operation that answers with a body now shows a worked example of it, and one
  that takes a body shows what to send. A reader can therefore try an operation
  without writing the request from the schema first. The same content reaches a
  generated client through `/openapi.json`, as `info.description`, `tags`,
  `servers` and an `example` on each operation.

- `prusa-connect core healthcheck` asks a core already running on this
  host whether it is ready, and exits 0 when it is. It reads the same
  configuration file the server reads, so a moved address or a configured
  certificate needs no second place to update. It is what the container
  image runs, so the image carries no curl.

- `POST /api/v1/resources/{kind}/{name}/restart` restarts one supervised
  instance. It is what the `operate_resource` permission was declared for, and
  that permission had no endpoint at all. An operator could read a failed sink,
  and could delete it with a manager grant, and had no way to retry it. The
  instance is stopped and built again from the declaration in force. A caller
  who may restart one is still not a caller who may rewrite the connection
  string inside it. Nothing is written, so there is no ETag to quote and none
  comes back, and the answer is 204. An instance the supervision plane is not
  running answers 409 `core.validation/RESOURCE_NOT_RUNNING`. A disabled
  instance is that case, because starting it is a write.

- The core can run its database on PostgreSQL. Set `persistence.type: postgres`
  and put a DSN in `persistence.postgres.dsn`. The authorization graph then lives
  on Postgres, and `make build` links the driver from the same key. A binary
  built for SQLite refuses to start on a DSN and names the build tag it needs.
  The value `memory` is refused outright, because the core has no in-memory
  engine.


- Reading and writing the contents of a storage take two new authorization
  actions, `read_storage` and `write_storage`, separate from the three that
  cover an instance's declaration. `read_storage` browses a storage instance and
  downloads a file from it, and `write_storage` uploads, makes folders, renames
  and deletes.
  A viewer on the kind gets the first and an operator the second, so nobody has
  to be made a manager of the `storage` kind, which is the grant that repoints
  and deletes instances, to be allowed to touch the files inside one.
- A file held by one of this deployment's storage resources can be sent to a
  printer, with `POST /api/v1/printers/{printer}/files` naming the storage, the
  file inside it and where it lands on the printer. It returns when the printer
  reports the file arrived, so the client's timeout bounds the transfer. The
  request names a file rather than carrying one: the printer fetches the bytes
  over its own connection, and nothing on this path grows with the file's size.
- The server's own storage is browsable and writable over the API, with `GET
  /api/v1/storages` and browse, upload, download, delete, mkdir and move under
  one storage. The listing reports what an operator declared and what each
  one's type can do; under a storage, `GET .../files` browses, `PUT
  .../content?path=` uploads,
  `GET .../content?path=` downloads with `Range` support so an interrupted
  download resumes, `DELETE .../files?path=` removes, `POST .../folders` makes a
  folder and `POST .../moves` renames. A file is streamed in both directions and
  never held, and an upload is bounded by `core.api.max_upload_bytes` rather than
  by the 1 MiB bound on a document body.
- A path carrying a `..` segment is refused by the storage API rather than
  resolved. It used to be cleaned into the storage instead, so a caller asking
  for `../../etc/passwd` silently got `etc/passwd` written: nothing escaped, and
  nothing said the path had changed either.
- The built-in `filesystem` storage takes an `allowed_extensions` list. Omitting
  the key takes the built-in list of print-file and image extensions, and an
  explicit empty list permits any extension, which is what `config.yaml` has
  documented all along.
- A printer's own storage can be managed through the core: `POST
  /api/v1/printers/{printer}/folders` makes a folder, `DELETE
  .../folders?path=` removes one and `DELETE .../files?path=` removes a file. A
  delete aimed at the wrong kind of entry is refused with
  `core.printer/FILE_CHANGE_REFUSED`, carrying both the printer's machine reason
  to branch on and its human sentence to show.
- `[sdk]` `connectorapi.Transfers` and `connectorapi.FileManager` are the halves
  of a session that write a file and change a device's storage, probed with
  `connectorapi.Uploads` and `connectorapi.Manages` the way `Filesystem` is
  probed with `Files`. A driver implements the ones it can serve and the rest are
  refused with `core.printer/CAPABILITY_NOT_SUPPORTED`.
- `[sdk]` The file vocabulary the core defines is named for what an operation
  does rather than for one dialect's wire command, and a driver maps it onto its
  own ([ADR-0021](decisions/0021-the-file-vocabulary-is-dialect-neutral.md)).
  Sending a file is `core.feature.file_upload`; the four xBuddy transfer commands
  are no longer in `sdk/connectorapi` at all. A connector for other hardware
  implements the contract without declaring a Prusa command name.
- `[sdk]` `connectorapi.Spec` carries a `Logger`. A driver built from
  configuration previously had no logger at all, so nothing it did could be
  seen: a connector now logs through the process logger, at the level and format
  the operator configured, tagged with the connector's name.
- The built-in `filesystem` storage type can open a file for reading, confined
  to its own root. It could previously only create the directory, so a storage
  declared in the `resources` block held files nothing could reach.

### Removed

- The `core` command serves no web page at `/`. A person who opens the server in
  a browser now gets a problem document there rather than the old frontend, and
  the developer view moves to `/dev`. That frontend called 49 paths this server
  does not serve, so every one of its requests answered 404 and the page loaded
  and stayed empty. The product frontend is being reworked and does not ship in
  this binary. What the binary serves is the versioned API under `/api/v1`, the
  OpenAPI document at `/openapi.json`, the API reference at `/docs` and the
  developer view at `/dev`. Every shipping binary is smaller for it: 1.9 MB on
  amd64 and arm, and 2.0 MB on arm64.

- `make generate-sse-types` and `make generate-mqtt-types` are gone, along with
  the two commands behind them. Neither kept anything in sync. The MQTT one had
  not been able to write its output for some time, and emptied
  `frontend/src/generated/mqtt-events.ts` on every run before failing. The
  SSE one wrote a copy of its document to a path nothing publishes. The two
  AsyncAPI documents the site renders are unchanged and are now fixed records of
  what the shipping server publishes rather than files a build step rewrites.

- `LOG_LEVEL` is gone from both compose files and from `.env.example`. Nothing
  in this repository read it, so setting it moved no log level and printed no
  message saying why. Set `core.log.level` in `config/config.yaml`, or pass
  `-log-level` to the `core` command.

- The `state_cache` block and the `queue.type` key are gone from the shipped
  configuration, and no build tag is derived from either any more. One key,
  `persistence.type`, now names the engine for the whole database. The
  authorization graph, the device records and the command queue can no longer be
  split across two engines by one file. A configuration that still carries the
  removed keys loads, and no build reads them. A deployment that ran the
  Dragonfly cache has to pass the `dragonflycache` tag itself until that code is
  deleted. The example profile that named it is gone for the same reason.

### Security

- A run of failed logins now slows down the next one, counted per account and
  per client address. `POST /api/v1/auth/login` waits before it checks the
  password. Three failed attempts on one account and twelve from one address are
  free. Each attempt after that doubles the wait, up to five seconds. A correct
  password clears both counts and is never refused. Somebody who knows your
  username can make you wait, and cannot keep you out. Behind a reverse proxy,
  set the new `core.trusted_proxies` key to the networks the proxy reaches this
  server from. Without it every request arrives from the proxy's own address, so
  one person getting a password wrong slows everybody else down.

- Somebody guessing at a named account can no longer hold its owner out of this
  server. Ten wrong passwords on one account used to refuse every further
  attempt on it for fifteen minutes, the owner's correct password included, and
  refilling that count was ten requests anybody could send, so knowing a
  username was enough to keep a person off their own printers for as long as
  the guessing went on. The password is now checked before the failure count is
  read, so a correct one is always admitted and clears the count, and a wrong
  one is still refused. What bounds guessing is the price of a guess: one 19 MiB
  argon2id pass, with no more than four running at once. The login itself is
  handed a username and a password and nothing else, so a count kept there
  cannot tell an account's owner from whoever is guessing at them. The limit
  that counts attempts sits in front of it, on the login endpoint.

- A camera cannot spend the host's memory on an access unit that never ends. The
  WebRTC remux bounded one NAL unit and nothing bounded the buffer that holds
  units until a picture boundary arrives, and where that boundary falls is a
  decision the device writes into the stream: a camera that sends no slice, or
  only slices whose `first_mb_in_slice` is non-zero, is framing H.264 legally and
  never ends a picture. Thirty-two megabytes of that input held thirty-one, per
  viewer. An access unit now has the same four megabyte ceiling one unit has,
  which is two orders of margin over a 1080p keyframe. A picture that passes it
  is dropped whole and the stream rejoins at the next boundary, so a device that
  glitches costs a viewer a freeze until the next keyframe rather than the view;
  nothing partial is ever emitted as a whole picture, and nothing at all is
  emitted until a keyframe the viewer received. Reaching this needed a caller who
  may already watch the camera and a hostile or compromised device, which is the
  stated threat model for cameras.

- A password written into a camera URL is lifted out of it, whichever URL it is.
  `config.url` had its userinfo taken into the credential closure because a
  string field holding a password is printed in full by every `%v` that reaches
  the struct; `config.snapshot_url` was checked for its scheme and host and kept
  verbatim, so `http://user:pass@camera/snapshot.jpg`, which is how a camera with
  an HTTP still endpoint documents itself, stored the password a second time. The
  rule is now the type rather than the field: every URL an operator writes goes
  through one function that refuses the scheme and returns the URL with the
  userinfo already gone. The `mjpeg` driver's `snapshot_url` and `stream_url` are
  fixed the same way. A credential in a URL still authenticates the camera, and
  what the `username` and `password` options name wins over it.

- An extension may not declare an authorization permission on a core object. A
  declaration naming `device`, or naming one of this server's own resource
  kinds, is refused. An extension declares on a resource kind it contributes
  instead, which is `resource_kind:<namespace>/<name>`, and the namespace is the
  one this host derives from the declaring module. An extension has no object of
  its own in the graph. So without the refusal a driver written for one printer
  model would hand its permission to every device manager in the fleet, on
  machines it never touches. A contributed kind is an authorization
  object and not a resource kind. Nothing configures, constructs or supervises
  one, so the set of kinds this server holds configuration for does not move and
  no operator sees a new `resources:` block. Nothing declares a permission yet,
  so no deployment's answers change today. See ADR-0018 rules 14 to 17.

- `GET /api/v1/gcode-presets` sends no `ETag` when the caller may read no
  preset. The token versions the whole overlay, and every caller received it.
  Polling this route therefore reported each write an operator made, to somebody
  holding no grant on presets. A caller who may read a preset receives the token
  as before, and a write still fences with it. The resource listing already
  works this way.

- A camera this server does not supervise and a camera bound to a printer the
  caller may not see now cost the same authorization query. Both refusals were
  already the same bytes. What separated them was the order. The media path
  decided which camera exists before it asked who may see it. A name nothing
  supervises was therefore refused without a database read. Measured in process,
  that is under half a microsecond against 61 to 113 microseconds, with no
  overlap between the two. Whether the gap survives a network is not measured.
  The graph is read for every name now, before the camera is resolved. Nothing
  a caller may see changes, on either the listing or the two media paths.

- The printer registration table is bounded, and it was not. `POST /p/register`
  carries no credential, because a printer has none before it registers.
  Anything that could open a socket to the port could therefore grow four maps
  that nothing evicted. The table now holds 400 registrations, which is the
  fleet this product claims. `core.connectors.<name>.config.max_registrations`
  raises it for a larger one. Over the bound the oldest registration no printer
  completed is dropped, and a warning names it. Collecting the token is the last
  step of the handshake, so a printer that finished one keeps its code through a
  flood of registrations it did not make. A registration an operator approved is
  never dropped. A server holding nothing but those answers a new printer 503
  with a `Retry-After`.

- A printer dialect built from configuration now writes to the process log. It
  was handed its settings and no logger, so every line it had went nowhere. That
  included the refusals a printer's session reports. The lines carry the
  connector's instance name.

- `GET /api/v1/resources` sends no `ETag` when the caller may see no instance.
  The token covers the whole configuration overlay, and it was sent to every
  caller. Somebody the graph grants nothing could poll the route and watch the
  value change whenever an operator wrote an instance. That told them the
  configuration had moved, which is more than an empty list should say. A caller
  who may see an instance gets the token exactly as before. Nothing else
  changes: a write still quotes it in `If-Match`, and creating an instance still
  quotes `If-None-Match` instead.

- An older binary refuses an authorization graph a newer one migrated, and says
  so. There are no down migrations. The way back from a bad upgrade is to stop,
  restore the backup and start the old binary. That binary had no way to tell
  that the database in front of it had been migrated past it. It would have
  served authorization questions against a schema it does not understand. A
  one-row `schema_meta` table now records the lowest schema version a database
  may be opened by. Which build applied each migration is recorded too, in
  `authz_migration.binary_version`, for a person reading it during a support
  call.

- The event stream at `/api/v1/events` is judged as a stream whatever a caller's
  `Accept` header says. Which credentials a route accepts depends on whether it
  is a stream or a plain call. That was read from the caller's own `Accept`
  header, so a credential the stream refuses was admitted without it. No
  deployment can issue such a credential yet, so no shipped configuration was
  open to it.

- The Go toolchain is 1.26.7, which closes six standard-library vulnerabilities
  reachable from this code. Two sit on paths the product always runs.
  `GO-2026-6089` reached the core's HTTP listener, and `GO-2026-6090` reached
  every TLS connection a printer, a camera or the AMQP sink opens. The other
  four were in `net/url`, `html/template`, `encoding/asn1` and the punycode
  handling behind `net/http`. Both modules were scanned again afterwards, and
  neither has a reachable vulnerability left.

- `[sdk]` The `sdk` module builds against gin 1.12.0 rather than 1.9.1. The old
  pin required `golang.org/x/net` 0.10.0, which carried five vulnerabilities
  reachable from the contract test helpers an extension author compiles. One is
  the HTTP/2 CONTINUATION flood. An extension that pins the `sdk` module itself
  receives the fix by upgrading.

- A revoked or expired session no longer keeps its event stream on
  /api/v1/events. The identity behind that stream was resolved once, when it
  opened, and never again. A token that was revoked, or that expired, kept
  receiving events for as long as the client held the connection, up to the
  whole `core.auth.token_ttl` of 720 hours. The credential is now re-checked at
  least every 30 seconds. A stream whose credential has gone is sent a
  `prusa-connect.auth.expired` frame and closed, so a reader can tell a
  revoked credential from a dropped connection.

- A `Last-Event-ID` reaching behind the caller's grant no longer replays a
  printer's retained window. A principal granted a printer after it had been
  publishing could send `<epoch>:printer/<id>=0`. The server gave that reader up
  to the last 32 events, including the model, firmware and serial number the
  printer reported on arrival. Such a reader is now told its continuity is gone,
  and resumes at the head. That is the same answer a position already aged out
  of the window gets.

- A printer could change what the server believed about it using a frame the
  server had already decided it could not read. A frame carrying an unusable
  command id was acted on before the id was checked. A printer on the network
  could therefore replace the storage devices the server reports for it. It
  could also plant an entry that a later directory listing returned, in a folder
  whose real answer carried no such file. Such a frame is now dropped before
  anything in it is believed.

- Cancelling somebody's print is checked as `cancel_job` again. A verb this
  build does not know now reaches no printer. `POST
  /api/v1/printers/{printer}/commands` derived `send_command` for every verb
  but the two raw gcode ones. `core.command.stop_print` was therefore
  authorized as an ordinary instruction. The published vocabulary described
  `cancel_job` as a permission of its own. Every verb the capability vocabulary
  names now carries the action it takes. A verb none of them names answers 400
  `core.printer/VERB_UNKNOWN`. No deployment's operator gains or loses
  anything: both actions enter the role layer at operator. The separation
  matters to a refusal, to an audit line, and to any level that divides them
  later.

- A login that was already in flight when an account was deleted or its password
  reset no longer opens a session. The account was read before the login queued
  for one of four password checks, and anybody with no credential can fill that
  queue and hold it open. The password is now checked against the account as it
  stands, so nothing is issued for an account that has been removed.

- A flood of unauthenticated setup calls can no longer drive this server out of
  memory. Sixteen concurrent calls to `POST /api/v1/auth/setup` held 305 MiB and
  were enough to get a container killed. Each one now waits its turn behind the
  four password checks this server runs at once, and callers racing to be the
  first administrator do that work once between them.

- Guessing at a named account with a password too short to be a real one now
  costs the same as any other guess. A password below the eight character floor
  was refused before any work was done and still counted against that account's
  ten-attempt budget. Ten one-character requests spent that budget for nothing;
  every refused password is now charged the same work, whatever its length.

### Fixed

- The published swagger document names its definitions the way it always meant
  to. Keys under `definitions` read `httpapi.X` again rather than
  `legacy_transport_httpapi.X`. Anyone reading `swagger.json` or `openapi3.json`
  directly sees the shorter names. The generated frontend types do not change,
  because they were already correct.

- A command waiting when a printer reconnects is told what happened to it. It
  used to answer with a 500 and no explanation. There are three answers now.
  The printer came back as something else, so the command was never sent, and
  it can be retried. Or the session ended some other way, also retryable. Or the
  command was already on the wire, which is never retryable, because nobody can
  know whether the printer acted on it.

  A reconnect on the same firmware still carries out whatever was queued, as
  before.

- A resource kind this build does not know is refused as unknown, and a kind of
  the wrong sort is refused for what it is. Both answers came from one place, so
  `PUT /api/v1/resources/gcode-preset/x` reported `unknown resource kind` for a
  kind this server publishes, and registering an extension type on `printer`
  said the same. The refusal now names which of the two happened, and a kind
  outside the closed set lists the values that are inside it.

- One long log line no longer silences an out-of-process extension for the rest
  of its life. The host copies what a child writes to its standard error into
  its own log. A line over 8 KiB ended that copy for good, so everything the
  child said afterwards was lost. A long line is now written as
  several records, and every record but the last carries `split=true` so a
  reader can tell a cut line from two lines.

- Installing the frontend no longer switches off the commit and push gates. A
  `pnpm install` points git at husky's hooks directory, and the files there now
  run the repository's own hooks, so the order in which `make install-hooks` and
  `pnpm install` ran decides nothing. A push from a clone whose hooks are inert
  is refused, and the refusal says which directory git is reading and what to
  run.

- A command verb spelled the way the shipping server spells it is answered with
  this API's name for the same command. A request carrying
  `{"verb":"SET_NOZZLE_TEMPERATURE"}` was refused with
  `core.printer/VERB_UNKNOWN`, and it repeated the verb back. An integrator
  migrating from that API was told their word was wrong and never which word
  was right. The refusal now carries `suggested_verb`, here
  `core.command.set_nozzle_temperature`. It answers any spelling that differs
  from a verb only in letter case or in a missing `core.command.` prefix. That
  covers all 57 of the shipping server's command names. The suggestion is
  computed from the verb the caller sent and from the vocabulary at
  `GET /api/v1/vocabularies/capabilities`, which needs no credential. So the
  refusal still says nothing about the printer that was addressed. The same
  request against an id no printer answers to returns the same document.

- A reader that reconnects to `GET /api/v1/events` with a `Last-Event-ID` is no
  longer handed the event that cursor names a second time. The server gives an
  event its place in the replay window before it hands that event to readers. A
  reader attaching between those two steps is replayed nothing, because the
  replay starts above its cursor. The delivery it has just joined then hands it
  the event. A browser reconnecting under load saw one event twice, and
  a client that acts on each event once acted twice. A reconnecting reader now
  starts a stream knowing the place it holds, so anything at or below it is
  refused before it is queued.

- A path no route claims is refused rather than answered with the frontend. The
  frontend is served at `/`, so every address on this origin returned 200 and a
  page. `POST /p/nothing` did, and so did a printer dialling a core with no
  connector mounted. That is a success the caller cannot use, and nothing about
  it looks wrong from either end. Two prefixes changed. The paths beside a
  dialect's own frozen ones are reserved, so `/p/anything` beside `/p/register`
  and `/p/ws` answers `core.validation/ROUTE_NOT_FOUND`. And the single-page
  fallback answers a browser navigation, which is a GET, so a write anywhere
  under `/` is refused instead of served a page. A browser reaches the
  application at `/` and at every client-side route exactly as before.

- A deployment that declares no resource instance is no longer warned at every
  start that its principal holds no grant. The check read the list of instances
  the caller may see, and it runs before the configuration is reconciled. A
  server with nothing to list therefore looked like a server whose grants were
  gone. It now asks whether the principal reaches any resource kind, which is
  what the first grant is written on. The warning still appears for the case it
  exists for. An operator who changed `core.principal` after the first run
  keeps a graph naming the old one, and the line names the principal to
  restore.

- `/openapi.json` publishes the camera surface. `GET /api/v1/cameras`, `GET
  /api/v1/cameras/{camera}/snapshot` and `GET /api/v1/cameras/{camera}/stream`
  are served by every build that has a media plane. The document named none of
  them. A generated client therefore reached none of them, and the API
  compatibility gate compared two documents that both lacked them. The document
  is generated from a server with every optional dependency wired. A new gate
  refuses a document that does not publish an operation this package registers.

### Changed

- **`required: true` is refused on a resource instance the core did not write.**
  The flag makes `GET /api/v1/readiness` answer 503 while the instance is not
  serving. A load balancer then stops sending traffic to this server, so an
  extension that fails to start could silence a whole print farm. An instance is
  the core's when its type is compiled into this build and its type name carries
  no third-party namespace. A `service` runtime, a `script` runtime and a
  namespaced type are each refused. In the configuration file the block is
  refused and the rest of the file still runs, with one log line naming the
  file, the instance and the key. `PUT /api/v1/resources/{kind}/{name}` answers
  422 naming `body.required`. An instance that already holds the flag in the
  machine-owned overlay is refused at the next start. To upgrade, remove
  `required` from every instance whose type is not the core's, and watch that
  instance's own condition instead.

- **The printer paths are now the device paths.** `GET /api/v1/printers` is
  `GET /api/v1/devices`, and every path under it moves the same way. That
  covers `/api/v1/devices/{device_id}` and its `/commands`, `/files`, `/folders`,
  `/gcode`, `/gcode/presets/{preset}` and `/storages`, plus
  `/api/v1/dev/devices/{device_id}/commands`. The path parameter is
  `{device_id}` rather than `{printer}`. A client sending an old path gets 404,
  because nothing is served there any more. Ten schemas move with them:
  `Printer` is `Device`, and `PrinterPage`, `PrinterDeparture`, `PrinterLink`,
  `PrinterIssue`, `PrinterIssueSummary`, `PrinterFile`, `PrinterListing`,
  `PrinterStorage` and `PrinterUpload` each take the same prefix. The file
  listing answers `items` where it answered `entries`. `Camera.printer` is
  `Camera.device_id`, and the developer command log's `printer` member is
  `device_id`. What names a type of device keeps the word printer:
  `PrinterCommand`, `PrinterResult`, the gcode schemas, the `core.command.*`
  verbs and the `kind` value `printer` are all unchanged. Read the served
  document at `/openapi.json` for the whole surface.

- **A grant is written on a device, not on a printer.** The authorization
  object type `printer` is `device`, and `printer_group` is `device_group`. That
  holds on every surface naming one: a tuple, a grant path
  (`/api/v1/grants/device/{object_id}`), the access check body's `object_type`,
  and the published vocabulary at `GET /api/v1/vocabularies/authorization`. Its
  major version is now 3, because two published types are retired. An
  upgrade migrates an existing graph in place, on SQLite and on Postgres. Every
  tuple naming the old types is rewritten, the two projections become
  `authz_device` and `device_access`, and their `printer_id` column becomes
  `device_id`. Grants survive the move and nobody gains or loses one. A
  deployment that had already started the binary before this upgrade cannot
  return to it. The compatibility floor rises, so an older binary refuses to open
  the migrated database rather than reading tables that are no longer there.
  Take a backup before the upgrade, which is the only way back. The error code
  `core.auth/INVALID_PRINTER_NAME` is now `core.auth/INVALID_DEVICE_NAME`.

- **Whoever owns a printer can flash it.** The authorization graph carries a new
  relation, `owner`, written on a printer through
  `POST /api/v1/grants/device/{id}` like any other grant. It is not a role. It
  adds the command actions and `provision` to whatever the person already holds.
  Somebody who owns a printer and holds only `operator` on it may flash,
  upgrade, re-token and rename it, and an operator who owns nothing may not. An
  owner may not administer the grants on their own printer and may not send raw
  gcode, so ownership is assigned by whoever administers the printer and cannot
  be passed on by the owner. Blocking somebody on a printer removes their
  ownership of it with everything else. Nothing assigns ownership on its own. A
  printer that registers is admitted with no person involved while
  `auto_register` is true. Every printer is therefore unowned until an
  administrator says otherwise, and a printer that is already registered stays
  unowned.
  The published authorization vocabulary is at version 3.2.0.

- **An operator can no longer flash firmware.** `core.command.flash`,
  `core.command.upgrade`, `core.command.set_token` and
  `core.command.set_hostname` took `send_command`, which every operator holds.
  They now take a new action, `provision`, which the manager level confers and
  the operator level does not. This server can undo none of the four: firmware
  cannot be put back, a token hands the machine to another Connect account, and
  a hostname is the address every other caller reaches it at. Anybody whose
  highest role on a printer is operator, whether it is written on the printer,
  on a printer group above it or on the org, sends those four verbs today and is
  refused after the upgrade. So does a service account or a peer node holding
  operator. To keep somebody flashing, raise their grant on that printer to
  `manager`, or make them an org `admin`. There is no tuple to write for
  `provision` on its own, because it is conferred by a role and never granted
  directly. Nobody gains anything: a manager and an org admin already held
  `send_command`. The published authorization vocabulary is at version 3.1.0 and
  carries `provision` with the four verbs it governs.

- The authorization type `device` is now `device_credential`, and the published
  vocabulary is at version 2.0.0. It is what a printer authenticates as, and the
  printer itself is the `printer` type, which is the object a grant is written
  on. One word for both was going to collide. Nothing in this server mints a
  subject of that type yet, so no deployment holds a grant naming it and no
  upgrade step is needed. A graph whose tuples were edited outside this server
  and still name the old type is refused at start-up, with the rows listed.

- A gcode preset refused for the printer's model answers
  `core.printer/PRESET_MODEL_MISMATCH`. The status stays 422 and the refusal
  stays a refusal: what moves is the code, which was
  `core.validation/REQUEST_INVALID`, the same answer a malformed request gets.
  The problem now carries `printer_model` and `preset_models` as parameters, so
  a client showing one printer can offer the presets that run on it rather than
  reading a sentence. A client branching on the old code sees this refusal as an
  unrecognised one until it reads the new code.

- **The container image now runs the core.** It used to run the shipping
  server, because the image's entry point is the binary and the binary's default
  subcommand is that server. Somebody who pulls the new image gets a different
  server on the same port. `/api/v1/...`, `/openapi.json` and the API reference
  at `/docs` answer, and every path the shipping server published under
  `/api/...` now answers 404. The web interface the image carries is written
  against those old paths and does not work against the core yet. The MQTT
  broker on 1883 and 1884 is gone. The printer-facing listener on port 80 is
  gone too, and printers dial the API listener instead. `./data/state.db`,
  `./data/commands.db` and `./data/auth.db` are no longer read, so accounts,
  jobs, labels and command history from an earlier image are not carried over.
  To keep the old server, name it: `docker run <image> serve`, or
  `command: ["serve"]` in compose. The binary's own default is unchanged.

- The container writes the resource overlay and the authorization graph into
  `/config/config.d`, which needs a volume of its own. The root filesystem is
  read-only, and the operator's `config.yaml` is mounted read-only beside it. The
  bundled compose file declares that volume. A deployment that writes its own
  container definition adds it, or points `core.overlay` and
  `core.authz.database` at a path under `/data`.

- The container health check probes `GET /api/v1/readiness` instead of
  `/health`, so a container reported healthy is one that can serve.

- Two processes starting against one Postgres database no longer leave one of
  them dead. Migrations ran unguarded there. A second process creating the same
  tables answered `relation "authz_migration" already exists`, or a unique
  violation on an internal catalogue, and refused to start. Each run now takes a
  Postgres advisory lock for as long as it lasts. A process that finds the lock
  held waits for the run before it. SQLite is unchanged: the file has one writer,
  and a migration there already waited its turn.

- The authorization graph is put into write-ahead logging once, when it is
  opened, rather than on every connection. The journal mode is a property of the
  file, and setting it takes a brief exclusive lock that no busy timeout covers.
  Several connections opening at the same moment against one graph could each
  answer `database is locked`, before anything this server does about contention
  ran. A deployment that starts two processes at once is where it showed.

- A printer whose session ends now stays on `GET /api/v1/printers` instead of
  leaving the API. It used to be removed the moment its connector released it,
  so the listing dropped it, its id answered 404 and the last state it reported
  was lost. An operator could not tell a printer that went away from one that
  had never registered. Such a printer now reads `connected: false` and carries
  a `departed` member with `reason` and `at`, and the rest of the record is the
  last reading this server took. An id nothing ever registered is still a 404.
  A command, a file listing or a storage listing for a departed printer answers
  409 `core.printer/SESSION_ENDED` rather than queueing for a session that no
  longer exists. The record covers the four hundred most recent departures.

- The developer view at `/dev` no longer re-reads `GET /api/v1/printers` every
  five seconds. It polled for one fact no event carried: what this server makes
  of the frames a printer sends. An open page therefore cost one listing request
  every five seconds, against the server somebody opened it to watch. That
  reading now arrives as `com.prusa.connect.printer.link.changed`. The page
  reads the listing once per event-stream connection and on the refresh button.

- A sink instance can now be a short expression instead of a compiled adapter.
  Write `runtime: script` and `type: script` under `resources.sink.<name>`, put
  the expression in a `script:` block, and name each destination under
  `config.endpoints` as `<name>: <url>`. The expression is handed one event and
  returns `msg(endpoint, key, body)`, several through `fan(...)`, or `nil` to
  drop the event. The host posts each body to the address the operator wrote for
  that endpoint name, with the routing key in an `X-Prusa-Routing-Key` header. An
  expression that will not compile fails its own instance at start and reports
  the line, the column and the token. The rest of the server keeps serving.
- [sdk] `sdk/scriptapi` carries the two types a script author writes against:
  the `Event` a script is handed and the `Message` it returns. It imports the
  standard library alone.
- `GET /api/v1/vocabularies/resource-kinds` now answers with the runtimes each
  kind offers, and with a description of each runtime. A client can tell which
  kinds accept `service` and which accept `script` without reading these notes.
  The `resource-kinds` page carries the same two additions.
- A printer connector is now declared, under `core.connectors`. Every dialect
  this binary carries used to be mounted in every deployment, with no settings
  and no switch. An entry names an instance, a `type` and the dialect's own
  `config` block. Only the dialects an entry names are mounted. A file that
  declares no connector still mounts the shipping xBuddy dialect on
  `/p/register` and `/p/ws`, so printers in the field keep dialling.
- The xBuddy connector's keepalive is configurable: `read_deadline`,
  `ping_interval` and `ping_timeout`, defaulting to the shipping 60s, 54s and
  10s. The three keys sit under `core.connectors.<name>.config` and reach the
  driver that is running. They were published as options and ignored, so a
  silently unplugged printer always took up to 56 seconds to be reported
  unreachable. A block the dialect refuses now stops the server starting and
  names the key.

- A verb a printer does not have is now refused with the verbs it does have.
  The 409 at /api/v1/printers/{printer}/commands carries `verbs`, the printer's
  capability set as a JSON array, and `verb_count`, how many verbs it has in
  total. A caller who sent `SET_NOZZLE_TEMPERATURE` is now told the printer
  answers to `core.command.set_nozzle_temperature` instead of being told only
  that their word was wrong. At most 128 verbs are listed, so a large set is
  read from the printer itself.

- A gcode line a printer refuses now says why at
  /api/v1/printers/{printer}/gcode. The step for that line carries
  `machine_reason`, the machine's own word for the refusal, where it used to say
  only "the printer did not accept it". A line this server declined to send is
  unchanged: it carries an error code and no machine word.

- A command a Prusa printer refuses now says why at
  /api/v1/printers/{printer}/commands. The answer carries the machine's own word
  for the refusal, either a code such as `FILE_EXISTS` or a sentence written for
  a person, and `/dev` shows the same word.

- A dialog on a Prusa printer's screen is now readable in its state tree, under
  /dialog. Beside the id that `core.command.dialog_action` names, the tree
  carries the code, the title, the text and the button labels the firmware
  displayed. All five are removed once the machine says the dialog closed, which
  is either a state no dialog is up in or a dialog id of zero. A frame that
  simply leaves the id out says nothing, so a dialog no longer flickers between
  telemetry frames that carry it and frames that do not.

- The printer list can be paged, and a resource that refuses to start no longer
  stops the server coming up. `GET /api/v1/printers` takes `limit` and `offset`
  and answers a total, where it used to return the first fifty rows and no way to
  ask for more. Printers that are away are now filtered before the page is cut,
  so a page no longer arrives short. A resource instance that will not start is
  reported on its own and the rest of the fleet still comes up.

- The developer view no longer stops updating when a link names a printer that
  has not arrived. Opening `/dev?select=` on a printer the server has not seen
  left the page's live connection unsubscribed until it was reloaded, which is
  the ordinary case for a link handed to somebody else.

- Log lines below info are sampled instead of every one being written: the first
  few of a given message in each second, then one in a hundred. Info, warning
  and error lines are never sampled. What sampling discarded is counted and
  published beside what was written, so a quiet debug volume can be told from a
  sampled one.

- The MQTT topics the extension reference publishes were wrong on four of five
  entries, so a Home Assistant install that subscribed to what we documented
  received nothing. The reference said `v1/printers/{printer}/data/state` and the
  wire carries `v1/devices/printers/{printerId}/data/state`; the job topic was
  also missing its `data/` segment. The topic table is now read out of the
  adapter's own routing rather than typed a second time into its descriptor.
- [sdk] `extension.Doc` separates what an extension puts on the host's event
  stream from what it puts on a transport of its own. `Emits[].Type` is a type in
  the host's event vocabulary and is now checked against it at registration, so a
  descriptor naming something that is not an event type is refused instead of
  reaching the published page. `Publishes[].Address` is a topic, routing key or
  discriminator the extension owns and is checked only for the characters the
  page cannot carry. The four event sinks moved their entries and now emit
  nothing onto the host bus, which is what they always did.

- [sdk] An extension descriptor now separates the event types it publishes onto
  the server's event stream from the addresses it publishes to on a transport of
  its own. `emits[].type` is a name in the server's event vocabulary and is
  refused at registration when that vocabulary refuses it, including a name
  under `com.prusa.connect.` the server does not carry. A topic, a routing key
  or a message kind belongs in the new `publishes[].address`, which is checked
  only for text the reference can carry. An extension that declared a
  destination in `emits` moves it, or it no longer registers.
- The server can serve its printer-facing endpoints over TLS on a second
  listener configured under `core.printer`, pinned to the TLS 1.2 profile Prusa
  xBuddy firmware accepts. That profile is TLS 1.2 with
  `TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256`, and the firmware verifies the
  certificate, so the listener refuses a certificate that is not ECDSA.
  `core.api` keeps its own floor
  and its own certificate, because a browser and a printer trust different
  anchors. A deployment that configures neither is unchanged and serves plain
  HTTP on one listener.
- The printer listener now refuses TLS 1.3 as well as everything below 1.2. Go
  ignores a cipher suite list under TLS 1.3, so a client capable of 1.3 was
  negotiating a suite no Prusa printer speaks while the startup line and the
  documentation both named one 1.2 suite. Nothing that reaches this listener is
  affected: xBuddy firmware speaks 1.2 only, and any other client belongs on
  `core.api.addr`.
- `core.api.addr` under TLS now offers HTTP/2. A browser was limited to six
  connections to the origin, one of which `GET /api/v1/events` held open for as
  long as the page was on screen.
- A grant change can no longer lock the caller out of an object. Withdrawing the
  grant you administer with, blocking yourself, or severing the organisation
  your access arrives through are refused when nothing else confers that access,
  and the refusal names the object, the permission being protected and the order
  a handover runs in.
- A frame a printer sends that the server cannot read now costs that frame
  rather than the connection, and is counted. A printer running firmware this
  build does not fully understand stays connected and keeps serving commands
  instead of reconnecting in a loop. Keeping the connection removed the bound on
  the log, so a printer whose frames are all unreadable is now logged once when
  its connection starts refusing and once with the total when that connection
  ends, rather than once per frame.
- The capability profiles compiled into the binary are parsed once per process
  instead of once per caller, cutting a read from 2.7 ms and 1.27 MB to 0.12 ms
  and 247 KB.

- [sdk] `com.prusa.connect.printer.arrived` is documented as firing once per
  session, when the server takes a printer and registers it. A printer that goes
  away and comes back without its session ending is carried by
  `com.prusa.connect.printer.presence.changed`. The event-type vocabulary moves
  to 1.2.0; no type, class or payload changed.
- `GET /api/v1/events` refuses a request that names an origin the caller may not
  observe with a message saying so. It previously said the caller had been
  granted nothing to observe, which was never the case for a request that
  reached it. The refusal still names no origin.
- [sdk] `connectorapi.Result` gained a `MachineReason` field, carrying a
  dialect's own word for why a command was refused. It is empty on an accepted
  command, and the conformance suite fails a driver that sets it on one.
- [sdk] `extension.Declare` no longer takes a frame count. The namespace is
  derived by walking the call stack to the registering package, so a driver
  invoked from a host frame registers under its own module and cannot claim the
  application's `core` namespace.
- [sdk] A module whose path is not already a valid namespace is refused at
  registration instead of having its path rewritten. `github.com/Acme/mk4` and
  `github.com/acme/mk4` previously shared one collision domain; an author whose
  module path carries an uppercase letter or an underscore must rename it.
- [sdk] One name may be taken once per extension point across the whole
  process, not once per registry, so a storage backend registered two ways can
  no longer appear twice in the published reference.
- [sdk] A registration is refused when any descriptor string carries a control
  character, a line break, or a backtick in a value the reference publishes
  inside a code span, and a config key must be `lower_snake_case`. A crafted
  description could previously add a section to the published extension
  reference under another entry's name.
- A printer running a release candidate is no longer given the capabilities of
  the finished release. Firmware versions are ordered the way semantic
  versioning orders them, so `6.5.0-RC` is treated as a build made before
  `6.5.0` rather than as `6.5.0` itself, and a printer on a candidate build of
  the oldest supported firmware still matches its profile.
- A capability set that breaks its own description is reduced to the part that
  can be acted on, rather than being reported and then commanded against. The
  printer keeps every capability that is sound and loses only the ones nothing
  could have carried out.
- [sdk] `connectorapi.Set` gains `Prune`, which returns the set with every key
  its own descriptor rules refuse removed, the keys it removed, and why.
  `FirmwareLabel.String` is refused by lint outside the profile and capability
  packages: read the resolved capability set instead of the version text.
- [sdk] The published storage conformance suite gained a `PathEscape` group.
  Every provider operation is driven with an absolute path, a climbing relative
  path, a rooted climb, a `..` segment in the middle, a deep climb to the
  filesystem root and the backslash form of each, and must leave everything
  outside the store untouched and report every path relative to the store. A
  provider resolving caller-supplied names with `filepath.Join` fails the suite
  where it previously passed.
- Builds without the observability feature link no Prometheus client and are
  4.4 MB smaller on ARM.
- [sdk] `Register`, `TryRegister` and `Declare` take a descriptor. An author
  calling `connectorapi.Register(Type, New)` changes it to
  `connectorapi.Register(Type, doc, New)` with an `extension.Doc` filling in at
  least the summary, description, version, contract version and stability.
- [sdk] Routes are registered under a name: `routesapi.Register(fn)` becomes
  `routesapi.Register(name, doc, fn)`. The name is qualified by the registering
  module, so a log line and the published reference can say which plugin
  mounted which routes.
- [sdk] `RegisterFactory`, `IsRegisteredType` and `BuildProvider` on
  `storageapi`, `eventsapi` and `authapi` are deprecated. Use `Register` and a
  registry built with `NewRegistry`.
- [sdk] The namespace and the registration rule moved into a new
  `sdk/extension` package. `connectorapi.Namespace` and
  `connectorapi.CoreNamespace` are aliases of it, so no import changes.
- [sdk] An extension hosted at a module path with a host in it, which is every
  extension on GitHub or GitLab, can now mint a capability key, an error domain
  and an event type from its own namespace. None of the three accepted the
  namespace such a module was given, so a driver could register and then not name
  anything it needed to name. A module at `github.com/acme/mk4` now holds
  `com.github.aux.acme.mk4`. A capability key and an error domain accept every
  label a module path may carry, which adds hyphens and a leading digit to both.
  A module path is refused when it carries the label `aux`, when it is a single
  label with no host, and when its namespace renders past 96 bytes, and each
  refusal names the reason.
- A principal reaches the authorization graph as `user:<subject>`, so
  `GET /api/v1/principal` answers `user:operator` where it answered
  `operator`. The graph will also key on service accounts and, later, on
  federated nodes, so the type is decided once where the credential is wired
  rather than by every surface that reads one.
- [sdk] A printer driver declares which direction it is reached in.
  `connectorapi.Driver` is `Baseline()` alone, `Outbound` carries the `Connect`
  it used to, and `Inbound` returns a `net/http` handler the host mounts. A
  driver that only dials out adds nothing but the `Outbound` name.

### Added

- The event stream now carries `com.prusa.connect.printer.presence.changed`
  when a printer becomes unreachable and again when it comes back. The session
  does not end at either point, so a reader watching the stream would otherwise
  see only a printer that has gone quiet.

- Event sinks and identity providers can now be declared under `resources:` and
  are supervised like any other instance. A declaration names an instance, a
  `type` and the adapter's own `config` block.

- A declared event sink now receives printer state changes. It reads the event
  bus and publishes what its grants let it see.

- [sdk] `com.prusa.connect.printer.link.changed`, a new core event type. It
  carries the printer and what this server makes of its frames. That is whether
  the connector counts them, how many it had no reading of, and how many
  telemetry patches it dropped. Both totals come with a rate and the window it
  was measured over. A printer running firmware this build cannot read connects,
  stays connected and publishes nothing. It looks exactly like a printer that is
  idle, and no other event says otherwise. The frame repeats while either total
  is moving, and once more after it settles. A reader is therefore never left
  showing a refusal storm that ended. `com.prusa.connect.printer.departed` now
  carries a `reason` beside the printer and the connector. The event-type
  vocabulary is version 1.3.0.

- Cameras. A camera is declared under `resources.camera.<name>` like any other
  resource, and it names the printer it watches. The listing at
  `GET /api/v1/cameras` carries every camera whose printer the caller may read
  telemetry from. A still image comes from
  `GET /api/v1/cameras/<name>/snapshot`. A live view comes from
  `GET /api/v1/cameras/<name>/stream`, which relays the camera's own frames as
  multipart MJPEG that an `img` tag plays directly. Nothing is decoded or
  re-encoded, so a snapshot is the bytes the device sent. The snapshot carries
  `Last-Modified` and `Cache-Control: private`. A thumbnail grid over a large
  fleet therefore costs one conditional GET per cell rather than one image
  transfer.
- [sdk] `sdk/cameraapi` is the contract a camera driver implements. It is a
  declared format set, a `[]byte` snapshot with a media type, and an
  `io.ReadCloser` of frames the driver has already framed. It imports the
  standard library only, and `sdk/cameraapi/cameraapitest` is the conformance
  suite an implementation runs against.
- The `core/mjpeg` camera driver ships in the binary. It reads any camera that
  answers a JPEG on one URL and a multipart MJPEG stream on another, which
  covers the Prusa ESP32-CAM firmware and most IP cameras. A body that is not a
  JPEG is refused, and one frame is bounded at `max_frame_bytes`. A camera is a
  device on somebody's network, and its answer is served to a browser.
- The reference `echo` connector ships in the binary. It is a plain JSON dialect
  over HTTP, meant for trying the device wire without a printer, and it
  authenticates nothing. It is mounted only where `core.connectors` names it,
  under `/connectors/<name>`.
- [sdk] fault.DomainResource() is the core error domain for a named resource
  instance: one that is not there, one whose version moved on, one the API does
  not write. A client that branches on the domain half of an error code can now
  tell those from `core.validation`, which is a request the caller has to change
  before sending again. No code moves domain in this release.
- `GET /metrics` serves this process's Prometheus exposition. It is an operation
  of the same router and document as everything else, it takes the same identity
  every other endpoint takes, and a build compiled without the observability
  feature answers 404 there rather than an empty page that would read as a
  healthy process with nothing to report. Scrape configuration needs a
  credential for it.
- `GET /api/v1/health/connectors` reports device health per connector: the
  counts by state, a verdict over the population, and a capped list of the
  devices worth looking at. It covers the printers the caller may see and
  nothing else. `GET /api/v1/health` is unchanged and still reads no dependency,
  so a printer switched off overnight cannot take the process out of rotation.
- The exposition now carries what the supervision plane measures, so every
  configured resource is reported the same way whoever wrote it: calls,
  durations, refusals, constructions, drops, panics, state changes, discarded
  reports and abandonments, each labelled by kind and instance. Log volume is
  reported by level, and per-driver byte throughput and pending-command depth
  replace the per-printer counters the previous server exposed.

- [sdk] `storageapitest.RunStreaming` holds a storage provider to the rule that
  nothing on its path scales with file size. It streams half a gigabyte in
  through `Save` and back out through `Open` inside a second process whose live
  heap is killed at 32 MiB, so a provider that reads an upload into memory fails
  the suite on a workstation instead of passing there and being killed by the
  kernel on a Raspberry Pi. It is a separate call rather than part of
  `RunProviderContract`, because it moves a gigabyte of disk and forks a process.

- A dialog waiting on a printer's own display can now be answered over the API,
  and a firmware image sitting on the printer's storage can be installed.
  `POST /api/v1/printers/{printer}/commands` accepts `DIALOG_ACTION`,
  `UPGRADE` and `FLASH`, which every supported model declares and none of which
  reached the machine before, so a print stopped on a question had to be
  answered by walking to it. A path naming a firmware image is held to the same
  rule every other path is: one that climbs out of its storage is refused and
  never reaches the printer.

- `make style-check` gates six more of the writing rules: a heading that names
  nothing, a count with no items behind it, a run of twelve words repeated
  inside one document, and a per-file budget for the bolded lead-in, the
  contrast frame and the absent agent. A document that quotes what these refuse
  puts the quotation in a fenced block, which every prose gate now skips, and a
  page a generator wrote says so in its head and is skipped by the six. The 53
  documents already over a budget are recorded in `scripts/writing-style.baseline`,
  which fails when a count rises and when a count falls without the line being
  corrected. The rules are `docs/decisions/0017-writing-that-reads-as-written.md`.
- A printer running firmware this build does not fully understand now says so.
  `GET /api/v1/printers` and `GET /api/v1/printers/{printer}` carry a `link`
  block reporting how many frames the server could not read and how many
  telemetry samples it was too slow to take, each as a total for the session and
  as a rate over the span the reading covers. The developer view at `/dev` shows
  it as a column beside every printer and announces an unreadable one above every
  panel, because such a machine stays connected, reports nothing, and until now
  looked exactly like a printer that was idle.
- The developer view deep-links one printer. `/dev?select=<printer>` opens on
  that printer, and selecting one updates the address bar, so a reading can be
  handed to somebody else as a URL.
- Gcode sequences can be saved and reused. `PUT /api/v1/gcode-presets/{name}`
  stores a named, ordered list of gcode commands with the printer models it was
  written for, and `POST /api/v1/printers/{printer}/gcode/presets/{preset}`
  runs one. A run stops at the first step the server or the printer did not
  accept and reports every step, including the ones it did not attempt, exactly
  as a raw sequence does.
- Running a stored gcode preset takes `send_gcode` on the printer and nothing
  on the preset, and writing one takes `configure_resource` on the new
  `gcode-preset` resource kind. An operator can be trusted with a reviewed
  maintenance macro without being trusted to change what it sends to a machine.
- A gcode preset that names printer models is refused on a printer that is not
  one of them, with 422 and nothing sent to the machine. A preset that names no
  model runs on any printer. The comparison uses the model a printer reported
  in its handshake and ignores case.
- Presets are stored in the same machine-owned configuration overlay as the
  resource instances, so they survive a restart, are written atomically, and
  share one ETag: reading either and quoting its ETag in `If-Match` fences a
  write against both.
- The server publishes every closed set it depends on at
  `GET /api/v1/vocabularies`. The sets are error codes, extensions, capability
  keys, resource kinds, the authorization vocabulary and the core event types.
  Each answers with what that build carries, needs no credential, and is
  generated from the same code as its documentation page.
- The authorization vocabulary is documented for the first time. Every action
  says which object type it is defined on and which relations confer it, read
  from the code that enforces the check rather than written beside it.
- A printer's own storage devices and its file tree are readable through the
  API. `GET /api/v1/printers/{printer}/storages` reports each device's
  mountpoint, type, free space and read-only flag, and
  `GET /api/v1/printers/{printer}/files?path=` lists one directory at a time.
  Printers also accept the four file-management commands.
- Raw gcode can be sent to a printer at `POST /api/v1/printers/{printer}/gcode`,
  one command per line and in order. The response says what became of each
  line, and a run that stops names the lines it did not attempt. Sending it
  takes a new `send_gcode` permission held at manager level and above, so a
  caller who may pause a print does not thereby hold it.
- A gcode payload is refused unless it is a single command on one line in
  printable ASCII, and `M997`, `M28`, `M29`, `M30` and every `D` code are never
  sent whatever the caller holds. A capability profile can withhold
  `core.command.send_gcode` or `core.command.force_gcode`, turning raw gcode
  off for one model or firmware band without affecting any other printer.
- Domain events can be published to a Kafka topic, keyed by the printer they
  are about so one printer's history stays in order on a single partition; to
  an AMQP 0-9-1 exchange such as RabbitMQ, with publisher confirms on so a
  message the broker refused is reported rather than lost silently; and to MQTT
  on the same 22 `v1/` channels at the same quality of service and retain flags
  Home Assistant already subscribes to.
- Local disk and staging storage are configurable adapters, resolving every
  caller-supplied name through a directory handle so a symlink or a Windows
  junction planted inside a store is refused rather than followed. Signing in
  against an OpenID Connect issuer is one too.
- [sdk] Event sink plugins have a published conformance suite, so a sink
  written outside this repository is held to the same rules about never
  blocking the event bus and never outliving its own shutdown as the built-in
  ones.
- A developer view at `/dev` shows the connectors this build carries, the
  resource instances and their supervision state, the printers that have
  dialled in, one printer in detail, and the event stream as it arrives. It
  takes the same credentials every other endpoint takes and shows only what
  those credentials already reach.
- The test suite drives every printer model the capability profiles cover, at
  every firmware version Prusa has released for it, so a change that breaks one
  model on one firmware fails the build and names which. End-to-end tests run
  against the built binary with an operator's configuration file.
- The HTTP API has a written compatibility promise and a gate that holds it:
  `make api-check` classifies every difference from the last published surface
  and refuses any it cannot classify.
- Printers reach `prusa-connect core`. The server answers the three paths
  Prusa xBuddy and Buddy firmware dials, `POST /p/register`, `GET /p/register`
  and `GET /p/ws`, on the same origin as the API. A printer registers itself, is
  admitted, and appears on `GET /api/v1/printers`; nothing in the configuration
  file names one.
- `GET /api/v1/printers` lists the printers a caller may see, `GET
  /api/v1/printers/{printer}` returns one with the state it last reported, and
  `POST /api/v1/printers/{printer}/commands` sends it a command. A verb the
  printer does not have, or an argument outside what it accepts, is refused
  before the printer is reached and the refusal names the verb.
- A printer that connects publishes `com.prusa.connect.printer.arrived` and
  every telemetry value it reports as
  `com.prusa.connect.printer.telemetry.sampled` on `GET /api/v1/events`, and
  `com.prusa.connect.printer.departed` when its session ends.
- The web interface is served from the binary at `/`. It is compiled in, so
  nothing is fetched at runtime and an air-gapped install needs no network. A
  build carrying the `minimal` tag embeds none, and `/` answers a problem
  document there.
- A resource instance can say how its type is reached: `runtime: builtin`
  compiles it into the binary, `runtime: service` runs it as a supervised child
  process verified by checksum before every launch. There is no third install
  mode, because whether a type is in the binary is a property of the artefact
  and not a switch an operator flips.
- `make build` derives its build tags from the resource instances you declared,
  so an adapter you configure is compiled in and one you reach as a service is
  not. A configuration file never names a Go build tag: `adapters/tags.json` is
  the only place a type is mapped onto one, and a missing one fails the build
  rather than quietly producing a binary with no optional features.
- A printer refused a command now reports the printer's own reason for the
  refusal alongside the refusal itself, so a caller can tell "the printer
  already holds this file" from "that did not work" without decoding the
  vendor's raw reply.
- Grants can be written over the API. `/api/v1/grants/{object_type}/{object_id}`
  lists the grants on a printer or a resource kind, adds one, and withdraws one,
  so an operator can give somebody access without editing the database.
  Changing an object's grants requires the same level as managing that object,
  and a grant may not hand out anything the caller does not already hold there.
- Metrics follow one naming and cardinality rule: a printer identifier may
  label a counter or a gauge and never a histogram, so a four-hundred printer
  fleet costs about 2,400 series rather than tens of thousands. Every histogram
  is defined once and served two ways, with explicit buckets for a text scrape
  and native buckets for a Prometheus 3.8 or later scrape, so moving to native
  histograms is a scrape-config change rather than a re-instrumentation.
- Connector health is reported as one entry per connector, with counts by state
  and the twenty longest-broken devices, rather than one entry per printer. A
  printer not heard from within its connector's grace period reports unknown
  rather than connected.
- [sdk] `storageapi.RemoveStaged` deletes the object a `LocalWriter` named, for
  a caller that kept the name and no longer holds the provider. It refuses a
  name that is not absolute, and it unlinks a link found at the name instead of
  following it.
- The server now enforces who may see and change each configured resource. It
  keeps an authorization graph in a SQLite file at `core.authz.database`,
  created empty beside the overlay on first run and never fetched from
  anywhere, and asks it on every list, read, write and event subscription. A
  caller with no grant is told an instance does not exist, in the same bytes as
  one that really does not. A brand-new install grants `core.principal`
  ownership of the node so it is usable immediately; that first grant is
  written once, into an empty graph, and a restart never rewrites it.
- The event hub is readable from outside the process: `GET /api/v1/events`
  streams every event the caller may observe as server-sent events, resumable
  with `Last-Event-ID`. Authorization is taken once when the stream opens
  rather than per event, a grant withdrawn while the stream is open narrows it
  without a reconnect, frames are coalesced into windows so a fleet does not
  cost one write per event, and a reader that falls too far behind is sent a
  `com.prusa.connect.stream.continuity.lost` frame naming where to carry on
  from rather than being quietly given part of a stream.
  `core.events.heartbeat` sets how often an idle stream writes a keepalive
  comment, and defaults to 15s.
- [sdk] Every extension registration now carries a descriptor: a summary and a
  longer description, its own version and the version of the contract it was
  compiled against, a stability level, the config keys it accepts with their
  types and defaults, and the event types it emits with their versions. A
  registration missing any of the mandatory fields does not start the server.
- A generated reference at `/docs/extensions` lists every extension point this
  build serves, everything registered on each, and the settings each accepts.
  It is built from the registries and never edited by hand.
- [sdk] An extension declares the contract version it was built against, and a
  server running a different major version of that contract refuses it at
  start-up instead of at first use.
- `GET /api/v1/readiness` answers 200 while every resource instance declared
  `required` is serving, and 503 while one is not, naming which. It is a
  separate question from `GET /api/v1/health`, which stays a liveness probe:
  a node whose storage backend is retrying is behaving correctly and should
  be taken out of rotation, not restarted.
- A resource instance changing state publishes an event. A storage backend
  going degraded and coming back arrives at a subscriber as
  `com.prusa.connect.resource.state.changed` on that instance's own origin,
  the same way a printer's state does.
- `core.site` names this node in every event it publishes. It defaults to
  `local`, and a deployment that federates upward must set its own, because
  two nodes calling themselves the same thing publish events a parent cannot
  tell apart.
- Human authentication over OAuth 2.1 with PKCE. The issuer establishes who a
  person is once; every request after that resolves a `pcl_` credential this
  server minted, so the identity provider is not on the request path. A local
  mock issuer runs in the dev stack, so login works end to end without any
  test-only branch in the auth path.
- A `core` subcommand runs the new resource plane. Start it with
  `prusa-connect core --config config/config.yaml`. It serves the resource
  API, the OpenAPI document at `/openapi.json` and the reference at `/docs`, and
  supervises the instances declared in a new `resources:` block. A new `core:`
  block configures the listener, logging and the shutdown budget. Running the
  binary with no subcommand is unchanged.
- [sdk] `connectorapi.Register` selects a printer driver from configuration, the
  way `authapi` and `eventsapi` already do for their kinds. A duplicate
  registration and one arriving after the host built its registry are both
  errors, and the namespace is derived from the registering module.
- A reference connector, `adapters/connector/echo`, which is the first inbound
  driver and the shape to copy.
- Resource instances can be created and edited through the API. The operator's
  config file is never rewritten: a change is written to a machine-owned
  overlay the loader merges over it, so comments and formatting survive. An
  instance declared by hand wins, reports `source: file`, and the API refuses
  to edit it while naming the file to change instead.
- An HTTP API on a sealed router, with the OpenAPI document and a Scalar
  reference served from the binary. The document is at `/openapi.json` and the
  reference at `/docs`. Neither needs network access, so both work in an
  air-gapped install.
- Resource instances are readable and writable over the API. A write needs
  `If-Match` with the version from the read, or `If-None-Match: *` to create,
  and answers 409 when somebody changed the file in between. `?dry_run=true`
  validates without writing.
- [sdk] `sdk/connectorapi`, the contract a printer driver implements. It carries
  a capability set, one `Do(Command)` per session, and host-side argument
  validation, so a driver never sees an argument its descriptor does not
  permit. Six new error codes appear in the published error reference.

### Security

- A grant made after a printer had already been publishing no longer replays
  what that printer published before it. A page opened before any printer had
  dialled in, then granted one, received up to the last 32 events of that
  printer, including the model, firmware and serial number it reported on
  arrival. A grant now carries only what was published from the moment it was
  made.
- A `Last-Event-ID` naming a generation this server never issued is answered
  with silence for a caller who may observe nothing, rather than with a frame
  carrying this server's own generation. That value encodes the moment the
  process started, so anybody who could open the stream could read the start
  time to the millisecond and detect every restart.
- An event type outside the set this server publishes is refused before it
  reaches a subscriber, and a type carrying a newline or a colon is refused
  everywhere. A malformed type could previously write an extra frame into an
  event stream.
- A grant may no longer take access away from a principal who inherits more
  than the caller does. A manager delegated one printer or one resource kind
  can no longer block the administrator who delegated it to them. Naming them
  directly and naming a team they belong to are both refused.
- Withdrawing a grant takes the same containment check writing one takes.
  Severing the org tuple a printer reaches its administrators through is
  refused unless the caller holds as much through that org themselves.
- A printer upload that timed out, finished or failed deleted its staged file
  by the whole path it had been created under, so anything that changed a
  directory along that path between staging and cleanup redirected the delete.
  The staging directory is now opened first and the file unlinked through that
  handle.
- [sdk] A plugin could still take over a built-in storage backend, identity
  provider or event sink on the path the shipping server uses. The registry
  refused a duplicate registration, but the older lookup answered a bare name
  with the first match in namespace order, and any third-party module sorts
  ahead of the built-ins. A bare name two modules answer to is now refused and
  names both, and an operator resolves it by configuring the qualified name.
- `GET /api/v1/readiness` named every configured resource instance to anybody,
  with no credential. That answered, on one unauthenticated endpoint, the
  question every other surface refuses to answer. It now reports the verdict
  and the counts to any caller, which is what a probe reads, and names
  instances only to a caller who may already see them through
  `/api/v1/resources`.
- [sdk] Registering a storage backend, identity provider or event sink under a
  name another module already took replaced it silently, so a third-party
  plugin could take over a built-in just by naming it. Every registry now
  refuses the second registration and names the module each registrant came
  from, and registering after the server has built its tables is an error
  rather than a silent no-op.
- A command naming a file on a printer storage could climb out of it. The
  path was checked for `/../` as a substring, which misses the three shapes
  that carry the segment at an edge, so `../etc/passwd` reached the printer
  as `/usb/../etc/passwd`. Any `..` segment is now refused.
- [sdk] The host handed callers the live capability map it validates commands
  against. A caller could add a capability to it and have the driver carry the
  command out. `Capabilities()` returns a copy now, and a firmware version
  the comparator cannot parse no longer selects a profile: both were paths by
  which a printer appeared to support something it does not.
- [sdk] A storage provider handed callers a filesystem path after checking it
  was inside the store. The caller opened that path afterwards, so a link
  planted in between was followed. Providers now return an already-open handle
  and there is no name left to race. `LocalFilePath` is replaced by `OpenLocal`
  and `CreateLocal`, and the conformance suite requires both to refuse a path
  that leaves the store.
- The storage confinement reopened its root by path on every operation, so
  replacing the storage directory itself with a link defeated it. The root's
  identity is now pinned when the provider is built and checked on every use.
- A rename whose source was a link pointing out of the store skipped the
  extension allowlist, so a `.gcode` link could be renamed to `payload.sh`
  inside the store. A source that cannot be inspected is now refused.
- A path that escaped the store and a path that did not exist returned
  different errors, which told a caller which files in the store are links
  pointing outside it. Both now answer the same way, and as a client error
  rather than a server one.
- A symbolic link or a Windows directory junction inside a storage root let a
  request read, overwrite or delete files outside it. Every storage operation is
  now confined to its configured root and refuses a path that leaves it.
- [sdk] The storage conformance suite now requires providers to refuse a link
  that points out of their store. A filesystem-backed provider sets
  `storageapitest.Subject.Root` so the suite can plant one. A provider that
  leaves it unset must now say so with `NoFilesystemRoot`; a provider that
  declares neither fails the suite rather than silently skipping every
  security case and reporting a pass.

### Fixed

- `prusa-connect module add -approve` now grants the module what the
  permissions of its manifest name, so the module reads the printers of its
  server. The install prints each grant with everything its role carries, and
  `module remove` takes the grants back. Approving an install over HTTP writes
  no grant yet.

- An approved install refuses when the principal `core.principal` names cannot
  write the module's grants, so nothing is installed. Removing a module over
  `DELETE /api/v1/modules/<namespace>` takes its grants back, and installing a
  release whose manifest dropped a permission takes back the grant that
  permission justified.

- A dialog a printer raises now reaches a reader whose client is behind. A
  printer's session queues state patches for the server and drops the oldest
  when the queue is full. That is right for telemetry, because the next frame
  repeats it four times a second. A dialog is written once, when it moves, and
  repeated by nothing. Measured: a printer in ATTENTION raised a dialog and held
  it through 400 frames while nothing read the queue. The server was told
  nothing at `/dialog/code`, `/dialog/title`, `/dialog/text` or
  `/dialog/buttons`. An operator saw a machine waiting on a question with no
  question on it. Each of the five dialog paths now holds a slot the queue
  cannot evict. A newer value for a path replaces the one waiting there rather
  than queueing behind it. Telemetry still drops, and the dropped counter now
  counts only paths a later frame repeats.

- Four of the eight published binaries could not be built. The release image
  compiled the repository root. That directory has held no Go file since the
  entry point moved under `cmd/`, so a tag would have failed at the tag rather
  than here. The container image and the two minimal binaries were unaffected.
  A gate now reads every build file and refuses a `go build` whose package holds
  no command, so this cannot wait for a tag again.

- `GET /api/v1/printers` was capped at fifty printers with no way to reach the
  fifty-first, and the page it returned was then shortened again by every
  printer that happened to be away, so a page of fifty of which thirty were
  offline came back as twenty items and looked exactly like a fleet of twenty.
  It now takes `limit` and `offset`, answers with the total across every page
  and the window that was applied, and fills the page before it is cut. A client
  that sends neither parameter still gets the first page at the default size.
- One resource instance the supervision plane will not take no longer stops the
  server from starting. The declaration is accepted, the instance is reported as
  failing on its own status, and the rest of the configuration runs. An instance
  marked `required` still holds the readiness verdict down, which is the axis
  for deciding whether the process should be serving traffic.

- A `/dev?select=` link naming a printer that has not dialled in left the whole
  developer view dead. The panel it could not fill stopped the page before it
  subscribed to the event stream, so nothing on it ever updated again and the
  wire reported "connecting" for as long as it was open. The page now says which
  printer it could not find, keeps the selection for when that printer arrives,
  and opens the stream before it fills any panel, so an endpoint that refuses a
  request or never answers one can no longer strand it.
- The developer view showed the first page of the fleet and read like the whole
  of it, so a site with four hundred printers looked like a site with fifty. It
  now asks for as large a page as the server will grant and, when there are still
  more printers than it was given, says how many of how many it is showing.
- A fleet reconnecting at once made the developer view ask for the printer
  listing once per printer arriving. Four hundred printers coming back after a
  restart aimed four hundred requests at the server that was recovering. A burst
  of arrivals now costs one re-read.
- The published extension reference named four MQTT topics that no subscriber
  could match. It listed `v1/printers/{printer}/data/state` where the broker
  carries `v1/devices/printers/{printerId}/data/state`, and dropped `data/` from
  the per-job topic as well. The list is now read from the same table the sink
  publishes from, so the reference and the wire cannot disagree.
- A TLS handshake a printer refused was written to raw stderr and reached
  nothing else. It now goes through the configured logger, so it obeys
  `core.log.level` and `core.log.format` and names which listener the printer
  reached and the address it dialled from. The alert the printer sends says
  which of the three certificate failures it was, and the line now carries it:
  an untrusted CA, a name that does not match, and an expired certificate read
  differently.
- The core started on a printer certificate that had already expired, and every
  printer then failed to connect while the process reported itself healthy. A
  `core.printer.tls.certificate` outside its validity window is now refused
  before any port is bound, naming the date and the clock this machine reads,
  and one within 30 days of expiry warns at startup.
- `core.api.addr` and `core.printer.addr` naming one socket passed validation.
  On Linux the second bind failed and the process would not start; on Windows
  both bound and the whole API answered in the clear on the port the printer
  listener held a certificate for. The pair is now refused at load, and a
  wildcard address on either side counts as an overlap.
- A certificate chain written root-first was refused as a private key that does
  not match its certificate, which named a certificate the operator did not mean
  to present. The refusal now says the chain is in the wrong order and which
  certificate in the file the key belongs to.
- [sdk] An extension could declare that it requires a host capability whose name
  is not a capability key, and the value reached the published extension
  reference as a requirement no host can serve. A registration naming one is now
  refused where it is made, by the same check the capability vocabulary applies.
- A printer that dialled in while a browser was already reading
  `GET /api/v1/events` was never announced on that stream. The arrival now
  reaches a reader who was watching before the printer existed.
- `connected` on `GET /api/v1/printers` was always true. It now reports whether
  a session is open right now, so a printer that has been unplugged reads as
  away while the commands queued for it are still kept.
- A caller holding `send_command` and not `send_gcode` could reach the raw
  gcode verbs by naming one in the body of
  `POST /api/v1/printers/{printer}/commands`. The action a command takes is now
  derived from its verb wherever the command enters the server, rather than
  only on the HTTP path.
- A printer that reconnected kept the capability set resolved at its first
  connection, so a firmware update across a reconnection was never reflected.
  `capabilities`, `capability_epoch` and `profile_matched` now describe the
  firmware on the machine now.
- A printer never reported which storage devices it had, so nothing could
  address a file on anything but the configured default mountpoint. The server
  now asks each printer what it is as soon as it connects, and reads the answer
  whether or not the printer tagged it as a reply.
- One unreadable block in the `resources` section no longer takes the whole
  file with it. A typo, an unknown kind or a name that is not a label fails that
  instance, is named in the log, and every other instance in the file starts.
- A resource instance declaring `runtime` or `service` no longer stops the
  server. Those keys were read by the build-tag detector and refused by the
  running binary, so a configuration that built correctly would not start.
- A resource instance naming a runtime its kind cannot offer is refused at
  startup with the file and the key, instead of failing the first time
  something tries to use it.
- A printer that lost power or dropped off the network was never noticed. Its
  connection stayed open as far as the server was concerned, the offline grace
  period never started, and commands sent to it were discarded instead of being
  held until it came back. The server now checks each printer is still there on
  the same schedule the printer's firmware expects, and treats one that stops
  answering as disconnected.
- A printer whose model or firmware no capability profile covers was offered
  more commands than a recognised one, because an unrecognised printer kept
  everything its driver could theoretically send. It is now limited to what that
  driver's known profiles cover between them.
- The server accepted a nozzle diameter of zero and passed it to the printer,
  which refuses it. It is now refused before it is sent.
- A server whose authorization graph had been emptied handed the whole graph
  back to the principal named by `core.principal` on its next start, which on a
  deployment with no identity provider is every anonymous request. The first
  grant is recorded when it is written, so a graph somebody revoked stays
  revoked across a restart.
- Two servers starting against one authorization database at the same time
  could leave one refusing to start with a locked-database error. Writes to the
  graph take the database lock as they begin instead of upgrading to it partway.
- A printer's capability set carries a number that changes every time the
  printer reconnects, so a client holding a set from before a reconnect can
  tell that it is stale. The published advice to re-read the set on a reconnect
  described something the server did not do.
- Password verification did not compile for 32-bit ARM, so the armv7 and armhf
  builds were broken as soon as authentication was reachable from the binary.
- A write to a resource required a conditional header and did not require it to
  fence anything. A fabricated `If-None-Match` tag or `If-Match: *` from a
  caller that had never read the instance overwrote or deleted it. Both are
  answered 428 now, and a write needs an `If-Match` tag list or
  `If-None-Match: *`.
- Repeated `If-Match` and `If-None-Match` field lines are read as one list, and
  an empty list element is ignored, as the standard requires.
- `GET /api/v1/health` no longer refuses a query parameter it does not publish,
  so a cache-buster from a probe or a proxy is answered 200 rather than 422.
- [sdk] A number argument written in Go syntax such as `0x1p3` or `1_0.5` is
  refused rather than reaching a driver as text the host said it had validated.
  A non-finite or negative bound in a descriptor is reported by `Set.Problems`
  and refuses every value rather than silently disabling itself.
- A conforming HTTP client was locked out of every resource write. The `ETag`
  was sent unquoted, so sending it back in the form the standard requires was
  refused, permanently and with no way to recover.
- `?dryRun=true` was silently ignored and the write went through. Unknown query
  parameters are refused now, so a typo cannot be a live write.
- A failed `If-Match` answers `412` rather than `409`, which is what RFC 9110
  requires, and a create answers `201` with a `Location`.
- A database created before the migration checksum landed refused to start.
  The error named a missing column rather than saying what to do. The migration
  ledger now has a version of its own and upgrades in place.
- `authz_epoch.id` accepted a NULL on SQLite where Postgres refused one. The
  column was `INTEGER PRIMARY KEY`, which is the rowid alias, so SQLite
  substituted the next rowid whatever the constraint said.
- SQLite accepted a NULL in five primary key columns that Postgres refused.
  The authorization vocabulary tables are among them, and a NULL vocabulary
  entry is one no relation shape can reference. The schema parity gate found
  it.
- Eighteen error codes the server can return were documented nowhere. The
  gate that is supposed to catch that could not see them, because the
  generator only knows the codes in packages it imports and nothing checked
  that it imported them all.
- [sdk] `fault.Payload` marshalled but could not be unmarshalled. No consumer
  could decode the error an event envelope carries, or an RFC 9457 response
  body, back into the published type. `Code` and `Params` now have decoders,
  and `Params` keeps the order it was written in.
- Every `make build CONFIG_FILE=...` line in the documentation named a path that
  no longer exists. The example profiles moved to `config/examples/`, and the
  documented commands failed with a missing file.
- The SSE overview documented an envelope and event names the server stopped
  sending. Events are CloudEvents 1.0 with dotted type names, so every
  `addEventListener` example on the page listened for something that never
  fires.
- An upload that failed partway destroyed the file it was replacing and left a
  truncated file in its place. Writes now land under a temporary name and are
  renamed into place only once the whole transfer has been flushed to disk, so
  an interrupted upload leaves the previous file untouched.
- A full disk reported a successful upload. The error a filesystem raises when
  the last bytes are flushed was being discarded, so the API answered "created"
  for data that never reached the disk.
