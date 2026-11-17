package xbuddy

import (
	"context"
	"errors"

	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
)

// Where the registration table meets the store it survives a restart in.
//
// The table is one lock over a whole fleet and the store is a database or a
// disk, so the two are kept apart. Every write here is decided under the lock
// and made with the lock released, and it carries the order the table gave it.

// storeWrite is one write this table has decided on and has not made yet.
//
// A write made under the lock holds every handshake, every authentication and
// the operator's listing behind it for as long as the store takes. Made with
// the lock released, a slow store holds up the printer it is writing about and
// nobody else.
//
// Everything the write needs is copied into it, because the table it came out
// of may have moved on by the time it runs.
type storeWrite struct {
	store     admission.Store
	connector string
	// record is the row to write and forget is the code to remove. A write sets
	// one of the two.
	record admission.Record
	forget string
	// after closes when the write before this one about the same registration
	// has landed, and is nil for the first. mine closes when this one has.
	//
	// The order is per registration and never across the table, so two
	// decisions about one printer reach the store in the order the table took
	// them and a slow write about one printer delays nobody else. Without it a
	// rejection overtakes the save in front of it and the row it removed is
	// written back.
	after <-chan struct{}
	mine  chan struct{}
}

// run makes the write. The zero value is what a driver with no store owes, and
// it does nothing.
func (w storeWrite) run(ctx context.Context) error {
	if w.mine == nil {
		return nil
	}

	// Closed on every path, the one that gives up included. The write behind
	// this one is waiting for it, and a turn nobody releases stalls that
	// registration for as long as the process runs.
	defer close(w.mine)

	if w.after != nil {
		select {
		case <-w.after:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if w.forget != "" {
		return w.store.Forget(ctx, w.connector, w.forget)
	}

	return w.store.Save(ctx, w.record)
}

// runWrites makes several writes in the order the table gave them out.
//
// Every one is made, even after one has failed, because each holds a turn the
// writes behind it are waiting for. The failures are joined, so a caller reads
// what went wrong rather than the first of it.
func runWrites(ctx context.Context, writes []storeWrite) error {
	var failures []error

	for _, write := range writes {
		if err := write.run(ctx); err != nil {
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}

// queueLocked puts one more write at the end of this registration's queue and
// returns the two ends of the handover. The caller holds the lock.
func queueLocked(item *entry) (after <-chan struct{}, mine chan struct{}) {
	previous := item.lastWrite
	mine = make(chan struct{})
	item.lastWrite = mine

	return previous, mine
}

// saveLocked is the write that stores one entry, and the zero write for a
// driver with no store. The caller holds the lock and makes the write with the
// lock released.
func (r *registrar) saveLocked(item *entry) storeWrite {
	if r.store == nil {
		return storeWrite{}
	}

	after, mine := queueLocked(item)

	return storeWrite{
		store:     r.store,
		connector: r.connector,
		record:    item.recordOf(r.connector, r.kind),
		after:     after,
		mine:      mine,
	}
}

// forgetStoredLocked is the write that removes one entry from the store. The
// caller holds the lock.
func (r *registrar) forgetStoredLocked(item *entry) storeWrite {
	if r.store == nil {
		return storeWrite{}
	}

	after, mine := queueLocked(item)

	return storeWrite{
		store:     r.store,
		connector: r.connector,
		forget:    item.Code,
		after:     after,
		mine:      mine,
	}
}
