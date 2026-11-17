package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// isolateGit points git at a configuration of this test's own, with an
// identity to commit as, so the result does not depend on the machine's.
// t.Setenv keeps the test off t.Parallel, and the parallel tests wait until it
// is done.
func isolateGit(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("no git on PATH: %v", err)
	}

	config := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(config,
		[]byte("[user]\n\tname = Test\n\temail = test@example.com\n[init]\n\tdefaultBranch = main\n"), 0o600))

	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// gitIn runs git in dir and returns what it printed.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()

	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = dir

	said, err := command.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, said)

	return string(said)
}

// TestTheModuleIsCommittedAndPushedToTheRemote. The first commit holds what
// the skeleton wrote, and it reaches the remote.
func TestTheModuleIsCommittedAndPushedToTheRemote(t *testing.T) {
	isolateGit(t)

	remote := filepath.Join(t.TempDir(), "remote.git")
	gitIn(t, "", "init", "--quiet", "--bare", remote)

	env := here(t)

	var out bytes.Buffer

	require.NoError(t, run(t.Context(), &out, nil, env, []string{
		"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf", "-remote", remote,
	}))

	dir := filepath.Join(env.workDir, "shelf")

	tracked := gitIn(t, dir, "ls-files")
	require.Contains(t, tracked, "module.yaml")
	require.Contains(t, tracked, ".gitignore")
	require.Contains(t, tracked, ".vscode/extensions.json")

	require.Contains(t, gitIn(t, "", "--git-dir", remote, "log", "--all", "--format=%s"), "start shelf")

	// What a build writes stays out of the repository. --quiet only accepts one
	// pathname at a time.
	gitIn(t, dir, "check-ignore", "--quiet", "ui/node_modules/x")
	gitIn(t, dir, "check-ignore", "--quiet", "ui/dist/ui/index.js")

	require.Contains(t, out.String(), "pushed it to "+remote)
}

// TestAFailedPushLeavesTheModuleWrittenAndCommitted. The files and the commit
// are the author's work, and a remote that refuses is no reason to lose them
// or to fail the run: it ends with one line saying why and the command that
// tries again.
func TestAFailedPushLeavesTheModuleWrittenAndCommitted(t *testing.T) {
	isolateGit(t)

	mine := t.TempDir()
	gitIn(t, mine, "init", "--quiet")
	gitIn(t, mine, "commit", "--quiet", "--allow-empty", "--message", "mine")

	full := filepath.Join(t.TempDir(), "full.git")
	gitIn(t, "", "clone", "--quiet", "--bare", mine, full)

	for name, c := range map[string]struct{ remote, reason string }{
		"a remote that does not exist":      {filepath.Join(t.TempDir(), "nothing-here.git"), "does not appear"},
		"a remote that already has commits": {full, "git push: the remote already has commits this one does not\n"},
	} {
		t.Run(name, func(t *testing.T) {
			env := here(t)

			var out bytes.Buffer

			require.NoError(t, run(t.Context(), &out, nil, env, []string{
				"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf", "-remote", c.remote,
			}))

			dir := filepath.Join(env.workDir, "shelf")
			require.FileExists(t, filepath.Join(dir, "module.yaml"))
			gitIn(t, dir, "rev-parse", "--verify", "HEAD")

			said := out.String()
			require.Contains(t, said, "3. warning: the push to "+c.remote+" failed: ")
			require.Contains(t, said, c.reason)
			require.Contains(t, said, "\n     To try again: git -C "+shelfIn()+" push --set-upstream origin HEAD\n")
			require.NotContains(t, said, "hint:")
			require.Contains(t, said, "✔ shelf is ready\n")
			require.NotContains(t, said, "  repo ", "a repository the push never reached was named")
		})
	}
}

// TestAFailedGitHubRepositoryIsAWarning. gh refusing, for a name the account
// already has, leaves the module written and committed, and says how to try
// again.
func TestAFailedGitHubRepositoryIsAWarning(t *testing.T) {
	isolateGit(t)

	var calls []string

	env := here(t)
	env.gh = gitHub("acme", errors.New("gh repo create: Name already exists on this account"), &calls)

	var out bytes.Buffer

	require.NoError(t, run(t.Context(), &out, nil, env, []string{
		"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf", "-github",
	}))
	gitIn(t, filepath.Join(env.workDir, "shelf"), "rev-parse", "--verify", "HEAD")

	said := out.String()
	require.Contains(t, said,
		"3. warning: the new repository on GitHub failed: gh repo create: Name already exists on this account\n")
	require.Contains(t, said,
		"     To try again: gh repo create acme/shelf --private --source "+shelfIn()+" --remote origin --push\n")
}

// TestAGitHubPushThatFailsIsTriedAgainWithAPush. gh makes the repository and
// adds origin before it pushes, so a push that fails leaves a name gh would
// now refuse, and the way on is a push.
func TestAGitHubPushThatFailsIsTriedAgainWithAPush(t *testing.T) {
	isolateGit(t)

	var calls []string

	env := here(t)
	created := gitHub("acme", nil, &calls)
	env.gh = func(ctx context.Context, dir string, args ...string) (string, error) {
		if args[0] != "repo" {
			return created(ctx, dir, args...)
		}

		gitIn(t, dir, "remote", "add", "origin", "https://github.com/acme/shelf.git")

		return "", errors.New("gh repo create: failed to push")
	}

	var out bytes.Buffer

	require.NoError(t, run(t.Context(), &out, nil, env, []string{
		"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf", "-github",
	}))

	require.Contains(t, out.String(), "     To try again: git -C "+shelfIn()+" push --set-upstream origin HEAD\n")
}
