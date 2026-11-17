package devtools_test

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/devtools"
)

// The page executed rather than read.
//
// Every other gate here matches strings against page.js, and a string match
// cannot tell a function that runs from one that is declared and never reached:
// a gate over this page stays green with the alarm's only call site deleted.
// These cases run the script against a stubbed document and a stubbed network
// and assert on what it did.

// visitRoute is one endpoint's answer. Frames, when present, are the event
// stream's blocks, delivered in order and followed by the end of the stream.
type visitRoute struct {
	Status int    `json:"status,omitempty"`
	Body   string `json:"body,omitempty"`
	// Frames is nil for an ordinary endpoint and non-nil for the event stream,
	// including when the stream carries nothing, so it is not omitted when empty.
	Frames []string `json:"frames"`
	// Hang accepts the request and never answers it, which is what a proxy in
	// front of a loaded server does.
	Hang bool `json:"hang,omitempty"`
}

// visitElement is one element the served document declares, as the stub has to
// stand it up: an element whose class or checked state the stub invented would
// let the page pass against a document it does not have.
type visitElement struct {
	ID      string `json:"id"`
	Tag     string `json:"tag"`
	Class   string `json:"class"`
	Value   string `json:"value"`
	Checked bool   `json:"checked"`
	Hidden  bool   `json:"hidden"`
}

// visitCall is one request with the method it carried, so a case can say that a
// button sent a PUT rather than that something reached the same path.
type visitCall struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	IfMatch     string `json:"ifMatch"`
	IfNoneMatch string `json:"ifNoneMatch"`
	Sent        string `json:"sent"`
}

type visitPlan struct {
	URL      string                `json:"url"`
	Routes   map[string]visitRoute `json:"routes"`
	Elements []visitElement        `json:"elements"`
	// Values are typed into fields once the page has loaded, a string into an
	// input and a bool into a checkbox.
	Values map[string]any `json:"values,omitempty"`
	// Clicks are the buttons pressed afterwards, in order. A console is mostly
	// buttons, and a handler no case runs is code nothing here has executed.
	Clicks []string `json:"clicks,omitempty"`
}

// visitReport is what the page did: what it asked the network for, what it wrote
// into each element the document carries, and what it reached for that the
// document does not.
type visitReport struct {
	Fetched   []string              `json:"fetched"`
	Calls     []visitCall           `json:"calls"`
	Missing   []string              `json:"missing"`
	Text      map[string]string     `json:"text"`
	ClassName map[string]string     `json:"className"`
	Hidden    map[string]bool       `json:"hidden"`
	Values    map[string]string     `json:"values"`
	Rows      map[string][][]string `json:"rows"`
	Failures  []string              `json:"failures"`
	Location  string                `json:"location"`
}

var (
	anyIdentified = regexp.MustCompile(`<([a-z][a-z0-9]*)((?:\s[^<>]*)?\sid="([A-Za-z0-9_-]+)"[^<>]*)>`)
	anyClass      = regexp.MustCompile(`\sclass="([^"]*)"`)
	anyValue      = regexp.MustCompile(`\svalue="([^"]*)"`)
)

// documentShape reads the identified elements out of the page's own markup, so
// the stubbed document carries what the served document carries and no more. An
// element page.js asks for and page.html never declared is a lookup miss here,
// which is how a renamed id is caught rather than shipped.
func documentShape(t *testing.T) []visitElement {
	t.Helper()

	markup, err := os.ReadFile("page.html")
	require.NoError(t, err)

	var elements []visitElement

	for _, found := range anyIdentified.FindAllStringSubmatch(string(markup), -1) {
		element := visitElement{
			ID:      found[3],
			Tag:     found[1],
			Checked: strings.Contains(found[2], " checked"),
			Hidden:  strings.Contains(found[2], " hidden"),
		}

		if class := anyClass.FindStringSubmatch(found[2]); class != nil {
			element.Class = class[1]
		}

		if value := anyValue.FindStringSubmatch(found[2]); value != nil {
			element.Value = value[1]
		}

		elements = append(elements, element)
	}

	require.NotEmpty(t, elements, "the page declares no element id, so this harness stubs nothing")

	return elements
}

// visit runs page.js at one URL against one routing table and returns what it
// did. It skips where node is not on PATH, on the model of the openssl cases in
// internal/app: the gate is real where the tool is there and says so where it is
// not.
func visit(t *testing.T, url string, routes map[string]visitRoute) visitReport {
	t.Helper()

	return run(t, visitPlan{URL: url, Routes: routes})
}

// use is visit with somebody at the keyboard: the page loads, the named fields
// are filled, and the named buttons are pressed in order.
func use(t *testing.T, url string, routes map[string]visitRoute,
	values map[string]any, clicks ...string,
) visitReport {
	t.Helper()

	return run(t, visitPlan{URL: url, Routes: routes, Values: values, Clicks: clicks})
}

func run(t *testing.T, plan visitPlan) visitReport {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH, so the page cannot be executed and only the string gates run")
	}

	plan.Elements = documentShape(t)

	encoded, err := json.Marshal(plan)
	require.NoError(t, err)

	planPath := filepath.Join(t.TempDir(), "plan.json")
	require.NoError(t, os.WriteFile(planPath, encoded, 0o600))

	// Every script file, in the order the page concatenates them. The harness
	// joins them the way the binary does, so a case runs the console that ships
	// rather than one file of it.
	args := append([]string{"testdata/browser.js", planPath}, devtools.ScriptFiles...)

	cmd := exec.CommandContext(t.Context(), node, args...)

	out, err := cmd.Output()
	require.NoError(t, err, "the harness did not finish: %s", out)

	var report visitReport
	require.NoError(t, json.Unmarshal(out, &report), "the harness printed %s", out)

	return report
}

// printerJSON is one row of /api/v1/devices as the page reads it.
func printerJSON(id, link string) string {
	return `{"id":"` + id + `","name":"` + id + `","vendor":"Prusa","model":"COREONE",` +
		`"serial":"SN-` + id + `","firmware":"6.4.0","driver":"xbuddy","connected":true,` +
		`"capabilities":["core.command.stop"],"capability_epoch":3,"profile_matched":true,` +
		`"state":{"nozzle.temperature":215},"link":` + link + `}`
}

