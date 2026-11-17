package app

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/internal/extension/wirehost"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// What a module's release holds, and how it lands on this host.
//
// The layout is one directory with the manifest at its root, one binary per
// platform named the way the host looks for it, a checksum file, and, for a
// module that draws in the browser, the built bundle beside them. A release is
// a directory here or a .tar.gz of the same tree. A release is not fetched
// from a tag on a repository.

// manifestName is the one file a release has to carry, at its root.
const manifestName = "module.yaml"

// checksumsName is the file a release carries to prove its own bytes. It is
// optional here, because a directory an author built a minute ago has nothing
// to prove against, and it is verified whenever it is there.
const checksumsName = "checksums.txt"

// signatureName is the author's signature over checksums.txt, which a
// marketplace release carries. The checksum file cannot list its own
// signature, so it is the one file beside it that need not be listed. This
// server does not verify the signature.
const signatureName = "checksums.txt.sig"

// The bounds one archive is read under. A release is a handful of files, and
// an archive that claims otherwise is refused before anything is written,
// because the file being unpacked is somebody else's.
const (
	maxArchiveBytes = 256 << 20
	maxArchiveFiles = 4096
)

// archiveSuffix is what this build reads as an archive. Everything else is
// read as a directory.
const archiveSuffix = ".tar.gz"

// The one platform whose executables carry a suffix. A release names a binary
// <name>_<GOOS>_<GOARCH>, and only this one adds anything to it.
const (
	windowsGOOS   = "windows"
	windowsSuffix = ".exe"
)

// release is one module's release as this command reads it: where the files
// are, what the manifest says, which file is this host's binary, and which
// files make up the browser half.
type release struct {
	dir      string
	manifest manifest.Manifest
	binary   string
	bundle   []string
}

// stageRelease makes the release readable as a directory, and returns what to
// call when the install is over. A directory is read where it is; an archive is
// unpacked into a directory of this command's own.
func stageRelease(from string) (string, func(), error) {
	if !strings.HasSuffix(strings.ToLower(from), archiveSuffix) {
		info, err := os.Stat(from)
		if err != nil {
			return "", nil, fmt.Errorf("read the release at %s: %w", from, err)
		}

		if !info.IsDir() {
			return "", nil, fmt.Errorf("%w: %s is neither a directory nor a %s archive",
				ErrUsage, from, archiveSuffix)
		}

		return from, func() {}, nil
	}

	staged, err := os.MkdirTemp("", "module-add")
	if err != nil {
		return "", nil, fmt.Errorf("make a staging directory: %w", err)
	}

	if err = unpack(from, staged); err != nil {
		_ = os.RemoveAll(staged)

		return "", nil, err
	}

	return staged, func() { _ = os.RemoveAll(staged) }, nil
}

// unpack extracts one gzipped tar into a directory this command owns.
//
// Every entry is held to naming a regular file inside that directory. A name
// that climbs out, an absolute name, a link and a device node are all refused
// rather than skipped: an archive carrying one is not the release layout, and
// unpacking the rest of it would install half of something nobody described.
// The one entry skipped is an AppleDouble file, which isAppleDouble describes.
func unpack(archive, into string) error {
	file, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("open the release archive: %w", err)
	}

	defer func() { _ = file.Close() }()

	zipped, err := gzip.NewReader(bufio.NewReader(file))
	if err != nil {
		return fmt.Errorf("read %s as a gzip stream: %w", archive, err)
	}

	defer func() { _ = zipped.Close() }()

	reader := tar.NewReader(io.LimitReader(zipped, maxArchiveBytes+1))
	written := int64(0)
	files := 0

	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			return nil
		}

		if readErr != nil {
			return fmt.Errorf("read %s: %w", archive, readErr)
		}

		if isAppleDouble(header.Name) {
			continue
		}

		files++
		if files > maxArchiveFiles {
			return fmt.Errorf("%s holds over %d files, which is not a module release",
				archive, maxArchiveFiles)
		}

		if written, err = unpackEntry(reader, header, into, written); err != nil {
			return err
		}
	}
}

