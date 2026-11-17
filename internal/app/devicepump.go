package app

import (
	"context"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/deviceissue"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// pump reads one printer's state and its reachability until the driver closes
// the state channel, then closes the wrapped session and retires the entry.
//
// Closing here is the whole of the host's half of the contract: connectorapi
// says the host owns the session from Arrived and releases it when the state
// channel closes, and a host that only dropped the reference would leave the
// driver holding a session nothing will ever close.
//
// Reachability and the link reading are read on this goroutine rather than one
// of their own, because all three are the same printer's story and a second
// goroutine would have to be ordered against this one to tell it.
func (d *devices) pump(entry *device) {
	defer d.pumps.Done()

	state := entry.session.State()
	reach := d.reachabilityOf(entry)
	reading := d.linkReportOf(entry)
	issues := d.issueReportOf(entry, reading.last)
	storages := entry.session.StoragesChanged()
	files := entry.session.FilesChanged()

	defer reach.stop()
	defer reading.tick.Stop()
	defer issues.stop()

	for {
		select {
		case patch, open := <-state:
			if !open {
				d.retirePrinter(entry)

				return
			}

			// The event carries what the host kept, so the stream and the
			// stored tree say the same thing. A patch the host kept nothing
			// of reaches no subscriber, and is counted on the entry.
			held, kept := entry.record(patch)
			if !kept {
				continue
			}

			d.publish(context.Background(), entry.id, event.TypeDeviceTelemetrySampled,
				event.ClassTelemetry, held.Path, sampleOf(held))

			if strings.HasPrefix(held.Path, deviceissue.DialogSubtree) {
				issues.touch()
			}

			if entry.warm.Load() {
				d.tryWarmUp(entry)
			}
		case <-reach.changed:
			reach.wake()
			issues.touch()
		case <-reach.quiet.C:
			reach.elapsed()
			issues.touch()
		case <-reading.tick.C:
			reading.sample()
			issues.touch()
		case <-issues.settle.C:
			issues.elapsed(reading.last)
		case <-storages:
			d.storagesChanged(entry)
		case change := <-files:
			d.printerChangedFile(entry, change)
		}
	}
}
