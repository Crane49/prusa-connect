package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"
)

// probeInterval is how often one probe calls its endpoint. It is slow on
// purpose: the fleet is the load, not the probes.
const probeInterval = 500 * time.Millisecond

// probe is one endpoint being timed while the fleet reports.
type probe struct {
	name string
	url  string

	mu      sync.Mutex
	samples []time.Duration
	bytes   int64
	fails   int
}

// run calls the endpoint until the context ends and records each duration.
func (p *probe) run(ctx context.Context, client *http.Client) {
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.once(ctx, client)
		}
	}
}

func (p *probe) once(ctx context.Context, client *http.Client) {
	started := time.Now()

	body, status, err := get(ctx, client, p.url)
	took := time.Since(started)

	// A call cancelled by the end of the window is not a failure.
	if ctx.Err() != nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err != nil || status != http.StatusOK {
		p.fails++

		return
	}

	p.samples = append(p.samples, took)
	p.bytes += int64(len(body))
}

// result is what one probe measured.
type result struct {
	name    string
	calls   int
	fails   int
	bytes   int64
	median  time.Duration
	p95     time.Duration
	p99     time.Duration
	slowest time.Duration
}

func (p *probe) result() result {
	p.mu.Lock()
	defer p.mu.Unlock()

	sorted := slices.Clone(p.samples)
	slices.Sort(sorted)

	out := result{name: p.name, calls: len(sorted), fails: p.fails}
	if len(sorted) == 0 {
		return out
	}

	out.bytes = p.bytes / int64(len(sorted))
	out.median = quantile(sorted, quantile50)
	out.p95 = quantile(sorted, quantile95)
	out.p99 = quantile(sorted, quantile99)
	out.slowest = sorted[len(sorted)-1]

	return out
}

// quantile returns one quantile of a sorted sample, by nearest rank.
func quantile(sorted []time.Duration, fraction float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	index := int(fraction * float64(len(sorted)))

	return sorted[min(index, len(sorted)-1)]
}

// get reads one whole response and returns its body and status.
func get(ctx context.Context, client *http.Client, url string) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}

	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("read %s: %w", url, err)
	}

	return body, response.StatusCode, nil
}
