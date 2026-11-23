package resourcehost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/extension/script"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// errUnknownOutcome reports a plan carrying an outcome this build does not know.
var errUnknownOutcome = errors.New("resourcehost: unknown reconcile outcome")

// instance is one supervised resource and the declaration it was last
// reconciled to.
//
// spec is a pointer the constructor reads on every start. A config change is
// therefore a supervisor restart and not a new instance: the resource is
// rebuilt from the declaration in force when it is rebuilt, and the attempt
// counter, the drop count and the metric series continue across the restart.
//
// declaration and generation belong to the host, and every read and write of
// them holds Host.mu: a plan is carried out on the goroutine Run drives while
// the readiness watcher and the HTTP surface are reading. The four atomic
// pointers are read by the supervisor's own goroutine at a start, so they are
// atomics and not fields under that lock. runner, cancel and stopped are
// written once, before the record is put into Host.running, and only read
// afterwards.
type instance struct {
	runner      *supervisor.Instance[any]
	spec        atomic.Pointer[registry.Spec]
	script      atomic.Pointer[scriptSpec]
	service     atomic.Pointer[serviceSpec]
	asset       atomic.Pointer[assetSpec]
	declaration resource.Declaration
	generation  int64
	cancel      context.CancelFunc
	stopped     chan struct{}
}

// scriptSpec is the declaration of an instance whose runtime is script: the
// expression itself, and the destinations it may name. It is separate from
// spec because every registered type is built from registry.Spec, and a script
// has no registered type.
type scriptSpec struct {
	source string
	config json.RawMessage
}

// adopt records the declaration an instance is built from next. All four
// parts are stored together, so a restart cannot rebuild a script instance
// from one declaration's source and another's settings, or launch one
// declaration's artefact against another's checksum.
func (i *instance) adopt(declared resource.Declaration) {
	i.spec.Store(specOf(declared))
	i.script.Store(&scriptSpec{source: declared.Spec.Script, config: declared.Spec.Config})
	i.service.Store(serviceOf(declared))
	i.asset.Store(assetOf(declared))
}

// Instance returns the supervised instance behind one name, for a caller that
// routes work through it.
func (h *Host) Instance(instance ref.Ref) (*supervisor.Instance[any], bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	running, ok := h.running[instance]
	if !ok {
		return nil, false
	}

	return running.runner, true
}

// Declared returns the declaration the host holds for one instance.
//
// It exists for the read verbs a kind resolves per query. An instance's
// `reads:` list is a host key and not a setting inside its opaque config block,
// so the code that resolves the query reads the list from the declaration.
// Everything else about an instance is reached through its supervisor.
func (h *Host) Declared(instance ref.Ref) (resource.Declaration, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	running, ok := h.running[instance]
	if !ok {
		return resource.Declaration{}, false
	}

	return running.declaration, true
}

// MemoryHistory is what the instance's process used since the instant, and the
// failed stops inside the same window. It reports false for an instance that
// has no memory watch.
func (h *Host) MemoryHistory(instance ref.Ref, since time.Time) (httpapi.ResourceMemoryHistory, bool) {
	h.mu.Lock()
	watch := h.memory[instance]
	running := h.running[instance]
	h.mu.Unlock()

	if watch == nil {
		return httpapi.ResourceMemoryHistory{}, false
	}

	history := httpapi.ResourceMemoryHistory{Items: watch.points(since), Stops: []httpapi.ResourceStop{}}

	if running != nil {
		for _, stop := range running.runner.Status().RecentStops {
			if !stop.At.Before(since) {
				history.Stops = append(history.Stops, resource.StopToWire(stop))
			}
		}
	}

	return history, true
}

// Log is the standard error kept of the instance, which survives a stop.
func (h *Host) Log(instance ref.Ref) (httpapi.ResourceLog, bool) {
	h.mu.Lock()
	watch := h.memory[instance]
	h.mu.Unlock()

	if watch == nil {
		return httpapi.ResourceLog{}, false
	}

	return httpapi.ResourceLog{Lines: stderrLines(watch.log)}, true
}