const (
	// linkClean is a driver that counts and has read every frame.
	linkClean = `{"counted":true,"refused_frames":0,"dropped_telemetry":0,` +
		`"refused_frames_per_minute":0,"dropped_telemetry_per_minute":0,"measured_over_seconds":30}`
	// linkRefusing is the machine this page exists for: connected, publishing
	// nothing, and unreadable.
	linkRefusing = `{"counted":true,"refused_frames":412,"dropped_telemetry":0,` +
		`"refused_frames_per_minute":48,"dropped_telemetry_per_minute":0,"measured_over_seconds":30}`
)

// firstFrame is a telemetry sample, and it is the first block of a stream whose
// case is about the block after it.
//
// The roster is read when the stream opens, and a frame naming a printer that
// read has not landed yet is about a printer this page does not hold, so it is
// dropped. That is the ordinary first moment of a page and not what these cases
// are about: they are about what the page does with a frame for a printer it
// has.
const firstFrame = "id: 1\nevent: com.prusa.connect.device.telemetry.sampled\n" +
	`data: {"type":"com.prusa.connect.device.telemetry.sampled","subject":"device:p1",` +
	`"time":"2026-08-26T10:00:01Z","data":{"path":"nozzle.temperature","value":215}}` + "\n\n"

// fleetRoutes is a server answering everything the page asks on load, with one
// printer on it. A caller adds or overrides whatever the case is about.
func fleetRoutes(printers ...string) map[string]visitRoute {
	if len(printers) == 0 {
		printers = []string{printerJSON("p1", linkClean)}
	}

	const inventoryJSON = `{"connectors":[{"name":"xbuddy","qualified":"prusa.xbuddy",` +
		`"direction":"inbound","status":"mounted","routes":["/p/telemetry"],` +
		`"baseline_capabilities":12}],"routes":[{"pattern":"/p/telemetry",` +
		`"reason":"the firmware has it compiled in"}]}`

	const readinessJSON = `{"ready":true,"serving":2,"declared":2,"blocked":[],` +
		`"instances":[{"resource":"printers","state":"serving","required":true,"serving":true}]}`

	// The capability vocabulary is what the command form's fields come from, so
	// a fleet with no vocabulary behind it is a fleet whose commands cannot be
	// sent. One verb with one argument is enough to render one field.
	const capabilitiesJSON = `{"contract":"1","kinds":[{"kind":"number","numeric":true}],` +
		`"keys":[{"key":"core.command.stop"},` +
		`{"key":"core.command.stop#axis","verb":"core.command.stop","argument":"axis",` +
		`"kind":"text","required":true}]}`

	return map[string]visitRoute{
		"/api/v1/health":    {Body: `{"version":"0.3.0-test"}`},
		"/api/v1/principal": {Body: `{"principal":"operator"}`},
		"/api/v1/auth/info": {
			Body: `{"provider":"","credentials":true,"redirect":false,"needs_setup":false}`,
		},
		"/api/v1/dev/connectors":            {Body: inventoryJSON},
		"/api/v1/readiness":                 {Body: readinessJSON},
		"/api/v1/devices":                   {Body: `{"items":[` + strings.Join(printers, ",") + `]}`},
		"/api/v1/events":                    {Frames: []string{}},
		"/api/v1/gcode-presets":             {Body: `{"items":[]}`},
		"/api/v1/vocabularies/capabilities": {Body: capabilitiesJSON},
		"/api/v1/vocabularies/event-types":  {Body: `{"items":[{"type":"com.prusa.connect.device.arrived"}]}`},
	}
}

// TestADeepLinkToAPrinterThatIsNotThereStillOpensTheLiveWire is the gate on the
// hand-over link.
//
// The link is handed to somebody else, and the printer it names is routinely
// not there when they open it: it has not dialled in yet, it departed, it is one
// they may not view, or the id was mistyped. Every one of those answers 404 from
// the command log, and the page must still come up: the live wire is the panel
// that would tell the reader when the printer arrives.
func TestADeepLinkToAPrinterThatIsNotThereStillOpensTheLiveWire(t *testing.T) {
	t.Parallel()

	out := visit(t, "http://127.0.0.1:8000/dev?select=ghost", fleetRoutes())

	require.Contains(t, out.Fetched, "/api/v1/events",
		"the page never subscribed, so nothing on it will ever update again")
	require.Empty(t, out.Failures, "the page failed and nothing caught it")
	require.Equal(t, "open", trimWire(out.Text["wire-state"]),
		"the wire panel reports a state it never reached")
}

// TestOneEndpointThatNeverAnswersDoesNotStopTheLiveWireFromOpening is the gate
// on the order the page starts in.
//
// A request that is accepted and never answered is what a proxy in front of a
// loaded server does, and no amount of catching rejections reaches it: an
// awaited load that never settles holds up everything after it for ever. The
// stream is what this page is for and what would report the recovery, so it is
// subscribed before the panels are filled rather than after.
func TestOneEndpointThatNeverAnswersDoesNotStopTheLiveWireFromOpening(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/readiness"] = visitRoute{Hang: true}

	out := visit(t, "http://127.0.0.1:8000/dev", routes)

	require.Contains(t, out.Fetched, "/api/v1/events",
		"one endpoint that never answered took the whole page down with it")
	require.Equal(t, "open", trimWire(out.Text["wire-state"]))
	require.NotEmpty(t, out.Rows["printers"], "the roster the stream reconciles on open never arrived")
}

// trimWire strips the label the wire pill carries, leaving the state.
func trimWire(text string) string {
	const prefix = "wire: "
	if len(text) > len(prefix) && text[:len(prefix)] == prefix {
		return text[len(prefix):]
	}

	return text
}

// TestADeepLinkToAPrinterThatIsNotThereSaysSo is the other half. Coming up is
// not enough if the detail panel is blank: a reader handed a link needs to know
// whether they are early or wrong.
func TestADeepLinkToAPrinterThatIsNotThereSaysSo(t *testing.T) {
	t.Parallel()

	out := visit(t, "http://127.0.0.1:8000/dev?select=ghost", fleetRoutes())

	require.False(t, out.Hidden["detail-missing"], "the panel says nothing about the printer it named")
	require.Contains(t, out.Text["detail-missing"], "ghost", "the message does not name the printer")
}

