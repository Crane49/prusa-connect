package wirehost

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ErrArtefactMissing reports an install directory with no artefact in it for
// the operating system and architecture this host runs on. The message names
// the whole path, so a person reads both where the host looked and what it
// expected to find there.
var ErrArtefactMissing = errors.New("wirehost: the install directory holds no artefact for this platform")

// windowsSuffix is the extension an executable carries on Windows and nowhere
// else.
const windowsSuffix = ".exe"

// Artefact names the file a host launches out of an install directory.
//
// The file is <name>_<GOOS>_<GOARCH>, with .exe on Windows. An author's release
// workflow already names its files this way, so one directory can hold every
// platform's build of one extension and each host picks its own.
func Artefact(dir, name string) string {
	file := name + "_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		file += windowsSuffix
	}

	return filepath.Join(dir, file)
}

// Find returns the artefact in dir for this platform.
//
// A directory with builds for other platforms and not this one is what an
// operator gets from installing the wrong release, so the refusal names the
// file this host wanted and not the directory it read.
func Find(dir, name string) (string, error) {
	path := Artefact(dir, name)

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrArtefactMissing, path, err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("%w: %s is a directory", ErrArtefactMissing, path)
	}

	return path, nil
}
