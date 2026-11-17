package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/transferapi"
)

const (
	printerNameLimit = 96

	// fleetScanWindow is how many directory rows one query of the listing walk
	// asks for. The walk exists because liveness cannot be joined in SQL, so
	// the whole authorized set is read to cut the caller's page and to count
	// the total. At the four hundred printers this product claims that is two
	// statements, and the window is what keeps the row buffer bounded however
	// large a deployment turns out to be.
	fleetScanWindow = 200

	// departedLimit is how many printers whose session has ended this plane
	// keeps the last reading of.
	//
	// The record is what tells a printer that went away from one that never
	// existed, and it costs one state tree per printer with no socket under it.
	// The limit is what stops that being a way to fill this process's memory:
	// the shipping connector admits a printer that presents a fingerprint, so
	// anything that can reach the registration path can mint identities faster
	// than an operator can look at them. Four hundred is the fleet this product
	// claims, so a whole fleet can go away and still be readable, and the four
	// hundred and first departure drops the oldest.
	departedLimit = 400

	// presenceQuiet bounds how often one printer's reachability reaches the bus.
	// A cable in a socket it does not quite fit bounces at whatever rate the
	// contact does, and a durable frame per bounce would flush the replay window
	// every reader resumes from. The first change after a quiet stretch still
	// leaves immediately, so what this costs is never the cable coming out.
	presenceQuiet = time.Second

	// admissionLifetime bounds how long a resolution waits for the session it
	// was resolved for. Admit and Arrived are two steps of one connection, so
	// the wait is milliseconds. A connection that drops in between, and a
	// driver that reattaches the session it already holds, both leave the
	// resolution parked, and this is when the fleet stops holding it.
	admissionLifetime = time.Minute
)

// parkedResolution is a resolution waiting for the session it was resolved
// for, with the moment it started waiting, so one whose session never arrives
// has an end.
type parkedResolution struct {
	resolution capability.Resolution
	at         time.Time
}

// devices is the host's half of every inbound dialect: it admits a printer that
// has dialled in, resolves what it can do, registers it so somebody may see it,
// and holds its state entry until the session ends.
//
// It lives in the composition root because it joins four things no one of them
// may import: a connector, the capability resolver, the authorization graph and
// the event hub.
//
// A printer holds a live entry for as long as its driver session does. The
// driver keeps a session across a disconnection, which is what lets a command
// queue for a printer that is switched off, so an entry moves out of the live
// table only when its session really ends.
//
// What it moves into is the departed table, which is the record an operator
// reads to tell a printer that went away from one that was never here. It holds
// the last state the printer reported and why the session ended; every
// operation that has to reach the machine is refused against it.
type devices struct {
	graph *authz.Store
	hub   *stream.Hub
	log   *slog.Logger
	org   string
	// meters is what this plane writes into the metric catalog. Its zero value
	// records nothing, so a plane built without one is a plane nobody is
	// scraping rather than a plane with a nil handle in the frame path.
	meters fleetMeters
	// files opens what this deployment's storage resources hold, which is where
	// a file sent to a printer is read from.
	files staging

	// incoming holds the temporary copies a send relays and deletes them
	// afterwards. Nil holds nothing and deletes nothing.
	incoming *incomingCopies
	// copies is what each printer holds that this process sent it, so a print
	// of the same file can start that copy instead of sending it again.
	copies sentCopies
	// previews holds the pictures printers read out of their own files. It is
	// the storage plane's cache, so core.preview_cache_bytes bounds both. A
	// plane built without one asks the printer every time.
	previews *previewCache
	// listings is the last directory listing each printer answered. A plane
	// built without one asks the printer every time.
	listings *listingCache
	// models is the catalogue the cards show, so a search finds a printer by
	// the model name on its card. Its zero value holds the core's rows only.
	models profile.Catalogue
	// thumbnails keeps the same pictures on disk, so a restart does not ask
	// every printer again. A plane built without one keeps them in memory only.
	thumbnails *thumbnailDisk
	// prefetchThumbnails is core.device_thumbnail_prefetch: whether a listing
	// from the printer starts the thumbnail walk. See deviceprefetch.go.
	prefetchThumbnails bool
	// transfers is what this process is moving onto a printer right now. It is
	// the only place a live byte count exists: the stored record is folded from
	// the two durable frames and knows nothing between them.
	transfers *transferRegister
	// past resolves the history adapter's transfer projection, and reports
	// false for a build that keeps none. It is a function and not a value for
	// the reason the history read surface is one: which instance answers is
	// resolved per query and not captured at boot.
	past func() (transferapi.Records, bool)
	// durable writes one frame straight to the store as it is published, so
	// the record of a command exists before the process could lose it. See
	// historydurable.go; a build with no history instance has none.
	durable durableWriter
	// stateBudget is what one device's state tree may retain, from the
	// operator's file. Zero and below take defaultTreeBudget.
	stateBudget int
	// records is where the fleet survives a restart. It is the graph's own
	// store: device_record carries a foreign key onto the directory the graph
	// owns, so the two are one handle, one migration ledger and one answer to
	// which devices exist.
	records records

	// commands mints one id per invocation. It is a monotonic ULID minter, so
	// the ids of one process sort in the order the commands were issued.
	commands ref.Minter

	// announcedJobs is what this process has already published about each
	// device's print. It is read and written by the flush goroutine alone, so
	// it carries no lock; see devicejobevents.go.
	announcedJobs map[string]jobAnnouncement
	// prints is the print every device began last, which a reading is matched
	// against to find the print it belongs to. See printidentity.go.
	prints *printBook
	// printStarts holds the instruction that started the print each device is
	// about to begin, until the row that print opens claims it. It is written
	// on a caller's goroutine and read on the flush, so it carries its own.
	printStarts *printStarts

	// identities binds a machine's fingerprint to the id this server addresses
	// it by. It is the only place a device id is minted, and it
	// carries its own lock rather than riding on mu below, because a handshake
	// asks it a question while a flush is holding the fleet.
	identities *deviceIdentities

	// openOwner is who owns a printer that nobody approved. See openOwnerOf.
	// Nil means nobody.
	openOwner func() string

	// forgetters are the connectors that can take back the credential they
	// issued a device, by connector name. A retirement calls the one that holds
	// the device, so the machine cannot dial in again on its old credential.
	// It is written while the connectors are mounted and read under mu.
	forgetters map[string]connectorapi.ForgetsDevices

	// residents are the connectors whose dialect says the server runs on their
	// printers, by connector name. Retire refuses their devices. It is written
	// while the connectors are mounted and read under mu.
	residents map[string]bool

	// dropCameras removes the cameras that watch a printer an operator
	// removed. Nil removes none.
	dropCameras func(ctx context.Context, printer string)

	mu       sync.Mutex
	entries  map[string]*device
	departed map[string]*device
	admitted map[string]parkedResolution
	closed   bool
	// pending is a record taken as its change happened, waiting for the next
	// flush. It is keyed by device, so a device that changes twice before one
	// flush is one row and not two, and the two changes' reported fields are
	// merged rather than the earlier one dropped.
	pending map[string]changedRecord
	// running is the in-flight idempotency map, keyed by (device, key) and
	// holding the id of the invocation that claimed it. It survives nothing.
	// See inFlight.
	running map[inFlight]string

	pumps sync.WaitGroup

	// life ends when Close starts, and every thumbnail walk runs under it.
	// walks is how Close waits for them.
	life    context.Context //nolint:containedctx // the plane's own lifetime, not a request's.
	endLife context.CancelFunc
	walks   sync.WaitGroup

	// The record flush. flushNow asks for one before the next tick, flushStop
	// ends the goroutine after a last one, and flushed is closed when it has
	// returned. flushNow is buffered by one because one flush writes every
	// changed device, so a second request while one is pending buys nothing.
	//
	// stopFlush owns the close of flushStop rather than the closed flag above.
	// closed says a printer may not arrive any more and is set by a caller that
	// is not always Close, so keying the one-shot on it would leave a Close
	// waiting on a goroutine nothing ever told to stop.
	flushNow  chan struct{}
	flushStop chan struct{}
	flushed   chan struct{}
	stopFlush sync.Once

	// flushing lets one flush run at a time. A flush takes the changed
	// records before it writes them, so without it a second caller could
	// find nothing to take and return while the first is still writing.
	flushing sync.Mutex
}