// isAppleDouble reports an entry macOS tar adds beside a file that carries
// extended attributes: the same name with a "._" prefix, holding the
// attributes. It is no file of the release, so it is never written, and a
// "._checksums.txt" or "._module.yaml" is never read as the file it shadows.
func isAppleDouble(name string) bool {
	return strings.HasPrefix(path.Base(filepath.ToSlash(name)), "._")
}

// unpackEntry writes one archive entry, and returns how many bytes the archive
// has written in total so far.
func unpackEntry(reader io.Reader, header *tar.Header, into string, written int64) (int64, error) {
	name := path.Clean(strings.TrimPrefix(filepath.ToSlash(header.Name), "./"))

	if name == "." || !fs.ValidPath(name) || path.IsAbs(name) || strings.HasPrefix(name, "../") {
		return written, fmt.Errorf("the archive holds %q, which does not name a file inside it", header.Name)
	}

	target := filepath.Join(into, filepath.FromSlash(name))

	switch header.Typeflag {
	case tar.TypeDir:
		return written, os.MkdirAll(target, dirPerm) //nolint:wrapcheck // the path is in the error already.
	case tar.TypeReg:
	default:
		return written, fmt.Errorf("the archive holds %q, which is not a regular file", header.Name)
	}

	if err := os.MkdirAll(filepath.Dir(target), dirPerm); err != nil {
		return written, fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, releasePerm(header.Mode))
	if err != nil {
		return written, fmt.Errorf("write %s: %w", target, err)
	}

	copied, err := io.Copy(out, io.LimitReader(reader, maxArchiveBytes-written+1))

	if closeErr := out.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return written, fmt.Errorf("write %s: %w", target, err)
	}

	written += copied
	if written > maxArchiveBytes {
		return written, fmt.Errorf("the archive unpacks to over %d bytes", maxArchiveBytes)
	}

	return written, nil
}

// The file modes an installed module is written under. The bundle is read by
// this process alone, and the binary is launched by it, so nothing here is
// readable or runnable by anybody else on the host.
const (
	filePerm       = 0o600
	executablePerm = 0o700
	dirPerm        = 0o700
)

// releasePerm is the mode one unpacked file is written under: executable when
// the archive said so, and readable by this server alone either way.
func releasePerm(mode int64) os.FileMode {
	if mode&0o100 != 0 {
		return executablePerm
	}

	return filePerm
}

// readRelease reads the manifest and works out which files are which.
func readRelease(dir string) (release, error) {
	document, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return release{}, fmt.Errorf("read the release's %s: %w", manifestName, err)
	}

	declared, err := manifest.Parse(manifestName, document, yaml.Unmarshal)
	if err != nil {
		return release{}, fmt.Errorf("%w", err)
	}

	held := release{dir: dir, manifest: declared}

	platforms, err := platformFiles(declared)
	if err != nil {
		return release{}, err
	}

	if declared.ContractMajor > 0 {
		held.binary = wirehost.Artefact("", declared.Name)
		if !slices.Contains(platforms, held.binary) {
			return release{}, fmt.Errorf("the release ships no binary for %s/%s. It ships %s",
				runtime.GOOS, runtime.GOARCH, strings.Join(declared.Platforms, ", "))
		}

		if _, err = os.Stat(filepath.Join(dir, held.binary)); err != nil {
			return release{}, fmt.Errorf("read the release's %s: %w", held.binary, err)
		}
	}

	if held.bundle, err = bundleFiles(dir, platforms); err != nil {
		return release{}, err
	}

	return held, nil
}

// platformFiles is the file name of every platform's binary, from the
// manifest's own platforms list. The list is what says which files are
// programs, so a bundle that happens to carry a file with that shape of name is
// still a bundle file.
func platformFiles(declared manifest.Manifest) ([]string, error) {
	out := make([]string, 0, len(declared.Platforms))

	for _, platform := range declared.Platforms {
		operating, architecture, found := strings.Cut(platform, "_")
		if !found || operating == "" || architecture == "" {
			return nil, fmt.Errorf("platforms names %q, and a platform is written GOOS_GOARCH", platform)
		}

		name := declared.Name + "_" + platform
		if operating == windowsGOOS {
			name += windowsSuffix
		}

		out = append(out, name)
	}

	return out, nil
}

