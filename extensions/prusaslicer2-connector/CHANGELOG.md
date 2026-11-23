# Changelog

What changed in this module, newest first. The top heading is the version
`module.yaml` declares and the version this process reports to the host.

## 0.1.0 - 2026-11-18

### Added

- PrusaSlicer 2.4 and later sends a print to each printer of this server as it sends one to a printer with PrusaLink. Each printer has its own address under the module, and PrusaSlicer signs in with an API credential of this server as the API key.
- Upload and Print sends the file to the printer and starts it. A printer that already holds a file of that name gets the file under a numbered name.
- Upload keeps the file in the `local` storage of this server, or in the storage the `storage` setting names, and prints nothing.
- PrusaSlicer 2.6 and later sends the file with PUT, and PrusaSlicer 2.5 and older with POST in a form. Both take the file on a staged route, up to the limit the operator approves at install, 1024 MiB as the module asks.
- A print to a printer that is not ready is refused before the printer is asked. So is a print with a key whose person may not operate the printer. PrusaSlicer shows the reason.
- The foot of each printer's Status tab shows the address to paste into PrusaSlicer and makes the API key, with the steps in PrusaSlicer.
- The module keeps a printed file for a day, so the printer can download it, and then removes it.
- Needs a Prusa Connect build that takes an API credential in `X-Api-Key` and passes a header a module asked for: 0.1.0-beta.2 or later.
