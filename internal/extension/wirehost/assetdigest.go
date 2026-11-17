package wirehost

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// The refusals a directory digest returns beside ErrDigestFormat and
// ErrDigestMismatch.
var (
	// ErrBundleEmpty reports a directory holding no regular file. Nothing in it
	// could be served, so a digest over it records nothing.
	ErrBundleEmpty = errors.New("wirehost: the bundle holds no file")

	// ErrBundleEntry reports an entry that is neither a directory nor a regular
	// file. A symbolic link is the case that matters. It can name a file
	// outside the bundle, and a digest that followed it would vouch for bytes
	// the install never covered.
	ErrBundleEntry = errors.New("wirehost: the bundle holds an entry that is not a file or a directory")
)

// VerifyDir reads every file under dir and reports whether the tree is the one
// the digest covers. It is the directory form of Verify, used for an asset
// module. It runs before every start, so a file swapped after installation is
// refused and never served.
func (d Digest) VerifyDir(dir string) error {
	want, err := d.recorded()
	if err != nil {
		return err
	}

	got, err := hashDir(dir)
	if err != nil {
		return err
	}

	if got != want {
		return fmt.Errorf("%s is %s%s: %w", dir, digestPrefix, got, ErrDigestMismatch)
	}

	return nil
}

// DigestDir records what a directory of files hashes to, in the form VerifyDir
// checks. An installer writes its result into the declaration.
func DigestDir(dir string) (Digest, error) {
	sum, err := hashDir(dir)
	if err != nil {
		return "", err
	}

	return Digest(digestPrefix + sum), nil
}

// hashDir hashes the tree as one stream. For every regular file in lexical walk
// order it writes the slash-separated path inside the directory, a NUL, the hex
// sha-256 of the file's bytes, and a newline. Renaming, adding, removing or
// rewriting a file changes the result, and the stream is never held in memory.
func hashDir(dir string) (string, error) {
	sum := sha256.New()
	files := 0

	tree := os.DirFS(dir)

	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("read the bundle: %w", err)
		}

		if entry.IsDir() {
			return nil
		}

		if !entry.Type().IsRegular() {
			return fmt.Errorf("%w: %s", ErrBundleEntry, path)
		}

		fileSum, err := hashFile(tree, path)
		if err != nil {
			return err
		}

		files++

		fmt.Fprintf(sum, "%s\x00%s\n", path, fileSum)

		return nil
	}

	if err := fs.WalkDir(tree, ".", walk); err != nil {
		return "", err
	}

	if files == 0 {
		return "", fmt.Errorf("%w: %s", ErrBundleEmpty, dir)
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

// hashFile is the sha-256 of one file's bytes, as hex.
func hashFile(tree fs.FS, path string) (string, error) {
	file, err := tree.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}

	defer func() {
		_ = file.Close()
	}()

	sum := sha256.New()
	if _, err = io.Copy(sum, file); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}
