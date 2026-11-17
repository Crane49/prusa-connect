// A module that slices a 3MF project into G-code with the PrusaSlicer command
// line, and writes the result into one of the server's storages.
//
// It carries its own go.mod so that it builds the way a module from somebody
// else's repository builds: against the published sdk alone, outside the
// core's Go module. It reads and rewrites projects with the reader of the
// threemf module, which this file requires for the same reason.
//
// Until the sdk and the threemf module are tagged, the replaces below bind
// them to the checkouts beside this directory, which is what a development
// build does. A released module requires published versions and carries no
// replace.
module github.com/prusa3d/server-slicing

go 1.26.11

require (
	github.com/prusa3d/prusa-connect/sdk v0.0.0-00010101000000-000000000000
	github.com/prusa3d/threemf v0.0.0-00010101000000-000000000000
)

replace (
	github.com/prusa3d/prusa-connect/sdk => ../../sdk
	github.com/prusa3d/threemf => ../threemf
)
