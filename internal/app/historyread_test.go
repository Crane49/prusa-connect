package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/db/sqlite"
	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/supervised"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// The two machines every case here divides between two principals, and the
// instant their rows carry.
const (
	alphaPrinter = "p-alpha"
	betaPrinter  = "p-beta"
)

var historyBase = time.Date(2026, time.August, 26, 9, 14, 5, 0, time.UTC)

// newHistoryStore is a migrated history store on a file of this test's own,
// built the way the plane builds one: through the adapter's own factory, from
// the opaque settings block an operator writes.
//
// Retention is "0" so the fixture keeps everything. The rows below are dated in
// 2026 and the adapter's default window is 48 hours, so a store that expired on
// the default would answer every query with a refusal.
func newHistoryStore(t *testing.T) historyapi.Provider {
	t.Helper()

	path := filepath.ToSlash(filepath.Join(t.TempDir(), "history.db"))

	settings, err := json.Marshal(map[string]any{
		"path": path, "retention": map[string]any{"raw": "0"},
	})
	require.NoError(t, err)

	store, err := sqlite.New(historyapi.ProviderSpec{ID: "past", Type: sqlite.Type, Config: settings})
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, store.(interface{ Close() error }).Close()) })

	return store
}

// twoPrinterPast is a graph holding two printers, one visible to each of two
// principals, and a store holding a fact and a sample for both of them.
//
// The rows are written for both machines whatever the caller may read, because
// the property under test is what a query answers and not what a store holds. A
// fixture that only stored what the caller was allowed would pass whether or
// not the reduction happened.
func twoPrinterPast(t *testing.T) (historyReader, authz.Subject, authz.Subject) {
	t.Helper()

	graph := newGraph(t)

	owner, err := authz.NewSubject(humanSubjectType, config.DefaultPrincipal)
	require.NoError(t, err)

	alice, err := authz.NewSubject(humanSubjectType, "alice")
	require.NoError(t, err)

	bob, err := authz.NewSubject(humanSubjectType, "bob")
	require.NoError(t, err)

	// A printer is placed in an org before anybody can be granted anything on
	// it: administering a grant takes a grant, and the org is where the first
	// one comes from.
	for who, printer := range map[authz.Subject]string{alice: alphaPrinter, bob: betaPrinter} {
		require.NoError(t, graph.UpsertDevice(t.Context(), printer, printer))
		require.NoError(t, graph.Write(t.Context(), tuple(t,
			authz.TypeDevice, printer, authz.RelOrg, "org:"+bootstrapOrg)))

		wrote, grantErr := graph.WriteGrant(t.Context(), owner, tuple(t,
			authz.TypeDevice, printer, authz.RelViewer, who.String()))
		require.NoError(t, grantErr)
		require.True(t, wrote)
	}

	store := newHistoryStore(t)

	facts := make([]historyapi.Fact, 0, 2)
	samples := make([]historyapi.Sample, 0, 2)

	for i, printer := range []string{alphaPrinter, betaPrinter} {
		facts = append(facts, historyapi.Fact{
			At:       historyBase,
			Ref:      printerOrigin(t, printer),
			Type:     event.TypeDeviceArrived,
			Sequence: uint64(i + 1),
			Epoch:    "epoch-a",
			Payload:  json.RawMessage(`{"device_id":"` + printer + `"}`),
			Class:    event.ClassState,
		})
		samples = append(samples, historyapi.Sample{
			At:     historyBase,
			Ref:    printerOrigin(t, printer),
			Metric: "/tools/0/nozzle_temp",
			Value:  float64(200 + i),
		})
	}

	require.NoError(t, store.AppendFacts(t.Context(), facts))
	require.NoError(t, store.AppendSamples(t.Context(), samples))

	return historyReader{feed: oneProvider{provider: store}, seen: visibility{graph: graph}}, alice, bob
}

// oneProvider is a resolution over exactly one provider: the deployment with a
// single serving instance, which is what every case here is about. The
// fan-out and ambiguity cases are about the feed and live with it.
type oneProvider struct{ provider historyapi.Provider }

func (o oneProvider) reader(string) (historyapi.Provider, error) { return o.provider, nil }

func printerOrigin(t *testing.T, id string) event.Origin {
	t.Helper()

	origin, err := event.NewOrigin(event.KindDevice, id)
	require.NoError(t, err)

	return origin
}

