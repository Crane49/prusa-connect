// Command printersim runs a fleet of simulated printers against a running
// server.
//
// Each printer is a goroutine that performs the frozen registration handshake,
// connects, streams telemetry and answers commands for whatever model the
// capability profiles cover. Nothing about a model is written here: the
// vocabulary comes from internal/printer/simulator, which projects it from
// internal/printer/profile/profiles.json.
//
// This is the manual lane. The lane that gates a pull request drives the same
// package over net.Pipe inside a testing/synctest bubble, because a real socket
// leaves virtual time.
//
//	printersim -models
//	printersim -url http://localhost:8080 -model MK4 -count 400
//	printersim -count 4 -cameras=false
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/tools/camerasim"
)

// cameraFPS is how fast each simulated camera renders. Five frames a second
// reads as moving in a browser and costs nothing beside the fleet it sits with.
const cameraFPS = 5

type options struct {
	url      string
	model    string
	firmware string
	serial   string
	count    int
	cameras  bool
	idle     time.Duration
	printing time.Duration
	printFor time.Duration
	duration time.Duration
	verbose  bool
	list     bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "printersim:", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parse()

	if opts.list {
		models, err := simulator.Models()
		if err != nil {
			return err
		}

		fmt.Fprintln(os.Stdout, strings.Join(models, "\n"))

		return nil
	}

	if opts.count < 1 {
		return errors.New("-count must be at least 1")
	}

	level := slog.LevelInfo
	if opts.verbose {
		level = slog.LevelDebug
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.duration > 0 {
		timed, cancel := context.WithTimeout(ctx, opts.duration)
		defer cancel()

		ctx = timed
	}

	printers, err := build(opts, log)
	if err != nil {
		return err
	}

	defer stopCameras(startCameras(ctx, opts, printers))

	log.InfoContext(ctx, "starting simulated printers",
		slog.Int("count", len(printers)),
		slog.String("model", printers[0].Model().Name),
		slog.String("device_type_id", printers[0].Model().DeviceTypeID),
		slog.Int("commands", len(printers[0].Model().Commands)),
		slog.String("server", opts.url))

	failures := start(ctx, printers, log)

	report(printers, failures)

	return nil
}

func parse() options {
	var opts options

	flag.StringVar(&opts.url, "url", "http://localhost:8080", "the server to connect to")
	flag.StringVar(&opts.model, "model", "MK4", "printer model, one of -models")
	flag.StringVar(&opts.firmware, "firmware", "", "firmware label to report, defaults to the profile floor")
	flag.StringVar(&opts.serial, "serial", "SIM", "serial number prefix")
	flag.IntVar(&opts.count, "count", 1, "how many printers to run")
	flag.BoolVar(&opts.cameras, "cameras", true,
		"serve a simulated camera for each printer, and print the two URLs to declare it under")
	flag.DurationVar(&opts.idle, "idle", 250*time.Millisecond, "telemetry cadence while idle")
	flag.DurationVar(&opts.printing, "printing", time.Second, "telemetry cadence while printing")
	flag.DurationVar(&opts.printFor, "print-time", defaultPrintTime,
		"how long a print takes before it finishes, zero to print until stopped")
	flag.DurationVar(&opts.duration, "duration", 0, "stop after this long, zero to run until interrupted")
	flag.BoolVar(&opts.verbose, "v", false, "log every session and refusal")
	flag.BoolVar(&opts.list, "models", false, "list the models the capability profiles cover and exit")
	flag.Parse()

	return opts
}

// startCameras serves one simulated camera per printer and prints the two URLs
// each is reached at.
//
// It prints rather than declares, and that is the honest half of what this
// command can do. A camera is declared through the resource API, which takes an
// operator's credential, and this command holds none: it carries the printers'
// own registration tokens and nothing else. So it puts the device on a port and
// says where it is, and the operator declares it with one PUT, which is what
// they would do for a real ESP32-CAM on the bench. cmd/demo owns an account, so
// it declares them itself.
//
// A camera that will not start is printed and skipped. A fleet whose printers
// are on the wire is still the fleet worth having.
func startCameras(
	ctx context.Context, opts options, printers []*simulator.Printer,
) []*camerasim.Server {
	if !opts.cameras {
		return nil
	}

	servers := make([]*camerasim.Server, 0, len(printers))

	for _, printer := range printers {
		server, err := camerasim.Start(ctx, camerasim.Options{
			Addr:  "127.0.0.1:0",
			FPS:   cameraFPS,
			Label: printer.Serial(),
			Status: func() camerasim.Status {
				return camerasim.Status{State: printer.State(), Progress: printer.Progress()}
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stdout, "%s has no camera: %s\n", printer.Serial(), err)

			continue
		}

		servers = append(servers, server)

		fmt.Fprintf(os.Stdout, "%s camera snapshot %s stream %s\n",
			printer.Serial(), server.SnapshotURL(), server.StreamURL())
	}

	return servers
}

// stopCameras closes the simulated devices. Nothing else owns them: the core
// owns the declared camera instances, and these are the far end of that wire.
func stopCameras(servers []*camerasim.Server) {
	for _, server := range servers {
		_ = server.Close()
	}
}

// defaultPrintTime is how long a print takes unless -print-time says
// otherwise: the five minutes make demo gives its prints. The simulator's own
// zero prints until somebody stops it, so its progress never moves.
const defaultPrintTime = 5 * time.Minute

// jobFrames is how many telemetry frames a print of that length takes at the
// printing cadence, or zero, which prints until stopped.
func jobFrames(printFor, cadence time.Duration) int {
	if printFor <= 0 || cadence <= 0 {
		return 0
	}

	return max(1, int(printFor/cadence))
}

func build(opts options, log *slog.Logger) ([]*simulator.Printer, error) {
	printers := make([]*simulator.Printer, 0, opts.count)

	for i := range opts.count {
		printer, err := simulator.New(simulator.Config{
			Model:          opts.model,
			Serial:         fmt.Sprintf("%s-%04d", opts.serial, i),
			Firmware:       opts.firmware,
			BaseURL:        opts.url,
			IdlePeriod:     opts.idle,
			PrintingPeriod: opts.printing,
			JobFrames:      jobFrames(opts.printFor, opts.printing),
			Logger:         log,
		})
		if err != nil {
			return nil, err
		}

		printers = append(printers, printer)
	}

	return printers, nil
}

func start(ctx context.Context, printers []*simulator.Printer, log *slog.Logger) int {
	var (
		group    sync.WaitGroup
		mu       sync.Mutex
		failures int
	)

	for _, printer := range printers {
		group.Go(func() {
			err := printer.RegisterAndRun(ctx)
			if err == nil || ctx.Err() != nil {
				return
			}

			mu.Lock()
			failures++
			mu.Unlock()

			log.ErrorContext(ctx, "simulated printer stopped",
				slog.String("serial", printer.Serial()),
				slog.String("error", err.Error()))
		})
	}

	group.Wait()

	return failures
}

func report(printers []*simulator.Printer, failures int) {
	var sessions uint64
	for _, printer := range printers {
		sessions += printer.Sessions()
	}

	fmt.Fprintf(os.Stdout, "%d printers, %d websocket sessions, %d failures\n",
		len(printers), sessions, failures)
}
