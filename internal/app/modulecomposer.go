package app

// The module commands that follow Composer's: install from the lock,
// update, why, why-not and outdated. add and remove are in
// modulecli.go and moduleremove.go, and every command that changes what is
// installed writes the lock.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/moduledeps"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// The verbs that follow Composer's.
const (
	installVerb  = "install"
	updateVerb   = "update"
	whyVerb      = "why"
	whyNotVerb   = "why-not"
	outdatedVerb = "outdated"
)

// errLockStale is a lock install refuses: it does not satisfy its modules or
// the core, or the server has modules the lock does not name.
var errLockStale = errors.New("the lock cannot be installed")

// recordLock writes the lock of what the server has installed. Every command
// and every API call that changes the installed modules calls it.
func recordLock(core config.Core, configPath string) error {
	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	if err != nil {
		return err
	}

	return moduledeps.WriteLock(moduledeps.LockPath(core.Overlay),
		moduledeps.LockOf(moduledeps.InstalledOf(doc.Instances), resolverSettings(core)))
}

// lockWritable refuses a change of the installed modules before anything is
// written when the lock beside the overlay could not be written afterwards,
// so a full disk or a read-only directory stops the change instead of
// leaving a lock that does not name what is installed.
func lockWritable(core config.Core) error {
	dir := filepath.Dir(moduledeps.LockPath(core.Overlay))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("the module lock directory %s: %w", dir, err)
	}

	probe, err := os.CreateTemp(dir, ".modules.lock.*")
	if err != nil {
		return fmt.Errorf("the module lock directory %s is not writable: %w", dir, err)
	}

	name := probe.Name()
	_ = probe.Close()

	if err = os.Remove(name); err != nil {
		return fmt.Errorf("the module lock directory %s: %w", dir, err)
	}

	return nil
}

// commandInstaller is the installer a command resolves and fetches through.
// It holds no graph and no plane: the command writes the files the next
// start reads.
func commandInstaller(core config.Core, configPath string) *moduleInstaller {
	return newModuleInstaller(core, configPath, nil, nil, nil, nil, nil)
}

// installFromLock installs exactly the releases the lock names, as composer
// install does: no resolution, each release from the source and digest the
// lock records.
func installFromLock(ctx context.Context, out io.Writer, core config.Core, configPath string, approve bool) error {
	path := moduledeps.LockPath(core.Overlay)

	lock, err := moduledeps.ReadLock(path)
	if err != nil {
		return err
	}

	if len(lock.Modules) == 0 {
		return fmt.Errorf("%w: %s names no module. `%s %s` installs a module and writes it",
			errLockStale, path, ModuleCommand, addVerb)
	}

	settings := resolverSettings(core)
	if err = lock.Check(settings); err != nil {
		return fmt.Errorf("%w: %w. `%s %s` resolves the modules again", errLockStale, err, ModuleCommand, updateVerb)
	}

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	if err != nil {
		return err
	}

	installed := moduledeps.InstalledOf(doc.Instances)
	if extra := notLocked(installed, lock); len(extra) > 0 {
		return fmt.Errorf("%w: %s are installed and not in the lock. Remove them, or `%s %s` to resolve "+
			"them into the lock", errLockStale, strings.Join(extra, ", "), ModuleCommand, updateVerb)
	}

	installer := commandInstaller(core, configPath)
	operation := moduleInstall{
		request: moduleRequest{approve: approve},
		grants:  func() (*moduleAuthority, func(), error) { return openModuleGrants(ctx, core) },
	}

	for _, locked := range lockedInOrder(lock) {
		if held, found := installedOf(installed, locked.Namespace); found && sameLocked(held, locked) {
			continue
		}

		if err = verifyLockedSource(locked.Source, locked.Digest); err != nil {
			return fmt.Errorf("%w: %s %s: %w", errLockStale, locked.Namespace, locked.Version, err)
		}

		step := moduledeps.Step{Release: moduledeps.Release{
			Module: locked.Path, Namespace: locked.Namespace, Version: locked.Version,
			Source: locked.Source, Digest: locked.Digest,
		}, Pulled: locked.Pulled}

		if _, err = installer.installPlannedAs(ctx, step, operation, locked.Constraint); err != nil {
			return err
		}

		fmt.Fprintf(out, "installed %s %s from the lock\n", locked.Namespace, locked.Version)
	}

	return runningCoreNotice(ctx, out, core, configPath, approve, "")
}

// sameLocked reports whether an installed release is the one the lock names:
// the same version and, when the lock records one, the same digest. A release
// installed again under the same version from other bytes is not.
func sameLocked(held moduledeps.Installed, locked moduledeps.LockedModule) bool {
	if held.Version != locked.Version {
		return false
	}

	return locked.Digest == "" || strings.EqualFold(held.Digest, locked.Digest)
}