// Restart rebuilds one instance from the declaration the host holds for it, and
// reports false when it is supervising nothing under that name.
//
// It asks the supervisor and reconstructs nothing itself, so a restart an
// operator asked for and a restart a configuration change caused go through
// one state machine. The declaration is not changed. The instance is rebuilt
// from the merged configuration in force, which may differ from the one it was
// last built from.
func (h *Host) Restart(instance ref.Ref) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	running, ok := h.running[instance]
	if !ok {
		return false
	}

	running.runner.Restart()

	return true
}

// CameraTypes is the camera types the installed modules offer, as the last
// reconcile declared them (ADR-20261106-km2).
func (h *Host) CameraTypes() map[string]resource.ModuleCameraType {
	h.mu.Lock()
	defer h.mu.Unlock()

	return maps.Clone(h.cameraTypes)
}

// Supervising returns every instance of one kind the host is running.
//
// A caller that routes work to a kind compares this with what it holds, so a
// status change it missed does no harm. A caller that watched transitions
// would, after one missed change, leave a resource unattached for the life of
// the process.
func (h *Host) Supervising(kind ref.Kind) map[ref.Ref]*supervisor.Instance[any] {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make(map[ref.Ref]*supervisor.Instance[any])

	for instance, running := range h.running {
		if instance.Kind() == kind {
			out[instance] = running.runner
		}
	}

	return out
}

// heldWhy is the log line for a module that ModulesOff kept from starting.
func (h *Host) heldWhy() string {
	if h.cfg.SafeMode {
		return "a module was not started, because the server runs in safe mode"
	}

	return "an installed module was not started, because modules are switched off"
}

