package upgrade

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type countingSource struct {
	calls int
	err   error
}

func (c *countingSource) List(context.Context, string) ([]Release, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}

	return []Release{{Version: "1.1.0"}}, nil
}

func (c *countingSource) Download(context.Context, Asset, io.Writer) error { return nil }

func TestCachedSourceReadsTheCatalogueOncePerChannelScopeAndTTL(t *testing.T) {
	t.Parallel()

	upstream := &countingSource{}
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	scope := "paired-a"
	cache := &CachedSource{
		Source: upstream, Scope: func() string { return scope }, Now: func() time.Time { return now },
	}

	read := func() {
		t.Helper()
		if _, err := cache.List(t.Context(), "1.0.0"); err != nil {
			t.Fatal(err)
		}
	}

	read()
	read()
	if upstream.calls != 1 {
		t.Fatalf("two reads within the TTL asked the update server %d times", upstream.calls)
	}
	if _, err := cache.List(t.Context(), "1.1.0"); err != nil {
		t.Fatal(err)
	}
	if upstream.calls != 2 {
		t.Fatalf("another current version did not read the catalogue: %d calls", upstream.calls)
	}
	scope = "paired-b"
	read()
	if upstream.calls != 3 {
		t.Fatalf("a new pairing did not read the catalogue: %d calls", upstream.calls)
	}
	now = now.Add(DefaultCatalogueTTL)
	read()
	if upstream.calls != 4 {
		t.Fatalf("an expired answer was reused: %d calls", upstream.calls)
	}
	cache.Invalidate()
	read()
	if upstream.calls != 5 {
		t.Fatalf("Invalidate kept the answer: %d calls", upstream.calls)
	}
}

func TestCachedSourceNeverCachesAFailure(t *testing.T) {
	t.Parallel()

	upstream := &countingSource{err: errors.New("portal down")}
	cache := &CachedSource{Source: upstream}
	for range 2 {
		if _, err := cache.List(t.Context(), "1.0.0"); err == nil {
			t.Fatal("a failure was answered from the cache")
		}
	}
	if upstream.calls != 2 {
		t.Fatalf("calls are %d", upstream.calls)
	}
}

func TestMachineClientReportsTheUpdateServerLimitWithItsWait(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Retry-After", "120")
		http.Error(response, "Too many requests", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := MachineClient{BaseURL: server.URL, Client: server.Client()}
	_, err := client.List(t.Context(), "1.0.0")
	limited, ok := errors.AsType[*LimitedError](err)
	if !ok || !errors.Is(err, ErrCatalogueLimited) {
		t.Fatalf("error is %v", err)
	}
	if limited.RetryAfter != 2*time.Minute {
		t.Fatalf("wait is %s", limited.RetryAfter)
	}

	err = client.Download(t.Context(), Asset{Name: "core", URL: server.URL + "/core", Size: 4}, io.Discard)
	if !errors.Is(err, ErrCatalogueLimited) {
		t.Fatalf("download error is %v", err)
	}
}
