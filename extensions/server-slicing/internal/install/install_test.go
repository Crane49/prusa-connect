package install_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/server-slicing/internal/catalog"
	"github.com/prusa3d/server-slicing/internal/install"
)

// fakeSlicer is a shell script that answers --help the way PrusaSlicer does.
func fakeSlicer(version string) []byte {
	return []byte("#!/bin/sh\necho 'PrusaSlicer-" + version + " based on Slic3r (with GUI support)'\n")
}

func tarGz(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()

	var buffer bytes.Buffer

	zipped := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipped)

	for _, header := range entries {
		body, _ := header.PAXRecords["body"]
		header.PAXRecords = nil
		header.Size = int64(len(body))

		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}

		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

func file(name, body string, mode int64) *tar.Header {
	return &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode, PAXRecords: map[string]string{"body": body}}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// serve answers one download over https and counts the requests.
func serve(t *testing.T, data []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var asked atomic.Int32

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)

	return server, &asked
}

func TestADownloadIsCheckedAndKeptByItsDigest(t *testing.T) {
	t.Parallel()

	data := []byte("the build")
	server, asked := serve(t, data)
	cache := t.TempDir()

	build := catalog.Build{URL: server.URL + "/b.tar.gz", SHA256: digest(data), Size: int64(len(data))}

	for range 2 {
		path, err := install.Fetch(t.Context(), server.Client(), build, cache, nil)
		if err != nil {
			t.Fatal(err)
		}

		if filepath.Base(path) != build.SHA256 {
			t.Fatalf("the download is kept as %s", path)
		}
	}

	if asked.Load() != 1 {
		t.Fatalf("the server was asked %d times, and the second fetch should read the cache", asked.Load())
	}
}

func TestADownloadThatDoesNotMatchIsThrownAway(t *testing.T) {
	t.Parallel()

	data := []byte("the build, changed on the server")
	server, _ := serve(t, data)
	cache := t.TempDir()

	build := catalog.Build{URL: server.URL + "/b.tar.gz", SHA256: digest([]byte("the build"))}

	_, err := install.Fetch(t.Context(), server.Client(), build, cache, nil)
	if !errors.Is(err, install.ErrDigest) {
		t.Fatalf("a download with another digest answered %v", err)
	}

	left, _ := os.ReadDir(cache)
	if len(left) != 0 {
		t.Fatalf("the cache kept %d files of a download that did not match", len(left))
	}

	build.SHA256 = digest(data)
	build.Size = 3

	if _, err = install.Fetch(t.Context(), server.Client(), build, cache, nil); !errors.Is(err, install.ErrDownload) {
		t.Fatalf("a download of another size answered %v", err)
	}
}

func TestAnArchiveThatLeavesItsDirectoryIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string][]*tar.Header{
		"a climbing entry":  {file("../evil", "x", 0o644)},
		"an absolute entry": {file("/tmp/evil", "x", 0o644)},
		"a link out": {
			{Name: "bin/out", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"},
		},
		"an absolute link": {
			{Name: "bin/out", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		},
		"a device": {{Name: "dev", Typeflag: tar.TypeChar}},
	}

	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			archive := filepath.Join(t.TempDir(), "b.tar.gz")
			if err := os.WriteFile(archive, tarGz(t, entries...), 0o600); err != nil {
				t.Fatal(err)
			}

			build := catalog.Build{Archive: catalog.ArchiveTarGz, Binary: "bin/prusa-slicer"}

			_, err := install.Unpack(t.Context(), build, archive, filepath.Join(t.TempDir(), "dest"))
			if !errors.Is(err, install.ErrArchive) {
				t.Fatalf("the archive was unpacked: %v", err)
			}
		})
	}
}

func TestAZipWithAClimbingEntryIsRefused(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer

	writer := zip.NewWriter(&buffer)

	entry, err := writer.Create("PrusaSlicer/../../evil.exe")
	if err != nil {
		t.Fatal(err)
	}

	_, _ = entry.Write([]byte("x"))

	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "b.zip")
	if err = os.WriteFile(archive, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	build := catalog.Build{Archive: catalog.ArchiveZip, Binary: "PrusaSlicer/prusa-slicer-console.exe"}

	if _, err = install.Unpack(
		t.Context(),
		build,
		archive,
		filepath.Join(t.TempDir(), "dest"),
	); !errors.Is(
		err,
		install.ErrArchive,
	) {
		t.Fatalf("the zip was unpacked: %v", err)
	}
}

