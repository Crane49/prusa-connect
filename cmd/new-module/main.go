// Command new-module writes a new module from a skeleton that builds. -kind
// ui|process|both picks what it writes: a browser bundle the host serves and
// never runs, a Go process serving one route, or both.
//
//	make new-module NAME=spools REPO=github.com/acme/spools
//	go run ./cmd/new-module -start demo -dir ../nozzle
//
// -start writes nothing. It starts a module already written. On a terminal
// the command asks for what the flags leave out. Without a terminal it asks
// nothing and the required flags must be given.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/scaffold"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// Exit codes. A usage error has its own code so a script can tell a typo
// from a failure to write.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// errUsage marks a bad command line.
var errUsage = errors.New("usage")

// firstVersion is the version in a new module's manifest. It is not 0.0.0
// because a module is installed by version.
const firstVersion = "0.1.0"

// The resource kinds of a new module. A module with a process is initKind. A
// bundle-only module is assetKind, which the host serves and never runs.
const (
	initKind  = string(ref.KindSink)
	assetKind = string(ref.KindAsset)
)

// slotNames is every value -contributes accepts: the names in the slot table.
func slotNames() []string {
	names := make([]string, 0, len(manifest.Slots()))
	for _, slot := range manifest.Slots() {
		names = append(names, string(slot.Name))
	}

	return names
}

func main() { os.Exit(report(start(context.Background(), os.Stdout, os.Stdin, os.Args[1:]))) }

// environment is what run reads from the machine. A test passes its own.
type environment struct {
	// workDir is where the command was run.
	workDir string
	// version runs a tool and returns the version it reports.
	version func(ctx context.Context, tool string) (string, error)
	// gh runs the GitHub command line tool in dir and returns its stdout.
	// It is nil on a machine without gh.
	gh func(ctx context.Context, dir string, args ...string) (string, error)
	// install runs the module's install.sh in dir for a target and returns
	// its output.
	install func(ctx context.Context, dir string, into target) ([]byte, error)
	// launch starts a module and returns when it stops.
	launch func(ctx context.Context, out io.Writer, color bool, l launch) error
}

// start builds the real environment. The asker is nil when stdin is not a
// terminal. Color is off when stdout is not a terminal or NO_COLOR is set.
func start(ctx context.Context, out io.Writer, in *os.File, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("read the working directory: %w", err)
	}

	var a *asker
	if isTerminal(in) {
		stdout, isFile := out.(*os.File)
		_, noColor := os.LookupEnv("NO_COLOR")
		a = &asker{in: bufio.NewReader(in), out: out, color: isFile && isTerminal(stdout) && !noColor}
	}

	return run(ctx, out, a, environment{
		workDir: workDir, version: toolVersion, gh: runGH, install: runInstall, launch: runLaunch,
	}, args)
}

// requiredFlags are the flags a run without a terminal must give.
//
//nolint:gochecknoglobals // a closed table, read and never written.
var requiredFlags = []string{"name", "kind", "repo"}

func missingFlags(given map[string]bool) []string {
	var missing []string

	for _, name := range requiredFlags {
		if !given[name] {
			missing = append(missing, "-"+name)
		}
	}

	return missing
}

func run(ctx context.Context, out io.Writer, a *asker, env environment, args []string) error {
	request, given, err := parse(out, env, args)
	if err != nil {
		return err
	}

	if request.start != "" {
		return startLater(ctx, out, a, env, request)
	}

	request, err = withGitHub(ctx, a != nil, env, given, request)
	if err != nil {
		return err
	}

	var plan scaffold.Plan

	if a == nil {
		plan, err = fromFlags(ctx, env, given, request)
	} else {
		request, plan, err = settle(ctx, a, env, given, request)
		if errors.Is(err, errCancelled) {
			fmt.Fprintln(out, "Cancelled. Nothing was written.")

			return nil
		}
	}

	if err != nil {
		return err
	}

	return create(ctx, out, a, env, request, plan)
}

// fromFlags builds the plan from the flags alone, for a run with no terminal.
func fromFlags(ctx context.Context, env environment, given map[string]bool, request answers) (scaffold.Plan, error) {
	if missing := missingFlags(given); len(missing) > 0 {
		return scaffold.Plan{}, fmt.Errorf("%w: there is no terminal to ask on, so it needs %s",
			errUsage, strings.Join(missing, " "))
	}

	if err := checkTools(ctx, request.shape, env); err != nil {
		return scaffold.Plan{}, err
	}

	return planFor(request)
}

