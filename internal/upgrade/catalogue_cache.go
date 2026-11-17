package upgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"
)

// DefaultCatalogueTTL is how long one catalogue answer is reused. The update
// server limits how often one server may read its catalogue in an hour, and
// the settings page, staging and every reload all read it.
const DefaultCatalogueTTL = 5 * time.Minute

// ErrCatalogueLimited reports that the update server refused a request
// because this server asked too often. It is retryable after RetryAfter.
var ErrCatalogueLimited = errors.New("upgrade: the update server limits how often this server may ask")

// LimitedError carries the wait the update server named in Retry-After.
type LimitedError struct {
	RetryAfter time.Duration
}

func (e *LimitedError) Error() string {
	return fmt.Sprintf("%s: retry after %s", ErrCatalogueLimited, e.RetryAfter)
}

func (e *LimitedError) Unwrap() error { return ErrCatalogueLimited }

// limitedResponse turns a 429 into a LimitedError, and anything else into nil.
func limitedResponse(response *http.Response, now time.Time) error {
	if response.StatusCode != http.StatusTooManyRequests {
		return nil
	}

	return &LimitedError{RetryAfter: retryAfter(response.Header.Get("Retry-After"), now)}
}

const defaultRetryAfter = time.Minute

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now).Round(time.Second)
	}

	return defaultRetryAfter
}

// CachedSource reuses a catalogue answer for TTL. An entry is keyed by the
// current version and Scope, so a new pairing reads the catalogue again.
// Errors are never cached. Downloads pass through.
type CachedSource struct {
	Source ReleaseSource
	TTL    time.Duration
	// Scope names the credential the catalogue was read with, such as the
	// pairing state. A change of scope is a cache miss.
	Scope func() string
	Now   func() time.Time

	mu      sync.Mutex
	entries map[string]cachedCatalogue
}

type cachedCatalogue struct {
	releases []Release
	expires  time.Time
}

func (c *CachedSource) List(ctx context.Context, current string) ([]Release, error) {
	key := current
	if c.Scope != nil {
		key += "\x00" + c.Scope()
	}
	now := c.now()

	c.mu.Lock()
	entry, found := c.entries[key]
	c.mu.Unlock()
	if found && now.Before(entry.expires) {
		return slices.Clone(entry.releases), nil
	}

	releases, err := c.Source.List(ctx, current)
	if err != nil {
		return nil, err
	}
	ttl := c.TTL
	if ttl <= 0 {
		ttl = DefaultCatalogueTTL
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]cachedCatalogue)
	}
	for stale, held := range c.entries {
		if !now.Before(held.expires) {
			delete(c.entries, stale)
		}
	}
	c.entries[key] = cachedCatalogue{releases: slices.Clone(releases), expires: now.Add(ttl)}

	return releases, nil
}

func (c *CachedSource) Download(ctx context.Context, asset Asset, destination io.Writer) error {
	return c.Source.Download(ctx, asset, destination)
}

// Invalidate forgets every cached answer.
func (c *CachedSource) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
}

func (c *CachedSource) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}

	return time.Now()
}
