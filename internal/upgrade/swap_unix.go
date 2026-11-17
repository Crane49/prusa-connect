//go:build !windows

package upgrade

import "os"

// replaceExecutable renames source over destination. POSIX rename replaces
// the name atomically, even while destination is the running image.
func replaceExecutable(source, destination string) error {
	return os.Rename(source, destination)
}
