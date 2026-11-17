package deviceissue

import (
	"regexp"
	"strings"
	"testing"
)

// tokenPattern is how a reason is spelled on this surface: lower case words
// joined by underscores, the spelling `departed` and `session_ended` use on a
// device record.
var tokenPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// everyReason is the closed set written out by hand, so a reason added to the
// table and not to this list fails instead of being tested against itself.
//
//nolint:gochecknoglobals // the expected set for the tests below.
var everyReason = []Reason{
	ReasonAwaitingOperator,
	ReasonDeparted,
	ReasonDisconnected,
	ReasonFramesRefused,
	ReasonTelemetryDropped,
	ReasonNoCapabilityProfile,
}

// A reason a device record can carry and this table cannot describe reaches a
// client as a token with no sentence, and the client has nothing to show a
// person.
func TestEveryReasonHasADescriptor(t *testing.T) {
	t.Parallel()

	for _, reason := range everyReason {
		doc, ok := Lookup(reason)
		if !ok {
			t.Errorf("%s is in the closed set and has no descriptor", reason)

			continue
		}

		if !doc.Severity.Valid() {
			t.Errorf("%s carries severity %q, which is not a declared severity", reason, doc.Severity)
		}

		for _, field := range []struct {
			name  string
			value string
		}{
			{"summary", doc.Summary},
			{"description", doc.Description},
			{"action", doc.Action},
		} {
			if strings.TrimSpace(field.value) == "" {
				t.Errorf("%s has no %s", reason, field.name)
			}
		}

		if doc.Version != VocabularyVersion {
			t.Errorf("%s carries version %q, want %q", reason, doc.Version, VocabularyVersion)
		}
	}
}

// A descriptor left behind by a retired reason publishes a token nothing
// raises.
func TestTheDescriptorTableIsTheClosedSet(t *testing.T) {
	t.Parallel()

	reasons := Reasons()
	if len(reasons) != len(everyReason) {
		t.Fatalf("the table holds %d reasons, the test names %d", len(reasons), len(everyReason))
	}

	for i, reason := range everyReason {
		if reasons[i] != reason {
			t.Errorf("entry %d is %s, want %s", i, reasons[i], reason)
		}
	}
}

// The table order is the order an issue list is built in. A person reads the
// first entry on a device record first, so putting an informational reason
// above a stopped machine would change what a UI shows.
func TestTheTableIsInDescendingSeverityOrder(t *testing.T) {
	t.Parallel()

	rank := map[Severity]int{}
	for i, severity := range Severities() {
		rank[severity] = i
	}

	previous := len(rank)

	for _, doc := range Docs() {
		current, ok := rank[doc.Severity]
		if !ok {
			t.Fatalf("%s carries severity %q, which Severities does not list", doc.Reason, doc.Severity)
		}

		if current > previous {
			t.Errorf("%s is severity %s and follows a lower one", doc.Reason, doc.Severity)
		}

		previous = current
	}
}

// One spelling on the wire. A reason is read beside `departed` and
// `session_ended` on the same record, and a value in a second casing makes a
// client branch on the set twice.
func TestEveryReasonIsSpelledAsAToken(t *testing.T) {
	t.Parallel()

	seen := map[Reason]bool{}

	for _, reason := range Reasons() {
		if !tokenPattern.MatchString(reason.String()) {
			t.Errorf("%q is not a lower-case underscore token", reason)
		}

		if seen[reason] {
			t.Errorf("%q is declared twice", reason)
		}

		seen[reason] = true
	}
}

// A Valid that answered true for anything would let a projection publish a
// token no client can look up, and SeverityOf would invent a severity beside
// it.
func TestAReasonOutsideTheSetIsRefused(t *testing.T) {
	t.Parallel()

	for _, reason := range []Reason{"", "AwaitingOperator", "awaiting-operator", "on_fire"} {
		if reason.Valid() {
			t.Errorf("%q is not in the closed set and reads as valid", reason)
		}

		if got := SeverityOf(reason); got != "" {
			t.Errorf("%q is not in the closed set and carries severity %q", reason, got)
		}
	}

	for _, reason := range Reasons() {
		if !reason.Valid() {
			t.Errorf("%q is in the closed set and reads as invalid", reason)
		}

		if got := SeverityOf(reason); !got.Valid() {
			t.Errorf("%q carries severity %q, which is not a declared severity", reason, got)
		}
	}
}

// The same pair of checks for the severity.
func TestASeverityOutsideTheSetIsRefused(t *testing.T) {
	t.Parallel()

	for _, severity := range []Severity{"", "critical", "Info", "fatal"} {
		if severity.Valid() {
			t.Errorf("%q is not in the closed set and reads as valid", severity)
		}
	}

	for _, severity := range Severities() {
		if !severity.Valid() {
			t.Errorf("%q is in the closed set and reads as invalid", severity)
		}
	}
}

// A caller must not be able to edit the table every other reader shares. The
// endpoint, the page and the device plane all read it, and returning the slice
// itself is one append away from a published vocabulary nobody declared.
func TestTheAccessorsHandBackACopy(t *testing.T) {
	t.Parallel()

	first := Docs()
	first[0].Summary = "edited"

	if Docs()[0].Summary == "edited" {
		t.Error("Docs hands out the table itself")
	}

	reasons := Reasons()
	reasons[0] = "edited"

	if Reasons()[0] == "edited" {
		t.Error("Reasons hands out the table itself")
	}

	all := Severities()
	all[0] = "edited"

	if Severities()[0] == "edited" {
		t.Error("Severities hands out the table itself")
	}
}

// The reserved paths stay in one subtree. The host reads a dialog to say a
// device is waiting for a person, and a path outside the subtree is one a
// driver fills in and nothing reads.
func TestTheDialogPathsShareOneSubtree(t *testing.T) {
	t.Parallel()

	for _, path := range []string{DialogIDPath, DialogTitlePath, DialogTextPath, DialogCodePath, DialogButtonsPath} {
		if !strings.HasPrefix(path, DialogSubtree) {
			t.Errorf("%q is not under the reserved dialog subtree", path)
		}
	}
}
