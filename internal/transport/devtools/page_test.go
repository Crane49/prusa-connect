package devtools_test

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/devtools"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// inlineBlock pulls one element's contents out of the rendered page, which is
// what a browser hashes when it evaluates a hash-source policy.
func inlineBlock(t *testing.T, body, tag string) string {
	t.Helper()

	pattern := regexp.MustCompile(`(?s)<` + tag + `>(.*?)</` + tag + `>`)

	found := pattern.FindStringSubmatch(body)
	require.Len(t, found, 2, "the page carries exactly one <%s> block", tag)

	return found[1]
}

func hashSource(body string) string {
	sum := sha256.Sum256([]byte(body))

	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

// TestThePolicyNamesTheScriptAndStyleThePageActuallyCarries is the gate on the
// only defence this page has. default-src 'none' plus two hash sources means a
// browser runs the script this binary shipped and nothing else; a hash computed
// over different bytes than the ones in the document leaves a page whose script
// never runs, and the failure is silent in the browser console.
func TestThePolicyNamesTheScriptAndStyleThePageActuallyCarries(t *testing.T) {
	t.Parallel()

	page := devtools.Rendered()
	body := string(page.Body)

	require.Contains(t, page.CSP, "default-src 'none'")
	require.Contains(t, page.CSP, "connect-src 'self'", "the live wire and every fetch go to this origin")

	require.Contains(t, page.CSP, "script-src '"+hashSource(inlineBlock(t, body, "script"))+"'")
	require.Contains(t, page.CSP, "style-src '"+hashSource(inlineBlock(t, body, "style"))+"'")
}

// TestThePageCarriesNoMarkerAndNoOffOriginReference holds the two ways this
// document stops being self-contained: a marker that was never replaced, so the
// page renders with no behaviour at all, and a reference to a host the policy
// would refuse anyway, which is a panel that is permanently empty.
func TestThePageCarriesNoMarkerAndNoOffOriginReference(t *testing.T) {
	t.Parallel()

	body := string(devtools.Rendered().Body)

	require.NotContains(t, body, "<!--style-->")
	require.NotContains(t, body, "<!--script-->")
	require.NotContains(t, body, "http://")
	require.NotContains(t, body, "https://")
	require.NotContains(t, body, "//cdn.")
}

// TestTheSourceFilesAreTheOnesThePageServes fails when the markup, the
// stylesheet or any script file is edited without the binary being rebuilt
// around it, which is the one way the embedded copy and the file on disk can
// disagree.
//
// The script is several files in one block, so the stylesheet is compared whole
// and each script file is required to be inside the block it was concatenated
// into. A file that was edited and not rebuilt fails here rather than shipping.
func TestTheSourceFilesAreTheOnesThePageServes(t *testing.T) {
	t.Parallel()

	body := string(devtools.Rendered().Body)

	onDisk, err := os.ReadFile("page.css")
	require.NoError(t, err)
	require.Equal(t, string(onDisk), inlineBlock(t, body, "style"))

	script := inlineBlock(t, body, "script")
	require.Greater(t, len(devtools.ScriptFiles), 1, "the console is one file, so this gate watches one")

	for _, name := range devtools.ScriptFiles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source, readErr := os.ReadFile(name)
			require.NoError(t, readErr)
			require.Contains(t, script, string(source))
		})
	}
}

// TestEveryScriptFileInThisDirectoryIsServed is the gate on the list itself.
//
// A file added beside the others and not named in ScriptFiles is a panel that is
// written, reviewed and never runs, and nothing about the page says so: the
// markup declares the elements it fills, the elements stay empty, and an empty
// panel reads as a server with nothing to report.
func TestEveryScriptFileInThisDirectoryIsServed(t *testing.T) {
	t.Parallel()

	found, err := filepath.Glob("*.js")
	require.NoError(t, err)
	require.NotEmpty(t, found)

	served := make(map[string]bool, len(devtools.ScriptFiles))
	for _, name := range devtools.ScriptFiles {
		served[name] = true
	}

	for _, name := range found {
		require.Truef(t, served[name], "%s is in this package and no page serves it", name)
	}

	require.Len(t, found, len(devtools.ScriptFiles), "a served file is not in this directory")
}

