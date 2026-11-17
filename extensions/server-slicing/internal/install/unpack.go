package install

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/server-slicing/internal/catalog"
)

// ErrArchive reports an archive this module refuses to unpack: an entry that
// leaves the directory, a link that points out of it, or more than the bounds
// below.
var ErrArchive = errors.New("the archive cannot be unpacked")

// The bounds on one unpacked build. The largest public PrusaSlicer build
// unpacks to well under a gigabyte in a few thousand files.
const (
	maxUnpackedBytes int64 = 4 << 30
	maxEntries             = 100_000
)

// unpackFile is the file name an ArchiveFile build is kept under.
const unpackFile = "prusa-slicer"

// The permissions of what is unpacked, and the longest link target read.
const (
	dirPerm        fs.FileMode = 0o750
	filePerm       fs.FileMode = 0o644
	executablePerm fs.FileMode = 0o755
	ownerReadWrite fs.FileMode = 0o600
	maxLinkBytes               = 4096
)

// Unpack puts the build's download into dest, which must not exist yet, and
// returns the path of the executable inside it.
func Unpack(ctx context.Context, build catalog.Build, archive, dest string) (string, error) {
	if err := os.MkdirAll(dest, dirPerm); err != nil {
		return "", fmt.Errorf("make the directory for the build: %w", err)
	}

	root, err := os.OpenRoot(dest)
	if err != nil {
		return "", fmt.Errorf("open the directory for the build: %w", err)
	}

	defer func() { _ = root.Close() }() // only a handle; nothing was written through Close

	binary := build.Binary

	switch build.Archive {
	case catalog.ArchiveZip:
		err = unzip(archive, root)
	case catalog.ArchiveTarGz:
		err = untar(archive, root, gzipped)
	case catalog.ArchiveTarBz2:
		err = untar(archive, root, bzipped)
	case catalog.ArchiveDMG:
		binary, err = undmg(ctx, archive, dest, build.Binary)
	case catalog.ArchiveFile:
		binary = unpackFile
		err = copyInto(archive, root, unpackFile, executablePerm)
	default:
		err = fmt.Errorf("%w: the archive kind %q", ErrArchive, build.Archive)
	}

	if err != nil {
		return "", err
	}

	// A GitHub release does not say where the executable is in its archive,
	// so the install looks for it. The version probe that follows checks it
	// is the one the release names.
	if binary == "" {
		if binary, err = findBinary(dest); err != nil {
			return "", err
		}
	}

	executable := filepath.Join(dest, filepath.FromSlash(binary))

	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: it holds no file at %s", ErrArchive, binary)
	}

	// A zip keeps no execute bit for a Windows build, and a Unix one needs it.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		if err = os.Chmod(executable, info.Mode().Perm()|0o100); err != nil {
			return "", fmt.Errorf("mark the slicer executable: %w", err)
		}
	}

	return executable, nil
}

// budget counts what an archive unpacks to.
type budget struct {
	entries int
	bytes   int64
}

func (b *budget) take(size int64) error {
	b.entries++
	b.bytes += max(size, 0)

	if b.entries > maxEntries {
		return fmt.Errorf("%w: more than %d entries", ErrArchive, maxEntries)
	}

	if b.bytes > maxUnpackedBytes {
		return fmt.Errorf("%w: more than %d bytes unpacked", ErrArchive, maxUnpackedBytes)
	}

	return nil
}

