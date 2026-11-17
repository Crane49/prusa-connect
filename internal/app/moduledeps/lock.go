package moduledeps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/kernel/atomicfile"
)

// LockName is the lock file, beside the machine-owned overlay.
const LockName = "modules.lock.json"

// lockPerm is the lock's mode. It names releases and where they came from,
// which is no secret, and only the server writes it.
const lockPerm = 0o640

// lockDirPerm is the mode of the directory the lock is written into.
const lockDirPerm = 0o750

// Lock is the resolved modules, as Composer's composer.lock records its
// packages.
type Lock struct {
	// ContentHash is the hash of what the operator asked for, the roots and
	// the settings (ContentHash). A lock whose hash differs from the server's
	// own is out of date.
	ContentHash      string         `json:"content-hash"`
	MinimumStability string         `json:"minimum-stability,omitempty"`
	PreferStable     bool           `json:"prefer-stable,omitempty"`
	Modules          []LockedModule `json:"modules"`
}

// LockedModule is one release in the lock.
type LockedModule struct {
	Namespace string `json:"namespace"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version"`
	Source    string `json:"source,omitempty"`
	Digest    string `json:"digest,omitempty"`
	// Pulled is true for a module an install added for another module, and
	// Constraint what the operator asked for one they installed themselves.
	Pulled     bool              `json:"pulled,omitempty"`
	Constraint string            `json:"constraint,omitempty"`
	Core       string            `json:"core,omitempty"`
	Require    map[string]string `json:"require,omitempty"`
	Conflict   map[string]string `json:"conflict,omitempty"`
}

// LockPath is where the lock is beside an overlay.
func LockPath(overlay string) string { return filepath.Join(filepath.Dir(overlay), LockName) }

// ReadLock reads a lock, and an empty one when there is none.
func ReadLock(path string) (Lock, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Lock{}, nil
	}

	if err != nil {
		return Lock{}, fmt.Errorf("read %s: %w", path, err)
	}

	var held Lock
	if err = json.Unmarshal(content, &held); err != nil {
		return Lock{}, fmt.Errorf("read %s: %w", path, err)
	}

	return held, nil
}

// WriteLock writes a lock in one rename.
func WriteLock(path string, lock Lock) error {
	encoded, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("render %s: %w", path, err)
	}

	if err = os.MkdirAll(filepath.Dir(path), lockDirPerm); err != nil {
		return fmt.Errorf("create the directory of %s: %w", path, err)
	}

	if err = atomicfile.Write(path, append(encoded, '\n'), lockPerm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

// LockOf is the lock of what a server has installed.
func LockOf(installed []Installed, settings Settings) Lock {
	out := Lock{
		ContentHash: ContentHash(installed, settings), MinimumStability: settings.MinimumStability,
		PreferStable: settings.PreferStable, Modules: make([]LockedModule, 0, len(installed)),
	}

	sorted := slices.Clone(installed)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Namespace < sorted[j].Namespace })

	for _, one := range sorted {
		out.Modules = append(out.Modules, LockedModule{
			Namespace: one.Namespace, Path: one.Path, Version: one.Version, Source: one.Source,
			Digest: one.Digest, Pulled: one.Pulled, Constraint: one.Constraint, Core: one.Core,
			Require: tableOf(one.Require), Conflict: tableOf(one.Conflict),
		})
	}

	return out
}

// ContentHash is the hash of what the operator asked for: each module they
// installed themselves with its constraint, and the settings. It is what
// Composer's content-hash is over composer.json. A lock with another hash does
// not match the server.
func ContentHash(installed []Installed, settings Settings) string {
	roots := map[string]string{}

	for _, one := range installed {
		if !one.Pulled {
			roots[one.Namespace] = one.Constraint
		}
	}

	stability := settings.MinimumStability
	if stability == "" {
		stability = "stable"
	}

	encoded, _ := json.Marshal(struct {
		Require          map[string]string `json:"require"`
		MinimumStability string            `json:"minimum-stability"`
		PreferStable     bool              `json:"prefer-stable"`
	}{roots, stability, settings.PreferStable})

	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:])
}

// Fresh reports whether the lock was written for what the operator asks for.
func (l Lock) Fresh(installed []Installed, settings Settings) bool {
	return l.ContentHash == ContentHash(installed, settings)
}

// Locked is the release the lock names for a namespace, and false for none.
func (l Lock) Locked(namespace string) (LockedModule, bool) {
	for _, held := range l.Modules {
		if held.Namespace == namespace {
			return held, true
		}
	}

	return LockedModule{}, false
}

// Installed is the lock as the server would have it installed.
func (l Lock) Installed() []Installed {
	out := make([]Installed, 0, len(l.Modules))

	for _, held := range l.Modules {
		out = append(out, Installed{
			Namespace: held.Namespace, Path: held.Path, Version: held.Version, Source: held.Source,
			Digest: held.Digest, Core: held.Core, Require: dependenciesOf(held.Require),
			Conflict: dependenciesOf(held.Conflict), Pulled: held.Pulled, Constraint: held.Constraint,
		})
	}

	return out
}

// Check refuses a lock that does not satisfy itself and the core: a locked
// module whose require another locked release does not hold, a conflict two
// locked releases meet, or a core constraint the core does not hold. Install
// from a lock takes it only when it passes.
func (l Lock) Check(settings Settings) error {
	installed := l.Installed()

	for _, one := range installed {
		if refused := (Release{Namespace: one.Namespace, Version: one.Version, Core: one.Core}); settings.Core != "" &&
			!refused.coreHolds(settings.Core) {
			return fmt.Errorf("%w: %s %s requires core %s, and core is %s", ErrLockStale, one.Namespace,
				one.Version, one.Core, settings.Core)
		}

		for _, dependency := range one.Require {
			held, found := lockedOf(installed, dependency.Module)
			if !found || !dependency.Holds(held.Version) {
				return fmt.Errorf("%w: %s needs %s, and the lock has %s", ErrLockStale, one.Namespace,
					dependency.String(), versionOrNone(held, found))
			}
		}

		for _, dependency := range one.Conflict {
			if held, found := lockedOf(installed, dependency.Module); found && dependency.Holds(held.Version) {
				return fmt.Errorf("%w: %s conflicts with %s, and the lock has %s", ErrLockStale,
					one.Namespace, dependency.String(), held.Version)
			}
		}
	}

	return nil
}

// ErrLockStale is a lock that does not satisfy its modules or the core.
var ErrLockStale = errors.New("the lock does not satisfy its modules")

func lockedOf(installed []Installed, namespace string) (Installed, bool) {
	for _, one := range installed {
		if one.Namespace == namespace {
			return one, true
		}
	}

	return Installed{}, false
}

func versionOrNone(held Installed, found bool) string {
	if !found {
		return "none"
	}

	return held.Version
}

func tableOf(dependencies []install.Dependency) map[string]string {
	if len(dependencies) == 0 {
		return nil
	}

	out := make(map[string]string, len(dependencies))
	for _, dependency := range dependencies {
		out[dependency.Module] = dependency.Version
	}

	return out
}

func dependenciesOf(table map[string]string) []install.Dependency {
	out := make([]install.Dependency, 0, len(table))
	for _, module := range slices.Sorted(maps.Keys(table)) {
		out = append(out, install.Dependency{Module: module, Version: table[module]})
	}

	return out
}
