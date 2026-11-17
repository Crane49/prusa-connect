package moduledeps

import (
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// rule is one constraint on one module, and who wrote it.
type rule struct {
	by       string
	needs    install.Dependency
	conflict bool
}

// resolver holds one search.
type resolver struct {
	settings Settings
	// fixed is the installed modules the request leaves as they are.
	fixed map[string]Installed
	// rules is every constraint the fixed modules, the added releases and the
	// operator put. A choice adds its own while it stands.
	rules  []rule
	offers map[string][]Release
	// prefer is the release to try first for a module, from the lock or the
	// installed release, when the request does not update the module.
	prefer map[string]string
	// subject is the module the request is about, which a refusal names.
	subject string
}

// Resolve answers the plan for one request: the releases to install, in the
// order to write them. A module no release fits is refused with
// MODULE_DEPENDENCY_CONFLICT, which names who needs what, and a module no
// index offers with MODULE_DEPENDENCY_NOT_OFFERED. It searches newest release
// first and backs out of a choice a later one contradicts. It never changes a
// module the request leaves fixed.
func Resolve(request Request) (Plan, error) {
	held := newResolver(request)

	chosen := map[string]Release{}
	for _, release := range request.Add {
		chosen[release.Namespace] = release
	}

	for _, release := range request.Add {
		held.addRules(release.Namespace, release)

		if refused := held.platformOf(release); refused != "" {
			return Plan{}, httpfault.ModuleDependencyConflict(held.subject, refused)
		}
	}

	for namespace, release := range chosen {
		if refused := held.conflictOn(namespace, release.Version, held.rules); refused != nil {
			return Plan{}, refused
		}
	}

	solved, err := held.solve(chosen, held.rules)
	if err != nil {
		return Plan{}, err
	}

	return held.plan(solved, request), nil
}

// newResolver reads a request into a search.
func newResolver(request Request) *resolver {
	held := &resolver{
		settings: request.Settings, fixed: map[string]Installed{}, offers: map[string][]Release{},
		prefer: map[string]string{},
	}

	free := freeOf(request)

	for _, one := range request.Installed {
		held.readInstalled(one, free[one.Namespace], request.KeepInstalled)
	}

	for _, locked := range request.Lock.Modules {
		if _, set := held.prefer[locked.Namespace]; !set && !free[locked.Namespace] {
			held.prefer[locked.Namespace] = locked.Version
		}
	}

	for namespace, constraint := range request.Require {
		held.rules = append(
			held.rules,
			rule{by: root, needs: install.Dependency{Module: namespace, Version: constraint}},
		)
	}

	for _, release := range request.Available {
		held.offers[release.Namespace] = append(held.offers[release.Namespace], release)
	}

	switch {
	case len(request.Add) > 0:
		held.subject = request.Add[0].Namespace
	case len(request.Require) > 0:
		held.subject = slices.Sorted(maps.Keys(request.Require))[0]
	case len(request.Update) > 0:
		held.subject = request.Update[0]
	}

	return held
}

// readInstalled adds one installed module to the search: as fixed, with its
// require and conflict as rules, or as resolved again, held to the
// operator's constraint when they installed it themselves.
func (r *resolver) readInstalled(one Installed, free, keep bool) {
	if !free {
		r.fixed[one.Namespace] = one

		for _, dependency := range one.Require {
			r.rules = append(r.rules, rule{by: one.Namespace, needs: dependency})
		}

		for _, dependency := range one.Conflict {
			r.rules = append(r.rules, rule{by: one.Namespace, needs: dependency, conflict: true})
		}

		return
	}

	if keep {
		r.prefer[one.Namespace] = one.Version
	}

	if one.Pulled {
		return
	}

	constraint := one.Constraint
	if constraint == "" {
		constraint = "*"
	}

	r.rules = append(r.rules, rule{by: root, needs: install.Dependency{Module: one.Namespace, Version: constraint}})
}

// freeOf is the installed modules a request resolves again: the ones it
// adds or requires, the ones it updates, and with WithDependencies the
// modules an install added for those, all the way down.
func freeOf(request Request) map[string]bool {
	free := map[string]bool{}

	for _, release := range request.Add {
		free[release.Namespace] = true
	}

	for namespace := range request.Require {
		free[namespace] = true
	}

	installed := map[string]Installed{}

	for _, one := range request.Installed {
		installed[one.Namespace] = one

		if request.UpdateAll {
			free[one.Namespace] = true
		}
	}

	for _, namespace := range request.Update {
		free[namespace] = true
	}

	if !request.WithDependencies {
		return free
	}

	for moved := true; moved; {
		moved = false

		for namespace := range free {
			for _, dependency := range installed[namespace].Require {
				if held, ok := installed[dependency.Module]; ok && held.Pulled && !free[dependency.Module] {
					free[dependency.Module] = true
					moved = true
				}
			}
		}
	}

	return free
}

// addRules adds a release's require and conflict to the rules.
func (r *resolver) addRules(namespace string, release Release) {
	for _, dependency := range release.Require {
		r.rules = append(r.rules, rule{by: namespace, needs: dependency})
	}

	for _, dependency := range release.Conflict {
		r.rules = append(r.rules, rule{by: namespace, needs: dependency, conflict: true})
	}
}

// solve meets every require in rules, choosing a release for each module
// that is not fixed, and answers the choices.
func (r *resolver) solve(chosen map[string]Release, rules []rule) (map[string]Release, error) {
	for _, one := range rules {
		if one.conflict {
			continue
		}

		namespace := one.needs.Module

		if installed, isFixed := r.fixed[namespace]; isFixed {
			if refused := r.conflictOn(namespace, installed.Version, rules); refused != nil {
				return nil, refused
			}

			continue
		}

		if _, done := chosen[namespace]; done {
			continue
		}

		return r.choose(chosen, rules, namespace)
	}

	for namespace, release := range chosen {
		if refused := r.conflictOn(namespace, release.Version, rules); refused != nil {
			return nil, refused
		}
	}

	return chosen, nil
}

// choose tries each acceptable release of one module in order, and answers
// the first one with which every rule can still be met.
func (r *resolver) choose(chosen map[string]Release, rules []rule, namespace string) (map[string]Release, error) {
	if len(r.offers[namespace]) == 0 {
		return nil, httpfault.ModuleDependencyNotOffered(r.subject, describeRules(rules, namespace))
	}

	candidates, skipped := r.candidatesOf(namespace, rules)

	var last error

	for _, candidate := range candidates {
		if r.conflictOn(namespace, candidate.Version, rules) != nil {
			continue
		}

		if r.conflictsWithPresent(candidate, chosen) {
			continue
		}

		next := make(map[string]Release, len(chosen)+1)
		maps.Copy(next, chosen)
		next[namespace] = candidate

		more := slices.Clone(rules)
		for _, dependency := range candidate.Require {
			more = append(more, rule{by: namespace, needs: dependency})
		}

		for _, dependency := range candidate.Conflict {
			more = append(more, rule{by: namespace, needs: dependency, conflict: true})
		}

		solved, err := r.solve(next, more)
		if err == nil {
			return solved, nil
		}

		last = err
	}

	if last != nil {
		return nil, last
	}

	line := describeRules(
		rules,
		namespace,
	) + ", and the index offers " + namespace + " " + versionsOf(
		r.offers[namespace],
	)
	if skipped != "" {
		line += ". " + skipped
	}

	return nil, httpfault.ModuleDependencyConflict(r.subject, line)
}

// conflictsWithPresent reports whether a candidate's conflict holds a fixed
// or chosen module.
func (r *resolver) conflictsWithPresent(candidate Release, chosen map[string]Release) bool {
	for _, conflict := range candidate.Conflict {
		if installed, ok := r.fixed[conflict.Module]; ok && conflict.Holds(installed.Version) {
			return true
		}

		if release, ok := chosen[conflict.Module]; ok && conflict.Holds(release.Version) {
			return true
		}
	}

	return false
}

// candidatesOf is the releases of one module the platform and the
// stabilities allow, in the order the resolver tries them: the preferred
// release first, then by prefer-stable, then newest first. skipped says what
// the platform and the stabilities left out, for a refusal.
func (r *resolver) candidatesOf(namespace string, rules []rule) ([]Release, string) {
	allowed := r.settings.minimum()

	for _, one := range rules {
		if one.needs.Module != namespace || one.conflict {
			continue
		}

		if held, err := manifest.ParseConstraint(one.needs.Version); err == nil {
			if flag, named := held.Stability(); named && flag < allowed {
				allowed = flag
			}
		}
	}

	var (
		out     []Release
		reasons []string
	)

	for _, release := range r.offers[namespace] {
		if release.Stability() < allowed {
			reasons = appendOnce(reasons, "Releases below "+allowed.String()+" are not taken")

			continue
		}

		if refused := r.platformOf(release); refused != "" {
			reasons = appendOnce(reasons, refused)

			continue
		}

		out = append(out, release)
	}

	preferred := r.prefer[namespace]

	// sort.SliceStable and not slices.SortStableFunc: the generic sort is
	// compiled once per element shape.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]

		switch {
		case (a.Version == preferred) != (b.Version == preferred):
			return a.Version == preferred
		case r.settings.PreferStable && a.Stability() != b.Stability():
			return a.Stability() > b.Stability()
		}

		return compareVersions(a.Version, b.Version) > 0
	})

	return out, strings.Join(reasons, ". ")
}

