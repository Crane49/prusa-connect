package moduledatabase

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The releases of the example module the cases install. 1.0.0 is the module
// as it is: migrations 1 to 3, the third with a Go step. 1.1.0 adds version
// 4, a column and a step that fills it from the notes already kept, and
// raises the compat floor to 4. 1.2.0 adds version 5; its broken build's step
// fails after the file added a column.

const (
	titleColumn = "ALTER TABLE note ADD COLUMN title TEXT NOT NULL DEFAULT '';\n"
	tagsColumn  = "ALTER TABLE note ADD COLUMN tags TEXT NOT NULL DEFAULT '';\n"
)

// titleStep is version 4: the first word of every note becomes its title, and
// no older release may open the database afterwards.
const titleStep = `package main

import (
	"context"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

func init() {
	schema.Compat = 4
	schema.Steps = append(schema.Steps, moduledb.Step{Version: 4, Name: "fill_titles", Up: fillTitles})
}

func fillTitles(ctx context.Context, tx moduledb.Tx, _ wirerpc.DatabaseEngine) error {
	rows, err := tx.QueryContext(ctx, "SELECT id, text FROM note")
	if err != nil {
		return err
	}

	titles := map[int64]string{}

	for rows.Next() {
		var (
			id   int64
			text string
		)

		if err = rows.Scan(&id, &text); err != nil {
			_ = rows.Close()

			return err
		}

		titles[id] = strings.Fields(text)[0]
	}

	if err = rows.Close(); err != nil {
		return err
	}

	for id, title := range titles {
		if _, err = tx.ExecContext(ctx, "UPDATE note SET title = $1 WHERE id = $2", title, id); err != nil {
			return err
		}
	}

	return nil
}
`

// tagStep is version 5. The broken build returns an error after it wrote.
func tagStep(broken bool) string {
	ending := "return nil"
	if broken {
		ending = `return errors.New("the tags cannot be written")`
	}

	return `package main

import (
	"context"
	"errors"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

var _ = errors.New

func init() {
	schema.Steps = append(schema.Steps, moduledb.Step{Version: 5, Name: "tag_notes", Up: tagNotes})
}

func tagNotes(ctx context.Context, tx moduledb.Tx, _ wirerpc.DatabaseEngine) error {
	if _, err := tx.ExecContext(ctx, "UPDATE note SET tags = 'kept'"); err != nil {
		return err
	}

	` + ending + `
}
`
}

// withVersion4 is the files 1.1.0 adds, and withVersion5 the ones 1.2.0 adds
// beside them.
func withVersion4() map[string]string {
	return map[string]string{
		"migrations/sqlite/0004_note_title.sql":   titleColumn,
		"migrations/postgres/0004_note_title.sql": titleColumn,
		"e2e_title_step.go":                       titleStep,
	}
}

func withVersion5(broken bool) map[string]string {
	out := withVersion4()
	out["migrations/sqlite/0005_note_tags.sql"] = tagsColumn
	out["migrations/postgres/0005_note_tags.sql"] = tagsColumn
	out["e2e_tag_step.go"] = tagStep(broken)

	return out
}

//nolint:gochecknoglobals // the releases every case installs, built once.
var (
	release100    = release{version: "1.0.0"}
	release110    = release{version: "1.1.0", overlay: withVersion4()}
	release120bad = release{version: "1.2.0", variant: "-broken", overlay: withVersion5(true)}
	release120    = release{version: "1.2.0", overlay: withVersion5(false)}
	release090    = release{version: "0.9.0", noAsk: true}
)

// installed is what an install answered.
type installed struct {
	Namespace     string   `json:"namespace"`
	State         string   `json:"state"`
	Reason        string   `json:"reason"`
	SchemaVersion int      `json:"schema_version"`
	Migrations    []string `json:"migrations"`
	DatabaseLeft  string   `json:"database_left"`
}

