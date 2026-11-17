package xbuddy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// Mode is the credential a printer has to present on the websocket. It is the
// four admission modes of earlier research, which are what a dialect
// checks before the host is asked whether it wants the printer at all.
type Mode string

// The admission modes.
const (
	// ModeNone accepts any printer that dials with a Fingerprint header, and
	// checks nothing about it. The header is still required: it is the only
	// identity a connection has, and a caller that sends none is not a printer
	// speaking this wire.
	ModeNone Mode = "none"
	// ModeToken requires the Token header to be one registration issued.
	ModeToken Mode = "token"
	// ModeFingerprint requires the Fingerprint header to be one registration
	// approved.
	ModeFingerprint Mode = "fingerprint"
	// ModeBoth requires both.
	ModeBoth Mode = "both"
)

// The errors registration reports. Callers inspect them with errors.Is.
var (
	// ErrUnknownMode reports an admission mode configuration does not name.
	ErrUnknownMode = errors.New("xbuddy: unknown admission mode")
	// ErrRegistrationPending reports a code whose printer nobody has approved
	// yet, which is what the printer keeps polling on.
	ErrRegistrationPending = errors.New("xbuddy: the registration is waiting for an operator")
	// ErrUnknownCode reports a code no registration answers to. It is the
	// vocabulary's own value, named here so the four sentinels read as one
	// list, and so a host that never imports this package still recognises the
	// refusal (internal/kernel/admission).
	ErrUnknownCode = admission.ErrUnknownCode
	// ErrRefused reports a printer whose credentials this driver does not
	// accept.
	ErrRefused = errors.New("xbuddy: the printer's credentials were refused")
	// ErrRegistrationsFull reports a table holding nothing this driver may
	// drop, so a printer it has never seen cannot be given a code. Every entry
	// in it is one an operator approved.
	ErrRegistrationsFull = errors.New("xbuddy: the registration table is full")
)

// defaultMaxRegistrations is how many printers this driver keeps a
// registration for.
//
// POST /p/register carries no credential, because a printer has none before it
// registers. Anything that can open a socket to this port can therefore put an
// entry in this table, and a bench run publishes the port on the cable. Four
// hundred is the fleet this product claims, and it is the denominator
// departedLimit in internal/app/devices.go already takes for the same shape of
// table. A whole fleet can register at once and still be found afterwards.
//
// It bounds the memory with maxRegistrationBytes, which is what one request may
// carry. An entry retains at most that, so four hundred of them retain about
// 6.6 MiB whatever a caller sends.
const defaultMaxRegistrations = 400

const (
	// codeLength and tokenLength are what the shipping registrar issues
	// (legacy/connector/xbuddy/auth.go:39).
	codeLength  = 10
	tokenLength = 20

	// codeAlphabet is the intersection of the two client-side checks an earlier
	// frontend made on an operator-typed code: it accepts [A-HJ-NP-Z2-79] and forbids
	// [018IOLB], so 0, 1, 8, I, O, L and B are out.
	codeAlphabet = "ACDEFGHJKMNPQRSTUVWXYZ2345679"

	// byteSpace is the number of values one random byte takes, which rejection
	// sampling divides to avoid modulo bias.
	byteSpace = 256
)

// Registration is one printer working through the handshake.
type Registration struct {
	// Code is what the printer polls with and an operator reads out.
	Code string
	// Fingerprint identifies the printer across reconnections.
	Fingerprint string
	// Token is what the printer presents on the websocket once approved.
	Token string
	// Serial, PrinterType and Firmware are what the printer said about itself.
	Serial      string
	PrinterType string
	Firmware    string
	// Approved is whether an operator, or auto-approval, has let it through.
	Approved bool
}

// entry is one registration and what the table needs to choose which of them to
// drop when it is full.
type entry struct {
	Registration

	// tokenHash is the hex-encoded SHA-256 of Token, and is what survives a
	// restart. Token itself is held only by the process that minted it: a
	// credential is what the printer keeps, and a table that could hand one out
	// again would be a second way to obtain it.
	tokenHash string
	// seq orders entries by when the printer first registered. It is a counter
	// and not a clock, so two registrations in the same millisecond are still
	// ordered.
	seq uint64
	// completed is whether a printer collected its token. That is the last step
	// of the handshake, and the only evidence this table holds that the caller
	// behind an entry is a printer rather than anything that opened a socket.
	completed bool
	// endorsed is whether an operator approved this one. Auto-approval
	// does not set it: nobody decided anything there.
	endorsed bool
	// announced is whether the host has been told about an approval this table
	// made on its own. It is not stored: what it guards is the identity the
	// host binds at an approval, and the host forgets an id no printer has
	// arrived under anyway (see internal/app/deviceregistration.go), so a
	// process that comes back re-announces at the printer's next registration.
	announced bool
	// admittedBy and admittedAt are who let the printer through and when, empty
	// and zero for one nobody has decided and for one auto-approval let in.
	admittedBy string
	admittedAt time.Time
	// issuedAt is when the code was minted. expiresAt is when a registration
	// nobody has approved stops being usable, and is zero once somebody has:
	// an approved registration is the credential the printer dials with, and
	// expiring it would turn a working fleet off on a timer.
	issuedAt  time.Time
	expiresAt time.Time
	// lastWrite closes when the newest store write about this registration has
	// landed, and is nil while none has been taken. It is what orders the
	// writes: see storeWrite.
	lastWrite chan struct{}
}

