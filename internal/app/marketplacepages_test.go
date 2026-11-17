package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// pagedMarketplace serves a catalogue of `total` modules a page at a time,
// the way the marketplace pages it, and lets a case rewrite each answer's
// next address.
type pagedMarketplace struct {
	server *httptest.Server
	total  int
	// next turns the honest next address into the one the page carries.
	// Nil keeps it; returning "" ends the walk.
	next func(honest string, offset int) string
	// pad is extra bytes each module's summary carries, to grow a page.
	pad int
	// paged false answers every module on one page with no next field, as a
	// marketplace from before paging does.
	paged bool
	// relative answers next as a path and a query, which is what the
	// marketplace sends.
	relative bool

	mu      sync.Mutex
	queries []string
}

func newPagedMarketplace(t *testing.T, total int) *pagedMarketplace {
	t.Helper()

	market := &pagedMarketplace{total: total, paged: true}
	market.server = httptest.NewServer(http.HandlerFunc(market.serve))
	t.Cleanup(market.server.Close)

	return market
}

func (m *pagedMarketplace) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != marketplaceExtensionsPath {
		http.NotFound(w, r)

		return
	}

	m.mu.Lock()
	m.queries = append(m.queries, r.URL.RawQuery)
	m.mu.Unlock()

	limit, offset := m.total, 0
	if m.paged {
		limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	}

	modules := []marketplaceModule{}
	for index := offset; index < min(offset+limit, m.total); index++ {
		modules = append(modules, marketplaceModule{
			ID:      fmt.Sprintf("com.example.m%d", index),
			Module:  fmt.Sprintf("example.com/m%d", index),
			Summary: strings.Repeat("x", m.pad),
		})
	}

	answer := map[string]any{"extensions": modules}

	if m.paged {
		var next any

		if offset+limit < m.total {
			query := r.URL.Query()
			query.Set("offset", strconv.Itoa(offset+limit))
			next = m.server.URL + marketplaceExtensionsPath + "?" + query.Encode()

			if m.relative {
				next = marketplaceExtensionsPath + "?" + query.Encode()
			}
		}

		if m.next != nil {
			honest, _ := next.(string)
			if rewritten := m.next(honest, offset); rewritten != "" {
				next = rewritten
			} else {
				next = nil
			}
		}

		answer["next"] = next
		answer["total"] = m.total
	}

	_ = json.NewEncoder(w).Encode(answer)
}

func (m *pagedMarketplace) requests() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.queries)
}

func (m *pagedMarketplace) catalogue() *marketplaceCatalogue {
	return newMarketplaceCatalogue(config.Marketplace{URL: m.server.URL, Timeout: 5 * time.Second}, discard())
}

// TestARelativeNextPageIsResolvedAgainstThePage is the marketplace's own
// shape: next is a path and a query on the origin that answered it.
func TestARelativeNextPageIsResolvedAgainstThePage(t *testing.T) {
	t.Parallel()

	market := newPagedMarketplace(t, 450)
	market.relative = true

	rows, err := market.catalogue().list(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 450)
	require.Equal(t, 3, market.requests())
}

func TestEveryPageOfTheCatalogueIsRead(t *testing.T) {
	t.Parallel()

	market := newPagedMarketplace(t, 450)

	rows, err := market.catalogue().list(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 450)
	require.Equal(t, "example.com/m449", rows[449].Module)

	// The first request asks for the largest page, so the walk is short.
	require.Equal(t, 3, market.requests())
	require.Contains(t, market.queries[0], "limit=200")
	require.Contains(t, market.queries[2], "offset=400")
}

// A marketplace from before paging answers one page with no next, and the
// catalogue is that page.
func TestAMarketplaceThatDoesNotPageIsReadInOneRequest(t *testing.T) {
	t.Parallel()

	market := newPagedMarketplace(t, 260)
	market.paged = false

	rows, err := market.catalogue().list(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 260)
	require.Equal(t, 1, market.requests())
}

