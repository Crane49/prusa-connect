package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/prusa3d/buddy-debug-logs/internal/query"
	"github.com/prusa3d/buddy-debug-logs/internal/routes"
	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

// The datagrams a printer sends, in the firmware's own formats.
const (
	syslogLine = `<11>1 - ` + testMAC + ` buddy Connect - [metrics@64164 v="5" msg="17" tm="9000000"] ` +
		`Connect error: connection reset`
	metricsDatagram = "<14>1 - " + testMAC + " buddy - - - msg=4,tm=9000000,v=4 " +
		"temp_bed v=60.5 -20\ntemp_bed v=61.5 980\nfan_speed v=4200i 5\n"
)

// One printer sends a log line and metrics to the ports, and every route, the
// download and the diagnostics bundle read them back, matched to the printer.
func TestWhatAPrinterSendsIsReadBackEverywhere(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	r.send(t, store.Syslog, syslogLine)
	r.send(t, store.Metrics, metricsDatagram)

	var page query.LogPage

	eventually(t, "the log line arriving", func() bool {
		r.get(t, "/logs?device=mk4", &page)

		return len(page.Lines) == 1
	})

	var status struct {
		Streams []struct {
			Stream       string     `json:"stream"`
			LastReceived *time.Time `json:"last_received"`
		} `json:"streams"`
	}
	r.get(t, "/status", &status)

	for _, stream := range status.Streams {
		if stream.Stream == "syslog" && stream.LastReceived == nil {
			t.Error("the status does not say when the log line arrived")
		}
	}

	line := page.Lines[0]
	if line.Device != "mk4" || line.Severity != "error" || line.Component != "Connect" ||
		line.Text != "Connect error: connection reset" || line.Seq != 17 || line.IP != "127.0.0.1" {
		t.Errorf("the line reads %+v", line)
	}

	var names struct{ Names []query.Name }

	eventually(t, "the metric points arriving", func() bool {
		r.get(t, "/metrics/names?device=mk4", &names)

		return len(names.Names) == 2
	})

	if names.Names[0].Name != "fan_speed" || names.Names[1].Name != "temp_bed" || names.Names[1].Count != 2 {
		t.Errorf("names = %+v", names.Names)
	}

	var series struct{ Points []query.Point }
	r.get(t, "/metrics/series?device=mk4&name=temp_bed&max_points=1", &series)

	if len(series.Points) != 1 || series.Points[0].Value != 61 || series.Points[0].Min != 60.5 {
		t.Errorf("series = %+v", series.Points)
	}

	var export query.ExportPage
	r.get(t, "/export?stream=syslog&device=mk4", &export)

	if !strings.Contains(export.Text, " mk4 ERROR Connect: Connect error: connection reset\n") || export.Next != "" {
		t.Errorf("the download reads %q", export.Text)
	}

	files := r.collectAll(t)

	if !strings.Contains(files["syslog/mk4.log"], "ERROR Connect: Connect error: connection reset") {
		t.Errorf("the bundle's log reads %q", files["syslog/mk4.log"])
	}

	if !strings.Contains(files["metrics/mk4.lp"], "fan_speed v=4200i 5") {
		t.Errorf("the bundle's metrics read %q", files["metrics/mk4.lp"])
	}

	if !strings.Contains(files["sources.json"], testMAC) || !strings.Contains(files["status.json"], `"storing": true`) {
		t.Errorf("the bundle's first files read %v", files)
	}
}

// A newer line is found with the cursor the page holds, and the older page
// with the cursor the first page answered.
func TestTheLogPagesBackAndPollsForward(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	for i := range 3 {
		r.send(t, store.Syslog, strings.Replace(syslogLine, `msg="17"`, fmt.Sprintf(`msg="%d"`, i), 1))
	}

	var page query.LogPage

	eventually(t, "three lines arriving", func() bool {
		r.get(t, "/logs", &page)

		return len(page.Lines) == 3
	})

	var first query.LogPage
	r.get(t, "/logs?limit=2", &first)

	if len(first.Lines) != 2 || first.Older == "" {
		t.Fatalf("the first page is %+v", first)
	}

	var older query.LogPage
	r.get(t, "/logs?limit=2&before="+first.Older, &older)

	if len(older.Lines) != 1 || older.Lines[0].Seq != 0 || older.Older != "" {
		t.Fatalf("the older page is %+v", older)
	}

	r.send(t, store.Syslog, strings.Replace(syslogLine, `msg="17"`, `msg="99"`, 1))

	var newer query.LogPage

	eventually(t, "the new line arriving", func() bool {
		r.get(t, "/logs?after="+first.Newest, &newer)

		return len(newer.Lines) == 1
	})

	if newer.Lines[0].Seq != 99 || newer.Newest == first.Newest {
		t.Errorf("the newer page is %+v", newer)
	}
}

