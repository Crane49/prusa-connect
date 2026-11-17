package main_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The module every path below is under.
const module = "github.com/prusa3d/prusa-connect"

// linkedAdapters is what an operator's resources block can name in the shipping
// artefact. The list is written out rather than read from the composition root,
// because a gate that asks the binary what it carries and then agrees with the
// answer passes against a binary that carries nothing, which is the defect this
// file exists for.
//
// An adapter added to internal/app/builtin belongs here in the same change.
//
//nolint:gochecknoglobals // the expectation this file is written against.
var linkedAdapters = []string{
	module + "/adapters/auth/oidc",
	module + "/adapters/connector/echo",
	module + "/adapters/events/amqp",
	module + "/adapters/events/kafka",
	module + "/adapters/events/mqtt",
	module + "/adapters/events/webhook",
	// diskstore registers no name and is reached by the built-in filesystem
	// type directly. Linked, declarable, no blank import.
	module + "/adapters/storage/diskstore",
	module + "/adapters/storage/local",
	module + "/adapters/storage/temporary",
}

// unlinkedAdapters is what this artefact deliberately leaves out, so leaving it
// out stays a decision rather than becoming an accident somebody reverses
// without noticing.
//
// It is empty. The two storage adapters were held out while the retired server
// registered core/local and core/temporary with sdk/storageapi, because a second
// registration of one qualified name panics before main. That server is deleted,
// so the names are the adapters' and both are linked above. An adapter held out
// again takes an entry here with the reason.
//
//nolint:gochecknoglobals // the expectation this file is written against.
var unlinkedAdapters = map[string]string{}

// TestTheShippingArtefactLinksTheAdaptersItPublishes asks the linker's own view
// rather than reading an import, so an adapter reached indirectly counts and a
// composition root that stopped importing the package that carries the blank
// imports fails here.
func TestTheShippingArtefactLinksTheAdaptersItPublishes(t *testing.T) {
	t.Parallel()

	// The three builds this gate covers: the default, the minimal variant, and
	// the full variant carrying the tags config/config.yaml actually selects.
	// The last is what make build produces, so an adapter put behind a build
	// tag is answered for here rather than in a release.
	for _, build := range []struct{ name, tags string }{
		{name: "untagged"},
		{name: "minimal", tags: "minimal"},
		{name: "full", tags: shippingTags(t)},
	} {
		t.Run(build.name, func(t *testing.T) {
			t.Parallel()

			linked := dependencies(t, build.tags)

			for _, adapter := range linkedAdapters {
				if !slices.Contains(linked, adapter) {
					t.Errorf("%s is not linked, so an operator declaring it is answered that the type "+
						"is unknown. The blank import is in internal/app/builtin/adapters.go", adapter)
				}
			}

			for adapter, why := range unlinkedAdapters {
				if slices.Contains(linked, adapter) {
					t.Errorf("%s is linked and this artefact is meant to leave it out: %s", adapter, why)
				}
			}
		})
	}
}

// dependencies is every package a build of the server under these tags reaches.
func dependencies(t *testing.T, tags string) []string {
	t.Helper()

	args := []string{"list", "-deps"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}

	args = append(args, "./cmd/prusa-connect")

	listed := strings.Fields(runInRoot(t, "go", args...))
	if len(listed) == 0 {
		t.Fatal("go list -deps returned no package, so this gate compared against an empty set")
	}

	return listed
}

// shippingTags is the tag set config/config.yaml selects for the full variant.
// It is asked of cmd/detect-build-tags rather than spelled here, so this gate
// follows the build it is about instead of a copy of it that can go stale.
func shippingTags(t *testing.T) string {
	t.Helper()

	detected := runInRoot(t, "go", "run", "./cmd/detect-build-tags",
		"--config", "config/config.yaml", "--variant", "full")

	return strings.TrimSpace("full " + strings.TrimSpace(detected))
}

// runInRoot runs one command from the repository root and returns its stdout.
// A failure reports what the command printed, because exec.ExitError's own
// message is only the exit status.
func runInRoot(t *testing.T, name string, args ...string) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("locating the repository root: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, exitErr.Stderr)
		}

		t.Fatalf("%s %s: %v", name, strings.Join(args, " "), err)
	}

	return string(out)
}