// consoleScript is every script file joined, which is what the browser runs. The
// gates below read it rather than page.js: a rule that held for one file and not
// for the five beside it is a rule that stopped being enforced when the console
// was split up.
func consoleScript(t *testing.T) string {
	t.Helper()

	var joined strings.Builder

	for _, name := range devtools.ScriptFiles {
		source, err := os.ReadFile(name)
		require.NoError(t, err)
		joined.Write(source)
	}

	return joined.String()
}

// TestTheEntityTagFollowsTheBytes is what makes a reload during a debugging
// session cost one conditional request. A tag that did not follow the content
// would serve yesterday's page from a browser cache after a rebuild, which is
// the worst possible failure for a tool that tells one build from another.
func TestTheEntityTagFollowsTheBytes(t *testing.T) {
	t.Parallel()

	page := devtools.Rendered()

	require.True(t, strings.HasPrefix(page.ETag, `"`) && strings.HasSuffix(page.ETag, `"`),
		"an entity-tag is a quoted string, RFC 9110 section 8.8.3")
	require.NotEqual(t, `""`, page.ETag)

	require.Equal(t, page.ETag, devtools.Rendered().ETag, "the same bytes keep the same tag")
	require.NotContains(t, page.ETag[1:len(page.ETag)-1], `"`, "the opaque value has no escape")
}

// TestThePageRoutesOnlyOnDeclaredEventTypes is the drift gate between the page
// and the vocabulary. The page routes on type names it spells itself, so a type
// renamed, retired or misspelled leaves a panel that silently stops updating,
// which is the failure this view exists to make visible.
func TestThePageRoutesOnlyOnDeclaredEventTypes(t *testing.T) {
	t.Parallel()

	script := consoleScript(t)

	named := regexp.MustCompile(regexp.QuoteMeta(event.CoreNamespace)+`[a-z0-9._-]+`).
		FindAllString(script, -1)
	require.NotEmpty(t, named, "the page names no core event type, so this gate watches nothing")

	for _, kind := range named {
		_, declared := event.LookupType(kind)
		require.Truef(t, declared,
			"the page routes on %q, which the event-type vocabulary does not carry", kind)
	}

	// The reachability frame is what tells the page a printer went away without
	// its session ending, and there is no other frame that says so.
	require.Contains(t, named, event.TypeDevicePresenceChanged,
		"the page does not read the frame a printer going away publishes")
}

// TestThePageRunsNothingOnAClock is the poll this page carried, and the rule
// that replaced it.
//
// The link column asked /api/v1/devices again every five seconds, because no
// frame carried what this server makes of a printer's frames. That is one
// request per five seconds per open page, aimed at the server somebody opened
// the page to watch, and it was the only thing here on a timer.
//
// The rule now is that every fact on this page arrives on the stream or is
// reconciled when the stream connects. setInterval is the shape a poll takes,
// so its absence is the gate. setTimeout is not: the page coalesces with it,
// and a coalescing window is started by a frame rather than by a clock.
func TestThePageRunsNothingOnAClock(t *testing.T) {
	t.Parallel()

	script := consoleScript(t)

	require.NotContains(t, script, "setInterval",
		"the page runs something on a timer, and everything it draws reaches it as a frame")
	require.Contains(t, script, event.TypeDeviceLinkChanged,
		"the page does not read the frame that carries the link reading, so the column is stale or polled")
}

// TestThePageReadsEveryLinkFieldThePrinterPayloadPublishes is the drift gate
// between the developer view and the payload it renders.
//
// The page names these keys as text. A key renamed on the wire leaves a column
// that reads "every frame read" for a printer whose frames are all being
// refused, which is the exact condition the column exists to end: a number
// that is correct and invisible. It is gated rather than reviewed for that
// reason.
func TestThePageReadsEveryLinkFieldThePrinterPayloadPublishes(t *testing.T) {
	t.Parallel()

	script := consoleScript(t)

	payload := reflect.TypeFor[httpapi.DeviceLink]()
	require.Positive(t, payload.NumField(), "the link payload carries no field, so this gate watches nothing")

	for field := range payload.Fields() {
		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")

		require.NotEmptyf(t, key, "%s goes on the wire with no json tag", field.Name)
		require.Containsf(t, script, key,
			"the page never reads %q, so a printer whose frames are all refused renders as one that is not",
			key)
	}
}