// verifyLockedSource checks a release on this server's filesystem against the
// digest the lock records for it, because reachable checks only a fetched
// release. A URL is checked when it is fetched. A directory has no single
// digest to check, so a lock that records one for a directory is refused.
func verifyLockedSource(source, digest string) error {
	lower := strings.ToLower(strings.TrimSpace(source))
	if digest == "" || strings.HasPrefix(lower, httpsPrefix) || strings.HasPrefix(lower, httpPrefix) {
		return nil
	}

	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("the release at %s: %w", source, err)
	}

	if info.IsDir() {
		return fmt.Errorf("the lock records a digest for %s, which is a directory and has none to check", source)
	}

	got, err := fileSHA256(source)
	if err != nil {
		return err
	}

	if !strings.EqualFold(got, digest) {
		return fmt.Errorf("%s hashes to %s and the lock records %s", source, got, digest)
	}

	return nil
}

// fileSHA256 is the hex SHA-256 of one file.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// notLocked is the installed modules the lock does not name.
func notLocked(installed []moduledeps.Installed, lock moduledeps.Lock) []string {
	var out []string

	for _, one := range installed {
		if _, found := lock.Locked(one.Namespace); !found {
			out = append(out, one.Namespace)
		}
	}

	return out
}

// installedOf is one installed module by namespace.
func installedOf(installed []moduledeps.Installed, namespace string) (moduledeps.Installed, bool) {
	at := slices.IndexFunc(installed, func(one moduledeps.Installed) bool { return one.Namespace == namespace })
	if at < 0 {
		return moduledeps.Installed{}, false
	}

	return installed[at], true
}

// lockedInOrder is the lock's modules, each after the modules it requires.
func lockedInOrder(lock moduledeps.Lock) []moduledeps.LockedModule {
	var out []moduledeps.LockedModule

	done := map[string]bool{}

	var visit func(held moduledeps.LockedModule)

	visit = func(held moduledeps.LockedModule) {
		if done[held.Namespace] {
			return
		}

		done[held.Namespace] = true

		for module := range held.Require {
			if required, found := lock.Locked(module); found {
				visit(required)
			}
		}

		out = append(out, held)
	}

	for _, held := range lock.Modules {
		visit(held)
	}

	return out
}

// updateModules resolves the named modules again, or every one when none is
// named, installs what changed, and writes the lock, as composer update does.
func updateModules(
	ctx context.Context, out io.Writer, core config.Core, configPath string, named []string, withDependencies,
	approve bool,
) error {
	installer := commandInstaller(core, configPath)

	request := moduledeps.Request{UpdateAll: len(named) == 0, WithDependencies: withDependencies}

	for _, one := range named {
		namespace := one
		if derived, err := extension.NamespaceOf(one); err == nil {
			namespace = string(derived)
		}

		request.Update = append(request.Update, namespace)
	}

	plan, err := installer.resolveOn(ctx, request)
	if err != nil {
		return err
	}

	operation := moduleInstall{
		request: moduleRequest{approve: approve},
		grants:  func() (*moduleAuthority, func(), error) { return openModuleGrants(ctx, core) },
	}

	for _, step := range plan.Steps {
		if _, err = installer.installPlanned(ctx, step, operation); err != nil {
			return err
		}

		from := step.From
		if from == "" {
			from = "nothing"
		}

		fmt.Fprintf(out, "updated %s from %s to %s\n", step.Release.Namespace, from, step.Release.Version)
	}

	if len(plan.Steps) == 0 {
		fmt.Fprintln(out, "nothing to update")
	}

	if err = recordLock(core, configPath); err != nil {
		return err
	}

	return runningCoreNotice(ctx, out, core, configPath, approve, "")
}

// whyModule prints what keeps a module installed, as composer why does.
func whyModule(out io.Writer, core config.Core, configPath, named string) error {
	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	if err != nil {
		return err
	}

	reasons := moduledeps.Why(moduledeps.InstalledOf(doc.Instances), namespaceOf(named))
	if len(reasons) == 0 {
		fmt.Fprintf(out, "nothing installed requires %s\n", named)
	}

	for _, reason := range reasons {
		fmt.Fprintln(out, reason.Text)
	}

	return nil
}

// whyNotModule prints what refuses one release of a module, as composer
// why-not does.
func whyNotModule(ctx context.Context, out io.Writer, core config.Core, configPath, named, version string) error {
	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("%w: %s %s <module> <version>", ErrUsage, ModuleCommand, whyNotVerb)
	}

	installer := commandInstaller(core, configPath)

	available, err := installer.candidates(ctx)
	if err != nil {
		return err
	}

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	if err != nil {
		return err
	}

	reasons := moduledeps.WhyNot(moduledeps.InstalledOf(doc.Instances), available, namespaceOf(named), version,
		resolverSettings(core))
	if len(reasons) == 0 {
		fmt.Fprintf(out, "nothing refuses %s %s\n", named, version)
	}

	for _, reason := range reasons {
		fmt.Fprintln(out, reason.Text)
	}

	return nil
}

