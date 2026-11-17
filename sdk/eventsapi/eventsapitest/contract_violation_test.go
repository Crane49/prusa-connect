package eventsapitest

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// A conformance suite that has never been watched failing is a suite that
// proves nothing. Each case here is a sink that breaks one clause, run through
// the whole of RunProcessorContract, with the run required to fail and to name
// the clause.
//
// The run happens in a child copy of this test binary. A subtest that fails
// fails its parent, so there is no way to assert "this run failed" in-process,
// and the sink whose Close never returns has to be watched failing rather than
// hanging the binary, which is the other half of what is being proved.
const (
	violatorEnv     = "EVENTSAPITEST_VIOLATOR"
	violatorTest    = "TestTheSuiteAgainstAViolatingSink"
	childRunTimeout = 2 * time.Minute

	// deadlockReport is how the runtime ends a process whose every goroutine is
	// blocked.
	deadlockReport = "all goroutines are asleep"
)

// violators are sinks that each break exactly one clause, with the words the
// suite must say when it catches them.
var violators = map[string]struct {
	build func() eventsapi.Processor
	names string
}{
	"blocks-the-caller": {
		build: func() eventsapi.Processor { return &sink{blocking: true} },
		names: "a fire-and-forget call has",
	},
	"panics-on-a-zero-value": {
		build: func() eventsapi.Processor { return &sink{indexesTheID: true} },
		names: "a zero-value event panicked the sink",
	},
	"never-returns-from-close": {
		build: func() eventsapi.Processor { return &sink{closeHangs: true} },
		names: "Close did not return within",
	},
	"panics-on-the-second-close": {
		build: func() eventsapi.Processor { return &sink{closeTwicePanics: true} },
		names: "the second Close panicked the sink",
	},
	"panics-after-close": {
		build: func() eventsapi.Processor { return &sink{closesTheQueue: true} },
		names: "delivered after Close panicked the sink",
	},
	"leaks-a-goroutine": {
		build: func() eventsapi.Processor { return &sink{leaks: true} },
		names: "outlived Close",
	},
	// The leak here is started by a goroutine of the sink's rather than by the
	// caller that built it, so the runtime records a different parent for it on
	// every build. A detector that reads the parent as part of where the
	// goroutine came from sees two unrelated leaks and reports neither.
	"leaks-from-inside-a-goroutine": {
		build: func() eventsapi.Processor { return &sink{leaksIndirectly: true} },
		names: "outlived Close",
	},
}

func TestTheSuiteFailsEveryViolatingSink(t *testing.T) {
	t.Parallel()

	if os.Getenv(violatorEnv) != "" {
		t.Skip("this is the child run; it exists to be failed by the parent's case")
	}

	for name, tc := range violators {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			output, err := runChild(t, name)
			if err == nil {
				t.Fatalf("the suite passed a sink that %s:\n%s", name, output)
			}

			if !strings.Contains(output, tc.names) {
				t.Errorf("the suite failed the sink but never said %q, so the author is not told "+
					"which clause broke:\n%s", tc.names, output)
			}
		})
	}
}

// The same sink with every defect off must pass, or the case above proves only
// that the suite dislikes something.
func TestTheSuitePassesAConformingSink(t *testing.T) {
	t.Parallel()

	if os.Getenv(violatorEnv) != "" {
		t.Skip("this is the child run")
	}

	output, err := runChild(t, "conforming")
	if err != nil {
		t.Fatalf("the suite failed a sink that keeps the contract: %v\n%s", err, output)
	}
}

// TestTheSuiteAgainstAViolatingSink is the child. It runs the whole contract
// against the sink its parent named and is expected to fail.
func TestTheSuiteAgainstAViolatingSink(t *testing.T) {
	name := os.Getenv(violatorEnv)
	if name == "" {
		t.Skip("no violator named: this case only runs as a child of TestTheSuiteFailsEveryViolatingSink")
	}

	// The sink whose Close never returns must be watched failing with the
	// clause named rather than hanging, and there is no reason to wait the
	// production budget out to see it.
	closeBudget = 2 * time.Second
	settleBudget = time.Second

	if name == "conforming" {
		RunProcessorContract(t, func(*testing.T) eventsapi.Processor { return &sink{} })

		return
	}

	tc, ok := violators[name]
	if !ok {
		t.Fatalf("no violator named %q", name)
	}

	RunProcessorContract(t, func(*testing.T) eventsapi.Processor { return tc.build() })
}

