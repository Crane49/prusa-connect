package app

// A printer's id, and the index that keeps one machine on one id.
//
// The rule is in one place because everything downstream of it is addressed by
// the string this file produces: the authorization graph's device row, the
// event origin, the URL segment under /api/v1/devices, the history rows and
// every grant an operator wrote. An authorization row, a URL segment and an
// event subject that disagree address different printers.

import (
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// unclaimedIdentityLimit is how many minted ids this index holds for machines
// that have never arrived.
//
// It is the bound on what an unauthenticated caller can make this process
// remember. The shipping admission mode is none, which takes any fingerprint,
// and GET /p/auth mints an id for the fingerprint it is handed: without a limit
// anything that can reach the printer port could fill this map one request at a
// time. A binding this fleet has met is not counted here, because that set is
// the fleet and is bounded by the same thing the device record table is.
//
// Ten times the four hundred printers this product claims. A farm powering up
// asks GET /p/auth and then dials, so every machine is briefly unmet at once,
// and the limit has to be well clear of one whole fleet arriving. Over it the
// oldest unmet binding goes, which costs that printer one extra mint before it
// is admitted and costs a caller filling the map everything it put in.
const unclaimedIdentityLimit = 4000

// deviceIdentities binds a machine to the id this server addresses it by.
//
// A binding is minted once and then read. It is a map and not a walk of the
// fleet on purpose: every websocket handshake and every GET /p/auth asks this
// question, and a linear scan over four hundred devices per handshake would put
// the reconnection storm after a power cut in front of the CPU rather than
// behind the sockets.
//
// The key is the unit id exactly as the connector reported it. Making one
// machine one string is the dialect's job and not this index's: xBuddy's
// authenticate answers with the spelling its registration carried, whether the
// printer presented all fifty characters in a registration body or the leading
// sixteen in a Fingerprint header. An index that also matched on a prefix would
// merge two machines whose ids happen to agree at the front, which is the one
// thing it exists to prevent.
type deviceIdentities struct {
	mu sync.Mutex
	// bound is the unit id to the device id.
	bound map[string]string
	// met is the ids of the devices this fleet has met: one that has been
	// admitted, and one a stored record came back for. Such a binding is the
	// fleet's, so it is never evicted and it is what tells an approval that a
	// fingerprint already has a device rather than an id nothing has used yet.
	//
	// It is keyed by device id and not by unit id because the transfer question
	// is asked about the device, and because a rebinding leaves the old id
	// behind with nothing pointing at it.
	met map[string]bool
	// minted is the keys this process has minted, oldest first, and is the
	// queue unclaimedIdentityLimit is enforced over. A key whose device is in
	// met leaves it without being deleted, so the queue holds at most one entry
	// per mint and the walk below is amortised constant.
	minted []string
	// approver is who approved the registration behind a device id, for a
	// device that has not arrived yet, keyed by device id.
	//
	// It is here because the two halves of one fact happen at two moments: a
	// person approves a registration, and the machine behind it arrives later,
	// and the graph cannot hold an owner for a device whose directory row does
	// not exist yet. The entry is dropped the moment the ownership is written,
	// so this holds at most one string per approval nobody's printer has
	// answered.
	approver map[string]string
}

// newDeviceIdentities returns an empty index. Its zero value is not usable: the
// maps are written on the first arrival, and a nil map would panic there rather
// than at boot.
func newDeviceIdentities() *deviceIdentities {
	return &deviceIdentities{
		bound: map[string]string{}, met: map[string]bool{}, approver: map[string]string{},
	}
}

// meet says this fleet has a device under this id, so its binding is not one an
// unknown caller can evict and its fingerprint is taken.
//
// It is called when a printer is admitted and when a stored record comes back,
// which are the two ways a device exists. It is deliberately not called when an
// approval mints an id: an id nothing has used yet is what makes approving one
// registration twice mint once.
func (i *deviceIdentities) meet(id string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.met[id] = true
}

// makeRoomLocked drops minted bindings this fleet has never met, oldest first,
// until one more fits. The caller holds i.mu.
//
// The approval that minted the binding goes with it. Who approved a
// registration is only wanted to write the ownership when that machine
// arrives, and a binding this drops is one no machine can arrive under any
// more, so keeping the principal would hold one string per id an unknown
// caller made this process forget.
func (i *deviceIdentities) makeRoomLocked() {
	for len(i.minted) >= unclaimedIdentityLimit {
		key := i.minted[0]
		i.minted = i.minted[1:]

		id := i.bound[key]

		if i.met[id] {
			// The fleet's, so it keeps its binding and only leaves the queue.
			continue
		}

		delete(i.bound, key)
		delete(i.approver, id)
	}
}

// mintDeviceID is a printer's identity: a random version 4 UUID in the
// canonical lowercase 36 character form.
//
// A registration is the identity and the machine is an attribute of it.
//
// A printer that changes owner is re-registered, and the approval mints a new
// id and retires the old one. The old id keeps its history rows, its command
// records, its grants and its audit, and none of it reaches the new owner. An
// id derived from the machine would hand the new owner all of that.
//
// The id is also what GET /p/auth answers under uuid, and a service built
// around Prusa Connect expects a UUID there: Connect itself keys a printer on a
// version 4 UUID with the fingerprint beside it as an attribute, so this is the
// shape a client already parses rather than a shape only this server produces.
//
// And a random id is not linkable. Anybody who knows a printer's fingerprint
// could recompute a derived id and recognise the same machine on a second
// server; there is nothing to recompute here.
//
// The form is legal everywhere the id is used. It is 36 bytes of lowercase
// hexadecimal and hyphens, so it is a DNS-1123 label, which is what makes one
// string an authorization key, a URL segment and an event subject at once
// (event.NewOrigin bounds a part at 63 bytes and refuses ':', ',', '=', '/',
// ';' and control characters).
func mintDeviceID() string {
	return uuid.NewString()
}

// identify is the id this server addresses a machine by, minting one the first
// time it meets a fingerprint and answering with the bound id every time after.
//
// It mints here rather than only at registration because admission mode none
// has no registration to mint at: first sight is admission there, and a printer
// that dialled in is a printer this fleet holds. Mode token reaches this with a
// binding already written by the approval, so the mint below is the mode none
// path and the path a build with no record store takes after a restart.
//
// The binding survives a restart through the device record, which already
// stores unit_uid beside the device id; restore reads the fleet back before any
// listener opens and puts every row into this index. A build that keeps no
// record store therefore mints one id per run for the same machine, which is
// the documented behaviour of such a build and not a defect: nothing in it
// remembers a printer between processes, so there is nothing for an id to be
// stable against.
//
// A printer whose connector gives it no unit id at all falls back to what it
// said about itself, which is the only thing left to tell two such machines
// apart. Two such printers are one device, because a connector that reports no
// serial and no unit id has given this server nothing to tell them apart.
// A machine whose handshake carried a second spelling of its unit id
// (connectorapi.Identity.UnitUIDAlias) is one machine here too, and this is the
// only place the two spellings are put together. The driver reports the alias
// for an exchange it authenticated and for no other, so what merges two
// spellings is a machine holding the credential one of them was issued against,
// and never a string a caller chose. An approval that merged on a shared
// prefix would let an operator hand a stranger a working printer's device id.
func (i *deviceIdentities) identify(identity connectorapi.Identity) string {
	key := identityKey(identity)
	alias := identityKeyOf(identity.UnitUIDAlias)

	i.mu.Lock()
	defer i.mu.Unlock()

	if id, resolved := i.resolveDialledLocked(key, alias); resolved {
		return id
	}

	i.makeRoomLocked()

	id := mintDeviceID()
	i.bindLocked(id, key)
	i.minted = append(i.minted, key)

	return id
}

// resolveDialledLocked is the id a machine that presented these two spellings
// of one unit id resolves to, and false for one this index has never bound
// either of them for. The caller holds i.mu.
//
// A binding the fleet has met wins, whichever spelling carries it. A printer
// met in mode none is bound under the sixteen characters its header carries,
// and its registration is approved under all fifty. The id the approval minted
// has nothing behind it, and the machine's own device does. Preferring the met
// one keeps that printer on the id that holds its history, its grants and its
// address, and binds the registered spelling to it.
//
// A met binding is never overwritten, so no device loses its last spelling
// here: the alias only rebinds a spelling that is unbound or bound to an id
// nothing has arrived under.
func (i *deviceIdentities) resolveDialledLocked(key, alias string) (string, bool) {
	own, bound := i.bound[key]
	if bound && i.met[own] {
		return own, true
	}

	if alias == "" || alias == key {
		return own, bound
	}

	other, aliased := i.bound[alias]
	if !aliased || (bound && !i.met[other]) {
		return own, bound
	}

	i.bindLocked(other, key)

	if !bound {
		// A key this index did not hold, so it is one more binding the limit
		// bounds. A key it did hold is already in the queue or is a met
		// device's, and queueing it twice would let one eviction drop a
		// binding a later mint wrote.
		i.minted = append(i.minted, key)
	}

	// The approval behind the id being left behind decided about this machine,
	// and the machine is this device. Moving the principal is what makes the
	// ownership land on the id the printer arrives under; an id nothing
	// arrives under would keep it until the limit dropped it.
	if by := i.approver[own]; by != "" && i.approver[other] == "" {
		i.approver[other] = by
	}

	delete(i.approver, own)

	return other, true
}

// bind writes a binding somebody else minted: the approval of a registration,
// and the record store handing back the fleet at boot.
//
// A key already bound is rebound. That is the ownership transfer: the approval
// has minted a new id for a fingerprint this server already knew, and from here
// the machine resolves to the new id while the old device keeps everything
// filed under the old one.
func (i *deviceIdentities) bind(id string, unitUID string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.bindLocked(id, identityKeyOf(unitUID))
}

// bindLocked writes one key. The caller holds i.mu.
func (i *deviceIdentities) bindLocked(id string, key string) {
	i.bound[key] = id
}

// approvalIdentity is the id an approved registration's fingerprint resolves
// to, minting and binding one under that spelling when this index holds none,
// and met says whether a machine has ever arrived under the id it answers with.
//
// Resolving, minting and binding happen under one lock because a printer
// dialling and an operator approving are two requests. Without the lock a
// handshake could land between the read and the bind. The two paths would then
// mint two ids for one machine and retire neither.
//
// A binding with nothing behind it stays as it is, so approving one code twice
// mints once and the id GET /p/auth may already have answered with is the id
// the printer arrives under. Only one spelling is bound, the one the
// registration carried: what makes two spellings one machine is the machine
// itself, on a handshake the driver authenticated (see resolveDialledLocked).
//
// by is recorded against an id no machine has arrived under, to be written as
// the ownership when it does. A met device's ownership is the caller's to write
// at once, because its directory row already exists.
func (i *deviceIdentities) approvalIdentity(spelling string, by string) (string, bool) {
	key := identityKeyOf(spelling)

	i.mu.Lock()
	defer i.mu.Unlock()

	if id, bound := i.bound[key]; bound {
		if i.met[id] {
			return id, true
		}

		i.approvedByLocked(id, by)

		return id, false
	}

	i.makeRoomLocked()

	id := mintDeviceID()
	i.bindLocked(id, key)
	i.minted = append(i.minted, key)
	i.approvedByLocked(id, by)

	return id, false
}

// transferIdentity mints the id a machine moves onto when an approval takes it
// away from the device that held it, and answers false when this spelling
// does not resolve to that device.
//
// The compare is the point. The retirement in between is a disk write, so a
// printer can arrive while it happens, and rebinding a spelling something else
// has since bound would put the machine on an id nothing admitted.
func (i *deviceIdentities) transferIdentity(spelling string, from string, by string) (string, bool) {
	key := identityKeyOf(spelling)

	i.mu.Lock()
	defer i.mu.Unlock()

	if i.bound[key] != from {
		return "", false
	}

	// The key is already in this map, so a transfer cannot grow it and there is
	// nothing for the limit to bound. It is deliberately not queued: a key
	// queued twice lets one eviction drop a binding a later mint wrote.
	minted := mintDeviceID()
	i.bindLocked(minted, key)
	i.approvedByLocked(minted, by)

	return minted, true
}

// approvedByLocked records the approver of one id. The caller holds i.mu.
func (i *deviceIdentities) approvedByLocked(id string, by string) {
	if by == "" {
		return
	}

	i.approver[id] = by
}

// takeApprover is who approved this device's registration, and empty for one no
// approval reached this process for. It is taken rather than read: the fact is
// wanted once, to write the ownership, and holding it afterwards would keep a
// principal for every device this process ever admitted.
func (i *deviceIdentities) takeApprover(id string) string {
	i.mu.Lock()
	defer i.mu.Unlock()

	by := i.approver[id]
	delete(i.approver, id)

	return by
}

// identityKey is what the index keys one machine on: its unit id, and what it
// said about itself when the connector gave it none.
func identityKey(identity connectorapi.Identity) string {
	if key := identityKeyOf(identity.UnitUID); key != "" {
		return key
	}

	return identity.Vendor + "\x00" + identity.Model + "\x00" + identity.Serial
}

// identityKeyOf is one unit id as the index holds it, with the surrounding
// space a header can carry taken off.
func identityKeyOf(unitUID string) string { return strings.TrimSpace(unitUID) }

// boundDevice is the device id one spelling resolves to, and the empty string
// for a spelling nothing has bound.
//
// It reads and writes nothing else, so an approval can answer with the id it
// bound without the approval path having to return one: the notice a driver
// calls carries no id, and one signature cannot serve both callers.
func (i *deviceIdentities) boundDevice(spelling string) string {
	key := identityKeyOf(spelling)
	if key == "" {
		return ""
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	return i.bound[key]
}

// release drops every binding that resolves to one device id, so the machine
// behind it is given a new id at its next handshake rather than arriving on a
// record an operator has retired.
//
// It walks the map rather than taking the spelling, because a device may be
// bound under more than one spelling and the caller of a retirement holds the
// id and not the fingerprint. The map is bounded by the fleet, and a retirement
// is an operator acting rather than a rate.
func (i *deviceIdentities) release(id string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	for key, bound := range i.bound {
		if bound == id {
			delete(i.bound, key)
		}
	}

	delete(i.met, id)
	delete(i.approver, id)
}
