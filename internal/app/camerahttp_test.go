package app

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// rawAnswer is one HTTP response with its headers, which the media cases need
// and the JSON helper does not carry.
type rawAnswer struct {
	status int
	header http.Header
	body   []byte
}

// raw performs one GET and returns the whole response. It exists beside the
// JSON helper because a snapshot is bytes and a conditional GET is a header,
// and neither survives being read as a string body.
func (p *process) raw(t *testing.T, path string, extra http.Header) rawAnswer {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+p.addr+path, nil)
	require.NoError(t, err)

	for name, values := range extra {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	return rawAnswer{status: response.StatusCode, header: response.Header, body: body}
}

// send performs one write with the headers the caller chose, which a rewrite
// needs: the version it read has to ride in If-Match, and the shared helper
// sends If-None-Match instead.
func (p *process) send(t *testing.T, method, path, body string, extra http.Header) rawAnswer {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), method,
		"http://"+p.addr+path, strings.NewReader(body))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/json")

	for name, values := range extra {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { _ = response.Body.Close() }()

	read, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	return rawAnswer{status: response.StatusCode, header: response.Header, body: read}
}

// watchEvents opens the published event stream and returns a reader of every
// frame that has arrived on it. The stream is the same one a browser opens, so
// a case that asserts on it is asserting on the published surface.
func watchEvents(t *testing.T, running *process) func() []string {
	t.Helper()

	ctx, stop := context.WithCancel(context.WithoutCancel(t.Context()))
	t.Cleanup(stop)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+running.addr+"/api/v1/events", nil)
	require.NoError(t, err)

	request.Header.Set("Accept", "text/event-stream")

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)

	var (
		mu     sync.Mutex
		frames []string
	)

	go func() {
		defer func() { _ = response.Body.Close() }()

		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)

		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}

			mu.Lock()
			frames = append(frames, line)
			mu.Unlock()
		}
	}()

	return func() []string {
		mu.Lock()
		defer mu.Unlock()

		return append([]string(nil), frames...)
	}
}