func runChild(t *testing.T, name string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), childRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+violatorTest, "-test.v")
	cmd.Env = append(os.Environ(), violatorEnv+"="+name)

	output, err := cmd.CombinedOutput()

	// A child that hangs also exits non-zero in the end, which the cases above
	// would read as the suite failing the fixture. It is the suite hanging on
	// it, which the bounded waits in the suite are there to prevent, and it has
	// to say so. It ends one of two ways: killed at the deadline, or, on a
	// machine idle enough that no timer is pending, by the runtime's deadlock
	// detector. Which one depends on the machine.
	if ctx.Err() != nil {
		t.Fatalf("the suite hung on the %s fixture until it was killed after %s, instead of "+
			"failing it:\n%s", name, childRunTimeout, output)
	}

	if strings.Contains(string(output), deadlockReport) {
		t.Fatalf("the suite hung on the %s fixture until the runtime reported a deadlock, "+
			"instead of failing it:\n%s", name, output)
	}

	return string(output), err
}

const (
	// queueDepth is the sink's buffer. It is small so the flood case fills it
	// and the offer path runs in the state that matters.
	queueDepth = 64

	// drainPerEvent is how long the sender takes over one event. It is past
	// perCallBudget, so a sink that hands its caller's goroutine to the
	// destination is caught rather than merely slowed.
	drainPerEvent = 400 * time.Millisecond
)

// sink is a conforming event sink with a switch for each clause it can be made
// to break. Everything it delivers goes nowhere, because what the suite asserts
// is how a sink behaves and not where its events end up.
type sink struct {
	blocking         bool
	indexesTheID     bool
	closeHangs       bool
	closeTwicePanics bool
	closesTheQueue   bool
	leaks            bool
	leaksIndirectly  bool

	start   sync.Once
	queue   chan struct{}
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func (s *sink) begin() {
	s.start.Do(func() {
		s.queue = make(chan struct{}, queueDepth)
		s.done = make(chan struct{})
		s.stopped = make(chan struct{})

		go func() {
			defer close(s.stopped)

			for {
				select {
				case <-s.done:
					return
				case <-s.queue:
					// The suite points a sink at a destination that never
					// answers, so the sender is deliberately slower than the
					// budget one fire-and-forget call has. A sender that kept
					// up would let a sink that blocks its caller pass.
					//
					// Close abandons the send in flight instead of draining
					// what is queued, which is what a conforming sink does with
					// a dead destination. Waiting the queue out would put this
					// sink's own Close at queue depth times this budget.
					select {
					case <-s.done:
						return
					case <-time.After(drainPerEvent):
					}
				}
			}
		}()

		if s.leaks {
			go func() {
				for {
					time.Sleep(time.Second)
				}
			}()
		}

		if s.leaksIndirectly {
			go func() {
				go func() {
					for {
						time.Sleep(time.Second)
					}
				}()
			}()
		}
	})
}

func (s *sink) offer() {
	s.begin()

	if s.blocking {
		s.queue <- struct{}{}

		return
	}

	// The closed-queue variant deliberately skips the done check, so the send
	// on a closed channel is what happens rather than one of two ready cases.
	if s.closesTheQueue {
		select {
		case s.queue <- struct{}{}:
		default:
		}

		return
	}

	select {
	case <-s.done:
	case s.queue <- struct{}{}:
	default:
	}
}

func (s *sink) PrinterStateChanged(eventsapi.PrinterState)   { s.offer() }
func (s *sink) PrinterLabelsChanged(eventsapi.PrinterLabels) { s.offer() }
func (s *sink) JobUpdated(eventsapi.JobInfo)                 { s.offer() }
func (s *sink) LabelDeleted(string)                          { s.offer() }
func (s *sink) FactObserved(eventsapi.Fact)                  { s.offer() }

func (s *sink) PrinterDeleted(id string) {
	if s.indexesTheID {
		_ = id[0]
	}

	s.offer()
}

func (s *sink) Close() error {
	s.begin()

	if s.closeTwicePanics {
		close(s.done)
	} else {
		s.once.Do(func() { close(s.done) })
	}

	<-s.stopped

	if s.closesTheQueue {
		close(s.queue)
	}

	if s.closeHangs {
		select {}
	}

	return nil
}