// A build is downloaded, unpacked, run once to read its version, and listed.
// A build whose banner names another version than its manifest is refused.
func TestABuildIsInstalledAndCheckedAgainstItsVersion(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the fake slicer is a shell script")
	}

	for _, claimed := range []string{"2.9.6", "2.9.5"} {
		t.Run(claimed, func(t *testing.T) {
			t.Parallel()

			archive := tarGz(t,
				&tar.Header{Name: "prusa-slicer/", Typeflag: tar.TypeDir, Mode: 0o755},
				file("prusa-slicer/bin/prusa-slicer", string(fakeSlicer("2.9.6")), 0o755),
				&tar.Header{Name: "prusa-slicer/bin/link", Typeflag: tar.TypeSymlink, Linkname: "prusa-slicer"},
			)
			server, _ := serve(t, archive)

			data := t.TempDir()

			store, err := install.Open(filepath.Join(data, "slicers"), filepath.Join(data, "cache"), server.Client())
			if err != nil {
				t.Fatal(err)
			}

			version, _ := catalog.ParseVersion(claimed)
			candidate := catalog.Candidate{
				Version: version, Channel: catalog.Stable, Source: catalog.FromOperator,
				Build: catalog.Build{
					OS: runtime.GOOS, Arch: runtime.GOARCH, URL: server.URL + "/b.tar.gz",
					SHA256: digest(archive), Archive: catalog.ArchiveTarGz, Binary: "prusa-slicer/bin/prusa-slicer",
				},
			}

			ended := make(chan error, 1)
			if err = store.Start(t.Context(), candidate, func(err error) { ended <- err }); err != nil {
				t.Fatal(err)
			}

			select {
			case err = <-ended:
			case <-time.After(time.Minute):
				t.Fatal("the install did not end")
			}

			if claimed != "2.9.6" {
				if !errors.Is(err, install.ErrProbe) {
					t.Fatalf("a build that names another version answered %v", err)
				}

				if len(store.Installed()) != 0 {
					t.Fatal("a build that names another version was listed")
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			reopened, err := install.Open(filepath.Join(data, "slicers"), filepath.Join(data, "cache"), server.Client())
			if err != nil {
				t.Fatal(err)
			}

			listed := reopened.Installed()
			if len(listed) != 1 || listed[0].Name() != "2.9.6" {
				t.Fatalf("the index lists %+v", listed)
			}

			if _, err = os.Stat(listed[0].Binary); err != nil {
				t.Fatalf("the listed binary is not on disk: %v", err)
			}

			done := reopened.Acquire("2.9.6")
			if err = reopened.Remove("2.9.6"); !errors.Is(err, install.ErrBusy) {
				t.Fatalf("a version in use was removed: %v", err)
			}

			done()

			if err = reopened.Remove("2.9.6"); err != nil {
				t.Fatal(err)
			}

			if _, err = os.Stat(listed[0].Binary); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("the removed build is still on disk")
			}
		})
	}
}

func slicerOf(t *testing.T, version string, source catalog.Source) install.Slicer {
	t.Helper()

	parsed, err := catalog.ParseVersion(version)
	if err != nil {
		t.Fatal(err)
	}

	channel := catalog.Stable
	if !parsed.Stable() {
		channel = catalog.Prerelease
	}

	return install.Slicer{Version: parsed, Channel: channel, Source: source, Binary: "/" + version + string(source)}
}

