// The tools a module needs, checked before the questions.

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// errTools means a tool the module needs is missing or too old.
var errTools = errors.New("this module needs tools that are missing or too old")

// need is one tool: its name, the oldest version that works and how to get
// it. An empty oldest skips the version check: pnpm switches to a project's pin.
type need struct{ tool, oldest, get string }

// A UI is built with pnpm on Node. A process is built with Go.
//
//nolint:gochecknoglobals // a closed table, read and never written.
var (
	uiNeeds = []need{
		{"node", oldestNode, "Install Node.js " + oldestNode + " or newer from https://nodejs.org/en/download."},
		{"pnpm", "", "Run corepack enable pnpm, or npm install --global pnpm."},
	}
	processNeeds = []need{
		{"go", publishedGo, "Install Go " + publishedGo + " or newer from https://go.dev/dl/."},
	}
)

// checkTools fails when a needed tool is missing or too old. The error names
// every such tool and how to get it.
func checkTools(ctx context.Context, s shape, env environment) error {
	missing := missingTools(ctx, s, env.version)
	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf("%w:\n  - %s", errTools, strings.Join(missing, "\n  - "))
}

// missingTools returns one line per tool that is missing or too old.
func missingTools(ctx context.Context, s shape, version func(context.Context, string) (string, error)) []string {
	var needs []need
	if s.draws() {
		needs = append(needs, uiNeeds...)
	}

	if s.runs() {
		needs = append(needs, processNeeds...)
	}

	var missing []string

	for _, n := range needs {
		said, err := version(ctx, n.tool)
		if err != nil {
			missing = append(missing, n.tool+" is not installed. "+n.get)

			continue
		}

		if n.oldest == "" {
			continue
		}

		have, parsed := versionOf(said)
		if parsed && older(have, n.oldest) {
			missing = append(missing, fmt.Sprintf("%s is %s, older than %s. %s", n.tool, have, n.oldest, n.get))
		}
	}

	return missing
}

// versionOf extracts the version from a tool's output: the last word, minus
// a leading "go" or "v". It reports false unless that is three numbers.
func versionOf(said string) (string, bool) {
	words := strings.Fields(said)
	if len(words) == 0 {
		return "", false
	}

	version := strings.TrimPrefix(strings.TrimPrefix(words[len(words)-1], "go"), "v")
	if _, _, _, err := extension.Version(version).Parse(); err != nil {
		return "", false
	}

	return version, true
}

// older reports whether have is before oldest. Both must already parse.
func older(have, oldest string) bool {
	a, b, c, _ := extension.Version(have).Parse()
	x, y, z, _ := extension.Version(oldest).Parse()

	return slices.Compare([]int{a, b, c}, []int{x, y, z}) < 0
}

// toolVersion asks tool for its version. Only stdout is read: pnpm prints its
// warnings about ~/.npmrc on stderr.
func toolVersion(ctx context.Context, tool string) (string, error) {
	args := []string{"--version"}
	if tool == "go" {
		args = []string{"env", "GOVERSION"}
	}

	said, err := exec.CommandContext(ctx, tool, args...).Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", tool, err)
	}

	return strings.TrimSpace(string(said)), nil
}
