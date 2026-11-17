package diagnostics

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/platform/observability"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// Sources is everything a bundle is built from, read by the caller under the
// requesting admin's identity. Build reads nothing else, apart from the
// modules in Extensions, which it asks for their files itself.
type Sources struct {
	Version string
	// Commit is the revision the binary was built from, empty when the build
	// carries none.
	Commit     string
	Started    time.Time
	Config     config.Core
	Logs       []observability.LogEntry
	Crash      []byte
	Devices    []httpapi.Device
	Resources  []httpapi.ResourceInstance
	Readiness  httpapi.ReadinessReport
	Connectors []httpapi.ConnectorRollup
	Accounts   int
	Secrets    []string
	// Extensions is the running modules that add files. Build asks them one
	// at a time, and only when Options.Extensions is set.
	Extensions []ExtensionSource
}

// Options are the operator's choices and what makes one bundle unlinkable to
// the next.
type Options struct {
	Modules bool
	// Extensions includes the files modules added. The operator chooses it.
	Extensions bool
	Frontend   []byte
	Now        time.Time
	Salt       []byte
}

// Build writes the zip. Every file passes the known-secret sweep last, so a
// value the projections missed is still cut out. It stops when ctx ends, which
// is how a browser that gave up stops the work. manifest.json is written
// last, because it describes the modules' files.
func Build(ctx context.Context, src Sources, opts Options) ([]byte, error) {
	s := NewScrubber(opts.Salt, src.Secrets)
	frontend, frontendState := frontendLines(opts.Frontend, s)

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	if err := putJSON(w, s, opts.Now, []namedValue{
		{"config.json", configView(src.Config, s)},
		{"modules.json", resourcesView(src.Resources, s)},
		{"printers.json", devicesView(src.Devices, s)},
		{"health.json", healthViewOf(src.Readiness, src.Connectors, src.Config, s)},
		{"accounts.json", map[string]int{"accounts": src.Accounts}},
	}); err != nil {
		return nil, err
	}

	lines, err := logLines(ctx, src.Logs, s, opts.Modules)
	if err != nil {
		return nil, fmt.Errorf("project the logs: %w", err)
	}

	if err = put(w, "logs.jsonl", opts.Now, s.Sweep(jsonl(lines))); err != nil {
		return nil, err
	}

	if frontendState == "included" {
		if err = put(w, "frontend.jsonl", opts.Now, s.Sweep(jsonl(frontend))); err != nil {
			return nil, err
		}
	}

	var extensions any = "not included"

	if opts.Extensions {
		if extensions, err = putExtensions(ctx, w, s, opts.Now, src.Extensions); err != nil {
			return nil, err
		}
	}

	// A panic message can name a file path or an address, so the crash text is
	// scrubbed as well as swept.
	if len(src.Crash) > 0 {
		if err = put(w, "crash.log", opts.Now, s.Sweep([]byte(s.Text(string(src.Crash))))); err != nil {
			return nil, err
		}
	}

	if err = putJSON(w, s, opts.Now, []namedValue{
		{"manifest.json", manifest(src, opts, frontendState, extensions)},
	}); err != nil {
		return nil, err
	}

	if err = w.Close(); err != nil {
		return nil, fmt.Errorf("close bundle: %w", err)
	}

	return buf.Bytes(), nil
}

type namedValue struct {
	name  string
	value any
}

// putJSON writes each value as indented JSON, swept.
func putJSON(w *zip.Writer, s *Scrubber, now time.Time, files []namedValue) error {
	for _, f := range files {
		encoded, err := json.MarshalIndent(f.value, "", "  ")
		if err != nil {
			return fmt.Errorf("encode %s: %w", f.name, err)
		}

		if err = put(w, f.name, now, s.Sweep(encoded)); err != nil {
			return err
		}
	}

	return nil
}

func manifest(src Sources, opts Options, frontend string, extensions any) map[string]any {
	_, docker := os.Stat("/.dockerenv")

	m := map[string]any{
		"version":      src.Version,
		"go":           runtime.Version(),
		"os":           runtime.GOOS,
		"arch":         runtime.GOARCH,
		"container":    docker == nil,
		"uptime":       opts.Now.Sub(src.Started).Round(time.Second).String(),
		"generated_at": opts.Now.UTC().Format(time.RFC3339),
		"modules":      opts.Modules,
		"frontend":     frontend,
		"log_records":  len(src.Logs),
		"extensions":   extensions,
	}

	if src.Commit != "" {
		m["commit"] = src.Commit
	}

	return m
}

func put(w *zip.Writer, name string, modified time.Time, content []byte) error {
	f, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified})
	if err != nil {
		return fmt.Errorf("add %s: %w", name, err)
	}

	if _, err = f.Write(content); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}

	return nil
}

func jsonl[T any](lines []T) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	for _, line := range lines {
		_ = enc.Encode(line) // a line is plain data; it always encodes
	}

	return buf.Bytes()
}
