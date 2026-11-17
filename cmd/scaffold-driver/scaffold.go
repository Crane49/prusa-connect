package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"strings"

	modulescaffold "github.com/prusa3d/prusa-connect/internal/app/scaffold"
)

// templates are the files a scaffolded driver is rendered from. They are
// .tmpl files and not Go strings because of the backticks in struct tags.
//
//go:embed templates/*.tmpl
var templates embed.FS

const (
	dirPerms  = 0o750
	filePerms = 0o600

	// sdkModule is the module a driver compiles against. The replace
	// directive in the scaffolded go.mod replaces it.
	sdkModule = "github.com/prusa3d/prusa-connect/sdk"
)

var (
	// ErrNoSDK means no usable sdk directory was given or found above the
	// working directory.
	ErrNoSDK = errors.New("no sdk directory")
	// ErrOccupied means the output directory already has one of the files
	// the scaffold writes. Nothing is overwritten.
	ErrOccupied = errors.New("the output directory already holds a scaffolded file")
)

// Plan is one scaffold, fully resolved. Rendering reads no flag and does not
// probe the filesystem to decide what to write.
type Plan struct {
	// Name is the driver type an operator writes in configuration.
	Name string
	// Module is the Go module path of the new driver.
	Module string
	// Package is the Go package name inside that module.
	Package string
	// Namespace is what a host derives from the module path. The driver's
	// own tests see "core" instead, because there it is the main module.
	Namespace string
	// Qualified is "namespace/name", for configuration when two modules
	// register the same bare name.
	Qualified string
	// Dir is the directory the files are written into.
	Dir string
	// SDKPath is the absolute path the replace directive points at. It has
	// forward slashes because go.mod needs them on every platform.
	SDKPath string
	// GoVersion is the go directive of the new go.mod, copied from the sdk
	// so a driver never declares less than the sdk.
	GoVersion string
	// CorePath is the core checkout build.yaml composes against, with
	// forward slashes. It is the parent directory of the sdk.
	CorePath string
	// KindName is the device kind this module declares, or empty when its
	// machines are printers. A storage scaffold uses it as the display name.
	KindName string
	// Storage selects a storage provider instead of a printer dialect. Both
	// share only go.mod and build.yaml.
	Storage bool
}

// files maps each template to the file it renders to. Every scaffold gets a
// build.yaml, because a module must be composed into a binary to be used.
func (p Plan) files() map[string]string {
	out := map[string]string{
		"templates/go.mod.tmpl":     "go.mod",
		"templates/build.yaml.tmpl": "build.yaml",
	}

	if p.Storage {
		out["templates/storage.go.tmpl"] = p.Name + ".go"
		out["templates/storage_test.go.tmpl"] = p.Name + "_test.go"
		out["templates/storage_internal_test.go.tmpl"] = p.Name + "_internal_test.go"

		return out
	}

	out["templates/driver.go.tmpl"] = p.Name + ".go"
	out["templates/driver_test.go.tmpl"] = p.Name + "_test.go"
	out["templates/faults.go.tmpl"] = "faults.go"
	out["templates/README.md.tmpl"] = "README.md"
	out["templates/CHANGELOG.md.tmpl"] = "CHANGELOG.md"

	if p.KindName != "" {
		out["templates/kind.go.tmpl"] = "kind.go"
	}

	return out
}

// write renders every template before it writes any file, and returns the
// written paths sorted. An existing target file fails with ErrOccupied.
func write(plan Plan) ([]string, error) {
	if err := os.MkdirAll(plan.Dir, dirPerms); err != nil {
		return nil, fmt.Errorf("create %s: %w", plan.Dir, err)
	}

	rendered := make(map[string][]byte, len(plan.files()))

	for source, target := range plan.files() {
		path := filepath.Join(plan.Dir, target)

		if _, err := os.Stat(path); err == nil {
			return nil, fmt.Errorf("%w: %s", ErrOccupied, path)
		}

		body, err := render(source, plan)
		if err != nil {
			return nil, err
		}

		rendered[path] = body
	}

	written := make([]string, 0, len(rendered))

	for path, body := range rendered {
		if err := os.WriteFile(path, body, filePerms); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}

		written = append(written, path)
	}

	slices.Sort(written)

	return written, nil
}

// render fills one template with internal/app/scaffold and formats Go output.
// go/format stays here: that package must not link the parser into the server.
func render(source string, plan Plan) ([]byte, error) {
	filled, err := modulescaffold.Render(templates, source, plan)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	if !strings.HasSuffix(source, ".go.tmpl") {
		return filled, nil
	}

	formatted, err := format.Source(filled)
	if err != nil {
		return nil, fmt.Errorf("format %s: %w", source, err)
	}

	return formatted, nil
}

// sdkPath resolves the directory the replace directive points at. A given
// path is checked too, so a wrong -sdk fails here and not at the first build.
func sdkPath(given string) (string, error) {
	if given != "" {
		absolute, err := filepath.Abs(given)
		if err != nil {
			return "", fmt.Errorf("resolve -sdk %s: %w", given, err)
		}

		if err = checkSDK(absolute); err != nil {
			return "", err
		}

		return filepath.ToSlash(absolute), nil
	}

	found, err := findSDK()
	if err != nil {
		return "", err
	}

	return filepath.ToSlash(found), nil
}

// findSDK walks up from the working directory to the first directory with
// an sdk module in it.
func findSDK() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("read the working directory: %w", err)
	}

	for {
		candidate := filepath.Join(dir, "sdk")
		if checkSDK(candidate) == nil {
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(
				"%w: no sdk/go.mod above the working directory. Pass -sdk with the path to the sdk "+
					"directory of a prusa-connect checkout", ErrNoSDK)
		}

		dir = parent
	}
}

// checkSDK returns nil when dir is the sdk module of a prusa-connect
// checkout.
func checkSDK(dir string) error {
	body, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return fmt.Errorf("%w: %s holds no go.mod: %w", ErrNoSDK, dir, err)
	}

	if !bytes.Contains(body, []byte("module "+sdkModule)) {
		return fmt.Errorf("%w: %s is not %s", ErrNoSDK, dir, sdkModule)
	}

	return nil
}

// goVersion reads the go directive of the sdk's go.mod.
func goVersion(sdk string) (string, error) {
	body, err := os.ReadFile(filepath.Join(sdk, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read the sdk go.mod: %w", err)
	}

	for line := range strings.SplitSeq(string(body), "\n") {
		field, version, found := strings.Cut(strings.TrimSpace(line), " ")
		if found && field == "go" {
			return strings.TrimSpace(version), nil
		}
	}

	return "", fmt.Errorf("%w: %s/go.mod declares no go version", ErrNoSDK, sdk)
}
