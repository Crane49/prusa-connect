### Fixed
- [sdk] `device.print` of a file the printer already holds fails with `deviceapi.ErrPrintRefused`, code 3010, when the printer refuses to start it. A module was answered as if the print had begun. The message carries the printer's own reason where it gave one.
- [sdk] `device.print` of a file the printer already holds fails with `deviceapi.ErrPrintFailed`, code 3011, when the printer takes the start command and then fails it. A module read that the host could not answer. The printer may have begun, so a module reads its state before it starts the print again.
