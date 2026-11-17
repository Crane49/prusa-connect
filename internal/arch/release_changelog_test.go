package arch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scripts/release.sh bumped the version, committed and tagged without reading
// docs/CHANGELOG.md at all. `make changelog-check` held every commit and
// nothing held the release, so a tag could ship with its own news still
// pending.
//
// Earlier research names the two checks and calls each free. They live
// in scripts/check-release-changelog.sh, and this runs that script rather than
// a second copy of its logic: a gate rebuilt outside itself passes and fails
// for its own reasons. Since ADR-20261021-arm the pending news is a fragment per change
// in docs/changes/, so the first check is that the folder holds none.
const releaseGate = "scripts/check-release-changelog.sh"

// changesGate is the shape of the fragments, which the cut runs before it files
// anything and `make changelog-check` runs on every push.
const changesGate = "scripts/check-changes.sh"

// fragment is one file of pending news in a fixture's docs/changes/.
type fragment struct {
	name string
	body string
}

// newsFixture writes a changelog and a folder of fragments, and answers both
// paths. The folder always holds the README the real one holds, which is no
// news.
func newsFixture(t *testing.T, changelog string, fragments ...fragment) (string, string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "CHANGELOG.md")
	changes := filepath.Join(dir, "changes")

	if err := os.MkdirAll(changes, 0o750); err != nil {
		t.Fatalf("making the fixture's changes folder: %v", err)
	}

	writes := append([]fragment{{name: "README.md", body: "# Pending changes\n\n- not news\n"}}, fragments...)

	for _, one := range writes {
		if err := os.WriteFile(filepath.Join(changes, one.name), []byte(one.body), 0o600); err != nil {
			t.Fatalf("writing the fixture fragment %s: %v", one.name, err)
		}
	}

	if err := os.WriteFile(path, []byte(changelog), 0o600); err != nil {
		t.Fatalf("writing the fixture changelog: %v", err)
	}

	return path, changes
}

// TestTheReleaseGateReadsTheChangelogItIsGiven runs the gate over a fixture per
// case. The passing case is what stops the refusals being a script that
// refuses everything.
func TestTheReleaseGateReadsTheChangelogItIsGiven(t *testing.T) {
	t.Parallel()

	const released = "## 0.2.0 - 2026-08-29\n\n### Added\n\n- A thing a reader can now do.\n"

	pending := fragment{name: "20261021-143210-a-thing.md", body: "### Fixed\n- A thing nobody filed.\n"}

	cases := []struct {
		name      string
		version   string
		changelog string
		fragments []fragment
		refused   bool
		says      string
	}{
		{
			name:      "a release whose news is filed",
			version:   "0.2.0",
			changelog: "# Changelog\n\n" + released,
			refused:   false,
			says:      "names 0.2.0",
		},
		{
			name:      "a release whose news is still pending",
			version:   "0.2.0",
			changelog: "# Changelog\n\n" + released,
			fragments: []fragment{pending},
			refused:   true,
			says:      "still holds 1 fragment",
		},
		{
			name:      "a heading naming another version",
			version:   "0.3.0",
			changelog: "# Changelog\n\n" + released,
			refused:   true,
			says:      "newest version heading is 0.2.0",
		},
		{
			name:      "a changelog with no version heading at all",
			version:   "0.2.0",
			changelog: "# Changelog\n",
			refused:   true,
			says:      "carries no version heading",
		},
		{
			name:      "a changelog that grew a pending section again",
			version:   "0.2.0",
			changelog: "# Changelog\n\n## Unreleased\n\n" + released,
			refused:   true,
			says:      "carries a '## Unreleased' heading",
		},
		{
			name:      "a prerelease whose news is filed",
			version:   "0.0.1-alpha.1",
			changelog: "# Changelog\n\n## 0.0.1-alpha.1 - 2026-10-02\n\n- A thing.\n",
			refused:   false,
			says:      "names 0.0.1-alpha.1",
		},
		{
			name:      "a stable tag over a prerelease heading",
			version:   "0.0.1",
			changelog: "# Changelog\n\n## 0.0.1-beta.2 - 2026-10-02\n\n- A thing.\n",
			refused:   true,
			says:      "newest version heading is 0.0.1-beta.2",
		},
	}

	root := repoRoot(t)
	requireScanned(t, "cases the release gate is run over", len(cases), 5,
		"A refusing gate with no passing case is a script that refuses everything.")

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			changelog, changes := newsFixture(t, testCase.changelog, testCase.fragments...)

			output, err := runScript(t, root, releaseGate, testCase.version, changelog, changes)

			switch {
			case testCase.refused && err == nil:
				t.Errorf("%s passed %q and should have refused it.\nIt said: %s",
					releaseGate, testCase.name, output)
			case !testCase.refused && err != nil:
				t.Errorf("%s refused %q and should have passed it.\nIt said: %s",
					releaseGate, testCase.name, output)
			}

			if !strings.Contains(output, testCase.says) {
				t.Errorf("%s did not say %q over %q.\nIt said: %s",
					releaseGate, testCase.says, testCase.name, output)
			}
		})
	}
}