// The filters narrow the log: the level, the component and a piece of text.
func TestTheFiltersNarrowTheLog(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	r.send(t, store.Syslog, syslogLine)
	r.send(t, store.Syslog, `<14>1 - `+testMAC+` buddy GUI - [metrics@64164 v="5" msg="18" tm="1"] screen changed`)

	var page query.LogPage

	eventually(t, "two lines arriving", func() bool {
		r.get(t, "/logs", &page)

		return len(page.Lines) == 2
	})

	for path, want := range map[string]int{
		"/logs?severity=warning": 1,
		"/logs?severity=info":    2,
		"/logs?component=gui":    1,
		"/logs?q=RESET":          1,
		"/logs?device=nobody":    0,
		"/logs?mac=" + testMAC:   2,
	} {
		var narrowed query.LogPage
		if status := r.get(t, path, &narrowed); status != http.StatusOK || len(narrowed.Lines) != want {
			t.Errorf("%s answered %d with %d lines, want %d", path, status, len(narrowed.Lines), want)
		}
	}

	for _, path := range []string{"/logs?severity=loud", "/logs?since=yesterday", "/logs?limit=0", "/logs?before=x"} {
		if status := r.get(t, path, nil); status != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", path, status)
		}
	}
}

// The setup sends M334 as the person who asked, with port 0 for a stream that
// is off, and the module never answers the printer's question itself.
func TestTheSetupSendsM334AsThePerson(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	syslogPort, metricsPort := r.port(t, store.Syslog), r.port(t, store.Metrics)

	for body, want := range map[string]string{
		`{"host":"192.168.1.10","syslog":true,"metrics":true}`:                  fmt.Sprintf("M334 192.168.1.10 %d %d", metricsPort, syslogPort),
		`{"host":"192.168.1.10","syslog":true,"metrics":false}`:                 fmt.Sprintf("M334 192.168.1.10 0 %d", syslogPort),
		`{"host":"pcl.lan","syslog":false,"metrics":true,"metrics_port":18514}`: "M334 pcl.lan 18514 0",
		`{"host":"","syslog":false,"metrics":false}`:                            "M334",
	} {
		response := r.call(t, routesapi.Request{
			Method: http.MethodPost, Path: "/devices/mk4/setup", Principal: "user:alice", Body: []byte(body),
		})
		if response.Status != http.StatusAccepted {
			t.Fatalf("%s answered %d: %s", body, response.Status, response.Body)
		}

		sent := r.devices.Commands()
		last := sent[len(sent)-1]

		if last.Args["gcode"] != want || last.Principal != "user:alice" || last.Verb != "core.command.send_gcode" {
			t.Errorf("%s sent %+v, want %q as user:alice", body, last, want)
		}
	}

	if got := len(r.devices.Commands()); got != 4 {
		t.Errorf("the module sent %d commands; it never answers the printer's question itself", got)
	}
}

