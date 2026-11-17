package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// writeSetup puts the wizard's file where Load looks for it beside path.
func writeSetup(t *testing.T, path, content string) string {
	t.Helper()

	setup := config.SetupFile(path)
	require.NoError(t, os.MkdirAll(filepath.Dir(setup), 0o750))
	require.NoError(t, os.WriteFile(setup, []byte(content), 0o600))

	return setup
}

// TestTheWizardsFileIsReadWhenThereIsNoOperatorFile is the release binary
// started in an empty folder: the wizard wrote its file and the operator never
// wrote one.
func TestTheWizardsFileIsReadWhenThereIsNoOperatorFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeSetup(t, path, "core:\n  site: workshop\n  api:\n    addr: \":9090\"\n")

	core, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, "workshop", core.Site)
	require.Equal(t, ":9090", core.API.Addr)
}

// TestAKeyTheOperatorWroteWinsOverTheWizard is rule 5 of ADR-20261015-ejz. The two
// blocks merge key by key, so the wizard's other keys in the same block stay.
func TestAKeyTheOperatorWroteWinsOverTheWizard(t *testing.T) {
	t.Parallel()

	path := write(t, "core:\n  api:\n    addr: \":7000\"\n")
	writeSetup(
		t,
		path,
		"core:\n  site: workshop\n  api:\n    addr: \":9090\"\n    allowed_origins: [\"https://a.example\"]\n",
	)

	core, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, ":7000", core.API.Addr, "the operator's key did not win")
	require.Equal(t, "workshop", core.Site, "the wizard's key outside the operator's block was lost")
	require.Equal(t, []string{"https://a.example"}, core.API.AllowedOrigins,
		"the wizard's key inside a block the operator also wrote was lost")
}

// TestAListIsReplacedWhole keeps the merge predictable: an operator who writes
// a list means that list, not that list plus whatever the wizard chose.
func TestAListIsReplacedWhole(t *testing.T) {
	t.Parallel()

	path := write(t, "core:\n  api:\n    allowed_origins: [\"https://b.example\"]\n")
	writeSetup(t, path, "core:\n  api:\n    allowed_origins: [\"https://a.example\", \"https://c.example\"]\n")

	core, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, []string{"https://b.example"}, core.API.AllowedOrigins)
}

// TestAnEmptyBlockInTheOperatorFileKeepsTheWizardsValues covers the shipped
// file's habit of writing a block header with every key commented out.
func TestAnEmptyBlockInTheOperatorFileKeepsTheWizardsValues(t *testing.T) {
	t.Parallel()

	path := write(t, "core:\n")
	writeSetup(t, path, "core:\n  site: workshop\n")

	core, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, "workshop", core.Site)
}

// TestAKeyTheWizardsFileDoesNotReadIsRefusedWithThatFilesLine is rule 6: the
// refusal names the wizard's file, not the operator's.
func TestAKeyTheWizardsFileDoesNotReadIsRefusedWithThatFilesLine(t *testing.T) {
	t.Parallel()

	path := write(t, "core:\n  site: workshop\n")
	setup := writeSetup(t, path, "core:\n  site: workshop\n  nonsense: 1\n")

	_, err := config.Load(path)
	require.ErrorIs(t, err, config.ErrInvalid)
	require.Contains(t, err.Error(), setup)
	require.Contains(t, err.Error(), "line 3")
}

// TestTheWizardsFileHoldsNoResources keeps declared instances in the overlay,
// where every instance declared in the browser is kept.
func TestTheWizardsFileHoldsNoResources(t *testing.T) {
	t.Parallel()

	path := write(t, "")
	writeSetup(t, path, "resources:\n  storage: {}\n")

	_, err := config.Load(path)
	require.ErrorIs(t, err, config.ErrInvalid)
	require.Contains(t, err.Error(), "resources")
}

// TestLoadWithChecksAFileThatIsNotWrittenYet is how the first run validates
// its answers before it writes anything.
func TestLoadWithChecksAFileThatIsNotWrittenYet(t *testing.T) {
	t.Parallel()

	path := write(t, "core:\n  site: operator\n")

	core, err := config.LoadWith(path, []byte("core:\n  site: workshop\n  principal: someone\n"))
	require.NoError(t, err)
	require.Equal(t, "operator", core.Site)
	require.Equal(t, "someone", core.Principal)

	_, statErr := os.Stat(config.SetupFile(path))
	require.ErrorIs(t, statErr, os.ErrNotExist, "LoadWith wrote the file it was asked to check")

	_, err = config.LoadWith(path, []byte("core:\n  nonsense: 1\n"))
	require.ErrorIs(t, err, config.ErrInvalid)
}