// device is one printer's state entry: the session the host commands through,
// the state tree the driver's patches build up, and what the connection under
// it has cost.
type device struct {
	id       string
	driver   string
	identity connectorapi.Identity
	session  *capability.Session
	// since is when this activation's session was filed. It is fixed for the
	// life of the entry, so it is read without the lock below.
	since time.Time

	// budget is what this device's state tree may retain, fixed for the life of
	// the entry and read without the lock below.
	budget int

	mu    sync.Mutex
	state map[string]json.RawMessage
	// quietSeen is whether the machine reported IDLE or READY since the last
	// take. The take hands it to the flush and clears it.
	quietSeen bool
	// tags are the plain words an operator put on this machine, in the order
	// the operator gave them. A person writes them and a machine never does, so
	// they are restored from the record store and never from a frame.
	tags []string
	// name is what an operator called this machine, and is empty for one
	// nobody has named. It is restored from the record store and never from a
	// frame, because a person wrote it.
	//
	// An empty one is not a gap. The rendering falls back to the directory's
	// display name, which is the model and the serial the machine reported, so
	// a device nobody named still reads as something a person recognises.
	name string
	// stateBytes is what the tree above costs, as the budget counts it: every
	// path and every value. It is carried rather than summed on each write,
	// because a walk of the tree per patch is a walk fifteen times per frame.
	stateBytes int
	// clippedValues and refusedPaths are what this host did not keep of what
	// this device sent.
	clippedValues uint64
	refusedPaths  uint64
	link          link
	// peer says which address the machine's connection comes from, and is nil
	// for a driver whose session does not say. It is fixed for the life of the
	// entry and read without the lock above.
	peer connectorapi.ReportsPeer
	// lastPeer is the address the machine's connection last came from. It
	// outlives the connection, so a printer that is away is still told apart
	// from one still to add. It is held under mu.
	lastPeer netip.Addr
	// gone is why the session ended, and is empty while it is open. lastLink is
	// the link reading taken as it ended, because the counters behind link
	// stop moving there and a rate over a window that keeps growing would read
	// as a machine going quiet.
	gone     string
	goneAt   time.Time
	lastLink httpapi.DeviceLink

	// retired is why this id stopped being its machine's, and is empty for
	// every device that still is one. retiredAt is when.
	//
	// It is not a departure. A departure is about one session and the next one
	// clears it; this is about the identity and is written once, when the id
	// is retired.
	// The entry stays in the fleet carrying it, so an operator reading the
	// listing finds the machine's history where they left it rather than a row
	// that vanished.
	retired   string
	retiredAt time.Time

	// lastSeen is when this device was last heard from, and changed is whether
	// the record store is behind what this entry holds. Both are the record's,
	// and both are read by takeRecord under the mutex above.
	lastSeen time.Time
	changed  bool

	// announced is the record as the last com.prusa.connect.device.record.changed
	// frame stated it, and stated is whether one has ever been published about
	// this entry. Together they are what makes the frame a change and not a
	// heartbeat: an entry marked by a telemetry patch is written to the store
	// and says nothing, because none of the fields the frame reports moved.
	announced recordFacts
	stated    bool

	// stopPrefetching ends the thumbnail walk running for this printer, and
	// prefetched is closed when that walk has returned. Both are nil while no
	// walk has started. See deviceprefetch.go.
	stopPrefetching context.CancelFunc
	prefetched      chan struct{}
	// warm is whether this printer is owed a warm-up of its storage roots.
	// See devicewarmup.go.
	warm atomic.Bool

	// pictures holds the one picture question this printer may have open. It
	// is made only for an entry with a session. See readThumbnail.
	pictures chan struct{}
}