// A setup the module cannot send is refused with a code the page shows.
func TestASetupTheModuleCannotSendIsRefused(t *testing.T) {
	t.Parallel()

	r := configuredRig(t, wirerpc.Grants{
		Data:   t.TempDir(),
		Listen: map[string]wirerpc.ListenGrant{listenSyslog: {Network: "udp", Port: 0}},
	})

	for body, code := range map[string]string{
		`{"host":"a-name-longer-than-twenty","syslog":true,"metrics":false}`: routes.Invalid.Name,
		`{"host":"bad host","syslog":true,"metrics":false}`:                  routes.Invalid.Name,
		`{"host":"10.0.0.1","syslog":false,"metrics":true}`:                  routes.NotListening.Name,
		`{"host":"10.0.0.1","syslog":true,"metrics":false,"extra":1}`:        routes.Invalid.Name,
	} {
		if got := refusalOf(t, r, "/devices/mk4/setup", body); got != code {
			t.Errorf("%s was refused with %q, want %q", body, got, code)
		}
	}

	if got := refusalOf(
		t,
		r,
		"/devices/nobody/setup",
		`{"host":"10.0.0.1","syslog":true,"metrics":false}`,
	); got != routes.DeviceNotFound.Name {
		t.Errorf("an unknown printer was refused with %q", got)
	}

	r.devices.Refuse = deviceapi.ErrDenied

	if got := refusalOf(
		t,
		r,
		"/devices/mk4/setup",
		`{"host":"10.0.0.1","syslog":true,"metrics":false}`,
	); got != routes.GCodeNotAllowed.Name {
		t.Errorf("a person with no raw G-code grant was refused with %q", got)
	}
}

func refusalOf(t *testing.T, r rig, path, body string) string {
	t.Helper()

	response := r.call(
		t,
		routesapi.Request{Method: http.MethodPost, Path: path, Principal: "user:alice", Body: []byte(body)},
	)

	var refusal struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body, &refusal); err != nil {
		t.Fatalf("%s answered %d %s", path, response.Status, response.Body)
	}

	return refusal.Code
}

// A module granted no port runs, says so, and listens on nothing. A module
// granted no data directory keeps nothing and says why.
func TestAModuleGrantedLessSaysWhatItLacks(t *testing.T) {
	t.Parallel()

	noPorts := configuredRig(t, wirerpc.Grants{Data: t.TempDir()})

	var status routes.Status
	noPorts.get(t, "/status", &status)

	for _, stream := range status.Streams {
		if stream.Listener.Granted || stream.Listener.Listening ||
			!strings.Contains(stream.Listener.Error, "approve_listen") {
			t.Errorf("%s reads %+v", stream.Stream, stream.Listener)
		}
	}

	noData := configuredRig(t, wirerpc.Grants{})

	if status := noData.get(t, "/logs", nil); status != http.StatusServiceUnavailable {
		t.Errorf("GET /logs without a data directory answered %d", status)
	}

	noData.get(t, "/status", &status)

	if status.Storing || status.Problem == "" {
		t.Errorf("the status reads %+v", status)
	}

	files := noData.collectAll(t)
	if len(files) != 1 || !strings.Contains(files["status.json"], "no data directory") {
		t.Errorf("the bundle reads %v", files)
	}
}

// A setting out of range refuses the start, and the operator reads why.
func TestASettingOutOfRangeRefusesTheStart(t *testing.T) {
	t.Parallel()

	held := &module{devices: nil, now: nil}
	serves := held.methods()

	params, err := json.Marshal(wirerpc.Settings{Config: json.RawMessage(`{"retention_hours":0,"max_mib":128}`)})
	if err != nil {
		t.Fatalf("render the settings: %v", err)
	}

	if _, err = serves[wirerpc.MethodConfigure](context.Background(),
		wirerpc.Call{Method: wirerpc.MethodConfigure, Params: params}); err == nil {
		t.Error("retention_hours 0 was accepted")
	}
}

// A MAC is matched to a printer by hand.
func TestASourceIsAssignedByHand(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	stranger := strings.ReplaceAll(syslogLine, testMAC, "aa:bb:cc:dd:ee:ff")
	r.send(t, store.Syslog, stranger)

	eventually(t, "the stranger's line arriving", func() bool {
		var page query.LogPage
		r.get(t, "/logs?mac=aa:bb:cc:dd:ee:ff", &page)

		return len(page.Lines) == 1
	})

	response := r.call(t, routesapi.Request{
		Method: http.MethodPut, Path: "/sources/aa:bb:cc:dd:ee:ff", Principal: "user:alice",
		Body: []byte(`{"device_id":"mk4"}`),
	})
	if response.Status != http.StatusOK {
		t.Fatalf("assign answered %d %s", response.Status, response.Body)
	}

	var page query.LogPage
	r.get(t, "/logs?device=mk4", &page)

	if len(page.Lines) != 1 {
		t.Errorf("after the assignment the printer has %d lines, want the stranger's one", len(page.Lines))
	}

	if got := refusalOfPut(t, r, "/sources/11:22:33:44:55:66"); got != routes.SourceNotFound.Name {
		t.Errorf("an unseen MAC was refused with %q", got)
	}
}

