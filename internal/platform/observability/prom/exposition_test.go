//go:build observability

package prom_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
	"github.com/prusa3d/prusa-connect/internal/platform/observability/prom"
)

// The test reads the exposition itself, the way Prometheus does, and not the
// registry's own view. The two can disagree: a bucket definition that names a
// native factor and no explicit bounds registers cleanly and serves a text
// scraper nothing.
func TestAScrapeCarriesTheClassicBuckets(t *testing.T) {
	t.Parallel()

	histogram := observability.NewHistogram("pcl_http_request_duration_seconds",
		"Time to serve an HTTP request.", observability.LatencySeconds(), "method", "route", "code")
	counter := observability.NewCounter("pcl_printer_errors_total",
		"Errors reported for a printer.", []observability.Label{"code"}, observability.DeviceID)

	registry, err := prom.New(prom.Options{}, histogram, counter)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	observer, err := registry.Observer(histogram, http.MethodGet, "/api/v1/devices", "200")
	if err != nil {
		t.Fatalf("observer: %v", err)
	}

	observer.Observe(0.042)

	deviceErrors, err := registry.Counter(counter, "printer.unreachable", "mk4-017")
	if err != nil {
		t.Fatalf("counter: %v", err)
	}

	deviceErrors.Add(1)

	body := scrape(t, registry.Handler(), "text/plain;version=0.0.4")

	for _, want := range []string{
		`pcl_http_request_duration_seconds_bucket{code="200",method="GET",route="/api/v1/devices",le="0.05"} 1`,
		`pcl_http_request_duration_seconds_count{code="200",method="GET",route="/api/v1/devices"} 1`,
		`pcl_printer_errors_total{code="printer.unreachable",device_id="mk4-017"} 1`,
		`# TYPE pcl_http_request_duration_seconds histogram`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the exposition does not carry\n  %s\ngot:\n%s", want, body)
		}
	}

	if strings.Contains(body, "device_id") && strings.Contains(body, "_bucket{") &&
		strings.Contains(body, `_bucket{device_id`) {
		t.Error("a bucketed series carries device_id")
	}
}

// Native buckets reach protobuf only, so the text scrape above cannot show
// them. The registry's own gather is where they are visible.
func TestTheSameDefinitionCarriesNativeBuckets(t *testing.T) {
	t.Parallel()

	histogram := observability.NewHistogram("pcl_call_duration_seconds", "Calls.",
		observability.LatencySeconds(), "driver")

	registry, err := prom.New(prom.Options{}, histogram)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	observer, err := registry.Observer(histogram, "xbuddy")
	if err != nil {
		t.Fatalf("observer: %v", err)
	}

	observer.Observe(0.042)

	families := gather(t, registry)

	family, ok := families["pcl_call_duration_seconds"]
	if !ok {
		t.Fatalf("the family is missing, got %v", families)
	}

	metric := family.GetMetric()[0].GetHistogram()

	// A factor of 1.1 picks schema 3. client_golang's pickSchema takes
	// floor(log2(log2(factor))) and negates it, which gives the eight buckets
	// per power of two its documentation promises.
	const schemaForFactor11 = 3

	if got := metric.GetSchema(); got != schemaForFactor11 {
		t.Fatalf("native schema is %d, want %d: a protobuf scrape reads no native histogram",
			got, schemaForFactor11)
	}

	if len(metric.GetBucket()) == 0 {
		t.Fatal("no classic buckets, so a text scrape reads no buckets")
	}

	if metric.GetZeroThreshold() == 0 {
		t.Error("no zero threshold on the native histogram")
	}
}

// Wrong label values are refused at binding. Left to the call site they would
// panic on the path the handle is used on.
func TestAHandleWithTheWrongLabelValuesIsRefused(t *testing.T) {
	t.Parallel()

	counter := observability.NewCounter("pcl_frames_total", "Frames.",
		[]observability.Label{"driver"})

	registry, err := prom.New(prom.Options{}, counter)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err = registry.Counter(counter); !errors.Is(err, prom.ErrLabelValues) {
		t.Fatalf("want %v, got %v", prom.ErrLabelValues, err)
	}
}

// scrape reads the handler the way a Prometheus server does.
func scrape(t *testing.T, handler http.Handler, accept string) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Accept", accept)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("scrape answered %d", recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("reading the exposition: %v", err)
	}

	return string(body)
}

// gather reads the registry's own view, which is where the native buckets are.
func gather(t *testing.T, registry *prom.Registry) map[string]*dto.MetricFamily {
	t.Helper()

	families, err := registry.Gatherer().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	byName := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		byName[family.GetName()] = family
	}

	return byName
}
