package app

// Retiring a device an operator has removed.
//
// A retirement is about the identity and not about a session. A departure says
// one session ended and the next session clears it; this says the id has
// stopped naming a machine, and nothing clears it. The record, the prints, the
// command log and every grant written on the id stay exactly where they are,
// which is what makes a retirement different from a delete: an operator reading
// last month's prints finds them under the id they were filed on.
//
// The machine loses its access with it. The connector that admitted it is told
// to take back the credential it issued, so the printer is refused on its next
// call and has to be added again, and approved again where a person approves.

import (
	"context"
	"time"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// retirementRemovedByOperator is why a device id stops naming a machine when a
// person takes the machine off this server.
//
// It is the second value this build writes, beside retirementOwnerChanged, and
// the set stays open for the reason that one gives: a client reading
// "retired: true" could not tell a machine that changed hands from one somebody
// unplugged for good.
const retirementRemovedByOperator = "removed_by_operator"

// Retire marks one device as retired, so its id stops naming a machine.
//
// It takes administer on the device, which is what naming and tagging take:
// removing a machine from a fleet is an administrative act and not something
// the machine did.
//
// Four things happen, in this order. The connector that holds the device takes
// back the credential it issued, so the machine is refused when it dials again.
// The retirement is written to the record store before this returns, so a
// process that stops straight afterwards comes back with the device still
// retired. The session is closed where one is open, so the machine stops
// reporting into a record an operator has finished with. And the identity
// binding is released, so a machine admitted again later is given a new device
// id rather than reviving this record.
//
// The credential goes first. Closing the session makes the printer dial again
// at once, and a credential still valid at that moment would let it straight
// back in as a new device. A failure on the way leaves the device on the
// listing and answers the operator with an error, and a second attempt does
// the whole thing again.
//
// Retiring twice is the same answer as retiring once: the first reason wins,
// because a retirement happens at one moment and a second one would restate it
// with a later one. The second call still asks the connector to take the
// credential back, as of the first retirement, so an operator who repeats a
// removal that failed halfway finishes it.
func (d *devices) Retire(ctx context.Context, principal string, id string) error {
	entry, _, err := d.reachHolding(ctx, principal, id, authz.ActionAdminister)
	if err != nil {
		return err
	}

	reason, at, retired := entry.retirement()

	// A resident device is not retired. A record that is already retired
	// still answers a repeat with 204.
	if !retired && d.isResident(entry.driver) {
		return httpfault.DeviceResident()
	}
	if !retired {
		at = time.Now()
	}

	if !retired || reason == retirementRemovedByOperator {
		if err = d.withdrawCredential(ctx, entry.driver, entry.id, entry.identity, at); err != nil {
			return httpfault.Unexpected("withdraw a removed printer's credential").Wrap(err)
		}
	}

	if retired {
		return nil
	}

	if err = d.retireIdentity(ctx, id, retirementRemovedByOperator, at); err != nil {
		return httpfault.Unexpected("retire a device").Wrap(err)
	}

	d.identities.release(id)

	if d.dropCameras != nil {
		d.dropCameras(ctx, id)
	}

	return nil
}

// useForgetter records the connector that takes back what it issued a device
// of its own. The composition root calls it once per connector, before the
// connector's routes are mounted.
func (d *devices) useForgetter(connector string, forgets connectorapi.ForgetsDevices) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.forgetters == nil {
		d.forgetters = map[string]connectorapi.ForgetsDevices{}
	}

	d.forgetters[connector] = forgets
}

// useResident records a connector whose dialect says the server runs on its
// printers. The composition root calls it once per such connector, before the
// connector's routes are mounted.
func (d *devices) useResident(connector string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.residents[connector] = true
}

func (d *devices) isResident(connector string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.residents[connector]
}

// withdrawCredential asks the connector that holds a device to take back the
// credential it issued the machine before the moment given. A connector that
// issues none, and one that is not mounted, has nothing to take back.
func (d *devices) withdrawCredential(
	ctx context.Context, connector string, id string, identity connectorapi.Identity, before time.Time,
) error {
	d.mu.Lock()
	forgets, holds := d.forgetters[connector]
	d.mu.Unlock()

	if !holds {
		return nil
	}

	if err := forgets.ForgetDevice(ctx, identity, before); err != nil {
		return err
	}

	d.log.InfoContext(ctx, "the connector took back what it issued a removed printer, "+
		"so the printer has to be added again", "printer", id, "connector", connector)

	return nil
}

// withdrawRetiredCredentials asks one connector to take back what it issued
// every device an operator removed. It runs once, when the connector is
// mounted and before a printer can reach it.
//
// It finishes a removal whose credential was not taken back: one made by a
// build that did not take it back, one where the connector's write failed
// after the record was retired, and one on a connector that checked no
// credential at the removal and checks one at this start. The moment passed is the retirement's, so a printer
// added again since keeps the registration it made then. A device that changed
// hands is skipped, because its registration belongs to the new owner. So is a
// machine the fleet holds again under a newer record.
//
// A failure is logged and the server starts. It leaves the credential where
// the removal left it, and refusing to start would take every other printer
// off the server too.
func (d *devices) withdrawRetiredCredentials(
	ctx context.Context, connector string, forgets connectorapi.ForgetsDevices,
) {
	stored, err := d.records.DeviceRecords(ctx)
	if err != nil {
		d.log.ErrorContext(ctx, "the removed printers' credentials could not be checked, "+
			"so a removed printer may still connect", "connector", connector, "error", err.Error())

		return
	}

	// A machine the fleet holds again under a new record joined after the
	// removal, through an approval or through a connector that admits every
	// printer. Its registration is the one that new record is using.
	live := map[string]bool{}

	for _, record := range stored {
		if record.Driver == connector && record.RetiredReason == "" {
			live[record.UnitUID] = true
		}
	}

	for _, record := range stored {
		if live[record.UnitUID] {
			continue
		}

		if record.Driver != connector || record.RetiredReason != retirementRemovedByOperator ||
			record.UnitUID == "" {
			continue
		}

		identity := connectorapi.Identity{
			Vendor: record.Vendor, Model: record.Model, Serial: record.Serial, UnitUID: record.UnitUID,
		}

		if err = forgets.ForgetDevice(ctx, identity, record.RetiredAt); err != nil {
			d.log.ErrorContext(ctx, "a removed printer's credential could not be taken back, "+
				"so it may still connect", "printer", record.DeviceID, "connector", connector,
				"error", err.Error())
		}
	}
}
