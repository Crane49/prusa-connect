//go:build !full

package hostfeature

// Every other build carries no application, so a module that needs the slots
// is told this host has none instead of being started against nothing.
const fullBuild = false
