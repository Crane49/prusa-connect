package arch_test

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const sdkModulePath = modulePath + "/sdk"

// authorSource is what a community extension author writes: a module that
// imports the published sdk path and serves HTTP routes the way ADR-20260827-wmv says a
// module does. It names no application package and carries no replace, so it
// compiles only if sdk/go.mod is complete on its own.
//
// The sdk module depends on nothing outside the standard library, which is
// the in-process plugin contract of the retired server, and it is deleted: a
// module is declared in configuration and answers routesapi.MethodServe over
// the wire, so what an author writes is an ordinary http.Handler and nothing
// else. Nothing here reaches outside the standard library, which is now what
// sdk/go.mod requires as well.
const authorSource = `package plugin

import (
	"net/http"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// Methods is the set this module answers on the wire. The host calls
// routesapi.MethodServe with one request and reads back one response; the
// principal is the host's own answer, not a header the caller set.
func Methods() wirerpc.Methods {
	mux := http.NewServeMux()
	mux.HandleFunc("/spools", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(routesapi.PrincipalOf(r)))
	})

	return wirerpc.Methods{routesapi.MethodServe: routesapi.Serve(mux)}
}
`

// TestAnOutsideModuleCompilesAgainstTheSdk builds a throwaway module that
// requires github.com/prusa3d/prusa-connect/sdk by its published path,
// with no replace directive, the way somebody who never cloned this repository
// would.
//
// go.mod:127 has `replace github.com/prusa3d/prusa-connect/sdk => ./sdk`,
// so every other build and test in this tree resolves the directory rather than
// the module. TestTheSdkImportsOnlyTheStandardLibrary reads the sdk's package
// list and says nothing about whether the module resolves at all: an sdk/go.mod
// that no outside module could use would leave this tree entirely green.
//
// What it proves: sdk/go.mod is self-contained, the module zip an author would
// download builds, and its exported surface is enough to serve a module's own
// HTTP routes.
//
// What it does not prove: that any tag exists, that a proxy or a VCS host would
// serve it, or that the sdk's own dependencies are fetchable from the network.
// The synthetic version is served from a proxy built here, and the sdk's
// dependencies come from this machine's module cache.
func TestAnOutsideModuleCompilesAgainstTheSdk(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	sdkDir := filepath.Join(root, "sdk")

	// The version is derived from the sdk's own bytes because the module cache
	// is shared with the rest of this machine: a fixed version would be
	// extracted once and every later run would compile the copy from the first.
	version := "v0.0.1-gate.h" + sdkFingerprint(t, sdkDir)

	proxy := filepath.Join(t.TempDir(), "proxy")
	publish(t, sdkDir, proxy, version)

	author := filepath.Join(t.TempDir(), "author")
	write(t, filepath.Join(author, "go.mod"), fmt.Sprintf(
		"module example.com/outside/routeplugin\n\ngo %s\n\nrequire %s %s\n",
		goDirective(t, filepath.Join(sdkDir, "go.mod")), sdkModulePath, version,
	))
	write(t, filepath.Join(author, "plugin.go"), authorSource)

	out, err := runGo(t, author, proxy, "build", "./...")
	if err != nil {
		t.Fatalf("an outside module does not compile against %s %s.\n"+
			"sdk/go.mod is what an author downloads, and nothing else in this tree resolves it.\n"+
			"If the failure is a missing dependency, warm the cache with `cd sdk && go mod download`.\n\n%s\n%v",
			sdkModulePath, version, out, err)
	}

	// What the author actually links, as evidence for ADR-20260824-gfr rule 2's cost.
	// It is logged rather than asserted: TestTheSdkImportsOnlyTheStandardLibrary
	// is the gate on the import list, and two gates on one invariant is the
	// shape where the forgotten copy is the wrong one.
	modules, err := runGo(t, author, proxy,
		"list", "-deps", "-f", "{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{end}}", "./...")
	if err != nil {
		t.Logf("the author's build list could not be read: %s\n%v", modules, err)

		return
	}

	t.Logf("an author serving a module's routes links:\n%s", distinct(modules))
}

// runGo runs the go command in dir against the synthetic proxy alone. The
// environment is built rather than inherited: GOFLAGS or GOWORK from the
// developer's shell would let the child resolve the sdk some way an author
// cannot.
func runGo(t *testing.T, dir, proxy string, args ...string) (string, error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), goTool(t), args...)
	cmd.Dir = dir
	cmd.Env = append(passThrough(),
		"GOPROXY="+fileURL(proxy)+","+fileURL(filepath.Join(goEnv(t, "GOMODCACHE"), "cache", "download"))+",off",
		"GOFLAGS=-mod=mod",
		"GOWORK=off",
		"GOSUMDB=off",
		"GONOSUMDB=*",
		"GOPRIVATE=",
		"GOTOOLCHAIN=local",
	)

	out, err := cmd.CombinedOutput()

	return string(out), err
}

// passThrough is the environment a hermetic child still needs: where the
// toolchain lives, where its caches are, and enough of the operating system to
// start a process.
func passThrough() []string {
	keep := []string{
		"PATH", "Path", "HOME", "USERPROFILE", "SystemRoot", "SYSTEMROOT", "COMSPEC", "ComSpec",
		"TMP", "TEMP", "TMPDIR", "GOROOT", "GOCACHE", "GOMODCACHE", "GOPATH", "LOCALAPPDATA", "APPDATA",
	}

	out := make([]string, 0, len(keep))

	for _, name := range keep {
		if value, ok := os.LookupEnv(name); ok {
			out = append(out, name+"="+value)
		}
	}

	return out
}

