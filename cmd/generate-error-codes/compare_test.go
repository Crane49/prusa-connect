package main

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func TestDiffRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		committed   []string
		current     []string
		wantRemoved []string
		wantAdded   []string
	}{
		{
			name:      "unchanged registry",
			committed: []string{"core.job/DUPLICATE_COMMAND", "core.storage/NO_SPACE"},
			current:   []string{"core.job/DUPLICATE_COMMAND", "core.storage/NO_SPACE"},
		},
		{
			name:        "code vanished since the committed release",
			committed:   []string{"core.job/DUPLICATE_COMMAND", "core.storage/NO_SPACE"},
			current:     []string{"core.storage/NO_SPACE"},
			wantRemoved: []string{"core.job/DUPLICATE_COMMAND"},
		},
		{
			name:      "code added",
			committed: []string{"core.storage/NO_SPACE"},
			current:   []string{"core.printer/NOT_FOUND", "core.storage/NO_SPACE"},
			wantAdded: []string{"core.printer/NOT_FOUND"},
		},
		{
			name:        "one code replaced by another",
			committed:   []string{"core.storage/NO_SPACE"},
			current:     []string{"core.storage/OUT_OF_SPACE"},
			wantRemoved: []string{"core.storage/NO_SPACE"},
			wantAdded:   []string{"core.storage/OUT_OF_SPACE"},
		},
		{
			name:      "first run, nothing committed yet",
			committed: nil,
			current:   []string{"core.storage/NO_SPACE"},
			wantAdded: []string{"core.storage/NO_SPACE"},
		},
		{
			name:        "every code removed",
			committed:   []string{"core.job/DUPLICATE_COMMAND", "core.storage/NO_SPACE"},
			current:     nil,
			wantRemoved: []string{"core.job/DUPLICATE_COMMAND", "core.storage/NO_SPACE"},
		},
		{
			name:        "result does not depend on input order",
			committed:   []string{"core.storage/NO_SPACE", "core.job/DUPLICATE_COMMAND", "core.auth/TOKEN_INVALID"},
			current:     []string{"core.printer/NOT_FOUND", "core.auth/TOKEN_INVALID"},
			wantRemoved: []string{"core.job/DUPLICATE_COMMAND", "core.storage/NO_SPACE"},
			wantAdded:   []string{"core.printer/NOT_FOUND"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			diff := diffRegistry(entriesFor(t, test.committed), entriesFor(t, test.current))

			if got := codeStrings(diff.Removed); !slices.Equal(got, test.wantRemoved) {
				t.Errorf("Removed = %v, want %v", got, test.wantRemoved)
			}

			if got := codeStrings(diff.Added); !slices.Equal(got, test.wantAdded) {
				t.Errorf("Added = %v, want %v", got, test.wantAdded)
			}
		})
	}
}

func TestCheckRemovals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		removed       []string
		allowRemovals bool
		wantErr       bool
	}{
		{name: "nothing removed", removed: nil, wantErr: false},
		{name: "a code vanished", removed: []string{"core.storage/NO_SPACE"}, wantErr: true},
		{
			name:          "a deliberate retirement",
			removed:       []string{"core.storage/NO_SPACE"},
			allowRemovals: true,
			wantErr:       false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := checkRemovals(registryDiff{Removed: entriesFor(t, test.removed)}, test.allowRemovals)

			if gotErr := err != nil; gotErr != test.wantErr {
				t.Fatalf("checkRemovals() error = %v, want error %v", err, test.wantErr)
			}

			if test.wantErr && !errors.Is(err, errCodesRemoved) {
				t.Fatalf("checkRemovals() error = %v, want errCodesRemoved", err)
			}
		})
	}
}

// The vanished-code gate reads the artefact it wrote last time, so the file
// format has to survive the round trip.
func TestRegistryFileRoundTrip(t *testing.T) {
	t.Parallel()

	registry := fault.NewRegistry()
	registry.Register(fault.DomainStorage(), "NO_SPACE", 507, fault.Document(
		"The storage ran out of space",
		"Free space and try again.",
		fault.RetryNever,
		fault.ShowDetail,
		"storage_id",
	))

	snapshot, err := registry.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	content, err := renderRegistryFile(snapshot)
	if err != nil {
		t.Fatalf("renderRegistryFile() error = %v", err)
	}

	path := filepath.Join(t.TempDir(), "error-codes.json")
	if err = codegen.WriteFile(path, content); err != nil {
		t.Fatalf("codegen.WriteFile() error = %v", err)
	}

	entries, err := readRegistryFile(path)
	if err != nil {
		t.Fatalf("readRegistryFile() error = %v", err)
	}

	if diff := diffRegistry(entries, registry.All()); len(diff.Removed) > 0 || len(diff.Added) > 0 {
		t.Fatalf("round trip lost codes: removed %v, added %v", codeStrings(diff.Removed), codeStrings(diff.Added))
	}

	if err = compareFile(path, content); err != nil {
		t.Fatalf("compareFile() error = %v", err)
	}
}

func TestCompareFileRejectsAStaleArtefact(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "error-codes.json")
	if err := codegen.WriteFile(path, []byte("stale\n")); err != nil {
		t.Fatalf("codegen.WriteFile() error = %v", err)
	}

	if err := compareFile(path, []byte("fresh\n")); !errors.Is(err, errStale) {
		t.Fatalf("compareFile() error = %v, want errStale", err)
	}

	missing := filepath.Join(t.TempDir(), "absent.json")
	if err := compareFile(missing, []byte("fresh\n")); !errors.Is(err, errStale) {
		t.Fatalf("compareFile() on a missing file error = %v, want errStale", err)
	}
}

// entriesFor turns code text into entries. Only the code is read by the
// comparison, so the rest of the entry stays zero.
func entriesFor(t *testing.T, codes []string) []fault.Entry {
	t.Helper()

	if len(codes) == 0 {
		return nil
	}

	entries := make([]fault.Entry, 0, len(codes))

	for _, text := range codes {
		code, err := fault.ParseCode(text)
		if err != nil {
			t.Fatalf("ParseCode(%q) error = %v", text, err)
		}

		entries = append(entries, fault.Entry{Code: code})
	}

	return entries
}
