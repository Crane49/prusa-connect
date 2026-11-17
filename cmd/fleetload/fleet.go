package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// fleet is the printers this run drives and what they did.
type fleet struct {
	printers []*simulator.Printer
	failures atomic.Int64
	group    *errgroup.Group

	mu     sync.Mutex
	reason []string
}

// device is the part of one listed printer this harness reads.
type device struct {
	ID        string `json:"id"`
	Serial    string `json:"serial"`
	Connected bool   `json:"connected"`
}

// listing is one page of the fleet and its issue counters.
type listing struct {
	Items  []device `json:"items"`
	Total  int      `json:"total"`
	Issues issues   `json:"issues"`
}

// issues is the fleet-wide issue count from the listing.
type issues struct {
	Affected int            `json:"affected"`
	Counts   map[string]int `json:"counts"`
}

// named returns the non-zero counters, or "none".
func (i issues) named() string {
	parts := make([]string, 0, len(i.Counts))

	for _, key := range slices.Sorted(maps.Keys(i.Counts)) {
		if i.Counts[key] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", key, i.Counts[key]))
		}
	}

	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, " ")
}

// newFleet builds the printers without starting them.
func newFleet(opts options, from, count int, log *slog.Logger) (*fleet, error) {
	printers := make([]*simulator.Printer, 0, count)

	for i := from; i < from+count; i++ {
		printer, err := simulator.New(simulator.Config{
			Model:          opts.model,
			Serial:         fmt.Sprintf("LOAD-%05d", i),
			BaseURL:        "http://" + opts.api,
			IdlePeriod:     opts.cadence,
			PrintingPeriod: opts.cadence,
			Logger:         log,
		})
		if err != nil {
			return nil, fmt.Errorf("build printer %d: %w", i, err)
		}

		printers = append(printers, printer)
	}

	return &fleet{printers: printers}, nil
}

// start runs every printer, spread over ramp. A printer that stops before the
// run ends is counted as a failure.
func (f *fleet) start(ctx context.Context, ramp time.Duration) {
	group, runCtx := errgroup.WithContext(ctx)
	f.group = group

	gap := time.Duration(0)
	if ramp > 0 && len(f.printers) > 1 {
		gap = ramp / time.Duration(len(f.printers))
	}

	for i, printer := range f.printers {
		if gap > 0 && i > 0 && sleepUntil(ctx, gap) != nil {
			return
		}

		group.Go(func() error {
			if err := printer.RegisterAndRun(runCtx); err != nil && ctx.Err() == nil {
				f.failures.Add(1)
				f.note(err)
			}

			return nil
		})
	}
}

func sleepUntil(ctx context.Context, gap time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(gap):
		return nil
	}
}

// note keeps the first keptReasons reasons a printer stopped.
func (f *fleet) note(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.reason) < keptReasons {
		f.reason = append(f.reason, err.Error())
	}
}

func (f *fleet) reasons() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.reason)
}

const keptReasons = 5

// wait blocks until every printer has stopped.
func (f *fleet) wait() {
	if f.group != nil {
		_ = f.group.Wait()
	}
}

// sessions is how many websocket sessions the fleet opened. It counts
// reconnections, so a number above the fleet size means printers were dropped.
func (f *fleet) sessions() uint64 {
	var total uint64

	for _, printer := range f.printers {
		total += printer.Sessions()
	}

	return total
}

