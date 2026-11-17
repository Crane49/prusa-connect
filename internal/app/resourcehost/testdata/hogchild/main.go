// Command hogchild is a service module the resourcehost tests launch to watch
// the host hold a process to its memory budget. Its setting mode says what it
// does once configured:
//
//	alloc  allocate and touch 4 MiB every 50 ms, up to 512 MiB, then hold it
//	idle   hold 1 MiB and wait
//	fork   start `sleep 300` and write "sleeper <pid>" to standard error
//
// It writes "hogchild <mode> is serving as pid <pid> under GOMEMLIMIT=<value>"
// to standard error once it has its settings.
//
// In every mode it serves two more methods, for the hang tests:
// fixture.sleep blocks for 30 s, and fixture.fail answers an internal error
// at once.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

const (
	mib       = 1 << 20
	chunk     = 4 * mib
	pageBytes = 4096
	every     = 50 * time.Millisecond
	// ceiling bounds the allocation, so a watchdog that does not kill cannot
	// exhaust the machine the tests run on.
	ceiling = 512 * mib

	methodSleep wirerpc.Method = "fixture.sleep"
	methodFail  wirerpc.Method = "fixture.fail"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "hogchild: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
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

	// held keeps what the child allocated reachable, so the collector frees
	// none of it.
	var held [][]byte

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{
		Role: wirerpc.RoleChild,
		Methods: wirerpc.Methods{
			wirerpc.MethodInfo: func(context.Context, wirerpc.Call) (any, error) {
				return wirerpc.Info{
					Name: "hogchild", Version: "1.0.0", Contract: wirerpc.Contract,
					Serves: []wirerpc.Method{wirerpc.MethodConfigure, methodSleep, methodFail},
				}, nil
			},
			wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) {
				return json.RawMessage("null"), nil
			},
			// A handler that never finishes inside any host deadline.
			methodSleep: func(context.Context, wirerpc.Call) (any, error) {
				time.Sleep(30 * time.Second)

				return json.RawMessage("null"), nil
			},
			// A handler that fails at once, as a module whose upstream is down.
			methodFail: func(context.Context, wirerpc.Call) (any, error) {
				return nil, &wirerpc.Error{Code: wirerpc.CodeInternal, Message: "failing on purpose"}
			},
			wirerpc.MethodConfigure: func(_ context.Context, call wirerpc.Call) (any, error) {
				var settings wirerpc.Settings
				if err := json.Unmarshal(call.Params, &settings); err != nil {
					return nil, err
				}

				var config struct {
					Mode string `json:"mode"`
				}

				if err := json.Unmarshal(settings.Config, &config); err != nil {
					return nil, err
				}

				switch config.Mode {
				case "alloc":
					go func() {
						for allocated := 0; allocated < ceiling; allocated += chunk {
							held = append(held, touched(chunk))

							time.Sleep(every)
						}
					}()
				case "idle":
					held = append(held, touched(mib))
				case "fork":
					sleeper := exec.Command("sleep", "300")
					if err := sleeper.Start(); err != nil {
						return nil, err
					}

					fmt.Fprintf(os.Stderr, "sleeper %d\n", sleeper.Process.Pid)
				default:
					return nil, fmt.Errorf("unknown mode %q", config.Mode)
				}

				fmt.Fprintf(os.Stderr, "hogchild %s is serving as pid %d under GOMEMLIMIT=%s\n",
					config.Mode, os.Getpid(), os.Getenv("GOMEMLIMIT"))

				return wirerpc.Configured{}, nil
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

// touched allocates size bytes and writes every page, so the memory is
// resident and not only reserved.
func touched(size int) []byte {
	block := make([]byte, size)
	for i := 0; i < size; i += pageBytes {
		block[i] = 1
	}

	return block
}

// stdio is the channel the host handed over.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