func refusalOfPut(t *testing.T, r rig, path string) string {
	t.Helper()

	response := r.call(
		t,
		routesapi.Request{
			Method:    http.MethodPut,
			Path:      path,
			Principal: "user:alice",
			Body:      []byte(`{"device_id":"mk4"}`),
		},
	)

	var refusal struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(response.Body, &refusal)

	return refusal.Code
}

// A purge of everything empties the log, and a purge the person cannot have
// meant is refused before anything is removed.
func TestAPurgeRemovesWhatWasKept(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.send(t, store.Syslog, syslogLine)

	var page query.LogPage

	eventually(t, "the log line arriving", func() bool {
		r.get(t, "/logs?device=mk4", &page)

		return len(page.Lines) == 1
	})

	for body, code := range map[string]string{
		`{"older_than_hours":0}`:          "buddy-debug-logs/INVALID",
		`{"streams":["kernel"]}`:          "buddy-debug-logs/INVALID",
		`{"streams":["syslog","syslog"]}`: "buddy-debug-logs/INVALID",
	} {
		if got := refusalOf(t, r, "/data/purge", body); got != code {
			t.Errorf("%s answered %s, want %s", body, got, code)
		}
	}

	r.get(t, "/logs?device=mk4", &page)
	if len(page.Lines) != 1 {
		t.Fatalf("a refused purge removed lines: %d left", len(page.Lines))
	}

	answer := r.call(t, routesapi.Request{
		Method: http.MethodPost, Path: "/data/purge", Principal: "user:alice", Body: []byte(`{}`),
	})
	if answer.Status != http.StatusOK || !strings.Contains(string(answer.Body), `"records":1`) {
		t.Fatalf("purge answered %d %s", answer.Status, answer.Body)
	}

	r.get(t, "/logs?device=mk4", &page)
	if len(page.Lines) != 0 {
		t.Errorf("%d lines left after a purge of everything", len(page.Lines))
	}
}

// getAsBob makes one GET as bob and decodes the answer.
func (r rig) getAsBob(t *testing.T, path string, into any) {
	t.Helper()

	response := r.call(t, routesapi.Request{Method: http.MethodGet, Path: path, Principal: "user:bob"})

	if err := json.Unmarshal(response.Body, into); err != nil {
		t.Fatalf("GET %s answered %d %s: %v", path, response.Status, response.Body, err)
	}
}

