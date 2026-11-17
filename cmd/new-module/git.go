// The git repository of a new module and where its first commit is pushed.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/scaffold"
)

// errNoGitHub means -github was given but gh is missing or not logged in.
var errNoGitHub = errors.New(
	"-github needs gh, the GitHub command line tool, logged in to your account. Run gh auth login first")

// errNoAddress means the remote's address was left empty.
var errNoAddress = errors.New("no address")

// errHasCommits means the remote already has commits, so the first push
// would be rejected.
var errHasCommits = errors.New("the repository already has commits")

// The options of the git question, in menu order.
const (
	homeGitHub = iota
	homeRemote
	homeLocal
	homeNone
)

// withGitHub reads the gh login before any question. On a terminal a login
// makes a new GitHub repository the default. -github fails without one.
func withGitHub(
	ctx context.Context, asking bool, env environment, given map[string]bool, request answers,
) (answers, error) {
	if !asking && !request.github {
		return request, nil
	}

	request.login = gitHubLogin(ctx, env)

	switch {
	case request.github && request.login == "":
		return request, errNoGitHub
	case asking && request.login != "" && !given["git"] && !given["remote"] && !given["github"]:
		request.github = true
	}

	return request, nil
}

// gitHubLogin is the account gh is logged in to, or empty. It is lower-cased
// because the namespace is made from it.
func gitHubLogin(ctx context.Context, env environment) string {
	if env.gh == nil {
		return ""
	}

	if _, err := env.gh(ctx, "", "auth", "status"); err != nil {
		return ""
	}

	login, err := env.gh(ctx, "", "api", "user", "--jq", ".login")
	if err != nil {
		return ""
	}

	return strings.ToLower(login)
}

// codeHome asks where the module's code goes. A new GitHub repository is
// offered only when gh is logged in.
func codeHome(ctx context.Context, a *asker, request answers, name string) answers {
	homes := []int{homeRemote, homeLocal, homeNone}
	offers := []string{
		"a repository I already made (paste its address): it must be empty, and the first commit is pushed to it",
		"only here, a local git repository: one first commit, and nothing is pushed",
		"no git: you set it up yourself later",
	}

	if request.login != "" {
		homes = append([]int{homeGitHub}, homes...)
		offers = append([]string{"a new private repository on GitHub, " + request.login + "/" + name +
			": gh makes it and pushes the first commit to it"}, offers...)
	}

	picked := homes[a.choose(questions["git"], offers, slices.Index(homes, homeOf(request))+1)-1]
	request.git, request.github, request.remote = picked != homeNone, picked == homeGitHub, ""

	if picked == homeRemote {
		request.remote = a.emptyRemote(ctx, request.remote)
	}

	return request
}

// homeOf maps the git flags in request to an option of the git question.
func homeOf(request answers) int {
	switch {
	case !request.git:
		return homeNone
	case request.github:
		return homeGitHub
	case request.remote != "":
		return homeRemote
	default:
		return homeLocal
	}
}

// emptyRemote asks for a remote until it is reachable and empty. A push to a
// remote with commits would fail after the module is written.
func (a *asker) emptyRemote(ctx context.Context, fallback string) string {
	a.show(questions["remote"], nil)

	for {
		typed := a.read(fallback)

		err := checkEmpty(ctx, typed)
		if err == nil {
			a.confirm(typed)

			return typed
		}

		switch {
		case a.ended:
			return fallback
		case typed == "":
			fmt.Fprintln(a.out, "  Paste the address of an empty repository.")
		case errors.Is(err, errHasCommits):
			fmt.Fprintf(a.out, "  %s already has commits. Pick an empty repository.\n", typed)
		default:
			fmt.Fprintf(a.out, "  %s cannot be reached: %v. Check the address, or pick another.\n", typed, err)
		}
	}
}

// checkEmpty returns nil when remote can be reached and has no refs.
func checkEmpty(ctx context.Context, remote string) error {
	if remote == "" {
		return errNoAddress
	}

	refs, err := git(ctx, "", "ls-remote", remote)
	if err != nil {
		return err
	}

	if refs != "" {
		return errHasCommits
	}

	return nil
}

