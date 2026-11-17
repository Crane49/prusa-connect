package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The module is driven the way the host drives it: a routes.serve call
// carrying the principal the host decided, answered over a real session with a
// fake core on the other end of it. Nothing here opens a socket, because the
// module opens none, and nothing here calls a resolver directly: a query that
// resolves has been through the schema, the wire and the fake fleet.

// fakeCore is the host side of sdk/deviceapi under test. It answers what the
// case wants and records what it was asked, so a case can hold that the
// principal the host handed the module is the one the module asked as.
type fakeCore struct {
	mu sync.Mutex

	page         deviceapi.Page
	device       deviceapi.Device
	jobs         deviceapi.Jobs
	listing      deviceapi.Listing
	result       deviceapi.Result
	registration deviceapi.Registration
	err          error

	sawPrincipal string
	sawID        string
	sawVerb      string
	sawArgs      map[string]string
	sawCode      string
	sawList      deviceapi.ListParams
	sawFiles     deviceapi.FilesParams
}

func (f *fakeCore) List(_ context.Context, params deviceapi.ListParams) (deviceapi.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sawList, f.sawPrincipal = params, params.Principal

	return f.page, f.err
}

func (f *fakeCore) Get(_ context.Context, params deviceapi.GetParams) (deviceapi.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sawID, f.sawPrincipal = params.ID, params.Principal

	return f.device, f.err
}

func (f *fakeCore) Command(_ context.Context, params deviceapi.CommandParams) (deviceapi.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sawID, f.sawVerb, f.sawArgs, f.sawPrincipal = params.ID, params.Verb, params.Args, params.Principal

	return f.result, f.err
}

func (f *fakeCore) Approve(
	_ context.Context, params deviceapi.ApproveParams,
) (deviceapi.Registration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sawCode, f.sawPrincipal = params.Code, params.Principal

	return f.registration, f.err
}

func (f *fakeCore) Jobs(_ context.Context, params deviceapi.JobsParams) (deviceapi.Jobs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sawID, f.sawPrincipal = params.ID, params.Principal

	return f.jobs, f.err
}

func (f *fakeCore) Files(_ context.Context, params deviceapi.FilesParams) (deviceapi.Listing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sawFiles, f.sawID, f.sawPrincipal = params, params.ID, params.Principal

	return f.listing, f.err
}

func (f *fakeCore) JobHistory(
	_ context.Context, params deviceapi.JobHistoryParams,
) (deviceapi.JobPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sawID, f.sawPrincipal = params.ID, params.Principal

	return deviceapi.JobPage{}, f.err
}

func (f *fakeCore) Print(_ context.Context, params deviceapi.PrintParams) (deviceapi.Started, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sawID, f.sawPrincipal = params.ID, params.Principal

	return deviceapi.Started{}, f.err
}

// asked is what the case reads back off the fake once a query has run.
func (f *fakeCore) asked() fakeCore {
	f.mu.Lock()
	defer f.mu.Unlock()

	return fakeCore{
		sawPrincipal: f.sawPrincipal, sawID: f.sawID, sawVerb: f.sawVerb,
		sawArgs: f.sawArgs, sawCode: f.sawCode, sawList: f.sawList, sawFiles: f.sawFiles,
	}
}

// channel is one end of a duplex pipe pair. Operating system pipes rather than
// net.Pipe, because a pipe with no buffer deadlocks the moment both read loops
// write at once, and a host hands a child a buffered pair.
type channel struct {
	reader *os.File
	writer *os.File
}

func (c channel) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c channel) Write(p []byte) (int, error) { return c.writer.Write(p) }

func (c channel) Close() error { return errors.Join(c.reader.Close(), c.writer.Close()) }

