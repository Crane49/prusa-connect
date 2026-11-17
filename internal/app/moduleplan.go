package app

// The adapter between an install and the resolver in internal/app/moduledeps:
// the releases the index offers as the resolver reads them, the plan of an
// install, and the plan's releases installed in order.

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/moduledeps"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// indexRelease is what an install needs of a release beyond what the
// resolver reads: the index row, and what the marketplace says it asks for.
type indexRelease struct {
	offer  httpapi.ModuleOffer
	listed *install.ModuleAsks
}

// offerOf is the index row behind a release of a plan.
func offerOf(release moduledeps.Release) (httpapi.ModuleOffer, *install.ModuleAsks) {
	held, _ := release.Extra.(indexRelease)
	if held.offer.Module == "" {
		held.offer = httpapi.ModuleOffer{
			Module: release.Module, Namespace: release.Namespace, Version: release.Version,
			Source: release.Source, Sha256: release.Digest, Channel: release.Channel,
			Process: true,
		}
	}

	return held.offer, held.listed
}

// resolverSettings is the resolver's settings on this server.
func resolverSettings(core config.Core) moduledeps.Settings {
	return moduledeps.Settings{
		Core: install.CoreVersion(), MinimumStability: core.ModulesMinimumStability,
		PreferStable: core.ModulesPreferStable,
	}
}

// candidates is every release the index this server reads offers: the
// index file, the release directories core.modules_local names, and every
// release the marketplace lists for this server when it is switched on.
func (m *moduleInstaller) candidates(ctx context.Context) ([]moduledeps.Release, error) {
	local, err := readModuleIndex(filepath.Join(m.core.Modules, moduleIndexName))
	if err != nil {
		return nil, err
	}

	local = m.localModuleOffers(ctx, local)

	out := make([]moduledeps.Release, 0, len(local))

	for _, offer := range local {
		offer.Origin = originLocal
		out = appendRelease(out, offer, nil)
	}

	on, err := m.marketplaceOn()
	if err != nil {
		return nil, err
	}

	if !on || m.market == nil {
		return out, nil
	}

	for _, row := range m.market.modules(ctx) {
		for _, release := range row.Versions {
			if !m.market.runsHere(release) {
				continue
			}

			held := row
			held.Latest = &release

			offer, ok := m.market.offerOf(held)
			if !ok {
				continue
			}

			listed := release.asks()
			out = appendRelease(out, offer, &listed)
		}
	}

	return out, nil
}

// appendRelease adds one offer as a release, and leaves out one whose module
// path names no namespace.
func appendRelease(
	out []moduledeps.Release,
	offer httpapi.ModuleOffer,
	listed *install.ModuleAsks,
) []moduledeps.Release {
	namespace, err := extension.NamespaceOf(offer.Module)
	if err != nil {
		return out
	}

	offer.Namespace = string(namespace)

	release := moduledeps.Release{
		Module: offer.Module, Namespace: string(namespace), Version: offer.Version, Channel: offer.Channel,
		Source: offer.Source, Digest: offer.Sha256, Extra: indexRelease{offer: offer, listed: listed},
	}

	for _, row := range offer.Requires {
		switch {
		case row.Module != "":
			release.Require = append(release.Require, install.Dependency{Module: row.Module, Version: row.Version})
		case row.Core != "":
			release.Core = row.Core
		}
	}

	for _, row := range offer.Conflicts {
		release.Conflict = append(release.Conflict, install.Dependency{Module: row.Module, Version: row.Version})
	}

	return append(out, release)
}

// targetOf is a release a manifest describes, as the resolver reads it.
func targetOf(modulePath, namespace string, held manifest.Manifest) moduledeps.Release {
	return moduledeps.Release{
		Module: modulePath, Namespace: namespace, Version: held.Version, Core: held.Core,
		Require: requiredModules(held.Requires), Conflict: recordedConflicts(held.Conflicts),
	}
}

