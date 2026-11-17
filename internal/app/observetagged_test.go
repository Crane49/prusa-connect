//go:build observability

package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TestAScrapeOfTheRunningProcessCarriesTheCatalog is an earlier design decision on the real
// binary, and it only means anything in this build: without the observability
// tag nothing links a Prometheus client and /metrics answers 404.
//
// It reads the exposition rather than the registry's own view of it, because
// the two have disagreed in this repository before.
func TestAScrapeOfTheRunningProcessCarriesTheCatalog(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// One declared instance, because a Prometheus vector exports only the
	// children something asked for. A plane supervising nothing is a plane
	// whose series are correctly absent, so asserting on them would pass
	// against a binder nothing ever calls.
	running := start(t, dir, `core:
  log:
    level: info
  observability:
    enabled: true
resources:
  storage:
    local:
      type: filesystem
      enabled: true
      config:
        root: `+slashed(filepath.Join(dir, "storage"))+`
`)

	scraped := running.get(t, "/metrics")
	require.Equal(t, http.StatusOK, scraped.status, scraped.body)

	require.Contains(t, scraped.body, `pcl_log_lines_total{level="info"}`,
		"the process logger is not counting its own lines into the registry")

	// Above zero, which is the difference between a series that is declared and
	// a series something writes. A catalog publishing pcl_log_lines_total with no
	// handler incrementing it satisfies every check that reads declarations.
	require.NotContains(t, strings.ReplaceAll(scraped.body, " ", ""),
		`pcl_log_lines_total{level="info"}0`,
		"pcl_log_lines_total is registered and nothing has incremented it")

	// The supervision plane writes into the same registry, which is the seam
	// that was declared and unfilled: resourcehost.Config.Metrics was a
	// supervisor.Binder nothing ever set.
	require.Contains(t, scraped.body, `pcl_resource_starts_total{kind="storage",resource="local"}`,
		"the supervision plane is not bound to the registry")

	// The three a server exports before any printer has dialled in. A scrape
	// taken during a four hundred printer load carried the two log families and
	// nothing else, so what this asserts is that a running process reports on
	// itself, its inventory and its own API without being asked anything first.
	require.Contains(t, scraped.body, `pcl_build_info{`,
		"the build this process is running is not stated")
	require.Contains(t, scraped.body, `pcl_http_request_duration_seconds_count{`,
		"nothing observes the requests this origin serves")

	// The gauge follows the instance rather than the reconcile that asked for
	// it, so it reads 0 for as long as the instance is still starting. What is
	// asserted is that it arrives at 1, which is the difference between a gauge
	// wired to the status hook and one nothing ever moves.
	require.Eventually(t, func() bool {
		return strings.Contains(running.get(t, "/metrics").body,
			`pcl_resource_up{kind="storage",resource="local"} 1`)
	}, settle, 50*time.Millisecond, "a serving instance never reported itself up")

	running.stopAndWait(t)
}

// TestAMetricsListenerServesTheExpositionAndTheAPIListenerStopsServingIt walks
// the whole path a deployment takes: the key in the file, the strict loader,
// the composition root, the socket that gets bound, and the document the API
// listener publishes.
//
// It is the only case that can hold the second half. The API listener answering
// 404 at /metrics is a property of how registerMetrics was called, and nothing
// below the composition root can see that call.
//
// The scrape carries no credential because the client is a bare one. That is
// the claim: an address a deployment publishes to its collector, and no account
// on this server for the collector to hold.
func TestAMetricsListenerServesTheExpositionAndTheAPIListenerStopsServingIt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	metrics := freeAddr(t)

	running := start(t, dir, `core:
  observability:
    enabled: true
    addr: `+metrics+`
`)

	scraped := scrape(t, metrics, "/metrics")
	require.Equal(t, http.StatusOK, scraped.status, scraped.body)
	require.Contains(t, scraped.body, `pcl_build_info{`,
		"the metrics listener is bound and answers nothing the registry holds")

	// Nothing else on that socket. Publishing the port to a network publishes
	// one document, not this server's API.
	for _, path := range []string{"/api/v1/health", "/api/v1/devices", "/openapi.json", "/"} {
		require.Equal(t, http.StatusNotFound, scrape(t, metrics, path).status, path)
	}

	// The other half: one listener serves the exposition, never both.
	require.Equal(t, http.StatusNotFound, running.get(t, "/metrics").status,
		"the exposition moved to a listener of its own and the API listener still serves it")

	published := running.get(t, "/openapi.json")
	require.Equal(t, http.StatusOK, published.status)
	require.NotContains(t, published.body, `"/metrics"`,
		"the API listener publishes an operation it answers 404 to")

	running.stopAndWait(t)
}

// scrape reads one path off an address of this process other than the API
// listener, with a client carrying nothing.
func scrape(t *testing.T, addr, path string) answer {
	t.Helper()

	read, err := http.Get("http://" + addr + path)
	require.NoError(t, err)

	defer func() { require.NoError(t, read.Body.Close()) }()

	body, err := io.ReadAll(read.Body)
	require.NoError(t, err)

	return answer{status: read.StatusCode, body: string(body)}
}

