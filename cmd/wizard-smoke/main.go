// Command wizard-smoke walks the first-run wizard over the API against one
// release binary.
//
// It starts the binary in an empty folder and does what the wizard does in a
// browser: it makes the certificates, turns on HTTPS and TLS for the printers'
// own port, declares a storage, the history and an MQTT sink, creates the
// first administrator and confirms. It then checks the server the binary
// restarts into, stops it, starts it again and checks that the setup held.
//
// The workflow .github/workflows/wizard-smoke.yml builds this command for every
// runner next to the binaries it tests, so a runner needs no toolchain.
//
//	wizard-smoke -binary bin/prusa_connect_amd64
//	wizard-smoke -binary bin/prusa_connect_armv7 -docker-platform linux/arm/v7 -docker-image debian:...@sha256:...
//	wizard-smoke -binary bin/prusa_connect_minimal_amd64 -no-web-app
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

// The ports the server under test takes. The API port is fixed with -addr, so
// the wizard shows it locked and the confirm keeps it.
const (
	apiPort     = 8090
	printerPort = 8443
)

// How long the walk waits for each thing it waits for, and how much of the
// server's log a failed walk prints.
const (
	startLimit     = 90 * time.Second
	resourcesLimit = 30 * time.Second
	stopLimit      = 30 * time.Second
	tlsLimit       = 20 * time.Second
	requestLimit   = 15 * time.Second
	pollEvery      = 250 * time.Millisecond
	logTail        = 20000
	bodyLimit      = 1 << 20
)

// errFailed reports a walk in which at least one check failed.
var errFailed = errors.New("at least one check failed")

// options is the whole command line.
type options struct {
	binary         string
	dockerPlatform string
	dockerImage    string
	noWebApp       bool
	keep           bool
}

func main() {
	os.Exit(exitCode())
}

func exitCode() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "wizard-smoke: "+err.Error())

		return 1
	}

	return 0
}

func run(ctx context.Context, args []string, out io.Writer) error {
	opts, err := parse(args)
	if err != nil {
		return err
	}

	folder, err := os.MkdirTemp("", "wizard-smoke-")
	if err != nil {
		return fmt.Errorf("make a folder for the server: %w", err)
	}

	binary := filepath.Join(folder, filepath.Base(opts.binary))
	if err = copyBinary(opts.binary, binary); err != nil {
		return err
	}

	where := runtime.GOOS + " " + runtime.GOARCH
	if opts.dockerPlatform != "" {
		where = opts.dockerPlatform
	}

	fmt.Fprintf(out, "     %s in %s on %s\n", filepath.Base(binary), folder, where)

	srv := newServer(binary, folder, opts.dockerPlatform, opts.dockerImage)
	report := newChecks(out)

	walkErr := walk(ctx, srv, report, !opts.noWebApp)

	log := srv.log(ctx)
	srv.stop(ctx)

	if report.failed() || walkErr != nil {
		fmt.Fprintf(out, "\n----- the server's log -----\n%s\n", tail(log, logTail))
	}

	if !opts.keep {
		_ = os.RemoveAll(folder) // a temporary folder left behind is the runner's to clean
	}

	return report.result(walkErr)
}

func parse(args []string) (options, error) {
	var opts options

	set := flag.NewFlagSet("wizard-smoke", flag.ContinueOnError)
	set.StringVar(&opts.binary, "binary", "", "the server binary to test")
	set.StringVar(&opts.dockerPlatform, "docker-platform", "", "run the binary in a container of this platform")
	set.StringVar(&opts.dockerImage, "docker-image", "", "the container image, pinned by digest")
	set.BoolVar(&opts.noWebApp, "no-web-app", false, "the build carries no web app")
	set.BoolVar(&opts.keep, "keep", false, "keep the server's folder")

	if err := set.Parse(args); err != nil {
		return options{}, fmt.Errorf("read the command line: %w", err)
	}

	if opts.binary == "" {
		return options{}, errors.New("-binary names no file")
	}

	if (opts.dockerPlatform == "") != (opts.dockerImage == "") {
		return options{}, errors.New("-docker-platform and -docker-image go together")
	}

	return opts, nil
}

// copyBinary puts the binary into the server's folder with the execute bit,
// which a downloaded artifact loses.
func copyBinary(from, to string) error {
	body, err := os.ReadFile(from)
	if err != nil {
		return fmt.Errorf("read %s: %w", from, err)
	}

	//nolint:gosec // the server binary has to be executable, by the container's user too
	if err = os.WriteFile(to, body, 0o755); err != nil {
		return fmt.Errorf("write %s: %w", to, err)
	}

	return nil
}

func tail(text string, size int) string {
	if len(text) <= size {
		return text
	}

	return text[len(text)-size:]
}