// start runs the module against a fake core over one session, and returns the
// module's own method set.
func start(t *testing.T, core *fakeCore) wirerpc.Methods {
	t.Helper()

	hostReads, childWrites, err := os.Pipe()
	if err != nil {
		t.Fatalf("open a pipe: %v", err)
	}

	childReads, hostWrites, err := os.Pipe()
	if err != nil {
		t.Fatalf("open a pipe: %v", err)
	}

	host, err := wirerpc.NewConn(
		channel{reader: hostReads, writer: hostWrites},
		wirerpc.Options{Role: wirerpc.RoleHost, Methods: deviceapi.Handlers(core)})
	if err != nil {
		t.Fatalf("build the host session: %v", err)
	}

	// The module's own way round: an empty method set, then the session, then
	// the methods filled in, which is what main.run does.
	serves := wirerpc.Methods{}

	child, err := wirerpc.NewConn(
		channel{reader: childReads, writer: childWrites},
		wirerpc.Options{Role: wirerpc.RoleChild, Methods: serves})
	if err != nil {
		t.Fatalf("build the module session: %v", err)
	}

	built, err := methods(deviceapi.NewClient(child))
	if err != nil {
		t.Fatalf("build the module's method set: %v", err)
	}

	maps.Copy(serves, built)

	var running sync.WaitGroup

	running.Add(2)

	go func() { defer running.Done(); _ = host.Serve(t.Context()) }()
	go func() { defer running.Done(); _ = child.Serve(t.Context()) }()

	t.Cleanup(func() {
		_ = host.Close()
		_ = child.Close()
		running.Wait()
	})

	return serves
}

// answer is one GraphQL answer as a client reads it.
type answer struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message    string         `json:"message"`
		Path       []any          `json:"path"`
		Extensions map[string]any `json:"extensions"`
	} `json:"errors"`
}

// caller is who the host says is asking. Every case runs as one person,
// because what a second person would see is the core's decision and not this
// module's: the wire carries the principal out and the graph answers for it.
const caller = "user:alice"

// ask runs one query through the module and reads the answer back.
func ask(t *testing.T, serves wirerpc.Methods, query string) (routesapi.Response, answer) {
	t.Helper()

	body, err := json.Marshal(request{Query: query})
	if err != nil {
		t.Fatalf("render the request: %v", err)
	}

	response := serve(t, serves, routesapi.Request{
		Method: http.MethodPost, Path: "/", Principal: caller, Body: body,
	})

	var read answer
	if err = json.Unmarshal(response.Body, &read); err != nil {
		t.Fatalf("read the answer %q: %v", response.Body, err)
	}

	return response, read
}

// serve makes one routes.serve call against the module's method set.
func serve(t *testing.T, serves wirerpc.Methods, ask routesapi.Request) routesapi.Response {
	t.Helper()

	params, err := json.Marshal(ask)
	if err != nil {
		t.Fatalf("render the request: %v", err)
	}

	handler, carried := serves[routesapi.MethodServe]
	if !carried {
		t.Fatalf("the module answers no %s", routesapi.MethodServe)
	}

	answered, err := handler(t.Context(), wirerpc.Call{Method: routesapi.MethodServe, Params: params})
	if err != nil {
		t.Fatalf("%s %s: %v", ask.Method, ask.Path, err)
	}

	response, ok := answered.(routesapi.Response)
	if !ok {
		t.Fatalf("the module answered %T and not a routesapi.Response", answered)
	}

	if err = response.Validate(); err != nil {
		t.Fatalf("the host would refuse this answer: %v", err)
	}

	return response
}

