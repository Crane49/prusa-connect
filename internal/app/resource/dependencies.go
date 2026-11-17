package resource

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// A module depends on other modules as a Composer package depends on other
// packages. The functions below read every installed module and check each
// dependency against the module it names: installed, switched on, and with a
// release in the range. A change that leaves a dependency unmet is refused.
// The install, in internal/app, adds the missing modules.

// Resolution is every installed module with the state of each of its
// dependencies. Modules are in type order.
type Resolution struct {
	Modules []ResolvedModule
}

// ResolvedModule is one installed module and what the resolver found for it.
type ResolvedModule struct {
	Instance  ref.Ref
	Type      string
	Namespace string
	Path      string
	Version   string
	Source    string
	Digest    string
	Enabled   bool
	Pulled    bool
	Requires  []ResolvedDependency
	// Conflicts is the conflict entries. Met is false for one whose module
	// is installed with a release the constraint holds.
	Conflicts []ResolvedDependency
	Suggests  []install.Suggestion
}

// ResolvedDependency is one dependency and the module it names, as this
// server has it. Instance is zero and Installed empty when the module is not
// installed.
type ResolvedDependency struct {
	install.Dependency

	Instance  ref.Ref
	Type      string
	Installed string
	Enabled   bool
	// Met is true for a requirement on an enabled module with a release in
	// range, and for a conflict whose module is absent or out of its
	// constraint.
	Met bool
}

// Resolve reads the installed modules among the declarations and checks each
// dependency against the module it names.
func Resolve(declared []Declaration) Resolution {
	modules := make([]ResolvedModule, 0, len(declared))

	for _, one := range declared {
		namespace, isModule := one.ModuleNamespace()
		if !isModule || one.Spec.Module == nil {
			continue
		}

		entry := one.Spec.Module
		held := ResolvedModule{
			Instance: one.Ref, Type: one.Spec.Type, Namespace: namespace, Path: entry.Path,
			Version: entry.Version, Source: entry.Source, Digest: entry.Digest,
			Enabled: one.Spec.Enabled, Pulled: entry.Pulled, Suggests: entry.Suggests,
		}

		for _, required := range entry.Requires {
			if required.Module != "" {
				held.Requires = append(held.Requires, ResolvedDependency{Dependency: required.Dependency()})
			}
		}

		for _, conflict := range entry.Conflicts {
			held.Conflicts = append(held.Conflicts, ResolvedDependency{Dependency: conflict})
		}

		modules = append(modules, held)
	}

	// sort.Slice and not slices.SortFunc: the generic sort is compiled once
	// per element shape, and this one would add its own copy to every build.
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].Type != modules[j].Type {
			return modules[i].Type < modules[j].Type
		}

		return strings.Compare(modules[i].Instance.String(), modules[j].Instance.String()) < 0
	})

	for index := range modules {
		for at := range modules[index].Requires {
			modules[index].Requires[at] = against(modules, index, modules[index].Requires[at].Dependency, true)
		}

		for at := range modules[index].Conflicts {
			modules[index].Conflicts[at] = conflictWith(modules, index, modules[index].Conflicts[at].Dependency)
		}
	}

	return Resolution{Modules: modules}
}

// against is one requirement checked against the module it names, other
// than the one at index.
func against(modules []ResolvedModule, index int, dependency install.Dependency, _ bool) ResolvedDependency {
	out := ResolvedDependency{Dependency: dependency}

	for at, candidate := range modules {
		if at == index || candidate.Namespace != dependency.Module {
			continue
		}

		out.Instance, out.Type, out.Installed, out.Enabled = candidate.Instance, candidate.Type,
			candidate.Version, candidate.Enabled
		out.Met = candidate.Enabled && dependency.Holds(candidate.Version)

		return out
	}

	return out
}

// conflictWith is one conflict checked against the module it names: met when
// that module is absent or its release is out of the constraint.
func conflictWith(modules []ResolvedModule, index int, dependency install.Dependency) ResolvedDependency {
	out := ResolvedDependency{Dependency: dependency, Met: true}

	for at, candidate := range modules {
		if at == index || candidate.Namespace != dependency.Module {
			continue
		}

		out.Instance, out.Type, out.Installed, out.Enabled = candidate.Instance, candidate.Type,
			candidate.Version, candidate.Enabled
		out.Met = !dependency.Holds(candidate.Version)

		return out
	}

	return out
}