// platformOf is why the core platform package refuses a release, and empty
// when it does not.
func (r *resolver) platformOf(release Release) string {
	if r.settings.Core == "" || strings.TrimSpace(release.Core) == "" {
		return ""
	}

	in, readable := install.CoreHolds(release.Core, r.settings.Core)
	if !readable {
		return ""
	}

	if !in {
		return release.Namespace + " " + release.Version + " requires core " + release.Core +
			", and core is " + r.settings.Core
	}

	return ""
}

// conflictOn refuses a release of one module that a require does not hold or
// a conflict holds.
func (r *resolver) conflictOn(namespace, version string, rules []rule) error {
	for _, one := range rules {
		if one.needs.Module != namespace || one.conflict == !one.needs.Holds(version) {
			continue
		}

		line := describeRules(rules, namespace) + ", and " + namespace + " is " + version
		if _, isFixed := r.fixed[namespace]; isFixed {
			line += ". Update " + namespace + " first"
		}

		return httpfault.ModuleDependencyConflict(r.subject, line)
	}

	return nil
}

// plan is the choices that change the server, in the order to write them.
func (r *resolver) plan(chosen map[string]Release, request Request) Plan {
	installed := map[string]Installed{}
	for _, one := range request.Installed {
		installed[one.Namespace] = one
	}

	asked := map[string]bool{}
	for _, release := range request.Add {
		asked[release.Namespace] = true
	}

	for namespace := range request.Require {
		asked[namespace] = true
	}

	var out Plan

	done := map[string]bool{}

	var visit func(namespace string)

	visit = func(namespace string) {
		release, isChosen := chosen[namespace]
		if done[namespace] || !isChosen {
			return
		}

		done[namespace] = true

		for _, dependency := range release.Require {
			visit(dependency.Module)
		}

		was, had := installed[namespace]
		if had && was.Version == release.Version && !asked[namespace] {
			return
		}

		out.Steps = append(out.Steps, Step{
			Release: release, From: was.Version, RequiredBy: r.requiredBy(namespace, chosen),
			Pulled: !asked[namespace] && (!had || was.Pulled),
		})
	}

	for _, namespace := range slices.Sorted(maps.Keys(chosen)) {
		visit(namespace)
	}

	return out
}