func unzip(archive string, root *os.Root) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	defer func() { _ = reader.Close() }() // read-only

	var counted budget

	for _, entry := range reader.File {
		if err = counted.take(int64(entry.UncompressedSize64)); err != nil { //nolint:gosec // bounded by take
			return err
		}

		name := strings.TrimSuffix(entry.Name, "/")
		if !catalog.InsidePath(name) {
			return fmt.Errorf("%w: the entry %q leaves the directory", ErrArchive, entry.Name)
		}

		mode := entry.Mode()

		switch {
		case mode.IsDir():
			err = root.MkdirAll(name, dirPerm)
		case mode&fs.ModeSymlink != 0:
			err = zipLink(entry, root, name)
		case mode.IsRegular():
			err = zipFile(entry, root, name, mode.Perm())
		default:
			err = fmt.Errorf("%w: the entry %q is neither a file, a folder nor a link", ErrArchive, entry.Name)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func zipFile(entry *zip.File, root *os.Root, name string, perm fs.FileMode) error {
	content, err := entry.Open()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	defer func() { _ = content.Close() }() // read-only

	return writeInto(content, root, name, perm, int64(entry.UncompressedSize64)) //nolint:gosec // bounded by take
}

func zipLink(entry *zip.File, root *os.Root, name string) error {
	content, err := entry.Open()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	defer func() { _ = content.Close() }() // read-only

	target, err := io.ReadAll(io.LimitReader(content, maxLinkBytes))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	return link(root, name, string(target))
}

// gzipped and bzipped open the compression around a tar file.
func gzipped(file io.Reader) (io.Reader, error) { return gzip.NewReader(file) }

func bzipped(file io.Reader) (io.Reader, error) { return bzip2.NewReader(file), nil }

func untar(archive string, root *os.Root, open func(io.Reader) (io.Reader, error)) error {
	file, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	defer func() { _ = file.Close() }() // read-only

	unpacked, err := open(file)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	reader := tar.NewReader(unpacked)

	var counted budget

	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			return nil
		}

		if nextErr != nil {
			return fmt.Errorf("%w: %w", ErrArchive, nextErr)
		}

		if err = counted.take(header.Size); err != nil {
			return err
		}

		name := strings.TrimSuffix(strings.TrimPrefix(header.Name, "./"), "/")
		if name == "" || name == "." {
			continue
		}

		if !catalog.InsidePath(name) {
			return fmt.Errorf("%w: the entry %q leaves the directory", ErrArchive, header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			err = root.MkdirAll(name, dirPerm)
		case tar.TypeSymlink:
			err = link(root, name, header.Linkname)
		case tar.TypeReg:
			err = writeInto(reader, root, name, permOf(header.Mode), header.Size)
		default:
			err = fmt.Errorf("%w: the entry %q is neither a file, a folder nor a link", ErrArchive, header.Name)
		}

		if err != nil {
			return err
		}
	}
}

// permOf is the permission bits of a tar header's mode, and 0644 for a mode
// outside them.
func permOf(mode int64) fs.FileMode {
	if mode < 0 || mode > int64(fs.ModePerm) {
		return filePerm
	}

	return fs.FileMode(mode).Perm()
}

// link makes a symbolic link whose target stays inside the build.
func link(root *os.Root, name, target string) error {
	if path.IsAbs(target) || strings.Contains(target, "\\") {
		return fmt.Errorf("%w: the link %q points outside the build", ErrArchive, name)
	}

	if resolved := path.Join(path.Dir(name), target); !catalog.InsidePath(resolved) {
		return fmt.Errorf("%w: the link %q points outside the build", ErrArchive, name)
	}

	if err := root.MkdirAll(path.Dir(name), dirPerm); err != nil && path.Dir(name) != "." {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	if err := root.Symlink(target, name); err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	return nil
}

// writeInto writes one file of an archive, at most size bytes of it.
func writeInto(content io.Reader, root *os.Root, name string, perm fs.FileMode, size int64) error {
	if dir := path.Dir(name); dir != "." {
		if err := root.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("%w: %w", ErrArchive, err)
		}
	}

	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm|ownerReadWrite)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	_, err = io.Copy(file, io.LimitReader(content, size))

	// A full disk can report itself on Close alone.
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return fmt.Errorf("unpack %s: %w", name, err)
	}

	return nil
}

func copyInto(source string, root *os.Root, name string, perm fs.FileMode) error {
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	defer func() { _ = file.Close() }() // read-only

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	return writeInto(file, root, name, perm, info.Size())
}

// dmgTimeout bounds attaching, copying out of and detaching one disk image.
const dmgTimeout = 10 * time.Minute

// attachTries and attachPause are how often an attach is tried, and how long
// it waits before the next try.
//
// hdiutil hands the attach to the system's disk image service. When that
// service cannot be reached for a moment, hdiutil answers "Device not
// configured" (ENXIO), and the same attach works a few seconds later. The
// same message is what a process gets when a sandbox denies it the service
// for good, so the tries end and the message says so.
const attachTries = 4

//nolint:gochecknoglobals // a pause the tests shorten; nothing else writes it.
var attachPause = 3 * time.Second

// hdiutil runs /usr/bin/hdiutil with a C locale, so its message reaches a job
// in English whatever language the server's user reads. Tests replace it.
//
//nolint:gochecknoglobals // a seam the tests replace; nothing else writes it.
var hdiutil = func(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "/usr/bin/hdiutil", args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")

	return command.CombinedOutput()
}

// attaching lets one disk image be attached at a time. Two installs that
// attach at once each ask the disk image service, and one attach after the
// other is what a person does at a desk.
//
//nolint:gochecknoglobals // one lock for the process, as the service is one for the machine.
var attaching sync.Mutex

// undmg mounts a macOS disk image read-only, copies the application out of it
// with ditto, which keeps the signature and the links of an application
// bundle, and detaches it again. It answers the executable's path inside
// dest. A binary of "" is looked for: the one application bundle in the image.
func undmg(ctx context.Context, archive, dest, binary string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("%w: a dmg opens only on macOS", ErrArchive)
	}

	ctx, cancel := context.WithTimeout(ctx, dmgTimeout)
	defer cancel()

	attaching.Lock()
	defer attaching.Unlock()

	mount, err := os.MkdirTemp(filepath.Dir(dest), "mount-")
	if err != nil {
		return "", fmt.Errorf("make a mount point: %w", err)
	}

	defer func() { _ = os.Remove(mount) }() // empty once detached; a failed detach leaves it, and the log says so

	if err = attach(ctx, archive, mount); err != nil {
		return "", err
	}

	defer detach(mount)

	if binary == "" {
		if binary, err = findApplication(mount); err != nil {
			return "", err
		}
	}

	top, _, _ := strings.Cut(binary, "/")

	//nolint:gosec // a folder of the image, under a mount point this function made
	copyOut := exec.CommandContext(ctx, "/usr/bin/ditto", filepath.Join(mount, top), filepath.Join(dest, top))
	if out, copyErr := copyOut.CombinedOutput(); copyErr != nil {
		return "", fmt.Errorf("%w: ditto: %w: %s", ErrArchive, copyErr, tail(out))
	}

	return binary, nil
}

