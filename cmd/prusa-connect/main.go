// Package main is the entry point for Prusa Connect.
//
// It only calls compose.Main, the same entry point a composed build calls. The
// shipped binary is a composition with no extra adapters.
//
// The argument `core` starts the server. It is optional. compose handles
// every other argument.
package main

import (
	"os"

	"github.com/prusa3d/prusa-connect/compose"
)

func main() {
	os.Exit(compose.Main())
}