// TestTheReleaseScriptRunsTheGate is the wiring. A gate nothing calls is the
// way this class of check dies, and release.sh is its only caller.
func TestTheReleaseScriptRunsTheGate(t *testing.T) {
	t.Parallel()

	const releaseScript = "scripts/release.sh"

	body, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(releaseScript)))
	if err != nil {
		t.Fatalf("reading %s: %v", releaseScript, err)
	}

	if !strings.Contains(withoutComments(normaliseNewlines(string(body))), "check-release-changelog.sh") {
		t.Errorf("%s does not run %s.\n"+
			"It is the only caller, so a release cut without it reads no changelog at all.",
			releaseScript, releaseGate)
	}
}

// scripts/cut-release-changelog.sh does the filing the gate above asks for, so
// that .github/workflows/cut-release.yml can cut a release with no person at
// the keyboard. Its output is judged by the gate itself: a cut the gate
// refuses is a cut that did nothing useful.
const changelogCut = "scripts/cut-release-changelog.sh"

// TestTheChangelogCutFilesEveryFragment runs the cut over two fragments, then
// the gate over what it wrote. The entries come grouped by category in the
// order changelog-discipline.md gives, each category in the order the
// fragments were written, the released sections below come out byte for byte
// as they went in, and the fragments are gone. A "###" line inside a code
// sample stays the sample's, and a link loses the one "../" a fragment needs.
func TestTheChangelogCutFilesEveryFragment(t *testing.T) {
	t.Parallel()

	const released = "## 0.0.1-alpha.1 - 2026-10-02\n\n### Added\n\n- An older thing.\n"

	changelog, changes := newsFixture(
		t,
		"# Changelog\n\nIntro.\n\n"+released,
		fragment{
			name: "20261021-100000-later.md",
			body: "### Fixed\n- A later fix, see [the record](../decisions/0083-x.md).\n\n### Added\n- A later thing\n  on two lines.\n\n  ```yaml\n  ### a comment in a sample\n  key: 1\n  ```\n",
		},
		fragment{
			name: "20261020-090000-earlier.md",
			body: "### Added\n- An earlier thing.\n\n```\n- not an entry\n```\n\n### Security\n- A hole closed.\n",
		},
	)

	root := repoRoot(t)

	output, err := runScript(t, root, changelogCut, "0.0.1-alpha.2", "2026-10-16", changelog, changes)
	if err != nil {
		t.Fatalf("%s refused two fragments.\nIt said: %s", changelogCut, output)
	}

	gate, err := runScript(t, root, releaseGate, "0.0.1-alpha.2", changelog, changes)
	if err != nil {
		t.Errorf("%s refused what %s wrote.\nIt said: %s", releaseGate, changelogCut, gate)
	}

	written, err := os.ReadFile(changelog)
	if err != nil {
		t.Fatalf("reading the fixture back: %v", err)
	}

	want := "# Changelog\n\nIntro.\n\n## 0.0.1-alpha.2 - 2026-10-16\n\n" +
		"### Security\n- A hole closed.\n\n" +
		"### Added\n- An earlier thing.\n\n```\n- not an entry\n```\n- A later thing\n  on two lines.\n\n" +
		"  ```yaml\n  ### a comment in a sample\n  key: 1\n  ```\n\n" +
		"### Fixed\n- A later fix, see [the record](decisions/0083-x.md).\n\n" + released
	if string(written) != want {
		t.Errorf("%s wrote\n%s\nand should have written\n%s", changelogCut, written, want)
	}

	left, err := os.ReadDir(changes)
	if err != nil {
		t.Fatalf("reading the fixture's changes folder: %v", err)
	}

	if len(left) != 1 || left[0].Name() != "README.md" {
		names := make([]string, 0, len(left))
		for _, one := range left {
			names = append(names, one.Name())
		}

		t.Errorf("%s left %v in the changes folder, and only README.md stays", changelogCut, names)
	}
}

