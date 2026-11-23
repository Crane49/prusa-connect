package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/transferapi"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
)

// ErrPlaneWedged reports a supervision plane that did not finish stopping
// inside the shutdown budget. The instances behind it are abandoned, because
// the alternative is a process an operator cannot stop.
var ErrPlaneWedged = errors.New("app: the supervision plane did not stop within its budget")

// plane is the running supervision plane and the bounded way to stop it.
type plane struct {
	host *resourcehost.Host
	// types is the table every compiled-in instance is built from. The
	// configuration store reads it to learn which of a type's settings are a
	// credential, so a read of an instance never carries one.
	types *registry.Registry
	feed  *sinkFeed
	// lists publishes a frame when the cameras a device lists change.
	lists *cameraLists
	// past is the history feed: the write side's reconcile loop and the read
	// side's per-verb resolver. They are one value because they answer from
	// one attached set.
	past *historyFeed
	// stopPast ends the history feed on its own, ahead of the instances it
	// writes into. See shutdown.
	stopPast context.CancelFunc
	stop     context.CancelFunc
	done     <-chan error

	// records is the state door: the resolver the module plane opens a
	// namespace through, and the accounting the API lists. It is one value
	// because the API lists what a namespace holds beside the instance that
	// claims it.
	records *recordDoor

	// fleets is the late binding a module's device.list is answered through.
	// It is filled once the fleet exists, which is after this plane started.
	fleets *moduleFleets

	// storages is the late binding a module's storage.read is answered
	// through. It is filled with the storage plane, which is built over this
	// plane and so cannot exist before it.
	storages *moduleStorages

	// cameraFrames is the late binding a module's camera.accept is answered
	// through, filled with the camera plane, which is built after this plane.
	cameraFrames *moduleCameras

	// accessors is the late binding a module's access.check is answered
	// through, filled with the authorization graph.
	accessors *moduleAccessors
	routes    *moduleRouteAccess
	people    *modulePeople

	// beside is the late binding of what the connectors mounted next to the
	// API. A module's root mount may not take those paths. It is filled once
	// the connectors are mounted, which is after this plane started and before
	// any module does.
	beside *besideMounts

	// admission is the late binding of which connectors hold a registration
	// table. A subscriber holding admit_device is offered those as origins.
	// The hub is built before the connectors, so the holder is filled once
	// they exist.
	admission *admissionOrigins

	// switchOffs carries the host's switch-offs to the resource store, which
	// is bound once it exists.
	switchOffs *switchOffs
}

// besideMounts holds the patterns mounted beside the API for the supervision
// plane to read at a module's start.
type besideMounts struct {
	patterns []string
}

// bind fills the holder. It runs once, before any module starts.
func (b *besideMounts) bind(patterns []string) { b.patterns = patterns }

// mounted is what resourcehost.Config.Beside takes.
func (b *besideMounts) mounted() []string { return b.patterns }

