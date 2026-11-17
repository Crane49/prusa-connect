// Command compose-build builds a server binary from a build configuration
// instead of from this checkout.
//
// It reads one file that names the core and the adapter modules. It generates
// a Go module in a scratch directory: a go.mod that requires them and a
// main.go that blank-imports the adapters and calls compose.Main. It then runs
// `go mod tidy`, detects the build tags as `make build` does, and builds. The
// core checkout is never written.
//
// The scratch module is removed after the build unless --keep is set.
//
// Run via: make compose-build BUILD_FILE=build.yaml
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// coreModulePath is the core module when the configuration names none.
const coreModulePath = "github.com/prusa3d/prusa-connect"

// sdkSuffix is the nested sdk module. The core's go.mod replaces it with a
// relative path.
const sdkSuffix = "/sdk"

func main() {
	// The context cancels the go commands on Ctrl-C.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// stop is called here and not deferred, because os.Exit skips deferred
	// calls.
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()

	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr *os.File) int {
	flags := flag.NewFlagSet("compose-build", flag.ContinueOnError)
	flags.SetOutput(stderr)

	configPath := flags.String("config", "build.yaml", "the build configuration naming the core and the adapters")
	output := flags.String("output", "", "where to write the binary, overriding the configuration's output key")
	workDir := flags.String("work", "", "the scratch directory to generate into, defaulting to a temporary one")
	keep := flags.Bool("keep", false, "leave the generated module in place after the build")
	printOnly := flags.Bool("print", false, "write the generated module and stop, building nothing")

	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	build, err := load(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, "compose-build: "+err.Error())

		return exitUsage
	}

	if *output != "" {
		build.Output = *output
	}

	if err = compose(
		ctx,
		build,
		options{work: *workDir, keep: *keep, printOnly: *printOnly},
		stdout,
		stderr,
	); err != nil {
		fmt.Fprintln(stderr, "compose-build: "+err.Error())

		return exitFailure
	}

	return exitOK
}

// options are the flags that say where the work happens.
type options struct {
	work      string
	keep      bool
	printOnly bool
}

// Build is the configuration an operator writes. Every key has a default. The
// shortest useful file names only a core.
type Build struct {
	// Core is where the core comes from, by version or by directory.
	Core Module `yaml:"core"`
	// Adapters are the modules the generated main blank-imports.
	Adapters []Module `yaml:"adapters"`
	// Config is the server configuration the tag detector reads.
	Config string `yaml:"config"`
	// Variant is full, standard or minimal.
	Variant string `yaml:"variant"`
	// ExtraTags are build tags added to the detected set.
	ExtraTags string `yaml:"extra_tags"`
	// Output is where the binary is written, relative to this file's
	// directory.
	Output string `yaml:"output"`
}

// Module names one module and where it comes from. Set Version for a pinned
// release, or Path to replace the module with a directory.
type Module struct {
	// Module is the module path. The core's may be left empty.
	Module string `yaml:"module"`
	// Version is a semantic version or a pseudo-version, for version mode.
	Version string `yaml:"version"`
	// Path is a directory holding the module, for path mode.
	Path string `yaml:"path"`
}

// errConfig reports a build configuration this tool cannot act on.
var errConfig = errors.New("build configuration")

// load reads and checks the build configuration.
func load(path string) (Build, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Build{}, fmt.Errorf("read %s: %w", path, err)
	}

	var build Build

	decoder := yaml.NewDecoder(strings.NewReader(string(content)))
	decoder.KnownFields(true)

	if err = decoder.Decode(&build); err != nil {
		return Build{}, fmt.Errorf("%w %s: %w", errConfig, path, err)
	}

	if build.Core.Module == "" {
		build.Core.Module = coreModulePath
	}

	if build.Config == "" {
		build.Config = "config/config.yaml"
	}

	if build.Variant == "" {
		build.Variant = "full"
	}

	if build.Output == "" {
		build.Output = filepath.Join("bin", "server")
	}

	// Every path in the file is relative to the file's directory. The
	// --output flag is relative to the working directory.
	base := filepath.Dir(path)

	if err = absolve(base, &build.Core); err != nil {
		return Build{}, fmt.Errorf("%w %s: %w", errConfig, path, err)
	}

	for i := range build.Adapters {
		if err = absolve(base, &build.Adapters[i]); err != nil {
			return Build{}, fmt.Errorf("%w %s: %w", errConfig, path, err)
		}
	}

	build.Config = against(base, build.Config)
	build.Output = against(base, build.Output)

	for _, module := range append([]Module{build.Core}, build.Adapters...) {
		if err = checkModule(module); err != nil {
			return Build{}, fmt.Errorf("%w %s: %w", errConfig, path, err)
		}
	}

	return build, nil
}

// absolve makes one entry's path absolute. A relative path is resolved
// against the configuration's directory.
func absolve(base string, module *Module) error {
	if module.Path == "" {
		return nil
	}

	absolute, err := filepath.Abs(against(base, module.Path))
	if err != nil {
		return fmt.Errorf("resolve the path of %s: %w", module.Module, err)
	}

	module.Path = absolute

	return nil
}

