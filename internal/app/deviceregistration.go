package app

// What an approved registration does to a device's identity.
//
// A registration is the identity and the machine is an attribute of it, so
// the approval is where an id is minted and bound. That is also the
// only place this server can see an ownership transfer: a printer that changed
// hands is factory reset and registers again, and the second registration for
// one fingerprint is the whole of the evidence.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
)

// retirementOwnerChanged is why a device id stops being its machine's.
//
// It is one word on the wire and this is the only value this build writes, but
// it is a reason rather than a flag because the set is open by design: a device
// removed from a site, or one whose id an operator replaced by hand, would be
// retired for a reason of its own and a client reading "retired: true" could
// not tell the three apart.
const retirementOwnerChanged = "ownership_changed"

// registrationApproved binds a device id to the fingerprint an approval just let
// through, and transfers the machine away from the device that held it when the
// approval is somebody else's.
//
// Which of the two happens is approvalKeepsTheIdentity, and it is the whole of
// the policy. What reaches it is the device the registration's own fingerprint
// is bound to, and no other spelling: xBuddy puts fifty characters of a
// fingerprint in a registration body and the leading sixteen in every header,
// and the two are put together at the printer's next handshake, where the
// machine holding the credential is what says they are one
// (deviceIdentities.resolveDialledLocked). An approval that matched the
// registration's leading characters against a working printer's spelling
// would hand that printer's device id, its token binding and its ownership to
// a stranger who had read a header off the wire.
//
// A binding with nothing behind it is one this same path minted for a printer
// that has not dialled yet, so approving one code twice mints once and
// transfers nothing.
//
// It asks the identity index and not the departed table. That table bounds the
// state trees this process holds at four hundred, and a site that has met more
// machines than that must still tell a second registration from a first one.
//
// What a transfer does not do is move anything. The old device keeps its
// record, its history rows, its command records and every grant an operator
// wrote on it, and the new id starts with none of them. That is the point: the
// previous owner's fleet screen must not become the new owner's, and a merge or
// a delete are the two ways to make it one.
//
// The retirement is written before the fingerprint is rebound, and a write that
// failed stops the whole thing: an approval that answered 200 having rebound a
// machine onto an id whose predecessor's row still reads live would hand the
// next boot back to the previous owner.
//
// A printer that registers again on its own reaches this only through the
// notice a driver that approves for itself sends (connectorapi.ApprovalNotice),
// and that notice names nobody, so it never transfers anything. An operator's
// second approval of one machine exists only after somebody rejected the first:
// begin hands a fingerprint it already holds an approved entry for that same
// entry, code and token and all. That is the deliberate step a transfer costs.
//
// The id minted here lives in the index and reaches the record store only when
// the printer arrives, because a device record names a row of the authorization
// directory and this path has no machine to register one for yet. So a process
// that stops between an approval and that printer's first handshake comes back
// having forgotten the id, and the printer is given another one when it dials.
// What that costs is the uuid GET /p/auth may already have answered with, and
// nothing else: the transfer itself survives, because the retirement is on the
// disk and restore does not bind a retired row, so the machine still resolves
// to a new device rather than to the one it left.
func (d *devices) registrationApproved(
	ctx context.Context, registration admission.Registration, by string,
) error {
	spelling := identityKeyOf(registration.Fingerprint)
	if spelling == "" {
		// A dialect that approves a registration carrying no fingerprint has
		// nothing to bind an id to, and the next handshake mints one under
		// whatever the identity turns out to be.
		return nil
	}

	previous, met := d.identities.approvalIdentity(spelling, by)
	if !met {
		// Nothing has ever arrived under it, so there is no machine to
		// transfer and nobody's history to retire. The binding is a first
		// registration's or an earlier approval's of a printer that has not
		// dialled yet, and it stays as it is so the id GET /p/auth may already
		// have answered with is the id the printer arrives under.
		//
		// The fingerprint is deliberately not on this line. In mode
		// fingerprint and mode both it is the credential a printer is admitted
		// on (xbuddy.checkFingerprintLocked), and the id says which machine
		// this is about without writing a credential into the log.
		d.log.InfoContext(ctx, "a registration was approved, so its printer has an identity",
			"printer", previous, "approved_by", by)

		return nil
	}

	keeps, err := d.approvalKeepsTheMachine(ctx, previous, by)
	if err != nil {
		return err
	}

	if keeps {
		return d.rebind(ctx, previous, by)
	}

	if err = d.retireIdentity(ctx, previous, retirementOwnerChanged, time.Now()); err != nil {
		return err
	}

	minted, moved := d.identities.transferIdentity(spelling, previous, by)
	if !moved {
		return fmt.Errorf("%w: %s no longer answers for the machine this approval transferred",
			errTransferRaced, previous)
	}

	d.log.InfoContext(ctx, "a registration was approved, so its printer has an identity",
		"printer", minted, "retired", previous, "approved_by", by)

	return nil
}

