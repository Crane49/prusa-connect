// Command service-plugin is an example extension the host launches as its own
// process. It shows each host API a module can call, one route per call.
//
// The wire is specified in docs/wire/plugin-wire-protocol.md.
// examples/wire-protocol/child.js is the same contract in another language.
//
// Build it for every platform, name each build <type>_<GOOS>_<GOARCH>, put
// them in one directory, and declare an instance against that directory:
//
//	resources:
//	  sink:
//	    example:
//	      type: service-plugin
//	      runtime: service
//	      enabled: true
//	      service:
//	        path: plugins/service-plugin
//	        checksum: sha256:<64 hex over the file for this platform>
//	        contract_major: 1
//
// The host hashes the file before it runs it. The channel is standard input
// and output. Log to standard error: the host copies it into its own log.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fileapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// What this child reports when the host asks it what it is.
const (
	name    = "example-service"
	release = extension.Version("1.0.0")
)

// openAPI describes the routes this module serves, with paths relative to the
// mount. The host checks it at launch and serves it under the namespace.
//
//go:embed openapi.json
var openAPI []byte

// workBeforeAWrite is how long the /state-late route works before it writes a
// record, so every concurrent request is in the handler before any calls the
// host.
const workBeforeAWrite = 100 * time.Millisecond

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

	// The routes call back into the host, so the clients need the session
	// first. The map is filled below, before Serve reads it.
	serves := wirerpc.Methods{}

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{
		Role:    wirerpc.RoleChild,
		Methods: serves,
	})
	if err != nil {
		return fmt.Errorf("build the session: %w", err)
	}

	// The host's key/value store. The host sets the namespace.
	records := stateapi.NewClient(conn)

	// The host's event stream. The module names an endpoint and a payload.
	// The host sets the origin and the event type.
	events := busapi.NewClient(conn)

	// The host's fleet. The host authorizes every call.
	fleet := deviceapi.NewClient(conn)

	maps.Copy(serves, methods(records, events, fleet, conn))

	fmt.Fprintf(os.Stderr, "%s %s is serving on stdio as pid %d\n", name, release, os.Getpid())

	if err = conn.Serve(ctx); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// methods is what this child answers. Every child answers plugin.info and
// plugin.shutdown. The shutdown handler does not close the channel: the host
// closes its side after the answer, and Serve ends with standard input.
func methods(
	records stateapi.Records, events *busapi.Client, fleet *deviceapi.Client, conn *wirerpc.Conn,
) wirerpc.Methods {
	// The host calls plugin.info during the launch. A state call made there
	// gets stateapi.ErrNotReady, so read records once the module is serving.
	var early atomic.Pointer[string]

	return wirerpc.Methods{
		wirerpc.MethodInfo: func(ctx context.Context, _ wirerpc.Call) (any, error) {
			said := "the host answered a call made before this module was ready"

			if _, err := records.Get(ctx, "startup/probe"); err != nil {
				said = err.Error()
			}

			early.Store(&said)

			// The OpenAPI fragment goes in this answer. The host publishes
			// it, and refuses a bad one at launch.
			return routesapi.Describe(
				wirerpc.Info{Name: name, Version: release, Contract: wirerpc.Contract}, openAPI)
		},
		wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) {
			fmt.Fprintf(os.Stderr, "%s was asked to stop\n", name)

			return nil, nil
		},

		// The module's HTTP routes. The host serves them under
		// /api/v1/modules/<namespace>/ and authorizes each request first.
		routesapi.MethodServe: routesapi.Serve(routes(records, events, fleet, conn, &early)),

		// The file type this module reads for the core. The host calls it
		// with a read handle for one .demo file.
		fileapi.MethodAnalyze: fileapi.Serve(storageapi.NewClient(conn), analyzeDemo),
	}
}

// limitRoutes are three routes that show the host's limits from outside. A
// real module needs none of them.
func limitRoutes(mux *http.ServeMux, records stateapi.Records, early *atomic.Pointer[string]) {
	// What the host said to the state call made before the module was ready.
	mux.HandleFunc("GET /startup", func(w http.ResponseWriter, _ *http.Request) {
		said := ""
		if held := early.Load(); held != nil {
			said = *held
		}

		answer(w, http.StatusOK, map[string]any{"early_state_call": said})
	})

	// A route that works before it writes a record. While it works it uses
	// one of the host's slots for calls into this module.
	mux.HandleFunc("PUT /state-late/{key...}", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			answer(w, http.StatusBadRequest, map[string]any{"error": err.Error()})

			return
		}

		select {
		case <-time.After(workBeforeAWrite):
		case <-r.Context().Done():
			answer(w, http.StatusRequestTimeout, map[string]any{"error": r.Context().Err().Error()})

			return
		}

		version, err := records.Put(r.Context(), stateapi.Key(r.PathValue("key")), body)
		if err != nil {
			answer(w, statusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, map[string]any{"version": version})
	})

	// n state calls at once, to reach the host's per-module limit. The
	// answer counts the outcomes.
	mux.HandleFunc("POST /state-burst", func(w http.ResponseWriter, r *http.Request) {
		calls, err := strconv.Atoi(r.URL.Query().Get("n"))
		if err != nil || calls <= 0 {
			answer(w, http.StatusBadRequest, map[string]any{"error": "n is a positive number"})

			return
		}

		answer(w, http.StatusOK, burst(r.Context(), records, calls))
	})
}