// resolveOn resolves a request against what this server has installed, the
// lock and the index.
func (m *moduleInstaller) resolveOn(ctx context.Context, request moduledeps.Request) (moduledeps.Plan, error) {
	doc, err := resource.Load(resource.Paths{File: m.configPath, Overlay: m.core.Overlay})
	if err != nil {
		return moduledeps.Plan{}, err
	}

	if request.Available == nil {
		if request.Available, err = m.candidates(ctx); err != nil {
			return moduledeps.Plan{}, err
		}
	}

	lock, err := moduledeps.ReadLock(moduledeps.LockPath(m.core.Overlay))
	if err != nil {
		return moduledeps.Plan{}, err
	}

	request.Installed, request.Lock, request.Settings = moduledeps.InstalledOf(
		doc.Instances,
	), lock, resolverSettings(
		m.core,
	)

	return moduledeps.Resolve(request)
}

// Plan is httpapi.ModuleInstaller. It resolves one release of the index
// against the modules installed here, and writes nothing.
func (m *moduleInstaller) Plan(
	ctx context.Context, principal string, release httpapi.ModuleReleaseRef,
) (httpapi.ModulePlan, error) {
	if err := modulesRefused(m.core); err != nil {
		return httpapi.ModulePlan{}, err
	}

	if _, err := authz.ParseSubject(principal); err != nil {
		return httpapi.ModulePlan{}, err
	}

	offered, err := m.candidates(ctx)
	if err != nil {
		return httpapi.ModulePlan{}, err
	}

	at := slices.IndexFunc(offered, func(one moduledeps.Release) bool {
		return one.Module == strings.TrimSpace(release.Module) && one.Version == strings.TrimSpace(release.Version)
	})
	if at < 0 {
		return httpapi.ModulePlan{}, httpfault.ModuleNotOffered(release.Module, release.Version)
	}

	plan, err := m.resolveOn(ctx, moduledeps.Request{Available: offered, Add: []moduledeps.Release{offered[at]}})
	if err != nil {
		return httpapi.ModulePlan{}, err
	}

	out := planOf(dependenciesIn(plan, offered[at].Namespace))

	doc, err := resource.Load(resource.Paths{File: m.configPath, Overlay: m.core.Overlay})
	if err != nil {
		return httpapi.ModulePlan{}, installFault(offered[at].Namespace, err)
	}

	out.Conflicts = planConflicts(offersOf(plan.Steps), doc.Instances)

	return out, nil
}

// offersOf is the offers of the releases a plan writes.
func offersOf(steps []moduledeps.Step) []httpapi.ModuleOffer {
	out := make([]httpapi.ModuleOffer, 0, len(steps))
	for _, step := range steps {
		offer, _ := offerOf(step.Release)
		out = append(out, offer)
	}

	return out
}

// planConflicts is every enabled module that claims a file type one of the
// releases claims too, other than an older release of the same module. Two
// of the releases themselves are checked against each other as well.
func planConflicts(releases []httpapi.ModuleOffer, installed []resource.Declaration) []httpapi.ModuleFileTypeConflict {
	adding := map[string]bool{}

	var claims []resource.FileTypeClaim

	for _, release := range releases {
		adding[release.Namespace] = true
		claims = append(claims, resource.ClaimsOf(ref.Ref{}, release.Namespace,
			release.FileTypes, release.ViewTypes)...)
	}

	for _, claim := range resource.FileTypeClaims(installed) {
		if !adding[claim.Namespace] {
			claims = append(claims, claim)
		}
	}

	out := []httpapi.ModuleFileTypeConflict{}

	for _, conflict := range resource.ConflictsAmong(claims) {
		module, with := conflict.First.Namespace, conflict.Second.Namespace
		if !adding[module] {
			module, with = with, module
		}

		if !adding[module] {
			continue
		}

		out = append(out, httpapi.ModuleFileTypeConflict{
			Module: module, With: with, Use: conflict.First.Use, Types: conflict.Types,
		})
	}

	return out
}

// dependenciesIn is the steps of a plan other than the module asked for.
func dependenciesIn(plan moduledeps.Plan, namespace string) []moduledeps.Step {
	var out []moduledeps.Step

	for _, step := range plan.Steps {
		if step.Release.Namespace != namespace {
			out = append(out, step)
		}
	}

	return out
}

