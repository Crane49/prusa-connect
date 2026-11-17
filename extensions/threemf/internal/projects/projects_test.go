package projects

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

// One version of a file is read once however many callers ask, a read
// stopped by its deadline is not kept, and a file that is no project stays
// no project.
func TestAProjectIsReadOnceAndAStoppedReadIsNotKept(t *testing.T) {
	t.Parallel()

	held := newCache(projectEntries, projectBytes, sizeOfProject, keptProject)
	k := key{storage: "bench", path: "a.3mf", size: 1, modified: 1, object: -1}

	var reads atomic.Int32

	for range 3 {
		answer := held.once(t.Context(), k, func() projectRead {
			reads.Add(1)

			return projectRead{project: &threemf.Project{}}
		})
		if answer.project == nil {
			t.Fatal("no project")
		}
	}

	stopped := key{storage: "bench", path: "b.3mf", size: 1, modified: 1, object: -1}
	for range 2 {
		held.once(t.Context(), stopped, func() projectRead {
			reads.Add(1)

			return projectRead{err: context.DeadlineExceeded}
		})
	}

	refused := key{storage: "bench", path: "c.3mf", size: 1, modified: 1, object: -1}
	for range 2 {
		held.once(t.Context(), refused, func() projectRead {
			reads.Add(1)

			return projectRead{err: errors.Join(threemf.ErrFormat)}
		})
	}

	if got := reads.Load(); got != 4 {
		t.Errorf("read %d times, want 4", got)
	}
}

// The cache drops the oldest answers to stay inside its count.
func TestTheCacheStaysInsideItsBounds(t *testing.T) {
	t.Parallel()

	held := newCache(2, 1<<20, func(key, int) int { return 1 }, func(int) bool { return true })

	for i := range 5 {
		held.once(t.Context(), key{path: string(rune('a' + i))}, func() int { return i })
	}

	if len(held.entries) != 2 || len(held.order) != 2 {
		t.Errorf("%d entries held, want 2", len(held.entries))
	}
}

// A caller that leaves while another caller's read runs answers nothing, and
// the read goes on for the one who started it.
func TestACallerWhoLeavesDoesNotStopTheRead(t *testing.T) {
	t.Parallel()

	held := newCache(2, 1<<20, func(key, int) int { return 1 }, func(int) bool { return true })
	k := key{path: "slow"}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan int)

	go func() {
		done <- held.once(context.Background(), k, func() int {
			close(started)
			<-release

			return 7
		})
	}()

	<-started

	gone, cancel := context.WithCancel(t.Context())
	cancel()

	if got := held.once(gone, k, func() int { return 1 }); got != 0 {
		t.Errorf("a caller who left got %d", got)
	}

	close(release)

	if got := <-done; got != 7 {
		t.Errorf("the reader got %d", got)
	}
}
