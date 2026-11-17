package install_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/extension/script"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// TestADisabledInstanceIsHeldAndNotDropped is decision R-series and earlier research
// §12.3: turning a backend off looks like turning it off, not like deleting it.
// The defect this closes is legacy/app/config.go:435, which skips the entry
// entirely, so a disabled storage has no spec, no API path and nothing an
// operator can switch back on.
func TestADisabledInstanceIsHeldAndNotDropped(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  storage:
    local:
      type: filesystem
      enabled: false
`))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)

	held := doc.Instances[0]
	require.Equal(t, "storage:local", held.Ref.String())
	require.Equal(t, install.StateDisabled, held.State)
	require.Equal(t, "filesystem", held.Type)
}

// TestAnAbsentEnabledKeyIsRuntime keeps the default the one an operator means:
// a block written down was meant, so a missing key must not read as off.
func TestAnAbsentEnabledKeyIsRuntime(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  storage:
    local:
      type: filesystem
`))
	require.NoError(t, err)
	require.Len(t, doc.Instances, 1)
	require.Equal(t, install.StateRuntime, doc.Instances[0].State)
}

// TestOneBadInstanceFailsOnlyItself is the defect at legacy/app/config.go:968
// and :1033, and the same shape at internal/app/resource/load.go:199. One
// unusable block must not take the other blocks of its own kind with it, and it
// must not take the process.
func TestOneBadInstanceFailsOnlyItself(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  storage:
    local:
      type: filesystem
    broken:
      type: ""
    other:
      type: filesystem
      enabled: false
  sink:
    hooks:
      type: webhook
`))
	require.NoError(t, err)

	require.Len(t, doc.Failures, 1)
	require.Equal(t, "resources.storage.broken", doc.Failures[0].Key)
	require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
	require.Contains(t, doc.Failures[0].Err.Error(), "config.yaml")
	require.Contains(t, doc.Failures[0].Err.Error(), "resources.storage.broken")

	names := make([]string, 0, len(doc.Instances))
	for _, instance := range doc.Instances {
		names = append(names, instance.Ref.String())
	}

	require.Equal(t, []string{"storage:local", "storage:other", "sink:hooks"}, names)
}

// TestAKindRefusesARuntimeItCannotOffer is the refusal at load rather than at
// first use. A database has no sdk/databaseapi, so there is no contract a child
// process could speak for one (earlier research verdict 2, earlier research).
func TestAKindRefusesARuntimeItCannotOffer(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  db:
    main:
      type: sqlite
      runtime: service
      service:
        path: plugins/pg
        checksum: sha256:`+strings.Repeat("a", 64)+`
        contract_major: 1
`))
	require.NoError(t, err)
	require.Empty(t, doc.Instances)
	require.Len(t, doc.Failures, 1)
	require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
	require.Contains(t, doc.Failures[0].Err.Error(), `runtime is "service"`)
	require.Contains(t, doc.Failures[0].Err.Error(), "db offers builtin")
}

// TestEveryKindThatMayRunAServiceDoesSo checks the table against the kinds
// rather than one case of it, so a kind added to ref without a runtime here is
// a failure and not a silent refusal of everything.
func TestEveryKindThatMayRunAServiceDoesSo(t *testing.T) {
	t.Parallel()

	for _, kind := range ref.SupervisedKinds() {
		offered := install.Runtimes(kind)
		require.NotEmpty(t, offered, "kind %s offers no runtime at all, so nothing of that kind can load", kind)

		// An asset is files and never code, so it is the one supervised kind
		// with nothing to compile in.
		if kind == ref.KindAsset {
			require.Equal(t, []install.Runtime{install.RuntimeAsset}, offered)

			continue
		}

		require.Contains(t, offered, install.RuntimeBuiltin, "kind %s cannot be compiled in", kind)
	}

	require.NotContains(t, install.Runtimes(ref.KindDB), install.RuntimeService)
	require.Contains(t, install.Runtimes(ref.KindDriver), install.RuntimeService)
}

