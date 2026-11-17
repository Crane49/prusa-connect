package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheCoreImportsNo3MF is ADR-20261028-jae rule 7: the 3MF reader is the
// threemf extension's, and the core reads only a project's picture, with
// code of its own. A test file counts, as for the Fractal gate.
func TestTheCoreImportsNo3MF(t *testing.T) {
	t.Parallel()

	graph := packageGraph(t)

	floors := map[string]int{"internal": 30, "cmd": 10}
	scanned := map[string]int{}

	var violations []string

	// The sdk is a module of its own, which the graph does not walk, so its
	// folder is asked directly.
	if _, err := os.Stat(filepath.Join("..", "..", "sdk", "threemf")); err == nil {
		violations = append(violations, "sdk/threemf is a 3MF package of the sdk")
	}

	for _, pkg := range graph.packages {
		for root := range floors {
			if !underPrefix(pkg, root) {
				continue
			}

			scanned[root]++

			if strings.Contains(pkg, "threemf") {
				violations = append(violations, rel(pkg)+" is a 3MF package of the core")
			}

			for _, imported := range graph.importsOf(pkg) {
				if strings.Contains(imported, "threemf") {
					violations = append(violations, fmt.Sprintf("%s imports %s", rel(pkg), rel(imported)))
				}
			}
		}
	}

	for _, root := range sortedKeys(floors) {
		requireScanned(t, "packages under "+root+"/", scanned[root], floors[root],
			"The walk reached too few of them, so too little was asked whether it imports a 3MF package.")
	}

	report(
		t,
		violations,
		"ADR-20261028-jae rule 7, docs/decisions/20261028-jae-3mf-projects-are-read-by-the-threemf-extension-and-the-core.md",
	)
}
