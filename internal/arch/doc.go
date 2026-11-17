// Package arch holds the dependency law from ADR-20260824-kkr, ADR-20260824-gfr, ADR-20260824-dym and
// ADR-20260825-n55 as data, and the tests that enforce it against the real import
// graph.
//
// The law is a table in one place rather than a lint configuration, so a change
// to it is a diff a reviewer reads next to the ADR it comes from. The gate runs
// under `go test`, which means CI cannot pass it by failing to install a tool.
//
// The graph is read once per tag set and platform the build recipes compute,
// and unioned. A single untagged `go list` polices the one configuration this
// project never ships.
//
// Two things here are importable, both for the same reason: a generator under
// cmd/ has to answer the question the gate beside it asks, and a second copy of
// the answer drifts.
//
//   - the recipe reader in recipes.go: what the project compiles, read out of
//     the recipes that compute it. cmd/generate-error-codes needs it, and the
//     copy it kept instead named three tag sets, none of them the one the image
//     ships
package arch
