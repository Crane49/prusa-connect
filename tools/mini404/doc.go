// Package mini404 launches one MINI404 emulated Prusa printer and holds the
// handles a test needs to drive it: the script console, QMP, and the forwarded
// PrusaLink port.
//
// MINI404 is a QEMU fork that emulates the STM32F4 Buddy and xBuddy boards at
// instruction level, so the firmware running under it is the real firmware
// image, not a model of one. That is the whole value and the whole cost. What
// this package produces is a transcript of what current firmware actually
// sends, which no simulator can establish.
//
// This package is never in a blocking lane. A real socket and a real process
// leave virtual time, so nothing here fits inside a testing/synctest bubble,
// and one emulated printer is one QEMU process at roughly real-time speed. The
// lane that gates a pull request drives internal/printer/simulator instead.
// See earlier research.
//
// Artefacts are resolved, never built. A missing emulator or firmware image is
// an error naming the script that produces it, so a test lane fails with an
// instruction rather than starting a twenty-minute compile nobody asked for.
package mini404