// A person reads the records of the printers they may see and no others. The
// records of a MAC matched to no printer, and a purge, take a person who may
// see every printer.
func TestAPersonReadsOnlyThePrintersTheyMaySee(t *testing.T) {
	t.Parallel()

	const (
		xlMAC       = "10:9c:70:00:00:02"
		strangerMAC = "02:00:00:00:00:09"
	)

	r := newRig(t)
	r.devices.Add("xl", "XL", xlMAC, "", "")
	r.devices.Sees("user:bob", "mk4")

	if err := r.module.current().Book.Sources.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	for _, mac := range []string{testMAC, xlMAC, strangerMAC} {
		r.send(t, store.Syslog, strings.Replace(syslogLine, testMAC, mac, 1))
		r.send(t, store.Metrics, strings.Replace(metricsDatagram, testMAC, mac, 1))
	}

	var page query.LogPage

	eventually(t, "three lines arriving", func() bool {
		r.get(t, "/logs", &page)

		return len(page.Lines) == 3
	})

	eventually(t, "the metrics arriving", func() bool {
		var names struct{ Names []query.Name }
		r.get(t, "/metrics/names", &names)

		return len(names.Names) == 2 && names.Names[0].Count == 3
	})

	r.getAsBob(t, "/logs", &page)

	if len(page.Lines) != 1 || page.Lines[0].Device != "mk4" {
		t.Errorf("bob reads %+v", page.Lines)
	}

	if r.getAsBob(t, "/logs?device=xl", &page); len(page.Lines) != 0 {
		t.Errorf("bob reads the lines of a printer he may not see: %+v", page.Lines)
	}

	var listed struct{ Sources []struct{ MAC string } }
	if r.getAsBob(t, "/sources", &listed); len(listed.Sources) != 1 || listed.Sources[0].MAC != testMAC {
		t.Errorf("bob lists %+v", listed.Sources)
	}

	var names struct{ Names []query.Name }
	if r.getAsBob(t, "/metrics/names", &names); len(names.Names) != 2 || names.Names[0].Count != 1 {
		t.Errorf("bob reads the names %+v", names.Names)
	}

	var export query.ExportPage
	if r.getAsBob(t, "/export?stream=syslog", &export); strings.Count(export.Text, "\n") != 1 ||
		strings.Contains(export.Text, xlMAC) || strings.Contains(export.Text, strangerMAC) {
		t.Errorf("bob downloads %q", export.Text)
	}

	for path, code := range map[string]string{
		"/sources/" + strangerMAC: routes.SourceNotFound.Name,
		"/sources/" + xlMAC:       routes.SourceNotFound.Name,
	} {
		if got := refusalOfPutAs(t, r, "user:bob", path, `{"device_id":"mk4"}`); got != code {
			t.Errorf("bob assigning %s was answered %q, want %q", path, got, code)
		}
	}

	got := refusalOfPutAs(t, r, "user:bob", "/sources/"+testMAC, `{"device_id":"xl"}`)
	if got != routes.DeviceNotFound.Name {
		t.Errorf("bob moving his printer's MAC to another printer was answered %q", got)
	}

	answer := r.call(t, routesapi.Request{
		Method: http.MethodPost, Path: "/data/purge", Principal: "user:bob", Body: []byte(`{}`),
	})
	if !strings.Contains(string(answer.Body), routes.PurgeNotAllowed.Name) {
		t.Errorf("bob's purge answered %d %s", answer.Status, answer.Body)
	}

	if r.get(t, "/logs", &page); len(page.Lines) != 3 {
		t.Errorf("after bob's purge alice reads %d lines, want 3", len(page.Lines))
	}
}

func refusalOfPutAs(t *testing.T, r rig, principal, path, body string) string {
	t.Helper()

	response := r.call(t, routesapi.Request{
		Method: http.MethodPut, Path: path, Principal: principal, Body: []byte(body),
	})

	var refusal struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body, &refusal); err != nil {
		t.Fatalf("%s answered %d %s", path, response.Status, response.Body)
	}

	return refusal.Code
}

// A cursor the page held before a purge is refused as stale, and the page
// reads the newest lines again.
func TestACursorFromBeforeAPurgeIsStale(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.send(t, store.Syslog, syslogLine)

	var page query.LogPage

	eventually(t, "the log line arriving", func() bool {
		r.get(t, "/logs", &page)

		return len(page.Lines) == 1
	})

	answer := r.call(t, routesapi.Request{
		Method: http.MethodPost, Path: "/data/purge", Principal: "user:alice", Body: []byte(`{"older_than_hours":1}`),
	})
	if answer.Status != http.StatusOK {
		t.Fatalf("purge answered %d %s", answer.Status, answer.Body)
	}

	var refusal struct{ Code string }
	if status := r.get(t, "/logs?after="+page.Newest, &refusal); status != http.StatusConflict ||
		refusal.Code != routes.CursorStale.Name {
		t.Errorf("a cursor from before the purge answered %d %s", status, refusal.Code)
	}
}

// When the server does not answer which printers the caller may see, no
// record is shown.
func TestNoRecordIsShownWhenTheFleetCannotBeRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.devices.RefuseList(deviceapi.ErrDenied)

	var refusal struct{ Code string }
	if status := r.get(t, "/logs", &refusal); status != http.StatusServiceUnavailable ||
		refusal.Code != routes.FleetUnavailable.Name {
		t.Errorf("/logs answered %d %s", status, refusal.Code)
	}
}
