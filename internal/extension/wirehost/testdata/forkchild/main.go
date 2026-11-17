// Command forkchild is a child the wirehost tests launch to check how the host
// ends a process and the processes it started. It answers the lifecycle method
// set and fork.exit, which ends it with the exit code it is given.
//
//	-sleeper  start `sleep 300` sharing this child's standard error, and write
//	          "grandchild <pid>" there
//	-setsid   start that grandchild in a session of its own, outside this
//	          child's process group (unix only)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// methodExit ends the child with the code in its params, without answering.
const methodExit wirerpc.Method = "fork.exit"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "forkchild: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	sleeper := flag.Bool("sleeper", false, "start a grandchild that holds standard error")
	escaper := flag.Bool("setsid", false, "start the grandchild in a session of its own")
	flag.Parse()

	if *sleeper {
		grandchild := exec.Command("sleep", "300")
		grandchild.Stderr = os.Stderr

		if *escaper {
			detach(grandchild)
		}

		if err := grandchild.Start(); err != nil {
			return fmt.Errorf("start the grandchild: %w", err)
		}

		fmt.Fprintf(os.Stderr, "grandchild %d\n", grandchild.Process.Pid)
	}

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

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{
		Role: wirerpc.RoleChild,
		Methods: wirerpc.Methods{
			wirerpc.MethodInfo: func(context.Context, wirerpc.Call) (any, error) {
				return wirerpc.Info{Name: "example-fork", Version: "1.0.0", Contract: wirerpc.Contract}, nil
			},
			wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) {
				return json.RawMessage("null"), nil
			},
			methodExit: func(_ context.Context, call wirerpc.Call) (any, error) {
				var code int
				if err := json.Unmarshal(call.Params, &code); err != nil {
					return nil, err
				}

				os.Exit(code)

				return nil, nil
			},
		},
	})
	if err != nil {
		return fmt.Errorf("build the session: %w", err)
	}

	if err = conn.Serve(context.Background()); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// stdio is the channel the host handed over.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
