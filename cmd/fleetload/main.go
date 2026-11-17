// Command fleetload measures what a whole fleet costs the whole server.
//
// It starts the real server in a child process and loads it as a site does:
// simulated printers that register and report, readers on the event stream,
// and a set of grants. The printers are internal/printer/simulator, which
// costs two goroutines and one socket each.
//
// The server is a child process so that its CPU and heap are its own. The
// child answers /stat with runtime/metrics counters and serves net/http/pprof
// on a separate address.
//
//	fleetload -printers 400 -subscribers 8 -duration 10m
//	fleetload -printers 400 -engine postgres -dsn "postgres://..."
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// roleServer is the -role value that makes this binary run as the server.
const roleServer = "server"

// options is the whole command line.
type options struct {
	role        string
	api         string
	admin       string
	dir         string
	engine      string
	dsn         string
	loglevel    string
	model       string
	printers    int
	maxprocs    int
	memlimit    int64
	subscribers int
	grants      int
	cadence     time.Duration
	duration    time.Duration
	settle      time.Duration
	ramp        time.Duration
	profile     time.Duration
	out         string
	history     bool
	keep        bool
}

// The defaults: four hundred printers that report four times a second.
const (
	defaultPrinters    = 400
	defaultSubscribers = 8
	defaultGrants      = 200
	defaultCadence     = 250 * time.Millisecond
	defaultDuration    = 2 * time.Minute
	defaultSettle      = 3 * time.Minute
	defaultRamp        = 20 * time.Second
	defaultProfile     = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fleetload:", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.role == roleServer {
		return serveRole(ctx, opts)
	}

	return drive(ctx, opts)
}

func parse() options {
	var opts options

	flag.StringVar(&opts.role, "role", "", "run as the server child rather than the driver")
	flag.StringVar(&opts.api, "api", "127.0.0.1:18080", "the address the core serves its API on")
	flag.StringVar(&opts.admin, "admin", "127.0.0.1:18081", "the address the child answers /stat and pprof on")
	flag.StringVar(&opts.dir, "dir", "", "the working directory for the config, the graph and the records")
	flag.StringVar(&opts.engine, "engine", "sqlite", "the database engine: sqlite or postgres")
	flag.StringVar(&opts.dsn, "dsn", "", "the PostgreSQL connection string, when the engine is postgres")
	flag.StringVar(&opts.loglevel, "loglevel", "error",
		"the server's log level, raised for a run whose finding is a line the server writes")
	flag.StringVar(&opts.model, "model", "MK4", "the printer model the fleet reports")
	flag.IntVar(&opts.printers, "printers", defaultPrinters, "how many printers connect and report")
	flag.IntVar(&opts.maxprocs, "maxprocs", 0, "cap the server's GOMAXPROCS, to stand in for a smaller machine")
	flag.Int64Var(&opts.memlimit, "memlimit", 0, "the server's GOMEMLIMIT in bytes, zero to leave it unset")
	flag.IntVar(&opts.subscribers, "subscribers", defaultSubscribers, "how many readers watch /api/v1/events")
	flag.IntVar(&opts.grants, "grants", defaultGrants, "how many device grants to write before measuring")
	flag.DurationVar(&opts.cadence, "cadence", defaultCadence, "the telemetry period of one printer")
	flag.DurationVar(&opts.duration, "duration", defaultDuration, "how long the steady state is measured for")
	flag.DurationVar(&opts.settle, "settle", defaultSettle, "how long the fleet has to finish connecting")
	flag.DurationVar(&opts.ramp, "ramp", defaultRamp, "spread the fleet's arrival over this long, zero for all at once")
	flag.DurationVar(&opts.profile, "profile", defaultProfile, "how long the CPU profile runs for")
	flag.StringVar(&opts.out, "out", "", "the directory the profiles and the report are written to")
	flag.BoolVar(&opts.history, "history", false, "enable the history provider")
	flag.BoolVar(&opts.keep, "keep", false, "keep the working directory after the run")

	flag.Parse()

	return opts
}

// errNoDir reports the server role started without -dir.
var errNoDir = errors.New("the server role needs -dir")
