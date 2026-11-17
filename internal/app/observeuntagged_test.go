//go:build !observability

package app

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAScrapeOfAnUntaggedBuildSaysSo is the metrics endpoint on the real
// composition root. This build links no Prometheus client, so the honest answer
// is 404: an empty exposition would read as a healthy process measuring
// nothing, and a scraper would alert on the wrong thing.
//
// The constraint is what makes the claim true. The case asserts what a build
// with no Prometheus client in it does, and compiled into the tagged build it
// asserts the opposite of what that build does.
// TestAScrapeOfTheRunningProcessCarriesTheCatalog is the other half, under the
// tag.
func TestAScrapeOfAnUntaggedBuildSaysSo(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	scraped := running.get(t, "/metrics")
	require.Equal(t, http.StatusNotFound, scraped.status, scraped.body)
	require.Contains(t, scraped.body, "not compiled into this build")

	running.stopAndWait(t)
}