// TestAScrapeCarriesEveryPrinterFamilyOnceAPrinterHasArrived is the fleet half
// of the same claim, and it is the half that was empty.
//
// The catalogue's per-printer families had no producer anywhere in internal/,
// so a site running four hundred printers scraped a server that said nothing
// about any of them, while the published example for this endpoint promised
// pcl_printer_up with a printer id in it.
func TestAScrapeCarriesEveryPrinterFamilyOnceAPrinterHasArrived(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, withMetrics(grantsConfig(dir)))

	_ = runSimulator(t, running, simulator.Config{Model: "MK4", Serial: "SIM-METRICS"}, "")

	var id string

	require.Eventually(t, func() bool {
		fleet := printers(t, running)
		if len(fleet) == 0 {
			return false
		}

		id = fleet[0].ID

		return true
	}, settle, 25*time.Millisecond, "the printer never reached the listing")

	// The fleet gauges are restated on the record flush's own period, so the
	// first walk after a printer arrives is what puts its series in the
	// registry. This waits for that walk and for nothing else.
	var scraped string

	require.Eventually(t, func() bool {
		read := running.get(t, "/metrics")
		scraped = read.body

		return read.status == http.StatusOK &&
			strings.Contains(scraped, `pcl_printer_up{device_id="`+id+`"} 1`)
	}, settle, 100*time.Millisecond,
		"a printer is connected and the scrape never carried pcl_printer_up for it")

	for _, want := range []string{
		`pcl_printer_info{`,
		`device_id="` + id + `"`,
		`pcl_printer_connected_since_timestamp_seconds{device_id="` + id + `"}`,
		`pcl_printer_last_frame_timestamp_seconds{device_id="` + id + `"}`,
		`pcl_printer_connections{driver="`,
		`pcl_printer_connection_attempts_total{`,
		`pcl_events_published_total{`,
	} {
		require.Containsf(t, scraped, want,
			"a printer has arrived and the scrape carries no %s", want)
	}

	// The state gauge is the one that has to be right in both directions. A
	// gauge that only ever counts up is one that reports a fleet nobody has.
	// The state gauge has to be right in both directions. A gauge that only
	// ever counts up is one that reports a fleet nobody has.
	require.Contains(t, scraped, `pcl_printer_connections{driver="xbuddy",state="connected"} 1`,
		"one printer is connected and the per-driver gauge does not say so")
	require.Contains(t, scraped, `pcl_printer_connections{driver="xbuddy",state="disconnected"} 0`,
		"the empty state is not exported, so a dashboard cannot alert on it reaching zero")

	running.stopAndWait(t)
}

// withMetrics switches the exposition on in a configuration, which a scrape
// of a tagged build needs since the key is read at start.
func withMetrics(file string) string {
	return strings.Replace(file, "core:\n", "core:\n  observability:\n    enabled: true\n", 1)
}

// TestATaggedBuildServesNoMetricsUntilTheFileAsks is the switch itself: the
// binary carries the client and the key is false, so /metrics answers 404 and
// names the key that changes it.
func TestATaggedBuildServesNoMetricsUntilTheFileAsks(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	scraped := running.get(t, "/metrics")
	require.Equal(t, http.StatusNotFound, scraped.status, scraped.body)
	require.Contains(t, scraped.body, "core.observability.enabled")

	running.stopAndWait(t)
}

// TestAPrinterOnAnotherFirmwareIsInfoOnce is CL-209: a CORE One flashed from
// 6.8.1 to 8.0.0 came back, and the scrape carried pcl_printer_info under both
// firmwares until the server restarted, so count(pcl_printer_info) counted it
// twice. The same holds for a model that moves.
func TestAPrinterOnAnotherFirmwareIsInfoOnce(t *testing.T) {
	t.Parallel()

	metrics, err := newObserved()
	require.NoError(t, err)

	sampler := newFleetSampler()
	entry := &device{
		id:     "printer-1",
		driver: "xbuddy",
		identity: connectorapi.Identity{
			Model: "COREONE", Firmware: connectorapi.NewFirmwareLabel("6.8.1+16182"),
		},
	}

	sampler.handlesFor(metrics.meters(), entry).info.Set(1)

	entry.identity.Firmware = connectorapi.NewFirmwareLabel("8.0.0+17068")
	sampler.handlesFor(metrics.meters(), entry).info.Set(1)

	entry.identity.Model = "COREONEL"
	sampler.handlesFor(metrics.meters(), entry).info.Set(1)

	scrape := httptest.NewRecorder()
	metrics.registry.Handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	var info []string

	for line := range strings.Lines(scrape.Body.String()) {
		if strings.HasPrefix(line, "pcl_printer_info{") {
			info = append(info, strings.TrimSpace(line))
		}
	}

	require.Equal(t, []string{
		`pcl_printer_info{device_id="printer-1",driver="xbuddy",firmware="8.0.0+17068",model="COREONEL"} 1`,
	}, info)
}