// TestASelectedPrinterThatIsThereRendersItsDetail is the counterpart, and the
// reason the case above cannot be satisfied by refusing to select anything.
func TestASelectedPrinterThatIsThereRendersItsDetail(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/devices/p1"] = visitRoute{Body: printerJSON("p1", linkClean)}
	routes["/api/v1/dev/devices/p1/commands"] = visitRoute{
		Body: `{"items":[{"at":"2026-08-26T10:00:00Z","verb":"core.command.stop","elapsed_ms":12,"ok":true}],"dropped":0}`,
	}

	out := visit(t, "http://127.0.0.1:8000/dev?select=p1", routes)

	require.True(t, out.Hidden["detail-missing"], "a printer that is there is reported as missing")
	require.False(t, out.Hidden["detail"])
	require.Contains(t, out.Rows["detail-identity"], []string{"driver", "xbuddy"})
	require.Contains(t, out.Rows["commands"], []string{"10:00:00.000", "core.command.stop", "", "12 ms", "accepted"})
	require.Contains(t, out.Location, "select=p1", "the selection did not reach the address bar")
}

// TestARefusedCommandShowsThePrintersOwnReasonWhereAPersonIsLooking is the
// bench half of the same reason the API now carries.
//
// A person driving a machine from this page reads the command log, and a row
// that says only "refused" sends them to a terminal to ask the printer what it
// meant. The machine's own word is what says whether to clear the bed, insert
// the stick, or wait.
func TestARefusedCommandShowsThePrintersOwnReasonWhereAPersonIsLooking(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/devices/p1"] = visitRoute{Body: printerJSON("p1", linkClean)}
	routes["/api/v1/dev/devices/p1/commands"] = visitRoute{
		Body: `{"items":[{"at":"2026-08-26T10:00:00Z","verb":"core.command.start_print",` +
			`"elapsed_ms":31,"ok":false,"machine_reason":"MEDIA_NOT_INSERTED"}],"dropped":0}`,
	}

	out := visit(t, "http://127.0.0.1:8000/dev?select=p1", routes)

	require.Contains(t, out.Rows["commands"], []string{
		"10:00:00.000", "core.command.start_print", "", "31 ms", "refused: MEDIA_NOT_INSERTED",
	}, "the command log rendered the refusal without the printer's reason for it")
}

// TestARefusalWithNoReasonSaysOnlyThatItWasRefused is the mutation guard beside
// the case above. A dialect with no such word is the ordinary case, and a row
// that rendered a separator with nothing after it would read as a reason the
// page had lost.
func TestARefusalWithNoReasonSaysOnlyThatItWasRefused(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/devices/p1"] = visitRoute{Body: printerJSON("p1", linkClean)}
	routes["/api/v1/dev/devices/p1/commands"] = visitRoute{
		Body: `{"items":[{"at":"2026-08-26T10:00:00Z","verb":"core.command.home",` +
			`"elapsed_ms":9,"ok":false}],"dropped":0}`,
	}

	out := visit(t, "http://127.0.0.1:8000/dev?select=p1", routes)

	require.Contains(t, out.Rows["commands"], []string{
		"10:00:00.000", "core.command.home", "", "9 ms", "refused",
	})
}

// TestThePageReachesForNoElementTheDocumentDoesNotCarry is the drift gate
// between the script and the markup.
//
// getElementById answers null for an id that is not there and the page then
// writes through it, which throws inside whichever load was running and stops
// that panel with no console anybody is watching. Renaming an id in page.html is
// the ordinary way to cause it.
func TestThePageReachesForNoElementTheDocumentDoesNotCarry(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/devices/p1"] = visitRoute{Body: printerJSON("p1", linkClean)}
	routes["/api/v1/dev/devices/p1/commands"] = visitRoute{Body: `{"items":[],"dropped":0}`}

	out := visit(t, "http://127.0.0.1:8000/dev?select=p1", routes)

	require.Empty(t, out.Missing, "the page asked for an element page.html does not declare")
	require.Empty(t, out.Failures)
}

// TestAPrinterRefusingEveryFrameIsAnnouncedAboveEveryPanel executes what the
// string gate beside it can only spell.
//
// The printer is connected, its capabilities resolved and its session open, so
// every other column on the page reads normal; the frames it sends are all being
// refused and it publishes nothing. The alarm is the only thing that says so
// without scrolling, so it is asserted as rendered text and not as a call site.
func TestAPrinterRefusingEveryFrameIsAnnouncedAboveEveryPanel(t *testing.T) {
	t.Parallel()

	out := visit(t, "http://127.0.0.1:8000/dev",
		fleetRoutes(printerJSON("p1", linkClean), printerJSON("p2", linkRefusing)))

	require.False(t, out.Hidden["link-alarm"], "the alarm stayed hidden for an unreadable printer")
	require.Contains(t, out.Text["link-alarm"], "p2")
	require.NotContains(t, out.Text["link-alarm"], "p1", "a printer whose frames are read is alarmed about")
	require.Equal(t, "alarm", out.ClassName["link-alarm"])
}

// TestEveryFrameReadLeavesNoAlarm is the mutation guard on the case above: an
// alarm that is always on is an alarm nobody reads.
func TestEveryFrameReadLeavesNoAlarm(t *testing.T) {
	t.Parallel()

	out := visit(t, "http://127.0.0.1:8000/dev", fleetRoutes())

	require.True(t, out.Hidden["link-alarm"])
	require.Empty(t, out.Text["link-alarm"])
	require.Contains(t, out.Rows["printers"], []string{
		"p1", "COREONE", "SN-p1", "6.4.0", "xbuddy", "open", "every frame read", "1", "3", "matched", "",
	})
}

// TestTheLiveWireRendersTheFramesItIsSent runs the stream end of the page: the
// frames are parsed, filed newest first, and rendered. Nothing else on this page
// is fed by the stream, so a parser that silently dropped every frame would look
// exactly like a quiet fleet.
func TestTheLiveWireRendersTheFramesItIsSent(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/events"] = visitRoute{Frames: []string{
		"id: 1\nevent: com.prusa.connect.device.telemetry.sampled\n" +
			`data: {"type":"com.prusa.connect.device.telemetry.sampled","subject":"device:p1",` +
			`"time":"2026-08-26T10:00:01Z","data":{"path":"nozzle.temperature","value":215}}` + "\n\n",
		"id: 2\nevent: com.prusa.connect.device.presence.changed\n" +
			`data: {"type":"com.prusa.connect.device.presence.changed","subject":"device:p1",` +
			`"time":"2026-08-26T10:00:02Z","data":{"connected":false}}` + "\n\n",
	}}

	out := visit(t, "http://127.0.0.1:8000/dev", routes)

	require.Len(t, out.Rows["events-rows"], 2, "the wire panel rendered a different number of frames than it was sent")
	require.Equal(t, "com.prusa.connect.device.presence.changed", out.Rows["events-rows"][0][1], "newest first")
	require.Equal(t, "device:p1", out.Rows["events-rows"][0][2])

	// The presence frame is the one fact the roster cannot learn any other way,
	// so the row must have followed it away.
	require.Contains(t, out.Rows["printers"][0], "away")
}

