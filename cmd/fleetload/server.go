package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app"
)

// registrationHeadroom multiplies the fleet size to get the connector's
// max_registrations, so the registration limit does not cap the fleet.
const registrationHeadroom = 3

// adminReadHeaderTimeout bounds a request on the admin listener.
const adminReadHeaderTimeout = 5 * time.Second

// quitPath is how the driver asks the child to shut down. Windows cannot send
// an interrupt signal to another process.
const quitPath = "/quit"

// serveRole is the child process: the real server, plus an admin listener for
// /stat and pprof. The admin routes are on their own listener, because they
// are not part of the product's API.
func serveRole(ctx context.Context, opts options) error {
	if opts.dir == "" {
		return errNoDir
	}

	path := filepath.Join(opts.dir, "config.yaml")
	if err := os.WriteFile(path, []byte(coreConfig(opts)), 0o600); err != nil {
		return fmt.Errorf("write the core configuration: %w", err)
	}

	if opts.maxprocs > 0 {
		runtime.GOMAXPROCS(opts.maxprocs)
	}

	if opts.memlimit > 0 {
		debug.SetMemoryLimit(opts.memlimit)
	}

	ctx, quit := context.WithCancel(ctx)
	defer quit()

	// The driver has the other end of stdin. When the driver is killed the
	// pipe closes and this child stops, so no orphan keeps the API port.
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		quit()
	}()

	admin := &http.Server{
		Addr: opts.admin,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			adminRoute(w, r, quit)
		}),
		ReadHeaderTimeout: adminReadHeaderTimeout,
	}

	go func() {
		if err := admin.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "fleetload: the admin listener stopped:", err)
		}
	}()

	defer func() { _ = admin.Close() }()

	// The server stays off the local network, whatever address it serves on.
	return app.Run(ctx, app.Options{ConfigPath: path, APIAddr: opts.api, Version: "fleetload", LoopbackOnly: true})
}

// adminRoute dispatches by path. This repository does not allow a ServeMux
// outside the server's router package.
func adminRoute(w http.ResponseWriter, r *http.Request, quit context.CancelFunc) {
	switch {
	case r.URL.Path == statPath:
		writeStat(w, r)
	case r.URL.Path == quitPath:
		w.WriteHeader(http.StatusAccepted)
		quit()
	case r.URL.Path == "/debug/pprof/profile":
		pprof.Profile(w, r)
	case r.URL.Path == "/debug/pprof/trace":
		pprof.Trace(w, r)
	case r.URL.Path == "/debug/pprof/symbol":
		pprof.Symbol(w, r)
	case strings.HasPrefix(r.URL.Path, "/debug/pprof/"):
		pprof.Index(w, r)
	default:
		http.NotFound(w, r)
	}
}

// coreConfig is the server configuration of the run: the defaults, with the
// log level at error so that logging is not what gets measured, and a raised
// registration limit.
func coreConfig(opts options) string {
	engine := opts.engine
	if engine == "" {
		engine = "sqlite"
	}

	level := opts.loglevel
	if level == "" {
		level = "error"
	}

	config := "core:\n" +
		"  api:\n" +
		"    addr: \"" + opts.api + "\"\n" +
		"  log:\n" +
		"    level: " + level + "\n" +
		"    format: text\n" +
		"  site: fleetload\n" +
		"  principal: operator\n" +
		"  authz:\n" +
		"    database: " + slashed(filepath.Join(opts.dir, "authz.db")) + "\n" +
		"  overlay: " + slashed(filepath.Join(opts.dir, "resources.json")) + "\n" +
		"  history:\n" +
		"    enabled: " + boolWord(opts.history) + "\n" +
		"    database: " + slashed(filepath.Join(opts.dir, "history.db")) + "\n" +
		"  connectors:\n" +
		"    prusa-xbuddy:\n" +
		"      type: xbuddy\n" +
		"      config:\n" +
		"        max_registrations: " +
		strconv.Itoa(opts.printers*registrationHeadroom+registrationHeadroom) + "\n" +
		// Admit every printer on arrival, with no approval step.
		"        mode: none\n" +
		"persistence:\n" +
		"  type: " + engine + "\n"

	if engine == "postgres" {
		config += "  postgres:\n    dsn: \"" + opts.dsn + "\"\n"
	}

	return config
}

// slashed returns a path with forward slashes, which are safe in YAML.
func slashed(path string) string { return filepath.ToSlash(path) }

func boolWord(value bool) string {
	if value {
		return "true"
	}

	return "false"
}
