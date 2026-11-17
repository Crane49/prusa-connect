package arch_test

import (
	"fmt"
	"strings"
	"testing"
)

// The arming check, under one name.
//
// A gate reports green over nothing when its input set is empty and nothing
// says so. Its green then carries two meanings at once: nothing was found, and
// nothing was looked at. Ten gates in this repository have been in that state,
// and the old backlog lists them with the
// mutation that exposed each one.
//
// The repair had been written seven times in this package under six spellings,
// so no search found them all and the next author wrote it again. Earlier research
// section 3.1 measured that and named the helper. The name is the whole point:
// `grep -rn requireScanned internal/arch` is the list.
//
// requireScanned fails a gate whose input set is smaller than the floor it
// proves anything over.
//
// what names the input set in the plural, because the count in front of it is
// usually zero: "Go files under adapters/storage", "commit messages",
// "registered types".
//
// n is the size of the gate's INPUT set, never the size of what it found. A
// gate counting its violations is measuring the tree. A gate counting its
// inputs is measuring itself, and only the second one can tell a clean tree
// from an empty read. TestTheRepositoryRootCarriesNoBuildArtefacts expects zero
// findings on every healthy run, and still owes a floor on the entries it read.
//
// remedy is what the author should do about it, in one sentence. It is optional
// because most sites have nothing to add beyond the count.
func requireScanned(t *testing.T, what string, n, floor int, remedy ...string) {
	t.Helper()

	if refusal := unscanned(t.Name(), what, n, floor, strings.Join(remedy, " ")); refusal != "" {
		t.Fatal(refusal)
	}

	t.Logf("%s read %d %s, against a floor of %d", t.Name(), n, what, floor)
}

// unscanned is requireScanned's decision, and returns the refusal or an empty
// string. It is separate so that TestAGateThatReadNothingIsRefused can read the
// decision without a testing.TB it has to fail on purpose.
func unscanned(gate, what string, n, floor int, remedy string) string {
	if floor < 1 {
		return fmt.Sprintf(
			"%s gives its %s a floor of %d.\n"+
				"Zero is the number a gate that stopped reading scores, so a floor there refuses nothing.\n"+
				"A set that is legitimately empty is a claim, and it goes in a named list the gate checks.",
			gate, what, floor)
	}

	if n >= floor {
		return ""
	}

	refusal := fmt.Sprintf(
		"%s read %d %s, under its floor of %d, so this run proved nothing.\n"+
			"A green gate over an empty input set says \"nothing was found\" and \"nothing was\n"+
			"looked at\" in the same word. Either the input moved, or the scan stopped reaching it.",
		gate, n, what, floor)

	if remedy != "" {
		refusal += "\n" + remedy
	}

	return refusal
}

// TestAGateThatReadNothingIsRefused is the mutation check for the arming check
// itself. Every other gate here delegates its arming to requireScanned, so a
// requireScanned that refused nothing would take all of them with it.
func TestAGateThatReadNothingIsRefused(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		n, floor int
		refuse   bool
	}{
		"an empty input set under a floor of one":  {n: 0, floor: 1, refuse: true},
		"an input set that shrank under its floor": {n: 12, floor: 40, refuse: true},
		"a floor of zero, which refuses nothing":   {n: 0, floor: 0, refuse: true},
		"a negative floor":                         {n: 5, floor: -1, refuse: true},
		"an input set exactly at its floor":        {n: 1, floor: 1, refuse: false},
		"an input set over its floor":              {n: 400, floor: 1, refuse: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			refusal := unscanned("TestSomething", "files", c.n, c.floor, "")

			if c.refuse && refusal == "" {
				t.Errorf("%d read against a floor of %d was accepted, and it proves nothing", c.n, c.floor)
			}

			if !c.refuse && refusal != "" {
				t.Errorf("%d read against a floor of %d was refused:\n%s", c.n, c.floor, refusal)
			}
		})
	}
}

// TestTheRefusalNamesTheGateAndTheRemedy holds the two things the message is
// for. A refusal that names neither sends its reader to grep for which gate
// stopped reading.
func TestTheRefusalNamesTheGateAndTheRemedy(t *testing.T) {
	t.Parallel()

	refusal := unscanned("TestTheThing", "registered types", 0, 1, "Run make generate-extensions.")

	for _, want := range []string{"TestTheThing", "registered types", "Run make generate-extensions."} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, refusal)
		}
	}
}
