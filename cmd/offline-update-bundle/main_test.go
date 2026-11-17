package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

func TestBuildWritesAVerifiableBundle(t *testing.T) {
	t.Parallel()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	core := filepath.Join(root, "core")
	key := filepath.Join(root, "key")
	output := filepath.Join(root, "update.tar")
	if err = os.WriteFile(core, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(key, []byte(base64.StdEncoding.EncodeToString(private.Seed())), 0o600); err != nil {
		t.Fatal(err)
	}

	var report bytes.Buffer
	err = run([]string{
		"build",
		"-out", output,
		"-core", core,
		"-version", "1.2.3",
		"-goos", runtime.GOOS,
		"-goarch", runtime.GOARCH,
		"-profile", "normal",
		"-variant", "full",
		"-private-key", key,
	}, &report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(report.Bytes(), []byte(`"core_version": "1.2.3"`)) {
		t.Fatalf("build report is %s", report.String())
	}
	if err = run([]string{
		"build",
		"-out", output,
		"-core", core,
		"-version", "1.2.3",
		"-goos", runtime.GOOS,
		"-goarch", runtime.GOARCH,
		"-profile", "normal",
		"-variant", "full",
		"-private-key", key,
	}, &report); err == nil {
		t.Fatal("a second build replaced an existing output file")
	}

	file, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	verifier, err := upgrade.NewVerifier([]ed25519.PublicKey{public}, false)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := verifier.VerifyAndStageBundle(file, filepath.Join(root, "staged"), upgrade.BundleVerifyOptions{
		BuildProfile: "normal", BuildVariant: "full",
	})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Manifest.CoreVersion != "1.2.3" {
		t.Fatalf("core version is %q", staged.Manifest.CoreVersion)
	}
}
