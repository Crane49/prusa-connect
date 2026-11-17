// The fixture module ADR-20260901-7t4's joint gate composes. It is a module of its own,
// exactly as a community adapter is, and it compiles against sdk/ and the
// standard library alone: Go itself refuses an internal/ import from here.
//
// The replace is what a development build does before the sdk is tagged. A
// released adapter requires a published sdk version and carries no replace,
// and the composition module is where sdk resolution lives either way, because
// a replace binds only in the main module.
module example.com/composefixture

go 1.26.11

require github.com/prusa3d/prusa-connect/sdk v0.0.0

replace github.com/prusa3d/prusa-connect/sdk => ../../../sdk
