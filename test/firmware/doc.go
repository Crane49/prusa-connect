// Package firmware drives real Prusa firmware, running under the MINI404
// emulator, against the built binary over real sockets.
//
// It is the only lane whose printer is not a model of a printer. test/fleet
// drives internal/printer/simulator over net.Pipe and test/e2e drives it over a
// real listener, and both are this project's reading of the wire. Here the wire
// is produced by the firmware image itself, so what this lane establishes is
// what current firmware actually sends: its own network stack bringing a link
// up, its own Connect client dialling and being admitted, and its own idea of
// when a session has ended.
//
// # Why it never gates a pull request
//
// A separate process on a real socket cannot be in a testing/synctest bubble,
// because the package excludes network I/O from durable blocking
// (earlier research). So
// this lane runs in real time and pays real timeouts: one printer is one QEMU
// process at roughly real-time speed, and a boot is minutes. The second reason
// is honesty rather than cost. The vendor's own full-machine emulator test,
// test_printing_job, is marked flaky in the vendor's own suite (earlier research
// finding 5), and a zero-flake policy does not survive a lane that is flaky in
// the hands of the people who wrote the firmware.
//
// # How to run it
//
//	make test-firmware
//
// Without PCL_FIRMWARE=1 every scenario skips with one line naming that target.
//
// # What it needs
//
// The artefact cache under ${XDG_CACHE_HOME:-~/.cache}/pcl-mini404, holding the
// emulator and the firmware image the pin table in
// tools/mini404/knowngood.json names. Nothing is built here:
// scripts/build-mini404.sh produces the emulator and scripts/build-firmware.sh
// the image, and a miss is an error naming the script rather than a
// twenty-minute compile nobody asked for.
//
// The external flash image is bootstrapped once and then cached, because the
// first boot on a zeroed xflash has the firmware unpack its resources and
// format a filesystem before it serves anything. Every run copies the cached
// image and none mutates it.
//
// # Blocked scenarios
//
// A scenario whose assertion cannot yet be made honestly is written rather than
// stubbed, listed in blocked.json with the reason, and skipped while it is
// listed. Each entry names a probe, and
// TestNothingIsStillBlockedThatHasBeenWired fails the moment a probe starts
// passing while its scenario is still listed. A suite that quietly skips is how
// a lane goes green for a year without running.
package firmware
