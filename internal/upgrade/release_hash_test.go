package upgrade

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// releaseBinary copies the running test executable, a real linked Go
// binary, and appends the marker an older build reserved for a download
// identifier, twice. A hash that rewrote bytes after the marker would differ
// from the plain digest the release workflow signs.
func releaseBinary(t *testing.T, dir, name string) string {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	marker := "PCL-DOWNLOAD-FINGERPRINT:"
	content = append(content, []byte(marker+string(bytes.Repeat([]byte("."), 64))+marker+"tail")...)
	path := filepath.Join(dir, name)
	writeRunnerFile(t, path, string(content), 0o700)

	return path
}

// signLikeRelease writes SHA256SUMS the way `sha256sum -- *` does and signs it
// the way cmd/sign-upgrade-checksums does.
func signLikeRelease(t *testing.T, private ed25519.PrivateKey, binary string) ([]byte, []byte) {
	t.Helper()

	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + filepath.Base(binary) + "\n")

	public, ok := private.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("no Ed25519 public key")
	}
	signature, err := json.Marshal(Signature{
		Algorithm: "ed25519", KeyID: KeyID(public),
		Value: base64.StdEncoding.EncodeToString(ed25519.Sign(private, sums)),
	})
	if err != nil {
		t.Fatal(err)
	}

	return sums, append(signature, '\n')
}