// fleet is the fake core a case starts from: two printers, one of them
// printing.
func fleet() *fakeCore {
	ended := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)

	return &fakeCore{
		page: deviceapi.Page{
			Items: []deviceapi.Device{
				{
					Kind: "printer", ID: "p3f2a1c9b", Name: "Core One", Vendor: "Prusa",
					Model: "COREONE", Connected: true,
					Capabilities: []string{"core.command.pause_print"},
					State: map[string]json.RawMessage{
						"/state": json.RawMessage(`"PRINTING"`),
					},
					Labels: map[string]string{"room": "workshop"},
				},
				{Kind: "printer", ID: "p8c7d6e5f", Name: "MK4S", Vendor: "Prusa", Model: "MK4S"},
			},
			Total: 2, Limit: 50,
		},
		device: deviceapi.Device{Kind: "printer", ID: "p3f2a1c9b", Name: "Core One"},
		jobs: deviceapi.Jobs{
			Current: &deviceapi.Job{
				ID: "p3f2a1c9b:12", Path: "usb/part.bgcode", DisplayName: "part",
				State: "PRINTING", Progress: 41, TimeRemaining: 900,
				Started: time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC),
			},
			Last: &deviceapi.Job{
				ID: "p3f2a1c9b:11", Path: "usb/bracket.bgcode", DisplayName: "bracket",
				State: "FINISHED", Progress: 100,
				Started: time.Date(2026, time.August, 31, 9, 0, 0, 0, time.UTC),
				Ended:   &ended,
			},
		},
		result:       deviceapi.Result{OK: true},
		registration: deviceapi.Registration{Code: "8QK3", Connector: "xbuddy", Approved: true},
	}
}

func TestAQueryAnswersTheFleetAsThePersonTheHostNamed(t *testing.T) {
	t.Parallel()

	core := fleet()
	serves := start(t, core)

	response, read := ask(t, serves,
		`{ devices { total limit items { id name state } } }`)

	if response.Status != http.StatusOK {
		t.Fatalf("the query answered %d: %s", response.Status, response.Body)
	}

	if len(read.Errors) != 0 {
		t.Fatalf("the query was refused: %+v", read.Errors)
	}

	var page struct {
		Total int `json:"total"`
		Limit int `json:"limit"`
		Items []struct {
			ID    string                     `json:"id"`
			Name  string                     `json:"name"`
			State map[string]json.RawMessage `json:"state"`
		} `json:"items"`
	}

	if err := json.Unmarshal(read.Data["devices"], &page); err != nil {
		t.Fatalf("read the page: %v", err)
	}

	if page.Total != 2 || page.Limit != 50 || len(page.Items) != 2 {
		t.Fatalf("the page read as %+v", page)
	}

	if page.Items[0].ID != "p3f2a1c9b" || page.Items[0].Name != "Core One" {
		t.Errorf("the first row read as %+v", page.Items[0])
	}

	if string(page.Items[0].State["/state"]) != `"PRINTING"` {
		t.Errorf("the state tree read as %v", page.Items[0].State)
	}

	// The whole point of the wire: the call reached the core as the person the
	// host authorized, not as the module.
	if saw := core.asked().sawPrincipal; saw != caller {
		t.Errorf("the core was asked as %q", saw)
	}
}

func TestAMutationSendsOneVerbToOneDevice(t *testing.T) {
	t.Parallel()

	core := fleet()
	core.result = deviceapi.Result{OK: false, MachineReason: "the bed is still hot"}

	serves := start(t, core)

	_, read := ask(t, serves, `mutation {
		send_device_command(
			device_id: "p3f2a1c9b"
			verb: "core.command.set_nozzle_temperature"
			args: {nozzle_temperature: "215"}
		) { ok machine_reason }
	}`)

	if len(read.Errors) != 0 {
		t.Fatalf("the mutation was refused: %+v", read.Errors)
	}

	var result struct {
		OK            bool   `json:"ok"`
		MachineReason string `json:"machine_reason"`
	}

	if err := json.Unmarshal(read.Data["send_device_command"], &result); err != nil {
		t.Fatalf("read the result: %v", err)
	}

	// A device that answered and refused is an answer and not an error.
	if result.OK || result.MachineReason != "the bed is still hot" {
		t.Errorf("the result read as %+v", result)
	}

	saw := core.asked()
	if saw.sawID != "p3f2a1c9b" || saw.sawVerb != "core.command.set_nozzle_temperature" {
		t.Errorf("the core was asked %s %s", saw.sawID, saw.sawVerb)
	}

	if saw.sawArgs["nozzle_temperature"] != "215" {
		t.Errorf("the arguments read as %v", saw.sawArgs)
	}

	if saw.sawPrincipal != caller {
		t.Errorf("the core was asked as %q", saw.sawPrincipal)
	}
}

