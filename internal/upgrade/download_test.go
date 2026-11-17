package upgrade

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeSource map[string]string

func (f fakeSource) List(context.Context, string) ([]Release, error) { return nil, nil }

func (f fakeSource) Download(_ context.Context, asset Asset, destination io.Writer) error {
	_, err := io.Copy(destination, strings.NewReader(f[asset.Name]))

	return err
}

func testRelease(binary, sums, signature int64) Release {
	return Release{
		Version:   "1.1.0",
		Binary:    Asset{Name: "core", Size: binary},
		Sums:      Asset{Name: "SHA256SUMS", Size: sums},
		Signature: Asset{Name: "SHA256SUMS.sig", Size: signature},
	}
}

func TestDownloadReleaseStreamsTheBinaryToAPrivateFile(t *testing.T) {
	t.Parallel()

	source := fakeSource{"core": "binary", "SHA256SUMS": "sums", "SHA256SUMS.sig": "sig"}
	dir := filepath.Join(t.TempDir(), "download")
	got, err := DownloadRelease(t.Context(), source, testRelease(6, 4, 3), dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Sums) != "sums" || string(got.Signature) != "sig" {
		t.Fatalf("documents are %q and %q", got.Sums, got.Signature)
	}
	if readRunnerFile(t, got.Binary) != "binary" {
		t.Fatal("the binary file does not hold the download")
	}
	info, err := os.Stat(got.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != privateFile {
		t.Fatalf("the binary file mode is %v", info.Mode().Perm())
	}
}

func TestDownloadReleaseRefusesASizeOtherThanTheCatalogueSays(t *testing.T) {
	t.Parallel()

	source := fakeSource{"core": "binary", "SHA256SUMS": "sums", "SHA256SUMS.sig": "sig"}
	for name, release := range map[string]Release{
		"longer binary":       testRelease(5, 4, 3),
		"shorter binary":      testRelease(7, 4, 3),
		"longer sums":         testRelease(6, 3, 3),
		"unknown binary size": testRelease(0, 4, 3),
		"sums over the cap":   testRelease(6, MaxSumsBytes+1, 3),
		"sig over the cap":    testRelease(6, 4, MaxSignatureBytes+1),
		"binary over the cap": testRelease(DefaultMaxBinaryBytes+1, 4, 3),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "download")
			if _, err := DownloadRelease(t.Context(), source, release, dir, 0); !errors.Is(err, ErrDownloadSize) {
				t.Fatalf("error is %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("a refused download left %d files", len(entries))
			}
		})
	}
}
