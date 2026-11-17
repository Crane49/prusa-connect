package install

import (
	"errors"
	"fmt"
	"slices"

	"github.com/prusa3d/server-slicing/internal/catalog"
)

// ErrNoSlicer reports that no installed slicer can slice the project.
var ErrNoSlicer = errors.New("no installed slicer can slice this project")

// ErrTooOld reports a slicer older than the one that saved the project.
var ErrTooOld = errors.New("the slicer is older than the one that saved the project")

// Choose picks the slicer for one job from what is installed, newest first.
//
// A job that names a version gets that version. Otherwise the pinned version
// is used when there is one, and otherwise the newest final release, then the
// newest prerelease. needMajor is the major version of the PrusaSlicer that
// saved the project, or zero when the project does not say. A project saved
// by PrusaSlicer 3 needs a slicer of major version 3 or newer, whatever was
// named or pinned.
func Choose(installed []Slicer, requested, pinned string, needMajor int) (Slicer, error) {
	installed = slices.Clone(installed)
	slices.SortStableFunc(installed, func(a, b Slicer) int { return b.Version.Compare(a.Version) })

	fits := func(held Slicer) bool { return held.Version.Major >= needMajor }

	if requested != "" {
		held, found := find(installed, requested)
		if !found {
			return Slicer{}, fmt.Errorf("%w: %s", ErrNotInstalled, requested)
		}

		if !fits(held) {
			return Slicer{}, fmt.Errorf("%w: the project needs PrusaSlicer %d or newer, and %s was named",
				ErrTooOld, needMajor, requested)
		}

		return held, nil
	}

	if pinned != "" {
		if held, found := find(installed, pinned); found && fits(held) {
			return held, nil
		}
	}

	for _, wantStable := range []bool{true, false} {
		for _, held := range installed {
			if held.Version.Stable() == wantStable && fits(held) {
				preferred, _ := find(installed, held.Name())

				return preferred, nil
			}
		}
	}

	if needMajor > 0 {
		return Slicer{}, fmt.Errorf("%w: the project needs PrusaSlicer %d or newer", ErrNoSlicer, needMajor)
	}

	return Slicer{}, ErrNoSlicer
}

// find is the installed slicer of one version. A build this module downloaded
// is preferred to a local binary of the same version, because its digest was
// checked.
func find(installed []Slicer, version string) (Slicer, bool) {
	wanted, err := catalog.ParseVersion(version)
	if err != nil {
		return Slicer{}, false
	}

	var (
		local Slicer
		seen  bool
	)

	for _, held := range installed {
		if held.Version.Compare(wanted) != 0 {
			continue
		}

		if held.Source != FromLocal {
			return held, true
		}

		local, seen = held, true
	}

	return local, seen
}
