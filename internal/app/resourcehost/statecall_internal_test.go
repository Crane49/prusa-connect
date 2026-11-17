package resourcehost

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// This file is in the package because the bound is a property of the gate and
// not of the deployment. A module's state calls reach the host over one wire.
// On a runner with one CPU the host finishes each call before it reads the
// next, so it may never hold two at once.

// heldRecords is a store whose writes stay inside it until release is closed,
// so a case about the bound decides on the semaphore and not on the scheduler.
// Only Put is held. The embedded interface is nil, and a case that reached any
// other method would panic rather than pass.
//
// entered holds one token per write inside, up to its capacity. A write that
// finds it full returns at once, so a gate with no bound fails the case rather
// than hanging it.
type heldRecords struct {
	stateapi.Records

	release chan struct{}
	entered chan struct{}
}

func (h *heldRecords) Put(context.Context, stateapi.Key, []byte) (uint64, error) {
	select {
	case h.entered <- struct{}{}:
		<-h.release
	default:
	}

	return 1, nil
}

// TestAModuleAtItsStateLimitIsRefusedAndNotQueued is ADR-20260827-wgc rule
// 12's concurrency limit, held at the gate with the slots full for certain.
//
// The refusal names the width and the instance, because rule 12 asks that an
// author read the limit rather than guess it.
func TestAModuleAtItsStateLimitIsRefusedAndNotQueued(t *testing.T) {
	t.Parallel()

	const width = 2

	instance, err := ref.New(ref.KindSink, "planner")
	require.NoError(t, err)

	store := &heldRecords{release: make(chan struct{}), entered: make(chan struct{}, width)}
	gate := newGate(nil, instance, supervisor.Policy{Concurrency: width}, store)

	// A case that fails while the writes are held still lets them out.
	release := sync.OnceFunc(func() { close(store.release) })
	t.Cleanup(release)

	// The readiness answer is an input to this gate, not the mechanism it is
	// being held to.
	gate.serving = func() string { return "" }

	var held sync.WaitGroup

	held.Add(width)

	for i := range width {
		go func() {
			defer held.Done()

			_, _ = gate.Put(t.Context(), stateapi.Key("held/"+strconv.Itoa(i)), []byte("x"))
		}()
	}

	// Both writes are inside the store, so both hold a slot.
	require.Eventually(t, func() bool {
		return len(store.entered) == width
	}, 5*time.Second, time.Millisecond, "the writes never all reached the store")

	// The call over the width runs on its own goroutine, so a gate that queues
	// it fails the case rather than hanging it.
	answered := make(chan error, 1)

	go func() {
		_, over := gate.Put(t.Context(), "over", []byte("x"))
		answered <- over
	}()

	var refused error

	select {
	case refused = <-answered:
	case <-time.After(5 * time.Second):
		t.Fatal("a call over the width waited for a slot, so the excess is queued and not refused")
	}

	require.ErrorIs(t, refused, stateapi.ErrBusy,
		"a call over the width was not refused, so nothing bounds this door")
	require.Contains(t, refused.Error(), strconv.Itoa(width),
		"the refusal does not name the width")
	require.Contains(t, refused.Error(), instance.String(),
		"the refusal does not say whose limit was reached")

	release()
	held.Wait()

	// And the door opens again once the slots are free, so this is a limit and
	// not a closed door.
	_, err = gate.Put(t.Context(), "after", []byte("x"))
	require.NoError(t, err)
}
