package resourcehost_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// A module an operator mounted at a root prefix, the way the GraphQL module
// is: the same example child, under a second mountable
// namespace so that two of them can be declared without colliding on the
// namespace claim, which is a different refusal.
const (
	// otherNamespace is a second namespace the grammar accepts, for the second
	// module in the claim cases.
	otherNamespace = "com.github.aux.other"
	// otherModuleType is the example module under that namespace. The artefact
	// is named after the type with the namespace taken off, so both types
	// launch the one file service_test.go builds.
	otherModuleType = otherNamespace + "/" + pluginType
	// graphqlMount is the root prefix the cases mount the module at.
	graphqlMount = "/graphql"
)

// mounted is one module instance as a case declares it: its name, its type and
// the root prefix the operator wrote, empty for a module under its namespace.
type mounted struct {
	name     string
	typeName string
	mount    string
}

// declareMounted writes the configuration for any number of module instances,
// each with its own mount key, and reloads the store.
func (w *wired) declareMounted(dir, checksum string, modules ...mounted) {
	w.t.Helper()

	var file strings.Builder

	file.WriteString("resources:\n  sink:\n")

	for _, module := range modules {
		fmt.Fprintf(&file, `    %s:
      type: %s
      runtime: service
      enabled: true
      service:
        path: %s
        checksum: %s
        contract_major: 1
`, module.name, module.typeName, filepath.ToSlash(dir), checksum)

		if module.mount != "" {
			fmt.Fprintf(&file, "        mount: %s\n", module.mount)
		}
	}

	w.writeFile(file.String())

	require.NoError(w.t, w.store.Reload(w.t.Context()))
}

// sinkNamed is one instance of kind sink by name.
func sinkNamed(t *testing.T, name string) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindSink, name)
	require.NoError(t, err)

	return instance
}

// TestAModuleClaimsTheRootPrefixTheOperatorNamed is ADR-20260827-wmv rule 12 on the
// real plane: the operator writes service.mount, the module comes up, the plane
// answers the router's question about the prefix with that module, and the
// document is rebased under the prefix rather than under the namespace.
func TestAModuleClaimsTheRootPrefixTheOperatorNamed(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	module := install(t)
	instance := theServiceSink(t)

	w.declareMounted(module.dir, module.checksum, mounted{
		name: instance.ID(), typeName: mountedModuleType, mount: graphqlMount,
	})
	w.waitReady(instance)

	w.waitLog(func(logged string) bool { return strings.Contains(logged, "mount="+graphqlMount) },
		"the host never reported the mount beside the child's plugin.info")

	surface, claimed := w.host.Root(graphqlMount + "/spools")
	require.True(t, claimed, "the plane does not answer the prefix the operator named")
	require.Equal(t, instance, surface.Instance)
	require.Equal(t, graphqlMount, surface.Mount)
	require.NotNil(t, surface.Handler)

	require.Contains(t, string(surface.OpenAPI), `"`+graphqlMount+`/spools"`,
		"the document was not rebased under the root prefix")
	require.NotContains(t, string(surface.OpenAPI), `"/api/v1/modules/`,
		"the document was rebased under the namespace, where the module is not reached")

	// The prefix alone is the module's, and a path that merely starts with the
	// same letters is nobody's.
	_, claimed = w.host.Root(graphqlMount)
	require.True(t, claimed)

	_, claimed = w.host.Root(graphqlMount + "ql")
	require.False(t, claimed, "a path beside the prefix was resolved to the module")

	// The namespace still names the module, and says where it went.
	byNamespace, claimed := w.host.Module(mountedNamespace)
	require.True(t, claimed)
	require.Equal(t, graphqlMount, byNamespace.Mount)
}