func gitSummary(request answers, name string) string {
	switch homeOf(request) {
	case homeNone:
		return "no"
	case homeGitHub:
		return "a first commit, pushed to a new private repository on GitHub, " + request.login + "/" + name
	case homeRemote:
		return "a first commit, pushed to " + request.remote
	default:
		return "a first commit, only here"
	}
}

// startRepository makes dir a git repository and commits everything in it.
// The folder was empty before the skeleton, so that is only the skeleton.
func startRepository(ctx context.Context, dir, name string) error {
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"add", "--all"},
		{"commit", "--quiet", "--message", "feat: start " + name + " from the module skeleton"},
	} {
		if _, err := git(ctx, dir, args...); err != nil {
			return err
		}
	}

	return nil
}

// publish pushes the first commit and returns the repository it went to. A
// failure is only a warning with the command to retry: the module is written.
func publish(ctx context.Context, env environment, steps *stepper, request answers, plan scaffold.Plan) string {
	folder := scaffold.ShellWord(shown(request.workDir, plan.Dir))

	switch homeOf(request) {
	case homeGitHub:
		repo := request.login + "/" + plan.Name
		args := []string{"repo", "create", repo, "--private", "--source", ".", "--remote", "origin", "--push"}

		if _, err := env.gh(ctx, plan.Dir, args...); err != nil {
			again := "git -C " + folder + " push --set-upstream origin HEAD"

			// gh adds origin only after the repository exists. Without
			// origin the retry is gh again, with it a plain push.
			if _, missing := git(ctx, plan.Dir, "remote", "get-url", "origin"); missing != nil {
				args[5] = folder
				again = "gh " + strings.Join(args, " ")
			}

			steps.warn("the new repository on GitHub failed: "+err.Error(), again)

			return ""
		}

		steps.next("made a private repository on GitHub, %s, and pushed the first commit to it", repo)

		return "github.com/" + repo
	case homeRemote:
		if err := push(ctx, plan.Dir, request.remote); err != nil {
			steps.warn("the push to "+request.remote+" failed: "+err.Error(),
				"git -C "+folder+" push --set-upstream origin HEAD")

			return ""
		}

		steps.next("pushed it to %s", request.remote)

		return request.remote
	}

	return ""
}

// push adds remote as origin and pushes the current branch to it.
func push(ctx context.Context, dir, remote string) error {
	if _, err := git(ctx, dir, "remote", "add", "origin", remote); err != nil {
		return err
	}

	_, err := git(ctx, dir, "push", "--quiet", "--set-upstream", "origin", "HEAD")

	return err
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	return runTool(ctx, dir, "git", args...)
}

func runGH(ctx context.Context, dir string, args ...string) (string, error) {
	return runTool(ctx, dir, "gh", args...)
}

// runTool runs tool in dir and returns its stdout. On failure the error is
// the one line of stderr that gives the reason.
func runTool(ctx context.Context, dir, tool string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, tool, args...)
	command.Dir = dir

	var said bytes.Buffer
	command.Stderr = &said

	printed, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %s", tool, args[0], firstOf(reasonOf(said.String()), err.Error()))
	}

	return strings.TrimSpace(string(printed)), nil
}

// reasonOf picks the first error or fatal line of stderr, else the first
// line that is not a hint. A rejected push gets its own sentence.
func reasonOf(said string) string {
	first := ""

	for line := range strings.Lines(said) {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "! [rejected]") &&
			(strings.HasSuffix(line, "(fetch first)") || strings.HasSuffix(line, "(non-fast-forward)")) {
			return "the remote already has commits this one does not"
		}

		for _, prefix := range []string{"fatal: ", "error: "} {
			if reason, found := strings.CutPrefix(line, prefix); found {
				return reason
			}
		}

		if first == "" && line != "" && !strings.HasPrefix(line, "hint:") {
			first = line
		}
	}

	return first
}
