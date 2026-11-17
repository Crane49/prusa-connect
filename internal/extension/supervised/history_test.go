package supervised_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervised"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// errStoreDown is a provider failing, which the breaker is there to count.
var errStoreDown = errors.New("the store is down")

// fakeHistory refuses every fact query while refuse is set, fails every one
// while fail is set, and answers it otherwise.
type fakeHistory struct {
	refuse atomic.Bool
	fail   atomic.Bool
}

func (*fakeHistory) Caps() historyapi.Caps                                    { return 0 }
func (*fakeHistory) Retention() []historyapi.Resolution                       { return nil }
func (*fakeHistory) AppendSamples(context.Context, []historyapi.Sample) error { return nil }
func (*fakeHistory) AppendFacts(context.Context, []historyapi.Fact) error     { return nil }

func (*fakeHistory) QuerySamples(context.Context, historyapi.SampleQuery) (historyapi.SampleResult, error) {
	return historyapi.SampleResult{}, historyapi.ErrUnsupported
}

func (h *fakeHistory) QueryFacts(context.Context, historyapi.FactQuery) (historyapi.FactResult, error) {
	switch {
	case h.refuse.Load():
		return historyapi.FactResult{}, historyapi.RefuseQuery(
			"the window is older than this store keeps",
			0,
			time.Time{},
		)
	case h.fail.Load():
		return historyapi.FactResult{}, errStoreDown
	}

	return historyapi.FactResult{}, nil
}

func (*fakeHistory) QueryCommands(context.Context, historyapi.CommandQuery) (historyapi.CommandResult, error) {
	return historyapi.CommandResult{}, nil
}

// historyFixture runs one history instance, under the policy the host gives
// every history provider, and attaches the wrapper to it.
func historyFixture(t *testing.T, provider historyapi.Provider) (*supervised.History, func()) {
	t.Helper()

	instance, err := ref.New(ref.KindHistory, "past")
	require.NoError(t, err)

	policy := supervisor.DefaultPolicy()
	policy.Backoff.Spread = 0

	running, err := supervisor.New(supervisor.Config[any]{
		Ref:    instance,
		Type:   "fake",
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

	synctest.Wait()

	stop := func() {
		cancel()
		<-done
	}

	history, err := supervised.AttachHistory(running)
	if err != nil {
		stop()
		require.NoError(t, err)
	}

	return history, stop
}

// A page that asks for windows the provider has expired gets refusals, and
// history stays open to every caller. Counted as failures, a few such pages
// opened the breaker and the next caller read a 500.
func TestARefusedWindowDoesNotOpenTheBreaker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeHistory{}
		history, stop := historyFixture(t, provider)
		defer stop()

		ctx := context.Background()
		provider.refuse.Store(true)

		for range 4 * supervisor.DefaultBreakerMinRequests {
			_, err := history.QueryFacts(ctx, historyapi.FactQuery{})

			var refusal *historyapi.QueryRefusedError
			require.ErrorAs(t, err, &refusal)
			require.ErrorIs(t, err, historyapi.ErrQueryRefused)
		}

		_, err := history.QuerySamples(ctx, historyapi.SampleQuery{})
		require.ErrorIs(t, err, historyapi.ErrUnsupported)

		provider.refuse.Store(false)

		_, err = history.QueryFacts(ctx, historyapi.FactQuery{})
		require.NoError(t, err)
	})
}

// The breaker still opens on a provider that fails, which is what it is for.
func TestAFailingProviderStillOpensTheBreaker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeHistory{}
		history, stop := historyFixture(t, provider)
		defer stop()

		ctx := context.Background()
		provider.fail.Store(true)

		var last error
		for range 4 * supervisor.DefaultBreakerMinRequests {
			_, last = history.QueryFacts(ctx, historyapi.FactQuery{})
		}

		require.Error(t, last)
		assert.NotErrorIs(t, last, errStoreDown, "the breaker refuses the call before the provider sees it")
	})
}
