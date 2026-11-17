// Command dialin runs one simulated printer against a core that is already
// serving, so the API can be driven by hand against a real binary.
//
// It lives under testdata so `go list ./...` does not see it: it is a driver
// for a person at a terminal, not a package anything builds against.
//
//	go run ./internal/printer/simulator/testdata/dialin -url http://127.0.0.1:8099 -model MK4
//
// It registers, connects and stays up until it is interrupted, reporting
// telemetry the way a printer on a bench does.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

func main() {
	baseURL := flag.String("url", "http://127.0.0.1:8099", "the core's API address")
	model := flag.String("model", "MK4", "the printer model, matched against the capability profiles")
	serial := flag.String("serial", "SIM-0001", "the serial number registration carries")

	flag.Parse()

	if err := run(*baseURL, *model, *serial); err != nil {
		fmt.Fprintln(os.Stderr, "dialin: "+err.Error())
		os.Exit(1)
	}
}

func run(baseURL, model, serial string) error {
	printer, err := simulator.New(simulator.Config{Model: model, Serial: serial, BaseURL: baseURL})
	if err != nil {
		return fmt.Errorf("build the printer: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("dialling %s as %s %s, fingerprint %s\n", baseURL, model, serial, printer.Fingerprint())

	if err = printer.RegisterAndRun(ctx); err != nil && ctx.Err() == nil {
		return fmt.Errorf("run the printer: %w", err)
	}

	return nil
}
