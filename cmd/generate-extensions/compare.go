package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// catalogueDiff is the difference between the committed catalogue and the
// registered one. A removal breaks a configuration that names the entry.
type catalogueDiff struct {
	Removed []extension.Descriptor
	Added   []extension.Descriptor
}

// diffCatalogue compares the two lists by kind and qualified name only. Any
// other change shows up in the -check byte comparison. The result is sorted.
func diffCatalogue(committed, current []extension.Descriptor) catalogueDiff {
	committedNames := nameSet(committed)
	currentNames := nameSet(current)

	diff := catalogueDiff{}

	for _, entry := range committed {
		if _, ok := currentNames[entry.String()]; !ok {
			diff.Removed = append(diff.Removed, entry)
		}
	}

	for _, entry := range current {
		if _, ok := committedNames[entry.String()]; !ok {
			diff.Added = append(diff.Added, entry)
		}
	}

	extension.SortDescriptors(diff.Removed)
	extension.SortDescriptors(diff.Added)

	return diff
}

// errDuplicateEntries means one side has the same kind and name twice.
var errDuplicateEntries = errors.New("one qualified name is held twice")

// checkDuplicates refuses a name that appears twice on one side. diffCatalogue
// cannot see it because it reads each side into a set.
func checkDuplicates(where string, entries []extension.Descriptor) error {
	seen := make(map[string]extension.Descriptor, len(entries))

	var offences []string

	for _, entry := range entries {
		existing, taken := seen[entry.String()]
		if !taken {
			seen[entry.String()] = entry

			continue
		}

		offences = append(offences, fmt.Sprintf("%s, by %q and again by %q",
			entry, existing.Module(), entry.Module()))
	}

	if len(offences) == 0 {
		return nil
	}

	slices.Sort(offences)

	return fmt.Errorf("%w: %s holds %s. An operator writing that name cannot be told which entry "+
		"answered: rename one", errDuplicateEntries, where, strings.Join(offences, "; "))
}

func nameSet(entries []extension.Descriptor) map[string]struct{} {
	set := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		set[entry.String()] = struct{}{}
	}

	return set
}

func entryNames(entries []extension.Descriptor) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.String())
	}

	slices.Sort(out)

	return out
}

// headingOf is the section heading of one kind.
func headingOf(kind extension.Kind) string {
	switch kind {
	case extension.KindConnector:
		return "Printer connectors"
	case extension.KindStorage:
		return "Storage backends"
	case extension.KindIdentity:
		return "Identity providers"
	case extension.KindEventSink:
		return "Event sinks"
	case extension.KindRoutes:
		return "Route sets"
	case extension.KindDatabase:
		return "Record backends"
	case extension.KindHistory:
		return "History providers"
	case extension.KindCamera:
		return "Camera sources"
	case extension.KindDeviceKind:
		return "Device classes"
	case extension.KindLink:
		return "Links"
	}

	return strings.ToUpper(kind.String()[:1]) + kind.String()[1:]
}

// contractOf is the package an author of one kind compiles against.
func contractOf(kind extension.Kind) string {
	switch kind {
	case extension.KindConnector:
		return "sdk/connectorapi"
	case extension.KindStorage:
		return "sdk/storageapi"
	case extension.KindIdentity:
		return "sdk/authapi"
	case extension.KindEventSink:
		return "sdk/eventsapi"
	case extension.KindRoutes:
		return "sdk/routesapi"
	case extension.KindDatabase:
		return "sdk/stateapi"
	case extension.KindHistory:
		return "sdk/historyapi"
	case extension.KindCamera:
		return "the application's resource registry"
	case extension.KindDeviceKind:
		return "sdk/connectorapi"
	case extension.KindLink:
		return "the application's resource registry"
	}

	return ""
}