// TestAnUnreadableLinkIsAnnouncedAboveEveryPanel holds the half of this that a
// field cannot carry.
//
// The printer this is about publishes nothing, looks idle in every other column
// and is exactly the machine a person came to the page to explain. A reading
// that only lands in a column is a field they have to go and find, so the alarm
// is part of the surface and not decoration on it.
func TestAnUnreadableLinkIsAnnouncedAboveEveryPanel(t *testing.T) {
	t.Parallel()

	body := string(devtools.Rendered().Body)
	script := consoleScript(t)

	require.Contains(t, body, `id="link-alarm"`, "the page carries no alarm element")
	require.Contains(t, script, `el("link-alarm")`, "nothing on the page ever writes the alarm")
	// The call and not the declaration. "renderAlarm()" matches the line that
	// defines it, so a page that declares the alarm and never runs it passed
	// that spelling: watched green with the one call site deleted.
	require.Contains(t, script, "renderAlarm();",
		"the alarm is declared and never called, so it never renders")
	require.Contains(t, inlineBlock(t, body, "style"), ".alarm",
		"the alarm has no style of its own, so it reads as ordinary prose")
}

// TestTheWidePanelsTakeTheWholeRowAndTheRestReflow holds the layout finding the
// other developer view paid for on hardware: a fixed two-column split scaled its
// boxes until the labels were unreadable at laptop width.
//
// Two rules answer it, and both are load-bearing. The panels whose content is
// wide, one printer in detail and the live wire, span every column rather than
// sharing a row; everything else fits as many columns as there is room for and
// drops to fewer rather than being squeezed into the width that is left.
func TestTheWidePanelsTakeTheWholeRowAndTheRestReflow(t *testing.T) {
	t.Parallel()

	style := inlineBlock(t, string(devtools.Rendered().Body), "style")

	require.Contains(t, style, "grid-column: 1 / -1",
		"no panel spans the row, so the wire and the detail share their width with a neighbour")
	require.Contains(t, style, "auto-fit",
		"the columns are fixed, so a narrower window scales the panels instead of dropping one")
}

// TestEveryElementTheScriptsNameIsDeclaredInTheMarkup is the static half of the
// drift gate between the script and the markup.
//
// The executed gate beside it only sees an element on a path a case actually
// reaches, and most of a console is behind a button. An id renamed in page.html
// and left alone in a click handler passes every case that never presses that
// button, and the failure in a browser is one panel that throws where nobody is
// watching. This one reads every name the scripts spell.
func TestEveryElementTheScriptsNameIsDeclaredInTheMarkup(t *testing.T) {
	t.Parallel()

	markup, err := os.ReadFile("page.html")
	require.NoError(t, err)

	declared := make(map[string]bool)
	for _, found := range regexp.MustCompile(`\sid="([A-Za-z0-9_-]+)"`).
		FindAllStringSubmatch(string(markup), -1) {
		declared[found[1]] = true
	}

	named := regexp.MustCompile(`\bel\("([A-Za-z0-9_-]+)"\)`).
		FindAllStringSubmatch(consoleScript(t), -1)
	require.NotEmpty(t, named, "the scripts name no element, so this gate watches nothing")

	for _, found := range named {
		require.Truef(t, declared[found[1]],
			"the console reaches for %q and page.html declares no such element", found[1])
	}
}

// TestEveryPanelTheScriptsWriteOutcomesIntoIsDeclared covers the other spelling.
// An outcome line is where a refused action is reported, so a panel whose line
// is misnamed swallows exactly the message the console exists to show.
func TestEveryPanelTheScriptsWriteOutcomesIntoIsDeclared(t *testing.T) {
	t.Parallel()

	markup, err := os.ReadFile("page.html")
	require.NoError(t, err)

	named := regexp.MustCompile(`\bsay\("([A-Za-z0-9_-]+)"`).
		FindAllStringSubmatch(consoleScript(t), -1)
	require.NotEmpty(t, named, "nothing on the console reports an outcome")

	for _, found := range named {
		require.Containsf(t, string(markup), `id="`+found[1]+`"`,
			"the console writes an outcome into %q and page.html declares no such element", found[1])
	}
}