// errTransferRaced reports an approval that retired a device and then found the
// machine bound somewhere else, which nothing but a restore does. It is
// returned rather than ignored: the caller answers the operator with a failure,
// and a second approval decides the whole thing again rather than half of it.
var errTransferRaced = errors.New("the identity moved while the transfer was being written")

// rebind keeps the id a machine already has. Nothing is bound, because the
// registration's spelling is what resolved to this device in the first place,
// and nothing is retired: the machine did not change hands, so its history, its
// grants and its address stay where they are.
func (d *devices) rebind(ctx context.Context, id string, by string) error {
	// The device exists, so the ownership is written at once. Its arrival has
	// already happened. A machine that already has an owner keeps
	// it: this is the same-person case, or a site where nobody is named.
	if err := d.claimOwnership(ctx, id, by); err != nil {
		return err
	}

	d.log.InfoContext(ctx, "a registration was approved for a printer this fleet already had, "+
		"so it keeps its identity", "printer", id, "approved_by", by)

	return nil
}

// approvalKeepsTheMachine reads who owns the device this machine is bound to and
// answers the transfer question over it.
//
// The owner is read from the authorization graph, which is where ownership is
// written: an approval claims the machine for whoever approved it (see
// claimOwnership), so the graph carries the answer across restarts and the
// registration table does not have to.
func (d *devices) approvalKeepsTheMachine(ctx context.Context, device, by string) (bool, error) {
	owners, err := d.graph.DeviceOwners(ctx, device)
	if err != nil {
		return false, fmt.Errorf("read who owns %s: %w", device, err)
	}

	held := make([]string, 0, len(owners))
	for _, owner := range owners {
		held = append(held, owner.String())
	}

	return approvalKeepsTheIdentity(by, held), nil
}

// approvalKeepsTheIdentity is who may approve a machine this fleet already has
// without taking it away from the device that holds it.
//
// The same person re-approving is a re-bind: the machine keeps its device id,
// its history and its grants, and the new registration and token resolve to it.
// It is what a site moving from admission mode none to mode token does, and
// what a printer re-registering after a firmware reset does. Nothing changed
// hands.
//
// Somebody else approving is an ownership transfer: a new id is minted and the
// device that held the machine is retired with ownership_changed, so none of
// the previous owner's fleet screen reaches the new one.
//
// Two answers are re-binds because there is nobody to compare. A device nobody
// owns is a machine this fleet met without anybody claiming it, which every
// printer admitted in mode none is, and the approver is claiming it rather than
// taking it from somebody. An empty approver is a deployment that authenticates
// nobody, or a driver that approved on its own: neither names a person, and
// retiring a working device on the word of nobody is the worse of the two
// mistakes.
//
// This is the whole of the policy. It is one function, so the answer can be
// changed in one place.
func approvalKeepsTheIdentity(by string, owners []string) bool {
	if by == "" || len(owners) == 0 {
		return true
	}

	return slices.Contains(owners, by)
}

// claimOwnership writes owner on a device for the person who approved its
// registration, and does nothing for a device that already has an owner or an
// approval that names nobody.
//
// Whoever admits a printer onto a farm is whose printer it is. It is written
// here because this is the only place that knows a person decided. Ownership
// implies every dangerous verb of the device's kind. On a printer nobody owns,
// no dangerous verb can be granted without an org admin.
func (d *devices) claimOwnership(ctx context.Context, id string, by string) error {
	if by == "" {
		return nil
	}

	subject, err := authz.ParseSubject(by)
	if err != nil {
		// The principal is whatever the identity layer resolved, and it is
		// already a graph subject by the time it reaches a handler
		// (graphPrincipals). One this vocabulary will not take is a wiring
		// fault and not this printer's problem, so it is reported and the
		// approval stands.
		d.log.ErrorContext(ctx, "an approval named a principal the authorization graph cannot key on",
			"printer", id, "approved_by", by, "error", err.Error())

		return nil
	}

	owners, err := d.graph.DeviceOwners(ctx, id)
	if err != nil {
		return fmt.Errorf("read who owns %s: %w", id, err)
	}

	if len(owners) > 0 {
		return nil
	}

	wrote, err := d.graph.ClaimDevice(ctx, id, subject)
	if err != nil {
		return fmt.Errorf("write the owner of %s: %w", id, err)
	}

	if wrote {
		d.log.InfoContext(ctx, "the printer belongs to whoever approved it",
			"printer", id, "owner", subject.String())
	}

	return nil
}