// startPlane builds the type registry and the supervision host, and puts the
// host's Run loop on a goroutine before anything can reconcile against it.
//
// The plane's context is detached from the caller's. Every supervised instance
// is started under it, so it has to outlive the listeners. Cancelling it is
// the last step of a shutdown.
func startPlane(
	ctx context.Context,
	logger *slog.Logger,
	hub *stream.Hub,
	metrics *observed,
	moduleData string,
	modulesOff, safe bool,
	switchOff install.SwitchOff,
	reaper *procgroup.Reaper,
) (*plane, error) {
	types, err := builtin.Registry()
	if err != nil {
		return nil, fmt.Errorf("build the resource type registry: %w", err)
	}

	logger.InfoContext(ctx, "resource types registered", "types", qualifiedNames(types))

	feed := newSinkFeed(hub, logger)
	lists := newCameraLists(hub, logger)
	past := newHistoryFeed(hub, types, logger)

	// The state door is built empty and handed to the host as a function, then
	// the host is written into it below. This breaks a cycle: the record
	// backend is a supervised instance of kind db, so the host needs the door
	// before the plane can build the backend. A deployment with no serving
	// backend resolves nothing, and a module calling state.get against it
	// reads CodeMethodNotFound.
	door := &recordDoor{}

	// The fleet a module reads is bound after this, because the fleet stages
	// uploads through this plane and so cannot exist before it. The holder
	// lets the plane be built first and still answer device.list.
	//
	// One refusal log serves both the storage and the print call paths, so a
	// module that is refused on both in turn still logs one line per interval.
	refusals := newRefusalLog(logger)

	fleets := &moduleFleets{log: refusals}
	storages := &moduleStorages{log: refusals}
	cameraFrames := &moduleCameras{log: refusals}
	accessors := &moduleAccessors{}
	routes := &moduleRouteAccess{}
	people := &modulePeople{}
	beside := &besideMounts{}
	offs := newSwitchOffs(logger)

	host, err := resourcehost.New(resourcehost.Config{
		Registry: types,
		HostKeys: builtin.HostKeys,
		Logger:   logger,
		Metrics:  metrics.binder(),
		OnStatus: announce(logger, hub, feed, lists, past, metrics, offs),
		Events:   instanceEmitter(hub, metrics.meters()),
		State:    door.records,
		Fleet:    fleets.forModule,
		Storage:  storages.forModule,
		Cameras:  cameraFrames.forModule,
		Access:   accessors.forModule,
		// The graph is bound later, as the accessors' is. Until then a call
		// in a person's name is refused.
		RouteAccess: routes,
		People:      people,
		Feed:        (&moduleFeeds{hub: hub}).forModule,
		Beside:      beside.mounted,
		// Beside the modules directory and never inside it: an update and a
		// removal delete a module's folder there.
		ModuleData: moduleData,
		FreeSpace:  availableDisk,
		ModulesOff: modulesOff,
		SafeMode:   safe,
		SwitchOff:  switchOff,
		Reaper:     reaper,
	})
	if err != nil {
		return nil, fmt.Errorf("build the supervision plane: %w", err)
	}

	door.host = host

	planeCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan error, 1)

	go func() { done <- host.Run(planeCtx) }()

	feed.start(planeCtx, host)
	lists.start(planeCtx, host)

	go offs.run(planeCtx)

	// The history feed runs under a context of its own, a child of the plane's.
	// Cancelling the plane still ends it. A shutdown ends it first and waits
	// for its last write while the store it writes into is still serving.
	// Under one context the feed's final flush would race the supervisor
	// closing the instance it flushes into.
	pastCtx, stopPast := context.WithCancel(planeCtx)

	past.start(pastCtx, host)

	return &plane{
		host:         host,
		types:        types,
		feed:         feed,
		lists:        lists,
		past:         past,
		stopPast:     stopPast,
		stop:         cancel,
		done:         done,
		records:      door,
		fleets:       fleets,
		storages:     storages,
		cameraFrames: cameraFrames,
		accessors:    accessors,
		routes:       routes,
		people:       people,
		beside:       beside,

		switchOffs: offs,
	}, nil
}

// historyStore is the read surface over whatever history instance is serving,
// resolved per request and not captured at boot.
//
// Because it resolves per request, a provider that is restarting reads the
// same as one that was never declared. The visibility reduction stays in the
// composition root. A provider never receives a principal, so something has to
// reduce a query to the origins its caller may read. internal/authz may not
// import a transport and an adapter may not import a policy.
func (p *plane) historyStore(graph graph) httpapi.HistoryResolver {
	return func() (httpapi.HistoryStore, error) {
		if p.past.attached() == 0 {
			return nil, httpfault.HistoryNotConfigured()
		}

		return historyReader{feed: p.past, seen: visibility{graph: graph}}, nil
	}
}

// transferRecords is how the device plane reaches the transfer projection: the
// same per-request resolution the history read surface takes, asking the
// provider that answers fact queries whether it keeps transfer rows.
//
// It is a probe and not a capability flag, so an adapter cannot claim a table
// it does not have. With an adapter that keeps no transfer rows, the device
// plane reports only the transfers that are running.
func (p *plane) transferRecords() func() (transferapi.Records, bool) {
	return transferRecordsOf(p.past)
}

