// Package winservice runs the server as a Windows service: a program the
// Windows service control manager starts at boot, stops at shutdown and starts
// again when it fails. The service control manager is the part of Windows that
// does for a service what systemd does on Linux.
//
// A service has no terminal and starts in the system folder, so this package
// also holds the log file the service writes. The rules are in
// ADR-20261125-jqp.
package winservice

// Name is the name the installer registers the service under. `sc query
// PrusaConnect` reads it.
const Name = "PrusaConnect"

// StopWaitHint is how long, in milliseconds, the service control manager is
// told a stop may take. The server drains its listeners for
// core.shutdown_timeout, 15s by default, so this is longer, as TimeoutStopSec
// is in the systemd unit.
const StopWaitHint = 30_000
