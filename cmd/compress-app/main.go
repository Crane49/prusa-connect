// Command compress-app replaces each text file of the built web app with a
// gzip copy under the same name plus ".gz". The server sends that copy as it
// is stored, and decompresses it only for a client that does not accept gzip.
//
// Usage: go run ./cmd/compress-app <directory>
//
// A file keeps its plain form when its type is compressed already, such as a
// font or an image, or when the gzip copy would not be smaller. The entry
// document at the root keeps its plain form beside the copy.
package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	exitOK      = 0
	exitFailure = 1
)

// compressedSuffix is how the server finds the stored copy of a file.
const compressedSuffix = ".gz"

// entryDocument keeps its plain form beside the copy, because curl and health
// checks fetch it without gzip.
const entryDocument = "index.html"

// textTypes are the extensions worth compressing. Fonts and images are left
// out because their formats are compressed already.
//
//nolint:gochecknoglobals // a closed table read once per run, never written.
var textTypes = map[string]bool{
	".html":        true,
	".js":          true,
	".mjs":         true,
	".css":         true,
	".json":        true,
	".map":         true,
	".svg":         true,
	".txt":         true,
	".md":          true,
	".xml":         true,
	".webmanifest": true,
}

// summary counts what one run changed, for the line the build prints.
type summary struct {
	files     int
	before    int64
	after     int64
	keptPlain int
	keptBytes int64
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: compress-app <directory>")

		return exitFailure
	}

	done, err := compressTree(args[0])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "compress-app: "+err.Error())

		return exitFailure
	}

	_, _ = fmt.Fprintf(stdout, "compressed %d files of the web app from %d to %d bytes, "+
		"and kept %d of them plain beside the copy in %d more bytes\n",
		done.files, done.before, done.after, done.keptPlain, done.keptBytes)

	return exitOK
}

// compressTree compresses every text file under dir. It reads the whole tree
// before it writes anything, so a tree it refuses is left as it was. A run
// that fails while writing leaves some files compressed, and a second run
// then refuses the tree until the app is built again.
func compressTree(dir string) (summary, error) {
	var names []string

	//nolint:gosec // G703: the directory is the build's own output, named by the Makefile or a Dockerfile.
	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir():
			return nil
		case strings.HasSuffix(path, compressedSuffix):
			return fmt.Errorf("the tree already holds %s. The server reads a name ending in %s as the "+
				"compressed copy of another file, so build the web app again into an empty directory",
				path, compressedSuffix)
		case textTypes[strings.ToLower(filepath.Ext(path))]:
			names = append(names, path)
		}

		return nil
	})
	if walkErr != nil {
		return summary{}, fmt.Errorf("read %s: %w", dir, walkErr)
	}

	var done summary

	for _, name := range names {
		keepPlain := filepath.Join(dir, entryDocument) == name

		before, after, compressErr := compressFile(name, keepPlain)
		if compressErr != nil {
			return done, compressErr
		}

		if after < before {
			done.files++
			done.before += before
			done.after += after

			if keepPlain {
				done.keptPlain++
				done.keptBytes += before
			}
		}
	}

	return done, nil
}

// compressFile writes the gzip copy of one file and removes the plain file
// unless keepPlain is set. When the copy is not smaller it writes nothing. It
// returns the size before and after.
func compressFile(name string, keepPlain bool) (int64, int64, error) {
	plain, err := os.ReadFile(name)
	if err != nil {
		return 0, 0, fmt.Errorf("read %s: %w", name, err)
	}

	var packed bytes.Buffer

	// The header carries no time and no file name, so two builds of one
	// commit give the same bytes.
	writer, err := gzip.NewWriterLevel(&packed, gzip.BestCompression)
	if err != nil {
		return 0, 0, fmt.Errorf("compress %s: %w", name, err)
	}

	_, writeErr := writer.Write(plain)
	if joined := errors.Join(writeErr, writer.Close()); joined != nil {
		return 0, 0, fmt.Errorf("compress %s: %w", name, joined)
	}

	before := int64(len(plain))
	if int64(packed.Len()) >= before {
		return before, before, nil
	}

	err = writeFile(name+compressedSuffix, packed.Bytes())
	if err != nil {
		return 0, 0, err
	}

	if keepPlain {
		return before, int64(packed.Len()), nil
	}

	err = os.Remove(name)
	if err != nil {
		return 0, 0, fmt.Errorf("remove %s: %w", name, err)
	}

	return before, int64(packed.Len()), nil
}

// writeFile checks the error of Close, because a full disk is reported there.
func writeFile(name string, body []byte) error {
	file, err := os.Create(name)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}

	_, writeErr := file.Write(body)
	closeErr := file.Close()

	if joined := errors.Join(writeErr, closeErr); joined != nil {
		return fmt.Errorf("write %s: %w", name, joined)
	}

	return nil
}