// TestAPastReadIsNarrowedToWhatTheCallerMayObserve is the authorization half of
// this surface, and it is the reason the read exists at all: a caller who
// cannot see a printer in the present must not be able to read its past.
//
// Both principals hold a grant, and each of them holds it on the other's
// machine's neighbour, so a reduction that was skipped, inverted or applied to
// the wrong subject fails here rather than passing on an empty fixture.
func TestAPastReadIsNarrowedToWhatTheCallerMayObserve(t *testing.T) {
	t.Parallel()

	reader, alice, bob := twoPrinterPast(t)

	for name, expected := range map[string]struct {
		who     authz.Subject
		printer string
	}{
		"alice sees her own printer": {who: alice, printer: alphaPrinter},
		"bob sees his own printer":   {who: bob, printer: betaPrinter},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			facts, err := reader.Facts(t.Context(), expected.who.String(), httpapi.HistoryFactQuery{})
			require.NoError(t, err)
			require.Len(t, facts.Items, 1, "the answer carried a printer this caller may not observe")
			require.Equal(t, "device:"+expected.printer, facts.Items[0].Origin)

			samples, err := reader.Samples(t.Context(), expected.who.String(), httpapi.HistorySampleQuery{})
			require.NoError(t, err)
			require.Len(t, samples.Items, 1, "the answer carried a printer this caller may not observe")
			require.Equal(t, "device:"+expected.printer, samples.Items[0].Origin)

			t.Logf("observed: %s read %d fact and %d sample, both of %s",
				expected.who, len(facts.Items), len(samples.Items), expected.printer)
		})
	}
}

// TestNamingAnUnobservableOriginRefusesTheWholeRead holds the other direction.
// A narrowing that named a printer the caller may not observe is refused rather
// than quietly reduced, because a client that asked for two machines and was
// given one is a client watching a machine it does not know it is missing. The
// refusal names neither machine: which printers exist is what it withholds.
func TestNamingAnUnobservableOriginRefusesTheWholeRead(t *testing.T) {
	t.Parallel()

	reader, alice, _ := twoPrinterPast(t)

	for name, asked := range map[string][]event.Origin{
		"the other principal's printer alone": {printerOrigin(t, betaPrinter)},
		"her own printer and the other one":   {printerOrigin(t, alphaPrinter), printerOrigin(t, betaPrinter)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := reader.Facts(t.Context(), alice.String(),
				httpapi.HistoryFactQuery{HistoryWindow: httpapi.HistoryWindow{Origins: asked}})
			requireAccessDenied(t, err)

			_, err = reader.Samples(t.Context(), alice.String(),
				httpapi.HistorySampleQuery{HistoryWindow: httpapi.HistoryWindow{Origins: asked}})
			requireAccessDenied(t, err)
		})
	}
}

// TestAPrincipalWithNoGrantReadsAnEmptyPast is the empty-set rule: no permitted
// origin answers an empty page and never an unfiltered query.
func TestAPrincipalWithNoGrantReadsAnEmptyPast(t *testing.T) {
	t.Parallel()

	reader, _, _ := twoPrinterPast(t)

	stranger, err := authz.NewSubject(humanSubjectType, "stranger")
	require.NoError(t, err)

	facts, err := reader.Facts(t.Context(), stranger.String(), httpapi.HistoryFactQuery{})
	require.NoError(t, err)
	require.Empty(t, facts.Items)
	require.Empty(t, facts.Cursor, "a caller who may read nothing was offered another page")

	samples, err := reader.Samples(t.Context(), stranger.String(), httpapi.HistorySampleQuery{})
	require.NoError(t, err)
	require.Empty(t, samples.Items)
}

// TestAWithdrawnGrantClosesThePastItOpened is the revocation half. The read
// resolves the visible set per query rather than holding one, so a grant taken
// away between two reads takes the past with it.
func TestAWithdrawnGrantClosesThePastItOpened(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)

	owner, err := authz.NewSubject(humanSubjectType, config.DefaultPrincipal)
	require.NoError(t, err)

	alice, err := authz.NewSubject(humanSubjectType, "alice")
	require.NoError(t, err)

	require.NoError(t, graph.UpsertDevice(t.Context(), alphaPrinter, alphaPrinter))
	require.NoError(t, graph.Write(t.Context(), tuple(t,
		authz.TypeDevice, alphaPrinter, authz.RelOrg, "org:"+bootstrapOrg)))

	edge := tuple(t, authz.TypeDevice, alphaPrinter, authz.RelViewer, alice.String())

	wrote, err := graph.WriteGrant(t.Context(), owner, edge)
	require.NoError(t, err)
	require.True(t, wrote)

	store := newHistoryStore(t)
	require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{{
		At: historyBase, Ref: printerOrigin(t, alphaPrinter), Type: event.TypeDeviceArrived,
		Sequence: 1, Epoch: "epoch-a", Payload: json.RawMessage(`{}`),
		Class: event.ClassState,
	}}))

	reader := historyReader{feed: oneProvider{provider: store}, seen: visibility{graph: graph}}

	before, err := reader.Facts(t.Context(), alice.String(), httpapi.HistoryFactQuery{})
	require.NoError(t, err)
	require.Len(t, before.Items, 1)

	require.NoError(t, graph.WithdrawGrant(t.Context(), owner, edge))

	after, err := reader.Facts(t.Context(), alice.String(), httpapi.HistoryFactQuery{})
	require.NoError(t, err)
	require.Empty(t, after.Items, "a withdrawn grant left the past readable")
}

