// Package bridge shows the devices of this server to a Fractal network as
// Fractal interfaces, and turns the requests of the node above into device
// commands.
//
// mapping.json has the printer knowledge: which state path each property
// reads, how the value converts, and which verb each method becomes. The Go
// code names no printer and no verb.
//
// The bridge checks no permission. It sends each command to the host with a
// principal, and the host decides.
package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// The values a Config takes when a field is left at zero.
const (
	// DefaultFlush is how often the bridge sends the changed properties to the
	// node above. A printer sends telemetry several times a second, so the
	// bridge collects the readings and sends one report per interface.
	DefaultFlush = 500 * time.Millisecond
	// DefaultResync is how often the bridge reads the whole device listing
	// again. An event already announces every device that connects or leaves,
	// so the listing only corrects the bridge after it missed an event.
	DefaultResync = 30 * time.Second
	// DefaultPage is how many devices one listing call asks for.
	DefaultPage = 200
	// DefaultStartDeadline is how long a print of a fractal URL may take to
	// start before it is dropped.
	DefaultStartDeadline = 10 * time.Minute
	// DefaultMaxHold is how long a started print keeps its copy at most. It
	// frees the copy when the core's settled event was lost.
	DefaultMaxHold = 24 * time.Hour
)

// The errors New and Snapshot return.
var (
	// ErrNoFleet means New received a Config with no Fleet.
	ErrNoFleet = errors.New("the bridge has no fleet")
	// ErrNoNode means New received a Config with no Node.
	ErrNoNode = errors.New("the bridge has no node")
	// ErrNoMapping means New received a Config with no Mapping.
	ErrNoMapping = errors.New("the bridge has no mapping table")
	// ErrNoDevice means Snapshot received a device identifier the bridge does
	// not know.
	ErrNoDevice = errors.New("this server holds no such device")
)

// Fleet lists the devices of this server, reads one, and sends a command to
// one. The Client of sdk/deviceapi implements it with calls to the host.
type Fleet interface {
	List(ctx context.Context, params deviceapi.ListParams) (deviceapi.Page, error)
	Get(ctx context.Context, params deviceapi.GetParams) (deviceapi.Device, error)
	Command(ctx context.Context, params deviceapi.CommandParams) (deviceapi.Result, error)
	Print(ctx context.Context, params deviceapi.PrintParams) (deviceapi.Started, error)
}

// Registrar registers one interface instance with the Fractal node. The main
// package of this module implements it with an adapter around the Node of
// sdk/fractal/node.
type Registrar interface {
	Register(inst fractalnode.Instance) (Reporter, error)
}

// Reporter sends a state report for one registered interface instance. The
// handle that the Node of sdk/fractal/node returns from Register implements it.
type Reporter interface {
	Report(ctx context.Context, props fractal.Props, at fractal.Timestamp) error
}

// Files fetches the file a fractal URL names. The files package of this
// module implements it.
type Files interface {
	Reachable(ctx context.Context, id sdkfiles.Identity) bool
	Obtain(ctx context.Context, u sdkfiles.URL) (storage, source string, release func(), err error)
	PrinterPaths(ctx context.Context, u sdkfiles.URL, storage, source string) []string
}

// Config is the input of New. Fleet, Node and Mapping are required. Every other
// field has a default.
type Config struct {
	// Fleet reads the devices of this server and sends them commands.
	Fleet Fleet
	// Node is the Fractal node the bridge registers its interfaces with.
	Node Registrar
	// Mapping is the table that maps a device to Fractal interfaces.
	Mapping *Mapping
	// Principal returns who a command runs as, and is called at every command.
	// Empty or nil means the module itself.
	Principal func() string
	// Flush is how often the bridge sends the changed properties to the node
	// above. Zero or less means DefaultFlush.
	Flush time.Duration
	// Resync is how often the bridge reads the whole device listing again.
	// Zero or less means DefaultResync.
	Resync time.Duration
	// Now returns the current time. Nil means time.Now. A test passes a clock
	// it controls.
	Now func() time.Time
	// Logger receives the log lines of the bridge. Nil discards them.
	Logger *slog.Logger
	// Files prints a fractal URL. Nil refuses one with CodeURLNotFetched, as a
	// server without the files extension does.
	Files Files
	// StartDeadline is files.start_deadline. Zero or less means
	// DefaultStartDeadline.
	StartDeadline time.Duration
	// MaxHold is the longest a started print keeps its copy when the
	// core's settled event never arrives. Zero or less means DefaultMaxHold.
	MaxHold time.Duration
	// Access is asked whether the principal may print before a fractal URL is
	// fetched. Nil asks nothing, and device.print decides after the fetch.
	Access Access
}

