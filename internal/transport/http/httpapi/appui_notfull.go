//go:build !full

package httpapi

import "io/fs"

// A build that is not the full one carries no app. The origin root falls
// through to a module's root mount and then to this server's own refusal.
func appBundle() (fs.FS, bool) { return nil, false }
