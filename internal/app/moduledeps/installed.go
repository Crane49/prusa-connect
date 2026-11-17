package moduledeps

import (
	"github.com/prusa3d/prusa-connect/internal/app/resource"
)

// InstalledOf is the installed modules among declarations, as the resolver
// reads them.
func InstalledOf(declared []resource.Declaration) []Installed {
	var out []Installed

	for _, one := range declared {
		namespace, isModule := one.ModuleNamespace()
		if !isModule || one.Spec.Module == nil {
			continue
		}

		entry := one.Spec.Module
		held := Installed{
			Namespace: namespace, Path: entry.Path, Version: entry.Version, Source: entry.Source,
			Digest: entry.Digest, Pulled: entry.Pulled, Constraint: entry.Constraint,
			Conflict: entry.Conflicts,
		}

		for _, required := range entry.Requires {
			switch {
			case required.Module != "":
				held.Require = append(held.Require, required.Dependency())
			case required.Core != "":
				held.Core = required.Core
			}
		}

		out = append(out, held)
	}

	return out
}
