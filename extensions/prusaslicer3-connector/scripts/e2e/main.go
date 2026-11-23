// Command e2e drives a running demo server with this module installed, the
// way PrusaSlicer 3 in offline mode drives it: it signs in through the core's
// app pairing and sends the Prusa Connect requests to the module. scripts/check.sh
// runs it once per check, against a demo over HTTP and again over HTTPS.
//
//	e2e <check> -base <url> -password <admin password> -state <file> [-ca <file>]
//
// Each check reads and writes the state file, so a later check uses the
// people and tokens an earlier one made. The paths and bodies are
// written out here as PrusaSlicer sends them, and not taken from the module's
// own packages, so a change that moves both sides at once still fails here.
//
// The checks, in the order the gate runs them:
//
//	ready     the demo serves, its printers are connected and the module answers
//	people    adds alice, bob, carol and dave with their grants
//	signin    the app pairing as each person, me, and the page session the
//	          page inside PrusaSlicer opens
//	printers  the printer list each person reads, and its shape
//	register  the upload requests the module refuses, with the cloud's bodies
//	upload    a file over 1 MiB reaches the printer, and the printer list
//	          shows it printing
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
const mount = "/api/v1/modules/com.github.aux.prusa3d.prusaslicer3-connector"

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
	// People are the people the gate added, by username.
	People map[string]*person `json:"people"`
}

// person is one person the gate added and signed in to the slicer.
type person struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Display  string `json:"display"`
	Subject  string `json:"subject"`
	// ID is the id the me reply answers.
	ID int64 `json:"id"`
	// Access is the credential the slicer collected, in the JWT shape the
	// slicer sends as its bearer, and Bare the same credential bare.
	Access string `json:"access"`
	Bare   string `json:"bare"`
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: e2e ready|people|signin|printers|register|upload [flags]")

		return exitUsage
	}

	name := args[0]
	flags := flag.NewFlagSet(name, flag.ContinueOnError)

	var given settings

	flags.StringVar(&given.base, "base", "", "the demo's address, such as https://127.0.0.1:18643")
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
		"ready": g.ready, "people": g.people, "signin": g.signIn, "printers": g.printers,
		"register": g.register, "upload": g.upload,
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

	g := &gate{settings: given, held: &state{Printers: map[string]string{}, People: map[string]*person{}}}

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

// someone is a person the people check added.
func (g *gate) someone(username string) (*person, error) {
	held, ok := g.held.People[username]
	if !ok || held.Access == "" {
		return nil, fmt.Errorf("%s was not signed in; run the people and signin checks first", username)
	}

	return held, nil
}

// printer is the id of the demo's printer of a model.
func (g *gate) printer(model string) (string, error) {
	id, ok := g.held.Printers[model]
	if !ok {
		return "", fmt.Errorf("the demo has no %s; run the people check first", model)
	}

	return id, nil
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
