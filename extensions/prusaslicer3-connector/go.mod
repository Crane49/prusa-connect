// A module that lets PrusaSlicer 3 use this server the way it uses Prusa
// Account and Prusa Connect.
//
// It carries its own go.mod so that it builds the way a module from somebody
// else's repository builds: against the published sdk, and golang.org/x/image
// for the font the avatar is drawn in.
//
// Until the sdk is tagged, the replace below binds it to the checkout beside
// this directory.
module github.com/prusa3d/prusaslicer3-connector

go 1.26.11

require (
	github.com/prusa3d/prusa-connect/sdk v0.0.0-00010101000000-000000000000
	golang.org/x/image v0.48.0
)

require (
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
)

replace github.com/prusa3d/prusa-connect/sdk => ../../sdk