// against resolves a relative path against base.
func against(base, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(base, path)
}

// checkModule refuses an entry with no module path, with neither a version
// nor a path, or with both.
func checkModule(module Module) error {
	if module.Module == "" {
		return errors.New("a module entry has no module path")
	}

	switch {
	case module.Version == "" && module.Path == "":
		return fmt.Errorf("%s names neither a version nor a path", module.Module)
	case module.Version != "" && module.Path != "":
		return fmt.Errorf("%s names both a version and a path, and only one of them can be built", module.Module)
	}

	return nil
}

// compose generates the module, resolves it and builds it.
func compose(ctx context.Context, build Build, opts options, stdout, stderr *os.File) error {
	work, cleanup, err := scratch(opts)
	if err != nil {
		return err
	}

	defer cleanup()

	output, err := filepath.Abs(build.Output)
	if err != nil {
		return fmt.Errorf("resolve the output path %s: %w", build.Output, err)
	}

	config, err := filepath.Abs(build.Config)
	if err != nil {
		return fmt.Errorf("resolve the configuration path %s: %w", build.Config, err)
	}

	if err = write(work, build); err != nil {
		return err
	}

	fmt.Fprintln(stdout, "Composing in "+work)

	coreDir, err := resolveCore(ctx, work, build.Core, stderr)
	if err != nil {
		return err
	}

	if err = carryReplaces(work, coreDir, build.Core); err != nil {
		return err
	}

	if opts.printOnly {
		fmt.Fprintln(stdout, "Generated module left in "+work)

		return nil
	}

	if err = goCommand(ctx, work, stderr, "mod", "tidy").Run(); err != nil {
		return fmt.Errorf("resolve the composition module in %s: %w", work, err)
	}

	tags, err := detectTags(ctx, work, coreDir, build, config)
	if err != nil {
		return err
	}

	fmt.Fprintln(stdout, "Building with tags: "+tags)

	compile := goCommand(ctx, work, stderr, "build", "-tags", tags, "-o", output, ".")
	if err = compile.Run(); err != nil {
		return fmt.Errorf("build the composed binary: %w", err)
	}

	fmt.Fprintln(stdout, "Wrote "+output)

	return nil
}

// scratch returns the directory to generate into and its cleanup. A named
// directory is kept. A temporary one is removed unless --keep or --print is
// set.
func scratch(opts options) (string, func(), error) {
	if opts.work != "" {
		if err := os.MkdirAll(opts.work, 0o750); err != nil {
			return "", nil, fmt.Errorf("create the scratch directory %s: %w", opts.work, err)
		}

		absolute, err := filepath.Abs(opts.work)
		if err != nil {
			return "", nil, fmt.Errorf("resolve the scratch directory %s: %w", opts.work, err)
		}

		return absolute, func() {}, nil
	}

	work, err := os.MkdirTemp("", "compose-build-")
	if err != nil {
		return "", nil, fmt.Errorf("create a scratch directory: %w", err)
	}

	if opts.keep || opts.printOnly {
		return work, func() {}, nil
	}

	return work, func() { _ = os.RemoveAll(work) }, nil
}

// compositionModule is the module path of the generated module. Nothing
// fetches it.
const compositionModule = "composed.invalid/prusa-connect"

// write generates go.mod and main.go.
func write(work string, build Build) error {
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(gomod(build)), 0o600); err != nil {
		return fmt.Errorf("write the generated go.mod: %w", err)
	}

	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte(mainGo(build)), 0o600); err != nil {
		return fmt.Errorf("write the generated main.go: %w", err)
	}

	return nil
}

// gomod is the generated go.mod, before carryReplaces. An entry with a path
// becomes a require at v0.0.0 and a replace.
func gomod(build Build) string {
	var out strings.Builder

	out.WriteString("// Generated by compose-build. Edit build.yaml, not this file.\nmodule " +
		compositionModule + "\n\ngo 1.26\n\nrequire (\n")

	for _, module := range append([]Module{build.Core}, build.Adapters...) {
		out.WriteString("\t" + module.Module + " " + requiredVersion(module) + "\n")
	}

	out.WriteString(")\n")

	for _, module := range append([]Module{build.Core}, build.Adapters...) {
		if module.Path != "" {
			out.WriteString("\nreplace " + module.Module + " => " + toSlash(module.Path) + "\n")
		}
	}

	return out.String()
}

// requiredVersion is the version on the require line. A replaced module still
// needs one, so it gets v0.0.0.
func requiredVersion(module Module) string {
	if module.Version != "" {
		return module.Version
	}

	return "v0.0.0"
}

// mainGo is the generated main: one blank import per adapter, and the call.
func mainGo(build Build) string {
	var out strings.Builder

	out.WriteString(
		"// Generated by compose-build. Edit build.yaml, not this file.\npackage main\n\nimport (\n\t\"os\"\n\n",
	)
	out.WriteString("\t\"" + build.Core.Module + "/compose\"\n")

	if len(build.Adapters) > 0 {
		out.WriteString("\n")

		for _, adapter := range build.Adapters {
			out.WriteString("\t_ \"" + adapter.Module + "\"\n")
		}
	}

	out.WriteString(")\n\nfunc main() { os.Exit(compose.Main()) }\n")

	return out.String()
}