func TestAMutationAdmitsADeviceWaitingToJoin(t *testing.T) {
	t.Parallel()

	core := fleet()
	serves := start(t, core)

	_, read := ask(t, serves,
		`mutation { approve_registration(code: "8QK3") { code connector approved } }`)

	if len(read.Errors) != 0 {
		t.Fatalf("the mutation was refused: %+v", read.Errors)
	}

	if !strings.Contains(string(read.Data["approve_registration"]), `"approved":true`) {
		t.Errorf("the registration read as %s", read.Data["approve_registration"])
	}

	if saw := core.asked().sawCode; saw != "8QK3" {
		t.Errorf("the core was asked to approve %q", saw)
	}
}

func TestAJobIsAnsweredInTheUnitTheCoreRecordUses(t *testing.T) {
	t.Parallel()

	serves := start(t, fleet())

	_, read := ask(t, serves,
		`{ device_jobs(device_id: "p3f2a1c9b") {
			current { id time_remaining_ms started ended }
			last { id ended }
		} }`)

	if len(read.Errors) != 0 {
		t.Fatalf("the query was refused: %+v", read.Errors)
	}

	var jobs struct {
		Current struct {
			ID              string  `json:"id"`
			TimeRemainingMs int64   `json:"time_remaining_ms"`
			Started         string  `json:"started"`
			Ended           *string `json:"ended"`
		} `json:"current"`
		Last struct {
			ID    string  `json:"id"`
			Ended *string `json:"ended"`
		} `json:"last"`
	}

	if err := json.Unmarshal(read.Data["device_jobs"], &jobs); err != nil {
		t.Fatalf("read the jobs: %v", err)
	}

	// The wire counts seconds and the core's record counts milliseconds. The
	// schema was generated from the record, so the answer is in milliseconds.
	if jobs.Current.TimeRemainingMs != 900_000 {
		t.Errorf("the remaining time read as %d", jobs.Current.TimeRemainingMs)
	}

	if jobs.Current.Started != "2026-09-01T09:00:00Z" {
		t.Errorf("the start read as %q", jobs.Current.Started)
	}

	if jobs.Current.Ended != nil {
		t.Errorf("a print that is running answered an end of %q", *jobs.Current.Ended)
	}

	if jobs.Last.Ended == nil || *jobs.Last.Ended != "2026-09-01T10:00:00Z" {
		t.Errorf("the last print's end read as %v", jobs.Last.Ended)
	}
}

func TestARefusalCarriesTheCoreCodeAndWhereItIsDocumented(t *testing.T) {
	t.Parallel()

	core := fleet()
	core.err = fmt.Errorf("%w: p0000", deviceapi.ErrNoDevice)

	serves := start(t, core)

	response, read := ask(t, serves, `{ device(device_id: "p0000") { id } }`)

	// A query the schema could run answers 200 whatever happened to its
	// fields. The refusal is in the errors list beside the null.
	if response.Status != http.StatusOK {
		t.Fatalf("the refused query answered %d: %s", response.Status, response.Body)
	}

	if len(read.Errors) != 1 {
		t.Fatalf("the errors read as %+v", read.Errors)
	}

	carried := read.Errors[0].Extensions

	if carried["code"] != "core.printer/NOT_FOUND" {
		t.Errorf("the refusal carries the code %v", carried["code"])
	}

	if url, _ := carried["type"].(string); !strings.HasSuffix(url, "#core-printer-not-found") {
		t.Errorf("the refusal points at %v", carried["type"])
	}

	params, _ := carried["params"].(map[string]any)
	if params["device_id"] != "p0000" {
		t.Errorf("the refusal names %v", params)
	}
}

func TestARefusedPermissionIsTheAccessDeniedCode(t *testing.T) {
	t.Parallel()

	core := fleet()
	core.err = fmt.Errorf("%w: user:alice may not read that", deviceapi.ErrDenied)

	serves := start(t, core)

	_, read := ask(t, serves, `{ devices { total } }`)

	if len(read.Errors) != 1 {
		t.Fatalf("the errors read as %+v", read.Errors)
	}

	if code := read.Errors[0].Extensions["code"]; code != "core.auth/ACCESS_DENIED" {
		t.Errorf("the refusal carries the code %v", code)
	}
}

