package fleet

import (
	"slices"
	"strings"
	"testing"
)

// orderingUniverse is the label set the ordering is proved total and transitive
// over.
//
// It is every spelling this matrix can be handed rather than every spelling
// that ships: releases, candidates for them, build metadata, a fourth
// component, and the labels the resolver refuses. A property proved on the
// committed table alone would be a property of the table.
//
//nolint:gochecknoglobals // a fixture, read-only after init.
var orderingUniverse = []string{
	// Releases the table names or could name.
	"6.4.0", "6.4.2", "6.5.0", "6.5.7", "6.6.0", "6.9.0", "6.10.0", "7.0.0",
	// Candidates, including the band floor spelling and both identifier kinds.
	"6.4.0-0", "6.5.0-0", "6.5.0-RC", "6.5.0-RC1", "6.5.0-RC2", "6.5.0-1", "6.5.7-RC",
	// Build metadata, which semver rule 10 excludes from precedence and which a
	// firmware really reports.
	"6.5.7+8628", "6.5.0+1", "6.5.0-RC+9",
	// Spellings the resolver reads by padding or truncating.
	"6.5", "6.5.0.1",
	// Spellings the resolver refuses. The empty prerelease tag is the one that
	// produces an intransitive triple under any ordering that is not total.
	"", "6.5.0-", "abc", "v6.5.0", "6.5.0 ", " 6.5.0", "6,5,0", "MK4",
	"99999999999999999999.0.0",
}

// TestTheMatrixOrderingIsATotalOrder proves by exhaustion what decides which
// cell gates every commit.
//
// sortedVersions picks the fast set's firmware by taking the last entry, so an
// ordering that is not total leaves that choice to array position, and one that
// is not transitive makes the sorted result depend on the input permutation.
// Both were true here: cmp("6.5.7", "6.5.7+8628") was 0, and 40 of 8000 triples
// over a twenty-label universe violated transitivity.
//
// Examples cannot show this. Every pair and every triple of the universe above
// is checked.
func TestTheMatrixOrderingIsATotalOrder(t *testing.T) {
	t.Parallel()

	labels := orderingUniverse

	t.Run("antisymmetric", func(t *testing.T) {
		t.Parallel()

		for _, a := range labels {
			for _, b := range labels {
				if forward, back := sign(compareVersions(a, b)), sign(compareVersions(b, a)); forward != -back {
					t.Errorf("%q against %q is %d and %q against %q is %d, and one is the negation of the "+
						"other", a, b, forward, b, a, back)
				}
			}
		}
	})

	t.Run("total", func(t *testing.T) {
		t.Parallel()

		for _, a := range labels {
			for _, b := range labels {
				same := compareVersions(a, b) == 0
				if same != (a == b) {
					t.Errorf("%q and %q compare equal = %v, and they are the same label = %v. Two distinct "+
						"labels that compare equal leave which one gates every commit to array position",
						a, b, same, a == b)
				}
			}
		}
	})

	t.Run("transitive", func(t *testing.T) {
		t.Parallel()

		violations := 0

		for _, a := range labels {
			for _, b := range labels {
				if compareVersions(a, b) >= 0 {
					continue
				}

				for _, c := range labels {
					if compareVersions(b, c) >= 0 || compareVersions(a, c) < 0 {
						continue
					}

					violations++

					if violations <= 5 {
						t.Errorf("%q < %q < %q and %q against %q is %d",
							a, b, c, a, c, sign(compareVersions(a, c)))
					}
				}
			}
		}

		if violations > 5 {
			t.Errorf("%d triples in all violate transitivity", violations)
		}

		if violations == 0 {
			t.Logf("observed: %d triples over %d labels, none intransitive",
				len(labels)*len(labels)*len(labels), len(labels))
		}
	})
}

// TestALabelTheResolverCannotReadSortsBelowEveryRelease holds the direction of
// the fallback.
//
// fastCell takes the last entry of the sorted list, so a label the resolver
// cannot read sorting above every release would put a firmware in no band at
// the head of the fast set: a cell whose name claims a model and a firmware
// while the printer under it runs the driver baseline. That is the phantom
// profile shape Range.Contains' own comment warns about, arriving through the
// test matrix. The bottom is where such a label can be seen and not selected.
func TestALabelTheResolverCannotReadSortsBelowEveryRelease(t *testing.T) {
	t.Parallel()

	unreadable := []string{"", "abc", "v6.5.0", "6.5.0 ", " 6.5.0", "6,5,0", "MK4", "6.5.0-",
		"99999999999999999999.0.0"}
	readable := []string{"6.4.0-0", "6.4.0", "6.5.0-RC", "6.5.0", "6.5.7", "6.10.0"}

	for _, label := range unreadable {
		if atOrAbove(label, label) {
			t.Fatalf("%q is readable to the resolver, so this case measures nothing", label)
		}

		for _, release := range readable {
			if got := sign(compareVersions(label, release)); got != -1 {
				t.Errorf("%q against %q is %d, and a label the resolver cannot read belongs below every "+
					"one it can", label, release, got)
			}
		}
	}

	ordered := sortedVersions(append(slices.Clone(unreadable), readable...))
	if last := ordered[len(ordered)-1]; last != "6.10.0" {
		t.Errorf("the newest of %v is %q, and the fast set runs the last entry", ordered, last)
	}

	t.Logf("observed: %v sorts to %v", append(slices.Clone(unreadable), readable...), ordered)
}