// Why a printer's session ended, as far as this server can tell the cases
// apart. A driver reports that the session is over and nothing else, so a
// machine switched off, a cable pulled and firmware that stopped answering are
// one reason between them.
const (
	departureSessionEnded   = "session_ended"
	departureServerStopping = "server_stopping"
)

// arrivals is connectorapi.Arrivals for one driver instance.
//
// The baseline and the resolver are that driver's, captured when it was
// mounted. Both are per driver for the same reason: the ceiling is the driver's
// own code, and the profile registry a resolver is built over is the union that
// bounds a device those profiles do not cover. One resolver shared by every
// driver would hold every dialect's unknown devices to one dialect's data.
type arrivals struct {
	plane    *devices
	driver   connectorapi.Ref
	baseline connectorapi.Set
	resolver *capability.Resolver
	// kind is the device kind this driver's machines are registered under, and
	// therefore the kind their verbs are classified against. The empty value
	// is printer, so a device registered with no kind is a printer.
	kind authz.DeviceKind
}

var (
	_ httpapi.DeviceFleet   = (*devices)(nil)
	_ connectorapi.Arrivals = arrivals{}
)

// newDevices returns the device plane. org is the org an arriving printer is
// placed in: the graph's bootstrap writes nothing on a printer, so whatever
// registers one is what has to put it somewhere an operator can reach it.
//
// It is a parameter and not the constant because the composition root is what
// chooses it. Reaching for bootstrapOrg from in here would make a second org a
// change to this file rather than to the one line that decides it.
//
//nolint:unparam // see above: the seam is deliberate.
func newDevices(
	graph *authz.Store, stored records, hub *stream.Hub, log *slog.Logger, org string,
	files staging, meters fleetMeters, stateBudget int,
) *devices {
	plane := &devices{
		graph:       graph,
		hub:         hub,
		log:         log,
		org:         org,
		files:       files,
		meters:      meters,
		stateBudget: stateBudget,

		records:       stored,
		announcedJobs: map[string]jobAnnouncement{},
		prints:        newPrintBook(),
		printStarts:   newPrintStarts(),

		identities: newDeviceIdentities(),
		forgetters: map[string]connectorapi.ForgetsDevices{},
		residents:  map[string]bool{},
		transfers:  newTransferRegister(),
		entries:    map[string]*device{},
		departed:   map[string]*device{},
		admitted:   map[string]parkedResolution{},
		pending:    map[string]changedRecord{},
		flushNow:   make(chan struct{}, 1),
		flushStop:  make(chan struct{}),
		flushed:    make(chan struct{}),
	}

	plane.life, plane.endLife = context.WithCancel(context.Background())

	// The one goroutine that writes the fleet. It is started here rather than
	// by a caller, because a plane whose records are not being written is a
	// plane that forgets its fleet, and there is no configuration in which that
	// is what somebody wanted. Close ends it.
	go plane.flushRecords()

	return plane
}

// Arrivals returns the host surface one mounted driver hands its printers,
// carrying that driver's own ceiling and its own profile registry.
func (d *devices) Arrivals(
	driver connectorapi.Ref, baseline connectorapi.Set, resolver *capability.Resolver,
	kind authz.DeviceKind,
) connectorapi.Arrivals {
	return arrivals{plane: d, driver: driver, baseline: baseline, resolver: resolver, kind: kind}
}

