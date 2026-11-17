package arch_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// prometheusClient is the module a metric ends up in whatever it is called.
const prometheusClient = "github.com/prometheus/client_golang"

// metricsSink is the one package allowed to reach it.
const metricsSink = "internal/platform/observability/prom"

// readinessVerdict is the file that decides whether this process serves
// traffic.
const readinessVerdict = "internal/app/readiness.go"

// connectorHealth is the rollup that must never reach that decision.
const connectorHealth = modulePath + "/internal/platform/observability/health"

// TestOnlyOnePackageImportsAPrometheusClient makes the fan-out rule
// enforceable rather than advisory.
//
// The rule is that a fan-out label such as device_id never lands on a
// histogram, because a bucketed metric costs one series per device per bucket
// where a counter costs one per device. A rule like that cannot be held by
// review: it is one field in one options struct, and it looks harmless in a
// diff.
//
// It is held instead by making the metrics package the only door. Every series
// this process exposes is built from a validated definition, and the
// definition type has no parameter that accepts a fan-out label on a
// histogram. A second package constructing collectors of its own would walk
// around all of it, so that is what this gate refuses.
func TestOnlyOnePackageImportsAPrometheusClient(t *testing.T) {
	t.Parallel()

	graph := packageGraph(t)

	var violations []string

	for _, pkg := range graph.packages {
		if !isFirstParty(pkg) || underPrefix(pkg, metricsSink) {
			continue
		}

		for _, imported := range graph.importsOf(pkg) {
			if !strings.HasPrefix(imported, prometheusClient) {
				continue
			}

			violations = append(violations, fmt.Sprintf("%s imports %s", rel(pkg), imported))
		}
	}

	if len(violations) == 0 {
		return
	}

	t.Errorf("%d package(s) build metrics outside %s, which is where the cardinality rule\n"+
		"lives. A collector constructed anywhere else can put a per-printer label on a\n"+
		"histogram, which is an earlier design decision and 5,600 series for one metric at 400 printers.\n"+
		"Define the metric in internal/platform/observability and register it through the\n"+
		"registry:\n\t%s",
		len(violations), metricsSink, strings.Join(violations, "\n\t"))
}

// TestReadinessDoesNotDependOnConnectorHealth is an earlier design decision and requirement
// 28 held at the import graph.
//
// A printer being unplugged is its steady state, not a fault. A readiness
// probe that fails because printers are offline takes a healthy process out of
// rotation for the one condition that is normal operations, and under an
// orchestrator it restarts the process in a loop. The server is ready to serve
// its API with zero printers connected.
//
// The rollup and the verdict therefore stay apart: the verdict reads declared
// resources and their required flag, and the rollup is a report. Connector
// health reaches an operator through the health endpoint and
// pcl_printer_up{device_id}, never through the probe a load balancer routes on.
func TestReadinessDoesNotDependOnConnectorHealth(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	path := filepath.Join(root, filepath.FromSlash(readinessVerdict))

	file, err := parser.ParseFile(token.NewFileSet(), path, nil,
		parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v.\nIf the readiness verdict moved, point this gate at it rather than "+
			"deleting it: the rule it holds did not move.", readinessVerdict, err)
	}

	for _, imported := range importPaths(file.Imports) {
		if imported == connectorHealth || strings.HasPrefix(imported, connectorHealth+"/") {
			t.Errorf("%s imports %s.\nA connector rollup in the readiness verdict makes an offline "+
				"printer a reason to refuse API traffic, which is the cascading readiness failure "+
				"an earlier design decision exists to prevent.", readinessVerdict, rel(imported))
		}
	}
}
