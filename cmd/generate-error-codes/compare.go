package main

import (
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// registryDiff is the difference between the committed codes and the
// registered ones. A removal breaks a client that still branches on the code.
type registryDiff struct {
	Removed []fault.Entry
	Added   []fault.Entry
}

// diffRegistry compares the two lists by code only. A changed wording, status
// or params shows up in the -check byte comparison. The result is sorted.
func diffRegistry(committed, current []fault.Entry) registryDiff {
	committedCodes := codeSet(committed)
	currentCodes := codeSet(current)

	diff := registryDiff{}

	for _, entry := range committed {
		if _, ok := currentCodes[entry.Code]; !ok {
			diff.Removed = append(diff.Removed, entry)
		}
	}

	for _, entry := range current {
		if _, ok := committedCodes[entry.Code]; !ok {
			diff.Added = append(diff.Added, entry)
		}
	}

	sortByCode(diff.Removed)
	sortByCode(diff.Added)

	return diff
}

func codeSet(entries []fault.Entry) map[fault.Code]struct{} {
	set := make(map[fault.Code]struct{}, len(entries))
	for _, entry := range entries {
		set[entry.Code] = struct{}{}
	}

	return set
}

func sortByCode(entries []fault.Entry) {
	slices.SortFunc(entries, func(a, b fault.Entry) int {
		return strings.Compare(a.Code.String(), b.Code.String())
	})
}

func codeStrings(entries []fault.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Code.String())
	}

	return out
}
