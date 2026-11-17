// Command graphql serves this core's own vocabulary in GraphQL.
//
// It is a service module and runs in a process of its own, outside the server.
// An operator mounts it at /graphql, which is where a GraphQL client expects
// to find one, and the instance's enabled flag is the only switch.
//
// # What it serves
//
//	POST /graphql   a query or a mutation, as every GraphQL client sends one
//	GET  /graphql   the same, with the query in the URL
//
// The schema is generated from the core's committed OpenAPI document, so the
// two surfaces cannot drift: every field is one core operation, every type is
// one core schema, and every member keeps the snake_case name the REST surface
// uses. schema.graphql is what a client reads, and a test in this module
// refuses a copy that has fallen behind the document.
//
// # There is no subscription
//
// The subscription root is absent, and nothing is built for it. The module
// wire carries one request and one whole answer, so a stream cannot cross it,
// and no GraphQL client this server is written against opens one. A page that
// wants live data reads GET /api/v1/events on the core, which is the one
// stream this server publishes.
//
// # Who a query runs as
//
// The person the core resolved. Every routes.serve request carries the
// principal the host decided, and every fleet call a resolver makes carries it
// back out, so the authorization graph answers for that person before a row is
// read. This process holds no token and checks no permission.
//
// # What an operator writes
//
// Nothing beyond installing it. `prusa-connect module add` reads the
// release and writes one declaration. See README.md.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/signal"

	"github.com/graphql-go/graphql"

	"github.com/prusa3d/graphql/internal/schema"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

//go:generate go run ./cmd/generate-schema

// What this child reports when the host asks it what it is.
const (
	name    = "graphql"
	release = extension.Version("1.0.0")
)

// openAPI is the fragment describing the two routes, with both paths relative
// to the mount. The host holds it to routesapi.ParseFragment at launch and
// serves it rebased under the mount, so a client finds the module's own
// document at /graphql/openapi.json.
//
//go:embed openapi.json
var openAPI []byte

// generated is the schema as the generator wrote it. The module builds its
// runtime types out of this rather than parsing schema.graphql, so the two
// files can never describe different surfaces: one model produced both.
//
//go:embed model.json
var generated []byte

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

	// The method set is filled after the session, because the resolvers call
	// back into the host and a client needs the session to call on. Nothing
	// answers a frame until Serve is running, and the map is full before that.
	serves := wirerpc.Methods{}

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{Role: wirerpc.RoleChild, Methods: serves})
	if err != nil {
		return fmt.Errorf("build the session: %w", err)
	}

	built, err := methods(deviceapi.NewClient(conn))
	if err != nil {
		return err
	}

	maps.Copy(serves, built)

	// The pid is here because the host's log is the only place a person sees
	// this process. Nothing on the wire carries it.
	fmt.Fprintf(os.Stderr, "%s %s is serving on stdio as pid %d\n", name, release, os.Getpid())

	if err = conn.Serve(ctx); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// methods is the lifecycle set every child answers, plus the one call that
// carries this module's HTTP surface.
func methods(fleet *deviceapi.Client) (wirerpc.Methods, error) {
	built, err := buildSchema(fleet)
	if err != nil {
		return nil, err
	}

	return wirerpc.Methods{
		wirerpc.MethodInfo: func(context.Context, wirerpc.Call) (any, error) {
			// The fragment is attached here rather than served as a route,
			// because the host publishes it at a stable address whether or not
			// the module is serving yet, and a fragment the host would refuse
			// is refused on this call rather than found by a client.
			return routesapi.Describe(
				wirerpc.Info{Name: name, Version: release, Contract: wirerpc.Contract}, openAPI)
		},
		// The shutdown answer carries nothing. An empty result and no error is
		// what the host reads as "this child agreed to stop".
		//
		//nolint:nilnil // the contract's own answer to plugin.shutdown.
		wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) {
			fmt.Fprintf(os.Stderr, "%s was asked to stop\n", name)

			return nil, nil
		},
		routesapi.MethodServe: routesapi.Serve(routes(&built)),
	}, nil
}

// buildSchema turns the generated model into the runnable schema, with every
// root field bound to the fleet.
//
// It runs once, at start-up. A model naming a field nothing resolves, or a
// type nothing declares, stops the module here rather than answering null to a
// client for the life of the process.
func buildSchema(fleet *deviceapi.Client) (graphql.Schema, error) {
	var model schema.Model
	if err := json.Unmarshal(generated, &model); err != nil {
		return graphql.Schema{}, fmt.Errorf("read the generated schema: %w", err)
	}

	built, err := schema.Build(model, resolversFor(fleet))
	if err != nil {
		return graphql.Schema{}, fmt.Errorf("build the schema: %w", err)
	}

	return built, nil
}

// stdio is the pair the host handed over. Close shuts the reading half, which
// is what ends a session whose host has gone quiet.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