// TestTheChangelogPreviewChangesNothing is `make changelog-preview`: the
// section on standard output, and the changelog and the fragments untouched.
func TestTheChangelogPreviewChangesNothing(t *testing.T) {
	t.Parallel()

	const before = "# Changelog\n\n## 0.0.1 - 2026-10-02\n\n- A thing.\n"

	changelog, changes := newsFixture(t, before,
		fragment{name: "20261021-100000-thing.md", body: "### Added\n- A new thing.\n"})

	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no POSIX shell on PATH, so %s cannot be run here: %v", changelogCut, err)
	}

	root := repoRoot(t)
	cmd := exec.Command(shell, filepath.Join(root, filepath.FromSlash(changelogCut)),
		"0.0.2", "2026-10-21", changelog, changes)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PREVIEW=1")

	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s with PREVIEW=1 failed: %v", changelogCut, err)
	}

	if want := "## 0.0.2 - 2026-10-21\n\n### Added\n- A new thing.\n"; string(output) != want {
		t.Errorf("the preview printed\n%s\nand should have printed\n%s", output, want)
	}

	after, err := os.ReadFile(changelog)
	if err != nil {
		t.Fatalf("reading the fixture back: %v", err)
	}

	if string(after) != before {
		t.Errorf("the preview wrote the changelog:\n%s", after)
	}

	if _, statErr := os.Stat(filepath.Join(changes, "20261021-100000-thing.md")); statErr != nil {
		t.Errorf("the preview deleted the fragment: %v", statErr)
	}
}

// TestTheChangelogCutRefusesAReleaseWithNoNews holds the refusals: no
// fragment, a version that already has a heading, and a fragment the release
// could not file.
func TestTheChangelogCutRefusesAReleaseWithNoNews(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		changelog string
		fragments []fragment
		says      string
	}{
		{
			name:      "nothing pending",
			changelog: "# Changelog\n\n## 0.0.1 - 2026-10-02\n\n- A thing.\n",
			says:      "holds no entry",
		},
		{
			name:      "the version is filed already",
			changelog: "# Changelog\n\n## 0.0.2 - 2026-10-02\n\n- A thing.\n",
			fragments: []fragment{{name: "20261021-100000-new.md", body: "### Added\n- New.\n"}},
			says:      "already has a heading for 0.0.2",
		},
		{
			name:      "a fragment the release cannot file",
			changelog: "# Changelog\n\n## 0.0.1 - 2026-10-02\n\n- A thing.\n",
			fragments: []fragment{{name: "20261021-100000-new.md", body: "### Fixes\n- New.\n"}},
			says:      "does not have the shape a release files",
		},
	}

	root := repoRoot(t)

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			changelog, changes := newsFixture(t, testCase.changelog, testCase.fragments...)

			output, err := runScript(t, root, changelogCut, "0.0.2", "2026-10-16", changelog, changes)
			if err == nil {
				t.Errorf("%s cut %q and should have refused.\nIt said: %s", changelogCut, testCase.name, output)
			}

			if !strings.Contains(output, testCase.says) {
				t.Errorf("%s did not say %q over %q.\nIt said: %s", changelogCut, testCase.says, testCase.name, output)
			}
		})
	}
}