// findApplication is the executable of the PrusaSlicer bundle in a mounted
// image, one or two folders down: PrusaSlicer-3.0.0-alpha12.app, or Original
// Prusa Drivers/PrusaSlicer.app. A link, such as the one to /Applications, is
// not followed, so an application this machine has installed is never taken.
func findApplication(mount string) (string, error) {
	for _, pattern := range []string{"*.app/Contents/MacOS/PrusaSlicer", "*/*.app/Contents/MacOS/PrusaSlicer"} {
		found, err := filepath.Glob(filepath.Join(mount, pattern))
		if err != nil {
			continue
		}

		for _, candidate := range found {
			relative, relErr := filepath.Rel(mount, candidate)
			if relErr == nil && noLinkOnTheWay(mount, relative) {
				return filepath.ToSlash(relative), nil
			}
		}
	}

	return "", fmt.Errorf("%w: the image holds no PrusaSlicer application", ErrArchive)
}

// noLinkOnTheWay reports a regular file at relative under root, reached
// through no link. A glob follows a link, so the link a disk image holds to
// /Applications would otherwise lead to an application this machine has
// installed, and the copy to all of /Applications.
func noLinkOnTheWay(root, relative string) bool {
	path := root

	parts := strings.Split(relative, string(filepath.Separator))
	for i, part := range parts {
		path = filepath.Join(path, part)

		info, err := os.Lstat(path)
		if err != nil || info.Mode()&fs.ModeSymlink != 0 {
			return false
		}

		if i == len(parts)-1 {
			return info.Mode().IsRegular()
		}
	}

	return false
}

// binaryNames are the executables of the PrusaSlicer builds, in the order one
// is chosen: the console build on Windows, which prints what the command line
// does, then the Linux one, then the Windows application.
//
//nolint:gochecknoglobals // a fixed list, read and never written.
var binaryNames = []string{"prusa-slicer-console.exe", "prusa-slicer", "PrusaSlicer.exe"}

// maxFindDepth is how many folders down findBinary looks.
const maxFindDepth = 4

// findBinary is the executable in an unpacked archive, nearest the top first.
func findBinary(dest string) (string, error) {
	found := map[string]string{}

	_ = filepath.WalkDir(dest, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable folder holds nothing to run
		}

		relative, relErr := filepath.Rel(dest, name)
		if relErr != nil {
			return nil //nolint:nilerr // a path outside dest is not one to run
		}

		if entry.IsDir() && strings.Count(filepath.ToSlash(relative), "/") >= maxFindDepth {
			return filepath.SkipDir
		}

		if entry.Type().IsRegular() && slices.Contains(binaryNames, entry.Name()) {
			if held, seen := found[entry.Name()]; !seen || len(relative) < len(held) {
				found[entry.Name()] = filepath.ToSlash(relative)
			}
		}

		return nil
	}) // the walk reports nothing it does not skip

	for _, name := range binaryNames {
		if held, ok := found[name]; ok {
			return held, nil
		}
	}

	return "", fmt.Errorf("%w: it holds no PrusaSlicer executable", ErrArchive)
}

// attach mounts the image read-only at mount, trying again while the disk
// image service is out of reach.
func attach(ctx context.Context, archive, mount string) error {
	var (
		out []byte
		err error
	)

	for try := 1; try <= attachTries; try++ {
		out, err = hdiutil(ctx, "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mount, archive)
		if err == nil {
			return nil
		}

		if !strings.Contains(string(out), "Device not configured") || try == attachTries {
			break
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: hdiutil attach: %w", ErrArchive, ctx.Err())
		case <-time.After(attachPause):
		}
	}

	message := tail(out)
	if strings.Contains(message, "Device not configured") {
		message += ". The system's disk image service could not be reached. A server run inside a sandbox " +
			"that denies it cannot open a dmg: install from a manifest of .tar.gz builds, or name a local binary"
	}

	return fmt.Errorf("%w: hdiutil attach: %w: %s", ErrArchive, err, message)
}

// detach unmounts a disk image, by force when a plain detach is refused. It
// runs on its own deadline, so a cancelled install still detaches.
func detach(mount string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if _, err := hdiutil(ctx, "detach", mount); err == nil {
		return
	}

	_, _ = hdiutil(ctx, "detach", "-force", mount) // the last attempt; nothing is left to try
}

// tail is the end of a command's output, for an error message.
func tail(out []byte) string {
	const keep = 400

	text := strings.TrimSpace(string(out))
	if len(text) > keep {
		text = "..." + text[len(text)-keep:]
	}

	return text
}
