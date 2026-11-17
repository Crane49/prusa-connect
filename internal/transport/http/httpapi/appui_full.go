//go:build full

package httpapi

import (
	"embed"
	"io/fs"
	"sync"
)

// appBundleDir is the directory the app's build output is copied into before
// `go build` runs. `make build-full` writes it from frontend/dist, and a
// committed placeholder keeps the directory present so this file compiles in a
// checkout nobody has built the app in.
const appBundleDir = "appdist"

// The full build carries the core's own app. `all:` embeds the whole tree.
// Without it every name beginning with a dot or an underscore is dropped, and
// a bundler may write either.
//
//go:embed all:appdist
var appEmbed embed.FS

// appBundle is the embedded app, rooted at its entry document. It is read once,
// because the answer cannot change while the process runs.
var appBundle = sync.OnceValues(func() (fs.FS, bool) { //nolint:gochecknoglobals // one embedded tree, read once.
	return appBundleFS(appEmbed, appBundleDir)
})