// planOf is a plan as the API answers it.
func planOf(steps []moduledeps.Step) httpapi.ModulePlan {
	out := httpapi.ModulePlan{
		Dependencies: make([]httpapi.ModulePlanned, 0, len(steps)),
		Conflicts:    []httpapi.ModuleFileTypeConflict{},
	}
	for _, step := range steps {
		offer, _ := offerOf(step.Release)
		out.Dependencies = append(
			out.Dependencies,
			httpapi.ModulePlanned{ModuleOffer: offer, RequiredBy: step.RequiredBy},
		)
	}

	return out
}

// samePlan reports whether a plan is the one the person approved: the same
// modules, the same releases, and the same bytes, so an index that points a
// release at other bytes after the plan was read does not pass.
func samePlan(steps []moduledeps.Step, approved []httpapi.ModuleReleaseRef) bool {
	if len(steps) != len(approved) {
		return false
	}

	for index, step := range steps {
		if step.Release.Module != strings.TrimSpace(approved[index].Module) ||
			step.Release.Version != strings.TrimSpace(approved[index].Version) ||
			!strings.EqualFold(step.Release.Digest, strings.TrimSpace(approved[index].Sha256)) {
			return false
		}
	}

	return true
}

// describePlan writes a plan for a refusal or a command to print.
func describePlan(steps []moduledeps.Step) string {
	if len(steps) == 0 {
		return "nothing"
	}

	lines := make([]string, 0, len(steps))
	for _, step := range steps {
		lines = append(lines, step.Release.Module+"@"+step.Release.Version)
	}

	return strings.Join(lines, ", ")
}

// dependencyInstall is how an install adds the modules its release requires
// before it writes its own declaration: the plan is resolved, held to the one
// the person approved when check is set, and each release of it installed
// with the same approve. undo takes the added modules away again when the
// install fails after them.
type dependencyInstall func(
	ctx context.Context, module, namespace string, held manifest.Manifest,
) (added []moduledeps.Step, undo func(context.Context), err error)

// dependenciesFor is the dependencyInstall of one install made through this
// installer.
func (m *moduleInstaller) dependenciesFor(
	approved []httpapi.ModuleReleaseRef, check bool, parent moduleInstall,
) dependencyInstall {
	return func(
		ctx context.Context, module, namespace string, held manifest.Manifest,
	) ([]moduledeps.Step, func(context.Context), error) {
		nothing := func(context.Context) {}
		target := targetOf(module, namespace, held)

		if !m.missingAny(target) {
			if check && len(approved) > 0 {
				return nil, nothing, httpfault.ModulePlanChanged(module, "nothing")
			}

			return nil, nothing, nil
		}

		plan, err := m.resolveOn(ctx, moduledeps.Request{Add: []moduledeps.Release{target}})
		if err != nil {
			return nil, nothing, err
		}

		steps := dependenciesIn(plan, namespace)

		if check && !samePlan(steps, approved) {
			return nil, nothing, httpfault.ModulePlanChanged(module, describePlan(steps))
		}

		if check {
			parent.approved = approvedReleasesOf(approved)
		}

		undo, err := m.installPlan(ctx, steps, parent)
		if err != nil {
			return nil, nothing, err
		}

		return steps, undo, nil
	}
}

// installPlan installs every release of a plan in order, and answers how to
// take them away again. A failure takes away the ones it already wrote.
func (m *moduleInstaller) installPlan(
	ctx context.Context, planned []moduledeps.Step, parent moduleInstall,
) (func(context.Context), error) {
	var added []resource.Declaration

	undo := func(undoCtx context.Context) {
		for _, written := range slices.Backward(added) {
			_, _ = removeDeclared(undoCtx, moduleRemoval{
				core: m.core, configPath: m.configPath, found: written, stop: parent.stopper(),
				grants: m.grants, plan: declaredGrants(written),
			})
		}
	}

	for _, one := range planned {
		written, err := m.installPlanned(ctx, one, parent)
		if err != nil {
			undo(ctx)

			return nil, err
		}

		added = append(added, written)
	}

	return undo, nil
}