// TestALinkFrameMovesTheColumnWithoutRereadingTheRoster holds the link column
// to the frame that carries the reading.
//
// Without that frame the column has to ask the listing again on a timer, which
// is one request per period per open page, aimed at exactly the server somebody
// opened the page to watch. It is the only thing on this page that would run on
// a clock.
func TestALinkFrameMovesTheColumnWithoutRereadingTheRoster(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/events"] = visitRoute{Frames: []string{
		firstFrame,
		"id: 2\nevent: com.prusa.connect.device.link.changed\n" +
			`data: {"type":"com.prusa.connect.device.link.changed","subject":"device:p1",` +
			`"time":"2026-08-26T10:00:02Z","data":{"printer":"p1","driver":"xbuddy",` +
			`"link":` + linkRefusing + "}}\n\n",
	}}

	out := visit(t, "http://127.0.0.1:8000/dev", routes)

	require.Contains(t, out.Rows["printers"][0], "refusing 48/min over 30.0s, 412 this session",
		"the frame carried the reading and the column did not move")

	require.False(t, out.Hidden["link-alarm"], "a printer refusing every frame raised no alarm")
	require.Contains(t, out.Text["link-alarm"], "p1")
}

// TestAPrinterThatDepartedSaysWhyOnItsRow is the operator's half of keeping the
// record. A row that says only "gone" sends somebody to the machine to find out
// whether the server stopped or the printer did.
func TestAPrinterThatDepartedSaysWhyOnItsRow(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/events"] = visitRoute{Frames: []string{
		firstFrame,
		"id: 2\nevent: com.prusa.connect.device.departed\n" +
			`data: {"type":"com.prusa.connect.device.departed","subject":"device:p1",` +
			`"time":"2026-08-26T10:00:02Z","data":{"printer":"p1","driver":"xbuddy",` +
			`"reason":"session_ended"}}` + "\n\n",
	}}

	out := visit(t, "http://127.0.0.1:8000/dev", routes)

	require.Contains(t, out.Rows["printers"][0], "gone: session_ended",
		"the row does not say why the printer went")
}

// TestADepartedPrinterIsReadOffTheListingAndNotOnlyOffTheStream is the other
// way the page learns it. A page opened after the printer went reads the
// listing, which carries the same record, so the two agree.
func TestADepartedPrinterIsReadOffTheListingAndNotOnlyOffTheStream(t *testing.T) {
	t.Parallel()

	departed := `{"id":"p1","name":"p1","vendor":"Prusa","model":"COREONE","serial":"SN-p1",` +
		`"firmware":"6.4.0","driver":"xbuddy","connected":false,` +
		`"departed":{"reason":"server_stopping","at":"2026-08-26T10:00:01Z"},` +
		`"capabilities":["core.command.stop"],"capability_epoch":3,"profile_matched":true,` +
		`"state":{},"link":` + linkClean + `}`

	out := visit(t, "http://127.0.0.1:8000/dev", fleetRoutes(departed))

	require.Contains(t, out.Rows["printers"][0], "gone: server_stopping")
	require.True(t, out.Hidden["link-alarm"], "a printer that is gone was alarmed about")
}

// TestAFleetArrivingAtOnceDoesNotBecomeARequestPerPrinter is the bound on what
// the page does to the server it is watching.
//
// An arrival is the one frame that does not carry what the page needs, so it
// re-reads the roster. A restart is the whole fleet arriving inside a second,
// and this project's stated scale is four hundred printers: one listing request
// per arrival turns a page opened to watch a recovery into the thing that
// prevents it.
func TestAFleetArrivingAtOnceDoesNotBecomeARequestPerPrinter(t *testing.T) {
	t.Parallel()

	const arrivals = 40

	frames := make([]string, 0, arrivals)
	for i := range arrivals {
		id := "p" + strconv.Itoa(i)
		frames = append(frames, "id: "+id+"\nevent: com.prusa.connect.device.arrived\n"+
			`data: {"type":"com.prusa.connect.device.arrived","subject":"device:`+id+`",`+
			`"time":"2026-08-26T10:00:00Z","data":{}}`+"\n\n")
	}

	routes := fleetRoutes()
	routes["/api/v1/events"] = visitRoute{Frames: frames}

	out := visit(t, "http://127.0.0.1:8000/dev", routes)

	// Two of the three are the page coming up: the stream reconciles the roster
	// when it opens, and the first load reads it. The third is the whole burst.
	//
	// It is an equality and not a bound in one direction, because both directions
	// are defects: more is the request storm, and fewer is a page that coalesced
	// the arrivals into nothing and never learned the fleet came back.
	require.Equal(t, 3, countFetches(out, "/api/v1/devices"),
		"an arriving fleet must cost the roster one re-read, and must cost it one")
	require.NotEmpty(t, out.Rows["printers"])
}

// TestAFleetLargerThanOnePageSaysSoRatherThanShowingASlice is the bench
// question this panel could not answer: am I looking at the fleet, or at the
// first page of it.
//
// The listing reports what it applied and how many printers there are over every
// page. A view that renders the items and drops the other three fields shows
// fifty rows of four hundred printers and reads exactly like a fleet of fifty,
// which is the same class of correct-and-invisible number the link column exists
// to end.
func TestAFleetLargerThanOnePageSaysSoRatherThanShowingASlice(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/devices"] = visitRoute{
		Body: `{"items":[` + printerJSON("p1", linkClean) + `],"total":400,"limit":1,"offset":0}`,
	}

	out := visit(t, "http://127.0.0.1:8000/dev", routes)

	require.False(t, out.Hidden["roster-note"], "a fleet four hundred printers deep reads as one printer")
	require.Contains(t, out.Text["roster-note"], "1 of 400",
		"the note does not say how much of the fleet the panel is showing")
}

