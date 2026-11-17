package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

const (
	// repoRoot is the checkout this test scaffolds against, which is two levels
	// above the package directory.
	repoRoot = "../.."

	// driverName and driverModule are what every case here scaffolds. The two
	// grammars are held by TestANameThatCannotBeConfiguredIsRefused and
	// TestAModulePathNoHostCanNamespaceIsRefused, so nothing else varies them.
	driverName   = "acme"
	driverModule = "example.com/prusa-driver-acme"
)

// TestAScaffoldedDriverPassesTheConformanceSuite is the only claim this command
// makes that matters: what it writes compiles and passes the published suite
// without an author touching it.
//
// It runs the go tool, which costs a few seconds, and it deliberately does not
// skip under -short: CI runs `go test -short`, so a skip here would leave the
// scaffold ungated everywhere it matters.
func TestAScaffoldedDriverPassesTheConformanceSuite(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool on PATH: %v", err)
	}

	dir := scaffold(t)

	cmd := exec.CommandContext(t.Context(), goTool, "test", "-count=1", "./...")
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test in the scaffolded module: %v\n%s", err, out)
	}

	t.Logf("go test ./...\n%s", out)
}

// TestTheModuleIsBuiltAgainstTheCheckout holds the one thing an author cannot
// work out for themselves: the sdk module has no published version, so the
// generated go.mod has to point at a checkout or the first build fails on a
// module the proxy does not serve.
func TestTheModuleIsBuiltAgainstTheCheckout(t *testing.T) {
	dir := scaffold(t)

	body, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("read the generated go.mod: %v", err)
	}

	text := string(body)

	if !strings.Contains(text, "module "+driverModule) {
		t.Errorf("the generated go.mod does not declare the module that was asked for:\n%s", text)
	}

	if !strings.Contains(text, "replace "+sdkModule+" =>") {
		t.Errorf("the generated go.mod has no replace for %s, so it builds against nothing:\n%s",
			sdkModule, text)
	}
}

// TestAFileIsNeverOverwritten keeps a second run from replacing a driver
// somebody has written. Half a driver and half a skeleton compiles, which is
// what makes silent overwriting worse than refusing.
func TestAFileIsNeverOverwritten(t *testing.T) {
	dir := scaffold(t)

	plan, err := planOf(options{name: driverName, module: driverModule, dir: dir, sdk: sdkDir(t)})
	if err != nil {
		t.Fatalf("plan the second run: %v", err)
	}

	if _, err = write(plan); !errors.Is(err, ErrOccupied) {
		t.Errorf("a second scaffold into the same directory reported %v, want ErrOccupied", err)
	}
}

// TestANameThatCannotBeConfiguredIsRefused holds the grammar at the flag rather
// than at registration, where the failure is a panic inside somebody else's
// binary.
func TestANameThatCannotBeConfiguredIsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		given string
	}{
		{name: "empty", given: ""},
		{name: "uppercase", given: "Acme"},
		{name: "underscore", given: "acme_one"},
		{name: "leading hyphen", given: "-acme"},
		{name: "a label that is not a package name", given: "1acme"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			_, err := planOf(options{name: c.given, module: "example.com/d", sdk: sdkDir(t)})
			if err == nil {
				t.Errorf("-name %q was accepted, and no operator can write it in configuration", c.given)
			}
		})
	}
}

// TestAModulePathIsRequired refuses the flag combination that would otherwise
// produce a go.mod naming nothing.
func TestAModulePathIsRequired(t *testing.T) {
	if _, err := planOf(options{name: "acme", sdk: sdkDir(t)}); !errors.Is(err, errUsage) {
		t.Error("a scaffold with no -module was accepted")
	}
}