func TestANextPageThisServerWouldNotFollowFailsTheWalk(t *testing.T) {
	t.Parallel()

	for name, rewrite := range map[string]func(honest string, offset int) string{
		"another host": func(string, int) string {
			return "https://elsewhere.example" + marketplaceExtensionsPath + "?offset=200"
		},
		"another scheme": func(honest string, _ int) string {
			return strings.Replace(honest, "http://", "https://", 1)
		},
		"outside the catalogue": func(honest string, _ int) string {
			return strings.Replace(honest, marketplaceExtensionsPath, "/admin/export", 1)
		},
		"a relative address on another host": func(string, int) string {
			return "//elsewhere.example" + marketplaceExtensionsPath + "?offset=200"
		},
		"a relative address outside the catalogue": func(string, int) string { return "/admin/export?offset=200" },
		"credentials in the address": func(honest string, _ int) string {
			return strings.Replace(honest, "http://", "http://user:secret@", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			market := newPagedMarketplace(t, 450)
			market.next = rewrite

			_, err := market.catalogue().list(t.Context())
			require.ErrorIs(t, err, errMarketplacePages)
			require.Equal(t, 1, market.requests(), "the server followed an address it should have refused")
		})
	}
}

func TestANextPageThatComesRoundAgainEndsTheWalk(t *testing.T) {
	t.Parallel()

	market := newPagedMarketplace(t, 450)
	first := ""
	market.next = func(honest string, offset int) string {
		if offset == 0 {
			first = honest

			return honest
		}

		// The second page points back at itself.
		return first
	}

	_, err := market.catalogue().list(t.Context())
	require.ErrorIs(t, err, errMarketplacePages)
	require.Equal(t, 2, market.requests())
}

func TestACatalogueOfMorePagesThanTheCapIsRefusedWhole(t *testing.T) {
	t.Parallel()

	// Every page claims one more, forever.
	market := newPagedMarketplace(t, 1)
	market.next = func(_ string, offset int) string {
		return fmt.Sprintf("%s%s?limit=1&offset=%d", market.server.URL, marketplaceExtensionsPath, offset+1)
	}

	_, err := market.catalogue().list(t.Context())
	require.ErrorIs(t, err, errMarketplacePages)
	require.Equal(t, maxMarketplacePages, market.requests())
}

// The byte budget is the whole walk's, not each page's.
func TestTheWalkSpendsOneByteBudget(t *testing.T) {
	t.Parallel()

	// Three pages of about 1.6 MiB each pass a 4 MiB budget on the third.
	market := newPagedMarketplace(t, 600)
	market.pad = 8 << 10

	_, err := market.catalogue().list(t.Context())
	require.ErrorIs(t, err, errMarketplaceAnswer)
	require.Equal(t, 3, market.requests())
}

func TestAModuleOnTwoPagesIsListedOnce(t *testing.T) {
	t.Parallel()

	rows := uniqueModules([]marketplaceModule{
		{ID: "a", Module: "example.com/a"},
		{ID: "b", Module: "example.com/b"},
		{ID: "a", Module: "example.com/a", Summary: "the later copy"},
	})

	require.Len(t, rows, 2)
	require.Empty(t, rows[0].Summary)
}

// A page that fails keeps the copy from the last walk that finished, with
// the reason, as a single failed request always did.
func TestAFailedWalkKeepsTheLastCopy(t *testing.T) {
	t.Parallel()

	market := newPagedMarketplace(t, 450)
	catalogue := market.catalogue()

	catalogue.refresh(t.Context())
	require.Len(t, catalogue.rows, 450)

	market.next = func(string, int) string { return "https://elsewhere.example/x" }
	catalogue.refresh(t.Context())

	require.Len(t, catalogue.rows, 450)
	require.Contains(t, catalogue.status().Problem, "pages")
}
