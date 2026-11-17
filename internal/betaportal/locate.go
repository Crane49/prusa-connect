package betaportal

import (
	"errors"
	"os"
	"path/filepath"
)

// EarlierPath returns the pairing file release 0.0.1-alpha.7 kept in config.d
// beside the configuration file.
func EarlierPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "config.d", pairingFile)
}

// Locate returns the pairing file a server uses, as Resolve decides it. Like
// Resolve, it sets an earlier file to the pairing file mode, whether it uses
// that file or not.
func Locate(configPath, overlay string) string {
	path, _ := Resolve(configPath, overlay)

	return path
}

// Resolve decides which pairing file a server uses, so the server, the backup
// and the restore all name one file. Release 0.0.1-alpha.7 kept the pairing
// in config.d beside the configuration file, and a server keeps using that
// file while Open can use it without moving it. Every other server keeps the
// pairing beside the resource overlay, in the folder the server owns. Nothing
// is copied between the two folders.
//
// The error says why an earlier file that exists is not used: it cannot be
// read, decoded or protected. The file is then left where it is. Resolve sets
// an earlier file to the pairing file mode, as Open does, so a backup that
// looks the file up also protects it.
func Resolve(configPath, overlay string) (string, error) {
	earlier, beside := EarlierPath(configPath), Path(overlay)
	// Where the overlay sits in config.d, both are one file, and Open alone
	// decides what to do with it.
	if sameFolder(filepath.Dir(earlier), filepath.Dir(beside)) {
		return beside, nil
	}

	err := usable(earlier)
	switch {
	case err == nil:
		return earlier, nil
	case errors.Is(err, os.ErrNotExist):
		return beside, nil
	default:
		return beside, err
	}
}

// sameFolder reports whether two paths name one folder, however each is
// spelled. A folder that does not exist yet is compared by its absolute path.
func sameFolder(one, other string) bool {
	oneInfo, oneErr := os.Stat(one)
	otherInfo, otherErr := os.Stat(other)
	if oneErr == nil && otherErr == nil {
		return os.SameFile(oneInfo, otherInfo)
	}
	oneAbs, oneErr := filepath.Abs(one)
	otherAbs, otherErr := filepath.Abs(other)

	return oneErr == nil && otherErr == nil && oneAbs == otherAbs
}

// usable checks what Open needs from an existing file: it reads, it decodes,
// and it is protected. A file that is not yet protected is protected here, as
// Open would do. A file that does not decode is protected too, because it may
// still hold the key and the token.
func usable(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, _, err = decodeDocument(content); err != nil {
		// Only a file is protected, so a link to another file keeps that
		// file's mode.
		if info, statErr := os.Lstat(path); statErr == nil && info.Mode().IsRegular() {
			_ = protect(path)
		}

		return err
	}

	return protect(path)
}
