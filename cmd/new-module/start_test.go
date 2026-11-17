package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/scaffold"
)

// devLoop is what the fake install and launch saw: each install's target, and
// the launch, when one was asked for.
type devLoop struct {
	installs []target
	launched *launch
}

// faking gives env an install that prints what install.sh prints and fails
// with failure, and a launch that records what it was asked to start. Neither
// runs a process.
func faking(env environment, printed string, failure error) (environment, *devLoop) {
	seen := &devLoop{}

	env.install = func(_ context.Context, _ string, into target) ([]byte, error) {
		seen.installs = append(seen.installs, into)

		return []byte(printed), failure
	}
	env.launch = func(_ context.Context, _ io.Writer, _ bool, l launch) error {
		seen.launched = &l

		return nil
	}

	return env, seen
}

// written is a module written from flags beside a checkout of this test's own,
// and the environment that wrote it, run from that checkout.
func written(t *testing.T, kind string) (environment, string) {
	t.Helper()

	checkout := fakeCheckout(t)
	env := environment{workDir: checkout, version: allTools}

	require.NoError(t, run(t.Context(), io.Discard, nil, env,
		[]string{"-name", "shelf", "-kind", kind, "-repo", "github.com/acme/shelf", "-git=false"}))

	return env, filepath.Join(filepath.Dir(checkout), "shelf")
}

// TestAPortSomethingHoldsIsSkipped. The core is started beside whatever
// already runs, and a port another server answers on is never taken.
func TestAPortSomethingHoldsIsSkipped(t *testing.T) {
	t.Parallel()

	var binder net.ListenConfig

	held, err := binder.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer held.Close()

	taken := held.Addr().(*net.TCPAddr).Port

	port, err := freePort(t.Context(), "127.0.0.1", taken)
	require.NoError(t, err)
	require.Greater(t, port, taken)
}

// TestTheStartIsAskedOnlyAfterAnInstall. Starting a module that was never
// installed shows nothing of it, so the question comes only after the
// install, and "not now" prints the one command that starts it later.
func TestTheStartIsAskedOnlyAfterAnInstall(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		typed    []string
		installs []target
		started  target
		later    bool
	}{
		"the demo":                 {[]string{"y", "1"}, []target{targetDemo}, targetDemo, false},
		"the real app":             {[]string{"y", "2"}, []target{targetDemo, targetApp}, targetApp, false},
		"not now":                  {[]string{"y", "3"}, []target{targetDemo}, "", true},
		"no install":               {[]string{"n"}, nil, "", true},
		"nobody answers the start": {[]string{"y"}, []target{targetDemo}, "", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env, dir := written(t, "ui")
			env, seen := faking(env, "1. shelf installs and builds into a bundle...\n", nil)

			var out bytes.Buffer

			request := answers{workDir: env.workDir, shape: shapeUI}
			plan := planOf(t, request, dir)
			require.NoError(t, startDevLoop(t.Context(), answering(&out, c.typed...), &out, env,
				request, plan, &stepper{out: &out}))

			said := out.String()
			require.Equal(t, c.installs, seen.installs)
			require.NotContains(t, said, "shelf installs and builds", "install.sh's own steps were shown")
			require.Contains(t, said, "✔ shelf is ready\n  folder  "+filepath.Join("..", "shelf")+"\n")

			if len(c.installs) > 0 {
				require.Contains(t, said, "1. ✔ built and installed\n")
				require.Contains(t, said, "\n? Start it now?\n")
			} else {
				require.NotContains(t, said, "Start it now?")
			}

			if c.started == "" {
				require.Nil(t, seen.launched)
			} else {
				require.NotNil(t, seen.launched)
				require.Equal(t, c.started, seen.launched.into)
				require.Equal(t, "shelf", seen.launched.name)
				require.True(t, seen.launched.ui)
			}

			later := "go run ./cmd/new-module -start demo -dir " + shelfIn("..")
			if c.later {
				require.Contains(t, said, later)
			} else {
				require.NotContains(t, said, later)
			}
		})
	}
}

// planOf is the plan of the module already written at dir.
func planOf(t *testing.T, request answers, dir string) scaffold.Plan {
	t.Helper()

	return scaffold.Plan{Name: "shelf", Dir: dir, Checkout: filepath.ToSlash(request.workDir), UI: true}
}

// TestAFailedInstallShowsWhatInstallShPrinted. Nothing of a good install is
// shown, and all of a failed one is: the end of the log and where it is.
func TestAFailedInstallShowsWhatInstallShPrinted(t *testing.T) {
	t.Parallel()

	env, dir := written(t, "ui")
	env, _ = faking(env, "FAIL  pnpm --dir ui run build failed. The end of /x/install.log:\nboom\n",
		errors.New("exit status 1"))

	var out bytes.Buffer

	request := answers{workDir: env.workDir, shape: shapeUI}
	err := startDevLoop(t.Context(), answering(&out, "y"), &out, env, request, planOf(t, request, dir),
		&stepper{out: &out})
	require.Error(t, err)
	require.Contains(t, err.Error(), "The end of /x/install.log:\nboom")
	require.NotContains(t, out.String(), "built and installed")
}

