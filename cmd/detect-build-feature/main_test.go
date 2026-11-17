package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/build"
)

// repoPath addresses a file in the checkout from this package's directory.
func repoPath(parts ...string) string {
	return filepath.Join(append([]string{"..", ".."}, parts...)...)
}

// TestTheShippedConfigurationSwitchesOnTheStepsItDeclares runs the detector
// against the file `make build` actually passes it, rather than against a
// fixture. A table of invented configurations agrees with every implementation,
// including one that has stopped reading the real file's keys.
func TestTheShippedConfigurationSwitchesOnTheStepsItDeclares(t *testing.T) {
	t.Parallel()

	shipped := repoPath("config", "config.yaml")

	enabled, err := detect(shipped, "frontend")
	require.NoError(t, err)
	require.True(t, enabled, "config/config.yaml declares the app and the detector says it is off")
}

// TestAConfigurationDecidesTheApp pins the defaulting rule, which is the whole
// of this command: a file that says anything at all builds the app unless it
// switches it off, because that is how the running server reads the key.
func TestAConfigurationDecidesTheApp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		content  string
		frontend bool
	}{
		{name: "an empty configuration builds no optional step", content: ""},
		{name: "a configuration that names no key builds the app", content: "core:\n  site: local\n", frontend: true},
		{name: "the app switched off", content: "core:\n  frontend:\n    enabled: false\n"},
		{
			name:     "the app's other keys do not switch it off",
			content:  "core:\n  frontend:\n    title: Workshop\n    theme: dark\n",
			frontend: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(testCase.content), 0o600))

			enabled, err := detect(path, "frontend")
			require.NoError(t, err)
			require.Equal(t, testCase.frontend, enabled)
		})
	}
}

// TestARetiredKeyStopsTheBuildAndNamesItsReplacement is why the two old keys
// are still decoded. A file that says server.api.frontend: true would otherwise
// build no app and print nothing, and the operator would find out when the
// origin root answered 404.
func TestARetiredKeyStopsTheBuildAndNamesItsReplacement(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		content string
		says    string
	}{
		"the retired frontend key": {
			content: "server:\n  api:\n    frontend: true\n",
			says:    "core.frontend.enabled",
		},
		"the retired api key": {
			content: "server:\n  api:\n    enabled: false\n",
			says:    "core.frontend.enabled",
		},
		"the retired documentation key": {
			content: "documentation:\n  enabled: true\n",
			says:    "make docs",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(testCase.content), 0o600))

			_, err := detect(path, "frontend")
			require.ErrorIs(t, err, build.ErrRetiredKey)
			require.Contains(t, err.Error(), testCase.says)
		})
	}
}

// TestAConfigurationNobodyWroteYetBuildsNoOptionalStep matches detect-build-tags:
// a checkout that has not been configured builds the mandatory parts rather than
// refusing to build at all.
func TestAConfigurationNobodyWroteYetBuildsNoOptionalStep(t *testing.T) {
	t.Parallel()

	absent := filepath.Join(t.TempDir(), "nobody-wrote-this.yaml")

	enabled, err := detect(absent, "frontend")
	require.NoError(t, err)
	require.False(t, enabled)
}

// TestAFieldThisCommandDoesNotServeIsRefused keeps a typo in the Makefile from
// reading as a step that is switched off.
func TestAFieldThisCommandDoesNotServeIsRefused(t *testing.T) {
	t.Parallel()

	shipped := repoPath("config", "config.yaml")

	_, err := detect(shipped, "")
	require.ErrorContains(t, err, "--field is required")

	_, err = detect(shipped, "fronted")
	require.ErrorContains(t, err, `unknown build step "fronted"`)

	_, err = detect(shipped, "docs")
	require.ErrorContains(t, err, `unknown build step "docs"`)
}

// TestAConfigurationThatWillNotParseIsRefused keeps a broken file from reading
// as one that asked for nothing.
func TestAConfigurationThatWillNotParseIsRefused(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("core:\n\tfrontend: true\n"), 0o600))

	_, err := detect(path, "frontend")
	require.ErrorContains(t, err, "config.yaml")
}

// TestTheOutputIsTheWordTheMakefileComparesAgainst is the contract with
// `build-config`, which compares this command's whole stdout against the string
// "true" inside a shell test. A trailing newline breaks the build silently by
// skipping every optional step.
func TestTheOutputIsTheWordTheMakefileComparesAgainst(t *testing.T) {
	t.Parallel()

	require.Equal(t, "true", stdoutOf(t, "--config", repoPath("config", "config.yaml"), "--field", "frontend"))

	absent := filepath.Join(t.TempDir(), "absent.yaml")
	require.Equal(t, "false", stdoutOf(t, "--config", absent, "--field", "frontend"))
}

// stdoutOf runs the command and returns what it wrote, failing on a non-zero
// exit.
func stdoutOf(t *testing.T, args ...string) string {
	t.Helper()

	out, err := os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)

	defer func() { _ = out.Close() }()

	require.Equal(t, exitOK, run(args, out, os.Stderr))

	written, err := os.ReadFile(out.Name())
	require.NoError(t, err)

	return string(written)
}