// install installs one release. database is the database block of the
// request, and empty for none.
func (s *server) install(t *testing.T, one release, database string, approve bool) answer {
	t.Helper()

	body := map[string]any{"module": modulePath, "version": one.version, "from": releaseDir(t, one),
		"approve": approve}

	if database != "" {
		body["database"] = json.RawMessage(database)
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode the install: %v", err)
	}

	return s.call(t, http.MethodPost, "/api/v1/modules", string(encoded))
}

// installReady installs a release and fails unless it is serving.
func (s *server) installReady(t *testing.T, one release, database string) installed {
	t.Helper()

	out, err := s.installServing(t, one, database)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// installServing installs a release and answers an error unless it is
// serving, for a case that installs from more than one goroutine.
func (s *server) installServing(t *testing.T, one release, database string) (installed, error) {
	t.Helper()

	var out installed

	answered := s.install(t, one, database, true)
	if answered.status != http.StatusCreated {
		return out, fmt.Errorf("the install of %s answered %d: %s", one.version, answered.status, answered.body)
	}

	if err := json.Unmarshal(answered.body, &out); err != nil {
		return out, fmt.Errorf("decode the install: %w", err)
	}

	if out.State != "ready" {
		return out, fmt.Errorf("%s is %s, want ready: %s", one.version, out.State, out.Reason)
	}

	return out, nil
}

// note is one row the module's route answers.
type note struct {
	ID    int64  `json:"id"`
	Text  string `json:"text"`
	Words int    `json:"words"`
}

func (s *server) notes(t *testing.T, namespace string) []note {
	t.Helper()

	var out struct {
		Notes []note `json:"notes"`
	}

	if err := json.Unmarshal(s.call(t, http.MethodGet, "/api/v1/modules/"+namespace+"/notes", "").
		want(t, http.StatusOK).body, &out); err != nil {
		t.Fatalf("decode the notes: %v", err)
	}

	return out.Notes
}

func (s *server) addNote(t *testing.T, namespace, text string) {
	t.Helper()

	s.call(t, http.MethodPost, "/api/v1/modules/"+namespace+"/notes", `{"text":"`+text+`"}`).
		want(t, http.StatusCreated)
}

// schema is what the resources listing says about the module's database.
type schemaState struct {
	State   string
	Version int
	Applied []string
}

func (s *server) schema(t *testing.T) schemaState {
	t.Helper()

	var read struct {
		Status struct {
			State string `json:"state"`
		} `json:"status"`
		Module struct {
			Database *struct {
				Schema *struct {
					Version int      `json:"version"`
					Applied []string `json:"applied"`
				} `json:"schema"`
			} `json:"database"`
		} `json:"module"`
	}

	if err := json.Unmarshal(s.call(t, http.MethodGet, "/api/v1/resources/sink/"+moduleName, "").
		want(t, http.StatusOK).body, &read); err != nil {
		t.Fatalf("decode the resource: %v", err)
	}

	out := schemaState{State: read.Status.State}
	if read.Module.Database != nil && read.Module.Database.Schema != nil {
		out.Version, out.Applied = read.Module.Database.Schema.Version, read.Module.Database.Schema.Applied
	}

	return out
}

// left is the databases the server lists as left behind.
func (s *server) left(t *testing.T) []map[string]any {
	t.Helper()

	var out struct {
		Items []map[string]any `json:"items"`
	}

	if err := json.Unmarshal(s.call(t, http.MethodGet, "/api/v1/modules/databases", "").
		want(t, http.StatusOK).body, &out); err != nil {
		t.Fatalf("decode the databases left behind: %v", err)
	}

	return out.Items
}

// layout is one place an operator may put the core's data and the module's.
type layout struct {
	name string
	// deploy is the server's directory and its persistence block.
	deploy func(t *testing.T) deployment
	// choice is the database block of the first install.
	choice string
	// open opens the module's database the way the case reads it.
	open func(t *testing.T, d deployment) *sql.DB
	// purged checks what the purge did on the database's server.
	purged func(t *testing.T, d deployment)
	// done is what the purge answers.
	done string
	// kept says the purge leaves the data, so a fresh install finds it.
	kept bool
}

func layouts(t *testing.T) []layout {
	t.Helper()

	sqliteFile := func(t *testing.T, d deployment) *sql.DB {
		t.Helper()

		return openDB(t, "sqlite", filepath.Join(d.dir, "module-data", moduleName, "database.sqlite"))
	}

	provisioned := func(server postgresServer, core string) func(t *testing.T, d deployment) *sql.DB {
		return func(t *testing.T, _ deployment) *sql.DB {
			t.Helper()

			return openDB(t, "postgres", server.dsn("postgres", server.password, provisionedName(core)))
		}
	}

	gone := func(server postgresServer, core string) func(t *testing.T, d deployment) {
		return func(t *testing.T, _ deployment) {
			t.Helper()

			name := provisionedName(core)
			if server.exists(t, "pg_database", "datname", name) || server.exists(t, "pg_roles", "rolname", name) {
				t.Fatalf("the purge left the database or the role %s on %s", name, server.name)
			}
		}
	}

	operator := map[string]string{
		"module-on-second": secondServer.operatorDatabase(t, "notes_b"),
		"module-on-first":  firstServer.operatorDatabase(t, "notes_f"),
		"module-of-sqlite": secondServer.operatorDatabase(t, "notes_s"),
	}

	external := func(dsn string) func(t *testing.T, d deployment) *sql.DB {
		return func(t *testing.T, _ deployment) *sql.DB {
			t.Helper()

			return openDB(t, "postgres", dsn)
		}
	}

	untouched := func(server postgresServer, database string) func(t *testing.T, d deployment) {
		return func(t *testing.T, _ deployment) {
			t.Helper()

			if !server.exists(t, "pg_database", "datname", database) {
				t.Fatalf("the purge dropped %s, which the operator made", database)
			}
		}
	}

	return []layout{
		{
			name:   "core on sqlite, module on sqlite",
			deploy: func(t *testing.T) deployment { t.Helper(); return newDeployment(t, "") },
			open:   sqliteFile,
			purged: func(t *testing.T, d deployment) {
				t.Helper()

				if _, err := os.Stat(filepath.Join(d.dir, "module-data", moduleName, "database.sqlite")); err == nil {
					t.Fatal("the purge left the SQLite file")
				}
			},
			done: "deleted",
		},
		{
			name:   "core on sqlite, module on the operator's postgres",
			deploy: func(t *testing.T) deployment { t.Helper(); return newDeployment(t, "") },
			choice: postgresChoice(operator["module-of-sqlite"]),
			open:   external(operator["module-of-sqlite"]),
			purged: untouched(secondServer, "notes_s"),
			done:   "forgotten",
			kept:   true,
		},
		{
			name: "two servers: core on the first, module on the second",
			deploy: func(t *testing.T) deployment {
				t.Helper()

				return newDeployment(t, postgresCore(firstServer.coreDatabase(t, "core_c"), ""))
			},
			choice: postgresChoice(operator["module-on-second"]),
			open:   external(operator["module-on-second"]),
			purged: untouched(secondServer, "notes_b"),
			done:   "forgotten",
			kept:   true,
		},
		{
			name: "two servers: core on the first, module made there by module_admin_dsn",
			deploy: func(t *testing.T) deployment {
				t.Helper()

				return newDeployment(t, postgresCore(firstServer.coreDatabase(t, "core_d"),
					firstServer.moduleAdmin(t, "module_admin_d")))
			},
			choice: `{"mode":"provision"}`,
			open:   provisioned(firstServer, "core_d"),
			purged: gone(firstServer, "core_d"),
			done:   "dropped",
		},
		{
			name: "one server: core and module made by the core's own role",
			deploy: func(t *testing.T) deployment {
				t.Helper()

				return newDeployment(t, postgresCore(firstServer.coreDatabase(t, "core_e"), ""))
			},
			choice: `{"mode":"provision"}`,
			open:   provisioned(firstServer, "core_e"),
			purged: gone(firstServer, "core_e"),
			done:   "dropped",
		},
		{
			name: "one server: core, and the module in a database the operator made",
			deploy: func(t *testing.T) deployment {
				t.Helper()

				return newDeployment(t, postgresCore(firstServer.coreDatabase(t, "core_f"), ""))
			},
			choice: postgresChoice(operator["module-on-first"]),
			open:   external(operator["module-on-first"]),
			purged: untouched(firstServer, "notes_f"),
			done:   "forgotten",
			kept:   true,
		},
	}
}

// provisionedName is the database the server makes for the example module
// beside a core database.
func provisionedName(core string) string {
	return "module_" + core + "_" + strings.ReplaceAll(moduleName, "-", "_")
}

func postgresChoice(dsn string) string {
	encoded, _ := json.Marshal(map[string]string{"mode": "postgres", "dsn": dsn})

	return string(encoded)
}

// openDB opens a database the case reads, and closes it with the case.
func openDB(t *testing.T, driver, dsn string) *sql.DB {
	t.Helper()

	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatalf("open %s: %v", driver, err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

// column reads one text column of every note, in order.
func column(t *testing.T, db *sql.DB, name string) []string {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), "SELECT "+name+" FROM note ORDER BY id")
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	defer func() { _ = rows.Close() }()

	var out []string

	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		out = append(out, value)
	}

	if err = rows.Err(); err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	return out
}

