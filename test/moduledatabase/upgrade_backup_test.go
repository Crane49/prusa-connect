package moduledatabase

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

func TestUpgradeBackupRestoresPostgresExactly(t *testing.T) {
	requireEnabled(t)

	database := "upgrade_backup"
	owner := "upgrade_backup_owner"
	password := "upgrade-backup-secret"
	adminDSN := firstServer.moduleAdmin(t, "upgrade_backup_admin")
	firstServer.mustExec(t,
		"CREATE ROLE "+owner+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD '"+password+"'",
		"GRANT "+owner+" TO upgrade_backup_admin",
		"CREATE DATABASE "+database+" OWNER "+owner,
		"REVOKE ALL ON DATABASE "+database+" FROM PUBLIC",
		"COMMENT ON DATABASE "+database+" IS 'kept through rollback'")
	dsn := firstServer.dsn(owner, password, database)
	firstServer.mustExec(t, "ALTER DATABASE "+database+" SET statement_timeout = 0")

	db := openDB(t, "postgres", dsn)
	if _, err := db.ExecContext(t.Context(), `
CREATE TABLE note (id bigint PRIMARY KEY, text text NOT NULL);
INSERT INTO note (id, text) VALUES (1, 'before');
`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	clients := postgresClientWrappers(t, firstServer.name)
	manager := upgrade.BackupManager{
		Root: t.TempDir(), PGDumpPath: clients.dump, PGRestorePath: clients.restore, PSQLPath: clients.psql,
	}
	manifest, err := manager.Create(t.Context(), upgrade.BackupPlan{
		ID: "postgres-exact-restore",
		Postgres: []upgrade.PostgresSource{{
			Name: "module", DSN: dsn, RestoreDSN: adminDSN, Mode: upgrade.PostgresDump,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	db = openDB(t, "postgres", dsn)
	if _, err = db.ExecContext(t.Context(), `
UPDATE note SET text = 'candidate';
CREATE TABLE candidate_only (id bigint PRIMARY KEY);
`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	if err = manager.Restore(t.Context(), manifest.ID, map[string]upgrade.PostgresRestoreTarget{
		"module": {DSN: dsn, AdminDSN: adminDSN},
	}); err != nil {
		t.Fatal(err)
	}

	db = openDB(t, "postgres", dsn)
	var text string
	if err = db.QueryRowContext(t.Context(), "SELECT text FROM note WHERE id = 1").Scan(&text); err != nil {
		t.Fatal(err)
	}
	if text != "before" {
		t.Fatalf("restored text is %q", text)
	}
	if _, err = db.ExecContext(t.Context(), "SELECT id FROM candidate_only"); err == nil {
		t.Fatal("the database kept a table made after the backup")
	}

	var actualOwner string
	if err = db.QueryRowContext(t.Context(), `
SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()
`).Scan(&actualOwner); err != nil {
		t.Fatal(err)
	}
	if actualOwner != owner {
		t.Fatalf("restored database owner is %q", actualOwner)
	}

	var comment string
	if err = db.QueryRowContext(t.Context(), `
SELECT shobj_description(oid, 'pg_database') FROM pg_database WHERE datname = current_database()
`).Scan(&comment); err != nil {
		t.Fatal(err)
	}
	if comment != "kept through rollback" {
		t.Fatalf("restored database comment is %q", comment)
	}

	var statementTimeout string
	if err = db.QueryRowContext(t.Context(), "SHOW statement_timeout").Scan(&statementTimeout); err != nil {
		t.Fatal(err)
	}
	if statementTimeout != "0" {
		t.Fatalf("restored statement timeout is %q", statementTimeout)
	}

	var leftovers int
	if err = db.QueryRowContext(t.Context(), `
SELECT COUNT(*) FROM pg_database WHERE datname LIKE $1
`, database+"_pcl_%").Scan(&leftovers); err != nil {
		t.Fatal(err)
	}
	if leftovers != 0 {
		t.Fatalf("restore left %d staging databases", leftovers)
	}
}

func TestUpgradeBackupRefusesPostgresWithoutRestoreRights(t *testing.T) {
	requireEnabled(t)

	dsn := firstServer.operatorDatabase(t, "upgrade_unrestorable")
	clients := postgresClientWrappers(t, firstServer.name)
	manager := upgrade.BackupManager{
		Root: t.TempDir(), PGDumpPath: clients.dump, PGRestorePath: clients.restore, PSQLPath: clients.psql,
	}
	_, err := manager.Create(t.Context(), upgrade.BackupPlan{
		ID: "postgres-without-restore-rights",
		Postgres: []upgrade.PostgresSource{{
			Name: "module", DSN: dsn, RestoreDSN: dsn, Mode: upgrade.PostgresDump,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "restore role needs CREATEDB") {
		t.Fatalf("backup error is %v", err)
	}
}

type postgresClients struct {
	dump    string
	restore string
	psql    string
}

func postgresClientWrappers(t *testing.T, container string) postgresClients {
	t.Helper()

	root := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}

		return path
	}
	rewrite := `sed -E 's#@127\.0\.0\.1:[0-9]+/#@127.0.0.1:5432/#'`

	return postgresClients{
		dump: write("pg_dump", fmt.Sprintf(`#!/bin/bash
set -euo pipefail
for argument in "$@"; do
  case "$argument" in --file=*) output=${argument#--file=} ;; esac
done
dsn=${!#}
inside=/tmp/upgrade-backup-$$.dump
dsn=$(printf '%%s' "$dsn" | %s)
docker exec %s pg_dump --format=custom --create --file="$inside" "$dsn"
docker cp %s:"$inside" "$output"
docker exec %s rm -f "$inside"
`, rewrite, container, container, container)),
		restore: write("pg_restore", fmt.Sprintf(`#!/bin/bash
set -euo pipefail
for argument in "$@"; do
  case "$argument" in --dbname=*) dsn=${argument#--dbname=} ;; *) archive=$argument ;; esac
done
inside=/tmp/upgrade-restore-$$.dump
dsn=$(printf '%%s' "$dsn" | %s)
docker cp "$archive" %s:"$inside"
docker exec %s pg_restore --no-owner --dbname="$dsn" "$inside"
docker exec %s rm -f "$inside"
`, rewrite, container, container, container)),
		psql: write("psql", fmt.Sprintf(`#!/bin/bash
set -euo pipefail
arguments=()
for argument in "$@"; do
  case "$argument" in
    --dbname=*) argument="--dbname=$(printf '%%s' "${argument#--dbname=}" | %s)" ;;
  esac
  arguments+=("$argument")
done
docker exec -i %s psql "${arguments[@]}"
`, rewrite, container)),
	}
}
