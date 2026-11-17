package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/codegen"
)

// TestEveryKeyTheCoreReadsHasASentence is the gate an author trips by adding a
// field to a configuration struct and stopping there. The page would print an
// empty cell and nobody would see it.
func TestEveryKeyTheCoreReadsHasASentence(t *testing.T) {
	t.Parallel()

	require.NoError(t, check(blocks()))
}

// TestAKeyWithNoSentenceIsRefused proves the gate above measures something: a
// struct walked against an empty table fails, and the message names the key.
func TestAKeyWithNoSentenceIsRefused(t *testing.T) {
	t.Parallel()

	one := block{
		Name: "core",
		Rows: walk("core", reflect.TypeFor[config.Log](), reflect.Value{}, map[string]config.Key{}),
	}

	err := checkDocumented([]block{one})
	require.ErrorIs(t, err, errUndocumented)
	require.Contains(t, err.Error(), "core.level")
}

// TestASentenceForAKeyThatIsGoneIsRefused holds the other direction. A row left
// behind by a renamed key would keep describing something nobody can write.
func TestASentenceForAKeyThatIsGoneIsRefused(t *testing.T) {
	t.Parallel()

	err := checkUnknown([]block{{Name: "core", Rows: []row{{Path: "core.api.addr"}}}})
	require.ErrorIs(t, err, errUnknownKey)
	require.Contains(t, err.Error(), "persistence.type")
}

// TestALongSentenceIsRefused holds the word limit.
func TestALongSentenceIsRefused(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("word ", maxWords+1)

	err := checkLength([]block{{Name: "core", Rows: []row{{Path: "core.api.addr", Doc: long}}}})
	require.ErrorIs(t, err, errLongSentence)
	require.Contains(t, err.Error(), "core.api.addr")
}

// TestALevelTheWizardDoesNotDrawIsRefused holds ADR-20261015-ejz rule 15. A key with
// a misspelt level would be asked on neither path of the first-run wizard.
func TestALevelTheWizardDoesNotDrawIsRefused(t *testing.T) {
	t.Parallel()

	require.NoError(t, checkLevels(map[string]config.Key{
		"core.api.addr": {Level: config.LevelBasic},
		"core.site":     {},
	}))

	err := checkLevels(map[string]config.Key{"core.api.addr": {Level: "basik"}})
	require.ErrorIs(t, err, errLevel)
	require.Contains(t, err.Error(), "core.api.addr")
}

// TestASeeThatNamesNoPageIsRefused keeps the wizard's links on real pages.
func TestASeeThatNamesNoPageIsRefused(t *testing.T) {
	t.Parallel()

	dir := filepath.Join("..", "..", "docs", "src", "content", "docs")

	require.NoError(t, checkSee(dir, config.Keys, resource.Keys))
	require.NoError(t, checkSee(dir, map[string]config.Key{"a": {See: "mqtt/"}}), "a slug is a page")

	err := checkSee(dir, map[string]config.Key{"core.api.addr": {See: "no-such-page/"}})
	require.ErrorIs(t, err, errSee)
}

// TestTheDefaultsComeFromTheCode reads the defaults out of the walk rather
// than out of the page, so a default changed in Go moves the table by itself.
func TestTheDefaultsComeFromTheCode(t *testing.T) {
	t.Parallel()

	rows := map[string]row{}
	for _, one := range blocks() {
		for _, each := range one.Rows {
			rows[each.Path] = each
		}
	}

	require.Equal(t, "`"+config.DefaultAPIAddr+"`", rows["core.api.addr"].Default)
	require.Equal(t, "`6h`", rows["core.api.stream_timeout"].Default)
	require.Equal(t, "`30m`", rows["persistence.conn_max_lifetime"].Default)
	require.Equal(t, "duration", rows["core.shutdown_timeout"].Type)
	require.Equal(t, "when persistence.type is postgres", rows["persistence.postgres.dsn"].Required)
}

// TestThePersistenceBlockIsNotAlsoACoreKey holds the walk to what the decoder
// reads. core.persistence is tagged "-" and a file naming it is refused, so a
// page offering it would send an operator to a key that stops the server.
func TestThePersistenceBlockIsNotAlsoACoreKey(t *testing.T) {
	t.Parallel()

	for path := range pathsOf(blocks()) {
		require.False(t, strings.HasPrefix(path, "core.persistence."),
			"%s is published as a core key, and the core refuses it there", path)
	}
}

// TestTheResourceKeysAreTheOnesTheLoaderDecodes compares the published set
// against the struct one instance block is decoded into.
func TestTheResourceKeysAreTheOnesTheLoaderDecodes(t *testing.T) {
	t.Parallel()

	published := pathsOf(blocks())

	for field := range resource.EntryType().Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		require.NotEmpty(t, name)

		var found bool

		for path := range published {
			if strings.HasPrefix(path, "resources.<kind>.<name>."+name) {
				found = true

				break
			}
		}

		require.True(t, found, "%s is decoded out of an instance block and is not on the page", name)
	}
}

// TestOnlyTheRegionMoves proves the splice leaves a person's prose alone: the
// bytes before the opening marker and after the closing one survive a run.
func TestOnlyTheRegionMoves(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.md")
	before := "above\n\n" + regionStart + "x -->\n\nold\n\n" + regionEnd + "\n\nbelow\n"
	require.NoError(t, os.WriteFile(path, []byte(before), 0o600))

	page, err := renderPage(path, region(blocks()))
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(string(page), "above\n\n"))
	require.True(t, strings.HasSuffix(string(page), "\n\nbelow\n"))
	require.NotContains(t, string(page), "\nold\n")
	require.Contains(t, string(page), "`core.api.addr`")
}

// TestAPageWithoutItsMarkersIsRefused holds the other half of the splice. A
// page somebody rewrote without the markers is refused by name rather than
// overwritten.
func TestAPageWithoutItsMarkersIsRefused(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.md")
	require.NoError(t, os.WriteFile(path, []byte("no markers\n"), 0o600))

	_, err := renderPage(path, region(blocks()))
	require.ErrorIs(t, err, codegen.ErrNoRegion)
}

// TestTheCommittedPageMatchesTheStructs is the gate make config-reference-check
// runs. It fails when somebody edits the region by hand or changes a struct
// without regenerating.
func TestTheCommittedPageMatchesTheStructs(t *testing.T) {
	t.Parallel()

	reference := filepath.Join("..", "..", filepath.FromSlash(defaultReferencePath))
	published := 0

	for _, page := range config.Pages {
		path := pagePath(reference, page.Slug)

		body, err := renderPage(path, region(rowsOn(blocks(), page.Slug)))
		require.NoError(t, err, page.Slug)
		require.NoError(t, codegen.CompareFile(path, body, errStale, makeTarget), page.Slug)

		published += len(pathsOf(rowsOn(blocks(), page.Slug)))
	}

	require.Equal(t, len(pathsOf(blocks())), published, "a key is on no page, or on two")
}