// TestTheChangesGateRefusesWhatAReleaseCannotFile runs the shape gate over one
// fragment per case, with a passing case so the refusals are not a script that
// refuses everything.
func TestTheChangesGateRefusesWhatAReleaseCannotFile(t *testing.T) {
	t.Parallel()

	const changelog = "# Changelog\n\n## 0.0.1 - 2026-10-02\n\n- A thing.\n"

	cases := []struct {
		name      string
		changelog string
		fragment  fragment
		refused   bool
		says      string
	}{
		{
			name: "a fragment a release files",
			fragment: fragment{
				name: "20261021-143210-nearby-printers.md",
				body: "### Changed\n- A thing.\n\n### Added\n- Another.\n",
			},
			says: "changes gates passed",
		},
		{
			name:     "a name with no time in it",
			fragment: fragment{name: "nearby-printers.md", body: "### Changed\n- A thing.\n"},
			refused:  true,
			says:     "is not named <YYYYMMDD>-<HHMMSS>-<words>.md",
		},
		{
			name:     "a category the changelog does not have",
			fragment: fragment{name: "20261021-143210-a.md", body: "### Fixes\n- A thing.\n"},
			refused:  true,
			says:     "\"Fixes\" is not a category",
		},
		{
			name:     "text before the first category",
			fragment: fragment{name: "20261021-143210-a.md", body: "A thing.\n\n### Fixed\n- A thing.\n"},
			refused:  true,
			says:     "text before the first category heading",
		},
		{
			name:     "a version heading of its own",
			fragment: fragment{name: "20261021-143210-a.md", body: "## 0.0.2\n### Fixed\n- A thing.\n"},
			refused:  true,
			says:     "a level-one or level-two heading",
		},
		{
			name: "a link from this folder",
			fragment: fragment{
				name: "20261021-143210-a.md",
				body: "### Fixed\n- A thing, see [the record](decisions/a.md).\n",
			},
			refused: true,
			says:    "the link \"decisions/a.md\" is relative to this folder",
		},
		{
			name: "links the release can file, and a link in a code sample",
			fragment: fragment{name: "20261021-143210-a.md", body: "### Fixed\n- See [a](../decisions/a.md), " +
				"[b](https://example.com/b) and [c](#c).\n\n```\n[d](d.md)\n### not a heading\n```\n"},
			says: "changes gates passed",
		},
		{
			name:     "a heading with no entry under it",
			fragment: fragment{name: "20261021-143210-a.md", body: "### Fixed\n\n"},
			refused:  true,
			says:     "holds no entry",
		},
		{
			name:      "a changelog that grew a pending section again",
			changelog: "# Changelog\n\n## Unreleased\n\n- A thing.\n",
			fragment:  fragment{name: "20261021-143210-a.md", body: "### Fixed\n- A thing.\n"},
			refused:   true,
			says:      "carries a '## Unreleased' heading",
		},
	}

	root := repoRoot(t)
	requireScanned(t, "cases the changes gate is run over", len(cases), 5,
		"A refusing gate with no passing case is a script that refuses everything.")

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			body := testCase.changelog
			if body == "" {
				body = changelog
			}

			path, changes := newsFixture(t, body, testCase.fragment)

			output, err := runScript(t, root, changesGate, changes, path)

			switch {
			case testCase.refused && err == nil:
				t.Errorf("%s passed %q and should have refused it.\nIt said: %s", changesGate, testCase.name, output)
			case !testCase.refused && err != nil:
				t.Errorf("%s refused %q and should have passed it.\nIt said: %s", changesGate, testCase.name, output)
			}

			if !strings.Contains(output, testCase.says) {
				t.Errorf("%s did not say %q over %q.\nIt said: %s", changesGate, testCase.says, testCase.name, output)
			}
		})
	}
}

// TestTheReleaseWorkflowCommitsTheDeletedFragments is the wiring of the cut in
// CI. The cut deletes the fragments it filed, and a commit that names the
// changelog alone would leave them in the tree for the next release to file a
// second time.
func TestTheReleaseWorkflowCommitsTheDeletedFragments(t *testing.T) {
	t.Parallel()

	const workflow = ".github/workflows/cut-release.yml"

	body, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(workflow)))
	if err != nil {
		t.Fatalf("reading %s: %v", workflow, err)
	}

	text := withoutComments(normaliseNewlines(string(body)))

	if !strings.Contains(text, changelogCut) {
		t.Fatalf("%s does not run %s, so no release files its news", workflow, changelogCut)
	}

	if !strings.Contains(text, "git add -A docs/CHANGELOG.md docs/changes") {
		t.Errorf("%s does not stage docs/changes after the cut, so the fragments it deleted stay in the "+
			"tree and the next release files them again", workflow)
	}
}

func runScript(t *testing.T, root, script string, args ...string) (string, error) {
	t.Helper()

	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no POSIX shell on PATH, so %s cannot be run here: %v", script, err)
	}

	cmd := exec.Command(shell, append([]string{filepath.Join(root, filepath.FromSlash(script))}, args...)...)
	cmd.Dir = root

	output, runErr := cmd.CombinedOutput()

	return string(output), runErr
}

