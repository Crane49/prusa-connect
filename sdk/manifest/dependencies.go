package manifest

import (
	"fmt"
	"slices"
	"strings"
)

// A module depends on other modules the way a Composer package does. The
// three keys are Composer's:
//
//	require:
//	  core: ">=0.9"                          the core, a platform package
//	  core-device.list: "*"                  a capability the core serves
//	  com.github.aux.acme.threemf: "^1.2"    another module and a constraint
//	suggest:
//	  com.github.aux.acme.parts: "Shows the parts of a plan"
//	conflict:
//	  com.github.aux.acme.old: "<2.0"
//
// A module is named by its namespace. A suggestion gives a reason, has no
// constraint and installs nothing. A conflict refuses to run beside the
// releases its constraint matches.

// PlatformCore is the name the core goes by in require, as php does in
// Composer.
const PlatformCore = "core"

// PlatformCapability prefixes a capability the core serves, as ext- does in
// Composer: core-device.list.
const PlatformCapability = "core-"

// MaxDependencies is how many entries one of require, suggest and conflict
// may name.
const MaxDependencies = 64

// Dependency is one other module, by its namespace, and a constraint on its
// releases.
type Dependency struct {
	// Module is the namespace of the other module.
	Module string
	// Version is the constraint, in Composer's grammar (ParseConstraint).
	Version string
}

// Suggestion is one module a module works better with, and why.
type Suggestion struct {
	Module string
	Reason string
}

// readRequire reads the require map into requirements, in name order: the
// core, the capabilities, and the other modules.
func readRequire(tree map[string]any) ([]Requirement, string, error) {
	table, err := dependencyTable(tree, "require")
	if err != nil {
		return nil, "", err
	}

	var (
		out  []Requirement
		core string
	)

	for _, name := range slices.Sorted(mapsKeys(table)) {
		value := table[name]
		where := "require." + name

		switch {
		case name == PlatformCore:
			if err = constraintKey(where, value); err != nil {
				return nil, "", err
			}

			core = value
			out = append(out, Requirement{Core: value})
		case strings.HasPrefix(name, PlatformCapability):
			if value != "*" {
				return nil, "", fmt.Errorf("%w: %s is %q, and a capability has no version, so it is \"*\"",
					ErrInvalid, where, value)
			}

			out = append(out, Requirement{Capability: strings.TrimPrefix(name, PlatformCapability)})
		default:
			if err = moduleName(where, name); err != nil {
				return nil, "", err
			}

			if err = constraintKey(where, value); err != nil {
				return nil, "", err
			}

			out = append(out, Requirement{Module: name, Version: value})
		}
	}

	return out, core, nil
}

// readSuggest reads the suggest map.
func readSuggest(tree map[string]any) ([]Suggestion, error) {
	table, err := dependencyTable(tree, "suggest")
	if err != nil {
		return nil, err
	}

	out := make([]Suggestion, 0, len(table))

	for _, name := range slices.Sorted(mapsKeys(table)) {
		if err = moduleName("suggest."+name, name); err != nil {
			return nil, err
		}

		out = append(out, Suggestion{Module: name, Reason: table[name]})
	}

	return out, nil
}

// readConflict reads the conflict map.
func readConflict(tree map[string]any) ([]Dependency, error) {
	table, err := dependencyTable(tree, "conflict")
	if err != nil {
		return nil, err
	}

	out := make([]Dependency, 0, len(table))

	for _, name := range slices.Sorted(mapsKeys(table)) {
		where := "conflict." + name

		if err = moduleName(where, name); err != nil {
			return nil, err
		}

		if err = constraintKey(where, table[name]); err != nil {
			return nil, err
		}

		out = append(out, Dependency{Module: name, Version: table[name]})
	}

	return out, nil
}

// dependencyTable reads one of the three maps and holds it to its size.
func dependencyTable(tree map[string]any, key string) (map[string]string, error) {
	table, err := textTable(tree, "", key)
	if err != nil {
		return nil, err
	}

	if len(table) > MaxDependencies {
		return nil, fmt.Errorf("%w: %s names %d entries, and the limit is %d",
			ErrInvalid, key, len(table), MaxDependencies)
	}

	return table, nil
}

// moduleName holds a key to the namespace of a module.
func moduleName(where, name string) error {
	if !validNamespace(name) {
		return fmt.Errorf("%w: %s names %q, which is not the namespace of a module, such as "+
			"com.github.acme.reader, nor core or core-<capability>", ErrInvalid, where, name)
	}

	return nil
}

// mapsKeys is the keys of a table, as an iterator.
func mapsKeys(table map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range table {
			if !yield(key) {
				return
			}
		}
	}
}

// Dependencies is every other module the manifest requires, by name order.
func (m Manifest) Dependencies() []Dependency {
	var out []Dependency

	for _, required := range m.Requires {
		if required.Module != "" {
			out = append(out, required.Dependency())
		}
	}

	return out
}

// holdDependencies refuses a module that is both required and in conflict, and
// a module under calls that is neither required nor suggested.
func holdDependencies(held Manifest) error {
	named := map[string]bool{}

	for _, dependency := range held.Dependencies() {
		named[dependency.Module] = true
	}

	for _, conflict := range held.Conflicts {
		if named[conflict.Module] {
			return fmt.Errorf("%w: %s is under both require and conflict", ErrInvalid, conflict.Module)
		}
	}

	for _, suggestion := range held.Suggests {
		named[suggestion.Module] = true
	}

	for _, namespace := range held.Calls {
		if !named[namespace] {
			return fmt.Errorf("%w: calls names %s, which neither require nor suggest names, so the "+
				"releases this module works with are not known", ErrInvalid, namespace)
		}
	}

	// A module that draws another module's component must require that
	// module.
	if held.UI != nil {
		for _, imported := range held.UI.Imports {
			if !named[imported.Module] {
				return fmt.Errorf("%w: ui.imports names %s of %s, which neither require nor suggest names, so "+
					"the releases this module works with are not known", ErrInvalid, imported.Name, imported.Module)
			}
		}
	}

	return nil
}