// outdatedModules prints each installed module against the index, as
// composer outdated does.
func outdatedModules(ctx context.Context, out io.Writer, core config.Core, configPath string) error {
	installer := commandInstaller(core, configPath)

	available, err := installer.candidates(ctx)
	if err != nil {
		return err
	}

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	if err != nil {
		return err
	}

	for _, row := range moduledeps.OutdatedOf(moduledeps.InstalledOf(doc.Instances), available, resolverSettings(core)) {
		fmt.Fprintf(out, "%s\t%s\tallowed %s\tlatest %s\t%s\n", row.Namespace, row.Installed,
			noneWhenEmpty(row.Allowed), noneWhenEmpty(row.Latest), row.Status)
	}

	return nil
}

// namespaceOf reads a module path or a namespace as a namespace.
func namespaceOf(named string) string {
	if derived, err := extension.NamespaceOf(strings.TrimSpace(named)); err == nil {
		return string(derived)
	}

	return strings.TrimSpace(named)
}

func noneWhenEmpty(value string) string {
	if value == "" {
		return "none"
	}

	return value
}

// fromIndex resolves `module add path@constraint` against the index and
// points the request at the release it chose.
func (m *moduleInstaller) fromIndex(ctx context.Context, request *moduleRequest) (func(), error) {
	modulePath, constraint, found := strings.Cut(request.source, versionSeparator)
	if !found || strings.TrimSpace(modulePath) == "" {
		return nil, fmt.Errorf("%w: %s %s <module path>%s<constraint>", ErrUsage, ModuleCommand, addVerb,
			versionSeparator)
	}

	namespace := namespaceOf(modulePath)

	plan, err := m.resolveOn(
		ctx,
		moduledeps.Request{Require: map[string]string{namespace: strings.TrimSpace(constraint)}},
	)
	if err != nil {
		return nil, err
	}

	at := slices.IndexFunc(plan.Steps, func(step moduledeps.Step) bool { return step.Release.Namespace == namespace })
	if at < 0 {
		return nil, fmt.Errorf("%w: %s %s is installed already", ErrUsage, modulePath, constraint)
	}

	release, _ := offerOf(plan.Steps[at].Release)

	from, done, err := m.reachable(ctx, m.marketSource(release.Source, release.Sha256))
	if err != nil {
		return nil, err
	}

	request.from, request.address, request.digest = from, release.Source, release.Sha256
	request.source = modulePath + versionSeparator + strings.TrimSpace(constraint)

	return done, nil
}

// Why is httpapi.ModuleInstaller. It answers what keeps a module installed.
func (m *moduleInstaller) Why(ctx context.Context, principal, namespace string) ([]httpapi.ModuleReason, error) {
	installed, _, err := m.inspected(ctx, principal, false)
	if err != nil {
		return nil, err
	}

	return moduleReasonsOf(moduledeps.Why(installed, namespace)), nil
}

// WhyNot is httpapi.ModuleInstaller. It answers what refuses one release of
// a module.
func (m *moduleInstaller) WhyNot(
	ctx context.Context, principal, namespace, version string,
) ([]httpapi.ModuleReason, error) {
	installed, available, err := m.inspected(ctx, principal, true)
	if err != nil {
		return nil, err
	}

	return moduleReasonsOf(moduledeps.WhyNot(installed, available, namespace, version, resolverSettings(m.core))), nil
}

// Outdated is httpapi.ModuleInstaller. It answers each installed module
// against the index.
func (m *moduleInstaller) Outdated(ctx context.Context, principal string) ([]httpapi.ModuleOutdated, error) {
	installed, available, err := m.inspected(ctx, principal, true)
	if err != nil {
		return nil, err
	}

	rows := moduledeps.OutdatedOf(installed, available, resolverSettings(m.core))
	out := make([]httpapi.ModuleOutdated, 0, len(rows))

	for _, row := range rows {
		out = append(out, httpapi.ModuleOutdated{
			Namespace: row.Namespace, Path: row.Path, Installed: row.Installed, Allowed: row.Allowed,
			Latest: row.Latest, Status: row.Status,
		})
	}

	return out, nil
}

// inspected is the installed modules and, when asked, the index, for a
// caller who may read what is installed.
func (m *moduleInstaller) inspected(
	ctx context.Context, principal string, withIndex bool,
) ([]moduledeps.Installed, []moduledeps.Release, error) {
	if err := modulesRefused(m.core); err != nil {
		return nil, nil, err
	}

	if _, err := authz.ParseSubject(principal); err != nil {
		return nil, nil, err
	}

	doc, err := resource.Load(resource.Paths{File: m.configPath, Overlay: m.core.Overlay})
	if err != nil {
		return nil, nil, installFault("", err)
	}

	var available []moduledeps.Release

	if withIndex {
		if available, err = m.candidates(ctx); err != nil {
			return nil, nil, err
		}
	}

	return moduledeps.InstalledOf(doc.Instances), available, nil
}

func moduleReasonsOf(reasons []moduledeps.Reason) []httpapi.ModuleReason {
	out := make([]httpapi.ModuleReason, 0, len(reasons))
	for _, reason := range reasons {
		out = append(out, httpapi.ModuleReason{By: reason.By, Constraint: reason.Constraint, Text: reason.Text})
	}

	return out
}