// TestAFleetThatFitsInOnePageSaysNothing is the mutation guard on the case
// above. A panel that always warns about the fleet being larger than it shows is
// a panel nobody reads.
func TestAFleetThatFitsInOnePageSaysNothing(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/devices"] = visitRoute{
		Body: `{"items":[` + printerJSON("p1", linkClean) + `],"total":1,"limit":50,"offset":0}`,
	}

	out := visit(t, "http://127.0.0.1:8000/dev", routes)

	require.True(t, out.Hidden["roster-note"])
	require.Empty(t, out.Text["roster-note"])
}

// TestTheRosterAsksForMoreThanTheDefaultPage holds the other half. Reporting the
// truncation is not the same as not being truncated: this page is opened to watch
// a fleet, and the server answers a limit above its cap with the cap and says
// which limit it applied, so asking large is how a client asks for all of it.
func TestTheRosterAsksForMoreThanTheDefaultPage(t *testing.T) {
	t.Parallel()

	out := visit(t, "http://127.0.0.1:8000/dev", fleetRoutes())

	asked := ""

	for _, path := range out.Fetched {
		if strings.HasPrefix(path, "/api/v1/devices?") {
			asked = path
		}
	}

	require.NotEmpty(t, asked, "the roster is read at the server's default page size and never says so")
	require.Contains(t, asked, "limit=")
}

// countFetches counts the requests to one path, whatever query each carried.
func countFetches(out visitReport, path string) int {
	seen := 0

	for _, asked := range out.Fetched {
		if before, _, _ := strings.Cut(asked, "?"); before == path {
			seen++
		}
	}

	return seen
}

// The four areas beyond the printers, executed.
//
// Each is a screen an engineer would otherwise reach with curl, so what is
// asserted is the request that went out and the sentence that came back: a
// console that renders a form and sends the wrong body is worse than no console.

// sentCall finds the one request a case is about.
func sentCall(t *testing.T, out visitReport, method, path string) visitCall {
	t.Helper()

	for _, call := range out.Calls {
		if call.Method == method && call.Path == path {
			return call
		}
	}

	require.FailNowf(t, "the console never made the request the case is about",
		"wanted %s %s, and it made %v", method, path, out.Calls)

	return visitCall{}
}

// accessRoutes is a server with an authorization vocabulary, one account and one
// object that carries grants.
func accessRoutes() map[string]visitRoute {
	routes := fleetRoutes()

	routes["/api/v1/vocabularies/authorization"] = visitRoute{Body: `{"vocabulary":"authorization",` +
		`"version":"v3","format":"entry","types":[{"type":"device","object":true,"subject":false,` +
		`"summary":"a printer","description":"a printer"},{"type":"user","object":false,` +
		`"subject":true,"summary":"a person","description":"a person"}],` +
		`"relations":[{"relation":"manage","role":true,"summary":"administers it","description":"x"}],` +
		`"actions":[{"action":"send_command","domain":"device","conferred_by":["manage"],` +
		`"summary":"send a command","description":"x"}],` +
		`"shapes":[{"object_type":"device","relation":"manage","subject_type":"user"}]}`}

	routes["/api/v1/auth/accounts"] = visitRoute{Body: `{"items":[{"id":"operator",` +
		`"username":"owner@example.com","display_name":"Owner","created_at":"2026-08-26T09:14:05Z"}]}`}

	routes["/api/v1/grants/device/p1"] = visitRoute{Body: `{"items":[]}`}
	routes["POST /api/v1/grants/device/p1"] = visitRoute{Status: 201, Body: `{}`}

	return routes
}

// TestWritingAGrantSendsTheTupleTheFormHolds is the operation the graph was, up
// to this console, only reachable from a Go test.
func TestWritingAGrantSendsTheTupleTheFormHolds(t *testing.T) {
	t.Parallel()

	out := use(t, "http://127.0.0.1:8000/dev?tab=access", accessRoutes(),
		map[string]any{
			"grant-object-type": "device",
			"grant-object-id":   "p1",
			"grant-relation":    "manage",
			"grant-subject":     "user:operator",
		},
		"grant-write")

	call := sentCall(t, out, "POST", "/api/v1/grants/device/p1")
	require.JSONEq(t, `{"relation":"manage","subject":"user:operator"}`, call.Sent)

	require.Contains(t, out.Text["grants-outcome"], "wrote manage")
	require.Empty(t, out.Failures)
}

// TestTheAuthorizationVocabularyFillsTheRelationPicker is why the form above can
// be trusted. A console that let somebody type a relation would send tuples the
// graph has no shape for, and the refusal would read as a permission problem.
func TestTheAuthorizationVocabularyFillsTheRelationPicker(t *testing.T) {
	t.Parallel()

	out := visit(t, "http://127.0.0.1:8000/dev?tab=access", accessRoutes())

	require.Contains(t, out.Rows["authz-relations"], []string{"manage", "confers actions", "administers it"})
	require.Contains(t, out.Rows["authz-shapes"], []string{"device", "manage", "user", ""})
	require.Contains(t, out.Rows["accounts"], []string{
		"operator", "owner@example.com", "Owner", "2026-08-26T09:14:05Z", "",
	})
}

// TestGrantsRefusedToThisCallerSayThatRatherThanShowingNone is the failure this
// screen is most likely to meet. Reading grants takes the grant that writing one
// takes, so an empty table is either no grants or no permission, and only one of
// those is something the reader can act on.
func TestGrantsRefusedToThisCallerSayThatRatherThanShowingNone(t *testing.T) {
	t.Parallel()

	routes := accessRoutes()
	routes["/api/v1/grants/device/p1"] = visitRoute{
		Status: 403,
		Body:   `{"code":"authz/FORBIDDEN","detail":"this principal may not administer device:p1"}`,
	}

	out := use(t, "http://127.0.0.1:8000/dev?tab=access", routes,
		map[string]any{"grant-object-type": "device", "grant-object-id": "p1"}, "grants-load")

	require.Contains(t, out.Text["grants-outcome"], "403")
	require.Contains(t, out.Text["grants-outcome"], "authz/FORBIDDEN")
	require.Equal(t, "note bad", out.ClassName["grants-outcome"])
	require.Empty(t, out.Rows["grants"])
}

