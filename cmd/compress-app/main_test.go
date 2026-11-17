package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeTree writes each file of a map under one directory and returns it.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()

	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}

	return dir
}

func gunzipFile(t *testing.T, path string) string {
	t.Helper()

	stored, err := os.ReadFile(path)
	require.NoError(t, err)

	reader, err := gzip.NewReader(bytes.NewReader(stored))
	require.NoError(t, err)

	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	return string(body)
}

// TestATextFileIsReplacedByItsCompressedCopy is what the server relies on. A
// script and a stylesheet are stored only as .gz, and each copy decompresses
// to the file the build wrote.
func TestATextFileIsReplacedByItsCompressedCopy(t *testing.T) {
	t.Parallel()

	script := strings.Repeat("export const value = 'the same line again';\n", 200)
	style := strings.Repeat("body { color: red; }\n", 200)

	dir := writeTree(t, map[string]string{
		"assets/index-a1.js":  script,
		"assets/index-a1.css": style,
	})

	_, err := compressTree(dir)
	require.NoError(t, err)

	for name, want := range map[string]string{
		"assets/index-a1.js":  script,
		"assets/index-a1.css": style,
	} {
		plain := filepath.Join(dir, filepath.FromSlash(name))

		_, statErr := os.Stat(plain)
		require.ErrorIsf(t, statErr, os.ErrNotExist, "%s is still stored uncompressed", name)
		require.Equalf(t, want, gunzipFile(t, plain+".gz"), "%s.gz does not decompress to the built file", name)
	}
}

// TestTheEntryDocumentKeepsBothForms keeps the page that curl and health
// checks fetch plain, so the server never decompresses it. A browser still
// gets the gzip copy.
func TestTheEntryDocumentKeepsBothForms(t *testing.T) {
	t.Parallel()

	entry := "<!doctype html>" + strings.Repeat("<p>page</p>", 200)
	dir := writeTree(t, map[string]string{"index.html": entry, "help/index.html": entry})

	done, err := compressTree(dir)
	require.NoError(t, err)
	require.Equal(t, 2, done.files)
	require.Equal(t, 1, done.keptPlain, "the summary does not count the plain entry document")
	require.Equal(t, int64(len(entry)), done.keptBytes)

	plain, err := os.ReadFile(filepath.Join(dir, "index.html"))
	require.NoError(t, err)
	require.Equal(t, entry, string(plain))
	require.Equal(t, entry, gunzipFile(t, filepath.Join(dir, "index.html.gz")))

	_, statErr := os.Stat(filepath.Join(dir, "help", "index.html"))
	require.ErrorIs(t, statErr, os.ErrNotExist, "a page that is not the entry document kept its plain form")
}

// TestAFileThatDoesNotShrinkStaysAsItIs covers the files the server sends as
// they are: a type that is compressed already, and a text file so small that
// the gzip header makes it larger.
func TestAFileThatDoesNotShrinkStaysAsItIs(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{
		"fonts/inter.woff2": strings.Repeat("a", 4096),
		"img/dot.svg":       "<svg/>",
	})

	_, err := compressTree(dir)
	require.NoError(t, err)

	for _, name := range []string{"fonts/inter.woff2", "img/dot.svg"} {
		plain := filepath.Join(dir, filepath.FromSlash(name))

		_, statErr := os.Stat(plain)
		require.NoErrorf(t, statErr, "%s was removed", name)

		_, statErr = os.Stat(plain + ".gz")
		require.ErrorIsf(t, statErr, os.ErrNotExist, "%s was given a compressed copy", name)
	}
}

// TestTheCopyCarriesNoTimeAndNoName keeps the binary reproducible. A gzip
// header can carry a time and a file name, and either one would make two builds
// of one commit differ.
func TestTheCopyCarriesNoTimeAndNoName(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{"assets/index-a1.js": strings.Repeat("let a = 1;\n", 300)})

	_, err := compressTree(dir)
	require.NoError(t, err)

	stored, err := os.ReadFile(filepath.Join(dir, "assets", "index-a1.js.gz"))
	require.NoError(t, err)

	reader, err := gzip.NewReader(bytes.NewReader(stored))
	require.NoError(t, err)

	require.True(t, reader.ModTime.IsZero(), "the copy carries the time %s", reader.ModTime)
	require.Empty(t, reader.Name, "the copy carries a file name")
	require.Empty(t, reader.Comment, "the copy carries a comment")
}

// TestATreeThatHoldsAGzipFileIsRefused keeps one meaning for the .gz suffix.
// The server reads a name ending in .gz as the compressed copy of another
// file. Such a name comes from the frontend build or from an earlier run of
// this tool, and in both cases the tree has to be built again.
func TestATreeThatHoldsAGzipFileIsRefused(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{
		"assets/data.json.gz": "anything",
		"assets/index-a1.js":  strings.Repeat("let a = 1;\n", 300),
	})

	_, err := compressTree(dir)
	require.ErrorContains(t, err, "data.json.gz")
	require.ErrorContains(t, err, "build the web app again")

	_, statErr := os.Stat(filepath.Join(dir, "assets", "index-a1.js"))
	require.NoError(t, statErr, "a refused run changed the tree")
}
