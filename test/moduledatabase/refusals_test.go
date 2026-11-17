package moduledatabase

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// The installs a server refuses, and what two servers on one Postgres server
// do at the same time.

// The cases share the two Postgres servers and run one after another.
func TestARefusedDatabaseInstallsNothing(t *testing.T) {
	requireEnabled(t)

	t.Run("provision on a core that runs on sqlite", func(t *testing.T) {
		srv := newDeployment(t, "").start(t)

		srv.install(t, release100, `{"mode":"provision"}`, true).
			wantCode(t, http.StatusUnprocessableEntity, "REQUEST_INVALID")
		srv.call(t, http.MethodGet, "/api/v1/resources/sink/"+moduleName, "").want(t, http.StatusNotFound)
	})

	t.Run("a connection string that is not a postgres URL", func(t *testing.T) {
		srv := newDeployment(t, "").start(t)

		refused := srv.install(t, release100, `{"mode":"postgres","dsn":"host=db user=notes password=s3cret"}`, true).
			wantCode(t, http.StatusUnprocessableEntity, "REQUEST_INVALID")
		if strings.Contains(string(refused.body), "s3cret") {
			t.Fatalf("the refusal quoted the password: %s", refused.body)
		}
	})

	t.Run("a database chosen without approve", func(t *testing.T) {
		srv := newDeployment(t, "").start(t)

		srv.install(t, release100, `{"mode":"sqlite"}`, false).
			wantCode(t, http.StatusUnprocessableEntity, "REQUEST_INVALID")

		unapproved := srv.install(t, release100, "", false).want(t, http.StatusCreated)
		if !strings.Contains(string(unapproved.body), "database of its own") {
			t.Fatalf("an install without approve does not list the database: %s", unapproved.body)
		}
	})

	t.Run("a release that newly asks for a database", func(t *testing.T) {
		srv := newDeployment(t, "").start(t)

		srv.install(t, release090, "", true).want(t, http.StatusCreated)

		body, err := json.Marshal(map[string]any{
			"module": modulePath, "version": release100.version, "from": releaseDir(t, release100),
			"approve": true, "expect": map[string]any{},
		})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}

		srv.call(t, http.MethodPost, "/api/v1/modules", string(body)).
			wantCode(t, http.StatusConflict, "MODULE_REQUESTS_CHANGED")
	})

	t.Run("a database of the module's name that another role owns", func(t *testing.T) {
		d := newDeployment(t, postgresCore(firstServer.coreDatabase(t, "core_n"), ""))
		firstServer.mustExec(t, "CREATE ROLE squatter NOLOGIN",
			"CREATE DATABASE "+provisionedName("core_n")+" OWNER squatter")

		srv := d.start(t)
		srv.install(t, release100, `{"mode":"provision"}`, true).
			wantCode(t, http.StatusBadGateway, "MODULE_DATABASE_NOT_MADE")
		srv.call(t, http.MethodGet, "/api/v1/resources/sink/"+moduleName, "").want(t, http.StatusNotFound)

		if firstServer.exists(t, "pg_roles", "rolname", provisionedName("core_n")) {
			t.Fatal("a role was made beside a database the server refused")
		}
	})
}

// Two servers whose own databases are on one Postgres server each make a
// database of their own for the same module, and both serve.
func TestTwoCoresOnOnePostgresServerEachMakeTheirOwnModuleDatabase(t *testing.T) {
	requireEnabled(t)

	one := newDeployment(t, postgresCore(firstServer.coreDatabase(t, "core_x"), "")).start(t)
	two := newDeployment(t, postgresCore(firstServer.coreDatabase(t, "core_y"), "")).start(t)

	ns := ""

	for _, srv := range []*server{one, two} {
		ns = srv.installReady(t, release100, `{"mode":"provision"}`).Namespace
		srv.addNote(t, ns, "a note of "+srv.dir)
	}

	for _, core := range []string{"core_x", "core_y"} {
		if !firstServer.exists(t, "pg_database", "datname", provisionedName(core)) {
			t.Fatalf("no database %s", provisionedName(core))
		}
	}

	for _, srv := range []*server{one, two} {
		if held := srv.notes(t, ns); len(held) != 1 || !strings.Contains(held[0].Text, srv.dir) {
			t.Fatalf("a server reads another's notes: %+v", held)
		}
	}
}

// Two servers given one database start their modules at the same time: the
// advisory lock makes one wait, and every migration is applied once.
func TestTwoMigrationsRacingOnOneDatabaseApplyEachOnce(t *testing.T) {
	requireEnabled(t)

	dsn := secondServer.operatorDatabase(t, "notes_race")
	servers := []*server{newDeployment(t, "").start(t), newDeployment(t, "").start(t)}

	var wait sync.WaitGroup

	answers := make([]installed, len(servers))
	errs := make([]error, len(servers))

	for i, srv := range servers {
		wait.Go(func() { answers[i], errs[i] = srv.installServing(t, release120, postgresChoice(dsn)) })
	}

	wait.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	applied := len(answers[0].Migrations) + len(answers[1].Migrations)
	if applied != 8 {
		t.Fatalf("the two starts applied %v and %v, want the 8 migrations once between them",
			answers[0].Migrations, answers[1].Migrations)
	}

	db := openDB(t, "postgres", dsn)

	var rows, distinct int
	if err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*), COUNT(DISTINCT name) FROM module_migration").Scan(&rows, &distinct); err != nil {
		t.Fatalf("read the ledger: %v", err)
	}

	if rows != 8 || distinct != 8 {
		t.Fatalf("the ledger holds %d rows of %d names, want 8 of 8", rows, distinct)
	}
}
