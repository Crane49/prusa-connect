package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// The catalogue is paged: each answer carries the next page's address, or
// null on the last one. The address is a path and a query, resolved against
// the page that named it, or a whole address on the same origin. A marketplace
// that does not page answers one page with no next, and the walk ends there.
const (
	// marketplacePageLimit is what the first request asks for, the most a
	// page holds, so a catalogue of a few hundred modules is a handful of
	// requests.
	marketplacePageLimit = 200
	// maxMarketplacePages bounds the walk. A catalogue past it is refused
	// whole rather than listed in part, which keeps the last good copy.
	maxMarketplacePages = 20
)

// errMarketplacePages is a walk this server would not finish: a next page
// on another origin or outside the catalogue, a page it already read, or more
// pages or bytes than it takes.
var errMarketplacePages = errors.New("the marketplace's catalogue pages are not ones this server follows")

// marketplacePage is one answer of the catalogue.
type marketplacePage struct {
	Extensions []marketplaceModule `json:"extensions"`
	Next       *string             `json:"next"`
}

// list reads every page of the catalogue for this server's version and
// platform. The whole walk spends one timeout and one byte budget, and a
// page that fails fails the walk, so the caller keeps the copy it had.
func (c *marketplaceCatalogue) list(ctx context.Context) ([]marketplaceModule, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	query := url.Values{
		"platform": {c.platform},
		"limit":    {strconv.Itoa(marketplacePageLimit)},
	}
	if c.core != "" {
		query.Set("core", c.core)
	}

	address := c.base + marketplaceExtensionsPath + "?" + query.Encode()
	seen := map[string]bool{}
	budget := maxMarketplaceBytes

	var rows []marketplaceModule

	for pages := 0; ; pages++ {
		if pages == maxMarketplacePages {
			return nil, fmt.Errorf("%w: it runs past %d pages", errMarketplacePages, maxMarketplacePages)
		}

		seen[address] = true

		var page marketplacePage

		read, err := c.fetch(ctx, address, budget, &page)
		if err != nil {
			return nil, err
		}

		budget -= read
		rows = append(rows, page.Extensions...)

		if page.Next == nil || strings.TrimSpace(*page.Next) == "" {
			return uniqueModules(rows), nil
		}

		next, err := c.nextPage(address, *page.Next)
		if err != nil {
			return nil, err
		}

		if seen[next] {
			return nil, fmt.Errorf("%w: page %s comes round again", errMarketplacePages, next)
		}

		address = next
	}
}

// nextPage holds a next address to the catalogue this server was told to
// read: the configured marketplace's own scheme and host, and the catalogue's
// path under its base. A marketplace cannot send this server anywhere else.
// A relative address is resolved against current, the page that named it.
func (c *marketplaceCatalogue) nextPage(current, raw string) (string, error) {
	base, err := url.Parse(c.base)
	if err != nil {
		return "", fmt.Errorf("address the marketplace: %w", err)
	}

	page, err := url.Parse(current)
	if err != nil {
		return "", fmt.Errorf("address the marketplace: %w", err)
	}

	relative, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: %q is not an address", errMarketplacePages, raw)
	}

	next := page.ResolveReference(relative)

	if !strings.EqualFold(next.Scheme, base.Scheme) || !strings.EqualFold(next.Host, base.Host) ||
		next.User != nil {
		return "", fmt.Errorf("%w: %q is not on %s", errMarketplacePages, raw, c.base)
	}

	if next.Path != strings.TrimRight(base.Path, "/")+marketplaceExtensionsPath {
		return "", fmt.Errorf("%w: %q is not the catalogue", errMarketplacePages, raw)
	}

	return next.String(), nil
}

// uniqueModules keeps the first row of each module. A catalogue that moved
// while it was being paged can show one module on two pages.
func uniqueModules(rows []marketplaceModule) []marketplaceModule {
	seen := make(map[string]bool, len(rows))
	out := make([]marketplaceModule, 0, len(rows))

	for _, row := range rows {
		key := row.Module
		if key == "" {
			key = row.ID
		}

		if seen[key] {
			continue
		}

		seen[key] = true

		out = append(out, row)
	}

	return out
}
