package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOfflineBundleIsDeterministicAndStagesVerifiedArtifacts(t *testing.T) {
	t.Parallel()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	core := filepath.Join(root, "prusa-connect")
	if err = os.WriteFile(core, []byte("core executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	extension := moduleArchive(t, moduleYAML("1.2.3", ">=2.0.0 <3.0.0", 1))

	request := BundleBuild{
		CoreVersion: "2.1.0", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		BuildProfile: "normal", BuildVariant: "full", CorePath: core,
		Extensions: []BundleBuildExtension{{Module: "example.test/spools", Path: extension}},
		PrivateKey: private,
	}
	var first, second bytes.Buffer
	firstManifest, err := BuildBundle(&first, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = BuildBundle(&second, request); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("the same inputs produced different bundle bytes")
	}

	verifier, err := NewVerifier([]ed25519.PublicKey{public}, false)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "staged")
	staged, err := verifier.VerifyAndStageBundle(bytes.NewReader(first.Bytes()), destination, BundleVerifyOptions{
		BuildProfile: "normal", BuildVariant: "full", ContractMajor: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Manifest.CoreVersion != "2.1.0" || staged.KeyID != KeyID(public) {
		t.Fatalf("staged identity is %#v", staged)
	}
	if staged.CorePath != filepath.Join(destination, filepath.FromSlash(firstManifest.Core.Path)) {
		t.Fatalf("core path is %q", staged.CorePath)
	}
	if len(staged.Extensions) != 1 || staged.Extensions[0].Module != "example.test/spools" {
		t.Fatalf("extensions are %#v", staged.Extensions)
	}
	if body, readErr := os.ReadFile(staged.CorePath); readErr != nil || string(body) != "core executable" {
		t.Fatalf("staged core is %q, %v", body, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(destination, BundleManifestName)); statErr != nil {
		t.Fatal(statErr)
	}
}

func TestOfflineBundleRejectsTrustCompatibilityAndTampering(t *testing.T) {
	t.Parallel()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(t.TempDir(), "core")
	if err = os.WriteFile(core, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if _, err = BuildBundle(&archive, BundleBuild{
		CoreVersion: "1.1.0", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		BuildProfile: "normal", BuildVariant: "full", CorePath: core, PrivateKey: private,
	}); err != nil {
		t.Fatal(err)
	}

	trusted, err := NewVerifier([]ed25519.PublicKey{public}, false)
	if err != nil {
		t.Fatal(err)
	}
	untrusted, err := NewVerifier(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Clone(archive.Bytes())
	payload := bytes.Index(tampered, []byte("candidate"))
	if payload < 0 {
		t.Fatal("candidate payload is absent")
	}
	tampered[payload] ^= 1

	tests := []struct {
		name     string
		verifier Verifier
		content  []byte
		options  BundleVerifyOptions
		want     error
	}{
		{name: "unknown key", verifier: untrusted, content: archive.Bytes(), want: ErrSignature},
		{name: "wrong operating system", verifier: trusted, content: archive.Bytes(),
			options: BundleVerifyOptions{GOOS: "not-" + runtime.GOOS}, want: ErrBundleCompatibility},
		{name: "wrong profile", verifier: trusted, content: archive.Bytes(),
			options: BundleVerifyOptions{BuildProfile: "printer"}, want: ErrBundleCompatibility},
		{name: "changed artifact", verifier: trusted, content: tampered, want: ErrChecksum},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			destination := filepath.Join(t.TempDir(), "stage")
			_, verifyErr := test.verifier.VerifyAndStageBundle(
				bytes.NewReader(test.content), destination, test.options,
			)
			if !errors.Is(verifyErr, test.want) {
				t.Fatalf("error is %v, want %v", verifyErr, test.want)
			}
			if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("failed verification published staging directory: %v", statErr)
			}
		})
	}
}

func TestOfflineBundleCannotUseTheUnsignedReleaseEscapeHatch(t *testing.T) {
	t.Parallel()

	core := []byte("core")
	sum := sha256.Sum256(core)
	declared := BundleManifest{
		Schema: BundleSchema, CoreVersion: "1.0.0", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Core:       BundleArtifact{Path: "core/server", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(core))},
		Extensions: []BundleExtension{},
	}
	document, err := json.Marshal(declared)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err = writeTarBytes(writer, BundleManifestName, document, privateFile); err != nil {
		t.Fatal(err)
	}
	if err = writeTarBytes(writer, BundleSignatureName, nil, privateFile); err != nil {
		t.Fatal(err)
	}
	if err = writeTarBytes(writer, "core/server", core, executablePerm); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = verifier.VerifyAndStageBundle(
		bytes.NewReader(archive.Bytes()), filepath.Join(t.TempDir(), "stage"), BundleVerifyOptions{},
	)
	if !errors.Is(err, ErrSignature) {
		t.Fatalf("error is %v", err)
	}
}

func TestOfflineBundleRejectsMalformedArchiveLayout(t *testing.T) {
	t.Parallel()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	core := []byte("core")
	sum := sha256.Sum256(core)
	declared := BundleManifest{
		Schema: BundleSchema, CoreVersion: "1.0.0", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Core:       BundleArtifact{Path: "core/server", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(core))},
		Extensions: []BundleExtension{},
	}

	tests := []struct {
		name     string
		entries  []testTarEntry
		trailing bool
	}{
		{name: "duplicate", entries: []testTarEntry{
			{name: "core/server", body: core}, {name: "core/server", body: core},
		}},
		{name: "traversal", entries: []testTarEntry{{name: "../server", body: core}}},
		{name: "absolute path", entries: []testTarEntry{{name: "/server", body: core}}},
		{name: "link", entries: []testTarEntry{{name: "core/server", link: "elsewhere"}}},
		{name: "undeclared", entries: []testTarEntry{
			{name: "core/server", body: core}, {name: "extra", body: []byte("x")},
		}},
		{name: "missing", entries: nil},
		{name: "trailing bytes", entries: []testTarEntry{{name: "core/server", body: core}}, trailing: true},
	}
	verifier, err := NewVerifier([]ed25519.PublicKey{public}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			archive := signedTestBundle(t, declared, private, test.entries)
			if test.trailing {
				archive = append(archive, 1)
			}
			_, verifyErr := verifier.VerifyAndStageBundle(
				bytes.NewReader(archive), filepath.Join(t.TempDir(), "stage"), BundleVerifyOptions{},
			)
			if !errors.Is(verifyErr, ErrBundle) {
				t.Fatalf("error is %v", verifyErr)
			}
		})
	}
}

