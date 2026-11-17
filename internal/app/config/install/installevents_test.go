package install_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
)

// moduleService renders a service block for a module of one type, with whatever
// events table the case is about.
//
// The events are name and value in one flat list, because a map here would sort
// differently on every run and these cases are about one entry each.
func moduleService(typeName string, events ...string) []byte {
	block := strings.Builder{}

	block.WriteString(lines(
		"resources:",
		"  sink:",
		"    planner:",
		"      type: "+typeName,
		"      runtime: service",
		"      service:",
		"        path: plugins/planner",
		"        checksum: sha256:"+strings.Repeat("1f", 32),
		"        contract_major: 1",
	))

	if len(events) > 0 {
		block.WriteString(lines("        events:"))

		for i := 0; i < len(events); i += 2 {
			block.WriteString(lines("          " + events[i] + ": " + events[i+1]))
		}
	}

	return []byte(block.String())
}

// lines joins configuration lines, which keeps a YAML fixture readable beside
// the case that uses it.
func lines(parts ...string) string { return strings.Join(parts, "\n") + "\n" }

// TestTheEventTypeAModuleEmitsIsItsNamespaceAndTheOperatorsName is where the
// two halves a consumer routes on come from.
//
// The operator writes the name after the namespace, and the host writes the
// namespace, which is derived from the type the operator installed and which no
// module passes anywhere. Neither half is the module's, which is ADR-20260826-w3y rules
// 8 and 26 held for a module rather than for a script.
func TestTheEventTypeAModuleEmitsIsItsNamespaceAndTheOperatorsName(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml",
		moduleService("com.github.aux.acme.planner/spool-tracker", "changed", "spool.changed"))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)

	require.Equal(t, map[string]string{
		"changed": "com.github.aux.acme.planner.spool.changed",
	}, doc.Instances[0].Service.Events,
		"the type an endpoint publishes is not the namespace and the operator's name")
}

// TestAnInstanceThatDeclaresNoEventsCarriesNoEndpoints is the default an
// installed module gets: nothing. The host then installs no emit method at all,
// so the module is silent until an operator says otherwise.
func TestAnInstanceThatDeclaresNoEventsCarriesNoEndpoints(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml",
		moduleService("com.github.aux.acme.planner/spool-tracker"))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)
	require.Empty(t, doc.Instances[0].Service.Events)
}

// TestAnEventsTableTheHostCannotServeIsRefusedAtLoad is the operator reading
// the refusal at the line they wrote it on, per instance, with the rest of the
// document still loading.
func TestAnEventsTableTheHostCannotServeIsRefusedAtLoad(t *testing.T) {
	t.Parallel()

	for name, block := range map[string][]byte{
		"a bare type, which claims no namespace": moduleService(
			"service-plugin", "changed", "spool.changed"),
		"a namespace of one label": moduleService(
			"acme/webhook", "changed", "spool.changed"),
		"a name that is not an event type": moduleService(
			"com.github.aux.acme.planner/spool-tracker", "changed", "Spool Changed"),
		"a name with no segments": moduleService(
			"com.github.aux.acme.planner/spool-tracker", "changed", "'  '"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", block)
			require.NoError(t, err)
			require.Len(t, doc.Failures, 1, "the declaration loaded")
			require.Equal(t, "resources.sink.planner", doc.Failures[0].Key)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
			require.Empty(t, doc.Instances, "a refused instance was kept")
		})
	}
}

// TestAModuleUnderThisProjectsOwnNamespaceCannotForgeACoreType is the refusal
// with teeth.
//
// com.prusa.connect. is closed and every type in it is a published, versioned
// identifier (ADR-20260825-bhe and ADR-20260827-qvk). A namespace is derived from a module
// path, and the reversed rendering of a path under a prusa host lands inside
// it, so an author who chose such a path and an operator who wrote the type
// down would compose a type that shadows the core's. The composed type is held
// against the event vocabulary, and the declaration never loads.
func TestAModuleUnderThisProjectsOwnNamespaceCannotForgeACoreType(t *testing.T) {
	t.Parallel()

	forged := moduleService("com.prusa.connect.foo/tracker", "arrived", "printer.arrived")

	doc, err := install.Parse("config.yaml", forged)
	require.NoError(t, err)
	require.Len(t, doc.Failures, 1, "a module composed a type in the core namespace and loaded")

	said := doc.Failures[0].Err.Error()
	require.Contains(t, said, "com.prusa.connect.foo.printer.arrived",
		"the refusal does not name the type the declaration would have produced")
	require.Contains(t, said, "namespace of your own",
		"the operator is not told what to do about it")

	// A name nothing in the vocabulary carries is refused the same way, so this
	// is the namespace and not one type.
	doc, err = install.Parse("config.yaml",
		moduleService("com.prusa.connect.foo/tracker", "changed", "spool.changed"))
	require.NoError(t, err)
	require.Len(t, doc.Failures, 1)
	require.Contains(t, doc.Failures[0].Err.Error(), "com.prusa.connect.foo.spool.changed")
}
