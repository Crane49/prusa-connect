// The versions a module written outside a checkout asks for.

package main

// The versions of this tree. They must match sdk/go.mod and
// packages/*/package.json. A module outside a checkout gets the packages from
// the local registry: run `make registry-up` and `make registry-publish
// VERSION_TAG=0.1.0` in a checkout first.
const (
	// publishedGo is the go directive of sdk/go.mod.
	publishedGo = "1.26.11"
	// publishedSDK is the sdk version a module's go.mod requires.
	publishedSDK = "v0.1.0"
	// publishedPackages is the version of @prusa/connect and
	// @prusa/connect-ui.
	publishedPackages = "0.1.0"
)

// oldestNode must match the library's engines entry. checkTools enforces it.
// oldestPnpm must match the library's packageManager pin. It is not enforced.
const (
	oldestNode = "22.12.0"
	oldestPnpm = "11.3.0"
)