// openOwnerOf is who owns a printer that nobody approved: core.principal while
// this server serves every request as that principal, and nobody otherwise.
//
// On an open server a printer registers on its own, so no approval names a
// person. Without an owner nobody may flash the printer, update it or rename
// it, because no role gives those verbs, and the person who runs the server
// has no screen that makes them the owner.
//
// The mode is read on each call, because the first run records it while the
// process runs.
func openOwnerOf(core config.Core, mode *setupMode) func() string {
	return func() string {
		if !core.Auth.Anonymous() || mode.get() != resource.SetupOpen {
			return ""
		}

		return subjectOf(core.Principal)
	}
}

// retireIdentity marks a device id as retired from its machine, writes that to
// the record store before it returns, and ends the session if one is open. The
// plane's own retire is a different thing: it drops an entry from the tables,
// and this keeps one.
//
// The write is synchronous and its failure is returned. The ordinary flush
// stages a row and writes it on a two second tick, which is the right cost for
// telemetry and the wrong one here: the caller rebinds the fingerprint to a new
// id the moment this returns, so a process that stopped in between would come
// back with the fingerprint bound to a device whose row never said it was
// retired, and the new owner's printer would arrive on the previous owner's id.
// An approval that cannot write the retirement has not transferred anything and
// says so.
//
// The row is written whether or not this process still holds an entry for the
// device. departedLimit bounds the state trees a stopped printer keeps, so the
// device being transferred may have been dropped from both tables long ago and
// exist only as a row.
//
// The session goes last, after the row is on the disk. The machine behind it
// belongs to the new id from here on, and a socket left open would keep
// reporting a retired device's telemetry into the previous owner's rows.
// Closing it runs the ordinary departure path, so the entry moves to the
// departed table and the flush that follows writes the departure beside the
// retirement this one already wrote.
func (d *devices) retireIdentity(ctx context.Context, id string, reason string, at time.Time) error {
	d.mu.Lock()

	entry, live := d.entries[id]
	if !live {
		entry = d.departed[id]
	}

	d.mu.Unlock()

	if entry == nil {
		return d.retireStored(ctx, id, reason, at)
	}

	entry.markRetired(reason, at)

	taken := entry.forceRecord()
	if err := d.records.SaveDeviceRecords(ctx, taken.record); err != nil {
		return fmt.Errorf("write the retirement of %s: %w", id, err)
	}

	d.announce(ctx, []changedRecord{taken})

	if !live {
		return nil
	}

	if err := entry.session.Close(); err != nil {
		d.log.ErrorContext(ctx, "closing a retired printer's session",
			"printer", id, "error", err.Error())
	}

	return nil
}

// retireStored retires a device this process holds no entry for, by rewriting
// the row the store already has.
//
// The whole table is read because that is the only read the record store
// publishes, and it is a table this product bounds at the fleet it claims. It
// happens when an operator approves a transfer, which is not a rate.
//
// A device with no row is not an error. It is an id this process minted and
// nothing ever used, so there is nothing to retire and nothing a later boot
// could read back.
func (d *devices) retireStored(ctx context.Context, id string, reason string, at time.Time) error {
	stored, err := d.records.DeviceRecords(ctx)
	if err != nil {
		return fmt.Errorf("read the stored fleet to retire %s: %w", id, err)
	}

	for _, record := range stored {
		if record.DeviceID != id {
			continue
		}

		if record.RetiredReason != "" {
			return nil
		}

		record.RetiredReason, record.RetiredAt = reason, at.UTC()

		if err = d.records.SaveDeviceRecords(ctx, record); err != nil {
			return fmt.Errorf("write the retirement of %s: %w", id, err)
		}

		return nil
	}

	return nil
}

// markRetired records why this id stopped being its machine's. The first reason
// wins: retirement happens once, and a second one would restate a fact with a
// later moment than the one it happened at.
func (e *device) markRetired(reason string, at time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.retired != "" {
		return
	}

	e.retired, e.retiredAt = reason, at
	e.changed = true
}

// retirement is why this device id was retired from its machine, and when, or
// false for one that was not.
func (e *device) retirement() (string, time.Time, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.retired, e.retiredAt, e.retired != ""
}