// Module is the resolved record of one instance, and false for an instance
// that is not an installed module.
func (r Resolution) Module(instance ref.Ref) (ResolvedModule, bool) {
	for _, held := range r.Modules {
		if held.Instance == instance {
			return held, true
		}
	}

	return ResolvedModule{}, false
}

// Unmet is one dependency of an enabled module that the module it names
// does not meet.
type Unmet struct {
	Module     ResolvedModule
	Dependency ResolvedDependency
	conflict   bool
}

// key identifies an unmet dependency, so the same one can be found in two
// resolutions.
func (u Unmet) key() string {
	return u.Module.Instance.String() + " " + u.Dependency.String() + " " + strconv.FormatBool(u.conflict)
}

// describe is the text of the dependency in a refusal: which module needs
// what, and what this server has.
func (u Unmet) describe() string {
	if u.conflict {
		return u.Module.Namespace + " conflicts with " + u.Dependency.String() + ", and " +
			u.Dependency.Module + " is " + u.Dependency.Installed
	}

	line := u.Module.Namespace + " needs " + u.Dependency.String() + ", and "

	switch {
	case u.Dependency.Installed == "":
		return line + u.Dependency.Module + " is not installed"
	case !u.Dependency.Holds(u.Dependency.Installed):
		return line + u.Dependency.Module + " is " + u.Dependency.Installed
	}

	return line + u.Dependency.Module + " is switched off"
}

// Unmet is every requirement of an enabled module that is not met, and
// every conflict between two installed modules, switched on or off: a
// conflict refuses the two being installed together, as in Composer.
func (r Resolution) Unmet() []Unmet {
	var out []Unmet

	for _, held := range r.Modules {
		for _, conflict := range held.Conflicts {
			if !conflict.Met {
				out = append(out, Unmet{Module: held, Dependency: conflict, conflict: true})
			}
		}

		if !held.Enabled {
			continue
		}

		for _, dependency := range held.Requires {
			if !dependency.Met {
				out = append(out, Unmet{Module: held, Dependency: dependency})
			}
		}
	}

	return out
}

// CheckChange refuses a change of the installed modules that leaves a
// dependency unmet that was met before. before and after are the
// declarations before and after the change, and changed is the instance that
// changes.
//
// A conflict the change brings is refused with MODULE_DEPENDENCY_CONFLICT,
// whichever side declares it. Otherwise a dependency of changed itself is
// refused with MODULE_DEPENDENCY_MISSING, and one of any other module with
// MODULE_REQUIRED_BY. A dependency unmet on both sides does not refuse the
// change.
//
// A change that leaves two enabled modules claiming one file type is refused
// with MODULE_FILE_TYPE_CONFLICT.
func CheckChange(before, after []Declaration, changed ref.Ref) error {
	broken := newlyUnmet(before, after)
	if len(broken) == 0 {
		return checkFileTypes(before, after, changed)
	}

	var own, others, conflicts []Unmet

	for _, unmet := range broken {
		if unmet.conflict {
			conflicts = append(conflicts, unmet)

			continue
		}

		if unmet.Module.Instance == changed {
			own = append(own, unmet)
		} else {
			others = append(others, unmet)
		}
	}

	if len(conflicts) > 0 {
		return httpfault.ModuleDependencyConflict(changed.String(), describeUnmet(conflicts))
	}

	if len(own) > 0 {
		return httpfault.ModuleDependencyMissing(changed.String(), describeUnmet(own))
	}

	return httpfault.ModuleRequiredBy(changed.String(), describeUnmet(others))
}

// newlyUnmet is every dependency unmet after the change that was not unmet
// before it.
func newlyUnmet(before, after []Declaration) []Unmet {
	was := map[string]bool{}
	for _, unmet := range Resolve(before).Unmet() {
		was[unmet.key()] = true
	}

	var out []Unmet

	for _, unmet := range Resolve(after).Unmet() {
		if !was[unmet.key()] {
			out = append(out, unmet)
		}
	}

	return out
}

// describeUnmet writes the unmet dependencies for one line of a refusal.
func describeUnmet(unmet []Unmet) string {
	lines := make([]string, 0, len(unmet))
	for _, one := range unmet {
		lines = append(lines, one.describe())
	}

	return strings.Join(lines, "; ")
}

// RequiredBy is every installed module, switched on or off, that requires
// the module under one namespace. A suggestion is not counted: it never
// blocks a removal.
func RequiredBy(declared []Declaration, namespace string) []ResolvedModule {
	var out []ResolvedModule

	for _, held := range Resolve(declared).Modules {
		for _, dependency := range held.Requires {
			if dependency.Module == namespace {
				out = append(out, held)

				break
			}
		}
	}

	return out
}