// Command validates one command against the printer's resolved capability set
// and, only if it passes, sends it. The check is capability.Session's, which is
// above the driver: a driver is never reached with an argument nothing checked.
//
// A command for a printer that is away waits for it rather than failing, which
// is the queue-and-drain rule, and the caller's context is what bounds the wait.
//
// The action is derived from the verb and never named here. This is the one
// place every transport passes through. A site that named operate directly
// would hand the raw gcode verbs to anybody who may pause a print.
//
// The refusal for a verb nothing declares happens in two stages, so that one
// answer stays uniform and the other does not become an oracle.
//
// Stage one runs before the device is resolved. The verb is looked up in the
// union of every kind this build carries, both halves of the verb table
// together. A verb the union does not hold is refused with
// core.printer/VERB_UNKNOWN, carrying the verb and no fact about any device:
// the same answer whatever the caller holds and whether or not the id names a
// machine. The refusal names the verb this build spells the same command under
// where the spelling only differs in case or in the prefix, and that suggestion
// comes from the same union, so every caller gets the same one. The union is
// published to a caller holding no credential, so neither discloses anything
// the vocabulary route does not.
//
// Stage two runs behind the read grant. A verb the union holds but this
// device's kind does not declare is refused by name, and a caller who reaches
// nothing on the device gets the ordinary not-found instead, indistinguishable
// from a device that does not exist. That is deliberate: what stage two tells a
// caller is the device's kind, which the read grant already discloses.
func (d *devices) Command(
	ctx context.Context, principal string, id string, cmd httpapi.PrinterCommand,
) (httpapi.PrinterResult, error) {
	if !authz.KnownVerb(cmd.Verb) {
		refusal := fault.PrinterVerbUnknown.
			New("this build has no such capability verb").
			With(fault.StringParam("verb", cmd.Verb))

		if suggestion, named := authz.CanonicalVerb(cmd.Verb); named {
			refusal = refusal.With(fault.StringParam("suggested_verb", suggestion))
		}

		return httpapi.PrinterResult{}, refusal
	}

	kind, err := d.kindOf(ctx, id)
	if err != nil {
		return httpapi.PrinterResult{}, err
	}

	action := authz.ActionForVerb(kind, cmd.Verb)
	if !action.Valid() {
		// Stage two, and it is behind the read grant: a caller who holds
		// nothing on this device is told the device is not there, before it is
		// told what kind of device it is not.
		if _, sightErr := d.reach(ctx, principal, id, authz.ActionObserve); sightErr != nil {
			return httpapi.PrinterResult{}, sightErr
		}

		return httpapi.PrinterResult{}, fault.PrinterVerbUnknown.
			New("this device's kind does not declare that verb, though another kind in this build does").
			With(fault.StringParam("verb", cmd.Verb), fault.StringParam("kind", kind.String()))
	}

	// Minted before the authorization check and not after it. A refusal is
	// recorded under this id. With no id, a caller reaching for a verb they
	// may not send would exist only in the access log.
	commandID := d.mintCommand()

	entry, err := d.reach(ctx, principal, id, action)
	if err != nil {
		// A caller the graph refuses publishes nothing. A frame put on a
		// device's origin is a frame in that device's replay window, and
		// letting a caller who reaches nothing write into one would be a way to
		// evict a printer's presence frames while holding no grant on it. The
		// id is minted and unused, which costs a ULID.
		return httpapi.PrinterResult{}, err
	}

	// Past this line the caller reaches the device, so every refusal below is
	// recorded on the device's own origin: the reach check is what separates a
	// refusal worth a durable record from one that would be a write primitive
	// for anybody who can spell an id.
	if departed := entry.departure(); departed != nil {
		return httpapi.PrinterResult{}, d.refuseCommand(ctx, entry, commandID, cmd, kind,
			fault.PrinterSessionEnded.
				New("the printer's session has ended, so nothing can be sent to it").
				With(fault.StringParam("device_id", id)))
	}

	// The dangerous tier, after the class and never instead of it. A verb no
	// class confers is reached by a row naming it or by owning the machine.
	// CanVerb answers true for a verb no hand marked, so the ordinary verb
	// pays one indexed lookup.
	if err = d.reachesVerb(ctx, principal, id, kind, cmd.Verb); err != nil {
		return httpapi.PrinterResult{}, d.refuseCommand(ctx, entry, commandID, cmd, kind, err)
	}

	held, mine := d.claimKey(entry.id, cmd.Key, commandID)
	if !mine {
		// A second caller inside the first invocation's lifetime. It is not
		// dispatched twice and it publishes no frames of its own: the trail
		// then shows one principal requesting a command two callers asked for.
		// See inFlight.
		return httpapi.PrinterResult{CommandID: held, Duplicate: true}, nil
	}

	defer d.releaseKey(entry.id, cmd.Key)

	// The instruction first, because that is the order the two things happened
	// in: a caller asked, and then this server allowed it. Each frame is
	// stamped as it is published. Published the other way round, a stored
	// command would read authorized_at before requested_at, and the record
	// would say the command was allowed before anybody asked for it.
	//
	// It is still after the reach check and after the claim, which is what keeps
	// a caller who reaches nothing from writing into a device's replay window
	// and a deduplicated second caller from publishing frames of its own. It is
	// before the driver is reached, which narrows the window in which a command
	// can run unrecorded from the whole call to the feed's flush interval. It is
	// the only one of the three that carries a TTL, because it is the only one
	// of the three that expires.
	d.publishRequested(ctx, entry.id, commandID, cmd, kind, entry.session.Resolution())

	// Then the audit frame, at the authorization point, which is the only place
	// holding the principal, the action and the device together. Its class is
	// audit and seeing it takes view_audit on this device, on the live stream
	// and on the history read alike.
	authType, authID := principalParts(principal)
	d.publishAudit(ctx, entry.id, commandID, cmd.Verb, authType, authID)

	if cmd.Verb == httpapi.StartPrintVerb {
		entry.stopPrefetch()
	}

	// The gauge is moved either side of the call and not around the whole
	// function, because a command for a printer that is away waits inside Do:
	// what this measures is commands a machine has been given and not answered,
	// which is the queue an operator wants to see.
	pending := d.meters.pending(entry.driver)
	pending.Add(1)

	started := time.Now()

	result, err := entry.session.Do(ctx, connectorapi.Command{
		Verb: connectorapi.Capability(cmd.Verb),
		Args: cmd.Args,
		Key:  cmd.Key,
	})

	pending.Add(-1)

	if changesStorage(cmd.Verb) {
		written := err
		if written == nil && !result.OK {
			written = fmt.Errorf("the printer refused %s", cmd.Verb)
		}

		d.fileChanged(entry, connectorapi.Capability(cmd.Verb), cmd.Args["path"], nil, written)
	}

	d.meters.commanded(entry.driver, cmd.Verb, commandOutcome(result, err), time.Since(started))

	// The instruction is held for the print it starts, so the frame that opens
	// that print can name it. A start the machine refused names nothing: see
	// printStarts in devicejobevents.go.
	d.notePrintStart(entry.id, commandID, cmd.Verb, result, err)

	// On a context detached from the request, so a command abandoned by its
	// caller still terminates its own record. On the request's context, a
	// caller that went away would leave the command with no settled frame.
	d.publishSettled(
		context.WithoutCancel(ctx), ctx, entry.id, commandID, cmd.Verb,
		result, err, time.Since(started),
	)

	if err != nil {
		d.meters.failed(codeOf(err), entry.id)

		// The id goes back with the failure as well as with the answer. A
		// caller that cannot address the row this wrote holds a problem
		// document naming a record it cannot read.
		return httpapi.PrinterResult{}, withCommandID(err, commandID)
	}

	// A refusal the printer decided is an answer and not a fault of this
	// server's, so it leaves as a 200 whose body says no, carrying the code for
	// the refusal beside the machine's own word. That holds whether or not the
	// firmware said why: a bare REJECTED answering a 409 problem document while a
	// refusal carrying FILE_EXISTS answers 200 puts the machine's own no in the
	// shape this API reserves for a refusal this server decided.
	//
	// A refusal is not counted as an error either. printer_errors is what an
	// operator watches to see a machine or a link going wrong, and a printer
	// answering no is neither.
	return httpapi.PrinterResult{
		OK:            result.OK,
		CommandID:     commandID,
		FaultCode:     refusalCode(result),
		MachineReason: refusalReason(result),
		Answer:        commandAnswer(result),
		Ignored:       result.Ignored,
		Substituted:   result.Substituted,
	}, nil
}