// busRoutes are two routes that emit events on request. A real module emits
// where it decides that something happened.
func busRoutes(mux *http.ServeMux, events *busapi.Client) {
	// One event. The host refuses an endpoint the operator did not declare.
	// The client has no argument for an event type or an origin.
	mux.HandleFunc("POST /emit", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			answer(w, http.StatusBadRequest, map[string]any{"error": err.Error()})

			return
		}

		endpoint := r.URL.Query().Get("endpoint")
		if endpoint == "" {
			endpoint = "changed"
		}

		if err = events.EmitRaw(r.Context(), endpoint, r.URL.Query().Get("key"), body); err != nil {
			answer(w, busStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, map[string]any{"emitted": endpoint})
	})

	// n events at once, to reach the host's per-module emit limit.
	mux.HandleFunc("POST /emit-burst", func(w http.ResponseWriter, r *http.Request) {
		calls, err := strconv.Atoi(r.URL.Query().Get("n"))
		if err != nil || calls <= 0 {
			answer(w, http.StatusBadRequest, map[string]any{"error": "n is a positive number"})

			return
		}

		answer(w, http.StatusOK, emitBurst(r.Context(), events, calls))
	})
}

// emitBurst publishes calls events at once and counts what each was told.
func emitBurst(ctx context.Context, events *busapi.Client, calls int) map[string]any {
	var (
		sent   atomic.Int64
		busy   atomic.Int64
		failed atomic.Int64
		said   atomic.Pointer[string]
		group  sync.WaitGroup
	)

	group.Add(calls)

	for i := range calls {
		go func() {
			defer group.Done()

			err := events.Emit(ctx, "changed", fmt.Sprintf("burst/%04d", i),
				map[string]any{"n": i})

			switch {
			case err == nil:
				sent.Add(1)
			case errors.Is(err, busapi.ErrBusy):
				busy.Add(1)

				text := err.Error()
				said.Store(&text)
			default:
				failed.Add(1)

				text := err.Error()
				said.Store(&text)
			}
		}()
	}

	group.Wait()

	refusal := ""
	if held := said.Load(); held != nil {
		refusal = *held
	}

	return map[string]any{
		"sent":    sent.Load(),
		"busy":    busy.Load(),
		"failed":  failed.Load(),
		"refusal": refusal,
	}
}

// busStatusFor maps an emit refusal to an HTTP status. The mapping is the
// module's own choice.
func busStatusFor(err error) int {
	switch {
	case errors.Is(err, busapi.ErrNoEndpoint):
		// The operator did not declare this endpoint.
		return http.StatusNotFound
	case errors.Is(err, busapi.ErrDataTooLarge), errors.Is(err, busapi.ErrInvalidData):
		return http.StatusBadRequest
	case errors.Is(err, busapi.ErrNotReady), errors.Is(err, busapi.ErrBusy):
		return http.StatusServiceUnavailable
	}

	return http.StatusBadGateway
}