// RefuseRemoval refuses to remove a module another installed module
// requires, naming each one.
func RefuseRemoval(declared []Declaration, removed Declaration) error {
	namespace, isModule := removed.ModuleNamespace()
	if !isModule {
		return nil
	}

	dependants := RequiredBy(Without(declared, removed.Ref), namespace)
	if len(dependants) == 0 {
		return nil
	}

	lines := make([]string, 0, len(dependants))

	for _, held := range dependants {
		for _, dependency := range held.Requires {
			if dependency.Module == namespace {
				lines = append(lines, held.Namespace+" requires "+dependency.String())
			}
		}
	}

	return httpfault.ModuleRequiredBy(removed.Ref.String(), strings.Join(lines, "; "))
}

// Unused is every module that an install added for another module and that no
// installed module requires after the given instance is removed. It also
// holds the modules that only those required. The order is the order in which
// they can be removed. A module the operator asked for is never in it.
func Unused(declared []Declaration, removed ref.Ref) []Declaration {
	left := Without(declared, removed)

	var out []Declaration

	for {
		var next []Declaration

		for _, one := range left {
			namespace, isModule := one.ModuleNamespace()
			if !isModule || one.Spec.Module == nil || !one.Spec.Module.Pulled || one.Source != SourceOverlay {
				continue
			}

			if len(RequiredBy(left, namespace)) == 0 {
				next = append(next, one)
			}
		}

		if len(next) == 0 {
			return out
		}

		for _, one := range next {
			out = append(out, one)
			left = Without(left, one.Ref)
		}
	}
}

// Replaced is the declarations with one replaced, or added when it is not
// there.
func Replaced(declared []Declaration, next Declaration) []Declaration {
	out := slices.Clone(declared)

	for index := range out {
		if out[index].Ref == next.Ref {
			out[index] = next

			return out
		}
	}

	return append(out, next)
}

// Without is the declarations without one instance.
func Without(declared []Declaration, instance ref.Ref) []Declaration {
	return slices.DeleteFunc(slices.Clone(declared), func(one Declaration) bool { return one.Ref == instance })
}

// seenIn reports whether an instance is in a set the graph listed.
func seenIn(permitted map[ref.Ref]struct{}) func(ref.Ref) bool {
	return func(instance ref.Ref) bool {
		_, seen := permitted[instance]

		return seen
	}
}

// seeing reports whether one caller may see an instance. It asks the graph
// once, on its first use, so a projection with no dependency does not ask.
// When the graph returns an error, no module is visible.
func (s *Store) seeing(ctx context.Context, who authz.Subject) func(ref.Ref) bool {
	var (
		once    sync.Once
		allowed func(ref.Ref) bool
	)

	return func(instance ref.Ref) bool {
		once.Do(func() {
			permitted, err := s.permitted(ctx, who)
			if err != nil {
				allowed = func(ref.Ref) bool { return false }

				return
			}

			allowed = seenIn(permitted)
		})

		return allowed(instance)
	}
}

// seeingPrincipal is seeing for a caller named by its principal.
func (s *Store) seeingPrincipal(ctx context.Context, principal string) func(ref.Ref) bool {
	who, err := s.subject(principal)
	if err != nil {
		return func(ref.Ref) bool { return false }
	}

	return s.seeing(ctx, who)
}

// stateOf is the state of one instance as a listing would show it.
func (s *Store) stateOf(instance ref.Ref) string {
	if s.plane != nil {
		if observed, ok := s.plane.Status(instance); ok {
			return observed.State
		}
	}

	if declared, ok := s.current.Find(instance); ok {
		return s.declaredStatus(declared).State
	}

	return ""
}

// refuseUnresolved refuses a write of one module declaration that would leave
// a dependency unmet, or remove a module another installed module requires.
// Apply and Delete call it, so the resource API follows the same rules as an
// install and a removal. A declaration that is not a module is never refused
// here.
func refuseUnresolved(doc Document, next Declaration, removed bool) error {
	current, exists := doc.Find(next.Ref)
	_, wasModule := current.ModuleNamespace()
	_, isModule := next.ModuleNamespace()

	if (!exists || !wasModule) && !isModule {
		return nil
	}

	if removed {
		if err := RefuseRemoval(doc.Instances, current); err != nil {
			return err
		}

		return CheckChange(doc.Instances, Without(doc.Instances, next.Ref), next.Ref)
	}

	return CheckChange(doc.Instances, Replaced(doc.Instances, next), next.Ref)
}