func TestOfflineBundleManifestLimitsAreStrict(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("0", sha256.Size*2)
	base := BundleManifest{
		Schema: BundleSchema, CoreVersion: "1.0.0", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Core:       BundleArtifact{Path: "core/server", SHA256: digest, Size: 1},
		Extensions: []BundleExtension{},
	}
	options := bundleVerifyDefaults(BundleVerifyOptions{MaxExtensions: 2, MaxExtensionBytes: 8})

	tooMany := base
	for index := range 3 {
		tooMany.Extensions = append(tooMany.Extensions, BundleExtension{
			Module: "example.test/module" + strconv.Itoa(index),
			Name:   "module" + strconv.Itoa(index), Version: "1.0.0",
			Path:   "extensions/module" + strconv.Itoa(index) + ".tar.gz",
			SHA256: digest, Size: 1,
		})
	}
	if err := validateBundleManifest(tooMany, options); !errors.Is(err, ErrBundle) {
		t.Fatalf("extension count error is %v", err)
	}

	traversal := base
	traversal.Core.Path = "../server"
	if err := validateBundleManifest(traversal, options); !errors.Is(err, ErrBundle) {
		t.Fatalf("path error is %v", err)
	}

	oversized := base
	oversized.Extensions = []BundleExtension{{
		Module: "example.test/spools", Name: "spools", Version: "1.0.0",
		Path: "extensions/spools.tar.gz", SHA256: digest, Size: 9,
	}}
	if err := validateBundleManifest(oversized, options); !errors.Is(err, ErrBundle) {
		t.Fatalf("size error is %v", err)
	}
}