// routes is a plain http.Handler. The paths have no prefix: /spools here is
// /api/v1/modules/<namespace>/spools to a caller.
func routes(
	records stateapi.Records, events *busapi.Client, fleet *deviceapi.Client, conn *wirerpc.Conn,
	early *atomic.Pointer[string],
) http.Handler {
	mux := http.NewServeMux()

	limitRoutes(mux, records, early)
	busRoutes(mux, events)
	fleetRoutes(mux, fleet)
	storageRoutes(mux, storageapi.NewClient(conn))
	accessRoutes(mux, accessapi.NewClient(conn))
	eventRoutes(mux, conn)
	callRoutes(mux, routesapi.NewClient(conn))

	// The state routes: a key, some bytes, and a version to write against.
	// The host sets the namespace.
	mux.HandleFunc("GET /records/{key...}", func(w http.ResponseWriter, r *http.Request) {
		held, err := records.Get(r.Context(), stateapi.Key(r.PathValue("key")))
		if err != nil {
			answer(w, statusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, map[string]any{
			"value":   string(held.Bytes),
			"version": held.Version,
		})
	})

	mux.HandleFunc("PUT /records/{key...}", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			answer(w, http.StatusBadRequest, map[string]any{"error": err.Error()})

			return
		}

		key := stateapi.Key(r.PathValue("key"))

		version, err := writeRecord(r, records, key, body)
		if err != nil {
			answer(w, statusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, map[string]any{"version": version})
	})

	mux.HandleFunc("DELETE /records/{key...}", func(w http.ResponseWriter, r *http.Request) {
		if err := records.Delete(r.Context(), stateapi.Key(r.PathValue("key"))); err != nil {
			answer(w, statusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, map[string]any{"deleted": r.PathValue("key")})
	})

	mux.HandleFunc("GET /records", func(w http.ResponseWriter, r *http.Request) {
		keys, err := records.List(r.Context(), stateapi.Key(r.URL.Query().Get("prefix")))
		if err != nil {
			answer(w, statusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, map[string]any{"keys": keys})
	})

	// A read, which the host authorizes with view_resource on this instance.
	mux.HandleFunc("GET /spools", func(w http.ResponseWriter, r *http.Request) {
		answer(w, http.StatusOK, map[string]any{
			"spools": []string{"petg-black", "pla-galaxy"},
			// Who is calling, as the host authorized it.
			"principal": routesapi.PrincipalOf(r),
		})
	})

	// A write, which the host authorizes with operate_resource.
	mux.HandleFunc("POST /spools", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			answer(w, http.StatusBadRequest, map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusCreated, map[string]any{
			"created":   string(body),
			"principal": routesapi.PrincipalOf(r),
		})
	})

	// A handler that panics. Only this call fails, and the host keeps serving.
	mux.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) {
		panic("the module panicked while serving")
	})

	// Everything else is a 404 that reports the path the module received.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		answer(w, http.StatusNotFound, map[string]any{"module_saw": r.URL.Path})
	})

	return mux
}

// burst makes calls concurrent writes and counts what each was told. A real
// module would retry the busy ones.
func burst(ctx context.Context, records stateapi.Records, calls int) map[string]any {
	var (
		kept   atomic.Int64
		busy   atomic.Int64
		failed atomic.Int64
		said   atomic.Pointer[string]
		group  sync.WaitGroup
	)

	group.Add(calls)

	for i := range calls {
		go func() {
			defer group.Done()

			_, err := records.Put(ctx, stateapi.Key(fmt.Sprintf("burst/%04d", i)), []byte("x"))

			switch {
			case err == nil:
				kept.Add(1)
			case errors.Is(err, stateapi.ErrBusy):
				busy.Add(1)

				text := err.Error()
				said.Store(&text)
			default:
				failed.Add(1)

				text := err.Error()
				said.Store(&text)
			}
		}()
	}

	group.Wait()

	refusal := ""
	if held := said.Load(); held != nil {
		refusal = *held
	}

	return map[string]any{
		"kept":    kept.Load(),
		"busy":    busy.Load(),
		"failed":  failed.Load(),
		"refusal": refusal,
	}
}

// writeRecord uses CompareAndPut when the caller stated the version it read,
// and Put when it did not. Use CompareAndPut for a key with several writers.
func writeRecord(
	r *http.Request, records stateapi.Records, key stateapi.Key, body []byte,
) (uint64, error) {
	stated := r.URL.Query().Get("version")
	if stated == "" {
		return records.Put(r.Context(), key, body)
	}

	expected, err := strconv.ParseUint(stated, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("the version is not a number: %w", err)
	}

	return records.CompareAndPut(r.Context(), key, expected, body)
}

// statusFor maps a store refusal to an HTTP status. The mapping is the
// module's own choice.
func statusFor(err error) int {
	switch {
	case errors.Is(err, stateapi.ErrNoRecord):
		return http.StatusNotFound
	case errors.Is(err, stateapi.ErrVersion):
		return http.StatusConflict
	case errors.Is(err, stateapi.ErrKey), errors.Is(err, stateapi.ErrValueTooLarge):
		return http.StatusBadRequest
	case errors.Is(err, stateapi.ErrNamespaceFull):
		return http.StatusInsufficientStorage
	case errors.Is(err, stateapi.ErrNotReady), errors.Is(err, stateapi.ErrBusy):
		// Both mean "not at this moment", and a client retries both.
		return http.StatusServiceUnavailable
	}

	return http.StatusBadGateway
}

// answer writes one JSON body.
func answer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(body)
}

// stdio is the channel to the host. Close closes standard input, which ends
// the session.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
