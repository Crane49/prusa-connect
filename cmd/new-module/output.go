// What the command prints, and the steps after the files are written.

package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/scaffold"
)

func paint(color bool, style, text string) string {
	if !color {
		return text
	}

	return style + text + styleReset
}

// shown returns path relative to workDir, starting with ./ when it is below
// workDir.
func shown(workDir, path string) string {
	relative, err := filepath.Rel(workDir, path)
	if err != nil {
		return path
	}

	if relative == "." || strings.HasPrefix(relative, "..") {
		return relative
	}

	return "." + string(filepath.Separator) + relative
}

// create writes the plan, makes the git repository when asked, and hands
// over to startDevLoop. It numbers every step it prints.
func create(
	ctx context.Context, out io.Writer, a *asker, env environment, request answers, plan scaffold.Plan,
) error {
	written, err := scaffold.Write(plan)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	steps := &stepper{out: out, color: a != nil && a.color}
	steps.next("wrote %d files in %s, which is %s", len(written), shown(request.workDir, plan.Dir), plan.Dir)

	for _, path := range written {
		fmt.Fprintf(out, "     %s\n", shown(request.workDir, path))
	}

	if request.git {
		if err = startRepository(ctx, plan.Dir, plan.Name); err != nil {
			return fmt.Errorf("%s is written, and git failed: %w", shown(request.workDir, plan.Dir), err)
		}

		steps.next("made it a git repository with a first commit")
		request.pushed = publish(ctx, env, steps, request, plan)
	}

	return startDevLoop(ctx, a, out, env, request, plan, steps)
}

// stepper numbers the steps the command prints, from 1.
type stepper struct {
	out   io.Writer
	done  int
	color bool
}

// next prints the next step with its number.
//
//nolint:goprintffuncname // next is what advances the stepper; a printf-style name would hide that.
func (s *stepper) next(format string, args ...any) {
	s.done++

	fmt.Fprintf(s.out, "%d. %s\n", s.done, fmt.Sprintf(format, args...))
}

// warn prints the next step as a warning, with the command to retry it.
func (s *stepper) warn(what, retry string) {
	s.next("warning: %s", what)
	fmt.Fprintf(s.out, "     To try again: %s\n", retry)
}

// startDevLoop offers the first install and the first start, and prints what
// was made. Without a terminal or a checkout it installs and starts nothing.
func startDevLoop(
	ctx context.Context, a *asker, out io.Writer, env environment, request answers, plan scaffold.Plan,
	steps *stepper,
) error {
	installed := false

	if plan.Checkout != "" && a != nil && a.yes(questions["install"], true) && !a.ended {
		if err := installInto(ctx, env, steps, plan.Dir, targetDemo, request.workDir); err != nil {
			return err
		}

		installed = true
	}

	ready(out, a, request, plan)

	if plan.Checkout == "" {
		fmt.Fprintf(out, "\nWhat this module builds against comes from %s, and it is not published yet.\n"+
			"README.md in it has the commands for when it is.\n", registriesOf(plan))

		return nil
	}

	into := target("")
	if installed {
		into = askStart(a)
	}

	if into == "" {
		fmt.Fprintf(out, "\n  start it later  %s\n", laterLine(request, plan.Dir))

		return nil
	}

	l, err := launchFor(ctx, into, plan.Name, plan.Dir, filepath.FromSlash(plan.Checkout), request.workDir)
	if err != nil {
		return err
	}

	// The install above went into the demo. The app needs its own.
	if into == targetApp {
		if err = ensureInstalled(ctx, env, steps, l); err != nil {
			return err
		}
	}

	fmt.Fprintln(out)

	return env.launch(ctx, out, steps.color, l)
}

