package arch_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The two HTTP surface captures, the old backlog. Each fixture is a
// transcript of calls made against a running retired server, and this gate
// holds the transcript against what that server publishes.
//
// The comparison is over operations, meaning a method and a path template. An
// operation the fixture never called is a behaviour nobody wrote down, and a
// row naming an operation the server does not publish is a row invented after
// the fact. The gate fails on either.
//
// What it cannot catch is written down so the next reader does not infer it. A
// status or a body shape in the fixture is checked for form and never against
// the server, because the server is what the fixture replaces. Re-capturing is
// the only way to refresh those two columns, and the header of each file says
// how.
//
// The document is a capture too, under testdata/ here rather than under
// legacy/. Both sides are this package's own files, so the gate outlives the
// code it describes and no longer skips on an absent one.
const legacyPublishedOperations = "internal/arch/testdata/retired-openapi3.json"

// httpSurface is one capture's slice of the published surface. A matcher
// beginning with = is an exact path and anything else is a prefix, so
// /api/printers can be claimed without claiming everything under it.
type httpSurface struct {
	matchers []string
	floor    int
}

//nolint:gochecknoglobals // the two surfaces this page holds, read by one test.
var legacyHTTPSurfaces = map[string]httpSurface{
	"legacy-account-surface.tsv": {
		matchers: []string{"/setup", "/auth/", "/api/account", "/api/tokens", "/api/admin/"},
		floor:    18,
	},
	"legacy-label-surface.tsv": {
		matchers: []string{"/api/labels", "/api/printers/{id}/labels", "=/api/printers"},
		floor:    7,
	},
}

// TestEveryPublishedSurfaceOperationIsCaptured is the divergence test. It fails
// in both directions: an operation the retired server publishes and the
// transcript never called, and a transcript row naming an operation it does not
// publish.
func TestEveryPublishedSurfaceOperationIsCaptured(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	published := publishedOperations(t, filepath.Join(root, legacyPublishedOperations))

	require.NotEmpty(t, published, "%s published no operation, so this gate compares two empty sets",
		legacyPublishedOperations)

	for name, surface := range legacyHTTPSurfaces {
		want := selectOperations(published, surface.matchers)

		require.GreaterOrEqualf(t, len(want), surface.floor,
			"%s claims %d published operations and the floor is %d",
			name, len(want), surface.floor)

		require.Equalf(t, want, capturedOperations(t, name),
			"every operation %s publishes under %v is a row in %s/%s, and every row is one it publishes",
			legacyPublishedOperations, surface.matchers, captureDirectory, name)
	}
}

// TestEveryCapturedCaseIsNamedOnce refuses two rows with one case name. A
// duplicate name is a row that was meant to be a second case and became a
// second copy of the first.
func TestEveryCapturedCaseIsNamedOnce(t *testing.T) {
	t.Parallel()

	for name := range legacyHTTPSurfaces {
		seen := map[string]bool{}

		for _, row := range captureRows(t, name) {
			if seen[row[0]] {
				t.Errorf("%s names the case %s twice, so one of the two records nothing new", name, row[0])
			}

			seen[row[0]] = true
		}
	}
}

// publishedOperations is every method and path the retired server's generated
// document declares, as "METHOD path".
func publishedOperations(t *testing.T, document string) []string {
	t.Helper()

	content, err := os.ReadFile(filepath.Clean(document))
	if err != nil {
		t.Fatalf("read %s: %v", document, err)
	}

	var declared struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}

	if err = json.Unmarshal(content, &declared); err != nil {
		t.Fatalf("parse %s: %v", document, err)
	}

	methods := map[string]bool{"get": true, "post": true, "put": true, "delete": true, "patch": true}

	var operations []string

	for path, byMethod := range declared.Paths {
		for method := range byMethod {
			if methods[method] {
				operations = append(operations, strings.ToUpper(method)+" "+path)
			}
		}
	}

	sort.Strings(operations)

	return operations
}

// selectOperations is the slice of the published surface one capture claims.
func selectOperations(published, matchers []string) []string {
	var chosen []string

	for _, operation := range published {
		_, path, _ := strings.Cut(operation, " ")

		for _, matcher := range matchers {
			exact, found := strings.CutPrefix(matcher, "=")
			if (found && path == exact) || (!found && strings.HasPrefix(path, matcher)) {
				chosen = append(chosen, operation)

				break
			}
		}
	}

	sort.Strings(chosen)

	return chosen
}

// capturedOperations is every distinct operation one transcript called. A path
// naming {unknown} is the same operation as one naming {id}: the transcript
// spells them apart so a reader can tell the two cases, and the server routes
// them the same way.
func capturedOperations(t *testing.T, name string) []string {
	t.Helper()

	seen := map[string]bool{}

	for _, row := range captureRows(t, name) {
		seen[row[2]+" "+strings.ReplaceAll(row[3], "{unknown}", "{id}")] = true
	}

	operations := make([]string, 0, len(seen))
	for operation := range seen {
		operations = append(operations, operation)
	}

	sort.Strings(operations)

	return operations
}
