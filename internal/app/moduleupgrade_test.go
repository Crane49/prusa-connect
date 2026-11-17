package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// An update with a real child process: the public fixture's first release is
// installed and runs, then its second release is installed over it. The first
// process has to be gone before the second serves. The other upgrade
// cases use a stand-in for the plane; this one runs the plane and two builds
// of a module that answers plugin.info with the version it was built as.

// upgradeChildSource is that module: a process half and nothing else, which
// writes its pid where the test told it to once it is serving.
const upgradeChildSource = `package main

import (
	"context"
	"os"
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

var (
	name    = "spools"
	version = "0.0.0"
	pidFile = ""
)

type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }

func main() {
	line, err := wire.Handshake{
		Protocol: wire.ProtocolMajor, MinMinor: wire.MinMinor, MaxMinor: wire.MaxMinor,
		Address: wire.StdioAddress,
	}.Line()
	if err != nil {
		os.Exit(1)
	}
	_, _ = os.Stdout.WriteString(line)

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{Role: wirerpc.RoleChild, Methods: wirerpc.Methods{
		wirerpc.MethodInfo: func(context.Context, wirerpc.Call) (any, error) {
			return wirerpc.Info{Name: name, Version: extension.Version(version), Contract: wirerpc.Contract}, nil
		},
		wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) { return nil, nil },
	}})
	if err != nil {
		os.Exit(1)
	}

	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
	_ = conn.Serve(context.Background())
}
`

// processOnlyManifest is the module the child is: a process and no bundle.
func processOnlyManifest(name, version string) string {
	return strings.Join([]string{
		"name: " + name,
		"kind: sink",
		"license: MIT",
		"version: " + version,
		"contract_major: 1",
		"platforms:",
		"  - " + runtime.GOOS + "_" + runtime.GOARCH,
		"",
	}, "\n")
}

// upgradeRelease builds the child as one version into a release directory.
func upgradeRelease(t *testing.T, version, pidFile string) string {
	return upgradeReleaseNamed(t, "spools", version, pidFile)
}

func upgradeReleaseNamed(t *testing.T, name, version, pidFile string) string {
	t.Helper()

	toolchain, err := exec.LookPath("go")
	require.NoError(t, err)

	sdk, err := filepath.Abs(filepath.Join("..", "..", "sdk"))
	require.NoError(t, err)

	source := filepath.Join(t.TempDir(), "child")
	require.NoError(t, os.MkdirAll(source, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(source, "main.go"), []byte(upgradeChildSource), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(source, "go.mod"), []byte(strings.Join([]string{
		"module upgradechild",
		"",
		"go 1.26.11",
		"",
		"require github.com/prusa3d/prusa-connect/sdk v0.0.0",
		"",
		"replace github.com/prusa3d/prusa-connect/sdk => " + filepath.ToSlash(sdk),
		"",
	}, "\n")), 0o600))

	release := filepath.Join(t.TempDir(), "release-"+version)
	require.NoError(t, os.MkdirAll(release, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(release, manifestName),
		[]byte(processOnlyManifest(name, version)),
		0o600,
	))

	build := exec.Command(toolchain, "build",
		"-ldflags", "-X main.name="+name+" -X main.version="+version+" -X main.pidFile="+pidFile,
		"-o", filepath.Join(release, platformBinary(name)), ".")
	build.Dir = source
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")

	said, err := build.CombinedOutput()
	require.NoError(t, err, string(said))

	return release
}

func platformBinary(name string) string {
	file := name + "_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		file += ".exe"
	}

	return file
}

// pidIn reads the pid a child wrote, once it has written one.
func pidIn(t *testing.T, file string) int {
	t.Helper()

	var pid int

	require.Eventually(t, func() bool {
		body, err := os.ReadFile(file)
		if err != nil {
			return false
		}

		pid, err = strconv.Atoi(strings.TrimSpace(string(body)))

		return err == nil && pid > 0
	}, 30*time.Second, 50*time.Millisecond, "the module never wrote its pid, so it never served")

	return pid
}

type syntheticExtensionReleases struct {
	Module string `json:"module"`
	From   string `json:"from"`
	To     string `json:"to"`
}

func upgradeFixtureReleases(t *testing.T) syntheticExtensionReleases {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "..", "test", "upgrade", "releases.json"))
	require.NoError(t, err)

	var fixture struct {
		Releases syntheticExtensionReleases `json:"synthetic_extension_releases"`
	}
	require.NoError(t, json.Unmarshal(body, &fixture))
	require.Equal(t, theModulePath, fixture.Releases.Module)
	require.NotEmpty(t, fixture.Releases.From)
	require.NotEmpty(t, fixture.Releases.To)

	return fixture.Releases
}

func TestAnUpdateStopsTheOldProcessAndRunsTheNewOne(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal 0 does not tell a reaped process on Windows")
	}

	if testing.Short() {
		t.Skip("builds two module binaries")
	}

	t.Parallel()

	releases := upgradeFixtureReleases(t)
	pids := t.TempDir()
	first := upgradeRelease(t, releases.From, filepath.Join(pids, releases.From))
	second := upgradeRelease(t, releases.To, filepath.Join(pids, releases.To))

	deployment := aLiveDeployment(t)
	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	installed, err := deployment.installer.Install(t.Context(), deployment.principal, httpapi.ModuleRelease{
		Module: releases.Module, Version: releases.From, From: first, Approve: true,
	})
	require.NoError(t, err)
	require.True(t, installed.Running, "release %s did not start", releases.From)

	old := pidIn(t, filepath.Join(pids, releases.From))
	require.False(t, gone(old), "release %s exited on its own", releases.From)

	updated, err := deployment.installer.Install(t.Context(), deployment.principal, httpapi.ModuleRelease{
		Module: releases.Module, Version: releases.To, From: second, Approve: true,
	})
	require.NoError(t, err)
	require.Equal(t, releases.To, updated.Version)

	current := pidIn(t, filepath.Join(pids, releases.To))
	require.NotEqual(t, old, current)

	require.Eventually(t, func() bool { return gone(old) }, 10*time.Second, 50*time.Millisecond,
		"the process of release %s is still there after the update", releases.From)
	require.False(t, gone(current), "the process of release %s is not running", releases.To)

	item, found := deployment.listed(t, instance)
	require.True(t, found)
	require.NotNil(t, item.Module)
	require.Equal(t, releases.To, item.Module.Version)

	require.NoError(t, deployment.installer.Remove(t.Context(), deployment.principal, theNamespace, false))
	require.Eventually(t, func() bool { return gone(current) }, 10*time.Second, 50*time.Millisecond,
		"removing the module left its process running")
}
