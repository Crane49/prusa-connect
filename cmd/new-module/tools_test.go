package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// allTools answers every tool with a version newer than any this needs.
func allTools(context.Context, string) (string, error) { return "99.0.0", nil }

// versions answers each tool with the version in the map, and a tool the map
// does not name as not installed.
func versions(held map[string]string) func(context.Context, string) (string, error) {
	return func(_ context.Context, tool string) (string, error) {
		said, found := held[tool]
		if !found {
			return "", errors.New("executable file not found in $PATH")
		}

		return said, nil
	}
}

// TestEachKindIsCheckedForTheToolsItsHalvesNeed. A UI-only author is never
// asked for Go, and a process-only author never for Node.
func TestEachKindIsCheckedForTheToolsItsHalvesNeed(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		kind  shape
		tools map[string]string
		want  []string
	}{
		"UI only with everything": {
			shapeUI, map[string]string{"node": "v24.19.0", "pnpm": "11.3.0"}, nil,
		},
		"UI only never asks for Go": {
			shapeUI, map[string]string{"node": "v24.19.0", "pnpm": "11.3.0"}, nil,
		},
		"UI only with no pnpm": {
			shapeUI, map[string]string{"node": "v24.19.0"},
			[]string{"pnpm is not installed. Run corepack enable pnpm, or npm install --global pnpm."},
		},
		"UI only with an old Node": {
			shapeUI, map[string]string{"node": "v20.11.0", "pnpm": "11.3.0"},
			[]string{"node is 20.11.0, older than 22.12.0. Install Node.js 22.12.0 or newer from https://nodejs.org/en/download."},
		},
		"UI only with an old pnpm is not refused": {
			// pnpm switches itself to the version ui/package.json pins, so a global
			// 10.x still builds a module correctly.
			shapeUI, map[string]string{"node": "v24.19.0", "pnpm": "10.28.2"}, nil,
		},
		"process only never asks for Node": {
			shapeProcess, map[string]string{"go": "go1.26.11"}, nil,
		},
		"process only with no Go": {
			shapeProcess, map[string]string{},
			[]string{"go is not installed. Install Go 1.26.11 or newer from https://go.dev/dl/."},
		},
		"a version that does not parse is not called old": {
			shapeProcess, map[string]string{"go": "devel go1.27-abcdef"}, nil,
		},
		"both with nothing": {
			shapeBoth, map[string]string{},
			[]string{
				"node is not installed. Install Node.js 22.12.0 or newer from https://nodejs.org/en/download.",
				"pnpm is not installed. Run corepack enable pnpm, or npm install --global pnpm.",
				"go is not installed. Install Go 1.26.11 or newer from https://go.dev/dl/.",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, c.want, missingTools(t.Context(), c.kind, versions(c.tools)))
		})
	}
}

// TestAGeneratedUIPinsThePnpmVersionTheCheckDoesNotEnforce. checkTools only
// asks that pnpm is installed, so the version that builds the module comes
// from here: pnpm switches itself to a project's own packageManager pin.
func TestAGeneratedUIPinsThePnpmVersionTheCheckDoesNotEnforce(t *testing.T) {
	t.Parallel()

	dir := scaffolded(t, "-name", "shelf")

	packaged, err := os.ReadFile(filepath.Join(dir, "ui", "package.json"))
	require.NoError(t, err)

	var held struct {
		PackageManager string `json:"packageManager"`
	}

	require.NoError(t, json.Unmarshal(packaged, &held))
	require.Equal(t, "pnpm@"+oldestPnpm, held.PackageManager)
}
