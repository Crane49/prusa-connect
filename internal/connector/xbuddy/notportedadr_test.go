package xbuddy

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// adrPath is the record whose open items publish the verbs this driver cannot
// send, read from this package because this package owns the list.
var adrPath = filepath.Join("..", "..", "..", "docs", "decisions",
	"20260825-dee-the-capability-vocabulary.md")

// openItemMarker picks the one bullet that publishes the list. It is a phrase
// from the bullet rather than a position, so a reordered open items section
// still finds it and a deleted bullet fails rather than matching another one.
const openItemMarker = "have no wire form in"

// shoutedVerb is a wire command name inside a code span, which is how the
// record spells one.
var shoutedVerb = regexp.MustCompile("`([A-Z][A-Z0-9_]+)`")

// The record's count of verbs with no wire form is read off notPorted rather
// than written by hand.
//
// It was published as eight and then as six while the list held five. A count
// in prose beside a list in code is two copies of one fact, and the prose copy
// is the one no test opens. This opens it.
func TestTheOpenItemNamesTheVerbsWithNoWireForm(t *testing.T) {
	t.Parallel()

	bullet := openItemBullet(t)

	var published []string

	for _, match := range shoutedVerb.FindAllStringSubmatch(bullet, -1) {
		published = append(published, match[1])
	}

	if len(published) == 0 {
		t.Fatalf("the %q bullet in %s names no verb, so this test compared an empty list against %d",
			openItemMarker, adrPath, len(notPorted))
	}

	slices.Sort(published)

	expected := slices.Clone(notPorted)
	slices.Sort(expected)

	if !slices.Equal(published, expected) {
		t.Errorf("%s publishes %v as the verbs with no wire form and this package sends none of %v.\n"+
			"A reader is told a verb is missing that is there, or is not told about one that is.",
			adrPath, published, expected)
	}

	if count := countWord(bullet); count != len(notPorted) {
		t.Errorf("%s opens the bullet with a count of %d and the list holds %d verbs",
			adrPath, count, len(notPorted))
	}
}

// countWord is the number the bullet opens with, as the record spells it. A
// count that disagrees with the list is the drift this test exists to refuse.
func countWord(bullet string) int {
	words := []string{"", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine"}

	for value, word := range words {
		if value > 0 && strings.HasPrefix(bullet, word+" ") {
			return value
		}
	}

	return -1
}

// openItemBullet is the text of the one open item that publishes the list,
// from its marker to the next bullet.
func openItemBullet(t *testing.T) string {
	t.Helper()

	source, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("reading %s: %v", adrPath, err)
	}

	var (
		bullet  strings.Builder
		reading bool
		found   bool
	)

	for line := range strings.SplitSeq(string(source), "\n") {
		opens := strings.HasPrefix(line, "- ")

		if reading && opens {
			break
		}

		if opens {
			reading = false
		}

		if opens && strings.Contains(line, openItemMarker) {
			reading = true
			found = true
		}

		if reading {
			bullet.WriteString(strings.TrimPrefix(strings.TrimSpace(line), "- "))
			bullet.WriteString(" ")
		}
	}

	if !found {
		t.Fatalf("no bullet in %s contains %q, so the record no longer publishes the list and this "+
			"test would pass over nothing", adrPath, openItemMarker)
	}

	return strings.TrimSpace(bullet.String())
}