func goTool(t *testing.T) string {
	t.Helper()

	name := "go"
	if runtime.GOOS == "windows" {
		name = "go.exe"
	}

	if root, ok := os.LookupEnv("GOROOT"); ok {
		if candidate := filepath.Join(root, "bin", name); fileExists(candidate) {
			return candidate
		}
	}

	found, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the go command is not on PATH: %v", err)
	}

	return found
}

func goEnv(t *testing.T, name string) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), goTool(t), "env", name).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", name, err)
	}

	return strings.TrimSpace(string(out))
}

// escapePath is the case-encoding a module proxy's directory layout uses: an
// upper-case letter becomes "!" and its lower-case form, so two module paths
// differing only in case cannot land on one directory on a case-insensitive
// file system. The module path carries capitals (prusa3d), so the
// encoding is load-bearing rather than decorative.
func escapePath(modulePath string) string {
	var out strings.Builder

	for _, r := range modulePath {
		if r >= 'A' && r <= 'Z' {
			out.WriteByte('!')
			out.WriteRune(r + ('a' - 'A'))

			continue
		}

		out.WriteRune(r)
	}

	return out.String()
}

// publish writes sdkDir into proxy in the layout a Go module proxy serves:
// @v/<version>.info, @v/<version>.mod and @v/<version>.zip, under the
// case-encoded module path.
func publish(t *testing.T, sdkDir, proxy, version string) {
	t.Helper()

	at := filepath.Join(proxy, filepath.FromSlash(escapePath(sdkModulePath)), "@v")

	gomod, err := os.ReadFile(filepath.Join(sdkDir, "go.mod"))
	if err != nil {
		t.Fatalf("reading sdk/go.mod: %v", err)
	}

	write(t, filepath.Join(at, version+".info"), fmt.Sprintf(
		`{"Version":%q,"Time":"2026-01-01T00:00:00Z"}`, version))
	write(t, filepath.Join(at, version+".mod"), string(gomod))
	write(t, filepath.Join(at, "list"), version+"\n")

	archive, err := os.Create(filepath.Join(at, version+".zip"))
	if err != nil {
		t.Fatalf("creating the module zip: %v", err)
	}

	defer func() { _ = archive.Close() }()

	writer := zip.NewWriter(archive)
	prefix := sdkModulePath + "@" + version + "/"

	for _, rel := range sdkFiles(t, sdkDir) {
		entry, createErr := writer.Create(prefix + rel)
		if createErr != nil {
			t.Fatalf("adding %s to the module zip: %v", rel, createErr)
		}

		body, readErr := os.ReadFile(filepath.Join(sdkDir, filepath.FromSlash(rel)))
		if readErr != nil {
			t.Fatalf("reading %s: %v", rel, readErr)
		}

		if _, writeErr := entry.Write(body); writeErr != nil {
			t.Fatalf("writing %s into the module zip: %v", rel, writeErr)
		}
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("closing the module zip: %v", err)
	}

	if err = archive.Close(); err != nil {
		t.Fatalf("closing the module zip file: %v", err)
	}
}

// sdkFiles is every regular file under sdkDir, as slash-separated paths sorted
// so the zip and the fingerprint are stable across runs and platforms.
func sdkFiles(t *testing.T, sdkDir string) []string {
	t.Helper()

	var out []string

	err := filepath.WalkDir(sdkDir, func(where string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fs.SkipDir
			}

			return nil
		}

		if !entry.Type().IsRegular() {
			return nil
		}

		rel, relErr := filepath.Rel(sdkDir, where)
		if relErr != nil {
			return relErr
		}

		out = append(out, filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		t.Fatalf("walking sdk/: %v", err)
	}

	sort.Strings(out)

	return out
}

// sdkFingerprint is twelve hex digits over every file's path and contents.
func sdkFingerprint(t *testing.T, sdkDir string) string {
	t.Helper()

	sum := sha256.New()

	for _, rel := range sdkFiles(t, sdkDir) {
		body, err := os.ReadFile(filepath.Join(sdkDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}

		_, _ = fmt.Fprintf(sum, "%s\x00%d\x00", rel, len(body))
		_, _ = sum.Write(body)
	}

	return hex.EncodeToString(sum.Sum(nil))[:12]
}

// goDirective is the go version sdk/go.mod declares, so the throwaway module
// asks for the same language version rather than whatever this file was written
// against.
func goDirective(t *testing.T, gomod string) string {
	t.Helper()

	body, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatalf("reading %s: %v", gomod, err)
	}

	for line := range strings.SplitSeq(string(body), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "go" {
			return fields[1]
		}
	}

	t.Fatalf("%s declares no go directive", gomod)

	return ""
}

// distinct is the unique non-empty lines of out, sorted, one per line.
func distinct(out string) string {
	seen := map[string]bool{}

	for line := range strings.SplitSeq(out, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			seen[trimmed] = true
		}
	}

	return strings.Join(sortedKeys(seen), "\n")
}

func fileURL(dir string) string {
	where := filepath.ToSlash(dir)
	if !strings.HasPrefix(where, "/") {
		where = "/" + where
	}

	return "file://" + (&url.URL{Path: where}).EscapedPath()
}

func fileExists(where string) bool {
	info, err := os.Stat(where)

	return err == nil && info.Mode().IsRegular()
}

func write(t *testing.T, where, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(where), 0o755); err != nil {
		t.Fatalf("creating %s: %v", path.Dir(filepath.ToSlash(where)), err)
	}

	if err := os.WriteFile(where, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", where, err)
	}
}
