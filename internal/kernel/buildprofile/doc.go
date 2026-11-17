// Package buildprofile holds the default values that differ between the
// normal build and the printer build.
//
// The printer build is the binary a printer runs on its own board, where
// Prusa Connect has 15 to 20 MB of memory. The `printer` build tag selects
// printer.go, and every other build compiles normal.go. Both files declare the
// same names with different values. A setting whose default is smaller on a
// printer reads that default here.
//
// The tag changes values only. It removes no code and no route, and
// internal/arch refuses a `printer` constraint on any file outside this
// package.
package buildprofile
