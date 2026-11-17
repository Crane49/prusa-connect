package docsref_test

import (
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/docsref"
)

// TestAnchorIsTheIdentifierAReaderHolds pins the anchor of every shape of
// identifier the six vocabularies publish. ADR-20260826-swm rule 4: an anchor that
// moves is a broken link in somebody's incident notes, and nothing else in the
// build would notice.
func TestAnchorIsTheIdentifierAReaderHolds(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"core.printer/GCODE_NOT_ALLOWED":          "core-printer-gcode-not-allowed",
		"core.auth/ACCESS_DENIED":                 "core-auth-access-denied",
		"core.command.temperature.set":            "core-command-temperature-set",
		"com.prusa.connect.printer.state.changed": "com-prusa-connect-printer-state-changed",
		"printer":                                "printer",
		"core.feature.raw_gcode":                 "core-feature-raw-gcode",
		"read_telemetry":                         "read-telemetry",
		"filesystem/storage.read":                "filesystem-storage-read",
		"UPPER":                                  "upper",
		"double//separator":                      "double-separator",
		"trailing.":                              "trailing",
		".leading":                               "leading",
		"core.storage/UPLOAD_TOO_LARGE":          "core-storage-upload-too-large",
		"core.extension/CONTRACT_MAJOR_MISMATCH": "core-extension-contract-major-mismatch",
		"core.command.axis.home:arg:axis":        "core-command-axis-home-arg-axis",
		"printer.state":                          "printer-state",
	}

	for identifier, want := range cases {
		t.Run(identifier, func(t *testing.T) {
			t.Parallel()

			if got := docsref.Anchor(identifier); got != want {
				t.Errorf("Anchor(%q) = %q, want %q", identifier, got, want)
			}
		})
	}
}

// TestAnchorDependsOnNothingButTheIdentifier is the property the table above
// only samples: no neighbour, no ordering and no counter feeds the result, so
// regenerating the page cannot move an anchor.
func TestAnchorDependsOnNothingButTheIdentifier(t *testing.T) {
	t.Parallel()

	const identifier = "core.printer/GCODE_NOT_ALLOWED"

	first := docsref.Anchor(identifier)

	for range 100 {
		_ = docsref.Anchor("core.auth/ACCESS_DENIED")

		if again := docsref.Anchor(identifier); again != first {
			t.Fatalf("Anchor is not pure: %q then %q", first, again)
		}
	}
}

// TestAnchorUnderSeparatesTwoSetsThatShareAName is why the authorization page
// needs a second entry point: it publishes actions, types and relations on one
// page, and `read` is all three.
func TestAnchorUnderSeparatesTwoSetsThatShareAName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind       string
		identifier string
		want       string
	}{
		{kind: "action", identifier: "read", want: "action-read"},
		{kind: "relation", identifier: "read", want: "relation-read"},
		{kind: "type", identifier: "printer", want: "type-printer"},
		{kind: "action", identifier: "read_telemetry", want: "action-read-telemetry"},
		{kind: "relation", identifier: "parent_group", want: "relation-parent-group"},
		{kind: "type", identifier: "service_account", want: "type-service-account"},
		{kind: "action", identifier: ".leading", want: "action-leading"},
	}

	for _, c := range cases {
		t.Run(c.kind+"/"+c.identifier, func(t *testing.T) {
			t.Parallel()

			if got := docsref.AnchorUnder(c.kind, c.identifier); got != c.want {
				t.Errorf("AnchorUnder(%q, %q) = %q, want %q", c.kind, c.identifier, got, c.want)
			}
		})
	}
}

// TestAnchorUnderIsTheKindThenTheAnchor states the relationship the two
// functions hold, which is what lets one loop serve both without either page's
// links moving.
func TestAnchorUnderIsTheKindThenTheAnchor(t *testing.T) {
	t.Parallel()

	for _, identifier := range []string{
		"read", "read_telemetry", "core.printer/GCODE_NOT_ALLOWED", "UPPER", ".leading", "trailing.",
	} {
		want := "action-" + docsref.Anchor(identifier)
		if got := docsref.AnchorUnder("action", identifier); got != want {
			t.Errorf("AnchorUnder(action, %q) = %q, want %q", identifier, got, want)
		}
	}
}