// Access asks the host whether one principal may do one action on one
// object. *accessapi.Client implements it.
type Access interface {
	Check(ctx context.Context, params accessapi.CheckParams) (accessapi.Decision, error)
}

// Bridge reports the state of this server's devices as Fractal interfaces, and
// turns a property change or a method call from the node above into a device
// command. It is safe for concurrent use.
type Bridge struct {
	fleet     Fleet
	node      Registrar
	mapping   *Mapping
	principal func() string
	flush     time.Duration
	resync    time.Duration
	now       func() time.Time
	log       *slog.Logger

	// reporting orders a flush against a full report, so an older value is
	// never sent after a newer one. It is taken before mu.
	reporting sync.Mutex

	mu      sync.Mutex
	devices map[string]*deviceEntry
	dirty   map[string]struct{}
	// named maps each name on the network to the device that took it. A
	// second device with the same name is not shown.
	named map[string]string

	files         Files
	access        Access
	startDeadline time.Duration
	maxHold       time.Duration
	// life ends when Run returns, and with it every print still fetching.
	life context.Context //nolint:containedctx // the bridge's own lifetime, not a request's.
	end  context.CancelFunc
	// jobs are the prints still fetching, which Run waits for.
	jobs sync.WaitGroup
	// releases has, per transfer, the hold a print has on its copy, and
	// started the print that transfer belongs to.
	releases map[string]func()
	started  map[string]fetchJob
	// printing counts the prints between Fleet.Print and their hold, and
	// settledEarly the transfers that settled in that gap.
	printing     int
	settledEarly map[string]event.TransferSettled
	// fetched counts the prints of fractal URLs by outcome.
	fetched map[string]uint64
}

// deviceEntry is the record the bridge keeps for one device.
type deviceEntry struct {
	// device is the device as the host last returned it, with later telemetry
	// readings written into its State.
	device deviceapi.Device
	// present is false after the host stops listing the device.
	present bool
	// changed is the time of the newest reading.
	changed fractal.Timestamp
	// instances are the interface instances of the device, in the order
	// they were registered, and held has each of them by instanceKey.
	instances []*instance
	held      map[string]*instance
}

// instance is one interface instance of one device.
type instance struct {
	iface *InterfaceMap
	// tool is the head or the slot of the instance, and empty for an
	// interface with one instance per device.
	tool connectorapi.ToolID
	// handle sends the reports. It is nil when the node refused the
	// instance.
	handle Reporter
	// reported has the property values the bridge last sent to the node
	// above.
	reported fractal.Props
}

// instanceKey is the key of one instance in deviceEntry.held.
func instanceKey(name fractal.InterfaceName, tool connectorapi.ToolID) string {
	return string(name) + "/" + string(tool)
}

// New builds a Bridge. It registers no interface and starts no goroutine. Run
// does both.
func New(cfg Config) (*Bridge, error) {
	if cfg.Fleet == nil {
		return nil, ErrNoFleet
	}

	if cfg.Node == nil {
		return nil, ErrNoNode
	}

	if cfg.Mapping == nil {
		return nil, ErrNoMapping
	}

	built := &Bridge{
		fleet:     cfg.Fleet,
		node:      cfg.Node,
		mapping:   cfg.Mapping,
		principal: cfg.Principal,
		flush:     cfg.Flush,
		resync:    cfg.Resync,
		now:       cfg.Now,
		log:       cfg.Logger,
		devices:   make(map[string]*deviceEntry),
		dirty:     make(map[string]struct{}),
		named:     make(map[string]string),

		files:         cfg.Files,
		access:        cfg.Access,
		startDeadline: cfg.StartDeadline,
		maxHold:       cfg.MaxHold,
		releases:      make(map[string]func()),
		started:       make(map[string]fetchJob),
		settledEarly:  make(map[string]event.TransferSettled),
		fetched:       make(map[string]uint64),
	}

	built.life, built.end = context.WithCancel(context.Background())

	if built.flush <= 0 {
		built.flush = DefaultFlush
	}

	if built.resync <= 0 {
		built.resync = DefaultResync
	}

	if built.startDeadline <= 0 {
		built.startDeadline = DefaultStartDeadline
	}

	if built.maxHold <= 0 {
		built.maxHold = DefaultMaxHold
	}

	if built.now == nil {
		built.now = time.Now
	}

	if built.log == nil {
		built.log = slog.New(slog.DiscardHandler)
	}

	if built.principal == nil {
		built.principal = func() string { return "" }
	}

	return built, nil
}

