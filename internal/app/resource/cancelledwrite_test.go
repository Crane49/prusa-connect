package resource_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// TestAWriteWhoseCallerGaveUpStillStartsTheInstance holds the store to the one
// thing a committed write owes: the rest of the server agrees with the file.
//
// The caller here goes away in the middle of the write, which is what an
// operator's client does when it times out and what the route budget does when
// a loaded server runs past it. The declaration is already on disk by then, so
// the plan that starts it must still be carried out.
//
// The failure this refuses is silent and permanent. Reconcile adopts the
// document it applied, so a plan dropped here is a plan no later write and no
// later reload produces again: the instance stays declared, answers 200 on a
// read, and nothing supervises it until the process restarts.
func TestAWriteWhoseCallerGaveUpStillStartsTheInstance(t *testing.T) {
	t.Parallel()

	plane := &planeThatWaitsOnItsCaller{}
	harness := newHarness(t, func(cfg *resource.Config) { cfg.Plane = plane })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// The client hangs up at the moment the plan reaches the supervision
	// plane, which is the widest part of the window a slow write opens.
	plane.hangUp = cancel

	instance := mustRef(t, ref.KindSink, "workshop")

	_, err := harness.store.Apply(ctx, owner, instance,
		spec("webhook", `{"url":"http://127.0.0.1:1/hook"}`),
		httpapi.Precondition{MustNotExist: true})
	require.NoError(t, err)

	require.Equal(t, []ref.Ref{instance}, plane.startedInstances(),
		"the caller went away and the declaration on disk was never started")

	// A reload reads the same file and diffs against the document the write
	// adopted, so it repairs nothing. The start above is the only one there
	// will ever be, which is why it has to happen.
	require.NoError(t, harness.store.Reload(t.Context()))
	require.Equal(t, []ref.Ref{instance}, plane.startedInstances(),
		"a reload started the instance twice, so the case is measuring the wrong thing")
}

// planeThatWaitsOnItsCaller is the supervision plane as
// internal/app/resourcehost implements it. The plan is handed to the host's own
// loop and waited for on the context the caller passed in, so a caller whose
// context has ended is answered with that error and nothing in the plan runs.
type planeThatWaitsOnItsCaller struct {
	mu      sync.Mutex
	started []ref.Ref
	// hangUp ends the caller's context, once, before the plan is read. It
	// stands for the client that stopped waiting for the answer.
	hangUp func()
}

var _ resource.Plane = (*planeThatWaitsOnItsCaller)(nil)

func (p *planeThatWaitsOnItsCaller) Reconcile(ctx context.Context, plan resource.Plan) error {
	p.mu.Lock()

	if p.hangUp != nil {
		hangUp := p.hangUp
		p.hangUp = nil

		p.mu.Unlock()
		hangUp()
		p.mu.Lock()
	}

	defer p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	for _, step := range plan.Steps {
		if step.Outcome == resource.OutcomeStart {
			p.started = append(p.started, step.Ref)
		}
	}

	return nil
}

// startedInstances is every instance this plane was asked to start, in order.
func (p *planeThatWaitsOnItsCaller) startedInstances() []ref.Ref {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]ref.Ref(nil), p.started...)
}

func (p *planeThatWaitsOnItsCaller) Status(ref.Ref) (httpapi.ResourceStatus, bool) {
	return httpapi.ResourceStatus{}, false
}

func (p *planeThatWaitsOnItsCaller) Documented(ref.Ref) bool { return false }

func (p *planeThatWaitsOnItsCaller) UI(ref.Ref) (httpapi.ResourceUI, bool) {
	return httpapi.ResourceUI{}, false
}

func (p *planeThatWaitsOnItsCaller) Database(ref.Ref) (wirerpc.DatabaseState, bool) {
	return wirerpc.DatabaseState{}, false
}

func (p *planeThatWaitsOnItsCaller) Restart(ref.Ref) bool { return false }

func (p *planeThatWaitsOnItsCaller) MemoryHistory(ref.Ref, time.Time) (httpapi.ResourceMemoryHistory, bool) {
	return httpapi.ResourceMemoryHistory{}, false
}

func (p *planeThatWaitsOnItsCaller) Log(ref.Ref) (httpapi.ResourceLog, bool) {
	return httpapi.ResourceLog{}, false
}
