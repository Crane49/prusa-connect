package mini404

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// readinessPoll is the wait between probe attempts. Cold boots here are minutes
// long, so a tighter loop buys nothing and a looser one blurs the timings a
// caller reports.
const (
	readinessPoll = 500 * time.Millisecond
	// probeTimeout bounds one HTTP attempt. A guest mid-boot accepts the
	// connection and then says nothing, so without this the loop stops polling.
	probeTimeout = 2 * time.Second
)

// WaitFunc polls probe until it returns nil, and names the phase when the
// context runs out first.
//
// The phase name is the whole diagnostic this function offers. What a stalled
// phase means, and what to check, belongs to the caller that knows which phase
// it asked for.
func WaitFunc(ctx context.Context, phase string, probe func(context.Context) error) error {
	var last error
	for {
		err := probe(ctx)
		if err == nil {
			return nil
		}
		last = err

		select {
		case <-ctx.Done():
			return fmt.Errorf("phase %q did not complete: %w (last probe error: %w)", phase, ctx.Err(), last)
		case <-time.After(readinessPoll):
		}
	}
}

// WaitPrusaLink waits until the guest's HTTP stack answers on the forwarded
// port.
//
// Any response is the signal, including 401. An unauthorised reply means the
// server is routing and the API key check ran, which is what "the stack is up"
// means here; waiting for 200 would only be waiting for a credential.
func WaitPrusaLink(ctx context.Context, port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/version", port)
	client := &http.Client{Timeout: probeTimeout}

	return WaitFunc(ctx, "prusalink-http", func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		// The status does not matter, only that there was one.
		_ = resp.Body.Close()
		return nil
	})
}
