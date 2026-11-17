// Command offline-update-bundle builds and inspects signed offline updates.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "offline-update-bundle: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: offline-update-bundle build|inspect [flags]")
	}

	switch args[0] {
	case "build":
		return build(args[1:], out)
	case "inspect":
		return inspect(args[1:], out)
	default:
		return fmt.Errorf("unknown command %q; expected build or inspect", args[0])
	}
}

func build(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	flags.SetOutput(out)
	var extensions values
	output := flags.String("out", "", "output .tar path")
	core := flags.String("core", "", "core executable path")
	version := flags.String("version", "", "core semantic version")
	goos := flags.String("goos", "", "target operating system")
	goarch := flags.String("goarch", "", "target architecture")
	profile := flags.String("profile", "", "target build profile")
	variant := flags.String("variant", "", "target build variant")
	keyFile := flags.String("private-key", "", "file containing a base64 Ed25519 private key or seed")
	flags.Var(&extensions, "extension", "module=release.tar.gz; repeat for each extension")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *output == "" || *core == "" || *version == "" ||
		*goos == "" || *goarch == "" || *profile == "" || *variant == "" || *keyFile == "" {
		return errors.New(
			"build needs -out, -core, -version, -goos, -goarch, -profile, -variant and -private-key",
		)
	}

	private, err := readPrivateKey(*keyFile)
	if err != nil {
		return err
	}
	request := upgrade.BundleBuild{
		CoreVersion: *version, GOOS: *goos, GOARCH: *goarch,
		BuildProfile: *profile, BuildVariant: *variant, CorePath: *core, PrivateKey: private,
	}
	for _, value := range extensions {
		module, file, found := strings.Cut(value, "=")
		if !found || strings.TrimSpace(module) == "" || strings.TrimSpace(file) == "" {
			return fmt.Errorf("-extension %q must be module=release.tar.gz", value)
		}
		request.Extensions = append(request.Extensions, upgrade.BundleBuildExtension{
			Module: strings.TrimSpace(module), Path: strings.TrimSpace(file),
		})
	}

	if err = os.MkdirAll(filepath.Dir(*output), 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(*output)
		}
	}()

	manifest, buildErr := upgrade.BuildBundle(file, request)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(buildErr, syncErr, closeErr); err != nil {
		return err
	}
	keep = true

	return writeJSON(out, manifest)
}

func inspect(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(out)
	bundlePath := flags.String("bundle", "", "offline bundle path")
	configPath := flags.String("config", "", "configuration holding trusted upgrade keys")
	profile := flags.String("profile", buildprofile.Name, "expected build profile")
	variant := flags.String("variant", buildprofile.Variant, "expected build variant")
	contract := flags.Int("contract-major", currentContractMajor(), "expected extension wire contract major")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *bundlePath == "" || *configPath == "" {
		return errors.New("inspect needs -bundle and -config")
	}

	core, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err = core.Validate(); err != nil {
		return err
	}
	keys := make([]upgrade.TrustedKey, 0, len(core.Upgrade.TrustedKeys))
	for _, key := range core.Upgrade.TrustedKeys {
		keys = append(keys, upgrade.TrustedKey{ID: key.ID, Key: key.Key})
	}
	verifier, err := upgrade.BuildVerifier(keys, core.Upgrade.AllowUnsigned)
	if err != nil {
		return err
	}
	source, err := os.Open(*bundlePath)
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	defer func() { _ = source.Close() }()
	root, err := os.MkdirTemp("", "offline-update-inspect-")
	if err != nil {
		return fmt.Errorf("create inspection directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(root) }()

	staged, err := verifier.VerifyAndStageBundle(source, filepath.Join(root, "verified"), upgrade.BundleVerifyOptions{
		BuildProfile: *profile, BuildVariant: *variant, ContractMajor: *contract,
	})
	if err != nil {
		return err
	}

	return writeJSON(out, struct {
		KeyID    string                 `json:"key_id"`
		Manifest upgrade.BundleManifest `json:"manifest"`
	}{
		KeyID: staged.KeyID, Manifest: staged.Manifest,
	})
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(content)))
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf(
			"private key holds %d bytes, expected %s or %s",
			len(raw), strconv.Itoa(ed25519.SeedSize), strconv.Itoa(ed25519.PrivateKeySize),
		)
	}
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	return encoder.Encode(value)
}

type values []string

func currentContractMajor() int {
	major, _, _ := strings.Cut(wirerpc.Contract.String(), ".")
	value, _ := strconv.Atoi(major)

	return value
}

func (v *values) String() string { return strings.Join(*v, ",") }

func (v *values) Set(value string) error {
	*v = append(*v, value)

	return nil
}
