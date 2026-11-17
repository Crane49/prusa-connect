// The 3MF module: the reader of 3MF projects as a Go package other modules
// import, the process that answers the core and the project routes, and the
// project view in the browser bundle.
//
// It carries its own go.mod so that it builds the way a module from somebody
// else's repository builds: against the published sdk alone, outside the
// core's Go module. Another module links the reader by requiring this module
// and importing github.com/prusa3d/threemf/threemf.
module github.com/prusa3d/threemf

go 1.26.11

require github.com/prusa3d/prusa-connect/sdk v0.0.0-00010101000000-000000000000

// Until the sdk is tagged, the replace binds it to the checkout beside this
// directory, which is what a development build does. A released module
// requires a published sdk version and carries no replace.
replace github.com/prusa3d/prusa-connect/sdk => ../../sdk