// Close ends every session and waits for the goroutine reading each one. It
// runs before the event hub closes, so a printer publishing its way out of
// service still reaches whoever was watching the shutdown.
//
// The record flush is stopped last and writes one more time on its way out.
// Every session has ended by then and every pump has recorded its printer's
// departure, so the last thing written is the fleet as it really was when this
// process stopped: away, with the reason and the time each machine was last
// heard from. Stopping the flush first would lose exactly that.
func (d *devices) Close() {
	d.endLife()

	d.mu.Lock()
	d.closed = true

	open := make([]*device, 0, len(d.entries))
	for _, entry := range d.entries {
		open = append(open, entry)
	}

	d.mu.Unlock()

	for _, entry := range open {
		if err := entry.session.Close(); err != nil {
			d.log.Error("closing a printer session", "printer", entry.id, "error", err.Error())
		}
	}

	d.pumps.Wait()
	d.walks.Wait()

	// A second Close is a caller closing what it also deferred, and it must not
	// close the stop channel twice. Both callers still wait, so neither returns
	// while the fleet is still being written.
	d.stopFlush.Do(func() { close(d.flushStop) })

	<-d.flushed
}

// admit answers whether the host takes a printer that has identified itself,
// and with what.
//
// Arrive and not Resolve: the capability epoch moves on every arrival rather
// than only when the merged set differs, because a disconnection
// this host did not watch can carry a firmware update or a corrected profile,
// and "nothing changed" is not something it can promise about time it did not
// observe.
//
// A printer the host is already holding a session for takes the new resolution
// here. Its driver reattaches the socket to that session rather than announcing
// a second printer, so this is the only place the answer can land, and a
// resolution nothing adopted is a printer commanded against the firmware of an
// earlier activation.
func (d *devices) admit(
	ctx context.Context,
	resolver *capability.Resolver,
	baseline connectorapi.Set,
	identity connectorapi.Identity,
) (connectorapi.Set, error) {
	// What a driver says about a machine is text the machine chose, so it is
	// bounded here, before anything files it, logs it or labels a metric with
	// it. arrived and DeviceID bound the same identity the same way, because
	// the three are given it separately and an entry compares them.
	identity = machinetext.ClipIdentity(identity)
	id := d.identities.identify(identity)

	// The binding is the fleet's from here. A printer this host has admitted is
	// a device it has met, so the id is not one of the minted-and-never-used
	// bindings an unknown caller can evict (see unclaimedIdentityLimit), and a
	// registration approved for this fingerprint later is a transfer and not a
	// first admission.
	//
	// It is marked here and not at arrival, because the id has to survive the
	// window between the two: arrived looks the same fingerprint up again, and
	// a binding evicted in between would leave a printer arriving under an id
	// nothing admitted.
	d.identities.meet(id)

	// No probe here. The machine's own answer is on the session, and the
	// session does not exist yet for a dialect whose devices dial in: the
	// driver is asking what to narrow itself to before it builds one. arrived
	// resolves the third tier, on this epoch.
	resolution := resolver.Arrive(ctx, baseline, identity, nil)

	if err := d.record(id, identity, resolution); err != nil {
		return nil, err
	}

	d.log.InfoContext(ctx, "a printer was admitted",
		"printer", id,
		"model", identity.Model,
		"serial", serialOf(identity),
		"unit_uid", identity.UnitUID,
		"capabilities", len(resolution.Capabilities),
		"epoch", resolution.Epoch,
		"profile_matched", resolution.ProfileMatched)

	return resolution.Capabilities.Clone(), nil
}

// record parks the resolution for the session that is about to arrive and hands
// it to the entry that is already here.
//
// An entry describes one activation of one machine, so it takes the resolution
// only while the printer still says it is what the entry was filed as. A printer
// reporting a different model or firmware is a different activation: its driver
// ends the session it was holding and announces the machine that is there now,
// and adopting here would leave this entry reporting one firmware's identity
// beside another firmware's capabilities until it did.
//
// The epoch still moves on every arrival whether or not the set
// did. Nothing is published from here: arrived is one per entry, published when
// the session is filed, and a socket coming back on an entry that is already
// here is what presence carries.
func (d *devices) record(
	id string, identity connectorapi.Identity, resolution capability.Resolution,
) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return fmt.Errorf("%w: this server is shutting down", connectorapi.ErrDeviceRefused)
	}

	now := time.Now()

	d.dropStaleAdmissions(now)

	d.admitted[id] = parkedResolution{resolution: resolution, at: now}

	entry, live := d.entries[id]
	if !live || entry.identity != identity {
		return nil
	}

	entry.session.Adopt(resolution)

	return nil
}