// configRoutes is a server with one resource instance and the sets the
// configuration area reads.
func configRoutes() map[string]visitRoute {
	routes := fleetRoutes()

	routes["/api/v1/resources"] = visitRoute{Body: `{"items":[{"kind":"storage","name":"local",` +
		`"type":"filesystem","enabled":true,"required":true,"source":"overlay","version":"v7",` +
		`"uid":"u1","generation":3,"config":{"root":"/var/lib/files"},` +
		`"status":{"state":"ready","serving":true,"observed_generation":3,"attempt":0,"dropped":0,` +
		`"conditions":[]}}]}`}
	routes["/api/v1/vocabularies/resource-kinds"] = visitRoute{
		Body: `{"items":[{"kind":"storage","configurable":true,"summary":"where files go","description":"x"}]}`,
	}
	routes["/api/v1/connector-endpoints"] = visitRoute{Body: `{"items":[{"name":"bench",` +
		`"connector":"xbuddy","address":"192.168.1.42:80","enabled":true,"options":{},"version":"v7"}]}`}
	routes["/api/v1/health/connectors"] = visitRoute{Body: `{"items":[]}`}
	routes["/api/v1/state/namespaces"] = visitRoute{Body: `{"items":[]}`}
	routes["/api/v1/vocabularies"] = visitRoute{Body: `{"items":[]}`}
	routes["PUT /api/v1/resources/storage/scratch"] = visitRoute{Status: 201, Body: `{}`}
	routes["PUT /api/v1/connector-endpoints/bench"] = visitRoute{Status: 200, Body: `{}`}

	return routes
}

// TestAnInstanceThatDoesNotExistYetIsCreatedAndNeverOverwrites is the half of
// the precondition rule a console gets wrong by being helpful.
//
// The API refuses a write that carries neither precondition, and the easy way
// past that refusal is to send If-Match with a wildcard, which succeeds against
// whatever is there. A name already taken must be refused instead.
func TestAnInstanceThatDoesNotExistYetIsCreatedAndNeverOverwrites(t *testing.T) {
	t.Parallel()

	out := use(t, "http://127.0.0.1:8000/dev?tab=config", configRoutes(),
		map[string]any{"resource-new-kind": "storage", "resource-new-name": "scratch"},
		"resource-new", "resource-save")

	call := sentCall(t, out, "PUT", "/api/v1/resources/storage/scratch")
	require.Equal(t, "*", call.IfNoneMatch, "a new instance was written without asking to create it")
	require.Empty(t, call.IfMatch, "a new instance quoted a version it never read")
	require.Contains(t, out.Text["resource-outcome"], "written")
}

// TestAWriteToAnExistingEndpointQuotesTheVersionItWasRead is the other half. A
// console that dropped the tag would either be refused with 428 for ever or, if
// it reached for the wildcard, would overwrite a change somebody else made
// between the read and the save.
func TestAWriteToAnExistingEndpointQuotesTheVersionItWasRead(t *testing.T) {
	t.Parallel()

	out := use(t, "http://127.0.0.1:8000/dev?tab=config", configRoutes(),
		map[string]any{
			"endpoint-name": "bench", "endpoint-connector": "xbuddy",
			"endpoint-address": "192.168.1.42:80", "endpoint-enabled": true,
			"endpoint-options": "{}",
		},
		"endpoint-save")

	call := sentCall(t, out, "PUT", "/api/v1/connector-endpoints/bench")
	require.Equal(t, `"v7"`, call.IfMatch, "the save did not quote the version the listing carried")
	require.Empty(t, call.IfNoneMatch)
	require.JSONEq(t,
		`{"connector":"xbuddy","address":"192.168.1.42:80","enabled":true,"options":{}}`, call.Sent)
}

// TestTheConfigurationAreaSaysWhichRowsThisConsoleCannotWrite is the fact a
// reader needs before they edit. An instance declared in the operator's own file
// is not the overlay's, and a save against it is refused; saying so at the point
// of selection beats a conflict after somebody has typed a configuration out.
func TestTheConfigurationAreaSaysWhichRowsThisConsoleCannotWrite(t *testing.T) {
	t.Parallel()

	out := visit(t, "http://127.0.0.1:8000/dev?tab=config", configRoutes())

	require.Contains(t, out.Rows["config-resources"], []string{
		"storage", "local", "filesystem", "ready", "yes", "overlay", "v7",
	})
}

// TestTheFirstAdministratorIsCreatedThroughTheSetupOperation is the first run, on
// a server where nobody has one yet.
func TestTheFirstAdministratorIsCreatedThroughTheSetupOperation(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/auth/info"] = visitRoute{
		Body: `{"provider":"","credentials":true,"redirect":false,"needs_setup":true}`,
	}
	routes["POST /api/v1/auth/setup"] = visitRoute{
		Status: 201,
		Body: `{"id":"operator","username":"owner@example.com","display_name":"Owner",` +
			`"created_at":"2026-08-26T09:14:05Z"}`,
	}

	out := use(t, "http://127.0.0.1:8000/dev?tab=setup", routes,
		map[string]any{
			"setup-username": "owner@example.com",
			"setup-display":  "Owner",
			"setup-password": "a good password",
		},
		"setup-submit")

	call := sentCall(t, out, "POST", "/api/v1/auth/setup")
	require.JSONEq(t,
		`{"username":"owner@example.com","display_name":"Owner","password":"a good password"}`, call.Sent)

	require.Contains(t, out.Text["setup-outcome"], "operator")
	// The step after creating the administrator is logging in as them, and the
	// console carries the credentials across rather than asking twice.
	require.Equal(t, "owner@example.com", out.Values["login-username"])
}