// Run registers the devices, then flushes and resyncs on their intervals
// until ctx ends. It returns an error only when the first listing fails. When
// it returns, every print of a fractal URL that has not started is dropped.
func (b *Bridge) Run(ctx context.Context) error {
	defer func() {
		b.mu.Lock()
		b.end()
		b.mu.Unlock()

		b.jobs.Wait()
	}()

	if err := b.Sync(ctx); err != nil {
		return err
	}

	flushing := time.NewTicker(b.flush)
	defer flushing.Stop()

	resyncing := time.NewTicker(b.resync)
	defer resyncing.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-flushing.C:
			b.Flush(ctx)
		case <-resyncing.C:
			if err := b.Sync(ctx); err != nil {
				b.log.WarnContext(ctx, "the device listing could not be read", "error", err.Error())
			}
		}
	}
}

// Sync reads the whole device listing from the host. A device missing from
// the listing keeps its interface instances, because Fractal has no message
// that removes one. Its properties are reported as null where null is allowed.
func (b *Bridge) Sync(ctx context.Context) error {
	seen := make(map[string]struct{})

	for offset := 0; ; {
		page, err := b.fleet.List(ctx, deviceapi.ListParams{
			Principal: "",
			Limit:     DefaultPage,
			Offset:    offset,
		})
		if err != nil {
			return fmt.Errorf("list the devices of this server: %w", err)
		}

		for i := range page.Items {
			seen[page.Items[i].ID] = struct{}{}
			b.observe(ctx, page.Items[i])
		}

		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			break
		}
	}

	b.forgetOthers(seen)

	return nil
}

