// Command spools is a module with both halves: this process, which serves two
// routes, and a browser bundle under ui/, which draws a page on them.
//
// It tracks filament spools per device. A spool is held in memory for the life
// of the process, because the point of the example is the two halves meeting,
// not where the rows live. examples/service-plugin shows the host's record
// store, and a real inventory would use it.
//
// # The two routes
//
//	GET  /spools   the shelf, and who the host says is asking
//	POST /spools   add one spool, answered with the row that was added
//
// Both are relative to the mount, so a caller reaches them at
// /api/v1/modules/<namespace>/spools. The host decides who may call each one
// and then makes the call: view_resource on the instance for the read,
// operate_resource for the write. This process checks no permission and holds
// no grant.
//
// # What an operator writes
//
// Nothing has to be written. `prusa-connect module add` reads the
// release, installs both halves and writes one declaration, under one namespace
// with one enabled flag. One setting, shelf_size, can be overridden under the
// instance's config block, and module.yaml carries its default. See
// examples/spools/README.md.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// What this child reports when the host asks it what it is.
const (
	name    = "spools"
	release = extension.Version("1.0.0")
)

// openAPI is the fragment describing the two routes, with every path relative
// to the mount. The host holds it to routesapi.ParseFragment at launch and
// serves it rebased under the namespace, which is what cmd/generate-client-sdk
// reads to type this module's surface.
//
//go:embed openapi.json
var openAPI []byte

// maxSpools is how many rows the shelf keeps when the operator writes no
// setting. A module that grows without a bound is a module an operator has to
// restart, and a write over the bound is refused rather than dropping the
// oldest row, because a shelf that forgets a spool silently is worse than one
// that says it is full.
//
// It is the default of the shelf_size setting in module.yaml, and the two are
// the same number on purpose: what the manifest publishes is what this code
// does when nobody overrides it.
const maxSpools = 500

// maxFieldLen bounds every text field a caller writes. The body itself is
// bounded by routesapi.MaxModuleBody, so this only keeps one row readable.
const maxFieldLen = 120

// Spool is one reel on the shelf. Device is the device id it is loaded on, and
// empty for a spool nothing is printing from.
type Spool struct {
	ID       string `json:"id"`
	Material string `json:"material"`
	Colour   string `json:"colour"`
	Device   string `json:"device,omitempty"`
	Grams    int    `json:"grams"`
}

// Settings is what an operator writes under this instance's config block. The
// host merges it over the defaults module.yaml declares and sends the result on
// plugin.configure, before any request reaches this module.
type Settings struct {
	// ShelfSize is how many spools this module keeps.
	ShelfSize int `json:"shelf_size"`
}

// shelf is every spool this module holds. It is guarded by a mutex because the
// host serves several requests into a module at once.
type shelf struct {
	mu   sync.Mutex
	rows []Spool
	// size is how many rows this shelf keeps, from the settings the host sent.
	size int
}

// resize takes the settings the host sent.
func (s *shelf) resize(held Settings) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if held.ShelfSize > 0 {
		s.size = held.ShelfSize
	}
}

// list answers a copy, so a caller cannot hold a slice the next write appends
// to.
func (s *shelf) list() []Spool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.rows)
}

// errShelfFull reports a write over maxSpools.
var errShelfFull = errors.New("the shelf is full")

// errSpoolTaken reports a write under an id the shelf already holds.
var errSpoolTaken = errors.New("the shelf already holds a spool with that id")

// add keeps one spool and reports why it would not.
func (s *shelf) add(row Spool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.rows) >= s.size {
		return fmt.Errorf("%w: %d is the limit", errShelfFull, s.size)
	}

	if slices.ContainsFunc(s.rows, func(held Spool) bool { return held.ID == row.ID }) {
		return fmt.Errorf("%w: %s", errSpoolTaken, row.ID)
	}

	s.rows = append(s.rows, row)

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

// run writes the handshake line and then serves frames until the host closes
// the channel.
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

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{
		Role:    wirerpc.RoleChild,
		Methods: methods(&shelf{rows: starting(), size: maxSpools}),
	})
	if err != nil {
		return fmt.Errorf("build the session: %w", err)
	}

	// The pid is here because the host's log is the only place a person sees
	// this process. Nothing on the wire carries it.
	fmt.Fprintf(os.Stderr, "%s %s is serving on stdio as pid %d\n", name, release, os.Getpid())

	if err = conn.Serve(ctx); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// starting is what the shelf holds before anybody writes to it. A page with an
// empty list on first open cannot show that the read works, so the example
// ships two rows.
//
//nolint:mnd // two sample rows: each number is the weight on that reel and names nothing else.
func starting() []Spool {
	return []Spool{
		{ID: "petg-black", Material: "PETG", Colour: "black", Grams: 740},
		{ID: "pla-galaxy", Material: "PLA", Colour: "galaxy black", Grams: 210},
	}
}

