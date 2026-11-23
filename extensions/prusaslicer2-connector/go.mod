// A module that lets PrusaSlicer 2 send a print to the printers of this server
// as it sends one to a printer with PrusaLink.
//
// It carries its own go.mod so that it builds the way a module from somebody
// else's repository builds: against the published sdk.
//
// Until the sdk is tagged, the replace below binds it to the checkout beside
// this directory.
module github.com/prusa3d/prusaslicer2-connector

go 1.26.11

require github.com/prusa3d/prusa-connect/sdk v0.0.0-00010101000000-000000000000

replace github.com/prusa3d/prusa-connect/sdk => ../../sdk