// missingAny reports whether a target requires a module that no installed,
// switched on release satisfies, which is when an install reads the index. A
// module installed at a release outside the range, or switched off, does not
// satisfy it, so the resolver is asked and answers what to do about it.
func (m *moduleInstaller) missingAny(target moduledeps.Release) bool {
	rows, err := resource.Installed(resource.Paths{File: m.configPath, Overlay: m.core.Overlay})
	if err != nil {
		return true
	}

	installed := moduledeps.InstalledOf(rows)

	for _, dependency := range target.Require {
		if !satisfiedBy(dependency, rows, installed) {
			return true
		}
	}

	return false
}

// satisfiedBy reports whether a switched on module holds a release the
// dependency's range accepts.
func satisfiedBy(dependency install.Dependency, rows []resource.Declaration, installed []moduledeps.Installed) bool {
	for _, row := range rows {
		namespace, _ := row.ModuleNamespace()
		if namespace != dependency.Module || !row.Spec.Enabled {
			continue
		}

		held, found := installedOf(installed, namespace)

		return found && dependency.Holds(held.Version)
	}

	return false
}

// installPlanned installs one release of a plan, with the approve and the
// graph of the install that needed it, and answers its declaration.
func (m *moduleInstaller) installPlanned(
	ctx context.Context, step moduledeps.Step, parent moduleInstall,
) (resource.Declaration, error) {
	return m.installPlannedAs(ctx, step, parent, m.constraintOf(step))
}

// constraintOf is the constraint an update keeps for a module the operator
// installed themselves, and empty for a module the plan adds.
func (m *moduleInstaller) constraintOf(step moduledeps.Step) string {
	if step.Pulled {
		return ""
	}

	doc, err := resource.Load(resource.Paths{File: m.configPath, Overlay: m.core.Overlay})
	if err != nil {
		return ""
	}

	held, found := installedOf(moduledeps.InstalledOf(doc.Instances), step.Release.Namespace)
	if !found || held.Constraint == "" {
		return "^" + step.Release.Version
	}

	return held.Constraint
}

// installPlannedAs installs one release of a plan with the constraint the
// declaration records for it.
func (m *moduleInstaller) installPlannedAs(
	ctx context.Context, step moduledeps.Step, parent moduleInstall, constraint string,
) (resource.Declaration, error) {
	one, listed := offerOf(step.Release)

	shown := map[string]*install.ModuleAsks{shownByMarketplace: listed}
	approveListen := false

	// A plan approved over the API holds each module it adds to what the
	// person was shown of it, and grants its ports only when they approved
	// them. An approve with nothing shown for a module would grant whatever
	// its manifest asks, so it is refused.
	if parent.approved != nil && parent.request.approve {
		approved, found := parent.approved[one.Module]
		if !found || approved.expect == nil {
			return resource.Declaration{}, requestsChangedError{
				shown: shownByRequest, beyond: []string{"the plan showed nothing for " + one.Module},
			}
		}

		shown[shownByRequest] = approved.expect
		approveListen = approved.approveListen
	}

	from, done, err := m.reachable(ctx, m.marketSource(one.Source, one.Sha256))
	if err != nil {
		return resource.Declaration{}, err
	}

	defer done()

	held, err := installDeclared(ctx, m.core, m.configPath, moduleInstall{
		request: moduleRequest{
			source:        one.Module + versionSeparator + one.Version,
			from:          from,
			approve:       parent.request.approve,
			approveListen: approveListen,
			shown:         shown,
			channel:       one.Channel,
			pulled:        step.Pulled,
			address:       one.Source,
			digest:        one.Sha256,
			constraint:    constraint,
		},
		mayDeclare: parent.mayDeclare,
		grants:     parent.grants,
	})
	if err != nil {
		return resource.Declaration{}, err
	}

	return held.written, nil
}

// approvedRelease is what a person approved of one module a plan adds: the
// requests they were shown and whether they granted its ports.
type approvedRelease struct {
	expect        *install.ModuleAsks
	approveListen bool
}

// approvedReleasesOf indexes an approved plan by module path.
func approvedReleasesOf(approved []httpapi.ModuleReleaseRef) map[string]approvedRelease {
	out := make(map[string]approvedRelease, len(approved))
	for _, one := range approved {
		out[strings.TrimSpace(one.Module)] = approvedRelease{
			expect: expectedAsks(one.Expect), approveListen: one.ApproveListen,
		}
	}

	return out
}