func TestOfflineBundleRejectsIncompatibleAndMisdeclaredExtensions(t *testing.T) {
	t.Parallel()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(t.TempDir(), "core")
	if err = os.WriteFile(core, []byte("core"), 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		moduleYML string
		core      string
		contract  int
		want      error
	}{
		{
			name: "core range", moduleYML: moduleYAML("1.0.0", ">=3.0.0", 1),
			core: "2.0.0", contract: 1, want: ErrBundleCompatibility,
		},
		{
			name: "wire contract", moduleYML: moduleYAML("1.0.0", ">=2.0.0", 2),
			core: "2.0.0", contract: 1, want: ErrBundleCompatibility,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			extension := moduleArchive(t, test.moduleYML)
			var out bytes.Buffer
			_, buildErr := BuildBundle(&out, BundleBuild{
				CoreVersion: test.core, BuildProfile: "normal", BuildVariant: "full",
				CorePath: core, PrivateKey: private,
				Extensions: []BundleBuildExtension{{Module: "example.test/spools", Path: extension}},
			})
			if !errors.Is(buildErr, test.want) {
				// Compatibility is deliberately checked by the verifier, not by a
				// cross-platform builder.
				if buildErr != nil {
					t.Fatal(buildErr)
				}
				public := private.Public().(ed25519.PublicKey)
				verifier, verifierErr := NewVerifier([]ed25519.PublicKey{public}, false)
				if verifierErr != nil {
					t.Fatal(verifierErr)
				}
				_, verifyErr := verifier.VerifyAndStageBundle(
					bytes.NewReader(out.Bytes()), filepath.Join(t.TempDir(), "stage"),
					BundleVerifyOptions{
						BuildProfile: "normal", BuildVariant: "full", ContractMajor: test.contract,
					},
				)
				if !errors.Is(verifyErr, test.want) {
					t.Fatalf("error is %v, want %v", verifyErr, test.want)
				}
			}
		})
	}
}

func TestOfflineBundleBuilderRejectsExtensionLinks(t *testing.T) {
	t.Parallel()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	core := filepath.Join(root, "core")
	if err = os.WriteFile(core, []byte("core"), 0o700); err != nil {
		t.Fatal(err)
	}
	extension := filepath.Join(root, "extension.tar.gz")
	file, err := os.Create(extension)
	if err != nil {
		t.Fatal(err)
	}
	zipped := gzip.NewWriter(file)
	writer := tar.NewWriter(zipped)
	if err = writer.WriteHeader(&tar.Header{
		Name: moduleManifestName, Typeflag: tar.TypeSymlink, Linkname: "elsewhere",
	}); err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(writer.Close(), zipped.Close(), file.Close()); err != nil {
		t.Fatal(err)
	}

	_, err = BuildBundle(io.Discard, BundleBuild{
		CoreVersion: "1.0.0", CorePath: core, PrivateKey: private,
		Extensions: []BundleBuildExtension{{Module: "example.test/spools", Path: extension}},
	})
	if !errors.Is(err, ErrBundle) {
		t.Fatalf("error is %v", err)
	}
}

type testTarEntry struct {
	name string
	body []byte
	link string
}

func signedTestBundle(
	t *testing.T,
	declared BundleManifest,
	private ed25519.PrivateKey,
	entries []testTarEntry,
) []byte {
	t.Helper()

	document, err := json.Marshal(declared)
	if err != nil {
		t.Fatal(err)
	}
	detached, err := json.Marshal(Signature{
		Algorithm: "ed25519", KeyID: KeyID(private.Public().(ed25519.PublicKey)),
		Value: base64.StdEncoding.EncodeToString(ed25519.Sign(private, document)),
	})
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	writer := tar.NewWriter(&out)
	if err = writeTarBytes(writer, BundleManifestName, document, privateFile); err != nil {
		t.Fatal(err)
	}
	if err = writeTarBytes(writer, BundleSignatureName, detached, privateFile); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		header := deterministicHeader(entry.name, int64(len(entry.body)), privateFile)
		if entry.link != "" {
			header.Typeflag = tar.TypeSymlink
			header.Linkname = entry.link
			header.Size = 0
		}
		if err = writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if entry.link == "" {
			if _, err = writer.Write(entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}

	return out.Bytes()
}

func moduleArchive(t *testing.T, document string) string {
	t.Helper()

	name := filepath.Join(t.TempDir(), "extension.tar.gz")
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	zipped := gzip.NewWriter(file)
	zipped.Header.ModTime = time.Unix(0, 0)
	writer := tar.NewWriter(zipped)
	files := map[string][]byte{
		moduleManifestName: []byte(document),
		"spools_" + runtime.GOOS + "_" + runtime.GOARCH: []byte("extension"),
	}
	for _, path := range sortedKeys(files) {
		header := deterministicHeader(path, int64(len(files[path])), 0o700)
		if err = writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if _, err = writer.Write(files[path]); err != nil {
			t.Fatal(err)
		}
	}
	if err = errors.Join(writer.Close(), zipped.Close(), file.Close()); err != nil {
		t.Fatal(err)
	}

	return name
}

func moduleYAML(version, coreRange string, contract int) string {
	return strings.Join([]string{
		"name: spools",
		"kind: sink",
		"license: MIT",
		"version: " + version,
		"contract_major: " + strconv.Itoa(contract),
		"platforms:",
		"  - " + runtime.GOOS + "_" + runtime.GOARCH,
		"require:",
		"  core: " + strconv.Quote(coreRange),
		"",
	}, "\n")
}
