package app

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/observability"
)

// unmappedPlaneSeries are the supervisor's series that reach no catalog metric,
// each with the reason. A name that is in neither this map nor planeSeries
// fails the gate below, which is what makes a series the plane starts writing
// and the catalog does not carry a visible gap rather than a silent one.
var unmappedPlaneSeries = map[string]string{}

// TestEverySupervisionSeriesIsAccountedFor is the drift gate over the binder.
// The supervisor's names and the catalog's were designed from different
// research documents, and the only thing that keeps them in agreement is this.
func TestEverySupervisionSeriesIsAccountedFor(t *testing.T) {
	t.Parallel()

	catalog := map[string]observability.Def{}
	for _, def := range observability.Catalog() {
		catalog[def.Name()] = def
	}

	emitted := []string{
		supervisor.MetricCalls, supervisor.MetricDuration, supervisor.MetricRejected,
		supervisor.MetricDropped, supervisor.MetricPanics, supervisor.MetricStarts,
		supervisor.MetricBreaker, supervisor.MetricStates, supervisor.MetricStale,
		supervisor.MetricWedged,
	}

	for _, name := range emitted {
		published, mapped := planeSeries[name]
		_, unmapped := unmappedPlaneSeries[name]

		require.NotEqualf(t, mapped, unmapped,
			"the supervisor writes %q and it is in neither planeSeries nor unmappedPlaneSeries: "+
				"give it a catalog series, or say why it has none", name)

		if !mapped {
			continue
		}

		def, known := catalog[published]
		require.Truef(t, known, "%q is mapped to %q, which the catalog does not define", name, published)
		require.NotEmptyf(t, def.Name(), "%q resolves to the zero definition", published)
	}
}

// TestTheBinderLabelsEveryHandleWithItsInstance is what makes one dashboard
// work for storage, sinks and anything added later: the host emits the series,
// so a community plugin cannot label one with the wrong instance.
//
// It asserts the label values the registry is asked for rather than the counts,
// because a build with no observability tag hands out handles that record
// nothing and the wrong labels would be invisible in them.
func TestTheBinderLabelsEveryHandleWithItsInstance(t *testing.T) {
	t.Parallel()

	metrics, err := newObserved()
	require.NoError(t, err)

	instance, err := ref.New(ref.KindSink, "webhook")
	require.NoError(t, err)

	bound, ok := metrics.binder()(instance).(planeMetrics)
	require.True(t, ok)

	require.Equal(t, map[string]string{"kind": "sink", "resource": "webhook"}, bound.labels)

	// The supervisor's own label keys arrive as its spellings and are stored
	// under the catalog's, which is the whole reason the translation exists.
	child, ok := bound.With("op", "deliver").With("outcome", "success").(planeMetrics)
	require.True(t, ok)

	require.Equal(t, map[string]string{
		"kind": "sink", "resource": "webhook", "operation": "deliver", "outcome": "success",
	}, child.labels)

	// The values are ordered the way the definition declares them, so a
	// definition whose labels are reordered cannot silently swap two of them.
	require.Equal(t, []string{"sink", "webhook", "deliver", "success"},
		child.values(defOf("pcl_resource_operations_total")))
}

// TestASeriesTheCatalogDoesNotCarryRecordsNothing keeps an unmapped name from
// reaching the registry as a lookup that fails per call.
func TestASeriesTheCatalogDoesNotCarryRecordsNothing(t *testing.T) {
	t.Parallel()

	metrics, err := newObserved()
	require.NoError(t, err)

	instance, err := ref.New(ref.KindStorage, "local")
	require.NoError(t, err)

	bound := metrics.binder()(instance)

	require.NotNil(t, bound.Counter(supervisor.MetricBreaker))
	require.NotNil(t, bound.Counter("something_nothing_declares"))
	require.NotNil(t, bound.Histogram(supervisor.MetricCalls),
		"a counter asked for as a histogram must not return the counter")
}

// TestTheProcessLoggerCountsItsOwnLines is an earlier design decision's missing half at the
// composition root. The counting handler existing is not the same fact as the
// process using it, and the catalog published pcl_log_lines_total for a build
// where nothing wrote it.
func TestTheProcessLoggerCountsItsOwnLines(t *testing.T) {
	t.Parallel()

	metrics, err := newObserved()
	require.NoError(t, err)

	counting, err := metrics.logging()
	require.NoError(t, err)

	for _, level := range observability.LogLevels() {
		require.NotNilf(t, counting.Emitted(level), "no emitted handle for %s", level)
		require.NotNilf(t, counting.Dropped(level), "no dropped handle for %s", level)
	}

	logger, err := newLogger(config.Log{Level: "info", Format: "json"}, io.Discard, counting, nil)
	require.NoError(t, err)

	_, wrapped := logger.Handler().(*observability.LogHandler)
	require.True(t, wrapped, "the process logger does not pass through the counting handler")
}
