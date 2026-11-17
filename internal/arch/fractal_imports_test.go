package arch_test

import (
	"fmt"
	"testing"
)

// TestTheCoreImportsNoFractal is ADR-20261020-tca rule 2: the core runs the same with
// the Fractal module disabled. A test file counts, because a core test that
// needs the protocol is a core that needs it.
func TestTheCoreImportsNoFractal(t *testing.T) {
	t.Parallel()

	graph := packageGraph(t)

	floors := map[string]int{"internal": 30, "adapters": 3, "cmd": 10, "compose": 1}
	scanned := map[string]int{}

	var violations []string

	for _, pkg := range graph.packages {
		for root := range floors {
			if !underPrefix(pkg, root) {
				continue
			}

			scanned[root]++

			for _, imported := range graph.importsOf(pkg) {
				if underPrefix(imported, "sdk/fractal") {
					violations = append(violations, fmt.Sprintf("%s imports %s", rel(pkg), rel(imported)))
				}
			}
		}
	}

	for _, root := range sortedKeys(floors) {
		requireScanned(t, "packages under "+root+"/", scanned[root], floors[root],
			"The walk reached too few of them, so too little was asked whether it imports sdk/fractal.")
	}

	report(t, violations,
		"ADR-20261020-tca rule 2, docs/decisions/20261020-tca-a-fractal-node-keeps-a-shadow-of-the-nodes-above-it.md")
}