// TestAKindWithNothingToSuperviseIsRefusedInTheResourcesBlock covers the kind
// that is configuration and not an implementation. A gcode preset is written
// through its own API, so an operator declaring one here has written a block
// nothing will ever construct, and the refusal names what to do instead.
func TestAKindWithNothingToSuperviseIsRefusedInTheResourcesBlock(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  gcode-preset:
    purge:
      type: core/whatever
`))

	require.NoError(t, err)
	require.Empty(t, doc.Instances)
	require.Len(t, doc.Failures, 1)
	require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
	require.Contains(t, doc.Failures[0].Err.Error(), "gcode-preset")
	require.Contains(t, doc.Failures[0].Err.Error(), "declares no instances here")
}

// TestACameraBlockDeclaresNothing holds ADR-20261116-g5x: a camera is a
// device kept in the database, so a file's camera block is no declaration
// and no failure either. The host moves an older build's block once.
func TestACameraBlockDeclaresNothing(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  camera:
    front:
      type: acme/rtsp
`))
	require.NoError(t, err)
	require.Empty(t, doc.Instances)
	require.Empty(t, doc.Failures)
}

// TestAServiceInstanceCarriesWhatTheHostVerifies holds the three keys the host
// needs before it executes anything: where, what proves it, and which contract.
func TestAServiceInstanceCarriesWhatTheHostVerifies(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("1f", 32)

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  driver:
    bambu:
      type: acme/bambu
      runtime: service
      service:
        path: plugins/bambu
        checksum: `+digest+`
        contract_major: 1
      config:
        address: 192.168.1.50
`))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)

	declared := doc.Instances[0]
	require.Equal(t, install.RuntimeService, declared.Runtime)
	require.Equal(t, "plugins/bambu", declared.Service.Path)
	require.Equal(t, digest, declared.Service.Checksum)
	require.Equal(t, 1, declared.Service.ContractMajor)
}

// TestAServiceBlockIsRefusedWhenItCannotBeTrusted covers each half of the
// verification the host does before it launches a child.
func TestAServiceBlockIsRefusedWhenItCannotBeTrusted(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("1f", 32)

	cases := map[string]struct {
		block string
		says  string
	}{
		"no service block": {
			block: "      runtime: service\n",
			says:  "service is absent",
		},
		"no path": {
			block: "      runtime: service\n      service:\n        checksum: " + digest +
				"\n        contract_major: 1\n",
			says: "service.path is empty",
		},
		"a checksum that is not a sha256": {
			block: "      runtime: service\n      service:\n        path: plugins/bambu" +
				"\n        checksum: abc\n        contract_major: 1\n",
			says: "service.checksum",
		},
		"no contract major": {
			block: "      runtime: service\n      service:\n        path: plugins/bambu" +
				"\n        checksum: " + digest + "\n",
			says: "service.contract_major is absent",
		},
		"a contract major of zero": {
			block: "      runtime: service\n      service:\n        path: plugins/bambu" +
				"\n        checksum: " + digest + "\n        contract_major: 0\n",
			says: "service.contract_major is 0",
		},
		"a service block with no service runtime": {
			block: "      service:\n        path: plugins/bambu\n        checksum: " + digest +
				"\n        contract_major: 1\n",
			says: "nothing reads it",
		},
		"a runtime nothing offers": {
			block: "      runtime: wasm\n",
			says:  `runtime is "wasm"`,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml",
				[]byte("resources:\n  driver:\n    bambu:\n      type: bambu\n"+testCase.block))
			require.NoError(t, err)
			require.Empty(t, doc.Instances)
			require.Len(t, doc.Failures, 1)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
			require.Contains(t, doc.Failures[0].Err.Error(), testCase.says)
			require.Contains(t, doc.Failures[0].Err.Error(), "config.yaml")
		})
	}
}

// TestAnUnknownKindFailsItsOwnEntry keeps a mistyped kind from costing the rest
// of the file, and names what the closed set holds.
func TestAnUnknownKindFailsItsOwnEntry(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  storag:
    local:
      type: filesystem
  storage:
    real:
      type: filesystem
`))
	require.NoError(t, err)
	require.Len(t, doc.Instances, 1)
	require.Equal(t, "storage:real", doc.Instances[0].Ref.String())
	require.Len(t, doc.Failures, 1)
	require.Equal(t, "resources.storag.local", doc.Failures[0].Key)
}

