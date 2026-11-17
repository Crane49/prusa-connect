package resourcehost

import (
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/platform/observability"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

const (
	// sampleEvery is how often the watchdog reads a module's memory. A reading
	// walks /proc, which costs a few milliseconds on a busy machine.
	sampleEvery = 2 * time.Second
	// samplesKept is one hour of samples.
	samplesKept = int(time.Hour / sampleEvery)
	// nearPercent is the share of the budget from which a module is near it.
	nearPercent  = 80
	wholePercent = 100
)

// MemorySample is one reading of a module's memory. Bytes is -1 when the host
// could not read it.
type MemorySample struct {
	At    time.Time
	Bytes int64
}

// memoryWatch is one service instance's budget and the last hour of what its
// process used. It outlives the child, so the history survives a restart.
type memoryWatch struct {
	mu     sync.Mutex
	budget int64
	// mode is the instance's on_over_memory, and over says the last reading
	// was over the budget.
	mode string
	over bool
	// samples is a ring: once it holds samplesKept, next is the oldest.
	samples []MemorySample
	next    int
	// watching counts the loops running over a live child. It is a count
	// because a loop that ends after the next child's loop started must not
	// mark the new child as gone.
	watching int

	// limit applies a budget to the running child as a hard limit, where the
	// system has one, and is nil while no child runs. attached says which
	// child it belongs to, so a child that ends late cannot remove the limit
	// of the child started after it.
	limit    func(int64)
	attached int

	// log is the instance's standard error. It is kept and dropped together
	// with the history.
	log *observability.LogRing
}

func newMemoryWatch(budget int64) *memoryWatch {
	return &memoryWatch{budget: budget, log: observability.NewLogRing(logTailLines, logTailBytes)}
}

// setBudget takes effect at the next sample, without a restart, and at once
// as the running child's hard limit.
func (w *memoryWatch) setBudget(budget int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.budget = budget

	if w.limit != nil {
		w.limit(budget)
	}
}

// setMode takes effect at the next sample.
func (w *memoryWatch) setMode(mode string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.mode = mode
}

// attach makes limit the running child's, applies the budget through it, and
// returns the token detach takes.
func (w *memoryWatch) attach(limit func(int64)) int {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.attached++
	w.limit = limit
	limit(w.budget)

	return w.attached
}

// detach drops the limit attach set, unless a later child attached its own.
func (w *memoryWatch) detach(token int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.attached == token {
		w.limit = nil
	}
}

// Samples returns the kept samples, oldest first.
func (w *memoryWatch) Samples() []MemorySample {
	w.mu.Lock()
	defer w.mu.Unlock()

	out := make([]MemorySample, 0, len(w.samples))
	out = append(out, w.samples[w.next:]...)

	return append(out, w.samples[:w.next]...)
}

// points are the samples at or after since, oldest first. A reading the host
// could not take has no value, so it never reads as zero.
func (w *memoryWatch) points(since time.Time) []httpapi.MemoryPoint {
	out := []httpapi.MemoryPoint{}

	for _, sample := range w.Samples() {
		if sample.At.Before(since) {
			continue
		}

		point := httpapi.MemoryPoint{At: sample.At}
		if sample.Bytes >= 0 {
			point.Value = &sample.Bytes
		}

		out = append(out, point)
	}

	return out
}

// push keeps one sample, dropping the oldest once an hour is kept.
func (w *memoryWatch) push(sample MemorySample) {
	if len(w.samples) < samplesKept {
		w.samples = append(w.samples, sample)

		return
	}

	w.samples[w.next] = sample
	w.next = (w.next + 1) % samplesKept
}

// sample keeps one reading. Over the budget it returns the stop to kill for,
// with kill true, or in warn mode warned true on the reading that went over.
// A reading that failed is unknown and does neither.
func (w *memoryWatch) sample(at time.Time, used int64, err error) (stop supervisor.Stop, kill, warned bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err != nil {
		w.push(MemorySample{At: at, Bytes: -1})

		return supervisor.Stop{}, false, false
	}

	w.push(MemorySample{At: at, Bytes: used})

	was := w.over
	w.over = used > w.budget

	if !w.over {
		return supervisor.Stop{}, false, false
	}

	stop = supervisor.Stop{Result: supervisor.ResultOverMemory, At: at, Used: used, Budget: w.budget}
	if w.mode == install.OnOverMemoryWarn {
		return stop, false, !was
	}

	return stop, true, false
}

// watch samples read every sampleEvery until exited or stopped closes. Over
// the budget it calls kill once and returns, or in warn mode calls warn each
// time a reading goes over. It watches stopped as well as exited, because a
// grandchild holding the child's standard error keeps exited open after the
// child is gone.
func (w *memoryWatch) watch(
	exited, stopped <-chan struct{}, read func() (int64, error), kill, warn func(supervisor.Stop),
) {
	w.live(1)
	defer w.live(-1)

	ticker := time.NewTicker(sampleEvery)
	defer ticker.Stop()

	for {
		select {
		case <-exited:
			return
		case <-stopped:
			return
		case now := <-ticker.C:
			// A tick and the exit can be ready together, and select picks
			// either. A child that exited is not read: its pid may already
			// name another process.
			select {
			case <-exited:
				return
			default:
			}

			used, err := read()

			stop, overKill, warned := w.sample(now, used, err)
			if warned {
				warn(stop)
			}

			if overKill {
				kill(stop)

				return
			}
		}
	}
}

// live counts a loop starting or ending. A starting loop watches a new child,
// so the first reading over the budget warns again.
func (w *memoryWatch) live(delta int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.watching += delta
	if delta > 0 {
		w.over = false
	}
}

// fill writes the budget and the latest reading into a status. A child that
// is not running, or whose last reading failed, leaves used_bytes absent.
func (w *memoryWatch) fill(status *httpapi.ResourceStatus) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if status.Memory == nil {
		status.Memory = &httpapi.ResourceMemory{}
	}

	status.Memory.BudgetBytes = w.budget

	if w.watching == 0 || len(w.samples) == 0 {
		return
	}

	last := w.samples[(w.next+len(w.samples)-1)%len(w.samples)]
	if last.Bytes < 0 {
		return
	}

	used := last.Bytes
	status.Memory.UsedBytes = &used
	status.Memory.NearBudget = used*wholePercent >= w.budget*nearPercent
	status.Memory.OverBudget = w.over
}
