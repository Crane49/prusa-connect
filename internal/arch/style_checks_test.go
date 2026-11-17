package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// AGENTS.md tells a reader how many checks `make style-check` runs, and which
// numbers they carry. A refusal names itself by number, so the sentence is how
// a person who is refused finds the rule behind it.
//
// That sentence is the shape of claim that rots first. A check is added to
// scripts/check-writing-style.sh in the commit that needs it, and the total
// lives in a file the author has no reason to open. Nothing else compares the
// two, so the count drifts by one per check and stays green.
const writingStyleScript = "scripts/check-writing-style.sh"

// styleCheckAnnouncement matches the line a check prints as it starts. That is
// the check running rather than the header comment describing it, so a check
// that lands without its comment is counted all the same.
var styleCheckAnnouncement = regexp.MustCompile(`(?m)^echo "([0-9]+)\. `)

func TestAgentsCountsTheWritingStyleChecksTheScriptRuns(t *testing.T) {
	root := repoRoot(t)

	script, err := os.ReadFile(filepath.Join(root, writingStyleScript))
	if err != nil {
		t.Fatalf("reading %s: %v", writingStyleScript, err)
	}

	var numbers []int

	for _, match := range styleCheckAnnouncement.FindAllStringSubmatch(string(script), -1) {
		n, convErr := strconv.Atoi(match[1])
		if convErr != nil {
			t.Fatalf("%s announces check %q, which is not a number", writingStyleScript, match[1])
		}

		numbers = append(numbers, n)
	}

	requireScanned(t, "checks announced by "+writingStyleScript, len(numbers), 10,
		`Each check opens with echo "N. ...". A check that announces itself another way is invisible here.`)

	sort.Ints(numbers)

	want := fmt.Sprintf("%s checks, numbered %s", spelledOut(len(numbers)), numberRanges(numbers))

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}

	if !strings.Contains(string(agents), want) {
		t.Errorf("AGENTS.md does not say %q.\n"+
			"%s runs %d checks and numbers them %s.\n"+
			"A refusal names its own number, so the count and the numbering are how a\n"+
			"refused author reaches the rule that refused them.",
			want, writingStyleScript, len(numbers), numberRanges(numbers))
	}
}

// numberRanges renders a sorted set as its runs of consecutive numbers, in the
// form a sentence takes: 1 to 9 and 12 to 16. A run of one is its own number.
func numberRanges(numbers []int) string {
	var runs []string

	for i := 0; i < len(numbers); {
		end := i
		for end+1 < len(numbers) && numbers[end+1] == numbers[end]+1 {
			end++
		}

		if end == i {
			runs = append(runs, strconv.Itoa(numbers[i]))
		} else {
			runs = append(runs, fmt.Sprintf("%d to %d", numbers[i], numbers[end]))
		}

		i = end + 1
	}

	switch len(runs) {
	case 1:
		return runs[0]
	case 2:
		return runs[0] + " and " + runs[1]
	default:
		return strings.Join(runs[:len(runs)-1], ", ") + " and " + runs[len(runs)-1]
	}
}

// spelledOut gives the count as the word a sentence takes, so the expected
// string this gate builds is the string a writer would have written. The range
// beside it carries the digits, which is where a refused author looks.
func spelledOut(n int) string {
	words := []string{
		"zero", "one", "two", "three", "four", "five", "six", "seven", "eight",
		"nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen",
		"sixteen", "seventeen", "eighteen", "nineteen", "twenty",
	}

	if n >= 0 && n < len(words) {
		return words[n]
	}

	return strconv.Itoa(n)
}
