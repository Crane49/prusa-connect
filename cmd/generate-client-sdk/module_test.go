package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// The example module and the package generated from it, committed as the
// fixture the module mode is proved on.
const (
	exampleFragment    = "../../examples/service-plugin/openapi.json"
	exampleClient      = "../../examples/service-plugin/client"
	exampleNamespace   = "com.github.aux.acme.example"
	examplePackageName = "example-service-client"
)

func exampleModule(documentPath, outDir string, check bool) options {
	return options{
		documentPath: documentPath,
		versionPath:  committedVersion,
		module:       true,
		namespace:    exampleNamespace,
		packageName:  examplePackageName,
		outDir:       outDir,
		check:        check,
	}
}

// everyModuleFile is what one run writes, except package.json, which the
// fixture extends by hand and the -check gate holds by its peer range alone.
var everyModuleFile = []string{
	"src/generated/operations.ts",
	moduleDocumentName,
	moduleIndexPath,
	tsconfigName,
	readmeName,
}

// TestTheExampleModuleClientIsWhatTheFragmentGenerates is the fixture gate:
// the committed package under examples/service-plugin/client is what the
// module mode writes from the example's fragment today, generated and
// scaffold files alike.
func TestTheExampleModuleClientIsWhatTheFragmentGenerates(t *testing.T) {
	t.Parallel()

	if err := run(exampleModule(exampleFragment, exampleClient, true)); err != nil {
		t.Errorf("%v", err)
	}

	dir := t.TempDir()
	if err := run(exampleModule(exampleFragment, dir, false)); err != nil {
		t.Fatalf("generate into a fresh directory: %v", err)
	}

	for _, name := range everyModuleFile {
		want, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("the run did not write %s: %v", name, err)
		}

		got, err := os.ReadFile(filepath.Join(exampleClient, name))
		if err != nil {
			t.Fatalf("the fixture has no %s: %v", name, err)
		}

		if string(got) != string(want) {
			t.Errorf("%s differs from what the fragment generates. Delete it and rerun the module mode", name)
		}
	}
}

// TestTheModuleSkeletonPinsTheCoreTools holds the two versions the skeleton
// writes to the ones the core package pins, so a module package builds with
// the same compiler and renders its types with the same generator.
func TestTheModuleSkeletonPinsTheCoreTools(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{committedPackage, exampleClient} {
		file, err := readRegistry[struct {
			Dev map[string]string `json:"devDependencies"`
		}](filepath.Join(dir, manifestName))
		if err != nil {
			t.Fatal(err)
		}

		if got := file.Dev["openapi-typescript"]; got != openapiTypescriptVersion {
			t.Errorf("%s pins openapi-typescript %s and the skeleton writes %s", dir, got, openapiTypescriptVersion)
		}

		if got := file.Dev["typescript"]; got != typescriptVersion {
			t.Errorf("%s pins typescript %s and the skeleton writes %s", dir, got, typescriptVersion)
		}
	}
}

// servedForm is the example's fragment as the host serves it, with every path
// under the mount.
func servedForm(t *testing.T) []byte {
	t.Helper()

	raw, err := os.ReadFile(exampleFragment)
	if err != nil {
		t.Fatal(err)
	}

	fragment, err := routesapi.ParseFragment(raw)
	if err != nil {
		t.Fatal(err)
	}

	served, err := fragment.Rebase(mountOf(exampleNamespace))
	if err != nil {
		t.Fatal(err)
	}

	return served
}

// TestAServedDocumentGeneratesTheSamePackage: the document the host serves at
// GET /api/v1/modules/{namespace}/openapi.json, already under the mount, is
// read as the fragment it came from.
func TestAServedDocumentGeneratesTheSamePackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	served := filepath.Join(dir, "served.json")

	if err := os.WriteFile(served, servedForm(t), 0o600); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "client")
	if err := run(exampleModule(served, out, false)); err != nil {
		t.Fatalf("a served document refused: %v", err)
	}

	for _, name := range []string{"src/generated/operations.ts", moduleDocumentName} {
		got, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}

		want, err := os.ReadFile(filepath.Join(exampleClient, name))
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != string(want) {
			t.Errorf("%s generated from the served form differs from the one generated from the fragment", name)
		}
	}
}

func TestAMixedDocumentIsRefused(t *testing.T) {
	t.Parallel()

	mixed := `{"openapi": "3.1.0", "paths": {
		"/api/v1/modules/` + exampleNamespace + `/spools": {"get": {"operationId": "a", "summary": "s", "description": "d"}},
		"/state": {"get": {"operationId": "b", "summary": "s", "description": "d"}}
	}}`

	if _, err := rebaseFragment([]byte(mixed), exampleNamespace); !errors.Is(err, errMixedPaths) {
		t.Errorf("a mixed document reported %v, want errMixedPaths", err)
	}

	absolute := `{"openapi": "3.1.0", "paths": {"/api/v1/other": {"get": {"operationId": "a", "summary": "s", "description": "d"}}}}`

	if _, err := rebaseFragment([]byte(absolute), exampleNamespace); !errors.Is(err, routesapi.ErrFragment) {
		t.Errorf("a document under another mount reported %v, want ErrFragment", err)
	}
}