// TestTheFirstRunChecklistIsReadFromTheRunningServer is what makes the area
// usable a second time. A checklist kept in the page would say "not done" to
// somebody who finished the install from a terminal.
func TestTheFirstRunChecklistIsReadFromTheRunningServer(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/auth/info"] = visitRoute{
		Body: `{"provider":"","credentials":true,"redirect":false,"needs_setup":true}`,
	}
	routes["/api/v1/readiness"] = visitRoute{
		Status: 503,
		Body:   `{"ready":false,"serving":0,"declared":1,"blocked":["storage/local"],"instances":[]}`,
	}
	routes["/api/v1/devices"] = visitRoute{Body: `{"items":[],"total":0,"limit":50,"offset":0}`}

	out := visit(t, "http://127.0.0.1:8000/dev?tab=setup", routes)

	states := make(map[string]string, len(out.Rows["setup-checklist"]))
	for _, row := range out.Rows["setup-checklist"] {
		states[row[0]] = row[1]
	}

	require.Equal(t, "not yet", states["an administrator exists"])
	require.Equal(t, "not yet", states["somewhere to keep files"])
	require.Equal(t, "not yet", states["every required instance is serving"])
	require.Equal(t, "not yet", states["a printer has arrived"])

	// The one step this server has finished. A checklist that reported every row
	// as outstanding would be one that had read nothing.
	require.Equal(t, "done", states["this browser is somebody"])
}

// TestSearchingTheStoreNarrowsToTheTypeAsked is the events area's whole point:
// not a tail, a store somebody looks through afterwards.
func TestSearchingTheStoreNarrowsToTheTypeAsked(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/events"] = visitRoute{Frames: []string{
		firstFrame,
		"id: 2\nevent: com.prusa.connect.device.presence.changed\n" +
			`data: {"type":"com.prusa.connect.device.presence.changed","subject":"device:p1",` +
			`"time":"2026-08-26T10:00:02Z","data":{"connected":false}}` + "\n\n",
		"id: 3\nevent: com.prusa.connect.device.telemetry.sampled\n" +
			`data: {"type":"com.prusa.connect.device.telemetry.sampled","subject":"device:p2",` +
			`"time":"2026-08-26T10:00:03Z","data":{"path":"bed.temperature","value":60}}` + "\n\n",
	}}

	out := use(t, "http://127.0.0.1:8000/dev?tab=events", routes,
		map[string]any{"events-type": "telemetry"}, "events-apply")

	require.Len(t, out.Rows["events-rows"], 2, "the search did not narrow to the type asked for")
	require.Contains(t, out.Text["events-summary"], "3 events held")
	require.Contains(t, out.Text["events-summary"], "2 match")

	require.Contains(t, out.Rows["events-counts"],
		[]string{"com.prusa.connect.device.telemetry.sampled", "2", "10:00:01.000", "10:00:03.000"})
}

// TestSearchingByOriginAndTimeNarrowsTheSameStore holds the other two filters. A
// console that offered them and applied only the type would answer a question
// nobody asked and look like it had answered theirs.
func TestSearchingByOriginAndTimeNarrowsTheSameStore(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/events"] = visitRoute{Frames: []string{
		firstFrame,
		"id: 2\nevent: com.prusa.connect.device.telemetry.sampled\n" +
			`data: {"type":"com.prusa.connect.device.telemetry.sampled","subject":"device:p2",` +
			`"time":"2026-08-26T10:00:09Z","data":{"path":"bed.temperature","value":60}}` + "\n\n",
	}}

	byOrigin := use(t, "http://127.0.0.1:8000/dev?tab=events", routes,
		map[string]any{"events-origin": "device:p2"}, "events-apply")
	require.Len(t, byOrigin.Rows["events-rows"], 1)
	require.Equal(t, "device:p2", byOrigin.Rows["events-rows"][0][2])

	byTime := use(t, "http://127.0.0.1:8000/dev?tab=events", routes,
		map[string]any{"events-from": "10:00:05"}, "events-apply")
	require.Len(t, byTime.Rows["events-rows"], 1)
	require.Equal(t, "10:00:09.000", byTime.Rows["events-rows"][0][0])
}

// TestEveryRequestTheConsoleMadeIsOnTheLog is the claim the page makes about
// itself. It is the reason an engineer can use this instead of curl: what the
// screen did is on the screen, with the server's own refusal beside it.
func TestEveryRequestTheConsoleMadeIsOnTheLog(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/dev/connectors"] = visitRoute{
		Status: 401,
		Body:   `{"code":"auth/TOKEN_INVALID","detail":"the request carries no usable identity"}`,
	}

	out := visit(t, "http://127.0.0.1:8000/dev", routes)

	require.NotEmpty(t, out.Rows["calls"], "the console made requests and logged none of them")

	logged := false

	for _, row := range out.Rows["calls"] {
		if row[2] == "/api/v1/dev/connectors" {
			require.Equal(t, "401", row[3])
			require.Contains(t, row[5], "auth/TOKEN_INVALID",
				"the log shows a refusal without the code the reader would search for")

			logged = true
		}
	}

	require.True(t, logged, "a refused request is missing from the log")
}

// TestTwoLoadsOfOneAreaDoNotDoubleItsRows is the defect a live server found.
//
// Every listing emptied its table and then went to the network, so two loads in
// flight at once cleared twice and appended twice: one clear, the other clear,
// then both sets of rows. It shows as a configuration screen listing the same
// endpoint twice, and it is reached by pressing a tab while a write is still
// reloading behind it, which is the ordinary way a console is used.
func TestTwoLoadsOfOneAreaDoNotDoubleItsRows(t *testing.T) {
	t.Parallel()

	once := visit(t, "http://127.0.0.1:8000/dev?tab=config", configRoutes())
	require.Len(t, once.Rows["endpoints"], 1)

	// The area is opened again while the first load is still in flight, which is
	// what a second press of the tab does.
	twice := use(t, "http://127.0.0.1:8000/dev?tab=config", configRoutes(), nil,
		"tab-config", "tab-config")

	require.Len(t, twice.Rows["endpoints"], 1, "two loads at once listed the endpoint twice")
	require.Empty(t, twice.Rows["presets"])
	require.Len(t, twice.Rows["config-resources"], 1)
}