// TestAnchorCarriesNothingAMarkdownLinkCannot is the property that makes rule 3
// work at all: a reader pastes the result after a `#` and it survives.
func TestAnchorCarriesNothingAMarkdownLinkCannot(t *testing.T) {
	t.Parallel()

	for _, identifier := range []string{
		"core.printer/GCODE_NOT_ALLOWED",
		"a b\tc\nd",
		"path/to/../thing",
		"quote\"and'apostrophe",
		"<script>alert(1)</script>",
		"per%20cent",
		"emoji ☃ snowman",
	} {
		anchor := docsref.Anchor(identifier)
		for _, char := range anchor {
			if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
				continue
			}

			t.Errorf("Anchor(%q) = %q, which carries %q", identifier, anchor, string(char))
		}

		if strings.HasPrefix(anchor, "-") {
			t.Errorf("Anchor(%q) = %q, which starts with a separator", identifier, anchor)
		}
	}
}

// TestEscapeProseNeutralisesMarkupADescriptorCarries is ADR-20260825-rre rule 15 at
// render: markdown carries html through, so a tag a third-party descriptor
// wrote would run on the page every plugin author reads.
//
// The angle bracket that opens a tag is what is escaped, along with the
// ampersand that would otherwise let a text entity reintroduce it. A closing
// bracket with no opener is text to every parser, so it is left as it is.
func TestEscapeProseNeutralisesMarkupADescriptorCarries(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"<script>alert(1)</script>": "&lt;script>alert(1)&lt;/script>",
		"a & b":                     "a &amp; b",
		"&lt;":                      "&amp;lt;",
		"<img src=x onerror=1>":     "&lt;img src=x onerror=1>",
		"&#60;script&#62;":          "&amp;#60;script&amp;#62;",
		"plain prose, no markup":    "plain prose, no markup",
		"":                          "",
	}

	for text, want := range cases {
		if got := docsref.EscapeProse(text); got != want {
			t.Errorf("EscapeProse(%q) = %q, want %q", text, got, want)
		}
	}
}

// TestEscapeSpanKeepsAValueInsideItsCell holds the escape an injection finding
// put here: a pipe in a third-party value ends the cell it is published in.
func TestEscapeSpanKeepsAValueInsideItsCell(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a|b":               `a\|b`,
		"|":                 `\|`,
		"a||b":              `a\|\|b`,
		"no pipe here":      "no pipe here",
		"github.com/acme/x": "github.com/acme/x",
		"":                  "",
	}

	for text, want := range cases {
		if got := docsref.EscapeSpan(text); got != want {
			t.Errorf("EscapeSpan(%q) = %q, want %q", text, got, want)
		}
	}
}

// TestYesNoAndOrNoneSurviveBeingReadAloud is ADR-20260826-swm rule 6: a value carried
// by a blank or by a colour is refused, so the renderer says the word.
func TestYesNoAndOrNoneSurviveBeingReadAloud(t *testing.T) {
	t.Parallel()

	if got := docsref.YesNo(true); got != "yes" {
		t.Errorf("YesNo(true) = %q, want %q", got, "yes")
	}

	if got := docsref.YesNo(false); got != "no" {
		t.Errorf("YesNo(false) = %q, want %q", got, "no")
	}

	if got := docsref.OrNone(""); got != docsref.None {
		t.Errorf("OrNone(%q) = %q, want %q", "", got, docsref.None)
	}

	if docsref.None != "none" {
		t.Errorf("None = %q, want %q", docsref.None, "none")
	}

	if got := docsref.OrNone("core.printer"); got != "`core.printer`" {
		t.Errorf("OrNone(%q) = %q, want %q", "core.printer", got, "`core.printer`")
	}

	if got := docsref.OrNone("a|b"); got != "`a\\|b`" {
		t.Errorf("OrNone(%q) = %q, want %q", "a|b", got, "`a\\|b`")
	}
}
