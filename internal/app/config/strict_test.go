package config_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestAKeyTheCoreDoesNotReadIsRefusedWithItsLineNumber is the whole point of
// the strict loader. The retired server's blocks are the ones an upgraded
// deployment still holds, and an operator who configured one of them would
// otherwise discover on the next outage that it was never running.
//
// The line number is what makes the refusal actionable: the shipped file is
// hundreds of lines long, and a message naming only the block sends somebody
// searching it.
func TestAKeyTheCoreDoesNotReadIsRefusedWithItsLineNumber(t *testing.T) {
	t.Parallel()

	path := write(t, `core:
  principal: someone
server:
  host: "0.0.0.0"
`)

	_, err := config.Load(path)
	require.ErrorIs(t, err, config.ErrInvalid)
	require.Contains(t, err.Error(), "line 3")
	require.Contains(t, err.Error(), "server")
	require.Contains(t, err.Error(), path, "the refusal does not say which file it read")
	require.Contains(t, err.Error(), "core, persistence, resources",
		"the refusal does not say which blocks are read instead")
}

// TestARetiredKeyNamesTheKeyThatReplacedIt is the upgrade an operator makes
// without reading a changelog. A key that moved is a line they wrote on purpose
// and that nothing reads any more, so the refusal carries the key that does
// that work now and the process does not start pretending to obey it.
func TestARetiredKeyNamesTheKeyThatReplacedIt(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		file  string
		line  string
		names []string
	}{
		"the printer listener, which belongs to its connector now": {
			file: `core:
  printer:
    addr: ":443"
`,
			line:  "line 2",
			names: []string{"core.printer", "core.connectors.<name>.config.listener"},
		},
		"the compatibility switch, which is the resource row now": {
			file: `core:
  compat:
    graphql:
      enabled: false
`,
			line:  "line 2",
			names: []string{"core.compat", "resources.sink.graphql.enabled"},
		},
		"the MQTT block, which is a sink now": {
			file: `core:
  principal: operator
mqtt:
  enabled: true
`,
			line:  "line 3",
			names: []string{"mqtt", "resources.sink", "type mqtt"},
		},
		"the metrics block, which is a core key now": {
			file: `observability:
  enabled: true
`,
			line:  "line 1",
			names: []string{"observability", "core.observability.enabled"},
		},
		"the link block, which is a resource instance now": {
			file: `fractal:
  enabled: true
`,
			line:  "line 1",
			names: []string{"fractal", "resources.link.<name>.enabled"},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(write(t, testCase.file))
			require.ErrorIs(t, err, config.ErrInvalid)
			require.Contains(t, err.Error(), testCase.line)

			for _, names := range testCase.names {
				require.Contains(t, err.Error(), names)
			}
		})
	}
}

// TestARetiredKeyInTheSetupFileNamesThatFile is the deployment whose operator
// never wrote the key: the first-run wizard did, into its own file in the
// overlay folder. A refusal that named only the operator's file would send them
// looking for a line that is not there.
func TestARetiredKeyInTheSetupFileNamesThatFile(t *testing.T) {
	t.Parallel()

	path := write(t, "core:\n  principal: operator\n")
	setup := writeSetup(t, path, "core:\n  printer:\n    addr: \":443\"\n")

	_, err := config.Load(path)
	require.ErrorIs(t, err, config.ErrInvalid)
	require.Contains(t, err.Error(), setup)
	require.Contains(t, err.Error(), "line 2")
	require.Contains(t, err.Error(), "core.printer")
}

// TestAnIgnoredKeyStartsTheServerAndNamesItsLine is the upgrade from a release
// whose shipped file set a key that has since stopped doing anything. Refusing
// it would stop the server, and an in-app update with it, over a line that asks
// for nothing. The server starts, and Load hands back the file and the line to
// delete so the server can say so.
func TestAnIgnoredKeyStartsTheServerAndNamesItsLine(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]string{
		"a skin a build drew":    " slicer",
		"the skin with no value": "",
		"a skin no build drew":   " neon",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := write(t, "core:\n  principal: operator\n  frontend:\n    skin:"+value+"\n    enabled: true\n")

			core, err := config.Load(path)
			require.NoError(t, err)
			require.Equal(t, []config.IgnoredKey{{
				File:   path,
				Key:    "core.frontend.skin",
				Advice: "The web app has one appearance and no key chooses it. Delete the line",
				Line:   4,
			}}, core.Ignored)
			require.Equal(t, "operator", core.Principal, "the keys around the ignored one were not read")
		})
	}
}

// TestAnIgnoredKeyInTheSetupFileNamesThatFile is the wizard's file holding the
// key: the advice must send the operator to that file and not to their own.
func TestAnIgnoredKeyInTheSetupFileNamesThatFile(t *testing.T) {
	t.Parallel()

	path := write(t, "core:\n  principal: operator\n")
	setup := writeSetup(t, path, "core:\n  frontend:\n    skin: slicer\n")

	core, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, core.Ignored, 1)
	require.Equal(t, setup, core.Ignored[0].File)
	require.Equal(t, 3, core.Ignored[0].Line)
}

