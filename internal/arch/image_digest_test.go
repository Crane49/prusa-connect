package arch_test

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fromLine reads the image of a Dockerfile FROM instruction. The optional
// platform flag and the stage name are dropped.
var fromLine = regexp.MustCompile(`(?i)^FROM\s+(?:--platform=\S+\s+)?(\S+)`)

// imagePinViolations returns every FROM in one Dockerfile that names a
// registry image without a digest. A tag built from a build argument still
// needs one. scratch is exempt because it has no digest. A stage an earlier
// FROM named with AS is exempt, because it is built in the same file.
func imagePinViolations(body []byte) []string {
	var (
		violations []string
		stages     = map[string]bool{}
	)

	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		match := fromLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		image := match[1]

		if image != "scratch" && !stages[image] && !strings.Contains(image, "@sha256:") {
			violations = append(violations, line)
		}

		// The alias counts only for the FROM lines after this one.
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[len(fields)-2], "AS") {
			stages[fields[len(fields)-1]] = true
		}
	}

	return violations
}

// TestEveryImageIsPinnedByDigest holds the supply chain rule that a container
// image names one exact build. A floating tag lets two builds of one commit
// differ.
func TestEveryImageIsPinnedByDigest(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	out, err := exec.Command("git", "-C", root, "ls-files").Output()
	if err != nil {
		t.Fatalf("listing the tracked files: %v", err)
	}

	scanned := 0

	for path := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		base := filepath.Base(path)
		if !strings.HasPrefix(base, "Dockerfile") || strings.HasSuffix(base, ".dockerignore") {
			continue
		}

		body, readErr := os.ReadFile(filepath.Join(root, path))
		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}

		scanned++

		for _, line := range imagePinViolations(body) {
			t.Errorf("%s: %q names an image without a digest. Pin it as name:tag@sha256:<hex>.", path, line)
		}
	}

	requireScanned(t, "Dockerfiles", scanned, 5)
}

// TestTheDigestGateRefusesAFloatingTag proves the rule fires.
func TestTheDigestGateRefusesAFloatingTag(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		body    string
		refused bool
	}{
		"a pinned image":       {body: "FROM node:24-alpine@sha256:abc AS app\n", refused: false},
		"a floating tag":       {body: "FROM node:24-alpine AS app\n", refused: true},
		"a platform flag":      {body: "FROM --platform=linux/amd64 node:24-alpine\n", refused: true},
		"scratch":              {body: "FROM scratch AS binary\n", refused: false},
		"a tag from an arg":    {body: "FROM golang:${GO_VERSION}-trixie AS builder\n", refused: true},
		"a pinned arg tag":     {body: "FROM golang:${GO_VERSION}-trixie@sha256:abc AS builder\n", refused: false},
		"a whole arg image":    {body: "FROM ${BASE}\n", refused: true},
		"an alias of itself":   {body: "FROM alpine AS alpine\n", refused: true},
		"an earlier stage":     {body: "FROM node:1@sha256:abc AS base\nFROM base AS next\n", refused: false},
		"a lowercase from":     {body: "from node:24-alpine\n", refused: true},
		"a comment naming one": {body: "# FROM node:24-alpine\n", refused: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			found := imagePinViolations([]byte(testCase.body))
			if refused := len(found) > 0; refused != testCase.refused {
				t.Errorf("refused=%t, want %t. What it said: %v", refused, testCase.refused, found)
			}
		})
	}
}
