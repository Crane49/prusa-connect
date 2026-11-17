package diskstore

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// metaDir is the reserved directory at the store root. It mirrors the
	// object tree and has one digest file per object. Digests are files
	// because not every filesystem has extended attributes.
	metaDir = ".pcl-meta"
	// metaSuffix is appended to the mirrored object path. The file contains
	// only the lowercase hex digest.
	metaSuffix = ".sha256"
	// digestLength is the length of that hex. A file of any other length is
	// not reported as a digest.
	digestLength = 64
)

// reserved reports whether a caller-supplied name is inside metaDir. Such a
// name is refused.
func reserved(clean string) bool {
	return clean == metaDir || strings.HasPrefix(clean, metaDir+"/")
}

// sidecarOf is where the digest of the object at target is kept.
func sidecarOf(target string) string {
	return filepath.Join(metaDir, target+metaSuffix)
}

// mirrorOf is where the digests of everything under a directory are kept.
func mirrorOf(target string) string {
	return filepath.Join(metaDir, target)
}

// writeSidecar records the digest of an object. A failure is ignored: the
// object is already committed, and only a later Stat loses the digest.
func writeSidecar(root *os.Root, target, sum string) {
	sidecar := sidecarOf(target)
	if err := root.MkdirAll(filepath.Dir(sidecar), dirMode); err != nil {
		return
	}

	f, err := root.OpenFile(sidecar, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return
	}

	_, _ = io.WriteString(f, sum)
	_ = f.Close()
}

// readSidecar returns the digest recorded for target, or "" when there is
// none. A sidecar older than the object is also "": the object was rewritten
// in place after the digest was taken. The store writes the sidecar after the
// object, so an equal time is a current digest.
func readSidecar(root *os.Root, target string, objectModified time.Time) string {
	f, err := root.Open(sidecarOf(target))
	if err != nil {
		return ""
	}

	defer func() { _ = f.Close() }()

	recorded, err := f.Stat()
	if err != nil || recorded.ModTime().Before(objectModified) {
		return ""
	}

	// Read at most one digest, so a planted large file cannot use up memory.
	raw, err := io.ReadAll(io.LimitReader(f, digestLength+1))
	if err != nil {
		return ""
	}

	sum := strings.TrimSpace(string(raw))
	if len(sum) != digestLength {
		return ""
	}

	return sum
}

// removeSidecar removes an object's digest, or a directory's mirrored
// subtree. A failure is ignored: the delete of the object already succeeded.
func removeSidecar(root *os.Root, target string) {
	_ = root.RemoveAll(sidecarOf(target))
	_ = root.RemoveAll(mirrorOf(target))
}

// moveSidecar moves an object's digest to its new name. A failure is ignored:
// the rename of the object already succeeded.
func moveSidecar(root *os.Root, source, destination string) {
	move(root, sidecarOf(source), sidecarOf(destination))
	move(root, mirrorOf(source), mirrorOf(destination))
}

func move(root *os.Root, from, to string) {
	if _, err := root.Lstat(from); err != nil {
		return
	}

	if err := root.MkdirAll(filepath.Dir(to), dirMode); err != nil {
		return
	}

	_ = root.RemoveAll(to)
	_ = root.Rename(from, to)
}