// TestAnUnknownKeyInsideTheCoreBlockIsRefusedWithItsLineNumber is the typo an
// operator makes rather than the block they inherited. A misspelled address key
// would leave the process listening somewhere nobody asked for.
func TestAnUnknownKeyInsideTheCoreBlockIsRefusedWithItsLineNumber(t *testing.T) {
	t.Parallel()

	path := write(t, `core:
  api:
    addr: ":8080"
    address: ":9000"
`)

	_, err := config.Load(path)
	require.ErrorIs(t, err, config.ErrInvalid)
	require.Contains(t, err.Error(), "line 4")
	require.Contains(t, err.Error(), "core.api.address")
}

// TestAnUnknownKeyInsideThePersistenceBlockIsRefused closes the last permissive
// block. It held the retired server's keys, so a deployment could name a
// sqlite_path there and believe the database was going where it said.
func TestAnUnknownKeyInsideThePersistenceBlockIsRefused(t *testing.T) {
	t.Parallel()

	path := write(t, `persistence:
  type: sqlite
  sqlite_path: ./data/state.db
`)

	_, err := config.Load(path)
	require.ErrorIs(t, err, config.ErrInvalid)
	require.Contains(t, err.Error(), "line 3")
	require.Contains(t, err.Error(), "persistence.sqlite_path")
}

// TestAnUnknownKeyIsFoundHoweverDeepItIs walks the nested cases the loader has
// to reach: a listener's certificate block, an instance name the operator chose
// under core.connectors, and the settings block under that name, which is the
// driver's and must not be refused.
func TestAnUnknownKeyIsFoundHoweverDeepItIs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		content string
		key     string
		line    int
	}{
		"under a listener's certificate": {
			content: "core:\n  api:\n    tls:\n      cert_file: api.crt\n",
			key:     "core.api.tls.cert_file",
			line:    4,
		},
		"under an instance name the operator chose": {
			content: "core:\n  connectors:\n    xbuddy:\n      type: xbuddy\n      ws_path: /p/ws\n",
			key:     "core.connectors.xbuddy.ws_path",
			line:    5,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(write(t, testCase.content))
			require.ErrorIs(t, err, config.ErrInvalid)
			require.Contains(t, err.Error(), testCase.key)
			require.Contains(t, err.Error(), "line "+strconv.Itoa(testCase.line))
		})
	}
}

// TestADriversOwnSettingsAreNotHeldToTheCoresFieldNames is the other side of
// the walk. The core carries a connector's settings block to the dialect
// unread, because the dialect owns that schema and is the only thing that can
// say whether it is right.
func TestADriversOwnSettingsAreNotHeldToTheCoresFieldNames(t *testing.T) {
	t.Parallel()

	path := write(t, `core:
  connectors:
    xbuddy:
      type: xbuddy
      config:
        max_registrations: 400
        anything_at_all: true
`)

	core, err := config.Load(path)
	require.NoError(t, err)
	require.NoError(t, core.Validate())
}

// TestTheShippedConfigurationAndItsExamplesLoad is the gate that keeps this
// repository's own files honest. A key deleted in Go and left behind in one of
// them now stops a deployment at startup, and nothing else in the tree would
// see it.
func TestTheShippedConfigurationAndItsExamplesLoad(t *testing.T) {
	t.Parallel()

	shipped := filepath.Join("..", "..", "..", "config")

	entries, err := os.ReadDir(filepath.Join(shipped, "examples"))
	require.NoError(t, err)

	files := []string{filepath.Join(shipped, "config.yaml")}

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".yaml") {
			files = append(files, filepath.Join(shipped, "examples", entry.Name()))
		}
	}

	// Five examples and the shipped file. An empty sweep would otherwise pass.
	require.Len(t, files, 6)

	for _, file := range files {
		// Load and not Validate. The TLS profile names certificate files that
		// only exist once `make certificate-all` has run, and validation reads
		// them off the disk; what is under test here is the schema.
		_, loadErr := config.Load(file)
		require.NoError(t, loadErr, "%s names a key this build does not read", file)
	}
}

// TestEveryReleasedConfigurationStillLoads holds this build to the files people
// already run. Each one under testdata/released is the config.yaml a published
// release shipped, and a deployment installed from that release still has it.
// A key this build refuses in one of them stops the server on upgrade, and
// stops the in-app update before it starts. Retire such a key into
// ignoredKeys, or decide in review that the upgrade breaks on purpose.
func TestEveryReleasedConfigurationStillLoads(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob(filepath.Join("testdata", "released", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, file := range files {
		core, loadErr := config.Load(file)
		require.NoError(t, loadErr, "the configuration %s shipped no longer loads", file)
		require.NoError(t, core.Validate(), "the configuration %s shipped no longer validates", file)
	}
}

// TestThePrinterConfigurationSetsWhatThePrinterNeeds reads the file the
// printer's operating system installs beside the printer binary. It must
// start, and it must carry the small pool and the memory limit, because the
// printer binary does not default either of them.
func TestThePrinterConfigurationSetsWhatThePrinterNeeds(t *testing.T) {
	t.Parallel()

	core, err := config.Load(filepath.Join("..", "..", "..", "config", "examples", "config.printer.yaml"))
	require.NoError(t, err)
	require.NoError(t, core.Validate())

	require.Equal(t, 4, core.Persistence.MaxOpenConns)
	require.Equal(t, 2, core.Persistence.MaxIdleConns)
	require.Equal(t, int64(12<<20), core.MemoryLimit)
	require.True(t, core.Frontend.On(), "the printer serves the same web app as every build")
}