// observe stores one device and marks it for the next flush. It skips a
// device whose identifier is not a valid Interface Instance ID.
func (b *Bridge) observe(ctx context.Context, device deviceapi.Device) {
	name, usable := InstanceName(device.ID)
	if !usable {
		b.log.WarnContext(ctx, "a device is not shown to the network because its identifier "+
			"is not an interface instance id", "device", device.ID)

		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if took, taken := b.named[name]; taken && took != device.ID {
		b.log.WarnContext(ctx, "a device is not shown to the network because another device "+
			"already took its name there", "device", device.ID, "name", name, "taken_by", took)

		return
	}

	entry, held := b.devices[device.ID]
	if !held {
		b.named[name] = device.ID
		entry = &deviceEntry{held: make(map[string]*instance)}
		b.devices[device.ID] = entry
	}

	// Fractal cannot mark an instance as stale, so the last state of a
	// printer that is away is dropped and it reads as BLOCKED with nulls.
	if !device.Connected {
		device.State = nil
	}

	entry.device = device
	entry.present = true
	entry.changed = fractal.TimestampOf(b.now())
	b.dirty[device.ID] = struct{}{}

	b.register(ctx, device.ID, entry)
}

// register registers the interface instances one device does not have yet. A
// head disabled later keeps its instance. The caller holds b.mu.
func (b *Bridge) register(ctx context.Context, id string, entry *deviceEntry) {
	for _, exposed := range b.mapping.interfaces {
		tools := []numbered{{number: 1}}
		if exposed.Per != "" {
			tools = toolsOf(exposed.Per, entry.device.State)
		}

		for _, one := range tools {
			key := instanceKey(exposed.Name, one.tool)
			if _, done := entry.held[key]; done {
				continue
			}

			made := &instance{iface: exposed, tool: one.tool}
			entry.held[key] = made
			entry.instances = append(entry.instances, made)

			handle, err := b.node.Register(fractalnode.Instance{
				Definition: exposed.Definition,
				Owner:      &owner{bridge: b, device: id, iface: exposed, tool: one.tool},
				Name:       exposed.Name,
				Instance:   exposed.InstanceID(id, one.number),
			})
			if err != nil {
				b.log.WarnContext(ctx, "an interface instance was refused",
					"device", id, "interface", string(exposed.Name), "tool", string(one.tool), "error", err.Error())

				continue
			}

			made.handle = handle
		}
	}
}

// forgetOthers marks as gone every device that is not in seen, the set of
// identifiers in the last listing.
func (b *Bridge) forgetOthers(seen map[string]struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for id := range b.devices {
		if _, still := seen[id]; still {
			continue
		}

		b.markGone(id)
	}
}

// markGone clears the state of one device and marks it for the next flush, so
// its properties are reported as null. The caller holds b.mu.
func (b *Bridge) markGone(id string) {
	entry, held := b.devices[id]
	if !held || !entry.present {
		return
	}

	entry.present = false
	entry.device.Connected = false
	entry.device.State = nil
	entry.changed = fractal.TimestampOf(b.now())
	b.dirty[id] = struct{}{}
}

// Sample stores one telemetry reading. The next Flush sends it.
//
// It drops a reading for a path no property reads, and one for a device that
// is unknown or offline. The Touch after a reconnect reads what was dropped.
func (b *Bridge) Sample(ctx context.Context, id, path string, value json.RawMessage, at time.Time) {
	if !b.mapping.WatchesPath(path) {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	entry, held := b.devices[id]
	if !held || !entry.device.Connected {
		return
	}

	if entry.device.State == nil {
		entry.device.State = make(map[string]json.RawMessage)
	}

	entry.device.State[path] = value

	if namesTools(path) {
		b.register(ctx, id, entry)
	}

	if stamp := fractal.TimestampOf(at); stamp > entry.changed {
		entry.changed = stamp
	}

	b.dirty[id] = struct{}{}
}

// Touch reads one device again from the host. The bridge calls it after an
// event about that device: it connected, it left, or its record changed. When
// the host has no such device, Touch marks it as gone.
func (b *Bridge) Touch(ctx context.Context, id string) {
	device, err := b.fleet.Get(ctx, deviceapi.GetParams{ID: id})
	if err != nil {
		if errors.Is(err, deviceapi.ErrNoDevice) {
			b.forget(id)

			return
		}

		b.log.WarnContext(ctx, "a device could not be read again", "device", id, "error", err.Error())

		return
	}

	b.observe(ctx, device)
}

// forget marks one device as gone. The node above then reads null for its
// properties, and not the last value the printer reported before it left.
func (b *Bridge) forget(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.markGone(id)
}

// Flush sends a state report for every device marked since the last Flush. A
// report has only the properties whose value changed, with the time of the
// newest reading.
func (b *Bridge) Flush(ctx context.Context) {
	for _, id := range b.takeDirty() {
		b.report(ctx, id)
	}
}

// takeDirty returns the identifiers of the marked devices and clears the marks.
func (b *Bridge) takeDirty() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.dirty) == 0 {
		return nil
	}

	out := make([]string, 0, len(b.dirty))
	for id := range b.dirty {
		out = append(out, id)
	}

	clear(b.dirty)

	return out
}

// report sends the changed properties of one device, one message per
// interface. It logs a report the node did not send and continues.
func (b *Bridge) report(ctx context.Context, id string) {
	b.reporting.Lock()
	defer b.reporting.Unlock()

	for _, change := range b.changesOf(id) {
		if err := change.handle.Report(ctx, change.props, change.at); err != nil {
			b.log.WarnContext(ctx, "a state report was not sent",
				"device", id, "interface", string(change.iface), "error", err.Error())
		}
	}
}

// change is one state report waiting to be sent.
type change struct {
	handle Reporter
	props  fractal.Props
	iface  fractal.InterfaceName
	at     fractal.Timestamp
}