// ready prints the folder, the repository the commit was pushed to and, on a
// terminal, the flags that write the same module again.
func ready(out io.Writer, a *asker, request answers, plan scaffold.Plan) {
	color := a != nil && a.color

	fmt.Fprintf(out, "\n%s %s\n", paint(color, styleGreen, "✔"), paint(color, styleBold, plan.Name+" is ready"))
	fmt.Fprintf(out, "  folder  %s\n", shown(request.workDir, plan.Dir))

	if request.pushed != "" {
		fmt.Fprintf(out, "  repo    %s\n", request.pushed)
	}

	if a == nil {
		return
	}

	fmt.Fprintln(out, paint(color, styleDim, "  flags   "+flagLine(request, plan)))

	if changedLabels(plan) {
		fmt.Fprintln(out, paint(color, styleDim,
			"          No flag carries the addresses or ids you changed. This line writes the defaults."))
	}
}

// registriesOf names the registries a module outside a checkout builds
// against: npm for a UI, the Go module proxy for a process.
func registriesOf(plan scaffold.Plan) string {
	switch {
	case plan.UI && !plan.Process:
		return "npm"
	case plan.Process && !plan.UI:
		return "the Go module proxy"
	default:
		return "npm and the Go module proxy"
	}
}

// flagLine is the command line that writes the same module with no questions.
// No flag sets a contribution's id or title, so those get the defaults.
func flagLine(request answers, plan scaffold.Plan) string {
	line := []string{"go", "run", "./cmd/new-module",
		"-name", plan.Name, "-kind", string(request.shape), "-repo", plan.Module}

	if plan.Title != titleOf("", plan.Name) {
		line = append(line, "-title", scaffold.ShellWord(plan.Title))
	}

	if plan.UI {
		line = append(line, "-contributes", strings.Join(slotsOf(plan), ","))
	}

	if plan.Process && plan.MemoryMiB > 0 {
		line = append(line, "-memory-mib", strconv.Itoa(plan.MemoryMiB))
	}

	line = append(line, "-dir", scaffold.ShellWord(shown(request.workDir, plan.Dir)))

	if request.checkout != "" {
		line = append(line, "-checkout", scaffold.ShellWord(request.checkout))
	}

	switch homeOf(request) {
	case homeNone:
		line = append(line, "-git=false")
	case homeGitHub:
		line = append(line, "-github")
	case homeRemote:
		line = append(line, "-remote", scaffold.ShellWord(request.remote))
	}

	return strings.Join(line, " ")
}

func slotsOf(plan scaffold.Plan) []string {
	slots := make([]string, 0, len(plan.Contributions))
	for _, c := range plan.Contributions {
		slots = append(slots, string(c.Slot))
	}

	return slots
}

// addsOf lists each contribution as its slot and id, for the summary.
func addsOf(plan scaffold.Plan) []string {
	adds := make([]string, 0, len(plan.Contributions))

	for _, c := range plan.Contributions {
		adds = append(adds, string(c.Slot)+" "+c.ID)
	}

	return adds
}

// changedLabels reports whether any contribution has an id or a title that
// differs from the module's.
func changedLabels(plan scaffold.Plan) bool {
	for _, c := range plan.Contributions {
		if c.ID != plan.Name || c.Title != plan.Title {
			return true
		}
	}

	return false
}

// summarise prints what is about to be written, one line per answer.
func summarise(out io.Writer, request answers, plan scaffold.Plan) {
	fmt.Fprintf(out, "\n  kind       %s\n", request.shape.title())
	fmt.Fprintf(out, "  name       %s, shown as %s\n", plan.Name, plan.Title)
	fmt.Fprintf(out, "  namespace  %s  (the unique name its addresses start with)\n", plan.Namespace)

	if plan.UI {
		fmt.Fprintf(out, "  adds       %s  (the contributes list in module.yaml)\n", strings.Join(addsOf(plan), ", "))
	}

	fmt.Fprintf(out, "  folder     %s\n", plan.Dir)
	fmt.Fprintf(out, "  files      %d\n", len(plan.Files()))
	fmt.Fprintf(out, "  git        %s\n", gitSummary(request, plan.Name))
}
