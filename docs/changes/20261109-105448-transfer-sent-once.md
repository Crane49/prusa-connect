### Changed
- The server log says at INFO when a printer's connection ends, and why.

### Fixed
- [sdk] A transfer's `bytes_sent` counts a byte the printer asks for again once, so a transfer's progress no longer passes 100 %. A printer that started a file over on each connection was shown at over 1000 %.
