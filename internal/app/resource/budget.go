package resource

import (
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

const (
	mib = 1 << 20
	// DefaultBudget is the memory budget of a service module whose author
	// recommends none and whose operator set none.
	DefaultBudget = 128 * mib
	// minReserve is the least memory a detected pool leaves to the server and
	// the system. On a small board a quarter of the memory is less than the
	// server itself uses.
	minReserve = 256 * mib
	// reserveShare sets the part of the machine's memory left to the server
	// and the system: the memory divided by this number.
	reserveShare = 4
)

// BudgetSource is who set a module's memory budget.
type BudgetSource string

// The three sources. Each one overrides the one before it.
const (
	BudgetDefault  BudgetSource = "default"
	BudgetAuthor   BudgetSource = "author"
	BudgetOperator BudgetSource = "operator"
)

// Budget is the memory a service module's process may use, and who set it.
type Budget struct {
	Bytes  int64
	Source BudgetSource
}

// BudgetOf is the module's budget: the operator's memory_mib, else the
// author's recommendation, else DefaultBudget. It is false for an instance
// that runs no process of its own.
func BudgetOf(spec Spec) (Budget, bool) {
	if spec.Runtime != install.RuntimeService {
		return Budget{}, false
	}

	if spec.Service != nil && spec.Service.MemoryMiB > 0 {
		return Budget{Bytes: int64(spec.Service.MemoryMiB) * mib, Source: BudgetOperator}, true
	}

	if spec.Module != nil && spec.Module.Asked != nil && spec.Module.Asked.MemoryMiB > 0 {
		return Budget{Bytes: int64(spec.Module.Asked.MemoryMiB) * mib, Source: BudgetAuthor}, true
	}

	return Budget{Bytes: DefaultBudget, Source: BudgetDefault}, true
}

// SwitchOffPolicy is the switch-off in force for a service module, and who set
// it: BudgetOperator or BudgetDefault.
type SwitchOffPolicy struct {
	Failures int
	Window   time.Duration
	Source   BudgetSource
}

// SwitchOffOf is the module's switch-off: each value the operator set on the
// spec, else the server's, else the install defaults. It is false for an
// instance that runs no process of its own.
func SwitchOffOf(spec Spec, server install.SwitchOff) (SwitchOffPolicy, bool) {
	if spec.Runtime != install.RuntimeService {
		return SwitchOffPolicy{}, false
	}

	operator := spec.switchOff()
	in := operator.Over(server).Over(install.SwitchOff{
		Failures: new(install.DefaultSwitchOffFailures),
		Window:   install.DefaultSwitchOffWindow,
	})

	source := BudgetDefault
	if !operator.IsZero() {
		source = BudgetOperator
	}

	return SwitchOffPolicy{Failures: *in.Failures, Window: in.Window, Source: source}, true
}

// Pool is the memory the budgets of all enabled service modules are meant to
// fit in together. Known is false when the operator set none and the machine's
// memory could not be read. The pool is reported with the sum of the budgets,
// and no module is refused because of it.
type Pool struct {
	Bytes int64
	Known bool
	// Configured is true for a pool the operator set with core.modules_memory.
	Configured bool
	// Reserve is what a detected pool left to the server and the system, and
	// zero for one the operator set.
	Reserve int64
}

// PoolOf is the operator's core.modules_memory when it is set, and otherwise
// the machine's memory less a reserve of a quarter of it, at least 256 MiB.
func PoolOf(configured int64, detect func() (int64, bool)) Pool {
	if configured > 0 {
		return Pool{Bytes: configured, Known: true, Configured: true}
	}

	total, known := detect()
	if !known {
		return Pool{}
	}

	reserve := max(minReserve, total/reserveShare)

	return Pool{Bytes: max(0, total-reserve), Known: true, Reserve: reserve}
}

// PoolUse is the memory promised to the enabled service modules, as the sum
// of their budgets, and the pool it is promised from.
func (s *Store) PoolUse() (promised, pool Pool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return Pool{Bytes: promisedBy(s.withoutHeld(s.current), ref.Ref{}), Known: true}, s.pool
}

// ModuleMemory is the pool and the memory promised from it. It makes no
// permission check: the route that serves it requires the permission to
// administer modules.
func (s *Store) ModuleMemory() httpapi.ModuleMemoryPool {
	promised, pool := s.PoolUse()
	memory := httpapi.ModuleMemoryPool{CommittedBytes: promised.Bytes}

	if !pool.Known {
		return memory
	}

	memory.PoolBytes = &pool.Bytes
	memory.Source = "detected"

	if pool.Configured {
		memory.Source = "configured"
	}

	return memory
}

// promisedBy sums the budgets of the enabled service modules in doc, leaving
// out except.
func promisedBy(doc Document, except ref.Ref) int64 {
	var promised int64

	for _, declared := range doc.Instances {
		if declared.Ref == except || !declared.Spec.Enabled {
			continue
		}

		if budget, isService := BudgetOf(declared.Spec); isService {
			promised += budget.Bytes
		}
	}

	return promised
}

// withBudget writes a service module's budget into its projection: the
// operator's memory_mib, on_over_memory and switch_off on the spec, and the
// budget, the action and the switch-off in force on the status. server is the
// server's core.modules_switch_off. The plane's memory record is copied
// before it is changed.
func withBudget(instance *httpapi.ResourceInstance, spec Spec, server install.SwitchOff) {
	budget, isService := BudgetOf(spec)
	if !isService {
		return
	}

	if spec.Service != nil {
		instance.MemoryMiB = spec.Service.MemoryMiB
		instance.OnOverMemory = spec.Service.OnOverMemory
		instance.SwitchOff = switchOffWire(spec.Service.SwitchOff)
	}

	var memory httpapi.ResourceMemory
	if instance.Status.Memory != nil {
		memory = *instance.Status.Memory
	}

	memory.BudgetBytes = budget.Bytes
	memory.BudgetSource = string(budget.Source)
	memory.OnOverMemory = spec.OnOverMemory()

	switchOff, _ := SwitchOffOf(spec, server)
	memory.SwitchOffFailures = switchOff.Failures
	memory.SwitchOffWindowSeconds = int(switchOff.Window / time.Second)
	memory.SwitchOffSource = string(switchOff.Source)
	instance.Status.Memory = &memory
}

// switchOffWire is the operator's switch-off as the spec carries it, and nil
// when the operator set none.
func switchOffWire(operator install.SwitchOff) *httpapi.ResourceSwitchOffPolicy {
	if operator.IsZero() {
		return nil
	}

	return &httpapi.ResourceSwitchOffPolicy{
		Failures:      operator.Failures,
		WindowSeconds: int(operator.Window / time.Second),
	}
}