// requiredBy is who requires one module, each as module constraint.
func (r *resolver) requiredBy(namespace string, chosen map[string]Release) []string {
	var out []string

	for _, one := range r.rules {
		if !one.conflict && one.needs.Module == namespace {
			out = appendOnce(out, one.by+" "+one.needs.Version)
		}
	}

	for by, release := range chosen {
		for _, dependency := range release.Require {
			if dependency.Module == namespace {
				out = appendOnce(out, by+" "+dependency.Version)
			}
		}
	}

	slices.Sort(out)

	return out
}

// describeRules writes who needs what of one module, as a refusal names it.
func describeRules(rules []rule, namespace string) string {
	var lines []string

	for _, one := range rules {
		if one.needs.Module != namespace {
			continue
		}

		verb := " needs "
		if one.conflict {
			verb = " conflicts with "
		}

		lines = appendOnce(lines, one.by+verb+one.needs.String())
	}

	return strings.Join(lines, ", ")
}

// versionsOf writes the releases of one module.
func versionsOf(releases []Release) string {
	versions := make([]string, 0, len(releases))
	for _, release := range releases {
		versions = append(versions, release.Version)
	}

	return strings.Join(versions, ", ")
}

// compareVersions orders two releases, and puts one the grammar cannot read
// last.
func compareVersions(a, b string) int {
	left, errA := manifest.ParseRelease(a)
	right, errB := manifest.ParseRelease(b)

	switch {
	case errA != nil && errB != nil:
		return strings.Compare(a, b)
	case errA != nil:
		return -1
	case errB != nil:
		return 1
	}

	return left.Compare(right)
}

func appendOnce(lines []string, line string) []string {
	if slices.Contains(lines, line) {
		return lines
	}

	return append(lines, line)
}
