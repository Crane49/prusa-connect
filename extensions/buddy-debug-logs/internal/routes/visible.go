package routes

import (
	"net/http"
	"sync"
	"time"

	"github.com/prusa3d/buddy-debug-logs/internal/sources"
)

// visibleFor is how long a read reuses the printers a person may see. A page
// in live mode reads every two seconds, and each read would otherwise list the
// whole fleet on the host. A grant taken away reaches reads within this time.
const visibleFor = 5 * time.Second

// visibleCache holds the printers each person may see, for visibleFor.
type visibleCache struct {
	now func() time.Time

	mu      sync.Mutex
	entries map[string]visibleEntry
}

type visibleEntry struct {
	ids map[string]bool
	at  time.Time
}

func newVisibleCache(now func() time.Time) *visibleCache {
	return &visibleCache{now: now, entries: map[string]visibleEntry{}}
}

// visible answers the printers by may see. A read may use what the host said
// a moment ago. A change, such as a purge or a new assignment, always asks the
// host again, so it never acts on a grant that was taken away.
func (s Service) visible(r *http.Request, by string) (map[string]bool, error) {
	if s.seen == nil || r.Method != http.MethodGet {
		return sources.Visible(r.Context(), s.Devices, by)
	}

	return s.seen.of(r, s.Devices, by)
}

func (c *visibleCache) of(r *http.Request, devices Devices, by string) (map[string]bool, error) {
	now := c.now()

	c.mu.Lock()
	entry, held := c.entries[by]
	c.mu.Unlock()

	if held && now.Sub(entry.at) < visibleFor {
		return entry.ids, nil
	}

	ids, err := sources.Visible(r.Context(), devices, by)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Drop what is old, so a person who stopped reading is not kept.
	for who, old := range c.entries {
		if now.Sub(old.at) >= visibleFor {
			delete(c.entries, who)
		}
	}

	c.entries[by] = visibleEntry{ids: ids, at: now}

	return ids, nil
}