// TestWithNoTerminalTheLaterCommandIsPrinted. A script is never asked, starts
// nothing, and is told the one command that starts the module.
func TestWithNoTerminalTheLaterCommandIsPrinted(t *testing.T) {
	t.Parallel()

	checkout := fakeCheckout(t)

	var out bytes.Buffer

	require.NoError(t, run(t.Context(), &out, nil, environment{workDir: checkout, version: allTools},
		[]string{"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf", "-git=false"}))

	require.Contains(t, out.String(),
		"\n  start it later  go run ./cmd/new-module -start demo -dir "+shelfIn("..")+"\n")
	require.NotContains(t, out.String(), "Next, copy these")
}

// TestStartingLaterReadsTheModuleFromItsFolder. -start takes the name from
// module.yaml and installs the module first when that core has not got it.
func TestStartingLaterReadsTheModuleFromItsFolder(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		kind     string
		into     target
		ui       bool
		edit     string
		password bool
	}{
		"a UI in the demo":          {"ui", targetDemo, true, filepath.Join("ui", "src", "page.svelte"), true},
		"a process in the real app": {"process", targetApp, false, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env, dir := written(t, c.kind)
			env, seen := faking(env, "", nil)

			var out bytes.Buffer

			require.NoError(t, run(t.Context(), &out, nil, env,
				[]string{"-start", string(c.into), "-dir", filepath.Join("..", "shelf")}))

			require.Equal(t, []target{c.into}, seen.installs, "a core that has not got it installs it first")
			require.NotNil(t, seen.launched)
			require.Equal(t, "shelf", seen.launched.name)
			require.Equal(t, dir, seen.launched.dir)
			require.Equal(t, c.ui, seen.launched.ui)
			require.Equal(t, c.edit, seen.launched.edit)
			require.Equal(t, c.password, seen.launched.password != "")
			require.NotZero(t, seen.launched.core)
		})
	}
}

// TestACoreThatHasTheModuleIsNotInstalledAgain. The install rebuilds
// everything, and a core already declaring the module does not need it.
func TestACoreThatHasTheModuleIsNotInstalledAgain(t *testing.T) {
	t.Parallel()

	env, _ := written(t, "ui")
	env, seen := faking(env, "", nil)

	overlay := filepath.Join(env.workDir, "bin", "shelf-demo", "demo", "resources.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(overlay), 0o750))
	require.NoError(t, os.WriteFile(overlay, []byte(`{"asset":{"shelf":{}}}`), 0o600))

	require.NoError(t, run(t.Context(), io.Discard, nil, env,
		[]string{"-start", "demo", "-dir", filepath.Join("..", "shelf")}))
	require.Empty(t, seen.installs)
	require.NotNil(t, seen.launched)
}

// TestStartTakesOnlyDemoOrAppAndAFolder.
func TestStartTakesOnlyDemoOrAppAndAFolder(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{
		"another word": {"-start", "prod", "-dir", "shelf"},
		"no folder":    {"-start", "demo"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := run(t.Context(), io.Discard, nil, here(t), args)
			require.ErrorIs(t, err, errUsage)
		})
	}
}

// TestTheRunningBlockSaysWhereToLookAndHowToSignIn. It is the one thing the
// launch prints, so every line of it is read.
func TestTheRunningBlockSaysWhereToLookAndHowToSignIn(t *testing.T) {
	t.Parallel()

	workDir := filepath.FromSlash("/home/you/src/prusa-connect")
	dir := filepath.FromSlash("/home/you/src/shelf")
	logs := filepath.Join(".", "bin", "shelf-demo")

	for name, c := range map[string]struct {
		l    launch
		want string
	}{
		"a UI in the demo": {
			launch{into: targetDemo, name: "shelf", dir: dir, checkout: workDir, workDir: workDir, ui: true,
				edit: filepath.Join("ui", "src", "page.svelte"), core: 8081, vite: 5174, password: "s3cret"},
			"✔ running  http://localhost:5174   (demo · sign in as demo / s3cret)\n" +
				"  edit " + filepath.Join("..", "shelf", "ui", "src", "page.svelte") +
				", the page updates as you save\n" +
				"  Ctrl+C stops both\n" +
				"  logs " + "." + string(filepath.Separator) + filepath.Join(logs, "core.log") + ", " +
				"." + string(filepath.Separator) + filepath.Join(logs, "vite.log") + "\n",
		},
		"a process in the real app": {
			launch{into: targetApp, name: "shelf", dir: dir, checkout: workDir, workDir: workDir, core: 8080},
			"✔ running  http://127.0.0.1:8080   (real app · first run asks you to set up an account)\n" +
				"  Ctrl+C stops it\n" +
				"  logs ." + string(filepath.Separator) + filepath.Join("bin", "shelf-app", "core.log") + "\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, c.want, runningBlock(c.l, false))
		})
	}
}

// TestThePageLineNamesAFileTheModuleHas. A module with no page is edited in
// the component it does have.
func TestThePageLineNamesAFileTheModuleHas(t *testing.T) {
	t.Parallel()

	_, dir := written(t, "ui")
	require.Equal(t, filepath.Join("ui", "src", "page.svelte"), editedIn(dir))

	require.NoError(t, os.Remove(filepath.Join(dir, "ui", "src", "page.svelte")))
	require.Equal(t, filepath.Join("ui", "src", "index.ts"), editedIn(dir))
}