// shutdown ends supervision and waits for it. The supervisor already bounds
// each instance's Close with its own StopTimeout; this bounds the sum of them,
// so a deployment with many instances cannot outlive the budget an operator
// configured.
func (p *plane) shutdown(ctx context.Context, logger *slog.Logger, budget time.Duration) error {
	// The history feed stops first and writes what it was holding, because it
	// writes into the instances stopped below. The listeners drained before
	// this ran, so this waits only for the store to finish the rows of the
	// last coalescing window.
	logger.InfoContext(ctx, "draining the history feed")

	p.stopPast()
	p.past.wait()

	logger.InfoContext(ctx, "stopping the supervised resources", "budget", budget.String())

	p.stop()

	timer := time.NewTimer(budget)
	defer timer.Stop()

	select {
	case err := <-p.done:
		// The sink feed closes its subscriptions here and not at hub.Close, so
		// a sink that is still draining a window cannot be handed a fact after
		// the instance behind it has been closed.
		p.awaitFeeds(ctx, logger, timer)
		logger.InfoContext(ctx, "supervision stopped")

		return err
	case <-timer.C:
		// The feeds are abandoned with the instances they write into. Both end
		// on the plane's context, which p.stop already cancelled, and waiting
		// for them here would spend a second budget after the first ran out.
		// Every module process is killed before the abandon, so a restart that
		// replaces this process starts no second copy beside one still running.
		p.host.KillChildren()

		logger.ErrorContext(ctx, "a supervised resource did not return; killing every module process and abandoning it",
			"budget", budget.String())

		return ErrPlaneWedged
	}
}

// awaitFeeds waits for the sink feed to close its subscriptions, and gives up
// when the shutdown budget runs out.
//
// The feed ended on the plane's context, which is already cancelled, so this
// waits for its last write. The timer is the one the caller started over the
// whole stop, so a feed still holding a write when the budget is gone is
// abandoned.
func (p *plane) awaitFeeds(ctx context.Context, logger *slog.Logger, timer *time.Timer) {
	closed := make(chan struct{})

	go func() {
		defer close(closed)

		p.feed.wait()
		p.lists.wait()
	}()

	select {
	case <-closed:
	case <-timer.C:
		logger.ErrorContext(ctx,
			"a feed did not close its subscriptions; abandoning it",
			"feed", "sinks")
	}
}

// transition is what a subscriber reads off an instance state change. It
// carries only the supervisor's own status, so the hook can publish it without
// taking a lock (see announce).
type transition struct {
	Resource   string `json:"resource"`
	Type       string `json:"type"`
	State      string `json:"state"`
	Result     string `json:"result"`
	Serving    bool   `json:"serving"`
	Attempt    int    `json:"attempt"`
	Generation int64  `json:"generation"`
}

// announce is the hook every instance's status change arrives on. It puts the
// instance lifecycle on the event bus and wakes the feeds that reconcile on
// it.
//
// It must not read anything behind the resource store's lock. The hook runs on
// whichever goroutine caused the change. For a stop that is the supervisor
// goroutine Host.stop is waiting on, while Store.Apply holds the store's lock
// for the whole reconcile. Taking that lock here would deadlock the three of
// them. Everything published is carried in the status it was handed, and
// Hub.Publish answers from bounded queues without blocking, so the hook
// returns whatever a subscriber is doing.
//
// The frame goes on the bus before any feed is woken. A feed woken first could
// subscribe the instance before the frame that reports the change is
// published, and would then receive the frame announcing its own attach.
func announce(
	logger *slog.Logger, hub *stream.Hub, feed *sinkFeed, lists *cameraLists, past *historyFeed,
	metrics *observed, offs *switchOffs,
) func(supervisor.Status) {
	return func(status supervisor.Status) {
		// pcl_resource_up is set from the status the plane publishes. The
		// supervisor's own metric handles are counters and a histogram, so this
		// gauge cannot come through the binder. This hook sees every state an
		// instance passes through.
		metrics.resourceUp(status.Ref.Kind().String(), status.Ref.ID(), status.Serving())

		logger.Info("resource state changed",
			"resource", status.Ref.String(),
			"type", status.Type,
			"state", status.State.String(),
			"result", status.Result.String(),
			"serving", status.Serving(),
			"attempt", status.Attempt)

		publish(logger, hub, metrics, status)

		feed.observe(status)
		lists.observe(status)
		past.observe(status)
		offs.observe(status)
	}
}