// awaitConnected blocks until the server reports want connected printers and
// returns how long that took. It reads the connector health, because the
// device listing is capped at listingCap rows.
func awaitConnected(ctx context.Context, client *http.Client, base string, want int, budget time.Duration) (
	time.Duration, error,
) {
	started := time.Now()
	deadline := started.Add(budget)

	for {
		seen, err := connectedCount(ctx, client, base)
		if err == nil && seen >= want {
			return time.Since(started), nil
		}

		if time.Now().After(deadline) {
			return time.Since(started), fmt.Errorf("only %d of %d printers connected in %s",
				seen, want, budget)
		}

		select {
		case <-ctx.Done():
			return time.Since(started), ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// pollInterval is how often the harness polls the server.
const pollInterval = 250 * time.Millisecond

// connectorHealth is the number of connected printers per connector.
type connectorHealth struct {
	Items []struct {
		Counts struct {
			Connected int `json:"connected"`
		} `json:"counts"`
	} `json:"items"`
}

// connectedCount is how many printers are connected, over every connector.
func connectedCount(ctx context.Context, client *http.Client, base string) (int, error) {
	body, _, err := get(ctx, client, base+"/api/v1/health/connectors")
	if err != nil {
		return 0, err
	}

	var health connectorHealth
	if err = json.Unmarshal(body, &health); err != nil {
		return 0, fmt.Errorf("read the connector health: %w", err)
	}

	var total int
	for _, item := range health.Items {
		total += item.Counts.Connected
	}

	return total, nil
}

// listingCap is the largest page the API returns.
const listingCap = 1000

// fleetPage reads one page of the listing, of at most listingCap rows.
func fleetPage(ctx context.Context, client *http.Client, base string, want int) (listing, error) {
	var page listing

	body, _, err := get(ctx, client, fmt.Sprintf("%s/api/v1/devices?limit=%d", base, min(want, listingCap)))
	if err != nil {
		return page, err
	}

	if err = json.Unmarshal(body, &page); err != nil {
		return page, fmt.Errorf("read the fleet listing: %w", err)
	}

	return page, nil
}

// seedGrants writes org-wide viewer grants and per-printer operator grants.
// The time it takes is part of the report.
func seedGrants(ctx context.Context, client *http.Client, base string, devices []device, count int) (
	time.Duration, int, error,
) {
	started := time.Now()
	written := 0

	for i := range orgReaders {
		subject := fmt.Sprintf("user:reader-%02d", i)
		if err := writeGrant(ctx, client, base, "org/local", "viewer", subject); err != nil {
			return time.Since(started), written, err
		}

		written++
	}

	for i, item := range devices {
		if i >= count {
			break
		}

		subject := fmt.Sprintf("user:operator-%02d", i%orgReaders)
		if err := writeGrant(ctx, client, base, "device/"+item.ID, "operator", subject); err != nil {
			return time.Since(started), written, err
		}

		written++
	}

	return time.Since(started), written, nil
}

// orgReaders is how many subjects get a grant over the whole org.
const orgReaders = 20

func writeGrant(ctx context.Context, client *http.Client, base, object, relation, subject string) error {
	body := fmt.Sprintf(`{"relation":%q,"subject":%q}`, relation, subject)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/api/v1/grants/"+object, strings.NewReader(body))
	if err != nil {
		return err
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("write the grant on %s: %w", object, err)
	}

	defer func() { _ = response.Body.Close() }()

	read, _ := io.ReadAll(response.Body)

	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("write the grant on %s: %s: %s", object, response.Status, strings.TrimSpace(string(read)))
	}

	return nil
}

// reader counts what the event stream subscribers received, over all of them.
type reader struct {
	frames atomic.Int64
	bytes  atomic.Int64
	errors atomic.Int64
}

// watch opens count event streams and reads them until the context ends.
func watch(ctx context.Context, base string, count int) (*reader, *sync.WaitGroup) {
	seen := &reader{}
	group := &sync.WaitGroup{}

	for range count {
		group.Go(func() { seen.stream(ctx, base) })
	}

	return seen, group
}

// stream keeps one subscription open and counts what arrives. A stream that
// ends early is counted as an error and reopened.
func (r *reader) stream(ctx context.Context, base string) {
	client := &http.Client{}

	for ctx.Err() == nil {
		if err := r.readOnce(ctx, client, base); err != nil && ctx.Err() == nil {
			r.errors.Add(1)

			select {
			case <-ctx.Done():
			case <-time.After(pollInterval):
			}
		}
	}
}

func (r *reader) readOnce(ctx context.Context, client *http.Client, base string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/events", nil)
	if err != nil {
		return err
	}

	request.Header.Set("Accept", "text/event-stream")

	response, err := client.Do(request)
	if err != nil {
		return err
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the event stream answered %s", response.Status)
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, streamScanBuffer), streamScanBuffer)

	for scanner.Scan() {
		line := scanner.Bytes()

		r.bytes.Add(int64(len(line)) + 1)

		if strings.HasPrefix(string(line), "data:") {
			r.frames.Add(1)
		}
	}

	return scanner.Err()
}

// streamScanBuffer bounds one line of the stream. One data line can cover the
// whole fleet, so bufio's default of 64 KiB is too small.
const streamScanBuffer = 4 << 20

// simulatorLogger logs at error level, so the printers' logging does not load
// the harness.
func simulatorLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}