// TestAWriteRereadsTheWholeOverlayAndNotOnlyItsOwnList is the other defect the
// live server found.
//
// The version a write quotes covers the whole overlay, not the row. Writing a
// sequence moves the token every endpoint and every instance was read at, so a
// console that re-read only the sequences held two stale tokens, and the next
// write to either was refused with a version the reader had never seen.
func TestAWriteRereadsTheWholeOverlayAndNotOnlyItsOwnList(t *testing.T) {
	t.Parallel()

	routes := configRoutes()
	routes["PUT /api/v1/gcode-presets/smoke"] = visitRoute{Status: 201, Body: `{}`}

	out := use(t, "http://127.0.0.1:8000/dev?tab=config", routes,
		map[string]any{"preset-name": "smoke", "preset-steps": "M117 hello"},
		"preset-save")

	written := -1

	for at, call := range out.Calls {
		if call.Method == http.MethodPut && call.Path == "/api/v1/gcode-presets/smoke" {
			written = at
		}
	}

	require.Positive(t, written, "the sequence was never written")

	after := make(map[string]bool)
	for _, call := range out.Calls[written+1:] {
		if call.Method == http.MethodGet {
			after[call.Path] = true
		}
	}

	require.True(t, after["/api/v1/connector-endpoints"],
		"the endpoints were not re-read, so their version is now the stale one")
	require.True(t, after["/api/v1/resources"],
		"the instances were not re-read, so their version is now the stale one")
	require.True(t, after["/api/v1/gcode-presets"])
}

// TestAWriteReportsWhatItDidAndNotWhatTheReloadRead is the third. Every write is
// followed by a re-read, and a re-read reports what it read, so an outcome
// written before it is replaced a moment later and every save on the screen
// looks like it did nothing.
func TestAWriteReportsWhatItDidAndNotWhatTheReloadRead(t *testing.T) {
	t.Parallel()

	routes := configRoutes()
	routes["PUT /api/v1/gcode-presets/smoke"] = visitRoute{Status: 201, Body: `{}`}

	out := use(t, "http://127.0.0.1:8000/dev?tab=config", routes,
		map[string]any{"preset-name": "smoke", "preset-steps": "M117 hello"},
		"preset-save")

	require.Contains(t, out.Text["preset-outcome"], "written")
	require.Equal(t, "note ok", out.ClassName["preset-outcome"])
}

// TestTheChecklistReadsAStorageInstanceByTheNameReadinessGivesIt is the third
// defect a live server found.
//
// The readiness verdict names an instance as kind:name, and the check matched
// kind/name, so a server with a storage serving was told it had none and sent to
// declare a second one over the top of the first.
func TestTheChecklistReadsAStorageInstanceByTheNameReadinessGivesIt(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/readiness"] = visitRoute{Body: `{"ready":true,"serving":1,"declared":1,` +
		`"blocked":[],"instances":[{"resource":"storage:local","state":"ready",` +
		`"required":true,"serving":true}]}`}

	out := visit(t, "http://127.0.0.1:8000/dev?tab=setup", routes)

	states := make(map[string]string, len(out.Rows["setup-checklist"]))
	for _, row := range out.Rows["setup-checklist"] {
		states[row[0]] = row[1]
	}

	require.Equal(t, "done", states["somewhere to keep files"],
		"a storage that is serving was read as absent")
}

// TestAnInstanceListThisCallerMayNotSeeIsNotReadAsAnEmptyServer is the other
// half. The verdict counts every declared instance and lists only the ones the
// caller may see, so an empty list under a positive count is a permission rather
// than a server with nothing configured.
func TestAnInstanceListThisCallerMayNotSeeIsNotReadAsAnEmptyServer(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/readiness"] = visitRoute{
		Body: `{"ready":true,"serving":2,"declared":2,"blocked":[],"instances":[]}`,
	}

	out := visit(t, "http://127.0.0.1:8000/dev?tab=setup", routes)

	notes := make(map[string]string, len(out.Rows["setup-checklist"]))
	for _, row := range out.Rows["setup-checklist"] {
		notes[row[0]] = row[2]
	}

	require.Contains(t, notes["somewhere to keep files"], "may not see the 2 declared instances")
}

// TestACursorStampIsNotFiledAsAnEvent is the fourth defect a live server found.
//
// A window that moves a reader's place without publishing anything arrives as a
// block carrying an id and nothing else. Filing it as an event put a row with no
// type and no payload in the store, and a blank line at the top of the table
// that says what has arrived.
func TestACursorStampIsNotFiledAsAnEvent(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/events"] = visitRoute{Frames: []string{
		firstFrame,
		"id: 01M163N67E6BEYXEGZXAB2TWSX:printer/p1=3,storage/local=3\n\n",
		"id: 01M163N67E6BEYXEGZXAB2TWSY:printer/p1=4,storage/local=3\n\n",
	}}

	out := visit(t, "http://127.0.0.1:8000/dev?tab=events", routes)

	require.Len(t, out.Rows["events-rows"], 1, "a cursor stamp was filed as an event")

	for _, row := range out.Rows["events-counts"] {
		require.NotEmpty(t, row[0], "the type counts carry a row with no type")
	}

	require.Contains(t, out.Text["events-summary"], "1 events held")
}

// TestADeploymentThatAuthenticatesNobodyIsNotReportedAsAlreadySetUp is the
// fifth defect a live server found, and it is what `make demo` is.
//
// A deployment with neither a password login nor an issuer has no account store
// at all, so nothing needs setup and nothing was set up. Reading the one flag
// reported it as a server that already has an administrator, which sends a
// reader looking for the account that closed the path. There is not one.
func TestADeploymentThatAuthenticatesNobodyIsNotReportedAsAlreadySetUp(t *testing.T) {
	t.Parallel()

	routes := fleetRoutes()
	routes["/api/v1/auth/info"] = visitRoute{
		Body: `{"provider":"","credentials":false,"redirect":false,"needs_setup":false}`,
	}

	out := visit(t, "http://127.0.0.1:8000/dev?tab=setup", routes)

	require.Contains(t, out.Text["setup-state"], "authenticates nobody")
	require.NotContains(t, out.Text["setup-state"], "already has an account")

	notes := make(map[string]string, len(out.Rows["setup-checklist"]))
	for _, row := range out.Rows["setup-checklist"] {
		notes[row[0]] = row[2]
	}

	require.Contains(t, notes["an administrator exists"], "no account to create")
}

// TestAServerWithAPasswordLoginStillReportsTheSetupPathClosed is the mutation
// guard beside it. The wording above must not swallow the ordinary case, which
// is a deployment that does authenticate and has been set up.
func TestAServerWithAPasswordLoginStillReportsTheSetupPathClosed(t *testing.T) {
	t.Parallel()

	out := visit(t, "http://127.0.0.1:8000/dev?tab=setup", fleetRoutes())

	require.Contains(t, out.Text["setup-state"], "already has an account")
}