func TestARealBinarySignedLikeAReleaseVerifiesAndApplies(t *testing.T) {
	t.Parallel()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier([]ed25519.PublicKey{public}, false)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	name := ReleaseBinaryName("normal", "full")
	if name == "" {
		name = "prusa_connect_test"
	}
	candidate := releaseBinary(t, filepath.Join(root, "release"), name)
	sums, signature := signLikeRelease(t, private, candidate)
	current := filepath.Join(root, "bin", "core")
	writeRunnerFile(t, current, "old", 0o700)

	service := newReleaseService(t, filepath.Join(root, "upgrade"), current, verifier)
	binary, err := os.Open(candidate)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = binary.Close() }()

	if _, err = service.Stage(t.Context(), StageRequest{
		Name: name, Version: "1.1.0", Binary: binary, Sums: sums, Signature: signature,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Verify(t.Context()); err != nil {
		t.Fatalf("a binary hashed like the release workflow did not verify: %v", err)
	}
	if _, err = service.Preflight(t.Context(), PreflightRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Rehearse(t.Context()); err != nil {
		t.Fatal(err)
	}

	runner, err := NewRunner(RunnerOptions{
		Store:   service.store,
		Backups: BackupManager{Root: filepath.Join(root, "backups"), Keep: 1},
		BackupPlan: func(journal Journal) (BackupPlan, error) {
			return BackupPlan{ID: journal.ID, FromVersion: journal.FromVersion}, nil
		},
		Trial: func(context.Context, Journal) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := runner.Apply(t.Context())
	if err != nil {
		t.Fatalf("apply refused the verified binary: %v", err)
	}
	if journal.State != StateCommitted {
		t.Fatalf("state is %s", journal.State)
	}
}

func TestARealBinaryInAnOfflineBundleKeepsItsIdentity(t *testing.T) {
	t.Parallel()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier([]ed25519.PublicKey{public}, false)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	candidate := releaseBinary(t, filepath.Join(root, "release"), "core")
	var bundle bytes.Buffer
	if _, err = BuildBundle(&bundle, BundleBuild{
		CoreVersion: "1.1.0", BuildProfile: "full", CorePath: candidate, PrivateKey: private,
	}); err != nil {
		t.Fatal(err)
	}

	current := filepath.Join(root, "bin", "core")
	writeRunnerFile(t, current, "old", 0o700)
	service := newReleaseService(t, filepath.Join(root, "upgrade"), current, verifier)
	journal, err := service.StageBundle(t.Context(), BundleStageRequest{Bundle: &bundle})
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyStagedIdentity(journal); err != nil {
		t.Fatalf("the staged bundle core failed its own identity check: %v", err)
	}
}

func newReleaseService(t *testing.T, root, current string, verifier Verifier) *Service {
	t.Helper()

	service, err := NewService(ServiceOptions{
		Root: root, CurrentVersion: "1.0.0", CurrentBinary: current,
		BuildProfile: "full", Contract: "1.0.0", Verifier: verifier,
		Now: func() time.Time { return time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC) },
		Metadata: func(context.Context, string) (BinaryMetadata, error) {
			return BinaryMetadata{
				Version: "1.1.0", BuildProfile: "full", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
				Contract: "1.0.0", HostSchemaVersion: 10, HostSchemaCompat: 9,
			}, nil
		},
		Rehearse: func(context.Context, string) (RehearsalReport, error) {
			return RehearsalReport{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return service
}

// allow_unsigned is for a release an operator uploads. A release the server
// downloads itself is refused without a trusted signature.
func TestAllowUnsignedNeverAppliesToAnOnlineDownload(t *testing.T) {
	t.Parallel()

	verifier, err := NewVerifier(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, online := range []bool{false, true} {
		root := t.TempDir()
		current := filepath.Join(root, "bin", "core")
		writeRunnerFile(t, current, "old", 0o700)
		service := newReleaseService(t, filepath.Join(root, "upgrade"), current, verifier)
		content := []byte("candidate")
		sum := sha256.Sum256(content)
		if _, err = service.Stage(t.Context(), StageRequest{
			Name: "core", Version: "1.1.0", Binary: bytes.NewReader(content),
			Sums: []byte(hex.EncodeToString(sum[:]) + "  core\n"), Online: online,
		}); err != nil {
			t.Fatal(err)
		}
		_, err = service.Verify(t.Context())
		if online && !errors.Is(err, ErrSignature) {
			t.Fatalf("an unsigned online download verified with error %v", err)
		}
		if !online && err != nil {
			t.Fatalf("an unsigned upload under allow_unsigned failed: %v", err)
		}
	}
}

// A bundle that fails verification leaves the finished transaction and its
// staged files as they were; a verified bundle replaces them.
func TestStageBundleVerifiesBeforeItClearsTheLastTransaction(t *testing.T) {
	t.Parallel()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier([]ed25519.PublicKey{public}, false)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	current := filepath.Join(root, "bin", "core")
	writeRunnerFile(t, current, "old", 0o700)
	service := newReleaseService(t, filepath.Join(root, "upgrade"), current, verifier)

	oldStage := filepath.Join(root, "upgrade", "staged", "previous")
	writeRunnerFile(t, filepath.Join(oldStage, "core"), "kept", 0o700)
	finished, err := NewJournal("previous", "1.0.0", "1.1.0", filepath.Join(oldStage, "core"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.store.Transition(finished, StateFailedBeforeSwap, time.Now(), nil); err != nil {
		t.Fatal(err)
	}

	if _, err = service.StageBundle(t.Context(), BundleStageRequest{
		Bundle: bytes.NewReader([]byte("not a bundle")),
	}); err == nil {
		t.Fatal("a broken bundle staged")
	}
	if got := readRunnerFile(t, filepath.Join(oldStage, "core")); got != "kept" {
		t.Fatalf("the last transaction's files changed: %q", got)
	}
	if journal, loadErr := service.store.Load(); loadErr != nil || journal.ID != "previous" {
		t.Fatalf("the last journal is %v, error %v", journal.ID, loadErr)
	}

	candidate := releaseBinary(t, filepath.Join(root, "release"), "core")
	var bundle bytes.Buffer
	if _, err = BuildBundle(&bundle, BundleBuild{
		CoreVersion: "1.1.0", BuildProfile: "full", CorePath: candidate, PrivateKey: private,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.StageBundle(t.Context(), BundleStageRequest{Bundle: &bundle}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(oldStage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the finished transaction's files are left: %v", err)
	}
}
