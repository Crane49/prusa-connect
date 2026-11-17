package supervised_test

import (
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// fakeProcessor is an event sink that can be made slow.
type fakeProcessor struct {
	mu       sync.Mutex
	states   []eventsapi.PrinterState
	entered  chan struct{}
	release  chan struct{}
	closed   bool
	blocking bool
}

func newFakeProcessor(blocking bool) *fakeProcessor {
	return &fakeProcessor{
		entered:  make(chan struct{}, 1),
		release:  make(chan struct{}),
		blocking: blocking,
	}
}

func (p *fakeProcessor) PrinterStateChanged(state eventsapi.PrinterState) {
	if p.blocking {
		select {
		case p.entered <- struct{}{}:
		default:
		}

		<-p.release
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.states = append(p.states, state)
}

func (p *fakeProcessor) PrinterLabelsChanged(eventsapi.PrinterLabels) {}
func (p *fakeProcessor) PrinterDeleted(string)                        {}
func (p *fakeProcessor) JobUpdated(eventsapi.JobInfo)                 {}
func (p *fakeProcessor) LabelDeleted(string)                          {}
func (p *fakeProcessor) FactObserved(eventsapi.Fact)                  {}

func (p *fakeProcessor) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true

	return nil
}

func (p *fakeProcessor) delivered() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.states)
}

func (p *fakeProcessor) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.closed
}