// TestAFragmentIsWhereATrailerFindsItsEntry runs the trailer gate over real
// commits in a sparse clone of this repository. A commit whose trailer is the
// first sentence of a fragment it adds passes, and one whose fragment says
// something else is refused. It is the gate's one change under ADR-20261021-arm, and
// no fixture without history can reach it.
func TestAFragmentIsWhereATrailerFindsItsEntry(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	// The gate judges nothing at or below its floor and refuses a clone that
	// lacks it, so a shallow checkout cannot run this. CI checks out the whole
	// history.
	if err := exec.Command("git", "-C", root, "cat-file", "-e", "b935cff^{commit}").Run(); err != nil {
		t.Skip("this clone lacks the changelog gate's floor commit, so the gate cannot run here")
	}

	clone := filepath.Join(t.TempDir(), "clone")

	run := func(dir string, args ...string) string {
		t.Helper()

		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=fixture",
			"GIT_AUTHOR_EMAIL=fixture@example.com", "GIT_COMMITTER_NAME=fixture",
			"GIT_COMMITTER_EMAIL=fixture@example.com")

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}

		return string(out)
	}

	run(root, "clone", "--quiet", "--shared", "--no-checkout", root, clone)
	run(clone, "config", "core.hooksPath", "/dev/null")
	run(clone, "sparse-checkout", "set", "--no-cone", "/scripts/", "/docs/changes/", "/docs/CHANGELOG.md")
	run(clone, "checkout", "--quiet", "--detach", "HEAD")

	// The scripts under test are the working tree's, which may be ahead of
	// the commit the clone checked out.
	for _, script := range []string{"scripts/check-changelog.sh", changesGate} {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(script)))
		if err != nil {
			t.Fatalf("reading %s: %v", script, err)
		}

		if writeErr := os.WriteFile(filepath.Join(clone, filepath.FromSlash(script)), body, 0o600); writeErr != nil {
			t.Fatalf("copying %s into the clone: %v", script, writeErr)
		}
	}

	run(clone, "add", "scripts")
	run(clone, "commit", "--quiet", "--allow-empty", "-m", "test: take the scripts under test", "-m", "Changelog: none")
	base := strings.TrimSpace(run(clone, "rev-parse", "HEAD"))

	commit := func(name, body, trailer string) {
		t.Helper()

		if err := os.WriteFile(filepath.Join(clone, "docs", "changes", name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing the fragment %s: %v", name, err)
		}

		run(clone, "add", "docs/changes")
		run(clone, "commit", "--quiet", "-m", "fix: a fixture change", "-m", "Fixes: none", "-m", "Changelog: "+trailer)
	}

	gate := func() (string, error) {
		t.Helper()

		shell, err := exec.LookPath("sh")
		if err != nil {
			t.Skipf("no POSIX shell on PATH: %v", err)
		}

		cmd := exec.Command(shell, "scripts/check-changelog.sh", base+"..HEAD")
		cmd.Dir = clone

		out, runErr := cmd.CombinedOutput()

		return string(out), runErr
	}

	commit(
		"20261021-150000-fixture-one.md",
		"### Fixed\n- A fixture printer reconnects at once. A second one does too.\n",
		"A fixture printer reconnects at once.",
	)

	if output, err := gate(); err != nil {
		t.Fatalf("the gate refused a trailer whose sentence the fragment it added carries.\nIt said: %s", output)
	}

	commit("20261021-150001-fixture-two.md", "### Fixed\n- Something else entirely.\n",
		"A fixture camera answers again.")

	output, err := gate()
	if err == nil {
		t.Fatalf("the gate passed a trailer no fragment carries.\nIt said: %s", output)
	}

	if !strings.Contains(output, "nothing matching it was added to a fragment") {
		t.Errorf("the gate refused for another reason than the missing entry.\nIt said: %s", output)
	}

	// A later commit on the branch rewords the entry. The first commit's
	// trailer is gone from the net diff, and an exception for it would name a
	// sha a squash merge deletes, so the gate has to pass both without one.
	run(clone, "reset", "--quiet", "--hard", base)
	commit("20261021-150002-fixture-three.md", "### Fixed\n- A fixture printer reconnects at once.\n",
		"A fixture printer reconnects at once.")
	commit("20261021-150002-fixture-three.md", "### Fixed\n- A fixture printer reconnects within a second.\n",
		"A fixture printer reconnects within a second.")

	output, err = gate()
	if err != nil {
		t.Fatalf("the gate refused a trailer whose entry a later commit in the range reworded.\nIt said: %s", output)
	}

	// The entry is withdrawn whole, so the trailer names news that ships nowhere.
	run(clone, "rm", "--quiet", "docs/changes/20261021-150002-fixture-three.md")
	run(clone, "commit", "--quiet", "-m", "docs: withdraw the fixture entry", "-m", "Changelog: none")

	output, err = gate()
	if err == nil {
		t.Fatalf("the gate passed a trailer whose fragment the range deletes.\nIt said: %s", output)
	}

	if !strings.Contains(output, "nothing matching it was added to a fragment") {
		t.Errorf("the gate refused a withdrawn entry for another reason than the missing entry.\nIt said: %s", output)
	}
}
