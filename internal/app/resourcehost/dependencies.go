package resourcehost

import (
	"sync"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// The start order of modules that require each other, and what happens to
// dependants. A module is launched before its dependants. A dependant waits
// inside its own start for a module that is still starting. When a module
// stops serving, its dependants are restarted and wait for it. When it serves
// again, they are retried at once.

// statusSignal wakes every start that waits for a provider. Each status
// change closes the channel the waiters hold, and the next waiter takes a new
// one. A waiter takes its channel before it reads the states, so it misses no
// change between reading and waiting.
type statusSignal struct {
	mu sync.Mutex
	ch chan struct{}
}

// next is the channel closed at the next status change.
func (s *statusSignal) next() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ch == nil {
		s.ch = make(chan struct{})
	}

	return s.ch
}

// broadcast closes the channel the waiters hold.
func (s *statusSignal) broadcast() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ch != nil {
		close(s.ch)
		s.ch = nil
	}
}

// onStatusOf is the status callback one instance's supervisor is given. It
// passes every change to Config.OnStatus, wakes the starts that wait for a
// module, and restarts the dependants of a module that started or stopped
// serving.
func (h *Host) onStatusOf(declared resource.Declaration) func(supervisor.Status) {
	return func(status supervisor.Status) {
		if h.cfg.OnStatus != nil {
			h.cfg.OnStatus(status)
		}

		h.moved.broadcast()
		h.propagate(declared, status.State.Serving())
	}
}

// propagate restarts the dependants of one module when it starts or stops
// serving.
func (h *Host) propagate(declared resource.Declaration, serving bool) {
	h.mu.Lock()

	// The declaration in force, which a restart may have changed since the
	// supervisor was built. An instance already stopped keeps the one it ran.
	if running, ok := h.running[declared.Ref]; ok {
		declared = running.declaration
	}

	namespace, isModule := declared.ModuleNamespace()
	if !isModule {
		h.mu.Unlock()

		return
	}

	was := h.serving[declared.Ref]
	h.serving[declared.Ref] = serving
	dependants := h.dependantsOf(declared.Ref, namespace)
	h.mu.Unlock()

	if was == serving {
		return
	}

	for _, dependant := range dependants {
		if serving {
			h.retryWaiting(dependant)

			continue
		}

		h.takeDown(dependant, namespace)
	}
}

// heldDependant is one running instance that requires a module.
type heldDependant struct {
	instance ref.Ref
	runner   *supervisor.Instance[any]
}

// dependantsOf is every running instance that requires the module under a
// namespace. The caller holds h.mu.
func (h *Host) dependantsOf(module ref.Ref, namespace string) []heldDependant {
	var out []heldDependant

	for instance, running := range h.running {
		if instance == module || running.declaration.Spec.Module == nil {
			continue
		}

		for _, required := range running.declaration.Spec.Module.Requires {
			if required.Module == namespace {
				out = append(out, heldDependant{instance: instance, runner: running.runner})

				break
			}
		}
	}

	return out
}

// takeDown restarts a dependant that is serving or starting. The restart runs
// the start again. The start finds that the required module is down, so the
// instance waits in retry with DependenciesSatisfied false.
func (h *Host) takeDown(dependant heldDependant, namespace string) {
	state := dependant.runner.State()
	if !state.Serving() && state != condition.StateStarting() {
		return
	}

	h.cfg.Logger.Warn("a module was stopped because a module it requires stopped serving",
		"resource", dependant.instance.String(), "requires", namespace)
	dependant.runner.Restart()
}

// retryWaiting restarts a dependant that waits in retry for a module it
// requires, so it does not wait for the end of its backoff when the module
// serves again.
func (h *Host) retryWaiting(dependant heldDependant) {
	status := dependant.runner.Status()
	if status.State.Serving() || status.State == condition.StateStarting() {
		return
	}

	for _, held := range status.Conditions {
		if held.Type == condition.TypeDependenciesSatisfied && held.Status == condition.StatusFalse &&
			held.Reason == reasonDependencyMissing {
			dependant.runner.Restart()

			return
		}
	}
}

// ordered is the plan's steps with every start placed after the starts of
// the modules it requires, so a reconcile launches the modules in layers.
// Within a layer the steps keep the plan's order, and a stop is in the first
// layer.
func ordered(plan resource.Plan, desired []resource.Declaration) []resource.Step {
	layers := layersOf(desired)
	deepest := 0

	for _, step := range plan.Steps {
		deepest = max(deepest, layerOf(step, layers))
	}

	steps := make([]resource.Step, 0, len(plan.Steps))

	for layer := 0; layer <= deepest; layer++ {
		for _, step := range plan.Steps {
			if layerOf(step, layers) == layer {
				steps = append(steps, step)
			}
		}
	}

	return steps
}

// layerOf is how deep a step's instance sits, and zero for a step that stops
// it.
func layerOf(step resource.Step, layers map[ref.Ref]int) int {
	if step.Outcome == resource.OutcomeStop {
		return 0
	}

	return layers[step.Ref]
}
