// A module that receives the syslog lines and metrics a Prusa printer sends
// over UDP, keeps them for a day, serves them to its page, and adds them to
// the diagnostics bundle.
//
// It carries its own go.mod so that it builds the way a module from somebody
// else's repository builds: against the published sdk alone, outside the
// core's Go module.
//
// Until the sdk is tagged, the replace below binds it to the checkout beside
// this directory, which is what a development build does. A released module
// requires a published sdk version and carries no replace.
module github.com/prusa3d/buddy-debug-logs

go 1.26.11

require github.com/prusa3d/prusa-connect/sdk v0.0.0-00010101000000-000000000000

replace github.com/prusa3d/prusa-connect/sdk => ../../sdk
