package slicers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/prusa3d/server-slicing/internal/install"
	"github.com/prusa3d/server-slicing/internal/slicers"
)

func fake(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the stand-in slicer is a shell script")
	}

	path, err := filepath.Abs("../../testdata/fake-slicer.sh")
	if err != nil {
		t.Fatal(err)
	}

	return path
}

// A local binary is listed by the version it names, a manifest the operator
// hosts adds its builds, and a setting the module cannot use is reported
// rather than failing the start.
func TestTheServiceListsWhatItCanUse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")

	document := `{"schema":1,"releases":[{"version":"2.9.5","channel":"stable","builds":[{"os":"` + runtime.GOOS +
		`","arch":"` + runtime.GOARCH + `","url":"https://builds.example.com/ps.tar.gz","sha256":"` +
		strings.Repeat("ab", 32) + `","archive":"tar.gz","binary":"bin/prusa-slicer"}]}]}`

	if err := os.WriteFile(manifest, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}

	service, err := slicers.Open(slicers.Config{
		Dir: dir, OS: runtime.GOOS, Arch: runtime.GOARCH, ManifestURL: manifest,
		LocalBinaries: []string{fake(t), filepath.Join(dir, "missing")},
	})
	if err != nil {
		t.Fatal(err)
	}

	service.Load(t.Context())

	view := service.View(false)

	if len(view.Installed) != 1 || view.Installed[0].Source != install.FromLocal || view.Default != "2.9.6" {
		t.Fatalf("the installed slicers are %+v, default %q", view.Installed, view.Default)
	}

	if len(view.Available) != 1 || view.Available[0].Version != "2.9.5" || view.Available[0].Installed {
		t.Fatalf("the offered builds are %+v", view.Available)
	}

	if len(view.Problems) != 1 || !strings.Contains(view.Problems[0].What, "missing") {
		t.Fatalf("the problems are %+v", view.Problems)
	}

	// The binary's path never reaches a page.
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(encoded), fake(t)) {
		t.Fatal("the view a reader gets names the binary's path on this server")
	}

	// An administrator is told the path, and the size beside it.
	encoded, err = json.Marshal(service.View(true))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(encoded), `"path":"`+fake(t)+`"`) || !strings.Contains(string(encoded), `"bytes":`) {
		t.Fatalf("the view an administrator gets is %s", encoded)
	}
}

func TestAPinNeedsAnInstalledVersionAndSurvivesARestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	config := slicers.Config{Dir: dir, OS: runtime.GOOS, Arch: runtime.GOARCH, LocalBinaries: []string{fake(t)}}

	service, err := slicers.Open(config)
	if err != nil {
		t.Fatal(err)
	}

	service.Load(t.Context())

	if err = service.Pin("2.9.5"); !errors.Is(err, install.ErrNotInstalled) {
		t.Fatalf("a pin on a version that is not installed answered %v", err)
	}

	if err = service.Pin("2.9.6"); err != nil {
		t.Fatal(err)
	}

	again, err := slicers.Open(config)
	if err != nil {
		t.Fatal(err)
	}

	if again.Pinned() != "2.9.6" {
		t.Fatalf("after a restart the pin is %q", again.Pinned())
	}

	if err = again.Install(t.Context(), "2.9.6", false, nil); !errors.Is(err, install.ErrNotOffered) {
		t.Fatalf("installing a version no manifest offers answered %v", err)
	}
}

// releases serves the recorded GitHub release list as one page.
func releases(t *testing.T) *httptest.Server {
	t.Helper()

	data, err := os.ReadFile("../../testdata/github-releases.json")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)

	return server
}