// resolveCore returns the directory of the core's source: the configured path,
// or the module cache for a version.
func resolveCore(ctx context.Context, work string, core Module, stderr *os.File) (string, error) {
	if core.Path != "" {
		absolute, err := filepath.Abs(core.Path)
		if err != nil {
			return "", fmt.Errorf("resolve the core path %s: %w", core.Path, err)
		}

		return absolute, nil
	}

	if err := goCommand(ctx, work, stderr, "mod", "download", core.Module).Run(); err != nil {
		return "", fmt.Errorf("download %s %s: %w", core.Module, core.Version, err)
	}

	found := goCommand(ctx, work, stderr, "list", "-m", "-f", "{{.Dir}}", core.Module)

	out, err := found.Output()
	if err != nil {
		return "", fmt.Errorf("locate %s %s: %w", core.Module, core.Version, err)
	}

	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("%s %s resolved to no directory", core.Module, core.Version)
	}

	return dir, nil
}

// carryReplaces copies every replace directive from the core's go.mod into
// the generated module. Go applies replace directives only in the main
// module, so without the copy `go mod tidy` fails. A relative target is
// re-pointed at the core's directory. A replace already in the generated
// go.mod wins.
func carryReplaces(work, coreDir string, core Module) error {
	source := filepath.Join(coreDir, "go.mod")

	content, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read the resolved core's go.mod at %s: %w", source, err)
	}

	from, err := modfile.Parse(source, content, nil)
	if err != nil {
		return fmt.Errorf("parse %s: %w", source, err)
	}

	target := filepath.Join(work, "go.mod")

	generated, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read the generated go.mod: %w", err)
	}

	into, err := modfile.Parse(target, generated, nil)
	if err != nil {
		return fmt.Errorf("parse the generated go.mod: %w", err)
	}

	taken := map[string]bool{}
	for _, existing := range into.Replace {
		taken[existing.Old.Path] = true
	}

	for _, one := range from.Replace {
		if taken[one.Old.Path] {
			continue
		}

		newPath, newVersion := repoint(one.New.Path, one.New.Version, coreDir)

		if err = into.AddReplace(one.Old.Path, one.Old.Version, newPath, newVersion); err != nil {
			return fmt.Errorf("carry the replace of %s: %w", one.Old.Path, err)
		}
	}

	// Make sure the sdk is replaced with the core's own copy, for every
	// module in the build.
	if !taken[core.Module+sdkSuffix] && !replaces(into, core.Module+sdkSuffix) {
		if err = into.AddReplace(core.Module+sdkSuffix, "", toSlash(filepath.Join(coreDir, "sdk")), ""); err != nil {
			return fmt.Errorf("carry the replace of the sdk: %w", err)
		}
	}

	into.Cleanup()

	formatted, err := into.Format()
	if err != nil {
		return fmt.Errorf("format the generated go.mod: %w", err)
	}

	//nolint:gosec // G703: target is built from this repository's own layout, not from input.
	if err = os.WriteFile(target, formatted, 0o600); err != nil {
		return fmt.Errorf("write the generated go.mod: %w", err)
	}

	return nil
}

// replaces reports whether the file already replaces one module path.
func replaces(file *modfile.File, path string) bool {
	for _, one := range file.Replace {
		if one.Old.Path == path {
			return true
		}
	}

	return false
}

// repoint resolves a relative replace target against the core's directory. A
// target with a version is returned unchanged.
func repoint(path, version, coreDir string) (string, string) {
	if version != "" || !strings.HasPrefix(path, ".") {
		return path, version
	}

	return toSlash(filepath.Join(coreDir, filepath.FromSlash(path))), ""
}

// detectTags runs the core's detect-build-tags, the detector `make build`
// uses. --adapters is passed because the detector's default path is relative
// to a core checkout, and this runs in the scratch directory.
func detectTags(ctx context.Context, work, coreDir string, build Build, config string) (string, error) {
	detector := build.Core.Module + "/cmd/detect-build-tags"

	found := goCommand(ctx, work, os.Stderr, "run", detector,
		"--config", config,
		"--adapters", filepath.Join(coreDir, "adapters", "tags.json"),
		"--variant", build.Variant,
		"--extra", build.ExtraTags)

	out, err := found.Output()
	if err != nil {
		return "", fmt.Errorf("detect the build tags: %w", err)
	}

	detected := strings.Fields(string(out))

	return strings.Join(append([]string{build.Variant}, detected...), " "), nil
}

// goCommand builds one go command that runs in dir and writes to stderr.
func goCommand(ctx context.Context, dir string, stderr *os.File, args ...string) *exec.Cmd {
	child := exec.CommandContext(ctx, "go", args...)
	child.Dir = dir
	child.Stderr = stderr

	return child
}

// toSlash returns a path with forward slashes, as go.mod needs.
func toSlash(path string) string { return filepath.ToSlash(path) }