// TestTheShippedConfigurationLoads is the case a gate cannot fake: the file the
// image is built from, read by the reader the build uses.
func TestTheShippedConfigurationLoads(t *testing.T) {
	t.Parallel()

	doc, err := install.Load(filepath.Join("..", "..", "..", "..", "config", "config.yaml"))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.NotEmpty(t, doc.Instances, "the shipped config declares no resource instance, so every "+
		"assertion this suite makes about it is vacuous")

	byName := map[string]install.Instance{}
	for _, instance := range doc.Instances {
		byName[instance.Ref.String()] = instance
	}

	// The GraphQL module of ADR-20260910-cqq and the Fractal module are
	// declared here and switched off. No release carries the GraphQL module,
	// so a row switched on would warn at every start. The Fractal
	// module cannot start without settings that name a network only the
	// deployment knows. Everything else the shipped file declares is a type
	// this binary carries.
	for name, instance := range byName {
		switch name {
		case "sink:graphql":
			require.Equalf(t, install.RuntimeService, instance.Runtime, "%s", name)
			require.Equalf(t, install.StateDisabled, instance.State, "%s is declared and switched on", name)
		case "link:fractal":
			require.Equalf(t, install.RuntimeService, instance.Runtime, "%s", name)
			require.Equalf(t, install.StateDisabled, instance.State, "%s is declared and switched on", name)
		default:
			require.Equalf(t, install.RuntimeBuiltin, instance.Runtime, "%s", name)
		}
	}

	require.Contains(t, byName, "sink:graphql")
	require.Contains(t, byName, "link:fractal")

	// The GraphQL module claims /graphql.
	require.Equal(t, "/graphql", byName["sink:graphql"].Service.Mount)
}

// TestNoExampleConfigurationSwitchesFractalOn covers the files an operator
// copies to start a deployment. Fractal is unfinished and off by default, so
// an example may declare it but never switch it on.
func TestNoExampleConfigurationSwitchesFractalOn(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "config", "examples", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no example configuration was found, so this test checks nothing")

	for _, path := range paths {
		doc, loadErr := install.Load(path)
		require.NoError(t, loadErr, path)
		require.Empty(t, doc.Failures, path)

		for _, instance := range doc.Instances {
			if !strings.Contains(instance.Type, "fractal") {
				continue
			}

			require.Equalf(t, install.StateDisabled, instance.State,
				"%s switches on %s, and Fractal must be off by default", path, instance.Ref)
		}
	}
}

// TestAMissingFileIsAnEmptyDocument keeps a first run loadable: a checkout that
// has not been configured yet still has to build.
func TestAMissingFileIsAnEmptyDocument(t *testing.T) {
	t.Parallel()

	doc, err := install.Load(filepath.Join(t.TempDir(), "absent.yaml"))
	require.NoError(t, err)
	require.Empty(t, doc.Instances)
	require.Empty(t, doc.Failures)
}

// TestAResourcesBlockOfTheWrongShapeFailsTheDocument separates the one failure
// that is not per instance: a resources key that is not a map of maps names no
// instance to blame.
func TestAResourcesBlockOfTheWrongShapeFailsTheDocument(t *testing.T) {
	t.Parallel()

	_, err := install.Parse("config.yaml", []byte("resources: [one, two]\n"))
	require.ErrorIs(t, err, install.ErrInvalid)
	require.Contains(t, err.Error(), "config.yaml")
}

// TestLoadReadsTheSameFileParseDoes keeps the path and the bytes entry points
// from drifting.
func TestLoadReadsTheSameFileParseDoes(t *testing.T) {
	t.Parallel()

	body := "resources:\n  sink:\n    hooks:\n      type: webhook\n      enabled: false\n"
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	fromPath, err := install.Load(path)
	require.NoError(t, err)

	fromBytes, err := install.Parse(path, []byte(body))
	require.NoError(t, err)

	require.Equal(t, fromBytes, fromPath)
	require.Equal(t, install.StateDisabled, fromPath.Instances[0].State)
}