// methods is the lifecycle set every child answers, plus the one call that
// carries this module's own HTTP surface.
func methods(held *shelf) wirerpc.Methods {
	return wirerpc.Methods{
		wirerpc.MethodInfo: func(context.Context, wirerpc.Call) (any, error) {
			// The fragment is attached here rather than served as a route,
			// because the host publishes it at a stable address whether or not
			// the module is serving yet, and a fragment the host would refuse
			// is refused on this call rather than found by a client.
			return routesapi.Describe(
				wirerpc.Info{
					Name:     name,
					Version:  release,
					Contract: wirerpc.Contract,
					// This module reads one setting, so the host sends it.
					Serves: []wirerpc.Method{wirerpc.MethodConfigure},
				}, openAPI)
		},
		wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) {
			fmt.Fprintf(os.Stderr, "%s was asked to stop\n", name)

			return nil, nil
		},
		// The settings the host merged from module.yaml's defaults and this
		// instance's config block. A key this module does not read refuses the
		// start, and the operator reads the reason on the instance.
		wirerpc.MethodConfigure: wirerpc.Configure(func(_ context.Context, settings Settings) error {
			held.resize(settings)

			return nil
		}),
		routesapi.MethodServe: routesapi.Serve(routes(held)),
	}
}

// routes is the surface this module serves, written as any Go program writes
// one. The paths carry no prefix: /spools here is
// /api/v1/modules/<namespace>/spools to a caller.
func routes(held *shelf) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /spools", func(w http.ResponseWriter, r *http.Request) {
		rows := held.list()

		// The device id is a query parameter so a page can draw one printer's
		// shelf without reading the whole of it.
		if device := r.URL.Query().Get("device"); device != "" {
			rows = slices.DeleteFunc(rows, func(row Spool) bool { return row.Device != device })
		}

		answer(w, http.StatusOK, map[string]any{
			"spools": rows,
			// Who the host says is calling. It is the host's answer and not a
			// header this process trusted.
			"principal": routesapi.PrincipalOf(r),
		})
	})

	mux.HandleFunc("POST /spools", func(w http.ResponseWriter, r *http.Request) {
		row, err := spoolOf(r.Body)
		if err != nil {
			refuse(w, err)

			return
		}

		if err = held.add(row); err != nil {
			refuse(w, err)

			return
		}

		answer(w, http.StatusCreated, map[string]any{
			"spool":     row,
			"principal": routesapi.PrincipalOf(r),
		})
	})

	// Everything else is the module's own 404, and the path it reports is what
	// proves the group prefix was taken off before it arrived.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		answer(w, http.StatusNotFound, map[string]any{"module_saw": r.URL.Path})
	})

	return mux
}

// errNoID reports a body with no id in it, which is the one field a caller
// cannot leave out.
var errNoID = errors.New("id says which spool this is, and it is empty")

// errTooLong reports a text field over maxFieldLen.
var errTooLong = errors.New("a field is longer than the module keeps")

// errNegativeGrams reports a weight below zero.
var errNegativeGrams = errors.New("grams is a weight, so it is zero or more")

// spoolOf reads one spool out of a request body and refuses what the shelf
// would not be able to show.
func spoolOf(body io.Reader) (Spool, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return Spool{}, fmt.Errorf("read the body: %w", err)
	}

	var row Spool
	if err = json.Unmarshal(raw, &row); err != nil {
		return Spool{}, fmt.Errorf("the body is not a spool: %w", err)
	}

	row.ID = strings.TrimSpace(row.ID)
	row.Material = strings.TrimSpace(row.Material)
	row.Colour = strings.TrimSpace(row.Colour)
	row.Device = strings.TrimSpace(row.Device)

	if row.ID == "" {
		return Spool{}, errNoID
	}

	for _, field := range []string{row.ID, row.Material, row.Colour, row.Device} {
		if len(field) > maxFieldLen {
			return Spool{}, fmt.Errorf("%w: %d is the limit", errTooLong, maxFieldLen)
		}
	}

	if row.Grams < 0 {
		return Spool{}, errNegativeGrams
	}

	return row, nil
}

// refuse answers one refusal as RFC 9457 names it, which is the shape the
// client SDK reads: a caller gets the module's own code on ApiError.code and
// the sentence below on ApiError.message, rather than the bare status.
//
// The codes are this module's own. The core's registry holds the core's codes,
// and a module's are strings outside it, so they are namespaced by the module's
// short name to stay readable in a log beside them.
func refuse(w http.ResponseWriter, err error) {
	status, code, title := http.StatusBadRequest, "spools/INVALID_SPOOL", "the spool was not accepted"

	switch {
	case errors.Is(err, errSpoolTaken):
		status, code, title = http.StatusConflict, "spools/SPOOL_TAKEN", "that spool is already on the shelf"
	case errors.Is(err, errShelfFull):
		status, code, title = http.StatusInsufficientStorage, "spools/SHELF_FULL", "the shelf is full"
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":      "about:blank",
		"status":    status,
		"code":      code,
		"title":     title,
		"detail":    err.Error(),
		"params":    map[string]any{},
		"retryable": false,
	})
}

// answer writes one JSON body, which is every route above.
func answer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(body)
}

// stdio is the pair the host handed over. Close shuts the reading half, which
// is what ends a session whose host has gone quiet.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