// recordOf is this entry as the store keeps it between processes.
func (e *entry) recordOf(connector, kind string) admission.Record {
	return admission.Record{
		Connector:   connector,
		Code:        e.Code,
		Fingerprint: e.Fingerprint,
		TokenHash:   e.tokenHash,
		Serial:      e.Serial,
		Model:       e.PrinterType,
		Firmware:    e.Firmware,
		Kind:        kind,
		Approved:    e.Approved,
		Endorsed:    e.endorsed,
		Completed:   e.completed,
		AdmittedBy:  e.admittedBy,
		AdmittedAt:  e.admittedAt,
		IssuedAt:    e.issuedAt,
		ExpiresAt:   e.expiresAt,
		Seq:         e.seq,
	}
}

// entryOf is one stored record as this table holds it. The token is not in it,
// only its digest: see entry.tokenHash.
func entryOf(record admission.Record) *entry {
	return &entry{
		Registration: Registration{
			Code:        record.Code,
			Fingerprint: record.Fingerprint,
			Serial:      record.Serial,
			PrinterType: record.Model,
			Firmware:    record.Firmware,
			Approved:    record.Approved,
		},
		tokenHash:  record.TokenHash,
		seq:        record.Seq,
		completed:  record.Completed,
		endorsed:   record.Endorsed,
		admittedBy: record.AdmittedBy,
		admittedAt: record.AdmittedAt,
		issuedAt:   record.IssuedAt,
		expiresAt:  record.ExpiresAt,
	}
}

// registrar is the driver's half of admission: the registration table, the
// credentials it issued, and the allowlists an approval writes into.
//
// The host decides whether it wants a printer, through Arrivals.Admit. This
// decides whether the printer is who it says it is, which is the half a dialect
// cannot delegate because only the dialect knows what its handshake carries.
type registrar struct {
	mode  Mode
	limit int
	// ttl is how long a registration nobody has approved stays usable.
	ttl time.Duration
	// now is the clock, so a test can watch a code expire without waiting a
	// day for it.
	now func() time.Time

	mu            sync.Mutex
	seq           uint64
	byCode        map[string]*entry
	byFingerprint map[string]*entry
	// byPrefix resolves the sixteen character spelling a printer puts in its
	// Fingerprint header to the entry whose full fingerprint starts with it.
	//
	// It is a second index and never a second key: nothing is stored under a
	// prefix and no credential is compared against one, so an attacker holding
	// sixteen characters of a printer's fingerprint reaches an entry only after
	// resolveLocked has checked the entry's own full spelling against them, and
	// then still has to present that entry's token.
	//
	// The oldest entry wins a prefix. Two full fingerprints agreeing on sixteen
	// base32 characters is eighty bits of a SHA-256 colliding, so in practice
	// this is a registration deliberately made to shadow another, and the
	// machine that registered first keeps its header working.
	byPrefix map[string]*entry
	// tokens is keyed by the digest of the token and not by the token, so what
	// this table holds after a restart is what it holds before one.
	tokens       map[string]string
	fingerprints map[string]bool
	// unforgotten is the entries forgetDevice took out of this table whose
	// removal from the store failed, by fingerprint. The next call for the same
	// printer makes the write again. The map holds one entry per failed write,
	// and a host only calls forgetDevice when an operator removes a printer.
	unforgotten map[string]*entry

	// store is where the table survives a restart, and is nil for a driver the
	// host handed none: such a driver behaves exactly as this one did before
	// the store existed, which is what keeps it testable with no database.
	// connector and kind are what a row is written under.
	store     admission.Store
	connector string
	kind      string

	// notify is what the host is told about an approval this table made on its
	// own, and is nil for a driver nobody handed one. An operator's approval
	// needs none: the host is the caller there.
	//
	// It is read under the lock and called without it. The host binds a device
	// id and may close a session to do it, and holding this table while it does
	// would put the driver's own wire behind the host's decision.
	notify connectorapi.ApprovalNotice
}