func TestAQueryInTheURLIsRunTheSameWay(t *testing.T) {
	t.Parallel()

	serves := start(t, fleet())

	response := serve(t, serves, routesapi.Request{
		Method: http.MethodGet, Path: "/", Principal: caller,
		Query: "query=" + url("{ devices { total } }"),
	})

	if response.Status != http.StatusOK {
		t.Fatalf("the query answered %d: %s", response.Status, response.Body)
	}

	if !strings.Contains(string(response.Body), `"total":2`) {
		t.Errorf("the answer read as %s", response.Body)
	}
}

func TestARequestThatIsNotAQueryIsRefusedWithAStatus(t *testing.T) {
	t.Parallel()

	serves := start(t, fleet())

	for _, one := range []struct {
		name string
		body string
		code string
	}{
		{name: "not a request object", body: `["devices"]`, code: "graphql/REQUEST_INVALID"},
		{name: "no query", body: `{"query":"  "}`, code: "graphql/REQUEST_INVALID"},
		{
			name: "a query longer than the module runs",
			body: `{"query":"` + strings.Repeat("x", maxQueryBytes+1) + `"}`,
			code: "graphql/QUERY_TOO_LONG",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			response := serve(t, serves, routesapi.Request{
				Method: http.MethodPost, Path: "/", Principal: caller, Body: []byte(one.body),
			})

			if response.Status != http.StatusBadRequest {
				t.Fatalf("it answered %d: %s", response.Status, response.Body)
			}

			var refused struct {
				Code string `json:"code"`
			}

			if err := json.Unmarshal(response.Body, &refused); err != nil {
				t.Fatalf("read the refusal: %v", err)
			}

			if refused.Code != one.code {
				t.Errorf("the refusal carries the code %q and not %q", refused.Code, one.code)
			}
		})
	}
}

// TestThereIsNoSubscriptionRoot is the gate on ADR-20260910-cqq rule 11 from the
// module's side. The wire carries one request and one whole answer, so a
// subscription cannot be served, and a schema that grew one would promise a
// stream nothing can deliver.
func TestThereIsNoSubscriptionRoot(t *testing.T) {
	t.Parallel()

	serves := start(t, fleet())

	_, read := ask(t, serves, `{ __schema { subscriptionType { name } } }`)

	if len(read.Errors) != 0 {
		t.Fatalf("introspection was refused: %+v", read.Errors)
	}

	if !strings.Contains(string(read.Data["__schema"]), `"subscriptionType":null`) {
		t.Errorf("the schema declares a subscription root: %s", read.Data["__schema"])
	}
}

// TestTheFragmentIsOneTheHostAccepts is what a module author runs before the
// host does: the embedded document passes the rule the host holds it to, and
// it rebases under the mount without losing a path.
func TestTheFragmentIsOneTheHostAccepts(t *testing.T) {
	t.Parallel()

	info, err := routesapi.Describe(
		wirerpc.Info{Name: name, Version: release, Contract: wirerpc.Contract}, openAPI)
	if err != nil {
		t.Fatalf("the host would refuse this module's fragment: %v", err)
	}

	fragment, err := routesapi.ParseFragment(info.OpenAPI)
	if err != nil {
		t.Fatalf("parse what plugin.info carries: %v", err)
	}

	rebased, err := fragment.Rebase("/graphql")
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}

	if !strings.Contains(string(rebased), `"/graphql/"`) {
		t.Errorf("the rebased document does not carry the route under the mount: %s", rebased)
	}
}

// url escapes one query for a URL, which is all the test needs of escaping.
func url(query string) string {
	var out strings.Builder

	for _, r := range query {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			out.WriteRune(r)
		default:
			fmt.Fprintf(&out, "%%%02X", r)
		}
	}

	return out.String()
}