// TestOnlyASinkMayOfferTheScriptRuntime is ADR-20260826-w3y's tier boundary at the one
// place it is enforced. A script maps one event onto messages and performs no
// effect; every other kind holds a connection, answers a request or keeps
// state, and a language with no assignment expresses none of those.
func TestOnlyASinkMayOfferTheScriptRuntime(t *testing.T) {
	t.Parallel()

	for _, kind := range ref.SupervisedKinds() {
		offered := install.Runtimes(kind)

		if kind == ref.KindSink {
			require.Contains(t, offered, install.RuntimeScript, "a sink cannot run a script")

			continue
		}

		require.NotContains(t, offered, install.RuntimeScript,
			"kind %s offers the script runtime, and only a sink may", kind)
	}
}

// TestAScriptInstanceCarriesTheSourceTheHostCompiles is the load half of the
// tier: the expression reaches the document, and the type and the runtime say
// one thing rather than two.
func TestAScriptInstanceCarriesTheSourceTheHostCompiles(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  sink:
    audit:
      type: script
      runtime: script
      script: |
        msg("main", event.kind, event.state)
      config:
        endpoints:
          main: http://127.0.0.1:9/publish
`))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)

	declared := doc.Instances[0]
	require.Equal(t, install.RuntimeScript, declared.Runtime)
	require.Equal(t, install.ScriptType, declared.Type)
	require.Contains(t, declared.Script, `msg("main", event.kind, event.state)`)
	require.Equal(t, install.Service{}, declared.Service)
}

// TestAScriptDeclarationIsRefusedWhenItCannotBeRun covers everything an
// operator can get wrong about the pair of keys, and the length bound that is
// the only guard against the parser's unbounded recursion (ADR-20260826-w3y rule 12).
//
// Each case fails its own instance. The message names the file and the key,
// because that is what an operator needs to find the line.
func TestAScriptDeclarationIsRefusedWhenItCannotBeRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		block string
		says  string
	}{
		"a script runtime with no script": {
			block: "      type: script\n      runtime: script\n",
			says:  "script is empty",
		},
		"a script runtime under another type": {
			block: "      type: webhook\n      runtime: script\n      script: 'nil'\n",
			says:  `expected type "script"`,
		},
		"the script type under another runtime": {
			block: "      type: script\n      runtime: builtin\n",
			says:  `expected runtime "script"`,
		},
		"a script under a runtime that does not run one": {
			block: "      type: webhook\n      runtime: builtin\n      script: 'nil'\n",
			says:  "nothing runs it",
		},
		"a source over the length bound": {
			block: "      type: script\n      runtime: script\n      script: '" +
				strings.Repeat("x", script.MaxSourceBytes+1) + "'\n",
			says: "the limit is 8192",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml",
				[]byte("resources:\n  sink:\n    audit:\n"+tc.block))
			require.NoError(t, err)
			require.Empty(t, doc.Instances)
			require.Len(t, doc.Failures, 1)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
			require.Contains(t, doc.Failures[0].Err.Error(), "config.yaml: resources.sink.audit")
			require.Contains(t, doc.Failures[0].Err.Error(), tc.says)
		})
	}
}

// TestRequiredIsRefusedOnAnImplementationTheCoreDidNotWrite is earlier research
// and rule 1. required is the operator saying the deployment does not work
// without this instance, and the core answers 503 on /api/v1/readiness while it
// is not serving, so a module a customer installed could otherwise empty a pod
// out of its Service and leave a print farm reaching nothing.
//
// The runtime is half the answer and the type name is the other half. Neither
// alone refuses both shapes below.
//
// A third shape stood here until ADR-20260827-wmv rule 5 was adopted: a compiled-in type
// outside the core namespace, carrying required. It is gone because that
// declaration is now refused before required is read at all, by
// TestAModuleTypeIsRefusedAsBuiltin. The two that remain are the two an operator
// can still write, and both are types the core did not write.
func TestRequiredIsRefusedOnAnImplementationTheCoreDidNotWrite(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a child process under a bare type name": "      type: bambu\n" +
			"      required: true\n      runtime: service\n      service:\n" +
			"        path: plugins/bambu\n        checksum: sha256:" + strings.Repeat("1f", 32) + "\n" +
			"        contract_major: 1\n",
		"a script the operator wrote": "      type: script\n      required: true\n" +
			"      runtime: script\n      script: 'nil'\n",
	}

	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml",
				[]byte("resources:\n  sink:\n    audit:\n"+block))
			require.NoError(t, err)
			require.Empty(t, doc.Instances)
			require.Len(t, doc.Failures, 1)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)

			// The operator reads this line and has to know which file, which
			// block and which key to change.
			says := doc.Failures[0].Err.Error()
			require.Contains(t, says, "config.yaml: resources.sink.audit")
			require.Contains(t, says, "is not part of the core")
			require.Contains(t, says, "Remove required")
		})
	}
}

// TestAModuleTypeIsRefusedAsBuiltin is ADR-20260827-wmv rule 5, at the door an operator
// writes. A module extends the core and must not be able to break it, and
// nothing that shares this address space can be stopped without stopping the
// server with it, so the only runtime a module has is a process of its own.
//
// Every supervised kind is covered rather than one, because the barrier is
// about the address space and not about what the instance does in it.
func TestAModuleTypeIsRefusedAsBuiltin(t *testing.T) {
	t.Parallel()

	// One instance per supervised kind, so a kind added to the closed set and
	// not thought about here fails this gate rather than slipping past it. A
	// kind that declares no instance is not here, and ref.SupervisedKinds below
	// is what holds the set to the ones that do.
	//
	//nolint:exhaustive // the supervised kinds only, checked against the set below.
	kinds := map[ref.Kind]string{
		ref.KindStorage: "acme/s3",
		ref.KindAuth:    "acme/ldap",
		ref.KindSink:    "acme/webhook",
		ref.KindDriver:  "acme/bambu",
		ref.KindDB:      "acme/mysql",
		ref.KindHistory: "acme/timescale",
		ref.KindLink:    "acme/fractal",
		ref.KindAsset:   "acme/spools",
	}

	require.ElementsMatch(t, ref.SupervisedKinds(), slices.Collect(maps.Keys(kinds)),
		"a supervised kind was added or removed and this gate still reads the old set")

	for kind, typeName := range kinds {
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()

			// An asset kind reads no runtime key as its own runtime, so the
			// builtin door is asked for by name here.
			block := "resources:\n  " + kind.String() + ":\n    mine:\n      type: " + typeName + "\n"
			if kind == ref.KindAsset {
				block += "      runtime: builtin\n"
			}

			doc, err := install.Parse("config.yaml", []byte(block))
			require.NoError(t, err)
			require.Empty(t, doc.Instances, "a module was accepted into this binary's own address space")
			require.Len(t, doc.Failures, 1)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)

			says := doc.Failures[0].Err.Error()
			require.Contains(t, says, "config.yaml: resources."+kind.String()+".mine")

			if kind == ref.KindAsset {
				// The kind offers no builtin at all, so the refusal is the
				// table's and names the one runtime it does offer.
				require.Contains(t, says, "asset offers asset")

				return
			}

			require.Contains(t, says, typeName)
			require.Contains(t, says, "service", "the refusal does not name the runtime a module has")

			require.Contains(t, says, "would run inside itself")
		})
	}
}

// TestAModuleDeclaredAsAServiceLoads is the other direction, and the reason the
// refusal above is about the runtime rather than about the namespace. The same
// name the core did not write is accepted the moment it is declared as a process
// of its own, which is the shape ADR-20260827-wmv rule 5 sends an author to.
//
// Without this the gate above would pass over a build that refused every
// namespaced type outright, which would leave a module with no way to be
// declared at all.
func TestAModuleDeclaredAsAServiceLoads(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", []byte("resources:\n  sink:\n    audit:\n"+
		"      type: acme/webhook\n      runtime: service\n      service:\n"+
		"        path: plugins/acme\n        checksum: sha256:"+strings.Repeat("1f", 32)+"\n"+
		"        contract_major: 1\n"))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)
	require.Equal(t, install.RuntimeService, doc.Instances[0].Runtime)
	require.Equal(t, "acme/webhook", doc.Instances[0].Type)
}

// TestACoreTypeMayBeRequired is the other direction. The refusal is about who
// wrote the implementation, so a core type keeps the flag whichever way it is
// named.
func TestACoreTypeMayBeRequired(t *testing.T) {
	t.Parallel()

	for _, typeName := range []string{"filesystem", "core/filesystem"} {
		t.Run(typeName, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", []byte("resources:\n  storage:\n    local:\n"+
				"      type: "+typeName+"\n      required: true\n"))
			require.NoError(t, err)
			require.Empty(t, doc.Failures)
			require.Len(t, doc.Instances, 1)
		})
	}
}

// TestIsCoreAsksTheRuntimeBeforeTheName is the boundary as its two callers read
// it. A bare type name resolves inside this build, so the name alone reads as
// core for a child process whose binary is not in this artefact at all.
func TestIsCoreAsksTheRuntimeBeforeTheName(t *testing.T) {
	t.Parallel()

	require.True(t, install.IsCore(install.RuntimeBuiltin, "filesystem"))
	require.True(t, install.IsCore(install.RuntimeBuiltin, "core/filesystem"))
	require.False(t, install.IsCore(install.RuntimeBuiltin, "com.github.aux.acme.mk4/rtsp"))
	require.False(t, install.IsCore(install.RuntimeService, "filesystem"))
	require.False(t, install.IsCore(install.RuntimeScript, install.ScriptType))
}

// TestAKindBoundedToOneInstanceRefusesASecond is the rule at the loader: a
// server has one link, so two link blocks fail both rather than
// starting whichever sorted first, and the refusal names every block so the
// operator reads which two disagree.
func TestAKindBoundedToOneInstanceRefusesASecond(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", []byte(`
resources:
  link:
    first:
      type: com.github.aux.prusa3d.fractal/fractal
      runtime: service
      service:
        path: plugins/fractal
        checksum: "sha256:0000000000000000000000000000000000000000000000000000000000000000"
        contract_major: 1
    second:
      type: com.github.aux.prusa3d.fractal/fractal
      runtime: service
      service:
        path: plugins/fractal
        checksum: "sha256:0000000000000000000000000000000000000000000000000000000000000000"
        contract_major: 1
  storage:
    local:
      type: core/filesystem
`))
	require.NoError(t, err)

	require.Len(t, doc.Instances, 1, "the storage block loads and neither link does")
	require.Equal(t, ref.KindStorage, doc.Instances[0].Ref.Kind())

	require.Len(t, doc.Failures, 2)

	for _, failure := range doc.Failures {
		require.ErrorIs(t, failure.Err, install.ErrInvalid)
		require.Contains(t, failure.Err.Error(), "kind link allows one instance and 2 are declared: first, second")
	}

	require.Equal(t, "resources.link.first", doc.Failures[0].Key)
	require.Equal(t, "resources.link.second", doc.Failures[1].Key)

	one, err := install.Parse("config.yaml", []byte(`
resources:
  link:
    only:
      type: com.github.aux.prusa3d.fractal/fractal
      runtime: service
      service:
        path: plugins/fractal
        checksum: "sha256:0000000000000000000000000000000000000000000000000000000000000000"
        contract_major: 1
`))
	require.NoError(t, err)
	require.Len(t, one.Instances, 1, "one link block is the ordinary case")
	require.Empty(t, one.Failures)
}

func TestCheckOnOverMemory(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", install.OnOverMemoryStop, install.OnOverMemoryWarn} {
		require.NoError(t, install.CheckOnOverMemory("service.on_over_memory", value))
	}

	require.ErrorContains(t, install.CheckOnOverMemory("service.on_over_memory", "kill"), "service.on_over_memory")
}