// requireAccessDenied says the refusal is the graph's own and carries no name.
func requireAccessDenied(t *testing.T, err error) {
	t.Helper()

	var refusal *fault.Fault

	require.ErrorAs(t, err, &refusal)
	require.Equal(t, authz.CodeAccessDenied(), refusal.Code())
	require.NotContains(t, refusal.Error(), betaPrinter,
		"the refusal named a printer, so it says which printers exist")
}

// factsOnlyProvider is a provider carrying CapFacts and refusing samples, which
// is a deployment sdk/historyapi allows and this repository ships no example
// of. It exists so the surface is held to the contract rather than to the one
// provider that is built.
type factsOnlyProvider struct{ historyapi.Provider }

func (factsOnlyProvider) Caps() historyapi.Caps { return historyapi.CapFacts }

func (factsOnlyProvider) QuerySamples(
	context.Context, historyapi.SampleQuery,
) (historyapi.SampleResult, error) {
	return historyapi.SampleResult{}, historyapi.ErrUnsupported
}

// TestAProviderThatServesNoSamplesSaysTheFeatureIsAbsent holds the capability
// bits on the wire. A provider may store facts and refuse measurements, and a
// caller reading measurements from one is not sending a malformed request: the
// feature is absent, which is the answer a deployment with no provider gets.
func TestAProviderThatServesNoSamplesSaysTheFeatureIsAbsent(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)

	alice, err := authz.NewSubject(humanSubjectType, "alice")
	require.NoError(t, err)

	reader := historyReader{
		feed: oneProvider{provider: factsOnlyProvider{Provider: newHistoryStore(t)}},
		seen: visibility{graph: graph},
	}

	_, err = reader.Samples(t.Context(), alice.String(), httpapi.HistorySampleQuery{})

	var refusal *fault.Fault

	require.ErrorAs(t, err, &refusal)
	require.Equal(t, httpfault.CodeHistoryNotConfigured(), refusal.Code(),
		"a capability the provider does not carry was reported as a bad request")
}

// failingFacts is a provider whose every fact query fails, so its breaker opens.
type failingFacts struct{ historyapi.Provider }

func (failingFacts) QueryFacts(context.Context, historyapi.FactQuery) (historyapi.FactResult, error) {
	return historyapi.FactResult{}, errors.New("the store is down")
}

// TestAnOpenBreakerReachesTheCallerAsItself holds the answer a caller gets once
// a failing provider's breaker opens: the retryable 503 the supervisor raised,
// with its code. Wrapped as unexpected it was a 500 that told the caller
// nothing they could do.
func TestAnOpenBreakerReachesTheCallerAsItself(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		instance, err := ref.New(ref.KindHistory, "past")
		require.NoError(t, err)

		// The policy the host gives a history provider, started at once.
		policy := supervisor.DefaultPolicy()
		policy.Backoff.Spread = 0

		provider := failingFacts{Provider: newHistoryStore(t)}
		running, err := supervisor.New(supervisor.Config[any]{
			Ref:    instance,
			Type:   "sqlite",
			Policy: policy,
			Start:  func(context.Context, supervisor.Handle) (any, error) { return provider, nil },
			Stop:   func(context.Context, any) error { return nil },
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})

		go func() {
			defer close(done)

			_ = running.Run(ctx)
		}()

		defer func() {
			cancel()
			<-done
		}()

		synctest.Wait()

		history, err := supervised.AttachHistory(running)
		require.NoError(t, err)

		alice, err := authz.NewSubject(humanSubjectType, "alice")
		require.NoError(t, err)

		reader := historyReader{feed: oneProvider{provider: history}, seen: visibility{graph: newGraph(t)}}

		var last error
		for range 4 * supervisor.DefaultBreakerMinRequests {
			_, last = reader.Facts(t.Context(), alice.String(), httpapi.HistoryFactQuery{})
		}

		var refusal *fault.Fault

		require.ErrorAs(t, last, &refusal)
		require.Equal(t, supervisor.CodeBreakerOpen(), refusal.Code(),
			"an open breaker reached the caller as an unexpected failure")
	})
}