// defaultRegistrationTTL is how long a code nobody has typed stays usable.
//
// A registration is a claim on this table and on the code space, and one left
// there for ever is a code somebody may still guess. A day is longer than the
// walk from the machine to the browser and shorter than a holiday, and a
// printer whose code expired registers again at its next poll: the firmware
// keeps registering until it is let in, so the cost of expiring one is one
// handshake and a new code on the screen.
const defaultRegistrationTTL = 24 * time.Hour

func newRegistrar(mode Mode, limit int) *registrar {
	if limit < 1 {
		limit = defaultMaxRegistrations
	}

	return &registrar{
		mode:          mode,
		limit:         limit,
		ttl:           defaultRegistrationTTL,
		now:           func() time.Time { return time.Now().UTC() },
		byCode:        make(map[string]*entry),
		byFingerprint: make(map[string]*entry),
		byPrefix:      make(map[string]*entry),
		tokens:        make(map[string]string),
		fingerprints:  make(map[string]bool),
		unforgotten:   make(map[string]*entry),
	}
}

// restore reads the table this connector left behind and makes it this
// process's table.
//
// Every credential comes back with it, which is the whole point: a printer an
// operator approved before the restart presents the token it was issued and is
// admitted, with nobody deciding anything a second time. What does not come
// back is the plaintext of a token this process did not mint; see
// entry.tokenHash and complete.
//
// A registration nobody approved and whose code has run out is dropped here
// rather than loaded and ignored, so the table this process serves is the table
// it would have had if it had never stopped.
//
// It is the one path that reaches the store with the table locked, because the
// host calls it before the handler is mounted and no printer can be waiting for
// the lock. Every other write is taken under the lock and made without it: see
// storeWrite.
func (r *registrar) restore(ctx context.Context, connector, kind string, store admission.Store) error {
	if store == nil {
		return nil
	}

	records, err := store.Load(ctx, connector)
	if err != nil {
		return fmt.Errorf("read the registrations of %s: %w", connector, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.store, r.connector, r.kind = store, connector, kind

	now := r.now()

	for _, record := range records {
		if record.Expired(now) {
			if err = store.Forget(ctx, connector, record.Code); err != nil {
				return err
			}

			continue
		}

		item := entryOf(record)

		r.byCode[item.Code] = item
		r.fileLocked(item)

		if item.seq > r.seq {
			r.seq = item.seq
		}

		if item.Approved {
			r.allowLocked(item)
		}
	}

	return nil
}

// begin issues a code for a printer that has just introduced itself. dropped is
// the fingerprint of a registration forgotten to make room, and is empty when
// nothing had to be.
//
// A printer that retries gets the code it already has rather than a second one,
// because a printer piling up codes is an operator reading the wrong one out.
// The approval it makes on its own is reported to the host after the table is
// unlocked, so the host binding an identity for it cannot deadlock against the
// driver's own wire. The report is retried at the printer's next registration
// until it is made once, because the printer keeps registering until it is let
// in and a machine the host never heard approved is a machine that gets its id
// at its first handshake instead.
func (r *registrar) begin(ctx context.Context, meta Registration) (code, dropped string, err error) {
	made, writes, err := r.issue(meta)

	// Made whatever the table answered. Room taken for a registration that was
	// never issued has already been taken in memory, and every write holds a
	// turn the writes behind it wait for.
	stored := runWrites(ctx, writes)

	switch {
	case err != nil:
		return "", "", err
	case stored != nil:
		return "", "", stored
	}

	if err = r.announce(ctx, made.approved); err != nil {
		return "", "", err
	}

	return made.code, made.dropped, nil
}

// announce tells the host about an approval this table made on its own, once.
// It takes no lock and is called with none held.
func (r *registrar) announce(ctx context.Context, item *entry) error {
	if item == nil {
		return nil
	}

	r.mu.Lock()
	notify, view := r.notify, r.operatorViewLocked(item)
	r.mu.Unlock()

	if notify == nil {
		return nil
	}

	// Nobody decided this one, so there is no principal to name: an approval a
	// driver makes for itself is not an operator's act.
	if err := notify(ctx, view, ""); err != nil {
		return fmt.Errorf("report the registration this driver approved: %w", err)
	}

	r.mu.Lock()
	item.announced = true
	r.mu.Unlock()

	return nil
}

// issued is what one registration produced in the table: the code the printer
// polls on, the registration dropped to make room for it, and the entry this
// table approved on its own and has not told the host about.
type issued struct {
	code     string
	dropped  string
	approved *entry
}

// issue is begin's table half. It reserves the code in memory and hands back
// the store writes that reservation owes, which the caller makes with the lock
// released.
//
// The reservation is what stops two printers claiming one code while a write is
// in flight: the code is in byCode before the lock goes, so the next
// registration cannot draw it and a printer that retries finds the entry it
// already has.
func (r *registrar) issue(meta Registration) (issued, []storeWrite, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// The exact spelling and never a prefix. A registration body carries the
	// whole fingerprint, so a body that differs anywhere is a different machine
	// and gets a registration of its own. Matching a prefix here would hand a
	// caller who knows sixteen characters the code and the token of the printer
	// that owns them.
	if existing, ok := r.byFingerprint[meta.Fingerprint]; ok && !r.expiredLocked(existing) {
		existing.Serial = meta.Serial
		existing.PrinterType = meta.PrinterType
		existing.Firmware = meta.Firmware

		return issued{code: existing.Code, approved: r.unannouncedLocked(existing)},
			[]storeWrite{r.saveLocked(existing)}, nil
	}

	dropped, writes, err := r.makeRoomLocked()
	if err != nil {
		return issued{}, writes, err
	}

	code, err := randomCode()
	if err != nil {
		return issued{}, writes, err
	}

	token, err := randomToken()
	if err != nil {
		return issued{}, writes, err
	}

	r.seq++

	minted := r.now()

	item := &entry{
		Registration: Registration{
			Code:        code,
			Fingerprint: meta.Fingerprint,
			Token:       token,
			Serial:      meta.Serial,
			PrinterType: meta.PrinterType,
			Firmware:    meta.Firmware,
		},
		tokenHash: digestOf(token),
		seq:       r.seq,
		issuedAt:  minted,
		expiresAt: minted.Add(r.ttl),
	}

	if r.approvesOnArrival() {
		r.approveLocked(item)
	}

	r.byCode[code] = item
	r.fileLocked(item)

	return issued{code: code, dropped: dropped, approved: r.unannouncedLocked(item)},
		append(writes, r.saveLocked(item)), nil
}

// approvesOnArrival reports whether this table lets a machine through the
// moment it registers.
//
// It is mode none and nothing else, which is why there is no setting for it any
// more. That mode admits every printer that dials, so holding its registrations
// for an operator would stall a handshake whose answer is already yes. In every
// other mode the registration is what an operator decides on, and a table that
// approved one for them would hand the credential that mode checks to anything
// that can reach POST /p/register. In those modes a printer is never admitted
// without a person deciding.
func (r *registrar) approvesOnArrival() bool {
	return r.mode == ModeNone
}

// unannouncedLocked is the entry the host has to be told this table approved by
// itself, and nil when there is nothing to tell. The caller holds the lock.
func (r *registrar) unannouncedLocked(item *entry) *entry {
	if !item.Approved || item.endorsed || item.announced {
		return nil
	}

	return item
}

// expiredLocked reports whether an entry nobody approved has run out of time.
// The caller holds the lock.
func (r *registrar) expiredLocked(item *entry) bool {
	return !item.Approved && !item.expiresAt.IsZero() && r.now().After(item.expiresAt)
}

// makeRoomLocked forgets registrations until one more fits, and reports the
// fingerprint of the last one it forgot. The caller holds the lock.
//
// Which one goes is the whole of this. An entry an operator approved is
// never dropped, because a person decided it, and a table holding nothing else
// refuses the new registration instead. Among the rest, an entry nobody
// completed goes first: it is a claim no printer confirmed, so a caller minting
// fingerprints and never polling evicts only its own.
//
// It drops rather than refuses because a printer that is refused stays refused.
// A printer whose entry was dropped registers again and is given a new code,
// which is the same handshake it makes at every boot.
func (r *registrar) makeRoomLocked() (string, []storeWrite, error) {
	var (
		dropped string
		writes  []storeWrite
	)

	for len(r.byFingerprint) >= r.limit {
		// A code that has run out is nobody's registration any more, so it goes
		// before anything a caller might still be waiting on.
		victim := r.oldestLocked(r.expiredLocked)
		if victim == nil {
			victim = r.oldestLocked(func(item *entry) bool { return !item.completed && !item.endorsed })
		}

		if victim == nil {
			victim = r.oldestLocked(func(item *entry) bool { return !item.endorsed })
		}

		if victim == nil {
			return "", writes, fmt.Errorf("%w: %d registrations, every one approved by an operator",
				ErrRegistrationsFull, len(r.byFingerprint))
		}

		r.forgetLocked(victim)

		writes = append(writes, r.forgetStoredLocked(victim))

		dropped = victim.Fingerprint
	}

	return dropped, writes, nil
}

// oldestLocked is the entry with the lowest sequence among those eligible says
// may go, and nil when none is. The caller holds the lock.
func (r *registrar) oldestLocked(eligible func(*entry) bool) *entry {
	var oldest *entry

	for _, item := range r.byFingerprint {
		if !eligible(item) {
			continue
		}

		if oldest == nil || item.seq < oldest.seq {
			oldest = item
		}
	}

	return oldest
}

// forgetLocked takes one registration out of every table it appears in, so no
// credential outlives the entry that owned it. The caller holds the lock.
func (r *registrar) forgetLocked(item *entry) {
	delete(r.byCode, item.Code)
	delete(r.byFingerprint, item.Fingerprint)
	delete(r.tokens, item.tokenHash)
	delete(r.fingerprints, item.Fingerprint)

	r.reindexPrefixesLocked()
}

// complete answers one poll. ErrRegistrationPending is the printer's cue to
// keep polling, which is the whole of the wait-for-an-operator behaviour.
func (r *registrar) complete(ctx context.Context, code string) (Registration, error) {
	admitted, write, err := r.completeInTable(code)
	if err != nil {
		return Registration{}, err
	}

	if err = write.run(ctx); err != nil {
		return Registration{}, err
	}

	return admitted, nil
}

// completeInTable is complete's table half. It hands back the registration the
// printer walks away with and the write that stores it, which the caller makes
// with the lock released.
func (r *registrar) completeInTable(code string) (Registration, storeWrite, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	item, ok := r.byCode[code]
	if !ok || r.expiredLocked(item) {
		return Registration{}, storeWrite{}, fmt.Errorf("%w: %s", ErrUnknownCode, code)
	}

	if !item.Approved {
		return Registration{}, storeWrite{}, ErrRegistrationPending
	}

	// A registration this process did not mint has a digest and no token: the
	// plaintext is the printer's and was never written down. Rather than refuse
	// a printer that comes back for its token after a restart, a fresh one is
	// minted here. It is the same handshake step the printer already makes, and
	// the token it walks away with is the one the table then accepts.
	if item.Token == "" {
		token, err := randomToken()
		if err != nil {
			return Registration{}, storeWrite{}, err
		}

		delete(r.tokens, item.tokenHash)

		item.Token, item.tokenHash = token, digestOf(token)

		r.allowLocked(item)
	}

	item.completed = true

	return item.Registration, r.saveLocked(item), nil
}

// Approve lets one registration through, which is the operator's half of the
// handshake. It is exported because the host's own surface is what an operator
// reaches, and the driver is the only thing holding the table.
func (r *registrar) Approve(ctx context.Context, code, by string) (admission.Registration, error) {
	view, write, err := r.approveInTable(code, by)
	if err != nil {
		return admission.Registration{}, err
	}

	if err = write.run(ctx); err != nil {
		return admission.Registration{}, err
	}

	return view, nil
}

// approveInTable is Approve's table half. It hands back what an operator is
// shown and the write that stores the decision, which the caller makes with the
// lock released.
func (r *registrar) approveInTable(code, by string) (admission.Registration, storeWrite, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	item, ok := r.byCode[code]
	if !ok || r.expiredLocked(item) {
		return admission.Registration{}, storeWrite{}, fmt.Errorf("%w: %s", ErrUnknownCode, code)
	}

	item.endorsed = true
	item.admittedBy = by
	item.admittedAt = r.now()
	// The clock stops mattering here. What expires is a code nobody has typed;
	// this one has been typed, and the printer behind it is now the fleet's.
	item.expiresAt = time.Time{}

	r.approveLocked(item)

	return r.operatorViewLocked(item), r.saveLocked(item), nil
}

// Reject forgets one registration, which is the other half of the operator's
// decision. Every credential it held goes with it, so a device already approved
// is turned away the next time it presents one.
//
// The device is not refused for good. It registers again at its next boot and
// is given a new code, which is the same handshake a dropped entry produces.
func (r *registrar) Reject(ctx context.Context, code string) error {
	write, err := r.rejectInTable(code)
	if err != nil {
		return err
	}

	return write.run(ctx)
}

// rejectInTable is Reject's table half. The removal from the store waits for
// whatever write was already in flight about this registration, so a save the
// operator's decision overtook cannot write the row back.
func (r *registrar) rejectInTable(code string) (storeWrite, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	item, ok := r.byCode[code]
	if !ok {
		return storeWrite{}, fmt.Errorf("%w: %s", ErrUnknownCode, code)
	}

	r.forgetLocked(item)

	return r.forgetStoredLocked(item), nil
}

// forgetDevice withdraws the registration behind one printer, with its token
// and its approved fingerprint, so the printer is refused on /p/ws in every
// mode that checks a credential. unitUID is the spelling authenticate answered
// with, which is what the host keys the device on.
//
// Mode none checks no credential, so there is nothing to take back and this
// does nothing. Forgetting there would only lose what the printer said when it
// registered: the firmware keeps its token and does not register again, so it
// would come back without its serial. A site that later moves to a mode that
// checks a credential runs this again at start, and the registration goes then.
//
// The match is exact and never through the prefix index. A record met in mode
// none carries the sixteen characters of a header, and the entry that owns
// that prefix may be another machine's registration.
//
// A registration issued at or after issuedBefore is kept. The host passes the
// moment the record was retired, and a printer that registered since made a
// new request a person may already have approved.
//
// A printer this table holds nothing for is not an error, because the host
// calls this again for a retirement it already made. A store write that
// failed is remembered and made again by that next call, so a second call
// that answers nil has removed the row too.
func (r *registrar) forgetDevice(ctx context.Context, unitUID string, issuedBefore time.Time) error {
	if r.mode == ModeNone {
		return nil
	}

	key := strings.TrimSpace(unitUID)

	item, write, found := r.forgetDeviceInTable(key, issuedBefore)
	if !found {
		return nil
	}

	if err := write.run(ctx); err != nil {
		r.mu.Lock()
		r.unforgotten[key] = item
		r.mu.Unlock()

		return err
	}

	r.mu.Lock()
	if r.unforgotten[key] == item {
		delete(r.unforgotten, key)
	}
	r.mu.Unlock()

	return nil
}

// forgetDeviceInTable is forgetDevice's table half. It hands back the entry it
// took out and the write that removes it from the store, and found is false
// when there is nothing to forget.
func (r *registrar) forgetDeviceInTable(key string, issuedBefore time.Time) (*entry, storeWrite, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	item, ok := r.byFingerprint[key]
	if !ok {
		// Gone from the table already, and its row may still be on the disk.
		owed, failed := r.unforgotten[key]
		if !failed {
			return nil, storeWrite{}, false
		}

		return owed, r.forgetStoredLocked(owed), true
	}

	// A row with no issue time comes from before the time was stored, so it is
	// older than any retirement this host can name.
	if !item.issuedAt.IsZero() && !item.issuedAt.Before(issuedBefore) {
		return nil, storeWrite{}, false
	}

	r.forgetLocked(item)

	return item, r.forgetStoredLocked(item), true
}

// pending returns the registrations waiting for a decision, in code order, so
// an operator can be shown what is asking.
//
// The order is fixed here rather than left to the map walk. A person reading a
// list twice must not be shown two orders, and a client paging one must not be
// given a row twice.
func (r *registrar) pending() []admission.Registration {
	r.mu.Lock()
	defer r.mu.Unlock()

	waiting := make([]admission.Registration, 0, len(r.byCode))

	for _, item := range r.byCode {
		// An expired code is nobody's registration any more. It is left in the
		// table until something needs the room, and it is not offered to an
		// operator to approve: approving it would admit a printer that has
		// already been told to register again.
		if !item.Approved && !r.expiredLocked(item) {
			waiting = append(waiting, r.operatorViewLocked(item))
		}
	}

	slices.SortFunc(waiting, func(a, b admission.Registration) int {
		return strings.Compare(a.Code, b.Code)
	})

	return waiting
}

// operatorViewLocked is one registration as an operator surface may see it. The
// token is dropped: it is the credential the machine presents on the websocket,
// and a listing an operator reads must not hand one out. The caller holds the
// lock.
//
// One registration names one spelling. The header spelling deliberately does
// not ride along: a registration that shares the sixteen characters of a header
// with a printer this fleet is already working with has proved nothing by
// sharing them, and a host that bound identities from a registration's aliases
// let one operator approval hand a stranger that printer's device id. The
// second spelling is reported on the admission instead, where a credential was
// checked (see provenAliasOf).
func (r *registrar) operatorViewLocked(item *entry) admission.Registration {
	return admission.Registration{
		Code:        item.Code,
		Fingerprint: item.Fingerprint,
		Serial:      item.Serial,
		Model:       item.PrinterType,
		Firmware:    item.Firmware,
		Approved:    item.Approved,
	}
}

// secondSpellingOf is the spelling a printer put on the wire when this table
// knows it by another one, and empty when the two agree or the request carried
// none. presented is the header value as it arrived and known is what
// authenticate answered with.
//
// It is reported on a handshake and never on a registration, and that is the
// whole of what makes it safe to merge two device identities on. The leading
// sixteen characters of a fingerprint travel in the clear in every request a
// printer makes, so a caller that knows them has proved nothing by knowing
// them. A caller that reaches here has been through authenticate: in mode
// token, fingerprint and both it holds the credential the registration behind
// the known spelling was issued against, which is the machine. In mode none it
// holds nothing, and nothing is protected there either, because that mode
// admits whatever dials and the header spelling on its own already resolves to
// the device the machine was admitted as.
func secondSpellingOf(presented, known string) string {
	presented = strings.TrimSpace(presented)
	if presented == known {
		return ""
	}

	return presented
}

// authenticate checks what the websocket handshake carried, in the mode this
// driver was configured with. It returns the fingerprint the printer is known
// by, which is not always the one it sent: a printer whose registration this
// table holds is known by the full spelling that registration carried rather
// than by the 16 character prefix the header gave (see fingerprintHeaderLength).
//
// Answering with the registered spelling is what makes one machine one
// identity. This string becomes the session key, the UnitUID on the identity
// and the fingerprint the host binds a device id to, so a route that answered
// with the prefix and a route that answered with all 50 characters would be two
// printers as far as every table downstream is concerned.
//
// A handshake with no fingerprint is refused in every mode, mode none included.
// Firmware puts the header on every request it makes,
// so a caller that sends none is not a printer on this wire. An identity made
// up from its remote address would be a new device for every socket it opens,
// and a bare websocket handshake from a browser or a port scanner would be
// listed as a printer.
//
// It is the shipping Authenticate (legacy/connector/xbuddy/auth.go:175) with
// the printer-identity half taken out: which printer a token belongs to is the
// host's to decide, and this only answers whether the credential is one this
// driver issued.
func (r *registrar) authenticate(fingerprint, token string) (string, error) {
	if fingerprint == "" {
		return "", fmt.Errorf("%w: no fingerprint", ErrRefused)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	switch r.mode {
	case ModeNone:
		return r.knownAsLocked(fingerprint), nil
	case ModeToken:
		return r.knownAsLocked(fingerprint), r.checkTokenLocked(fingerprint, token)
	case ModeFingerprint:
		return r.knownAsLocked(fingerprint), r.checkFingerprintLocked(fingerprint)
	case ModeBoth:
		if err := r.checkFingerprintLocked(fingerprint); err != nil {
			return "", err
		}

		return r.knownAsLocked(fingerprint), r.checkTokenLocked(fingerprint, token)
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownMode, r.mode)
	}
}

// knownAsLocked is the spelling this table holds for the printer behind a
// fingerprint in either spelling, and the argument itself when no registration
// names it. The caller holds the lock.
func (r *registrar) knownAsLocked(fingerprint string) string {
	if item, ok := r.resolveLocked(fingerprint); ok {
		return item.Fingerprint
	}

	return fingerprint
}

// resolveLocked is the entry a presented fingerprint belongs to, in either
// spelling. The caller holds the lock.
//
// The exact spelling is tried first, so a full fingerprint answers for itself.
// A value of exactly fingerprintHeaderLength characters is then looked up in
// the prefix index, and the entry it finds has to start with what was
// presented: the index is rebuilt by hand and the check is what makes a stale
// or shadowed slot refuse rather than answer for the wrong machine.
func (r *registrar) resolveLocked(fingerprint string) (*entry, bool) {
	if item, ok := r.byFingerprint[fingerprint]; ok {
		return item, true
	}

	if !headerSpelling(fingerprint) {
		return nil, false
	}

	item, ok := r.byPrefix[fingerprint]
	if !ok || !strings.HasPrefix(item.Fingerprint, fingerprint) {
		return nil, false
	}

	return item, true
}

// fileLocked puts one entry in the fingerprint index and, when its fingerprint
// is longer than a header spelling, in the prefix index. The caller holds the
// lock.
//
// A prefix already taken is left alone, so the machine that registered first
// keeps its header working and a later registration cannot take it over.
func (r *registrar) fileLocked(item *entry) {
	r.byFingerprint[item.Fingerprint] = item

	if len(item.Fingerprint) <= fingerprintHeaderLength {
		return
	}

	prefix := item.Fingerprint[:fingerprintHeaderLength]
	if _, taken := r.byPrefix[prefix]; taken {
		return
	}

	r.byPrefix[prefix] = item
}

// reindexPrefixesLocked rebuilds the prefix index from the entries that are
// left. The caller holds the lock.
//
// It runs when an entry is forgotten, because that entry may have owned a
// prefix another entry can now have. A walk of the table is a walk of at most
// defaultMaxRegistrations rows and happens when an operator rejects a
// registration or one is dropped to make room, which is not a rate.
func (r *registrar) reindexPrefixesLocked() {
	r.byPrefix = make(map[string]*entry, len(r.byFingerprint))

	// Oldest first, so the entry that wins a prefix is the same one fileLocked
	// would have given it, whatever order the map is walked in.
	ordered := make([]*entry, 0, len(r.byFingerprint))
	for _, item := range r.byFingerprint {
		ordered = append(ordered, item)
	}

	slices.SortFunc(ordered, func(a, b *entry) int {
		switch {
		case a.seq < b.seq:
			return -1
		case a.seq > b.seq:
			return 1
		default:
			return 0
		}
	})

	for _, item := range ordered {
		if len(item.Fingerprint) <= fingerprintHeaderLength {
			continue
		}

		prefix := item.Fingerprint[:fingerprintHeaderLength]
		if _, taken := r.byPrefix[prefix]; !taken {
			r.byPrefix[prefix] = item
		}
	}
}

// registrationFor returns what a printer said about itself when it registered.
// ok is false for a printer this driver never registered, which ModeNone
// permits.
func (r *registrar) registrationFor(fingerprint string) (Registration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	item, ok := r.resolveLocked(fingerprint)
	if !ok {
		return Registration{}, false
	}

	return item.Registration, true
}

// approveLocked marks a registration approved and writes its credentials into
// the allowlists the configured mode reads. The caller holds the lock.
func (r *registrar) approveLocked(item *entry) {
	item.Approved = true

	r.allowLocked(item)
}

// allowLocked writes an approved registration's credentials into the allowlists
// the configured mode reads. It is the half of approveLocked a restore needs on
// its own: a stored row is already approved, and marking it approved again
// would be a write nobody asked for. The caller holds the lock.
func (r *registrar) allowLocked(item *entry) {
	switch r.mode {
	case ModeToken:
		r.tokens[item.tokenHash] = item.Fingerprint
	case ModeFingerprint:
		r.fingerprints[item.Fingerprint] = true
	case ModeBoth:
		r.tokens[item.tokenHash] = item.Fingerprint
		r.fingerprints[item.Fingerprint] = true
	case ModeNone:
	}
}

// checkTokenLocked refuses a token this driver did not issue, and one issued to
// a different printer. The caller holds the lock.
func (r *registrar) checkTokenLocked(fingerprint, token string) error {
	if token == "" {
		return fmt.Errorf("%w: no token", ErrRefused)
	}

	owner, ok := r.tokens[digestOf(token)]
	if !ok {
		return fmt.Errorf("%w: the token is not one this driver issued", ErrRefused)
	}

	// The owner is a full spelling and so is what the presented one resolves
	// to, so a printer dialling with its header prefix is compared against the
	// fingerprint its registration carried and nothing shorter.
	if owner != r.knownAsLocked(fingerprint) {
		return fmt.Errorf("%w: the token belongs to another printer", ErrRefused)
	}

	return nil
}

// checkFingerprintLocked refuses a fingerprint no approved registration names.
// The caller holds the lock.
func (r *registrar) checkFingerprintLocked(fingerprint string) error {
	if fingerprint == "" {
		return fmt.Errorf("%w: no fingerprint", ErrRefused)
	}

	if !r.fingerprints[r.knownAsLocked(fingerprint)] {
		return fmt.Errorf("%w: the fingerprint is not approved", ErrRefused)
	}

	return nil
}

// digestOf is the stored form of a token: the hex-encoded SHA-256 of it. The
// table compares digests rather than tokens so that what it holds at rest is
// not a credential somebody can present.
func digestOf(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

// randomCode returns an operator-readable code, drawn uniformly from the
// alphabet by rejection sampling so no character is likelier than another.
func randomCode() (string, error) {
	limit := byte(byteSpace - byteSpace%len(codeAlphabet))

	var out strings.Builder

	out.Grow(codeLength)

	buf := make([]byte, 1)

	for out.Len() < codeLength {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("draw a registration code: %w", err)
		}

		if buf[0] >= limit {
			continue
		}

		// The modulus bounds the index by construction, and limit is what keeps
		// the draw uniform over the alphabet.
		out.WriteByte(codeAlphabet[int(buf[0])%len(codeAlphabet)])
	}

	return out.String(), nil
}

// randomToken returns the token a printer presents on the websocket, in the
// unpadded base32 the firmware carries in a header.
func randomToken() (string, error) {
	const bitsPerChar, bitsPerByte = 5, 8

	buf := make([]byte, (tokenLength*bitsPerChar+bitsPerByte-1)/bitsPerByte)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("draw a registration token: %w", err)
	}

	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)[:tokenLength], nil
}
