package app

// The warm-up: when a printer reports its storages, this server reads the
// root of each one in the background lane, so a person who opens Files finds
// rows already held instead of waiting 12 to 15 s for a USB root of 300 files.
// StoragesChanged wakes on the first answer of every connection, so arrival
// and a link coming back need no hook of their own.

import (
	"context"
)

// warmUp owes the printer a warm-up and runs it if the printer is idle now.
// A printer that is busy is warmed by the first state patch that finds it idle.
func (d *devices) warmUp(entry *device) {
	entry.warm.Store(true)
	d.tryWarmUp(entry)
}

// tryWarmUp runs an owed warm-up on a connected, idle printer: one background
// read of each storage root that is neither held nor in flight. A lost link
// and a retired printer take back a warm-up still owed; a send or a print
// leaves it owed until the printer is idle again. A read already started runs
// to its end.
func (d *devices) tryWarmUp(entry *device) {
	if d.life.Err() != nil || !entry.session.Connected() || !entry.idle() || !entry.warm.CompareAndSwap(true, false) {
		return
	}

	// A failed answer leaves the warm-up owed, so the next patch that finds the
	// printer idle tries again.
	storages, err := entry.session.Storages(context.Background())
	if err != nil {
		entry.warm.Store(true)

		return
	}

	for _, storage := range storages {
		// A print can start while the storages are asked. The roots not yet
		// started stay owed to the next patch that finds the printer idle.
		if !entry.idle() {
			entry.warm.Store(true)

			return
		}

		if _, held := d.listings.get(entry.id, storage.Name); held || storage.Name == "" {
			continue
		}

		if read, mine := d.listings.join(entry.id, storage.Name, true); mine {
			d.startListing(d.life, entry, entry.id, storage.Name, read)
		}
	}
}
