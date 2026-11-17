package arch_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The commit gates are installed and not running.
//
// .githooks/ holds them and `make install-hooks` points core.hooksPath there.
// The frontend's `pnpm install` runs husky, which points the same setting at
// frontend/.husky/_ and takes every hook in .githooks/ out of the run. Two fix
// commits reached this branch with no Changelog: trailer in that state.
//
// The repair is delegation: a file under frontend/.husky per hook, running the
// one in .githooks. Both settings then run the same gates, so the order of
// `pnpm install` and `make install-hooks` decides nothing.
//
// What these cases hold is the wiring. Whether the delegate is reached is
// proved by running it, in scripts/check-hooks.sh, which .githooks/pre-push
// calls. It cannot be proved here: `sh` is not on a Windows PATH outside a git
// shell, so a Go test that shelled out would skip on the machine this was
// written on, and a skip is the failure this whole section is about.
const (
	repoHooksDir  = ".githooks"
	huskyHooksDir = "frontend/.husky"
	hooksScript   = "scripts/check-hooks.sh"
)

// TestEveryRepositoryHookHasAHuskyDelegate reads .githooks/ rather than a list,
// so a hook added there is covered the day it appears.
func TestEveryRepositoryHookHasAHuskyDelegate(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, repoHooksDir))
	if err != nil {
		t.Fatalf("read %s: %v", repoHooksDir, err)
	}

	var hooks []string

	// git names every hook it runs after an event, and no event name begins
	// with a dot. The directory's own .gitattributes is the file this skips.
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			hooks = append(hooks, e.Name())
		}
	}

	requireScanned(t, "hooks under "+repoHooksDir, len(hooks), 2,
		"The commit-msg and pre-push gates live there. An empty read means the directory moved.")

	for _, hook := range hooks {
		delegate := filepath.Join(huskyHooksDir, hook)

		body, readErr := os.ReadFile(filepath.Join(root, delegate))
		if readErr != nil {
			t.Errorf("%s has no delegate at %s, so the frontend's install disables it: %v",
				filepath.Join(repoHooksDir, hook), delegate, readErr)

			continue
		}

		want := repoHooksDir + "/" + hook

		if !strings.Contains(uncommented(string(body)), want) {
			t.Errorf("%s names no %q outside its comments.\n"+
				"husky exits 0 when a dispatcher has no file beside it, so a delegate that runs\n"+
				"nothing is a hook that passes every commit.", delegate, want)
		}
	}
}

// TestTheHuskyDelegatesSitWhereHuskyDispatches reads the directory husky is
// installed into out of the frontend's own package.json. husky's dispatcher
// runs `<dir>/<hook>` for a dispatcher at `<dir>/_/<hook>`, so a delegate in a
// directory husky no longer uses is a delegate nothing reaches.
func TestTheHuskyDelegatesSitWhereHuskyDispatches(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	body, err := os.ReadFile(filepath.Join(root, "frontend/package.json"))
	if err != nil {
		t.Fatalf("read frontend/package.json: %v", err)
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}

	if unmarshalErr := json.Unmarshal(body, &pkg); unmarshalErr != nil {
		t.Fatalf("parse frontend/package.json: %v", unmarshalErr)
	}

	requireScanned(t, "scripts in frontend/package.json", len(pkg.Scripts), 1)

	// A missing prepare script is a failure and not a skip. It means either that
	// the frontend stopped installing husky, in which case the delegates and
	// this case go with it, or that the install moved somewhere nothing here
	// reads. Both are decisions, and a skip makes neither.
	prepare, ok := pkg.Scripts["prepare"]
	if !ok {
		t.Fatalf("frontend/package.json runs no prepare script, so nothing here knows where husky\n" +
			"installs its hooks path. If the frontend no longer installs husky, delete the\n" +
			"delegates under " + huskyHooksDir + " and this case with them.")
	}

	if !strings.Contains(prepare, huskyHooksDir) {
		t.Errorf("frontend/package.json's prepare script is %q and does not name %s.\n"+
			"That is where the delegates are, so husky now dispatches to a directory holding none\n"+
			"and every gate in %s is off after a `pnpm install`.", prepare, huskyHooksDir, repoHooksDir)
	}
}

// hooksScriptCallers are the files that have to invoke the hook gate. It has no
// CI caller on purpose: CI checks out with no hooks, so a job asserting that
// hooks run would first have to install them and would then be asserting its
// own setup.
var hooksScriptCallers = []struct {
	path string
	call string
	why  string
}{
	{
		path: ".githooks/pre-push",
		call: "sh scripts/check-hooks.sh",
		why:  "the push, which is the moment a developer's hooks demonstrably ran",
	},
}

func TestTheHookGateIsCalledFromEveryPlaceThatNeedsIt(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	if _, err := os.Stat(filepath.Join(root, hooksScript)); err != nil {
		t.Fatalf("%s is missing, so nothing checks that a clone's hooks run: %v", hooksScript, err)
	}

	requireScanned(t, "callers of "+hooksScript, len(hooksScriptCallers), 1)

	for _, c := range hooksScriptCallers {
		body, err := os.ReadFile(filepath.Join(root, c.path))
		if err != nil {
			t.Fatalf("reading %s: %v", c.path, err)
		}

		if !strings.Contains(uncommented(string(body)), c.call) {
			t.Errorf("%s runs no %q outside its comments.\nIt is what judges %s.",
				c.path, c.call, c.why)
		}
	}
}