// TestTheFastSetDoesNotDependOnTheOrderTheTableIsWrittenIn is the consequence
// the ordering exists for, measured on the case that produced it.
//
// With ["6.5.0-RC", "6.5.7", "6.5.7+8628"] the fast set ran MK4@6.5.7+8628, and
// swapping the last two entries and changing nothing else it ran MK4@6.5.7.
// Every gate in the package passed both ways.
func TestTheFastSetDoesNotDependOnTheOrderTheTableIsWrittenIn(t *testing.T) {
	t.Parallel()

	table := []string{"6.5.0-RC", "6.5.7", "6.5.7+8628"}

	want := sortedVersions(table)

	for _, shuffled := range permutations(table) {
		if got := sortedVersions(shuffled); !slices.Equal(got, want) {
			t.Fatalf("%v sorts to %v and %v sorts to %v, so which cell gates every commit is decided by "+
				"the order the release table happens to be written in", shuffled, got, table, want)
		}
	}

	t.Logf("observed: every permutation of %v sorts to %v, so the fast set runs %s",
		table, want, want[len(want)-1])
}

// permutations returns every ordering of items. The sets it is used on are
// three long.
func permutations(items []string) [][]string {
	if len(items) <= 1 {
		return [][]string{slices.Clone(items)}
	}

	var out [][]string

	for i := range items {
		rest := slices.Concat(slices.Clone(items[:i]), slices.Clone(items[i+1:]))
		for _, tail := range permutations(rest) {
			out = append(out, append([]string{items[i]}, tail...))
		}
	}

	return out
}

// TestBuildMetadataIsAnOrderedSpellingAndNotASecondVersion records the decision
// the ordering takes about "+".
//
// Semver rule 10 excludes build metadata from precedence, and
// internal/printer/profile/version.go drops it for exactly that reason: a
// printer reporting "4.7.10+8628" resolves the profile "4.7.10" resolves. The
// matrix still has to choose one of the two spellings as the cell that gates a
// commit, and it chooses by text so that it chooses the same one every run. The
// two cells resolve to the same profile, so nothing is lost by running the
// longer spelling.
func TestBuildMetadataIsAnOrderedSpellingAndNotASecondVersion(t *testing.T) {
	t.Parallel()

	const bare, stamped = "6.5.7", "6.5.7+8628"

	if !atOrAbove(bare, stamped) || !atOrAbove(stamped, bare) {
		t.Fatalf("the resolver separates %q from %q, so this test describes the wrong rule", bare, stamped)
	}

	if got := sign(compareVersions(bare, stamped)); got != -1 {
		t.Errorf("%q against %q is %d, and the matrix orders two spellings of one precedence by text",
			bare, stamped, got)
	}

	ordered := sortedVersions([]string{stamped, bare})
	if want := []string{bare, stamped}; !slices.Equal(ordered, want) {
		t.Errorf("%v sorts to %v, want %v", []string{stamped, bare}, ordered, want)
	}

	t.Logf("observed: %s and %s are one precedence to the resolver and %s is the cell the fast set runs",
		bare, stamped, ordered[len(ordered)-1])
}

// TestTheOrderingUniverseCoversBothSidesOfTheFallback stops the exhaustive
// cases above quietly becoming one-sided.
//
// Every property they prove is a property of the labels they are given. A
// universe that drifted to readable labels only would prove the fallback is
// total by never reaching it.
func TestTheOrderingUniverseCoversBothSidesOfTheFallback(t *testing.T) {
	t.Parallel()

	var readable, unreadable []string

	for _, label := range orderingUniverse {
		if atOrAbove(label, label) {
			readable = append(readable, label)

			continue
		}

		unreadable = append(unreadable, label)
	}

	if len(readable) < 2 || len(unreadable) < 2 {
		t.Fatalf("the universe holds %d readable and %d unreadable labels, and the ordering has a rule "+
			"for each side and for the crossing", len(readable), len(unreadable))
	}

	if !slices.ContainsFunc(readable, func(label string) bool { return strings.Contains(label, "+") }) {
		t.Error("no readable label in the universe carries build metadata, which is the spelling that " +
			"made the ordering return 0 for two distinct labels")
	}

	t.Logf("observed: %d labels, %d readable and %d unreadable",
		len(orderingUniverse), len(readable), len(unreadable))
}
