// The in-memory storage provider as a module of its own, outside the core
// module.
//
// It used to live at adapters/storage/memory, compiled into every binary and
// admitted by a blank import plus a row in the composition root's adoption
// list. Nothing about it needed that: it imports sdk/extension and
// sdk/storageapi and nothing else, which is the whole of what ADR-20260825-n55 rule 2
// asks of an adapter. So it is here instead, as a worked example of
// ADR-20260901-7t4, and the first one that used to be in the tree. What
// the move demonstrates is that the boundary is real in both directions: a
// provider can leave the core without the core gaining a compatibility shim.
//
// This directory carries its own go.mod, so `go build ./...` in the core skips
// it and Go itself refuses an `internal/` import from here. A published adapter
// lives in its author's own repository and is named in a build configuration by
// version; this one is named by path, which is what ADR-20260901-7t4 rule 15 calls
// path mode.
//
// The replace is what a development build does before the sdk is tagged. A
// released adapter requires a published sdk version and carries no replace,
// and the composition module is where sdk resolution lives either way, because
// a replace binds only in the main module.
//
// There is no third-party requirement and no go.sum, which is not an accident:
// sdk/ pulls in nothing beyond the standard library (ADR-20260824-gfr rule 2), and the
// conformance suite this module's tests run, sdk/storageapi/storageapitest, is
// part of sdk/. An adapter that needs no assertion library needs no supply
// chain.
module example.com/prusa/storage-memory

go 1.26.11

require github.com/prusa3d/prusa-connect/sdk v0.0.0

replace github.com/prusa3d/prusa-connect/sdk => ../../../sdk