// The GitHub releases are offered newest first, a release with no build for
// the host is listed with the reason, a release without a digest needs a
// confirmation, and a newer final release than the default is the update.
func TestTheGitHubReleasesAreOffered(t *testing.T) {
	t.Parallel()

	server := releases(t)

	service, err := slicers.Open(slicers.Config{
		Dir: t.TempDir(), OS: "linux", Arch: "amd64", GitHub: true, GitHubURL: server.URL,
		Client: server.Client(), LocalBinaries: []string{fake(t)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err = service.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	service.Load(t.Context())

	view := service.View(true)

	if view.Catalogue.FetchedAt == nil || view.Catalogue.Supported != ">=2.7.0 <4.0.0" {
		t.Fatalf("the catalogue reads %+v", view.Catalogue)
	}

	var order []string

	for _, offer := range view.Available {
		order = append(order, offer.Version)

		if offer.Version == "2.9.6" && (offer.Compatible || !strings.Contains(offer.Reason, "linux/amd64")) {
			t.Errorf("2.9.6 has no Linux build and reads %+v", offer)
		}

		if offer.Version == "2.8.1" && (!offer.Compatible || offer.Verified || offer.SHA256 != "" ||
			!strings.HasSuffix(offer.Asset, ".AppImage")) {
			t.Errorf("2.8.1 has an AppImage and no digest, and reads %+v", offer)
		}
	}

	if strings.Join(order, " ") != "3.0.0-alpha12 2.9.6 2.9.6-rc1 2.9.5 2.9.2 2.8.1 2.7.4 2.7.0-alpha1" {
		t.Fatalf("the releases are listed as %v", order)
	}

	// The default is the local 2.9.6, and no final release this host can
	// install is newer.
	if view.Update != nil {
		t.Fatalf("an update is offered: %+v", view.Update)
	}

	if err = service.Install(t.Context(), "2.8.1", false, nil); !errors.Is(err, slicers.ErrUnverified) {
		t.Fatalf("an install with no digest answered %v", err)
	}
}

// An offer names the file it downloads and the digest GitHub published for
// it, so a page can show what an install will check.
func TestAnOfferNamesItsAssetAndDigest(t *testing.T) {
	t.Parallel()

	server := releases(t)

	service, err := slicers.Open(slicers.Config{
		Dir: t.TempDir(), OS: "darwin", Arch: "arm64", GitHub: true, GitHubURL: server.URL,
		Client: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err = service.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	service.Load(t.Context())

	for _, offer := range service.View(false).Available {
		if offer.Version != "2.9.6" {
			continue
		}

		if offer.Asset != "PrusaSlicer-2.9.6.dmg" ||
			offer.SHA256 != "94fd7b8a9f87c9631e1c71739b15b184fc5f4c0ceabd69072f1c78f229a4fe40" || !offer.Verified {
			t.Fatalf("2.9.6 reads %+v", offer)
		}

		return
	}

	t.Fatal("2.9.6 is not offered on macOS")
}

// A final release newer than the default is offered as an update, and
// nothing installs it.
func TestANewerFinalReleaseIsOfferedAsAnUpdate(t *testing.T) {
	t.Parallel()

	server := releases(t)
	dir := t.TempDir()

	service, err := slicers.Open(slicers.Config{
		Dir: dir, OS: "linux", Arch: "amd64", GitHub: true, GitHubURL: server.URL, Client: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err = service.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	// The stand-in reads as 2.9.6, so it is named as an older local binary
	// through a manifest of one build on disk.
	older := filepath.Join(t.TempDir(), "old-slicer.sh")
	if err = os.WriteFile(older, []byte("#!/bin/sh\necho 'PrusaSlicer-2.7.4 based on Slic3r'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	service, err = slicers.Open(slicers.Config{
		Dir: dir, OS: "linux", Arch: "amd64", GitHub: true, GitHubURL: server.URL, Client: server.Client(),
		LocalBinaries: []string{older},
	})
	if err != nil {
		t.Fatal(err)
	}

	service.Load(t.Context())

	view := service.View(false)
	if view.Default != "2.7.4" || view.Update == nil || view.Update.Version != "2.8.1" {
		t.Fatalf("the default is %s and the update %+v", view.Default, view.Update)
	}

	if len(view.Installing) != 0 {
		t.Fatal("an update was installed by itself")
	}
}
