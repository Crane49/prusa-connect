// Command blobchild is the child the wirehost tests launch. It answers the
// lifecycle method set and streams bulk bytes under credit, so a test can check
// that the host's credit gate stops a sender in another process.
//
// Standard error carries the logs. Standard output carries the protocol, so
// nothing here prints to it. Each flag makes the child misbehave in one way,
// for the refusal cases:
//
//	-mark <file>   create the file and exit, so a test can see whether the
//	               process ran when it should not have
//	-protocol <n>  put this major on the handshake line
//	-silent        exit without a handshake line
//	-openapi <s>   answer plugin.info with this as the routes fragment, to
//	               check the host's rule on a fragment across a process
//	-routes        name routes.serve on plugin.info, to check the host's rule
//	               that served routes carry a fragment across a process
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// What this child reports when the host asks it what it is.
const (
	name    = "example-blob"
	release = extension.Version("1.0.0")
)

// The bulk transfer: the host orders bytes and the child says when it has run
// out of credit for them.
const (
	methodRead    wirerpc.Method = "blob.read"
	methodStarved wirerpc.Method = "blob.starved"

	// pattern is the period of the bytes the child writes, which the host
	// checks position by position. It is a prime, so no chunk size matches it.
	pattern = 251
)

// blobRequest is the order a host places.
type blobRequest struct {
	Bytes int `json:"bytes"`
	Chunk int `json:"chunk"`
}

// starved is what the child says when it has bytes left and no credit.
type starved struct {
	Sent int `json:"sent"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

func run() error {
	mark := flag.String("mark", "", "create this file and exit")
	protocol := flag.Uint("protocol", uint(wire.ProtocolMajor), "the major on the handshake line")
	silent := flag.Bool("silent", false, "exit without a handshake line")
	openapi := flag.String("openapi", "", "the OpenAPI fragment to answer plugin.info with")
	routes := flag.Bool("routes", false, "name routes.serve on plugin.info")
	flag.Parse()

	if *mark != "" {
		return os.WriteFile(*mark, nil, 0o600)
	}

	if *silent {
		return nil
	}

	line, err := wire.Handshake{
		Protocol: uint32(*protocol), //nolint:gosec // a test flag, one small number.
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

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{
		Role: wirerpc.RoleChild,
		Methods: wirerpc.Methods{
			wirerpc.MethodInfo: func(context.Context, wirerpc.Call) (any, error) {
				info := wirerpc.Info{Name: name, Version: release, Contract: wirerpc.Contract}
				if *routes {
					info.Serves = []wirerpc.Method{routesapi.MethodServe}
				}

				if *openapi != "" {
					// The child does not check the fragment. The flag is
					// there to test the host, and a child that checked
					// first would never send anything to refuse.
					info.OpenAPI = json.RawMessage(*openapi)
				}

				return info, nil
			},
			// The answer is written after the handler returns. The host
			// closes its side once it has that answer, so Serve ends when
			// the standard input closes with it.
			wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) {
				return json.RawMessage("null"), nil
			},
			methodRead: writeBlob,
		},
	})
	if err != nil {
		return fmt.Errorf("build the session: %w", err)
	}

	fmt.Fprintf(os.Stderr, "%s %s is serving on stdio\n", name, release)

	if err = conn.Serve(context.Background()); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// writeBlob writes what it was asked for, chunk by chunk, and says so on a
// control frame before it waits for credit it does not hold. The frame costs
// no credit, so a host that stopped reading learns that the sender stopped,
// and how far it got, without waiting on a clock.
func writeBlob(_ context.Context, call wirerpc.Call) (any, error) {
	var asked blobRequest
	if err := json.Unmarshal(call.Params, &asked); err != nil {
		return nil, err
	}

	if asked.Bytes < 0 || asked.Chunk < 1 {
		return nil, errors.New("bytes is a count and chunk is at least one")
	}

	blob := make([]byte, asked.Bytes)
	for at := range blob {
		blob[at] = byte(at % pattern)
	}

	for sent := 0; sent < len(blob); {
		if call.Stream.Credit() == 0 {
			if err := call.Stream.Notify(methodStarved, starved{Sent: sent}); err != nil {
				return nil, err
			}
		}

		wrote, err := call.Stream.Write(blob[sent:min(sent+asked.Chunk, len(blob))])
		if err != nil {
			return nil, err
		}

		sent += wrote
	}

	return nil, call.Stream.CloseWrite()
}

// stdio is the channel the host handed over.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