// arrived takes the session for a printer admit accepted: it registers the
// printer in the authorization graph, wraps the session in the resolution the
// host decided, and starts reading its state.
func (d *devices) arrived(
	ctx context.Context, host arrivals, session connectorapi.Session,
) error {
	driver, kind := host.driver, host.kind

	identity := machinetext.ClipIdentity(session.Identity())
	id := d.identities.identify(identity)

	resolution, admitted := d.claim(id)
	if !admitted {
		return fmt.Errorf("%w: %s arrived without being admitted", connectorapi.ErrSessionNotTaken, id)
	}

	resolution = host.probed(ctx, identity, session, resolution)

	// An arrival whose context has ended, for example while the driver's Probe
	// had not answered, is refused here and not at register, so the error names
	// the reason and not the graph.
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %s: the arrival ended before the host took the session: %w",
			connectorapi.ErrSessionNotTaken, id, context.Cause(ctx))
	}

	if err := d.register(ctx, id, identity, kind); err != nil {
		return fmt.Errorf("%w: register %s: %w", connectorapi.ErrSessionNotTaken, id, err)
	}

	// The row exists now, so the ownership an approval decided can be written
	// on it. It waits until here because the graph holds no owner for a device
	// with no directory row, and an approval happens before its machine has
	// one. A failure is reported and the printer is served: the machine is on
	// the wire either way, and an unowned device is a state this server
	// already serves.
	//
	// When nobody approved the printer, an open server makes it the
	// principal's. That principal is the only person such a server has, so
	// nobody else could have let the printer in.
	by := d.identities.takeApprover(id)
	if by == "" && d.openOwner != nil {
		by = d.openOwner()
	}

	if by != "" {
		if err := d.claimOwnership(ctx, id, by); err != nil {
			d.log.ErrorContext(ctx, "the printer arrived and its owner could not be written",
				"printer", id, "approved_by", by, "error", err.Error())
		}
	}

	// What a person wrote about this machine, taken off the entry this one
	// replaces. Everything else below comes from the handshake, which is what
	// the machine is saying about itself now.
	named, tagged := d.operatorFacts(id)

	entry := &device{
		id:       id,
		driver:   driver.String(),
		identity: identity,
		session:  capability.Wrap(session, resolution),
		state:    map[string]json.RawMessage{},
		name:     named,
		tags:     tagged,
		budget:   d.treeBudget(),
		since:    time.Now(),
		lastSeen: time.Now(),
		pictures: make(chan struct{}, 1),
	}

	// capability.As finds the counters on the driver's session, also inside the
	// dial loop's wrapper. A driver that does not count leaves counters nil, and
	// the API reports its link as not counted.
	entry.link.counters, _ = capability.As[linkCounters](session)
	entry.peer, _ = capability.As[connectorapi.ReportsPeer](session)
	// The address is read once here, so a printer that goes away before
	// anyone lists the printers nearby is still named by it.
	entry.address()

	previous, taken := d.file(entry)
	if !taken {
		return fmt.Errorf("%w: this server is shutting down", connectorapi.ErrSessionNotTaken)
	}

	if previous != nil {
		// One printer, one entry. The session that was there is not this one,
		// and leaving it open would leave a goroutine reading a printer nothing
		// can reach.
		if err := previous.session.Close(); err != nil {
			d.log.ErrorContext(ctx, "closing a superseded printer session", "printer", id, "error", err.Error())
		}

		// The machine announced itself again over a session this host was still
		// holding. Its old series are dropped rather than left beside the new
		// ones, because a reconnection can carry another firmware and the info
		// gauge labels the firmware.
		d.meters.forget(id)
		d.meters.reconnected(entry.driver, reasonRearrived)
	}

	d.publish(ctx, entry.id, event.TypeDeviceArrived, event.ClassState, "", arrivalOf(entry, resolution))

	// The record is written at once and not at the next tick. This is the one
	// write that says the machine exists at all: a device admitted and then
	// lost with the process would otherwise be a device the next start has
	// never heard of, and its directory row would list with no model, no serial
	// and no last_seen.
	d.writeSoon(entry)

	//nolint:gosec // G118: the pump outlives the call that admits the device, so the request context would cancel it on return. There is no lifecycle context to inherit.
	go d.pump(entry)

	return nil
}

// storagesChanged publishes the storages a printer reported now and warms the
// root of each. The root listing and every storage no longer reported are
// forgotten, because the root lists the drives and a drive pulled out takes
// its whole tree with it.
func (d *devices) storagesChanged(entry *device) {
	held, err := heldStorages(context.Background(), entry)
	if err != nil {
		d.log.Error("reading the storages a printer reported", "printer", entry.id, "error", err.Error())
		d.listings.drop(entry.id)

		return
	}

	d.listings.keepStorages(entry.id, held.Items)

	d.publish(context.Background(), entry.id, event.TypeDeviceStoragesChanged,
		event.ClassState, "", storagesChangeOf(entry, held))

	d.warmUp(entry)
}

// retirePrinter ends a session the driver closed: it releases the driver's half,
// moves the entry to the departed table and says the printer has gone.
//
// The last link reading is taken before the session is closed. The counters
// behind it belong to the driver, and a reading taken after the close is a rate
// measured over a window nothing is filling.
func (d *devices) retirePrinter(entry *device) {
	entry.stopPrefetch()
	entry.warm.Store(false)
	d.listings.drop(entry.id)
	entry.depart(d.departureReason())

	if err := entry.session.Close(); err != nil {
		d.log.Error("closing a printer session that ended", "printer", entry.id, "error", err.Error())
	}

	d.retire(entry)

	d.publish(context.Background(), entry.id, event.TypeDeviceDeparted, event.ClassState, "", departureOf(entry))

	// Written at once and not at the next tick. This is the write that stops
	// the next start reporting the machine as reachable, and it carries the
	// reason and the last state a person reads while it is away.
	d.writeSoon(entry)
}