// bundleFiles is every file of the release that is not a platform binary and
// not the checksum file, as a slash-separated path under the release root. The
// manifest is one of them: the host reads the ui block out of the bundle it
// serves, and the digest covers it there.
func bundleFiles(dir string, platforms []string) ([]string, error) {
	var out []string

	err := filepath.WalkDir(dir, func(where string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return nil
		}

		if !entry.Type().IsRegular() {
			return fmt.Errorf("the release holds %s, which is not a regular file", where)
		}

		relative, relErr := filepath.Rel(dir, where)
		if relErr != nil {
			return relErr
		}

		name := filepath.ToSlash(relative)
		if name == checksumsName || slices.Contains(platforms, name) {
			return nil
		}

		out = append(out, name)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the release at %s: %w", dir, err)
	}

	slices.Sort(out)

	return out, nil
}

// verifyChecksums holds every file the release's checksum file names to the
// digest written beside it, and reports how many were checked.
//
// A release with no checksum file is checked against nothing, and the count is
// zero. An author's own build directory has no checksum file, and refusing it
// would mean no module could be installed without a release workflow. The
// install records a digest of the bytes it wrote, and the host verifies that
// digest before every launch, whichever way the files arrived.
func verifyChecksums(dir string) (int, error) {
	content, err := os.ReadFile(filepath.Join(dir, checksumsName))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("read the release's %s: %w", checksumsName, err)
	}

	checked := 0
	listed := map[string]bool{checksumsName: true, signatureName: true}

	for line := range strings.SplitSeq(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		name, lineErr := verifyChecksumLine(dir, line)
		if lineErr != nil {
			return checked, lineErr
		}

		listed[name] = true
		checked++
	}

	if err = everyFileListed(dir, listed); err != nil {
		return checked, err
	}

	return checked, nil
}

// everyFileListed refuses a release carrying a file its checksum file does not
// name. A checksum file vouches for the release, and a file beside it that it
// does not name would be installed with nothing having checked it. The
// marketplace holds a release to the same rule before it publishes one.
func everyFileListed(dir string, listed map[string]bool) error {
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		name, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("name %s inside the release: %w", path, err)
		}

		if !listed[filepath.ToSlash(name)] {
			return fmt.Errorf("the release carries %s, which %s does not list", filepath.ToSlash(name), checksumsName)
		}

		return nil
	})
}

// verifyChecksumLine holds one line of a checksum file, in the two-column form
// sha256sum writes: the digest, whitespace, and the file name, and answers the
// name it checked.
func verifyChecksumLine(dir, line string) (string, error) {
	digest, name, found := strings.Cut(strings.TrimSpace(line), " ")
	if !found {
		return "", fmt.Errorf("%s carries the line %q, and a line is a digest and a file name",
			checksumsName, line)
	}

	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "*"))

	if !fs.ValidPath(name) || strings.HasPrefix(name, "../") {
		return "", fmt.Errorf("%s names %q, which is not a file inside the release", checksumsName, name)
	}

	sum, err := digestOfFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		return "", err
	}

	if !strings.EqualFold(sum, digest) {
		return "", fmt.Errorf("%s says %s is %s, and it is sha256:%s", checksumsName, name, digest, sum)
	}

	return name, nil
}

// digestOfFile is the sha-256 of one file, as hex.
func digestOfFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	sum := sha256.New()
	if _, err = io.Copy(sum, file); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

// installed is where one module's halves landed and what proves each of them.
type installed struct {
	root string
	// previous is where an update keeps the release it replaced until the new
	// one has started, and empty when nothing was kept.
	previous     string
	binaryDir    string
	binaryDigest string
	bundleDir    string
	bundleDigest string
}