// TestAURLDocumentIsFetchedWithTheToken: a -document URL is read from the
// host's route with the bearer token, and the host's refusal is reported.
func TestAURLDocumentIsFetchedWithTheToken(t *testing.T) {
	t.Parallel()

	served := servedForm(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, `{"code": "core.auth/UNAUTHENTICATED"}`, http.StatusUnauthorized)

			return
		}

		w.Header().Set("Content-Type", "application/openapi+json")
		_, _ = w.Write(served)
	}))
	defer server.Close()

	url := server.URL + mountOf(exampleNamespace) + "/openapi.json"

	opts := exampleModule(url, t.TempDir(), false)
	opts.token = "secret"

	if err := run(opts); err != nil {
		t.Fatalf("fetching with the token refused: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(opts.outDir, "src/generated/operations.ts"))
	if err != nil {
		t.Fatal(err)
	}

	want, err := os.ReadFile(filepath.Join(exampleClient, "src/generated/operations.ts"))
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != string(want) {
		t.Error("the package generated from the fetched document differs from the fixture")
	}

	refused := exampleModule(url, t.TempDir(), false)
	refused.token = "wrong"

	err = run(refused)
	if !errors.Is(err, errFetch) || !strings.Contains(err.Error(), "401") {
		t.Errorf("a refused fetch reported %v, want errFetch naming the status", err)
	}
}

func TestModuleModeNeedsANamespaceAndAnOutput(t *testing.T) {
	t.Parallel()

	opts := exampleModule(exampleFragment, t.TempDir(), false)
	opts.namespace = ""

	if err := run(opts); !errors.Is(err, errNoNamespace) {
		t.Errorf("no namespace reported %v, want errNoNamespace", err)
	}

	opts = exampleModule(exampleFragment, "", false)
	if err := run(opts); !errors.Is(err, errNoOutDir) {
		t.Errorf("no output reported %v, want errNoOutDir", err)
	}

	opts = exampleModule(exampleFragment, t.TempDir(), false)
	opts.packageName = ""

	if err := completeModuleOptions(&opts); err != nil || opts.packageName != "prusa-connect-example" {
		t.Errorf("the default name is %q with %v, want prusa-connect-example", opts.packageName, err)
	}
}

// TestAPathItemParameterReachesTheOperation: the example fragment declares
// {key} once on the path item, as OpenAPI allows, and each operation under it
// takes the path member.
func TestAPathItemParameterReachesTheOperation(t *testing.T) {
	t.Parallel()

	doc, err := readDocument(filepath.Join(exampleClient, moduleDocumentName))
	if err != nil {
		t.Fatal(err)
	}

	ops, err := operationsOf(doc)
	if err != nil {
		t.Fatal(err)
	}

	byID := map[string]operation{}
	for _, op := range ops {
		byID[op.ID] = op
	}

	for _, id := range []string{"get-state", "put-state", "delete-state"} {
		op, ok := byID[id]
		if !ok {
			t.Fatalf("%s is not in the document", id)
		}

		if !slices.Equal(op.PathParams, []string{"key"}) {
			t.Errorf("%s takes path parameters %v, want [key]", id, op.PathParams)
		}

		if op.RequestOptional() {
			t.Errorf("%s reads as taking no request, and it takes a path parameter", id)
		}
	}

	put := byID["put-state"]
	if len(put.Query) != 1 || put.Query[0].Name != "version" {
		t.Errorf("put-state keeps its own query parameters as %v, want [version]", put.Query)
	}
}

// TestThePeerRangeIsStamped: only the peer range moves, and the link a
// checkout keeps under devDependencies under the same name is left alone
// whichever block comes first.
func TestThePeerRangeIsStamped(t *testing.T) {
	t.Parallel()

	manifest := "{\n  \"devDependencies\": {\n    \"@prusa/connect\": \"link:../core\"\n  },\n" +
		"  \"peerDependencies\": {\n    \"@prusa/connect\": \"0.0.0\"\n  }\n}\n"

	stamped, err := stampPeer([]byte(manifest), "1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	want := strings.Replace(manifest, "\"0.0.0\"", "\"1.2.3\"", 1)
	if string(stamped) != want {
		t.Errorf("stamped %q, want %q", stamped, want)
	}

	var decoded struct {
		Peer map[string]string `json:"peerDependencies"`
	}

	if err = json.Unmarshal(stamped, &decoded); err != nil {
		t.Fatalf("the stamped manifest does not parse: %v", err)
	}

	if _, err = stampPeer([]byte("{\"dependencies\": {}}"), "1.2.3"); !errors.Is(err, errNoPeer) {
		t.Errorf("a manifest with no peer reported %v, want errNoPeer", err)
	}
}

// TestADriftedModuleFileIsRefused is the -check gate on a module package
// whose operations file was edited by hand, and on one written once and
// checked unchanged.
func TestADriftedModuleFileIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := run(exampleModule(exampleFragment, dir, false)); err != nil {
		t.Fatalf("writing a fresh package refused: %v", err)
	}

	if err := run(exampleModule(exampleFragment, dir, true)); err != nil {
		t.Fatalf("a freshly written package reported drift: %v", err)
	}

	edited := filepath.Join(dir, "src/generated/operations.ts")
	if err := os.WriteFile(edited, []byte("// edited by hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := run(exampleModule(exampleFragment, dir, true))
	if !errors.Is(err, errStale) || !strings.Contains(err.Error(), "operations.ts") {
		t.Errorf("an edited file reported %v, want errStale naming operations.ts", err)
	}

	if err = run(exampleModule(exampleFragment, t.TempDir(), true)); !errors.Is(err, errStale) {
		t.Errorf("a check over an empty directory reported %v, want errStale", err)
	}
}
