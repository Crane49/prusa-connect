### Changed
- Printers nearby lists a machine that runs a Prusa Connect server once, as that server. A printer that runs a server beside its PrusaLink is no longer offered under Add printer.
### Added
- `core.discovery.host_records: system` lets another mDNS responder on the machine, such as Avahi, answer for its host name. The server then announces only its services, so discovery can stay on there without the machine being renamed to `<hostname>-2.local`.