// The two directories one installed module holds: the process half, which the
// host scans for this platform's file, and the browser half, whose whole
// directory the digest covers.
const (
	binaryDirName = "bin"
	bundleDirName = "bundle"
)

// installRelease copies the release into the modules directory and records what
// it wrote.
//
// The module's own directory is replaced whole. A reinstall of a version that
// ships fewer files than the one before would otherwise leave the extra files
// there, inside the directory the digest covers, and the bundle would fail
// every verification.
func installRelease(held release, modulesDir string, keep bool) (installed, error) {
	out, err := writeRelease(held, modulesDir, keep)
	if err != nil {
		return installed{}, installWriteError{dir: modulesDir, err: err}
	}

	return out, nil
}

// previousSuffix ends the name of the directory an update keeps the release it
// replaced in. The leading dot keeps it apart from every module's own
// directory, whose name is a manifest name and never starts with one.
const previousSuffix = ".previous"

// previousRelease is where an update keeps the release it replaced.
func previousRelease(modulesDir, name string) string {
	return filepath.Join(modulesDir, "."+name+previousSuffix)
}

// installWriteError is a failure to write a verified release into the modules
// directory. It is the server's, never the release's: the release was read and
// checked before anything was written, so what is left is the directory.
type installWriteError struct {
	dir string
	err error
}

func (e installWriteError) Error() string {
	return fmt.Sprintf("write the release into %s: %v", e.dir, e.err)
}

func (e installWriteError) Unwrap() error { return e.err }

// writeRelease is installRelease's work, every error of which is the modules
// directory's.
//
// keep moves the release already installed aside instead of deleting it, so an
// update whose module does not start can be rolled back to it.
func writeRelease(held release, modulesDir string, keep bool) (installed, error) {
	root := filepath.Join(modulesDir, held.manifest.Name)
	out := installed{root: root}

	if keep {
		previous := previousRelease(modulesDir, held.manifest.Name)

		if err := os.RemoveAll(previous); err != nil {
			return installed{}, fmt.Errorf("clear %s: %w", previous, err)
		}

		err := os.Rename(root, previous)

		switch {
		case err == nil:
			out.previous = previous
		case !errors.Is(err, fs.ErrNotExist):
			return installed{}, fmt.Errorf("keep %s aside: %w", root, err)
		}
	}

	if err := os.RemoveAll(root); err != nil {
		return installed{}, fmt.Errorf("clear %s: %w", root, err)
	}

	if held.binary != "" {
		out.binaryDir = filepath.Join(root, binaryDirName)

		if err := copyInto(held.dir, out.binaryDir, []string{held.binary}, executablePerm); err != nil {
			return installed{}, err
		}

		digest, err := digestOfFile(filepath.Join(out.binaryDir, held.binary))
		if err != nil {
			return installed{}, err
		}

		out.binaryDigest = "sha256:" + digest
	}

	if held.manifest.UI != nil {
		out.bundleDir = filepath.Join(root, bundleDirName)

		if err := copyInto(held.dir, out.bundleDir, held.bundle, filePerm); err != nil {
			return installed{}, err
		}

		digest, err := wirehost.DigestDir(out.bundleDir)
		if err != nil {
			return installed{}, fmt.Errorf("record the bundle's digest: %w", err)
		}

		out.bundleDigest = string(digest)
	}

	return out, nil
}

// copyInto copies the named files from one directory into another, keeping the
// path each one had inside the release.
func copyInto(from, to string, names []string, perm os.FileMode) error {
	for _, name := range names {
		target := filepath.Join(to, filepath.FromSlash(name))

		if err := os.MkdirAll(filepath.Dir(target), dirPerm); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
		}

		body, err := os.ReadFile(filepath.Join(from, filepath.FromSlash(name)))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		//nolint:gosec // G703: unpackEntry refuses a name fs.ValidPath rejects before any of these exist, and bundleFiles reads its names off a walk of the unpacked directory.
		if err = os.WriteFile(target, body, perm); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
	}

	return nil
}