// departureReason names what ended the session as far as this plane can tell.
// A printer that goes while the plane is closing went because this server is
// stopping, and every other case is the driver releasing it.
func (d *devices) departureReason() string {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return departureServerStopping
	}

	return departureSessionEnded
}

// reach resolves the caller, checks the action against the graph and returns
// the printer's entry, whether its session is open or has ended. Every refusal
// is the same one: a printer the caller may not act on is reported exactly as
// one this process has never been dialled by.
func (d *devices) reach(
	ctx context.Context, principal string, id string, action authz.Action,
) (*device, error) {
	entry, _, err := d.reachHolding(ctx, principal, id, action)

	return entry, err
}

// reachLive is reach for an operation that has to talk to the machine. A
// printer whose session has ended is refused with a code of its own, which is
// the answer that separates it from an id nothing answers to: the caller is
// told the printer is known and is not there, and may read its last state.
//
// The check runs after the authorization check and never before it. A caller
// who holds nothing on the printer must not learn from the refusal that this
// server has one.
func (d *devices) reachLive(
	ctx context.Context, principal string, id string, action authz.Action,
) (*device, error) {
	entry, err := d.reach(ctx, principal, id, action)
	if err != nil {
		return nil, err
	}

	if departed := entry.departure(); departed != nil {
		return nil, fault.PrinterSessionEnded.
			New("the printer's session has ended, so nothing can be sent to it").
			With(fault.StringParam("device_id", id))
	}

	return entry, nil
}

// register puts the printer in the directory the listing joins and places it in
// the org, because the graph's first grant deliberately writes nothing on any
// printer: a printer is invisible until whatever registered it says where it
// belongs.
//
// Both go in one call because both are one arrival. Each mutation of the graph
// recompiles the whole projection and moves the epoch every open subscription
// re-authorizes on, so two of them per machine is twice the write a fleet
// coming back after a restart pays for, on the one writer SQLite has.
func (d *devices) register(
	ctx context.Context, id string, identity connectorapi.Identity, kind authz.DeviceKind,
) error {
	object, err := authz.NewObject(authz.TypeDevice, id)
	if err != nil {
		return err
	}

	tenant, err := authz.NewSubject(authz.TypeOrg, d.org)
	if err != nil {
		return err
	}

	placement, err := authz.NewTuple(object, authz.RelOrg, tenant)
	if err != nil {
		return err
	}

	return d.graph.RegisterDevice(ctx,
		authz.DeviceRow{ID: id, Name: displayName(id, identity), Kind: kind}, placement)
}

// claim takes the resolution admit left for this printer, so a session that
// arrives without one is refused rather than served against a set nobody
// resolved.
func (d *devices) claim(id string) (capability.Resolution, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	parked, waiting := d.admitted[id]
	delete(d.admitted, id)

	return parked.resolution, waiting
}

// dropStaleAdmissions forgets every resolution whose session did not arrive
// inside admissionLifetime.
//
// It runs from the admission path and not from a ticker. The table grows only
// when a printer is admitted, so that is when it needs looking at, and the
// fleet starts no goroutine to keep one map short.
//
// The caller holds d.mu.
func (d *devices) dropStaleAdmissions(now time.Time) {
	for id, parked := range d.admitted {
		if now.Sub(parked.at) >= admissionLifetime {
			delete(d.admitted, id)
		}
	}
}

// file puts the entry in the live table and returns the one it replaced. It
// reports false once Close has run, which is what stops a printer arriving into
// a plane that is shutting down and leaving a goroutine behind.
//
// A printer that comes back leaves the departed table here. The record of its
// last visit is what an operator reads while it is away, and the machine is
// answering again.
func (d *devices) file(entry *device) (previous *device, taken bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil, false
	}

	delete(d.departed, entry.id)

	previous = d.entries[entry.id]
	d.entries[entry.id] = entry
	d.pumps.Add(1)

	return previous, true
}

// retire moves the entry out of the live table and into the departed one,
// unless a later arrival for the same printer already replaced it. That entry
// is the printer now, and a superseded one is not a departure anybody can read.
func (d *devices) retire(entry *device) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.entries[entry.id] != entry {
		return
	}

	delete(d.entries, entry.id)

	if d.closed {
		// Nothing will read this table again, and a shutdown is not the moment
		// to hold every printer's state tree for the length of the drain.
		return
	}

	d.evictDepartedLocked()

	d.departed[entry.id] = entry
}

// evictDepartedLocked makes room for one more departure by dropping the oldest,
// so the table never holds more than departedLimit. A printer dropped here
// reads as one this server was never dialled by, which is what the whole table
// exists to avoid, so the one dropped is always the one nobody is still looking
// for.
func (d *devices) evictDepartedLocked() {
	for len(d.departed) >= departedLimit {
		var (
			oldestID string
			oldestAt time.Time
		)

		for id, entry := range d.departed {
			at := entry.departedAt()
			if oldestID == "" || at.Before(oldestAt) {
				oldestID, oldestAt = id, at
			}
		}

		delete(d.departed, oldestID)
	}
}

// entry returns the printer's live entry, and reports false for a printer whose
// session has ended.
func (d *devices) entry(id string) (*device, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	entry, ok := d.entries[id]

	return entry, ok
}