func TestTheSlicerForAJob(t *testing.T) {
	t.Parallel()

	installed := []install.Slicer{
		slicerOf(t, "2.9.5", catalog.FromGitHub),
		slicerOf(t, "3.0.0-alpha12", catalog.FromGitHub),
		slicerOf(t, "2.9.6", install.FromLocal),
		slicerOf(t, "2.9.6", catalog.FromGitHub),
	}

	cases := []struct {
		name              string
		requested, pinned string
		needMajor         int
		want              string
		wantSource        catalog.Source
		wantErr           error
	}{
		{name: "the newest final release", want: "2.9.6", wantSource: catalog.FromGitHub},
		{name: "the pinned one", pinned: "2.9.5", want: "2.9.5"},
		{name: "a named one", requested: "3.0.0-alpha12", pinned: "2.9.5", want: "3.0.0-alpha12"},
		{name: "a project of PrusaSlicer 3", needMajor: 3, want: "3.0.0-alpha12"},
		{name: "a pin too old for the project", pinned: "2.9.5", needMajor: 3, want: "3.0.0-alpha12"},
		{name: "a named one too old", requested: "2.9.6", needMajor: 3, wantErr: install.ErrTooOld},
		{name: "a version that is not there", requested: "2.8.0", wantErr: install.ErrNotInstalled},
		{name: "a project nothing can slice", needMajor: 4, wantErr: install.ErrNoSlicer},
	}

	for _, held := range cases {
		t.Run(held.name, func(t *testing.T) {
			t.Parallel()

			chosen, err := install.Choose(installed, held.requested, held.pinned, held.needMajor)
			if held.wantErr != nil {
				if !errors.Is(err, held.wantErr) {
					t.Fatalf("answered %v, want %v", err, held.wantErr)
				}

				return
			}

			if err != nil || chosen.Name() != held.want {
				t.Fatalf("chose %s, %v, want %s", chosen.Name(), err, held.want)
			}

			if held.wantSource != "" && chosen.Source != held.wantSource {
				t.Fatalf("chose the %s build, want the %s one", chosen.Source, held.wantSource)
			}
		})
	}

	if _, err := install.Choose(nil, "", "", 0); !errors.Is(err, install.ErrNoSlicer) {
		t.Fatalf("nothing installed answered %v", err)
	}
}

// A GitHub release does not name its executable, so the install finds it, and
// a tar.bz2, the Linux arm64 build of PrusaSlicer 2.7, unpacks like a tar.gz.
func TestTheExecutableIsFoundWhenTheReleaseNamesNone(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the fake slicer is a shell script")
	}

	entries := []*tar.Header{
		{Name: "PrusaSlicer-2.7.4+linux-arm64-GTK3/", Typeflag: tar.TypeDir, Mode: 0o755},
		file("PrusaSlicer-2.7.4+linux-arm64-GTK3/resources/profiles/x.ini", "x", 0o644),
		file("PrusaSlicer-2.7.4+linux-arm64-GTK3/bin/prusa-slicer", string(fakeSlicer("2.7.4")), 0o755),
		file("PrusaSlicer-2.7.4+linux-arm64-GTK3/prusa-slicer", string(fakeSlicer("2.7.4")), 0o755),
	}

	dir := t.TempDir()
	gz := filepath.Join(dir, "b.tar.gz")

	if err := os.WriteFile(gz, tarGz(t, entries...), 0o600); err != nil {
		t.Fatal(err)
	}

	binary, err := install.Unpack(
		t.Context(),
		catalog.Build{Archive: catalog.ArchiveTarGz},
		gz,
		filepath.Join(dir, "gz"),
	)
	if err != nil || !strings.HasSuffix(binary, "PrusaSlicer-2.7.4+linux-arm64-GTK3/prusa-slicer") {
		t.Fatalf("found %s, %v", binary, err)
	}

	bzip2, err := exec.LookPath("bzip2")
	if err != nil {
		t.Skip("no bzip2 here to pack the tar.bz2")
	}

	plain := filepath.Join(dir, "b.tar")

	unzipped, err := exec.Command("gzip", "-dc", gz).Output()
	if err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(plain, unzipped, 0o600); err != nil {
		t.Fatal(err)
	}

	if out, packErr := exec.Command(bzip2, plain).CombinedOutput(); packErr != nil {
		t.Fatalf("bzip2: %v %s", packErr, out)
	}

	binary, err = install.Unpack(
		t.Context(),
		catalog.Build{Archive: catalog.ArchiveTarBz2},
		plain+".bz2",
		filepath.Join(dir, "bz2"),
	)
	if err != nil || !strings.HasSuffix(binary, "/prusa-slicer") {
		t.Fatalf("the tar.bz2 found %s, %v", binary, err)
	}

	if _, err = install.Unpack(t.Context(), catalog.Build{Archive: catalog.ArchiveTarGz}, writeTar(t, dir,
		file("readme.txt", "x", 0o644)), filepath.Join(dir, "none")); !errors.Is(err, install.ErrArchive) {
		t.Fatalf("an archive with no executable answered %v", err)
	}
}

func writeTar(t *testing.T, dir string, entries ...*tar.Header) string {
	t.Helper()

	path := filepath.Join(dir, "none.tar.gz")
	if err := os.WriteFile(path, tarGz(t, entries...), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}
