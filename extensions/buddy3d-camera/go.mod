// The server a Buddy3D camera talks to instead of Prusa's cloud, as a module
// of Prusa Connect.
//
// It carries its own go.mod so that it builds the way a module from somebody
// else's repository builds: against the sdk alone, outside the core's Go
// module. Until the sdk is tagged with camera.accept, the replace below binds
// it to the checkout beside this directory.
module github.com/prusa3d/buddy3d-camera

go 1.26.11

replace github.com/prusa3d/prusa-connect/sdk => ../../sdk

require (
	github.com/bufbuild/protocompile v0.14.1
	github.com/coder/websocket v1.8.15
	github.com/prusa3d/prusa-connect/sdk v0.0.0-00010101000000-000000000000
	google.golang.org/protobuf v1.36.12
)

require golang.org/x/sync v0.8.0 // indirect