// known returns the entry this plane holds for a printer, live or departed, and
// reports false for an id it has never been dialled by. It is what the listing
// and every read join against: a printer that went away is still a printer
// somebody is looking for.
func (d *devices) known(id string) (*device, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if entry, live := d.entries[id]; live {
		return entry, true
	}

	entry, ok := d.departed[id]

	return entry, ok
}

// publish puts one device event on the bus. A hub that has shut down is the
// ordinary end of a process rather than a failure worth a line above debug.
//
// It takes the device's id and not its entry, because the record flush publishes
// from rows it has written rather than from the entries they came off: an entry
// that departed during a shutdown is gone from both tables by then, and the row
// is still owed a frame. The origin is the same one every other device event
// carries, which is what makes one visibility answer cover all of them.
func (d *devices) publish(
	ctx context.Context, id string, kind string, class event.Class, key string, data json.RawMessage,
) {
	origin, err := event.NewOrigin(event.KindDevice, id)
	if err != nil {
		d.log.ErrorContext(ctx, "a device id is not an origin", "device", id, "error", err.Error())

		return
	}

	d.publishAt(ctx, origin, id, kind, class, key, data)
}

// publishAt puts one event on the bus under an origin the caller resolved.
//
// It exists for the frames that are about a machine and are not about a device
// row: a registration is held by a connector and names no device until it is
// approved, so its origin is the connector instance and the visibility answer
// that covers it is the one for that resource.
//
// what names the thing the frame is about, for the log line alone.
func (d *devices) publishAt(
	ctx context.Context, origin event.Origin, what string,
	kind string, class event.Class, key string, data json.RawMessage,
) {
	if _, err := d.hub.Publish(stream.Publication{
		Origin: origin,
		Type:   kind,
		Class:  class,
		Key:    key,
		Data:   data,
	}); err != nil {
		d.log.DebugContext(ctx, "a device event reached no hub",
			"origin", what, "type", kind, "error", err.Error())

		return
	}

	d.meters.published(kind)
}

// Admit asks the host whether it will take this printer. The outcome is counted
// here rather than inside admit, because this is the layer that knows which
// driver offered the machine.
func (a arrivals) Admit(ctx context.Context, identity connectorapi.Identity) (connectorapi.Set, error) {
	set, err := a.plane.admit(ctx, a.resolver, a.baseline, identity)
	if err != nil {
		a.plane.meters.admitted(a.driver.String(), outcomeRefused)

		return nil, err
	}

	a.plane.meters.admitted(a.driver.String(), outcomeAdmitted)

	return set, nil
}

// Arrived hands the host the session for a printer Admit accepted.
func (a arrivals) Arrived(ctx context.Context, session connectorapi.Session) error {
	return a.plane.arrived(ctx, a, session)
}

// probed removes from a resolution every capability the printer did not
// report. It runs here and not at admission, because a driver asks its printer
// during the handshake, and the session exists only after that.
//
// The epoch stays the same. The arrival event is published after this runs, so
// every reader of that event gets the narrowed set.
//
// The resolution from the admission stays when neither the session nor a
// session it wraps implements ReportsProbe, or when Probe reports nothing or
// panics (see askProbe). The result never has a capability the admission or
// the driver's baseline did not have.
func (a arrivals) probed(
	ctx context.Context, identity connectorapi.Identity,
	session connectorapi.Session, parked capability.Resolution,
) capability.Resolution {
	reporter, reports := capability.As[connectorapi.ReportsProbe](session)
	if !reports {
		return parked
	}

	probe := a.askProbe(ctx, identity, reporter)
	if probe == nil {
		return parked
	}

	narrowed := a.resolver.Resolve(ctx, a.baseline, identity, probe, parked.Epoch)

	a.plane.log.DebugContext(ctx, "a machine said what it has, and its capability set was narrowed to it",
		"model", identity.Model,
		"serial", serialOf(identity),
		"before", len(parked.Capabilities),
		"after", len(narrowed.Capabilities))

	return narrowed
}

// DeviceID is the id this host addresses a device by, which is what a dialect
// serving an identity lookup on its own frozen wire answers with.
//
// It satisfies xbuddy.Identities, going the other way from admits: there the
// host reaches into the driver's table, here the driver reaches the host for a
// name only the host can give.
//
// It is looked up and not derived, so a route that asks before the device
// has ever held a session is the route that mints. A printer asks this at
// GET /p/auth and then dials the websocket with the answer, so binding here is
// what makes the two the same string; see deviceIdentities.identify.
func (a arrivals) DeviceID(identity connectorapi.Identity) string {
	return a.plane.identities.identify(machinetext.ClipIdentity(identity))
}

// depart records why this printer's session ended, with the last link reading
// it will ever have. It is called once, before the session is closed, so the
// counters it reads are still the driver's own.
func (e *device) depart(reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.gone != "" {
		return
	}

	e.lastLink = e.link.observe(time.Now())
	e.goneAt = time.Now()
	e.gone = reason
}

// connected is whether a session is open to this printer right now. A printer
// whose session has ended is not connected, whatever the closed session
// underneath answers: the driver owns that answer while it holds the session,
// and it holds none.
//
// A device read back from the record store holds no session either, and the
// nil check guards that case a second time. Such an entry is always departed,
// so the first branch already answers it. If something ever builds one that
// is not, the answer is still false and not a panic in the listing, which is
// a page every client asks for.
func (e *device) connected() bool {
	if e.departure() != nil || e.session == nil {
		return false
	}

	return e.session.Connected()
}

// seenAt is when this process last heard from the machine: the moment it
// arrived, and every frame after that. A device restored from the store carries
// the instant the store holds instead, because this process has heard nothing
// from it at all.
//
// It is process state for the same reason presence is. Asking the record for it
// answers "never" about a machine whose frames are arriving, for as long as a
// flush is owed.
func (e *device) seenAt() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.lastSeen
}