// TestAModuleOnAReservedOrMalformedPrefixIsRefusedAtLoad is the refusal half:
// a prefix this server serves, and a prefix the router cannot route, each
// refused before a process is launched, each with the code that says which
// edit fixes it, and each naming the prefix.
func TestAModuleOnAReservedOrMalformedPrefixIsRefusedAtLoad(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mount string
		code  string
	}{
		"under the api":     {mount: "/api/v1/graphql", code: resourcehost.CodeMountReserved().String()},
		"the reference":     {mount: "/docs", code: resourcehost.CodeMountReserved().String()},
		"the metrics":       {mount: "/metrics", code: resourcehost.CodeMountReserved().String()},
		"a device wire":     {mount: "/p/graphql", code: resourcehost.CodeMountReserved().String()},
		"a trailing slash":  {mount: "/graphql/", code: resourcehost.CodeMountInvalid().String()},
		"a dot segment":     {mount: "/graphql/../api", code: resourcehost.CodeMountInvalid().String()},
		"a wildcard":        {mount: "/graphql/*", code: resourcehost.CodeMountInvalid().String()},
		"the origin root":   {mount: "/", code: resourcehost.CodeMountInvalid().String()},
		"not absolute":      {mount: "graphql", code: resourcehost.CodeMountInvalid().String()},
		"a percent escape":  {mount: "/graph%20ql", code: resourcehost.CodeMountInvalid().String()},
		"a space in a name": {mount: `"/graph ql"`, code: resourcehost.CodeMountInvalid().String()},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w := newWiredBeside(t, []string{"/p/ws", "/p/*"})
			module := install(t)
			instance := theServiceSink(t)

			w.declareMounted(module.dir, module.checksum, mounted{
				name: instance.ID(), typeName: mountedModuleType, mount: testCase.mount,
			})

			w.waitLog(func(logged string) bool { return strings.Contains(logged, testCase.code) },
				"the refusal never reached the log with its code")

			logged := w.logs.String()
			require.Contains(t, logged, strings.Trim(testCase.mount, `"`), "the refusal does not name the prefix")
			require.Contains(t, logged, instance.String(), "the refusal does not name the instance")
			require.NotContains(t, logged, "is serving on stdio", "a process was launched for a refused mount")

			_, supervising := w.host.Status(instance)
			require.False(t, supervising, "a module on a refused prefix is under supervision")

			_, claimed := w.host.Root(strings.Trim(testCase.mount, `"`))
			require.False(t, claimed, "a refused prefix is answered by the plane")
		})
	}
}

// TestASecondModuleOnOnePrefixFailsAloneAndTheFirstServes is one prefix, one
// claim: the second instance is refused at load with the code and both names,
// the first keeps serving there, and a prefix over or under a held one is a
// second claim on it too.
func TestASecondModuleOnOnePrefixFailsAloneAndTheFirstServes(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	module := install(t)
	first := sinkNamed(t, "example")
	second := sinkNamed(t, "twin")

	// One claims /graphql and the other /graphql/v2, which is under it. The
	// plan starts instances in ref order, so the first name is the one that
	// gets there first.
	w.declareMounted(module.dir, module.checksum,
		mounted{name: first.ID(), typeName: mountedModuleType, mount: graphqlMount},
		mounted{name: second.ID(), typeName: otherModuleType, mount: graphqlMount + "/v2"},
	)
	w.waitReady(first)

	w.waitLog(func(logged string) bool { return strings.Contains(logged, resourcehost.CodeMountClaimed().String()) },
		"the second claim was never refused")

	logged := w.logs.String()
	require.Contains(t, logged, first.String(), "the refusal does not name the instance holding the prefix")
	require.Contains(t, logged, second.String(), "the refusal does not name the instance that was refused")
	require.Contains(t, logged, graphqlMount+"/v2", "the refusal does not name the prefix")

	_, supervising := w.host.Status(second)
	require.False(t, supervising, "the second module on one prefix is under supervision")

	// The first is the one answering there, under both addresses the second
	// would have taken.
	for _, path := range []string{graphqlMount + "/spools", graphqlMount + "/v2/spools"} {
		surface, claimed := w.host.Root(path)
		require.True(t, claimed, "%s is unclaimed", path)
		require.Equal(t, first, surface.Instance, "%s is answered by the wrong module", path)
	}

	// Removing the first frees the prefix. The second's own block did not
	// change, so a reload alone plans nothing for it, and a refused instance is
	// under no supervision for a restart to reach; switching it off and on is
	// what starts it again, which is what the code's remedy says.
	for _, enabled := range []bool{false, true} {
		w.writeFile(fmt.Sprintf(`
resources:
  sink:
    twin:
      type: %s
      runtime: service
      enabled: %t
      service:
        path: %s
        checksum: %s
        contract_major: 1
        mount: %s/v2
`, otherModuleType, enabled, filepath.ToSlash(module.dir), module.checksum, graphqlMount))
		require.NoError(t, w.store.Reload(t.Context()))
	}

	w.waitReady(second)

	surface, claimed := w.host.Root(graphqlMount + "/v2/spools")
	require.True(t, claimed, "the freed prefix was not claimed by the second module")
	require.Equal(t, second, surface.Instance)

	_, claimed = w.host.Root(graphqlMount + "/spools")
	require.False(t, claimed, "a stopped module still holds its prefix")
}
