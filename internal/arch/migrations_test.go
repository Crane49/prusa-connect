package arch_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestPublishedMigrationBytesNeverChange(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "test", "upgrade", "releases.json"))
	if err != nil {
		t.Fatal(err)
	}

	var fixture struct {
		Migrations []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"published_migrations"`
	}
	if err = json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Migrations) == 0 {
		t.Fatal("the published migration list is empty")
	}

	for _, migration := range fixture.Migrations {
		content, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(migration.Path)))
		if readErr != nil {
			t.Errorf("%s: %v", migration.Path, readErr)

			continue
		}
		want, decodeErr := hex.DecodeString(migration.SHA256)
		if decodeErr != nil || len(want) != sha256.Size {
			t.Errorf("%s has an invalid published digest %q", migration.Path, migration.SHA256)

			continue
		}
		got := sha256.Sum256(content)
		if !slices.Equal(got[:], want) {
			t.Errorf("%s changed after it was published: got %x, want %x. "+
				"Restore its bytes and put schema changes in a new migration.", migration.Path, got, want)
		}
	}
}

// ADR-20260827-rwq rule 4: run no migration the host did not write, and embed none.
//
// The host's database has one writer and one migration ledger. A module that
// got a migration into either one produces every failure earlier research counted at
// once: a migration that stops the host starting, a lock the host waits on, a
// schema left behind that refuses a later install, and two writers on one file.
// Rule 1 puts a module's data outside that file entirely, and rule 2 gives it
// sdk/stateapi's Records instead.
//
// //go:embed is the mechanism that would break it, because it is how a
// migration directory becomes bytes inside this binary. This is the one
// mechanical gate the rule can carry: the set of embedded migration directories
// is the host's own, it is written down here, and a third one fails rather than
// shipping.
//
// hostMigrationEmbeds is that set, keyed by the file holding the directive and
// carrying its patterns in sorted order.
//
// It holds one entry per owner of a table in the host's own database, each
// beside the Go code that reads and writes those tables, and
// internal/app/hostschema applies them under one ledger. A module's records are
// an adapter's store, admitted under the pattern below like every other
// adapter's (ADR-20260901-rf2 rule 17).
var hostMigrationEmbeds = map[string][]string{
	"internal/authz/schema.go":                 {"migrations/postgres/*.sql", "migrations/sqlite/*.sql"},
	"internal/platform/auth/dbstore/schema.go": {"migrations/postgres/*.sql", "migrations/sqlite/*.sql"},
	"internal/platform/dashboards/schema.go":   {"migrations/postgres/*.sql", "migrations/sqlite/*.sql"},
	"internal/platform/devicestore/schema.go":  {"migrations/postgres/*.sql", "migrations/sqlite/*.sql"},
	"internal/platform/instanceid/schema.go":   {"migrations/postgres/*.sql", "migrations/sqlite/*.sql"},
}

// adapterMigrationRoot is where an adapter's own migrations are admitted from,
// and it is one pattern over a tree rather than a list of adapters.
//
// ADR-20260901-rf2 rules 8 and 9 are why the door opens at all: an adapter with a store
// of its own embeds and runs its own migrations there, inside its own
// supervised start, and it never touches the host's database, the host's ledger
// or the host's write lock. Rule 9 is why it opens this way and not by name: a
// per-adapter entry here would be a core patch per adapter, which is the thing
// the owner directive of 2026-09-01 forbids. So a new adapter under this tree
// edits nothing in internal/, and a migration embedded anywhere else is still
// refused.
const adapterMigrationRoot = "adapters/"

// moduleMigrationRoots are the trees a module's own migrations are admitted
// from, and only from a main package there (ADR-20261021-bqc rule 23). A main package
// is a module's own binary. No other package can import it, so its migrations
// never reach the host's binary or the host's ledger.
//
//nolint:gochecknoglobals // a closed list read by the gate, never written.
var moduleMigrationRoots = []string{"extensions/", "examples/"}

// admittedAsModule reports whether one embed is a module's own migrations.
func admittedAsModule(embed migrationEmbed) bool {
	return embed.mainPackage && slices.ContainsFunc(moduleMigrationRoots, func(root string) bool {
		return strings.HasPrefix(embed.file, root)
	})
}

// migrationEmbed is one //go:embed pattern that names a migration directory:
// where it was written, and what it names.
type migrationEmbed struct {
	// file is the path relative to the repository root, in slash form.
	file string
	// line is the line the directive is on, for a refusal somebody can open.
	line int
	// pattern is the one pattern, split out of a directive that may hold several.
	pattern string
	// mainPackage is whether the file's package clause names main.
	mainPackage bool
}

func TestTheEmbedNamesOnlyTheHostsOwnMigrationDirectories(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var (
		found   []migrationEmbed
		scanned int
	)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return skipDir(root, path)
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		scanned++

		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		embeds, scanErr := migrationEmbeds(path, filepath.ToSlash(relative))
		if scanErr != nil {
			return scanErr
		}

		found = append(found, embeds...)

		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	// The tree held 374 product Go files when this was written. The floor is
	// under that with room to delete a package, and far above the number a walk
	// that stopped at the first directory would reach.
	requireScanned(t, "Go files under the walked tree", scanned, 300)
	requireScanned(t, "go:embed patterns naming a migration directory", len(found), declaredEmbedCount(),
		"Either a migration embed moved, or the scan is not reaching it. "+
			"hostMigrationEmbeds in this file is the list it is read against.")

	for _, embed := range found {
		// An adapter's own migrations, admitted by the tree they are in and
		// never by the adapter's name. The directory is still checked to exist,
		// so a pattern that names nothing is as much a failure here as it is
		// for the host's own.
		if strings.HasPrefix(embed.file, adapterMigrationRoot) || admittedAsModule(embed) {
			requireEmbeddedDirectory(t, root, embed)

			continue
		}

		declared, known := hostMigrationEmbeds[embed.file]
		if !known || !slices.Contains(declared, embed.pattern) {
			t.Errorf("%s:%d embeds %q, which is a migration directory this gate does not know.\n"+
				"ADR-20260827-rwq rule 4: the host runs no migration it did not write and embeds none, because a\n"+
				"module's migration inside the host's ledger is the partly migrated database Odoo ships.\n"+
				"A module migrates its own store in its own process, before it reports ready (rule 5).\n"+
				"An adapter with a store of its own is admitted under %s (ADR-20260901-rf2 rule 9), and a module's\n"+
				"own database from a main package under %s (ADR-20261021-bqc rule 23). If this directory is the\n"+
				"host's own, add it to hostMigrationEmbeds in %s and say in the commit which schema it\n"+
				"migrates.",
				embed.file, embed.line, embed.pattern, adapterMigrationRoot,
				strings.Join(moduleMigrationRoots, " or "), migrationsGate)

			continue
		}

		requireEmbeddedDirectory(t, root, embed)
	}

	// The other direction. A pattern this gate expects and did not find means
	// the embed was renamed or deleted, and the list above is then guarding a
	// file nothing reads.
	for file, patterns := range hostMigrationEmbeds {
		for _, pattern := range patterns {
			if slices.ContainsFunc(found, func(e migrationEmbed) bool {
				return e.file == file && e.pattern == pattern
			}) {
				continue
			}

			t.Errorf("hostMigrationEmbeds expects %s to embed %q and it does not.\n"+
				"Either the host's migrations moved, in which case update the list in %s, or this gate\n"+
				"is reading a tree that no longer holds them.", file, pattern, migrationsGate)
		}
	}
}

// migrationsGate is this file, named in every refusal so the reader is not sent
// to grep for the list.
const migrationsGate = "internal/arch/migrations_test.go"

// TestTheMigrationEmbedGateSeesANewDirectory is the mutation check. The gate
// above passes over a clean tree, so on its own it cannot tell a tree with no
// stray embed from a scanner that stopped recognising one.
//
// The three shapes are the three a directive can take: one pattern, several on
// one line, and a quoted pattern, which go:embed accepts for a path with a
// space in it.
func TestTheMigrationEmbedGateSeesANewDirectory(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		source string
		want   []string
	}{
		"one pattern": {
			source: "//go:embed migrations/sqlite/*.sql\nvar files embed.FS\n",
			want:   []string{"migrations/sqlite/*.sql"},
		},
		"several patterns on one directive": {
			source: "//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql\nvar files embed.FS\n",
			want:   []string{"migrations/sqlite/*.sql", "migrations/postgres/*.sql"},
		},
		"a module's own migrations, which is what this refuses": {
			source: "//go:embed plugins/planner/migration/*.sql\nvar files embed.FS\n",
			want:   []string{"plugins/planner/migration/*.sql"},
		},
		"a quoted pattern": {
			source: "//go:embed \"db migrations/*.sql\"\nvar files embed.FS\n",
			want:   []string{"db migrations/*.sql"},
		},
		"an embed that is not a migration": {
			source: "//go:embed assets/reference.html\nvar page embed.FS\n",
		},
		"prose that mentions the directive": {
			source: "// go:embed hashes the bytes of the checkout, and migrations are read from it.\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "sample.go")
			if err := os.WriteFile(path, []byte("package sample\n\n"+c.source), 0o600); err != nil {
				t.Fatalf("writing the fixture: %v", err)
			}

			embeds, err := migrationEmbeds(path, "sample.go")
			if err != nil {
				t.Fatalf("scanning the fixture: %v", err)
			}

			got := make([]string, 0, len(embeds))
			for _, embed := range embeds {
				got = append(got, embed.pattern)
			}

			if !slices.Equal(got, c.want) {
				t.Errorf("the scanner read %v, and the directive names %v", got, c.want)
			}
		})
	}
}

// TestTheMigrationEmbedGateAdmitsOnlyAModulesOwnBinary is the mutation check of
// ADR-20261021-bqc rule 23. A module's migrations pass in a main package under a module
// tree, and the same directive in any other package or tree is still refused.
func TestTheMigrationEmbedGateAdmitsOnlyAModulesOwnBinary(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		file, source string
		admitted     bool
	}{
		"a main package under examples/": {
			file: "examples/notes/main.go", source: "package main\n", admitted: true,
		},
		"a main package under extensions/": {
			file: "extensions/notes/main.go", source: "package main\n", admitted: true,
		},
		"a library package under examples/": {
			file: "examples/notes/store/store.go", source: "package store\n",
		},
		"a main package inside the host": {
			file: "internal/tools/main.go", source: "package main\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "sample.go")
			body := c.source + "\n//go:embed migrations/sqlite/*.sql\nvar files embed.FS\n"

			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("writing the fixture: %v", err)
			}

			embeds, err := migrationEmbeds(path, c.file)
			if err != nil || len(embeds) != 1 {
				t.Fatalf("scanning the fixture: %v, %d embeds", err, len(embeds))
			}

			if got := admittedAsModule(embeds[0]); got != c.admitted {
				t.Errorf("admitted = %v, want %v", got, c.admitted)
			}
		})
	}
}

// requireEmbeddedDirectory holds a declared pattern to a directory that is
// really there and really holds migrations. Without it the list above could
// name a directory that was deleted, and the gate would go on reporting green
// over a claim nothing backs.
func requireEmbeddedDirectory(t *testing.T, root string, embed migrationEmbed) {
	t.Helper()

	dir := filepath.Join(root, filepath.FromSlash(filepath.ToSlash(filepath.Dir(embed.file))),
		filepath.FromSlash(filepath.Dir(embed.pattern)))

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Errorf("%s embeds %q and %s cannot be read: %v", embed.file, embed.pattern, dir, err)

		return
	}

	held := 0

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			held++
		}
	}

	if held == 0 {
		t.Errorf("%s embeds %q and %s holds no .sql file, so this gate is guarding an empty claim",
			embed.file, embed.pattern, dir)
	}
}

// declaredEmbedCount is how many patterns the list above holds, which is the
// floor the scan is armed against.
func declaredEmbedCount() int {
	total := 0
	for _, patterns := range hostMigrationEmbeds {
		total += len(patterns)
	}

	return total
}

// migrationEmbeds reports every //go:embed pattern in one file that names a
// migration directory.
//
// It reads lines rather than the parsed comment groups because a directive is
// only a directive on its own line, immediately above a var, and the raw form is
// what the toolchain itself matches. Prose that mentions the word does not begin
// a line with the marker, which the fixture above holds.
func migrationEmbeds(path, relative string) ([]migrationEmbed, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer func() { _ = file.Close() }()

	var (
		found       []migrationEmbed
		mainPackage bool
	)

	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for line := 1; lines.Scan(); line++ {
		if clause, isClause := strings.CutPrefix(strings.TrimSpace(lines.Text()), "package "); isClause {
			mainPackage = strings.TrimSpace(clause) == "main"
		}

		patterns, ok := strings.CutPrefix(strings.TrimSpace(lines.Text()), "//go:embed ")
		if !ok {
			continue
		}

		for _, pattern := range embedPatterns(patterns) {
			if !namesMigrations(pattern) {
				continue
			}

			found = append(found, migrationEmbed{
				file: relative, line: line, pattern: pattern, mainPackage: mainPackage,
			})
		}
	}

	if err = lines.Err(); err != nil {
		return nil, err
	}

	return found, nil
}

// embedPatterns splits one directive into its patterns, unquoting the form
// the embed directive accepts for a path with a space in it.
func embedPatterns(directive string) []string {
	var (
		out   []string
		field strings.Builder
		quote bool
	)

	flush := func() {
		if field.Len() == 0 {
			return
		}

		pattern := field.String()
		if unquoted, err := strconv.Unquote(pattern); err == nil {
			pattern = unquoted
		}

		out = append(out, pattern)
		field.Reset()
	}

	for _, char := range directive {
		switch {
		case char == '"':
			quote = !quote

			field.WriteRune(char)
		case char == ' ' && !quote:
			flush()
		default:
			field.WriteRune(char)
		}
	}

	flush()

	return out
}

// namesMigrations reports whether a pattern names a migration directory. It
// matches the word rather than the two directories the host has, so a third one
// under any spelling reaches the list above rather than passing unread.
func namesMigrations(pattern string) bool {
	for segment := range strings.SplitSeq(filepath.ToSlash(pattern), "/") {
		if strings.Contains(strings.ToLower(segment), "migration") {
			return true
		}
	}

	return false
}
