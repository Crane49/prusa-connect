package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/app/hostschema"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

func TestUpgradeCheckRehearsesTheHostOnASnapshot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "config", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	// The skin key is ignored rather than refused, so the rehearsal passes and
	// names it among the warnings the updater keeps.
	if err := os.WriteFile(configPath, []byte("core:\n  frontend:\n    skin: prusa\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	database := filepath.Join(root, "config", "config.d", "authz.db")
	if err := os.MkdirAll(filepath.Dir(database), 0o700); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", database+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := hostschema.Migrator(db, sqlstore.EngineSQLite, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrator.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err = Upgrade(t.Context(), &out, []string{"check", "-config", configPath, "-json"}); err != nil {
		t.Fatal(err)
	}

	var report upgrade.RehearsalReport
	if err = json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.CheckedAt.IsZero() || report.DurationMS <= 0 {
		t.Fatalf("rehearsal report is %#v", report)
	}
	if !slices.ContainsFunc(report.Warnings, func(warning string) bool {
		return strings.Contains(warning, "line 3: core.frontend.skin")
	}) {
		t.Fatalf("rehearsal warnings %q do not name the ignored key", report.Warnings)
	}

	migratorDB, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = migratorDB.Close() }()

	var applied int
	if err = migratorDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM schema_migration").
		Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 12 {
		t.Fatalf("live database has %d migrations", applied)
	}
}