// TestAModulePathNoHostCanNamespaceIsRefused holds the module grammar at the
// flag. A path a host cannot derive a namespace from compiles here and passes
// the conformance suite here, because a driver is the main module of its own
// tests; it panics in the operator's binary at init. Uppercase organisation
// names are common, and so are the gopkg.in paths.
func TestAModulePathNoHostCanNamespaceIsRefused(t *testing.T) {
	t.Parallel()

	for _, module := range []string{
		"github.com/Acme/prusa-driver-acme",
		"github.com/Masterminds/prusa-driver-acme",
		"example.com/prusa_driver_acme",
		"gopkg.in/yaml.v3",
		"example.com/a b",
		"example.com/",
		"core",

		// ADR-20260826-6x5: the boundary label, a path with no host, and a path that
		// renders past the namespace limit. Each registers nowhere, so the
		// flag is where an author should meet it.
		"example.com/aux",
		"aux.example.com/prusa-driver-acme",
		"prusadriveracme",
		"example.com/prusa-driver-acme/" + strings.Repeat("a", 63) + "/" + strings.Repeat("b", 30),
	} {
		t.Run(module, func(t *testing.T) {
			t.Parallel()

			_, err := planOf(options{name: "acme", module: module, sdk: sdkDir(t)})
			if !errors.Is(err, errUsage) {
				t.Fatalf("-module %q was accepted, and no host can register it: %v", module, err)
			}

			if !errors.Is(err, extension.ErrNoNamespace) {
				t.Errorf("-module %q was refused without saying the host cannot namespace it: %v", module, err)
			}
		})
	}
}

// TestTheScaffoldedTestNamesTheNameAHostGives closes the gap between what an
// author reads out of their own test run and what an operator writes. A driver
// is the main module of its own test binary, so it registers under "core"
// there, and under its module path's namespace everywhere else.
func TestTheScaffoldedTestNamesTheNameAHostGives(t *testing.T) {
	t.Parallel()

	dir := scaffold(t)

	body, err := os.ReadFile(filepath.Join(dir, driverName+"_test.go"))
	if err != nil {
		t.Fatalf("read the generated test: %v", err)
	}

	namespace, err := extension.NamespaceOf(driverModule)
	if err != nil {
		t.Fatalf("NamespaceOf(%s): %v", driverModule, err)
	}

	want := `const Qualified = "` + namespace.String() + "/" + driverName + `"`
	if !strings.Contains(string(body), want) {
		t.Errorf("the generated test does not carry %s, so the name it reports is not the one an operator writes:\n%s",
			want, body)
	}
}

// TestAnSdkPathThatIsNotTheSdkIsRefused fails at the flag rather than at the
// author's first build, where the message is about the module graph.
func TestAnSdkPathThatIsNotTheSdkIsRefused(t *testing.T) {
	if _, err := planOf(options{name: "acme", module: "example.com/d", sdk: t.TempDir()}); !errors.Is(err, ErrNoSDK) {
		t.Error("an -sdk pointing at a directory with no go.mod was accepted")
	}
}

// scaffold writes a driver into a fresh directory and returns it.
func scaffold(t *testing.T) string {
	t.Helper()

	return scaffoldWith(t, options{name: driverName, module: driverModule})
}

// scaffoldWith is scaffold for a shape other than the plain dialect. It fills
// in the directory and the sdk, which every case wants the same, and leaves the
// caller the flags that are the point of their case.
func scaffoldWith(t *testing.T, opts options) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "adapter")

	opts.dir = dir
	opts.sdk = sdkDir(t)

	plan, err := planOf(opts)
	if err != nil {
		t.Fatalf("plan the scaffold: %v", err)
	}

	if _, err = write(plan); err != nil {
		t.Fatalf("write the scaffold: %v", err)
	}

	return dir
}

