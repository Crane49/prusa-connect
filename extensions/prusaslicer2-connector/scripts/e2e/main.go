// Command e2e drives a running demo server with this module installed, the
// way PrusaSlicer 2 drives a printer with PrusaLink: every request carries the
// API key in X-Api-Key, and the paths are the PrusaLink paths under one
// printer's address. scripts/check.sh runs it once per check, against a demo
// over HTTP and again over HTTPS.
//
//	e2e <check> -base <url> -password <admin password> -state <file> [-ca <file>]
//
// Each check reads and writes the state file, so a later check uses the
// printers and the keys an earlier one found. The paths, headers and bodies
// are written out here as PrusaSlicer sends them, and not taken from the
// module's own packages, so a change that moves both sides at once still
// fails here.
//
// The checks, in the order the gate runs them:
//
//	ready    the demo serves, its printers are connected and the module answers
//	keys     the demo account mints an operate key and a read key
//	link     the connection test and the storage list, with and without a key
//	print    a file over 1 MiB with Print-After-Upload reaches the printer
//	keep     a file without Print-After-Upload lands in the local storage
//	form     the form older releases send starts a print on another printer
//	refused  the requests the module refuses, with their status codes
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Exit codes.
const (
	exitOK      = 0
	exitFailed  = 1
	exitUsage   = 2
	checkBudget = 3 * time.Minute
)

// mount is where the host serves the module's routes.
const mount = "/api/v1/modules/com.github.aux.prusa3d.prusaslicer2-connector"

// The demo's printer models the checks use.
const (
	modelCoreOne = "CoreOne"
	modelMK4S    = "MK4S"
	modelXL      = "XL"
)

// settings are the flags every check takes.
type settings struct {
	base     string
	ca       string
	state    string
	password string
}

// gate is one run of one check.
type gate struct {
	settings

	transport *http.Transport
	held      *state
}

// state is what one check leaves for the next.
type state struct {
	// Printers maps a printer model, such as XL, to its id.
	Printers map[string]string `json:"printers"`
	// Operate is an API credential with the operate scope, and Read one
	// with the read scope, both of the demo account.
	Operate string `json:"operate"`
	Read    string `json:"read"`
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: e2e ready|keys|link|print|keep|form|refused [flags]")

		return exitUsage
	}

	name := args[0]
	flags := flag.NewFlagSet(name, flag.ContinueOnError)

	var given settings

	flags.StringVar(&given.base, "base", "", "the demo's address, such as https://127.0.0.1:18743")
	flags.StringVar(&given.ca, "ca", "", "the PEM file of the CA that signed the demo's certificate")
	flags.StringVar(&given.state, "state", "", "the file the checks share")
	flags.StringVar(&given.password, "password", "", "the password of the demo account")

	if err := flags.Parse(args[1:]); err != nil {
		return exitUsage
	}

	g, err := newGate(given)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())

		return exitUsage
	}

	checks := map[string]func(context.Context) error{
		"ready": g.ready, "keys": g.keys, "link": g.link, "print": g.print,
		"keep": g.keep, "form": g.form, "refused": g.refused,
	}

	check, ok := checks[name]
	if !ok {
		fmt.Fprintln(os.Stderr, "no such check: "+name)

		return exitUsage
	}

	ctx, cancel := context.WithTimeout(context.Background(), checkBudget)
	defer cancel()

	if err = check(ctx); err != nil {
		fmt.Fprintln(os.Stderr, name+": "+err.Error())

		return exitFailed
	}

	if err = g.save(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())

		return exitFailed
	}

	return exitOK
}

func newGate(given settings) (*gate, error) {
	if given.base == "" || given.state == "" || given.password == "" {
		return nil, errors.New("-base, -state and -password are required")
	}

	g := &gate{settings: given, held: &state{Printers: map[string]string{}}}

	base, err := url.Parse(given.base)
	if err != nil {
		return nil, fmt.Errorf("read -base: %w", err)
	}

	var tlsConfig *tls.Config

	if given.ca != "" {
		pem, readErr := os.ReadFile(filepath.Clean(given.ca))
		if readErr != nil {
			return nil, fmt.Errorf("read -ca: %w", readErr)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("-ca holds no PEM certificate")
		}

		tlsConfig = &tls.Config{RootCAs: pool, ServerName: base.Hostname(), MinVersion: tls.VersionTLS12}
	}

	g.transport = &http.Transport{TLSClientConfig: tlsConfig}

	raw, err := os.ReadFile(filepath.Clean(given.state))
	if errors.Is(err, os.ErrNotExist) {
		return g, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read the state: %w", err)
	}

	if err = json.Unmarshal(raw, g.held); err != nil {
		return nil, fmt.Errorf("read the state: %w", err)
	}

	return g, nil
}

func (g *gate) save() error {
	raw, err := json.MarshalIndent(g.held, "", "  ")
	if err != nil {
		return fmt.Errorf("write the state: %w", err)
	}

	if err = os.WriteFile(g.state, raw, 0o600); err != nil {
		return fmt.Errorf("write the state: %w", err)
	}

	return nil
}

// printer is the id of the demo's printer of a model.
func (g *gate) printer(model string) (string, error) {
	id, ok := g.held.Printers[model]
	if !ok {
		return "", fmt.Errorf("the demo has no %s; run the ready check first", model)
	}

	return id, nil
}

// address is the address a person types into PrusaSlicer for one printer,
// without the server: the module's mount and the printer.
func address(printer string) string {
	return mount + "/printers/" + url.PathEscape(printer) + "/"
}

// poll calls try once a second until it answers true, an error, or the
// context ends.
func poll(ctx context.Context, what string, try func() (bool, error)) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	var last error

	for {
		done, err := try()
		if done {
			return err
		}

		last = err

		select {
		case <-ctx.Done():
			if last != nil {
				return fmt.Errorf("%s: %w", what, last)
			}

			return fmt.Errorf("%s: %w", what, ctx.Err())
		case <-tick.C:
		}
	}
}
