### Changed
- GET /api/v1/devices/{device_id}/files no longer waits for the printer. It used to wait on a folder it held nothing for, and on `refresh=true`, and failed with 409 or 504. It now answers 200 within about a second with the rows this server holds. While the printer reads the folder, the answer carries `reading: true` and `reading_since`, and `items` is empty if nothing was held. A read that failed answers 200 with `fault`, such as `core.printer/LISTING_UNFINISHED` for a read the printer did not finish. `refresh=true` starts a new read and answers the held rows at once. A client that checks only the status code reads an empty folder as an empty drive. Check `reading` and `fault`, wait for `com.prusa.connect.device.files.changed` for that folder, then ask again without `refresh`.
- GET /api/v1/devices/{device_id}/storages?refresh=true waits at most about a second for the printer. It used to wait for a busy printer and fail with 504. It now answers 200 with what the printer last said. A later answer that differs is published as `com.prusa.connect.device.storages.changed`, so a client that needs the current drives listens for that event.

### Added
- [sdk] connectorapi.FileChange is one entry of a device's file tree that the device said changed.
- The event com.prusa.connect.device.files.changed says when a printer folder starts being read and when its listing lands or changes.
- A printer's Files tab opens on rows already read, because the server reads each drive's root when the printer connects or a drive is put in.
- The demo takes `-listing-delay`, how long each simulated printer takes to list a folder. It answers no other command meanwhile, as a real printer does, so `-listing-delay 13s` shows what a full USB drive costs.

### Fixed
- The Files tab of a printer shows the rows the server holds at once and updates them when the printer's read lands.
- The Files tab says when the printer is reading its drive, counts the seconds, and offers Retry after a failed read.
- A send, a delete or a new folder on a printer no longer makes the Files tab read the whole drive again.
- A file sent to the root of a printer's USB drive finishes seconds sooner, because the printer is asked about the file instead of the whole folder.
- Sending several files to a printer in a row no longer waits behind a background read of its drive between them.
- Opening a printer folder no longer has the printer read it again, so a send started meanwhile does not wait 12 to 15 s behind that read. The Files tab offers Refresh for a change made on the printer's screen.
- A file uploaded, deleted or created on a printer through another client, such as PrusaLink, shows in the Files tab without a read of its folder. The server applies the printer's own file events.
- File pictures load when their row comes into view and two at a time, so a busy printer no longer stalls the rest of the page.
