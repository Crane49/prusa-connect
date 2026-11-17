// A Prusa Fractal network of three nodes in one process.
//
// This folder carries its own go.mod, so `go build ./...` in the core skips it
// and a reader who clones only this folder builds what they see. The replace
// is what a development build does before the sdk is tagged; a released
// example requires a published sdk version and carries none.
//
// There is no third-party requirement and no go.sum: sdk/ pulls in nothing
// beyond the standard library, and the in-memory transport this example runs
// on is part of sdk/ too.
module example.com/prusa/fractal-tree

go 1.26.11

require github.com/prusa3d/prusa-connect/sdk v0.0.0

replace github.com/prusa3d/prusa-connect/sdk => ../../sdk