// parse reads the flags into answers and returns the set of flags that were
// given. Only a flag that was not given is asked about later.
func parse(out io.Writer, env environment, args []string) (answers, map[string]bool, error) {
	flags := flag.NewFlagSet("new-module", flag.ContinueOnError)
	flags.SetOutput(out)

	request := answers{workDir: env.workDir}
	list, kind := "", ""

	flags.StringVar(&request.name, "name", "",
		"the module's short name: the instance, the binary and the page's route")
	flags.StringVar(&kind, "kind", "", "which halves the module has: "+shapeNames())
	flags.StringVar(&request.repo, "repo", "",
		"the repository address, like github.com/you/spools. The module's namespace is derived from it")
	flags.StringVar(&request.dir, "dir", "",
		"where the module's folder goes: ../<name> beside a checkout, ./<name> anywhere else")
	flags.StringVar(&request.title, "title", "",
		"what a person reads: the sidebar entry and the page heading")
	flags.StringVar(&request.checkout, "checkout", "",
		"the checkout this module builds against, found above the working directory when empty")
	flags.StringVar(&list, "contributes", string(manifest.SlotPage),
		"what the bundle registers, comma separated: "+strings.Join(slotNames(), ", "))
	flags.IntVar(&request.memoryMiB, "memory-mib", 0,
		"the memory in MiB the module's process recommends in module.yaml, from 16 to 8192. 0 recommends none")
	flags.BoolVar(&request.git, "git", true, "make the module's folder a git repository with a first commit")
	flags.StringVar(&request.remote, "remote", "", "an empty git remote to add as origin and push the first commit to")
	flags.BoolVar(&request.github, "github", false,
		"make a new private repository on GitHub with gh, and push the first commit to it")
	flags.StringVar(&request.start, "start", "",
		"start the module already written at -dir instead of writing one: demo, with simulated printers, or app")

	if err := flags.Parse(args); err != nil {
		return answers{}, nil, fmt.Errorf("%w: %w", errUsage, err)
	}

	given := make(map[string]bool, flags.NFlag())
	flags.Visit(func(written *flag.Flag) { given[written.Name] = true })

	if m := request.memoryMiB; m != 0 && (m < manifest.MinMemoryMiB || m > manifest.MaxMemoryMiB) {
		return answers{}, nil, fmt.Errorf("%w: -memory-mib is from %d to %d, or 0 for no recommendation, not %d",
			errUsage, manifest.MinMemoryMiB, manifest.MaxMemoryMiB, m)
	}

	if request.start != "" && request.start != string(targetDemo) && request.start != string(targetApp) {
		return answers{}, nil, fmt.Errorf("%w: -start is demo or app, not %q", errUsage, request.start)
	}

	if request.start != "" && !given["dir"] {
		return answers{}, nil, fmt.Errorf("%w: -start starts the module in -dir, so it needs -dir", errUsage)
	}

	request.shape = shape(kind)
	if kind != "" && !slices.Contains(shapes, request.shape) {
		return answers{}, nil, fmt.Errorf("%w: -kind is %s, not %q", errUsage, shapeNames(), kind)
	}

	// planFor derives a repo from the name when none is set. An explicit
	// empty -repo must not get that default.
	if given["repo"] && strings.TrimSpace(request.repo) == "" {
		return answers{}, nil, fmt.Errorf("%w: -repo is the repository address, like github.com/you/spools", errUsage)
	}

	if request.github && request.remote != "" {
		return answers{}, nil, fmt.Errorf("%w: -github makes the remote, so it takes no -remote", errUsage)
	}

	if request.shape == shapeProcess && given["contributes"] {
		return answers{}, nil, fmt.Errorf(
			"%w: a process-only module draws nothing, so it takes no -contributes", errUsage)
	}

	contributions, err := contributionsOf(list)
	if err != nil {
		return answers{}, nil, err
	}

	request.contributions = contributions

	return request, given, nil
}

// report prints the error and returns the exit code for it.
func report(err error) int {
	if err == nil {
		return exitOK
	}

	fmt.Fprintln(os.Stderr, "new-module: "+err.Error())

	if errors.Is(err, errUsage) {
		return exitUsage
	}

	return exitFailure
}

// answers is the flags plus what the questions added.
type answers struct {
	name string
	dir  string
	// repo is the repository address the namespace is derived from, like
	// github.com/acme/spools.
	repo     string
	title    string
	checkout string
	// workDir is where the command was run. Relative paths resolve against
	// it and printed paths are relative to it.
	workDir string
	// shape says whether the module has a UI, a process or both.
	shape shape
	// contributions is what the bundle registers, keyed by slot name. An
	// empty field in a value takes the module's name or title.
	contributions map[manifest.SlotName]scaffold.Contribution
	// memoryMiB is the memory module.yaml recommends. 0 recommends none.
	memoryMiB int
	// git makes the folder a repository with one commit. remote is where the
	// commit is pushed. github pushes to a new private GitHub repository.
	git    bool
	remote string
	github bool
	// login is the GitHub account gh is logged in to, or empty.
	login string
	// start is -start: demo or app, or empty for a run that writes a module.
	start string
	// pushed is the repository the first commit was pushed to, or empty.
	pushed string
}

// contributionsOf reads -contributes, a comma-separated list of slot names.
// An unknown name is a usage error.
func contributionsOf(list string) (map[manifest.SlotName]scaffold.Contribution, error) {
	held := make(map[manifest.SlotName]scaffold.Contribution, len(manifest.Slots()))

	for named := range strings.SplitSeq(list, ",") {
		named = strings.TrimSpace(named)
		if named == "" {
			continue
		}

		slot := manifest.SlotName(named)
		if _, known := manifest.SlotNamed(slot); !known {
			return nil, fmt.Errorf("%w: %q is not a contribution. -contributes takes %s",
				errUsage, named, strings.Join(slotNames(), ", "))
		}

		held[slot] = scaffold.Contribution{}
	}

	return held, nil
}
