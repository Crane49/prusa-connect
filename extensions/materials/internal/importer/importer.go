// Package importer loads the catalogue in the background, one run at a time.
package importer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/prusa3d/materials/internal/opt"
	"github.com/prusa3d/materials/internal/stock"
)

type Phase string

const (
	Idle    Phase = "idle"
	Running Phase = "running"
	Failed  Phase = "failed"
)

// timeout bounds one run, download included.
const timeout = 10 * time.Minute

var (
	// ErrRunning answers a Start while another run is in flight.
	ErrRunning = errors.New("an import is already running")
	// ErrClosed answers a Start after Close: a request that reached the
	// importer a configure replaced.
	ErrClosed = errors.New("the importer is closed")
)

// Reason names the step a failed run stopped at. Anyone who reads the
// catalogue reads it, so it carries no error text: that names local paths
// and addresses, and goes to the module's log instead.
type Reason string

const (
	// ReasonDownload is a ZIP that could not be fetched.
	ReasonDownload Reason = "download"
	// ReasonCatalogue is a ZIP that is not a catalogue this module reads.
	ReasonCatalogue Reason = "catalogue"
	// ReasonStore is a catalogue the database could not take.
	ReasonStore Reason = "store"
)

type Status struct {
	Phase             Phase
	Reason            Reason
	Started, Finished time.Time
	Last              *stock.CatalogueMeta
	Unchanged         bool
}

// Fetch answers the catalogue ZIP.
type Fetch func(ctx context.Context) (io.ReaderAt, int64, error)

type Importer struct {
	store *stock.Store
	now   func() time.Time

	// life ends with Close, and with it every run.
	life   context.Context //nolint:containedctx // the runs outlive the request that starts them.
	cancel context.CancelFunc
	runs   sync.WaitGroup

	mu     sync.Mutex
	status Status
}

func New(store *stock.Store, now func() time.Time) *Importer {
	life, cancel := context.WithCancel(context.Background())

	return &Importer{store: store, now: now, life: life, cancel: cancel, status: Status{Phase: Idle}}
}

// Close cancels a run in flight and waits for it, so none writes to a store
// that is closed after it.
func (i *Importer) Close() {
	// Under the lock, so a Start either adds its run before the wait or sees the end.
	i.mu.Lock()
	i.cancel()
	i.mu.Unlock()

	i.runs.Wait()
}

// Start begins a run on its own goroutine, under the importer's life rather
// than the request's.
func (i *Importer) Start(fetch Fetch) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.life.Err() != nil {
		return ErrClosed
	}

	if i.status.Phase == Running {
		return ErrRunning
	}

	i.status = Status{Phase: Running, Started: i.now(), Last: i.status.Last}

	i.runs.Go(func() { i.run(fetch) })

	return nil
}

// Status is a copy of the state of the last or current run.
func (i *Importer) Status() Status {
	i.mu.Lock()
	defer i.mu.Unlock()

	st := i.status
	if st.Last != nil {
		last := *st.Last
		st.Last = &last
	}

	return st
}

func (i *Importer) run(fetch Fetch) {
	ctx, cancel := context.WithTimeout(i.life, timeout)
	defer cancel()

	meta, unchanged, reason, err := i.load(ctx, fetch)

	i.mu.Lock()
	defer i.mu.Unlock()

	i.status.Finished = i.now()

	if err != nil {
		fmt.Fprintf(os.Stderr, "materials: the catalogue import failed at the %s step: %v\n", reason, err)
		i.status.Phase, i.status.Reason = Failed, reason

		return
	}

	i.status.Phase, i.status.Reason, i.status.Unchanged, i.status.Last = Idle, "", unchanged, &meta
}

func (i *Importer) load(ctx context.Context, fetch Fetch) (stock.CatalogueMeta, bool, Reason, error) {
	r, size, err := fetch(ctx)
	if err != nil {
		return stock.CatalogueMeta{}, false, ReasonDownload, err
	}

	// A download is a temporary file, gone once the catalogue is read.
	if c, ok := r.(io.Closer); ok {
		defer c.Close()
	}

	snap, err := opt.Read(r, size, opt.DefaultLimits)
	if err != nil {
		return stock.CatalogueMeta{}, false, ReasonCatalogue, err
	}

	meta, unchanged, err := i.store.ReplaceCatalogue(ctx, snap, i.now())

	return meta, unchanged, ReasonStore, err
}