// switchOffSlots is how many switch-offs may wait for the store. Each is a
// module the supervisor gave up on, so more than a few at once is unlikely.
const switchOffSlots = 16

// switchOffs hands the supervisor's switch-offs to the resource store.
//
// The status hook cannot call the store itself: a stop's status arrives while
// Store.Apply holds the store's lock for its reconcile (see announce). The
// hook puts the record on a queue and run writes it from its own goroutine.
type switchOffs struct {
	log   *slog.Logger
	queue chan switchOff
	store chan *resource.Store
}

type switchOff struct {
	instance ref.Ref
	record   resource.SwitchOff
}

func newSwitchOffs(logger *slog.Logger) *switchOffs {
	return &switchOffs{
		log:   logger,
		queue: make(chan switchOff, switchOffSlots),
		store: make(chan *resource.Store, 1),
	}
}

// bind hands run the store. It is called once, after the store is built.
func (o *switchOffs) bind(store *resource.Store) { o.store <- store }

// observe queues a switch-off and never blocks the hook.
func (o *switchOffs) observe(status supervisor.Status) {
	if status.Result != supervisor.ResultSwitchedOffByHost {
		return
	}

	select {
	case o.queue <- switchOff{
		instance: status.Ref,
		record:   resource.SwitchOff{At: time.Now(), Stops: status.RecentStops},
	}:
	default:
		o.log.Error("a module the host switched off could not be recorded and starts again at the next start",
			"resource", status.Ref.String())
	}
}

// run writes every queued switch-off into the store until ctx ends.
func (o *switchOffs) run(ctx context.Context) {
	var store *resource.Store

	select {
	case store = <-o.store:
	case <-ctx.Done():
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case off := <-o.queue:
			if err := store.SwitchOffByHost(ctx, off.instance, off.record); err != nil {
				o.log.ErrorContext(ctx, "a module the host switched off could not be recorded",
					"resource", off.instance.String(), "error", err.Error())
			}
		}
	}
}

// publish puts one status change on the bus. A change that cannot be rendered
// or reaches no hub is logged and costs the feeds nothing: they reconcile from
// the supervision plane's own state and are woken whatever happened here.
func publish(logger *slog.Logger, hub *stream.Hub, metrics *observed, status supervisor.Status) {
	publication, err := transitionOf(status)
	if err != nil {
		logger.Warn("a resource state change could not be published",
			"resource", status.Ref.String(), "error", err.Error())

		return
	}

	if _, err = hub.Publish(publication); err != nil {
		// A hub that has shut down is the ordinary end of a process, so this
		// is logged at debug.
		logger.Debug("a resource state change reached no hub",
			"resource", status.Ref.String(), "error", err.Error())

		return
	}

	metrics.meters().published(publication.Type)
}

// transitionOf renders one status as a publication. It is a state event and
// not telemetry: the next change does not supersede an instance going out of
// service, so a reader must not miss it.
func transitionOf(status supervisor.Status) (stream.Publication, error) {
	origin, err := event.NewOrigin(event.Kind(status.Ref.Kind()), status.Ref.ID())
	if err != nil {
		return stream.Publication{}, fmt.Errorf("render %s as an origin: %w", status.Ref, err)
	}

	data, err := json.Marshal(transition{
		Resource:   status.Ref.String(),
		Type:       status.Type,
		State:      status.State.String(),
		Result:     status.Result.String(),
		Serving:    status.Serving(),
		Attempt:    status.Attempt,
		Generation: status.Generation,
	})
	if err != nil {
		return stream.Publication{}, fmt.Errorf("encode the transition of %s: %w", status.Ref, err)
	}

	return stream.Publication{
		Origin: origin,
		Type:   event.TypeResourceStateChanged,
		Class:  event.ClassState,
		Data:   data,
	}, nil
}

// qualifiedNames renders the registered types for the startup line, so an
// operator who wrote an unknown type name can see what this build has.
func qualifiedNames(types *registry.Registry) []string {
	all := types.All()
	names := make([]string, 0, len(all))

	for _, declared := range all {
		names = append(names, declared.Kind().String()+"/"+declared.Qualified())
	}

	return names
}