// changesOf returns, per interface instance of one device, the properties
// that differ from the last report, and records them as reported.
func (b *Bridge) changesOf(id string) []change {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, held := b.devices[id]
	if !held {
		return nil
	}

	out := make([]change, 0, len(entry.instances))

	for _, one := range entry.instances {
		if one.handle == nil {
			continue
		}

		moved := one.moved(b.mapping.snapshotOf(one.iface, entry.device, one.tool))
		if len(moved) == 0 {
			continue
		}

		// Report raises a time earlier than the last report of the interface,
		// so the node above does not discard the values.
		out = append(out, change{
			handle: one.handle, props: moved, iface: one.iface.Name, at: entry.changed,
		})
	}

	return out
}

// moved returns the properties in current whose value differs from the last
// report of this instance, and records the new values as reported.
func (i *instance) moved(current fractal.Props) fractal.Props {
	if i.reported == nil {
		i.reported = make(fractal.Props, len(current))
	}

	moved := make(fractal.Props)

	for name, value := range current {
		if held, seen := i.reported[name]; seen && string(held) == string(value) {
			continue
		}

		i.reported[name] = value
		moved[name] = value
	}

	return moved
}

// snapshotOf returns every property of one interface instance. A property
// with no reading is null, or omitted when it cannot be null.
func (m *Mapping) snapshotOf(exposed *InterfaceMap, device deviceapi.Device, tool connectorapi.ToolID) fractal.Props {
	props := make(fractal.Props, exposed.Definition.NumProperties())
	inactive := len(exposed.ActiveIn) > 0 && !slices.Contains(exposed.ActiveIn, m.printerState(device))

	for _, mapped := range exposed.Properties {
		value, ok := mapped.read(device, tool)
		if inactive {
			value, ok = mapped.missing()
		}

		if ok {
			props[mapped.Name] = value
		}
	}

	for _, name := range exposed.AbsentProperties() {
		props[name] = rawNull
	}

	return props
}

// Snapshot returns every property of one interface of one device, for the
// full state report. A per-head interface answers for its first head. The
// time is zero, so the node stamps the report. It returns
// schema.ErrUnknownInterface or ErrNoDevice.
func (b *Bridge) Snapshot(
	id string, name fractal.InterfaceName,
) (fractal.Props, fractal.Timestamp, error) {
	exposed, ok := b.mapping.Interface(name)
	if !ok {
		return nil, 0, fmt.Errorf("%w: %s", schema.ErrUnknownInterface, name)
	}

	return b.snapshot(id, exposed, b.firstTool(id, exposed))
}

// firstTool returns the tool of the first instance of one interface the
// device has, and "" for an interface with one instance per device.
func (b *Bridge) firstTool(id string, exposed *InterfaceMap) connectorapi.ToolID {
	b.mu.Lock()
	defer b.mu.Unlock()

	if entry, held := b.devices[id]; held {
		for _, one := range entry.instances {
			if one.iface == exposed {
				return one.tool
			}
		}
	}

	return ""
}

// snapshot returns every property of one interface instance of one device.
func (b *Bridge) snapshot(
	id string, exposed *InterfaceMap, tool connectorapi.ToolID,
) (fractal.Props, fractal.Timestamp, error) {
	b.reporting.Lock()
	defer b.reporting.Unlock()

	b.mu.Lock()
	defer b.mu.Unlock()

	entry, held := b.devices[id]
	if !held {
		return nil, 0, fmt.Errorf("%w: %s", ErrNoDevice, id)
	}

	props := b.mapping.snapshotOf(exposed, entry.device, tool)

	// After a full report the node above has every value. Record them all as
	// reported, so the next flush sends only later changes.
	if one, registered := entry.held[instanceKey(exposed.Name, tool)]; registered {
		one.reported = maps.Clone(props)
	}

	return props, 0, nil
}

// Devices returns the identifier of every device the bridge registered, mapped
// to whether the host still lists the device. The caller owns the map.
func (b *Bridge) Devices() map[string]bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make(map[string]bool, len(b.devices))
	for id, entry := range b.devices {
		out[id] = entry.present
	}

	return out
}

// listed reports whether the host still lists one device.
func (b *Bridge) listed(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, held := b.devices[id]

	return held && entry.present
}

// device returns one device as the bridge last stored it, with a copy of its
// state, because Sample writes the stored state under b.mu.
func (b *Bridge) device(id string) (deviceapi.Device, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, held := b.devices[id]
	if !held {
		return deviceapi.Device{}, false
	}

	device := entry.device
	device.State = maps.Clone(entry.device.State)

	return device, true
}