// TestEveryScaffoldedShapePassesItsOwnSuite is the claim of this command over
// the three things it writes: a plain dialect, a dialect that declares a device
// kind of its own, and a storage provider. Each carries the published
// conformance suite for what it is, and each has to pass it with nobody
// touching the output.
//
// The storage provider is the case worth having beside the dialect. A contract
// with one implementation is a description of that implementation, so the
// scaffold that hands an author a second one is doing more than saving typing:
// it is the difference between a storage contract that is replaceable and one
// that is merely documented as replaceable.
//
// It runs the go tool for each shape, which costs a few seconds each, and it
// deliberately does not skip under -short for the reason the case above does
// not.
func TestEveryScaffoldedShapePassesItsOwnSuite(t *testing.T) {
	t.Parallel()

	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool on PATH: %v", err)
	}

	shapes := map[string]options{
		"a dialect that declares a device kind": {
			name: "shelfy", module: "example.com/prusa-driver-shelfy", kind: "rack",
		},
		"a storage provider": {
			name: "blobs", module: "example.com/prusa-store-blobs", storage: true,
		},
	}

	for name, opts := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := scaffoldWith(t, opts)

			cmd := exec.CommandContext(t.Context(), goTool, "test", "-count=1", "./...")
			cmd.Dir = dir

			out, cmdErr := cmd.CombinedOutput()
			if cmdErr != nil {
				t.Fatalf("go test in the scaffolded module: %v\n%s", cmdErr, out)
			}

			t.Logf("go test ./...\n%s", out)
		})
	}
}

// TestEveryScaffoldCarriesABuildConfiguration is the other half of "without
// edits". A module that compiles and cannot be composed is half an answer: the
// build configuration is the whole admission mechanism, and it is the one part
// of this an author has not seen before.
//
// The paths in it are absolute, because the scaffold does not know where its
// output will end up relative to a checkout, so the assertion below also holds
// compose-build to taking an absolute path as it stands.
func TestEveryScaffoldCarriesABuildConfiguration(t *testing.T) {
	t.Parallel()

	shapes := map[string]options{
		"a dialect":          {name: driverName, module: driverModule},
		"a storage provider": {name: "blobs", module: "example.com/prusa-store-blobs", storage: true},
	}

	core, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve the checkout: %v", err)
	}

	for name, opts := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			body, readErr := os.ReadFile(filepath.Join(scaffoldWith(t, opts), "build.yaml"))
			if readErr != nil {
				t.Fatalf("read the generated build configuration: %v", readErr)
			}

			text := string(body)

			for _, want := range []string{
				"path: " + filepath.ToSlash(core),
				"module: " + opts.module,
				"path: .",
			} {
				if !strings.Contains(text, want) {
					t.Errorf("the generated build configuration carries no %q:\n%s", want, text)
				}
			}
		})
	}
}

// sdkDir is the sdk of this checkout, which is what a test scaffolds against.
func sdkDir(t *testing.T) string {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join(repoRoot, "sdk"))
	if err != nil {
		t.Fatalf("resolve the sdk directory: %v", err)
	}

	return dir
}

// TestAScaffoldedDriverStartsAtTheStandardEveryDriverIsHeldTo names the files
// an author would otherwise write last or not at all: the codes the driver
// fails with, a README an operator can follow, and a changelog. An author
// copies what the skeleton gives them, so a skeleton that gives them none of
// this produces a driver that has none of it.
func TestAScaffoldedDriverStartsAtTheStandardEveryDriverIsHeldTo(t *testing.T) {
	t.Parallel()

	dir := scaffoldWith(t, options{name: driverName, module: driverModule})

	for _, name := range []string{"faults.go", "README.md", "CHANGELOG.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("the scaffold wrote no %s: %v", name, err)
		}
	}

	// Every failure a caller can see is a registered code in the driver's own
	// domain, never a sentence on its own.
	faults, err := os.ReadFile(filepath.Join(dir, "faults.go"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"fault.NewDomain", "fault.Register", "fault.Document"} {
		if !strings.Contains(string(faults), want) {
			t.Fatalf("faults.go does not call %s, so the codes are not registered", want)
		}
	}

	source, err := os.ReadFile(filepath.Join(dir, driverName+".go"))
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(source), "errors.New(") {
		t.Fatal("the dialect declares a bare error sentinel. Register a code in faults.go instead")
	}

	if strings.Contains(string(source), "os.Getenv") {
		t.Fatal("the dialect reads an environment variable. Declare the setting in Doc instead")
	}

	// The README is one an operator can follow, under the titles they look for.
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	for _, title := range []string{
		"## Installation", "## Configuration", "## Permissions", "## Troubleshooting",
	} {
		if !strings.Contains(string(readme), title) {
			t.Fatalf("the README has no %q section", title)
		}
	}
}