// highest is the highest version the module's ledger records.
func highest(t *testing.T, db *sql.DB) int {
	t.Helper()

	var version int
	if err := db.QueryRowContext(context.Background(), "SELECT MAX(version) FROM module_migration").
		Scan(&version); err != nil {
		t.Fatalf("read the ledger: %v", err)
	}

	return version
}

// TestTheModuleDatabaseLivesThroughEveryStepOnEveryLayout is the matrix: each
// layout takes the module through install, restart, two updates, a failed
// update, a refused downgrade, removal, reinstall, purge and a fresh install.
// The cells share the two Postgres servers and run one after another.
func TestTheModuleDatabaseLivesThroughEveryStepOnEveryLayout(t *testing.T) {
	requireEnabled(t)

	for _, cell := range layouts(t) {
		t.Run(cell.name, func(t *testing.T) { lifecycle(t, cell) })
	}
}

//nolint:funlen // one story, step by step, as the report lists it.
func lifecycle(t *testing.T, cell layout) {
	t.Helper()

	d := cell.deploy(t)
	srv := d.start(t)

	t.Log("install 1.0.0: the module migrates to version 3 and serves")

	first := srv.installReady(t, release100, cell.choice)
	if first.SchemaVersion != 3 || len(first.Migrations) != 4 {
		t.Fatalf("the first install reported version %d and %v", first.SchemaVersion, first.Migrations)
	}

	ns := first.Namespace
	srv.addNote(t, ns, "PETG spool on the left shelf")
	srv.addNote(t, ns, "ASA spool for the enclosure")

	if held := srv.notes(t, ns); len(held) != 2 || held[0].Words != 6 {
		t.Fatalf("the notes are %+v", held)
	}

	t.Log("restart: the notes are kept and nothing is migrated again")

	srv.stop(t)
	srv = d.start(t)

	eventually(t, "the module serving after the restart", func() bool { return srv.schema(t).State == "ready" })

	if state := srv.schema(t); state.Version != 3 || len(state.Applied) != 0 {
		t.Fatalf("after the restart the schema is %+v", state)
	}

	if held := srv.notes(t, ns); len(held) != 2 {
		t.Fatalf("after the restart the notes are %+v", held)
	}

	t.Log("update to 1.1.0: a schema and a data migration, applied once")

	second := srv.installReady(t, release110, "")
	if second.SchemaVersion != 4 || !slices.Equal(second.Migrations,
		[]string{"0004_note_title.sql", "0004_fill_titles.go"}) {
		t.Fatalf("the update reported version %d and %v", second.SchemaVersion, second.Migrations)
	}

	db := cell.open(t, d)
	if titles := column(t, db, "title"); !slices.Equal(titles, []string{"PETG", "ASA"}) {
		t.Fatalf("the step filled the titles %v", titles)
	}

	t.Log("update to a broken 1.2.0: taken back, the database as it was")

	srv.install(t, release120bad, "", true).wantCode(t, http.StatusConflict, "MODULE_UPDATE_ROLLED_BACK")

	if held := srv.notes(t, ns); len(held) != 2 {
		t.Fatalf("after the rollback the notes are %+v", held)
	}

	if _, err := db.ExecContext(context.Background(), "SELECT tags FROM note"); err == nil {
		t.Fatal("the column of the failed migration is in the database")
	}

	if version := highest(t, db); version != 4 {
		t.Fatalf("after the failed update the ledger holds version %d", version)
	}

	t.Log("update to a fixed 1.2.0")

	third := srv.installReady(t, release120, "")
	if third.SchemaVersion != 5 || len(third.Migrations) != 2 {
		t.Fatalf("the fixed update reported version %d and %v", third.SchemaVersion, third.Migrations)
	}

	if tags := column(t, db, "tags"); !slices.Equal(tags, []string{"kept", "kept"}) {
		t.Fatalf("the fixed step wrote %v", tags)
	}

	t.Log("downgrade to 1.0.0: below the compat floor, refused, 1.2.0 runs again")

	refused := srv.install(t, release100, "", true).wantCode(t, http.StatusConflict, "MODULE_UPDATE_ROLLED_BACK")
	if !strings.Contains(string(refused.body), "migrated by a newer release") {
		t.Fatalf("the downgrade was refused for another reason: %s", refused.body)
	}

	if state := srv.schema(t); state.Version != 5 {
		t.Fatalf("after the refused downgrade the schema is %+v", state)
	}

	t.Log("removal: the database is kept and listed")

	srv.call(t, http.MethodDelete, "/api/v1/modules/"+ns, "").want(t, http.StatusNoContent)

	left := srv.left(t)
	if len(left) != 1 || left[0]["instance"] != "sink:"+moduleName || left[0]["why"] != "removed" {
		t.Fatalf("the databases left behind are %v", left)
	}

	t.Log("reinstall: the data is back, nothing migrated")

	back := srv.installReady(t, release120, "")
	if back.SchemaVersion != 5 || len(back.Migrations) != 0 {
		t.Fatalf("the reinstall reported version %d and %v", back.SchemaVersion, back.Migrations)
	}

	if held := srv.notes(t, ns); len(held) != 2 {
		t.Fatalf("after the reinstall the notes are %+v", held)
	}

	if left = srv.left(t); len(left) != 0 {
		t.Fatalf("the database taken back is still listed: %v", left)
	}

	t.Log("removal and purge: refused without confirm, then done")

	srv.call(t, http.MethodDelete, "/api/v1/modules/"+ns, "").want(t, http.StatusNoContent)
	_ = db.Close()

	srv.call(t, http.MethodDelete, "/api/v1/modules/databases/sink/"+moduleName+"?confirm=notes", "").
		wantCode(t, http.StatusUnprocessableEntity, "REQUEST_INVALID")

	purged := srv.call(t, http.MethodDelete,
		"/api/v1/modules/databases/sink/"+moduleName+"?confirm=sink:"+moduleName, "").want(t, http.StatusOK)
	if !strings.Contains(string(purged.body), `"done":"`+cell.done+`"`) {
		t.Fatalf("the purge answered %s, want %s", purged.body, cell.done)
	}

	cell.purged(t, d)

	if left = srv.left(t); len(left) != 0 {
		t.Fatalf("the purged database is still listed: %v", left)
	}

	t.Log("fresh install: empty, unless the database was the operator's")

	fresh := srv.installReady(t, release120, cell.choice)
	held := srv.notes(t, ns)

	switch {
	case cell.kept && (len(held) != 2 || len(fresh.Migrations) != 0):
		t.Fatalf("the operator's database lost its notes: %+v, migrations %v", held, fresh.Migrations)
	case !cell.kept && (len(held) != 0 || len(fresh.Migrations) != 8):
		t.Fatalf("the fresh install found %+v and applied %v", held, fresh.Migrations)
	}
}
