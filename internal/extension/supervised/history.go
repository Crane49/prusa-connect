package supervised

import (
	"context"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
	"github.com/prusa3d/prusa-connect/sdk/transferapi"
)

// ErrNotAHistoryProvider reports an instance that is not a history provider, or
// one whose target is not the contract the feed would write into.
var ErrNotAHistoryProvider = errors.New("supervised: the instance is not a history provider")

// History is a supervised history provider. It satisfies historyapi.Provider,
// so the feed and the read surface both talk to the contract and neither one
// knows it has a supervised instance.
type History struct {
	instance *supervisor.Instance[any]
}

var _ historyapi.Provider = (*History)(nil)

// AttachHistory wraps an instance the host is already supervising. That is how
// a provider declared under `history:` is fed without a second supervision loop
// over one name.
//
// The kind alone is not enough, for the reason AttachSink gives. The runtime a
// declaration asked for decides what the host built, and no contract crosses
// the wire. A `runtime: service` instance under this kind would build a module
// the feed could write nothing into, and attaching on the kind alone would
// subscribe it, count one failed call per batch, and report ready the whole
// time.
func AttachHistory(instance *supervisor.Instance[any]) (*History, error) {
	if instance == nil {
		return nil, fmt.Errorf("%w: no instance", ErrNotAHistoryProvider)
	}

	if kind := instance.Ref().Kind(); kind != ref.KindHistory {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotAHistoryProvider, instance.Ref(), kind)
	}

	if err := storesHistory(instance); err != nil {
		return nil, err
	}

	return &History{instance: instance}, nil
}

// storesHistory checks the instance against the contract before anything
// subscribes to feed it. A target that is not there yet is not a mismatch. An
// instance still starting, or one climbing the restart ladder, is refused as
// not serving, and the feed's next pass attaches it.
func storesHistory(instance *supervisor.Instance[any]) error {
	mismatch, serving := supervisor.Peek(instance, func(target any) error {
		if _, ok := target.(historyapi.Provider); ok {
			return nil
		}

		return fmt.Errorf("%w: %s built a %T, which stores nothing. A history provider is "+
			"runtime: builtin. No contract crosses the wire, so runtime: service under this "+
			"kind would build a module with no method the feed could call",
			ErrNotAHistoryProvider, instance.Ref(), target)
	})

	if !serving {
		return fmt.Errorf("%w: %w: %s is %s",
			ErrNotAHistoryProvider, ErrNotServing, instance.Ref(), instance.State())
	}

	return mismatch
}

// Instance returns the supervised instance, for the host that runs it.
func (h *History) Instance() *supervisor.Instance[any] { return h.instance }

// Ref returns the instance's own name.
func (h *History) Ref() ref.Ref { return h.instance.Ref() }

// Serving reports whether the provider is up. The read surface asks before it
// resolves a query, because a restarting instance must answer the same as one
// that was never declared.
func (h *History) Serving() bool {
	_, serving := supervisor.Peek(h.instance, func(any) error { return nil })

	return serving
}

// Caps is what this provider serves. Like a storage driver's Capabilities it is
// not supervised. The contract requires it to be cheap and to do no I/O, and
// the resolver asks it on every query.
func (h *History) Caps() historyapi.Caps {
	caps, _ := supervisor.Peek(h.instance, func(target any) historyapi.Caps {
		provider, ok := target.(historyapi.Provider)
		if !ok {
			return 0
		}

		return provider.Caps()
	})

	return caps
}

// Retention is what this provider keeps, read the same way Caps is.
func (h *History) Retention() []historyapi.Resolution {
	held, _ := supervisor.Peek(h.instance, func(target any) []historyapi.Resolution {
		provider, ok := target.(historyapi.Provider)
		if !ok {
			return nil
		}

		return provider.Retention()
	})

	return held
}

// AppendSamples stores a batch of measurements through the instance's queue.
func (h *History) AppendSamples(ctx context.Context, samples []historyapi.Sample) error {
	return supervisor.CallVoid(ctx, h.instance, "append_samples",
		func(callCtx context.Context, target any) error {
			return providerOf(target).AppendSamples(callCtx, samples)
		})
}

// AppendFacts stores a batch of events through the instance's queue.
func (h *History) AppendFacts(ctx context.Context, facts []historyapi.Fact) error {
	return supervisor.CallVoid(ctx, h.instance, "append_facts",
		func(callCtx context.Context, target any) error {
			return providerOf(target).AppendFacts(callCtx, facts)
		})
}

// QuerySamples answers a range over the series the caller may read.
func (h *History) QuerySamples(
	ctx context.Context, query historyapi.SampleQuery,
) (historyapi.SampleResult, error) {
	return answered(ctx, h.instance, "query_samples",
		func(callCtx context.Context, provider historyapi.Provider) (historyapi.SampleResult, error) {
			return provider.QuerySamples(callCtx, query)
		})
}

