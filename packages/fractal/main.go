// Command fractal makes this server a node in a Prusa Fractal network. A
// Fractal network is a tree of servers, and the host is the
// prusa-connect server that runs this process as a module.
//
// The process joins the server above this one when the settings name one. It
// accepts servers and printers under this one when the settings have an accept
// block. It registers the printers of this server with the network as Fractal
// interfaces, so the server above can read their state and send them commands.
//
// # Calls from the host
//
// The host calls this process through sdk/linkapi. Status returns the state of
// the link. Approve and Revoke apply the decision an operator made about a
// server below this one. The links page of the host shows the result.
//
// # Routes
//
// The process has no web page. It serves three routes under the mount the host
// gave it, and the host is the only door to them: it decides who is calling and
// which permission the call needs before it forwards the request. GET /nodes
// lists every server and printer below this one. GET /nodes/{path} lists the
// interfaces of one of them, with their values. POST on a method of one of
// those interfaces sends a command down the tree. openapi.json describes all
// three.
//
// # Permissions for commands
//
// A method call from the server above becomes one command for one printer. The
// process sends the command to the host and names the server above as the
// principal, "node:<its Node ID>". The host checks the permissions of that
// principal. This process checks no permission itself. The setting
// command_principal changes which principal it names.
//
// # Configuration
//
// An operator writes the settings in the config block of this module's
// instance, in the configuration file of the host. module.yaml declares every
// key. The host sends the settings with plugin.configure before any other
// call. README.md describes each key.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/signal"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The name and the version this process returns when the host calls
// plugin.info. They repeat module.yaml, and the host compares the two.
const (
	name    = "fractal"
	release = extension.Version("1.0.0")
)

// openAPI describes the routes this module serves, with every path relative to
// the mount. The host checks it at the start and serves it rebased under the
// mount, so a client finds it beside the routes themselves.
//
//go:embed openapi.json
var openAPI []byte

func main() {
	if err := start(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

// start writes the handshake line to stdout and then answers the host's calls
// on stdin and stdout. It returns when the host closes stdin, when the node
// fails, or when the server above gives this node another path in the tree.
func start() error {
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

	// NewConn takes the map now and Serve reads it later, so the handlers are
	// copied into the map below, before Serve runs. state starts empty. When
	// the host asks for the status before the node has joined, it gets the
	// empty status and no error.
	serves := wirerpc.Methods{}
	state := newLink()
	below := &tree{}

	// The host sends the settings once per process, before any other call. The
	// buffer of one lets the plugin.configure handler return before serve has
	// read the settings.
	settings := make(chan Config, 1)

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{Role: wirerpc.RoleChild, Methods: serves})
	if err != nil {
		return fmt.Errorf("build the session: %w", err)
	}

	maps.Copy(serves, methods(state, below, settings))

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// The host copies stderr into its log. The pid is printed because no
	// message to the host has it.
	fmt.Fprintf(os.Stderr, "%s %s is serving on stdio as pid %d\n", name, release, os.Getpid())

	return together(ctx, conn, state, below, log, settings)
}

// together runs the node and the connection to the host at the same time. When
// one of them ends, it stops the other. It returns the error of the node when
// there is one, and the error of the connection otherwise.
func together(
	ctx context.Context, conn *wirerpc.Conn, state *link, below *tree,
	log *slog.Logger, settings <-chan Config,
) error {
	running, cancel := context.WithCancel(ctx)
	defer cancel()

	node := make(chan error, 1)

	go func() {
		node <- serve(running, state, below, session{
			fleet:   deviceapi.NewClient(conn),
			stream:  conn,
			changes: linkapi.NewReporter(conn),
			storage: storageapi.NewClient(conn),
			access:  accessapi.NewClient(conn),
		}, log, settings)

		// The node has stopped. Closing the connection ends Serve, the process
		// exits, and the host starts it again.
		_ = conn.Close()
	}()

	served := conn.Serve(ctx)

	cancel()

	reason := <-node

	switch {
	case errors.Is(reason, ErrRelocated):
		return reason
	case reason != nil:
		return reason
	case served != nil:
		return fmt.Errorf("serve: %w", served)
	}

	return nil
}

// methods returns the handlers of this process: plugin.info, plugin.configure
// and plugin.shutdown, which every module answers, and the link methods of
// sdk/linkapi.
func methods(state *link, below *tree, settings chan<- Config) wirerpc.Methods {
	serves := wirerpc.Methods{
		wirerpc.MethodInfo: func(context.Context, wirerpc.Call) (any, error) {
			// The fragment goes on this call rather than on a route, because
			// the host publishes it at a fixed address whether or not this
			// module is serving yet, and a fragment the host would refuse is
			// refused here instead of being found by a client.
			return routesapi.Describe(wirerpc.Info{
				Name:     name,
				Version:  release,
				Contract: wirerpc.Contract,
				// The host sends settings only to a module that lists
				// plugin.configure here.
				Serves: []wirerpc.Method{wirerpc.MethodConfigure, routesapi.MethodServe},
			}, openAPI)
		},
		routesapi.MethodServe: routesapi.Serve(routes(below)),
		// The node reads the settings once. When an operator changes the
		// config block, the host restarts this process.
		wirerpc.MethodConfigure: configureWith(settings),
		// The host reads an empty result with no error as "this module agreed
		// to stop".
		//
		//nolint:nilnil // the contract's own answer to plugin.shutdown.
		wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) {
			fmt.Fprintf(os.Stderr, "%s was asked to stop\n", name)

			return nil, nil
		},
	}

	maps.Copy(serves, linkapi.Handlers(state))

	return serves
}

// configureWith returns the handler for plugin.configure. The handler parses
// the settings the host sent and puts them on the channel that serve reads.
//
// When ParseConfig refuses the settings, the handler returns the error. The
// host then fails the start and shows the error on the instance. A second
// plugin.configure call in one process is dropped, because the channel is full.
func configureWith(settings chan<- Config) wirerpc.Handler {
	return func(_ context.Context, call wirerpc.Call) (any, error) {
		carried, err := wirerpc.ReadSettings[json.RawMessage](call.Params)
		if err != nil {
			return nil, err
		}

		held, err := ParseConfig(carried)
		if err != nil {
			return nil, err
		}

		if held.Grants, err = wirerpc.ReadGrants(call.Params); err != nil {
			return nil, err
		}

		if err = held.checkGrants(); err != nil {
			return nil, err
		}

		select {
		case settings <- held:
		default:
		}

		return nil, nil
	}
}

// stdio is the connection to the host: it reads stdin and writes stdout. Close
// closes stdin, which ends a Serve that waits for the next call.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }

// The connection to the host must be able to open an event subscription.
var _ busapi.Opener = (*wirerpc.Conn)(nil)
