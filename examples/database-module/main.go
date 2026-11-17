// Command database-module is a module that keeps its data in a database of its
// own. It keeps notes, and serves them on two routes.
//
// The manifest asks with needs.database: true, and an approved install writes
// the grant. Written by hand, the grant is in the service block:
//
//	resources:
//	  sink:
//	    notes:
//	      type: database-module
//	      runtime: service
//	      enabled: true
//	      service:
//	        path: plugins/database-module
//	        checksum: sha256:<64 hex over the file for this platform>
//	        contract_major: 1
//	        database:
//	          engine: sqlite
//
// For SQLite the host sends the path of database.sqlite in the module's data
// directory. For Postgres the declaration names engine: postgres and a dsn.
//
// The host sends the grant in plugin.configure. The module opens the database
// and applies its migrations inside that call, so it reports ready only on a
// schema it has applied. A migration that fails refuses the start.
package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"

	// The two drivers this module registers. sdk/moduledb imports none.
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/moduledb"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// name must match the name in the release's manifest. The host checks it.
const name = "example-database"

// version must match the version in the release's manifest. A release build
// sets it with -ldflags "-X main.version=1.1.0".
//
//nolint:gochecknoglobals // a value the linker writes, read once by info.
var version = "1.0.0"

// maxNoteBytes bounds one request body.
const maxNoteBytes = 64 << 10

// openAPI describes the two routes, with paths relative to the mount.
//
//go:embed openapi.json
var openAPI []byte

// migrations is the module's schema, one directory per engine.
//
//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrations embed.FS

// schema is what sdk/moduledb applies: the files, and the steps written in
// Go (steps.go). Compat stays at zero while every migration only adds.
//
//nolint:gochecknoglobals // a fixed description of the embedded files.
var schema = moduledb.Schema{Migrations: migrations, Steps: steps}

// errNoDatabase refuses a start without a database grant.
var errNoDatabase = errors.New("the operator granted this module no database. Approve its needs.database request")

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	line, err := wire.Handshake{
		Protocol: wire.ProtocolMajor,
		MinMinor: wire.MinMinor,
		MaxMinor: wire.MaxMinor,
		Address:  wire.StdioAddress,
	}.Line()
	if err != nil {
		return fmt.Errorf("render the handshake: %w", err)
	}

	if _, err = os.Stdout.WriteString(line); err != nil {
		return fmt.Errorf("write the handshake: %w", err)
	}

	var held notes

	defer held.close()

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{
		Role:    wirerpc.RoleChild,
		Methods: held.methods(),
	})
	if err != nil {
		return fmt.Errorf("build the session: %w", err)
	}

	if err = conn.Serve(ctx); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// notes is the module's state: the database, once plugin.configure opened it.
type notes struct {
	db atomic.Pointer[sql.DB]
}

func (n *notes) methods() wirerpc.Methods {
	return wirerpc.Methods{
		wirerpc.MethodInfo: func(context.Context, wirerpc.Call) (any, error) {
			return routesapi.Describe(wirerpc.Info{
				Name:     name,
				Version:  extension.Version(version),
				Contract: wirerpc.Contract,
				Serves:   []wirerpc.Method{wirerpc.MethodConfigure},
			}, openAPI)
		},
		wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) {
			n.close()

			return nil, nil
		},
		wirerpc.MethodConfigure: wirerpc.ConfigureReporting(n.configure),
		routesapi.MethodServe:   routesapi.Serve(n.routes()),
	}
}

// configure opens the granted database and migrates it before it answers. It
// answers the schema version, which the host shows the operator.
func (n *notes) configure(ctx context.Context, _ struct{}, grants wirerpc.Grants) (wirerpc.Configured, error) {
	if grants.Database == nil {
		return wirerpc.Configured{}, errNoDatabase
	}

	db, report, err := moduledb.Open(ctx, *grants.Database, schema)
	if err != nil {
		// The cause first: the host shows this line where it may be cut short.
		return wirerpc.Configured{}, fmt.Errorf("%w (the database is %s)", err, grants.Database)
	}

	if old := n.db.Swap(db); old != nil {
		_ = old.Close() // a second configure replaces the handle, and the old one has nothing left to say
	}

	fmt.Fprintf(os.Stderr, "%s keeps its notes in %s at schema version %d\n", name, grants.Database, report.Version)

	return wirerpc.Configured{Database: report.State()}, nil
}

func (n *notes) close() {
	if db := n.db.Swap(nil); db != nil {
		_ = db.Close() // the process is ending, and nothing reads the answer
	}
}

// note is one row, as the routes answer it.
type note struct {
	ID    int64  `json:"id"`
	Text  string `json:"text"`
	Words int    `json:"words"`
}

func (n *notes) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /notes", func(w http.ResponseWriter, r *http.Request) {
		db := n.db.Load()
		if db == nil {
			answer(w, http.StatusServiceUnavailable, map[string]any{"error": errNoDatabase.Error()})

			return
		}

		held, err := listNotes(r.Context(), db)
		if err != nil {
			answer(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, map[string]any{"notes": held})
	})

	mux.HandleFunc("POST /notes", func(w http.ResponseWriter, r *http.Request) {
		db := n.db.Load()
		if db == nil {
			answer(w, http.StatusServiceUnavailable, map[string]any{"error": errNoDatabase.Error()})

			return
		}

		var body struct {
			Text string `json:"text"`
		}

		if err := json.NewDecoder(io.LimitReader(r.Body, maxNoteBytes)).Decode(&body); err != nil ||
			strings.TrimSpace(body.Text) == "" {
			answer(w, http.StatusBadRequest, map[string]any{"error": "the body carries no text"})

			return
		}

		kept, err := addNote(r.Context(), db, body.Text)
		if err != nil {
			answer(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusCreated, kept)
	})

	return mux
}

func listNotes(ctx context.Context, db *sql.DB) ([]note, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, text, words FROM note ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read the notes: %w", err)
	}

	defer func() { _ = rows.Close() }() // rows.Err below reports what this would

	held := []note{}

	for rows.Next() {
		var one note
		if err = rows.Scan(&one.ID, &one.Text, &one.Words); err != nil {
			return nil, fmt.Errorf("read the notes: %w", err)
		}

		held = append(held, one)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read the notes: %w", err)
	}

	return held, nil
}

// addNote writes one note. SQLite and Postgres both take RETURNING and $1
// placeholders, so one query serves both.
func addNote(ctx context.Context, db *sql.DB, text string) (note, error) {
	kept := note{Text: text, Words: len(strings.Fields(text))}
	written := time.Now().UTC().Format(time.RFC3339)

	query := `INSERT INTO note (text, written_at, words) VALUES ($1, $2, $3) RETURNING id`
	if err := db.QueryRowContext(ctx, query, text, written, kept.Words).Scan(&kept.ID); err != nil {
		return note{}, fmt.Errorf("keep the note: %w", err)
	}

	return kept, nil
}

// answer writes one JSON body.
func answer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(body)
}

// stdio is the pair the host handed over.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