// start constructs and supervises one instance. The goroutine it starts ends
// when the instance's context is cancelled: at the host's shutdown, or when
// stop cancels the instance.
//
// An instance already under supervision is stopped first. Two supervision loops
// over one name would each close a target the other built. A plan that asks for
// a start of a name the host already has was made from an old view of the
// host, and it is carried out, not refused.
func (h *Host) start(ctx context.Context, declared resource.Declaration) error {
	h.stop(declared.Ref)

	if h.cfg.ModulesOff && (declared.Installed() || h.cfg.SafeMode && declared.Module()) {
		h.cfg.Logger.WarnContext(ctx, h.heldWhy(), "resource", declared.Ref.String())

		return nil
	}

	// Before anything is constructed, because a second module under one
	// namespace must not launch a process whose routes nothing can reach. The
	// refusal belongs to this instance: it is reported against this step and
	// the rest of the plan is still carried out.
	h.mu.Lock()
	claimErr := h.claimNamespace(declared)
	if claimErr == nil {
		claimErr = h.claimMount(declared)
	}
	h.mu.Unlock()

	if claimErr != nil {
		return claimErr
	}

	h.sweepStaged(ctx, declared)

	running := &instance{declaration: declared, generation: declared.Generation}
	running.adopt(declared)

	watch := h.watchMemory(declared)

	policy := h.policyFor(declared)
	mode := declared.Spec.Runtime

	supervised, err := supervisor.New(supervisor.Config[any]{
		Ref:    declared.Ref,
		UID:    declared.UID,
		Type:   declared.Spec.Type,
		Policy: policy,
		Start: func(startCtx context.Context, handle supervisor.Handle) (any, error) {
			switch mode {
			case install.RuntimeScript:
				return buildScript(handle, running.script.Load(), h.emitterFor(declared.Ref))
			case install.RuntimeService:
				serves, statErr := h.servesTo(declared)
				if statErr != nil {
					return nil, statErr
				}

				spec := running.service.Load()

				// Before the artefact is found and the process launched. A
				// module whose requirements are not met would answer a refusal
				// to every call it received, so it does not run at all.
				if depErr := h.dependenciesMet(startCtx, declared, spec, serves); depErr != nil {
					return nil, depErr
				}

				grants, grantErr := grantsOf(h.cfg.ModuleData, declared.Ref, spec)
				if grantErr != nil {
					return nil, grantErr
				}

				return buildService(
					startCtx, handle, declared.Ref, spec, policy.StopTimeout, serves, mountPath(declared, spec), grants,
					watch, h.cfg.Reaper, running.runner.Status, h.cfg.hang, &h.children)
			case install.RuntimeAsset:
				return buildAsset(startCtx, handle, declared.Ref, running.asset.Load())
			case install.RuntimeBuiltin:
			}

			return h.buildBuiltin(startCtx, handle, declared.Ref, *running.spec.Load())
		},
		Stop:     closeTarget,
		Logger:   h.cfg.Logger,
		Metrics:  h.cfg.Metrics,
		OnStatus: h.onStatusOf(declared),
	})
	if err != nil {
		h.mu.Lock()
		h.releaseMount(declared.Ref)
		h.mu.Unlock()

		return fmt.Errorf("supervise %s: %w", declared.Ref, err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	running.runner = supervised
	running.cancel = cancel
	running.stopped = make(chan struct{})

	h.mu.Lock()
	h.running[declared.Ref] = running
	h.mu.Unlock()

	go func() {
		defer close(running.stopped)

		if runErr := supervised.Run(runCtx); runErr != nil {
			h.cfg.Logger.ErrorContext(runCtx, "supervision ended with an error",
				"resource", declared.Ref.String(), "error", runErr.Error())
		}
	}()

	return nil
}

// watchMemory is the memory watch of a service instance, made the first time
// it starts, and nil for an instance that runs no process of its own.
func (h *Host) watchMemory(declared resource.Declaration) *memoryWatch {
	budget, isService := resource.BudgetOf(declared.Spec)
	if !isService {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// An existing watch takes the budget of the declaration being started,
	// here and not inside the start: an edit that arrives while a start is
	// launching must not be overwritten by the budget it began with.
	watch, ok := h.memory[declared.Ref]
	if !ok {
		watch = newMemoryWatch(budget.Bytes)
		h.memory[declared.Ref] = watch
	}

	watch.setBudget(budget.Bytes)
	watch.setMode(declared.Spec.OnOverMemory())

	return watch
}

// buildBuiltin constructs one instance of a type compiled into this binary.
func (h *Host) buildBuiltin(
	ctx context.Context, handle supervisor.Handle, instance ref.Ref, spec registry.Spec,
) (any, error) {
	// A camera of a type an installed module offers is built as the core's
	// generic camera (ADR-20261106-km2). The declaration keeps the module's
	// type, and no module code runs here.
	h.mu.Lock()
	spec.Type = resource.DriverType(instance.Kind(), spec.Type, h.cameraTypes)
	h.mu.Unlock()

	// Before the settings are checked, because a block can only be checked
	// against the keys of a type this build has.
	if typeErr := knownType(h.cfg.Registry, instance, spec.Type); typeErr != nil {
		return nil, typeErr
	}

	// Before the type is built, so a key the type would not read is refused
	// by name and is not silently ignored.
	if settingsErr := builtinSettings(
		h.cfg.Registry, h.hostKeys(instance.Kind()), instance.Kind(), spec.Type, spec.Config,
	); settingsErr != nil {
		return nil, settingsFailure(instance, settingsErr)
	}

	return registry.Construct[any](ctx, h.cfg.Registry, instance.Kind(), handle, spec)
}

// knownType refuses a type name the registry has no implementation for, with
// the code that keeps the instance in failed. A failure from Construct is
// recoverable and would be retried, so the registry is asked here first: a
// retry never adds a type this build does not have. The registry's error is
// not wrapped, because its text would repeat this message in the instance's
// status.
func knownType(types *registry.Registry, instance ref.Ref, typeName string) error {
	_, err := types.Lookup(instance.Kind(), typeName)
	if err == nil || !errors.Is(err, registry.ErrUnknownType) {
		return nil
	}

	have := "none"
	if registered := types.Types(instance.Kind()); len(registered) > 0 {
		names := make([]string, 0, len(registered))
		for _, t := range registered {
			names = append(names, t.Qualified())
		}

		have = strings.Join(names, ", ")
	}

	return typeUnknown.New(fmt.Sprintf("%s names type %q, and this build has no %s type of that name. It has %s",
		instance, typeName, instance.Kind(), have)).
		With(
			fault.StringParam("resource", instance.String()),
			fault.StringParam("kind", instance.Kind().String()),
			fault.StringParam("type", typeName),
		)
}

// hostKeys is the settings of one kind that this host reads and the type does
// not, and none for a host that was given no such list.
func (h *Host) hostKeys(kind ref.Kind) []extension.Option {
	if h.cfg.HostKeys == nil {
		return nil
	}

	return h.cfg.HostKeys(kind)
}

// mountPath is where a module's routes are served, for its document to be
// rebased under: the root prefix the operator named, or the namespace group. It
// is empty for a service instance that is not a module. The service spec is the
// one in force at this start, so a restart after the operator moved the mount
// rebases under the new one.
func mountPath(declared resource.Declaration, spec *serviceSpec) string {
	namespace, isModule := moduleNamespaceOf(declared)
	if !isModule {
		return ""
	}

	if spec != nil && spec.service.Mount != "" {
		return spec.service.Mount
	}

	return httpapi.ModuleMount(namespace)
}

// policyFor is the host policy for one instance: the kind's default, or the
// caller's choice. The operator's required setting is applied over either,
// because the operator sets required and the host does not. A service module's
// switch-off is applied too, because the operator or the server sets it.
func (h *Host) policyFor(declared resource.Declaration) supervisor.Policy {
	policy := defaultPolicy(declared.Ref.Kind())
	if h.cfg.Policy != nil {
		policy = h.cfg.Policy(declared.Ref.Kind(), declared)
	}

	policy.Required = declared.Spec.Required
	h.withSwitchOff(&policy, declared)

	return policy
}

// withSwitchOff sets a service module's failure limit and window from the
// switch-off in force. Zero failures is no limit, so the module is never
// switched off.
func (h *Host) withSwitchOff(policy *supervisor.Policy, declared resource.Declaration) {
	if switchOff, isService := resource.SwitchOffOf(declared.Spec, h.cfg.SwitchOff); isService {
		policy.FailureLimit = switchOff.Failures
		policy.FailureWindow = switchOff.Window
	}
}

// buildScript compiles one script instance. A script instance is not resolved
// through the type table: a script is its own implementation, so there is no
// registered name to look up.
//
// A source that does not compile is returned as an error. The supervisor
// records it as the instance's failed result, with the runtime's own message
// and caret. One bad expression fails only its own instance.
func buildScript(handle supervisor.Handle, declared *scriptSpec, emitter bus.Emitter) (any, error) {
	if declared == nil {
		return nil, fmt.Errorf("%w: the instance carries no script", script.ErrInvalidSource)
	}

	return script.NewSink(declared.source, declared.config, handle.Logger, emitter)
}

// emitterFor is the local event stream one instance may publish on, or nil when
// this host offers none. It is read at every start, so an instance rebuilt by a
// configuration change is bound to the bus the host holds at that start.
func (h *Host) emitterFor(instance ref.Ref) bus.Emitter {
	if h.cfg.Events == nil {
		return nil
	}

	return h.cfg.Events(instance)
}

// specOf is the declaration as the registry takes it. The settings block is
// passed unchanged: the core stores and returns it, and only the
// implementation named by type reads inside it.
func specOf(declared resource.Declaration) *registry.Spec {
	return &registry.Spec{
		Ref:    declared.Ref,
		UID:    declared.UID,
		Type:   declared.Spec.Type,
		Config: declared.Spec.Config,
	}
}

// projectStatus maps the supervision plane's status onto the wire shape. The
// mapping is here because the HTTP transport may not import the extension
// packages, and this package may import both.
//
// ObservedGeneration is the declaration generation this host last acted on, not
// the supervisor's construction counter. On the wire, a value lower than the
// instance's generation means the change has not taken effect yet.
func projectStatus(status supervisor.Status, generation int64) httpapi.ResourceStatus {
	out := httpapi.ResourceStatus{
		State:              status.State.String(),
		Serving:            status.Serving(),
		ObservedGeneration: generation,
		Attempt:            status.Attempt,
		Dropped:            status.Dropped,
		Conditions:         make([]httpapi.ResourceCondition, 0, len(status.Conditions)),
	}

	if status.Result != supervisor.ResultSuccess {
		out.Result = status.Result.String()
	}

	if !status.NextRetryAt.IsZero() {
		next := status.NextRetryAt
		out.NextRetryAt = &next
	}

	for _, held := range status.Conditions {
		out.Conditions = append(out.Conditions, httpapi.NewResourceCondition(held))
	}

	// Only a service module has a failure window, so only a service module
	// has recent stops. The memory record tells a person why its process
	// stopped.
	if stops := len(status.RecentStops); stops > 0 {
		last := resource.StopToWire(status.RecentStops[stops-1])
		out.Memory = &httpapi.ResourceMemory{FailuresInWindow: stops, LastStop: &last}
	}

	return out
}