// QueryFacts answers a window of events the caller may read.
func (h *History) QueryFacts(
	ctx context.Context, query historyapi.FactQuery,
) (historyapi.FactResult, error) {
	return answered(ctx, h.instance, "query_facts",
		func(callCtx context.Context, provider historyapi.Provider) (historyapi.FactResult, error) {
			return provider.QueryFacts(callCtx, query)
		})
}

// QueryCommands answers the folded command rows.
func (h *History) QueryCommands(
	ctx context.Context, query historyapi.CommandQuery,
) (historyapi.CommandResult, error) {
	return answered(ctx, h.instance, "query_commands",
		func(callCtx context.Context, provider historyapi.Provider) (historyapi.CommandResult, error) {
			return provider.QueryCommands(callCtx, query)
		})
}

// answered runs one query under supervision, and hands a refusal back to the
// caller outside it.
//
// A provider that refuses a window it has expired, or a capability it does not
// serve, answered the query correctly: the request was one it will not answer.
// Returned through the supervisor, the refusal is an error it cannot place, it
// counts as a failure, and a page that asks for a few windows the provider has
// expired opens the breaker and takes history away from every caller. So the
// supervised call ends as a success and the refusal is returned after it.
func answered[R any](
	ctx context.Context, instance *supervisor.Instance[any], op string,
	query func(context.Context, historyapi.Provider) (R, error),
) (R, error) {
	var refusal error

	result, err := supervisor.Call(ctx, instance, op,
		func(callCtx context.Context, target any) (R, error) {
			answer, err := query(callCtx, providerOf(target))
			if errors.Is(err, historyapi.ErrQueryRefused) || errors.Is(err, historyapi.ErrUnsupported) {
				refusal = err

				return answer, nil
			}

			return answer, err
		})
	if refusal != nil {
		var none R

		return none, refusal
	}

	return result, err
}

// providerOf reads the target as the contract. AttachHistory already refused an
// instance whose target is not one, and a restart builds from the same
// declaration, so a target that is not a provider here means the build changed
// under a running instance.
func providerOf(target any) historyapi.Provider {
	provider, ok := target.(historyapi.Provider)
	if !ok {
		return refusing{}
	}

	return provider
}

// refusing answers every call with the mismatch, so a target that is not a
// provider costs one refused batch and not a panic inside a supervised call.
type refusing struct{}

func (refusing) Caps() historyapi.Caps              { return 0 }
func (refusing) Retention() []historyapi.Resolution { return nil }
func (refusing) AppendSamples(context.Context, []historyapi.Sample) error {
	return ErrNotAHistoryProvider
}

func (refusing) AppendFacts(context.Context, []historyapi.Fact) error { return ErrNotAHistoryProvider }

func (refusing) QuerySamples(
	context.Context, historyapi.SampleQuery,
) (historyapi.SampleResult, error) {
	return historyapi.SampleResult{}, ErrNotAHistoryProvider
}

func (refusing) QueryFacts(context.Context, historyapi.FactQuery) (historyapi.FactResult, error) {
	return historyapi.FactResult{}, ErrNotAHistoryProvider
}

// TransferRecords is the driver's transfer projection, and false for a driver
// that keeps none.
//
// It is a probe and not a method this wrapper always answers. A wrapper that
// implemented sdk/transferapi.Records whatever it wrapped would make the host's
// own probe report a table that is not there, and the caller would read an
// empty page instead of learning that this build stores no transfer history.
// What comes back runs every read through the supervisor, like every other call
// into a driver.
func (h *History) TransferRecords() (transferapi.Records, bool) {
	kept, serving := supervisor.Peek(h.instance, func(target any) bool {
		_, ok := target.(transferapi.Records)

		return ok
	})

	if !serving || !kept {
		return nil, false
	}

	return supervisedTransfers{instance: h.instance}, true
}

// supervisedTransfers reads one driver's transfer projection under supervision.
type supervisedTransfers struct {
	instance *supervisor.Instance[any]
}

// Transfers answers the query through the driver, inside a supervised call. The
// assertion is made again here and not captured earlier. An instance that
// restarted between the probe and the read is a different value, and keeping
// the old one would read a store the host no longer supervises.
func (s supervisedTransfers) Transfers(
	ctx context.Context, query transferapi.Query,
) ([]transferapi.Record, error) {
	return supervisor.Call(ctx, s.instance, "transfers",
		func(callCtx context.Context, target any) ([]transferapi.Record, error) {
			records, ok := target.(transferapi.Records)
			if !ok {
				return nil, transferapi.ErrNoRecords
			}

			return records.Transfers(callCtx, query)
		})
}

func (refusing) QueryCommands(
	context.Context, historyapi.CommandQuery,
) (historyapi.CommandResult, error) {
	return historyapi.CommandResult{}, ErrNotAHistoryProvider
}
