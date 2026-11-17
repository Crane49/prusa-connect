package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// noPrinters is the fleet a server that is not being tested for printers gets.
// It holds none, which is what a process nothing has dialled into looks like.
type noPrinters struct{}

func (noPrinters) List(context.Context, string, DeviceQuery) (DevicePage, error) {
	return DevicePage{}, nil
}

func (noPrinters) Get(_ context.Context, _ string, id string) (Device, error) {
	return Device{}, fault.PrinterNotFound.New("no connected printer answers to that id").
		With(fault.StringParam("device_id", id))
}

func (noPrinters) Capabilities(_ context.Context, _ string, id string) (DeviceCapabilitySet, error) {
	return DeviceCapabilitySet{}, fault.PrinterNotFound.New("no connected printer answers to that id").
		With(fault.StringParam("device_id", id))
}

func (noPrinters) Jobs(_ context.Context, _ string, id string) (DeviceJobs, error) {
	return DeviceJobs{}, fault.PrinterNotFound.New("no connected printer answers to that id").
		With(fault.StringParam("device_id", id))
}

func (noPrinters) Job(_ context.Context, _ string, jobID string) (DeviceJob, error) {
	return DeviceJob{}, fault.PrinterJobNotFound.New("no print answers to that id").
		With(fault.StringParam("job_id", jobID))
}

func (noPrinters) Command(_ context.Context, _ string, id string, _ PrinterCommand) (PrinterResult, error) {
	return PrinterResult{}, fault.PrinterNotFound.New("no connected printer answers to that id").
		With(fault.StringParam("device_id", id))
}

func (noPrinters) Storages(_ context.Context, _ string, id string, _ bool) (DeviceStorages, error) {
	return DeviceStorages{}, fault.PrinterNotFound.New("no connected printer answers to that id").
		With(fault.StringParam("device_id", id))
}

func (noPrinters) Files(_ context.Context, _ string, id string, _ string, _ bool) (DeviceListing, error) {
	return DeviceListing{}, fault.PrinterNotFound.New("no connected printer answers to that id").
		With(fault.StringParam("device_id", id))
}

func (noPrinters) Send(
	_ context.Context, _ string, id string, _ DeviceUpload,
) (DeviceTransfer, error) {
	return DeviceTransfer{}, noSuchPrinter(id)
}

func (noPrinters) Transfers(_ context.Context, _ string, id string) ([]DeviceTransfer, error) {
	return nil, noSuchPrinter(id)
}

func (noPrinters) StopTransfer(_ context.Context, _ string, id string, _ string) error {
	return noSuchPrinter(id)
}

func (noPrinters) RunningTransfers(context.Context, string) ([]RunningTransfer, error) {
	return []RunningTransfer{}, nil
}

func (noPrinters) CreateFolder(_ context.Context, _ string, id string, _ string) error {
	return noSuchPrinter(id)
}

func (noPrinters) DeleteFile(_ context.Context, _ string, id string, _ string) error {
	return noSuchPrinter(id)
}

func (noPrinters) DeleteFolder(_ context.Context, _ string, id string, _ string) error {
	return noSuchPrinter(id)
}

func (noPrinters) Thumbnail(
	_ context.Context, _ string, id string, _ string, _ int64, _ int64,
) (StorageThumbnail, error) {
	return StorageThumbnail{}, noSuchPrinter(id)
}

func (noPrinters) SetTags(_ context.Context, _ string, id string, _ []string) ([]string, error) {
	return nil, noSuchPrinter(id)
}

func (noPrinters) SetLabels(_ context.Context, _ string, id string, _ map[string]string) (map[string]string, error) {
	return nil, noSuchPrinter(id)
}

func (noPrinters) SetName(_ context.Context, _ string, id string, _ string) (string, error) {
	return "", noSuchPrinter(id)
}

func (noPrinters) Retire(_ context.Context, _ string, id string) error {
	return noSuchPrinter(id)
}

func (noPrinters) JobHistory(
	_ context.Context, _ string, id string, _ DeviceJobQuery,
) (DeviceJobPage, error) {
	return DeviceJobPage{}, noSuchPrinter(id)
}

func (noPrinters) ListJobs(context.Context, string, JobQuery) (JobPage, error) {
	return JobPage{Items: []DeviceJob{}}, nil
}

func (noPrinters) Print(
	_ context.Context, _ string, id string, _ DevicePrint,
) (DevicePrintResult, error) {
	return DevicePrintResult{}, noSuchPrinter(id)
}

func (noPrinters) StartPrint(
	_ context.Context, _ string, id string, _ DevicePrint,
) (DevicePrintResult, error) {
	return DevicePrintResult{}, noSuchPrinter(id)
}

func noSuchPrinter(id string) error {
	return fault.PrinterNotFound.New("no connected printer answers to that id").
		With(fault.StringParam("device_id", id))
}

// fakeFleet is a fleet with one printer, and a record of what was commanded.
type fakeFleet struct {
	// mu guards every slice a call appends to. One fleet may stand behind a
	// server that parallel subtests call at the same time.
	mu sync.Mutex

	printer  Device
	resolved DeviceCapabilitySet
	result   PrinterResult
	refuse   error
	received []PrinterCommand

	storages []DeviceStorage
	listing  DeviceListing
	listed   []string
	picture  StorageThumbnail
	sent     []DeviceUpload
	created  []string
	removed  []string

	// transfers is what the transfer listing answers with, and stopped records
	// the ids the stop route asked for.
	transfers []DeviceTransfer
	stopped   []string
	// running is what the listing of every running transfer answers with,
	// and runningFor records the principal each call was made for.
	running    []RunningTransfer
	runningFor []string

	// extra are the printers the listing returns after the one Get answers
	// with, and issues is the rollup it reports over the whole fleet. Both are
	// here so a listing example carrying more than one printer has something
	// that can produce it.
	extra  []Device
	issues DeviceIssueSummary

	// queries records what every listing asked the fleet for, which is what a
	// test asserts against to tell a handler that reads the query parameters
	// from one that binds them and drops them.
	queries []DeviceQuery
	total   int

	// labelled records every label set a write handed the fleet, and tagged
	// every tag list, so a case can tell a handler that binds the body from one
	// that drops it.
	labelled []map[string]string
	tagged   [][]string

	// named records every name a rename handed the fleet, and retired every id
	// the retirement route asked for. Both are here for the reason labelled is.
	named   []string
	retired []string

	// history is what the job history route answers with, and jobQueries what
	// every such request asked the fleet for.
	history    DeviceJobPage
	jobQueries []DeviceJobQuery

	// jobs is what the jobs route answers for the printer this fleet holds.
	jobs DeviceJobs

	// fleetJobs is what the fleet's jobs route answers, and fleetJobQueries
	// every query that route handed the fleet.
	fleetJobs       JobPage
	fleetJobQueries []JobQuery
}

func (f *fakeFleet) ListJobs(_ context.Context, _ string, query JobQuery) (JobPage, error) {
	f.record(func() { f.fleetJobQueries = append(f.fleetJobQueries, query) })

	return f.fleetJobs, f.refuse
}

// record runs one write to the fleet's recorded calls under its lock.
func (f *fakeFleet) record(write func()) {
	f.mu.Lock()
	defer f.mu.Unlock()

	write()
}

func (f *fakeFleet) Storages(_ context.Context, _ string, id string, _ bool) (DeviceStorages, error) {
	if id != f.printer.ID {
		return DeviceStorages{}, fault.PrinterNotFound.New("no connected printer answers to that id").
			With(fault.StringParam("device_id", id))
	}

	return DeviceStorages{Items: f.storages, Reported: true}, nil
}

func (f *fakeFleet) Files(_ context.Context, _ string, id string, path string, _ bool) (DeviceListing, error) {
	if id != f.printer.ID {
		return DeviceListing{}, fault.PrinterNotFound.New("no connected printer answers to that id").
			With(fault.StringParam("device_id", id))
	}

	f.record(func() { f.listed = append(f.listed, path) })

	if f.refuse != nil {
		return DeviceListing{}, f.refuse
	}

	return f.listing, nil
}

func (f *fakeFleet) Thumbnail(
	_ context.Context, _ string, id string, path string, _ int64, _ int64,
) (StorageThumbnail, error) {
	if id != f.printer.ID {
		return StorageThumbnail{}, noSuchPrinter(id)
	}

	if f.picture.Bytes == nil {
		return StorageThumbnail{}, fault.StorageNoThumbnail.New("the file carries no picture").
			With(fault.StringParam("path", path))
	}

	return f.picture, nil
}

func (f *fakeFleet) Send(
	_ context.Context, _ string, id string, upload DeviceUpload,
) (DeviceTransfer, error) {
	if id != f.printer.ID {
		return DeviceTransfer{}, fault.PrinterNotFound.New("no connected printer answers to that id").
			With(fault.StringParam("device_id", id))
	}

	f.record(func() { f.sent = append(f.sent, upload) })

	if f.refuse != nil {
		return DeviceTransfer{}, f.refuse
	}

	return DeviceTransfer{TransferID: "01TRANSFER", Storage: upload.Storage,
		Source: upload.Source, Path: upload.Path, State: "done"}, nil
}

func (f *fakeFleet) Transfers(_ context.Context, _ string, id string) ([]DeviceTransfer, error) {
	if id != f.printer.ID {
		return nil, fault.PrinterNotFound.New("no connected printer answers to that id").
			With(fault.StringParam("device_id", id))
	}

	return f.transfers, nil
}

func (f *fakeFleet) RunningTransfers(_ context.Context, principal string) ([]RunningTransfer, error) {
	f.record(func() { f.runningFor = append(f.runningFor, principal) })

	return f.running, nil
}

func (f *fakeFleet) StopTransfer(_ context.Context, _ string, id string, transfer string) error {
	if id != f.printer.ID {
		return fault.PrinterNotFound.New("no connected printer answers to that id").
			With(fault.StringParam("device_id", id))
	}

	f.record(func() { f.stopped = append(f.stopped, transfer) })

	return f.refuse
}

func (f *fakeFleet) CreateFolder(_ context.Context, _ string, id string, path string) error {
	if id != f.printer.ID {
		return noSuchPrinter(id)
	}

	f.record(func() { f.created = append(f.created, path) })

	return f.refuse
}

func (f *fakeFleet) DeleteFile(_ context.Context, _ string, id string, path string) error {
	if id != f.printer.ID {
		return noSuchPrinter(id)
	}

	f.record(func() { f.removed = append(f.removed, "file:"+path) })

	return f.refuse
}

func (f *fakeFleet) DeleteFolder(_ context.Context, _ string, id string, path string) error {
	if id != f.printer.ID {
		return noSuchPrinter(id)
	}

	f.record(func() { f.removed = append(f.removed, "folder:"+path) })

	return f.refuse
}

func (f *fakeFleet) SetTags(_ context.Context, _ string, id string, tags []string) ([]string, error) {
	if id != f.printer.ID {
		return nil, noSuchPrinter(id)
	}

	if f.refuse != nil {
		return nil, f.refuse
	}

	f.record(func() { f.tagged = append(f.tagged, tags) })

	return tags, nil
}

func (f *fakeFleet) SetLabels(
	_ context.Context, _ string, id string, labels map[string]string,
) (map[string]string, error) {
	if id != f.printer.ID {
		return nil, noSuchPrinter(id)
	}

	if f.refuse != nil {
		return nil, f.refuse
	}

	f.record(func() { f.labelled = append(f.labelled, labels) })

	return labels, nil
}

func (f *fakeFleet) SetName(_ context.Context, _ string, id string, name string) (string, error) {
	if id != f.printer.ID {
		return "", noSuchPrinter(id)
	}

	if f.refuse != nil {
		return "", f.refuse
	}

	f.record(func() { f.named = append(f.named, name) })

	return name, nil
}

func (f *fakeFleet) Retire(_ context.Context, _ string, id string) error {
	if id != f.printer.ID {
		return noSuchPrinter(id)
	}

	if f.refuse != nil {
		return f.refuse
	}

	f.record(func() { f.retired = append(f.retired, id) })

	return nil
}

func (f *fakeFleet) JobHistory(
	_ context.Context, _ string, id string, query DeviceJobQuery,
) (DeviceJobPage, error) {
	if id != f.printer.ID {
		return DeviceJobPage{}, noSuchPrinter(id)
	}

	f.record(func() { f.jobQueries = append(f.jobQueries, query) })

	return f.history, f.refuse
}

func (f *fakeFleet) List(_ context.Context, _ string, query DeviceQuery) (DevicePage, error) {
	f.record(func() { f.queries = append(f.queries, query) })

	return DevicePage{
		Items:  append([]Device{f.printer}, f.extra...),
		Total:  f.total,
		Limit:  query.Limit,
		Offset: query.Offset,
		Issues: f.issues,

		Filters: exampleFilters(),
		Sorts:   exampleSorts(),
		Sort:    cmp.Or(query.Sort, "urgency"),
		Order:   cmp.Or(query.Order, "asc"),
	}, nil
}

func (f *fakeFleet) Get(_ context.Context, _ string, id string) (Device, error) {
	if id != f.printer.ID {
		return Device{}, fault.PrinterNotFound.New("no connected printer answers to that id").
			With(fault.StringParam("device_id", id))
	}

	return f.printer, nil
}

func (f *fakeFleet) Capabilities(_ context.Context, _ string, id string) (DeviceCapabilitySet, error) {
	if id != f.printer.ID {
		return DeviceCapabilitySet{}, fault.PrinterNotFound.New("no connected printer answers to that id").
			With(fault.StringParam("device_id", id))
	}

	return f.resolved, nil
}

func (f *fakeFleet) Job(_ context.Context, _ string, jobID string) (DeviceJob, error) {
	for _, held := range []*DeviceJob{f.jobs.Current, f.jobs.Last} {
		if held != nil && held.ID == jobID {
			return *held, nil
		}
	}

	return DeviceJob{}, fault.PrinterJobNotFound.New("no print answers to that id").
		With(fault.StringParam("job_id", jobID))
}

func (f *fakeFleet) Jobs(_ context.Context, _ string, id string) (DeviceJobs, error) {
	if id != f.printer.ID {
		return DeviceJobs{}, fault.PrinterNotFound.New("no connected printer answers to that id").
			With(fault.StringParam("device_id", id))
	}

	return f.jobs, nil
}

func (f *fakeFleet) Command(
	_ context.Context, _ string, id string, cmd PrinterCommand,
) (PrinterResult, error) {
	if id != f.printer.ID {
		return PrinterResult{}, fault.PrinterNotFound.New("no connected printer answers to that id").
			With(fault.StringParam("device_id", id))
	}

	f.record(func() { f.received = append(f.received, cmd) })

	if f.refuse != nil {
		return PrinterResult{}, f.refuse
	}

	return f.result, nil
}

// relayAndStart is the two halves a print is made of, so a fake composes them
// the way the device plane composes the real ones.
type relayAndStart interface {
	Send(ctx context.Context, principal string, id string, upload DeviceUpload) (DeviceTransfer, error)
	Command(ctx context.Context, principal string, id string, cmd PrinterCommand) (PrinterResult, error)
}

// printThrough relays the file and starts the print on one fake. The fake is
// passed in rather than reached through an embedded struct, so a fake that
// overrides Send or Command is the one this calls.
func printThrough(
	ctx context.Context, fleet relayAndStart, principal string, id string, request DevicePrint,
) (DevicePrintResult, error) {
	if err := ValidatePrint(request); err != nil {
		return DevicePrintResult{}, err
	}

	out := DevicePrintResult{Path: request.Path}

	if request.Storage != "" {
		transfer, err := fleet.Send(ctx, principal, id, DeviceUpload(request))
		if err != nil {
			return DevicePrintResult{}, err
		}

		out.Transfer = &transfer
	}

	result, err := fleet.Command(ctx, principal, id, PrinterCommand{
		Verb: StartPrintVerb,
		Args: map[string]string{StartPrintPathArgument: request.Path},
	})
	if err != nil {
		return DevicePrintResult{}, err
	}

	out.Command = result

	return out, nil
}

func (f *fakeFleet) Print(
	ctx context.Context, principal string, id string, request DevicePrint,
) (DevicePrintResult, error) {
	return printThrough(ctx, f, principal, id, request)
}

func (f *fakeFleet) StartPrint(
	ctx context.Context, principal string, id string, request DevicePrint,
) (DevicePrintResult, error) {
	return f.Print(ctx, principal, id, request)
}

// TestThePrinterSurfaceServesWhatTheFleetHolds is the whole surface on the real
// router: the listing, one printer with the state it reported, and a command
// reaching the fleet with the verb and arguments it was sent.
func TestThePrinterSurfaceServesWhatTheFleetHolds(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{
		printer: Device{
			ID:              "pdeadbeef",
			Name:            "MK4 SIM-1",
			Vendor:          "Prusa",
			Model:           "MK4",
			Firmware:        "6.4.0",
			Serial:          "SIM-1",
			Driver:          "xbuddy",
			Connected:       true,
			Capabilities:    []string{"SET_NOZZLE_TEMPERATURE"},
			CapabilityEpoch: 3,
			ProfileMatched:  true,
			State:           DeviceState{"/tools/0/nozzle_temp": json.RawMessage(`215.5`)},
		},
		result: PrinterResult{OK: true},
	}

	handler := serverWithFleet(t, fleet).Handler()

	listed := call(t, handler, http.MethodGet, devicesPath, "")
	require.Equal(t, http.StatusOK, listed.Code)
	require.Contains(t, listed.Body.String(), `"id":"pdeadbeef"`)
	require.Contains(t, listed.Body.String(), `"capability_epoch":3`)

	one := call(t, handler, http.MethodGet, devicesPath+"/pdeadbeef", "")
	require.Equal(t, http.StatusOK, one.Code)
	require.Contains(t, one.Body.String(), `"/tools/0/nozzle_temp":215.5`,
		"the state tree is re-encoded rather than passed through as the printer wrote it")

	sent := call(t, handler, http.MethodPost, devicesPath+"/pdeadbeef/commands",
		`{"verb":"SET_NOZZLE_TEMPERATURE","args":{"temperature":"215"}}`)
	require.Equal(t, http.StatusOK, sent.Code)
	require.Contains(t, sent.Body.String(), `"ok":true`)

	require.Equal(t, []PrinterCommand{{
		Verb: "SET_NOZZLE_TEMPERATURE",
		Args: map[string]string{"temperature": "215"},
	}}, fleet.received)
}

// TestTheFleetListingCarriesThePageTheCallerAskedFor is earlier research at the
// transport: an operation that binds no input leaves a client with four hundred
// printers holding fifty of them and no request that returns the fifty-first.
//
// The assertion is on what the fleet was asked for, not only on what came back.
// A handler that declares the parameters and drops them answers a page-two
// request with page one, and on a small fleet the two look identical.
func TestTheFleetListingCarriesThePageTheCallerAskedFor(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: Device{ID: "pdeadbeef"}, total: 400}
	handler := serverWithFleet(t, fleet).Handler()

	listed := call(t, handler, http.MethodGet, devicesPath+"?limit=25&offset=75", "")
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())

	require.Equal(t, []DeviceQuery{{Page: Page{Limit: 25, Offset: 75}}}, fleet.queries,
		"the page the caller asked for never reached the fleet")

	var body DevicePage
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &body))
	require.Equal(t, 400, body.Total, "the answer does not say how large the fleet is")
	require.Equal(t, 25, body.Limit)
	require.Equal(t, 75, body.Offset)

	// A client that sends neither asks for the server's default page, which is
	// what every caller written against the unpaged operation still does.
	unpaged := call(t, handler, http.MethodGet, devicesPath, "")
	require.Equal(t, http.StatusOK, unpaged.Code)
	require.Equal(t, DeviceQuery{}, fleet.queries[1])

	// A negative window is refused rather than silently reinterpreted.
	refused := call(t, handler, http.MethodGet, devicesPath+"?offset=-1", "")
	require.Equal(t, http.StatusUnprocessableEntity, refused.Code, refused.Body.String())
	require.Len(t, fleet.queries, 2, "a refused page reached the fleet")
}

// TestAPrinterTheCallerCannotReachIsAbsent holds the refusal to one shape. A
// printer nobody may see and one that is not connected answer the same, so a
// refusal never tells anybody which printers exist.
func TestAPrinterTheCallerCannotReachIsAbsent(t *testing.T) {
	t.Parallel()

	handler := serverWithFleet(t, &fakeFleet{printer: Device{ID: "pknown"}}).Handler()

	for _, target := range []string{
		devicesPath + "/pmissing",
		devicesPath + "/pmissing/commands",
	} {
		method := http.MethodGet
		body := ""

		if target != devicesPath+"/pmissing" {
			method = http.MethodPost
			body = `{"verb":"HOME"}`
		}

		rec := call(t, handler, method, target, body)

		require.Equal(t, http.StatusNotFound, rec.Code)
		require.Equal(t, problemMediaType, rec.Header().Get("Content-Type"))
		require.Contains(t, rec.Body.String(), fault.PrinterNotFound.Code().String())
	}
}

// TestACommandRefusalCarriesTheCodeTheCallerActsOn proves the descriptor's own
// refusal survives the transport: a verb the printer does not have is a 409
// carrying the registered code and the verb, not a generic failure.
func TestACommandRefusalCarriesTheCodeTheCallerActsOn(t *testing.T) {
	t.Parallel()

	refusal := fault.PrinterCommandRejected.New("the printer is printing").
		With(fault.StringParam("device_id", "pknown"))

	fleet := &fakeFleet{printer: Device{ID: "pknown"}, refuse: refusal}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodPost, devicesPath+"/pknown/commands", `{"verb":"HOME"}`)

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), fault.PrinterCommandRejected.Code().String())
	require.Contains(t, rec.Body.String(), "pknown")
}

// TestAPrinterRefusalAndAnAPIRefusalAreDifferentAnswers is the line between the
// two refusals, read as a client reads them.
//
// A command this server declined to send is a problem document with a registered
// code and an error status. A command the printer refused was sent and answered,
// so it is 200 with a result body, and the machine's own word for the refusal is
// carried there rather than dressed as one of this API's codes.
func TestAPrinterRefusalAndAnAPIRefusalAreDifferentAnswers(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{
		printer: Device{ID: "pknown"},
		result:  PrinterResult{OK: false, MachineReason: "Device is busy"},
	}
	handler := serverWithFleet(t, fleet).Handler()

	byThePrinter := call(t, handler, http.MethodPost, devicesPath+"/pknown/commands", `{"verb":"HOME"}`)
	t.Logf("observed: the printer's refusal answered %d %s %s",
		byThePrinter.Code, byThePrinter.Header().Get("Content-Type"), byThePrinter.Body.String())

	require.Equal(t, http.StatusOK, byThePrinter.Code, byThePrinter.Body.String())
	require.Contains(t, byThePrinter.Header().Get("Content-Type"), "application/json")
	require.NotEqual(t, problemMediaType, byThePrinter.Header().Get("Content-Type"),
		"a printer that refused a command is reported as this server failing to carry it")

	var result PrinterResult
	require.NoError(t, json.Unmarshal(byThePrinter.Body.Bytes(), &result))
	require.False(t, result.OK)
	require.Equal(t, "Device is busy", result.MachineReason,
		"the printer's own sentence did not reach the caller")

	// The same endpoint, refused by this server instead. It is an error status
	// carrying a code a client branches on, and it carries no machine reason,
	// because no machine was asked.
	fleet.refuse = fault.PrinterCommandRejected.New("the printer is printing")

	byTheAPI := call(t, handler, http.MethodPost, devicesPath+"/pknown/commands", `{"verb":"HOME"}`)
	t.Logf("observed: this server's refusal answered %d %s %s",
		byTheAPI.Code, byTheAPI.Header().Get("Content-Type"), byTheAPI.Body.String())

	require.Equal(t, http.StatusConflict, byTheAPI.Code)
	require.Equal(t, problemMediaType, byTheAPI.Header().Get("Content-Type"))
	require.NotContains(t, byTheAPI.Body.String(), "machine_reason")
}

// TestTheCommandSchemaNamesTheMachineReasonAsTheMachinesOwnWord holds the
// published document to the one thing a client must know about the field before
// it writes a switch over it: nothing enumerates the values.
//
// A generated client reads the document, not this package. A field published as
// a bare string with no description is one a client author reasonably reads as a
// stable code, and the codes on this API are the fault codes.
func TestTheCommandSchemaNamesTheMachineReasonAsTheMachinesOwnWord(t *testing.T) {
	t.Parallel()

	committed, err := os.ReadFile(committedSpecPath)
	require.NoError(t, err)

	var document map[string]any
	require.NoError(t, json.Unmarshal(committed, &document))

	schema := schemaAt(t, document, "PrinterResult")
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "PrinterResult publishes no properties")

	reason, ok := properties["machine_reason"].(map[string]any)
	require.True(t, ok, "the committed document does not publish machine_reason on PrinterResult")

	description, _ := reason["description"].(string)
	t.Logf("observed: machine_reason is described as %q", description)

	require.NotEmpty(t, description, "the field is published with no description")
	require.NotContains(t, schema["required"], "machine_reason",
		"machine_reason is required, and an accepted command carries none")
}

// schemaAt returns one named component schema out of the committed document.
func schemaAt(t *testing.T, document map[string]any, name string) map[string]any {
	t.Helper()

	components, ok := document["components"].(map[string]any)
	require.True(t, ok, "the document has no components")

	schemas, ok := components["schemas"].(map[string]any)
	require.True(t, ok, "the document has no component schemas")

	schema, ok := schemas[name].(map[string]any)
	require.True(t, ok, "the document has no %s schema", name)

	return schema
}

// serverWithFleet is a server whose printers are the caller's.
func serverWithFleet(t *testing.T, fleet DeviceFleet) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    fleet,
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.NoError(t, err)

	return server
}

// noStorages is a server with no storage declared, which is what most of these
// tests are: the storage routes are published whatever an operator wrote, so
// every Config needs something to answer them.
type noStorages struct{}

func (noStorages) List(context.Context, string) ([]Storage, error) { return nil, nil }

func (noStorages) Files(context.Context, string, string, string) ([]StorageEntry, error) {
	return nil, noSuchStorageHere()
}

func (noStorages) Open(context.Context, string, string, string) (Object, error) {
	return Object{}, noSuchStorageHere()
}

func (noStorages) Stat(context.Context, string, string, string) (StorageEntry, error) {
	return StorageEntry{}, noSuchStorageHere()
}

func (noStorages) Thumbnail(context.Context, string, string, string) (StorageThumbnail, error) {
	return StorageThumbnail{}, noSuchStorageHere()
}

func (noStorages) FileVersion(context.Context, string, string, string) (FileVersion, error) {
	return FileVersion{}, noSuchStorageHere()
}

func (noStorages) FileExtent(context.Context, string, string, string) (FileExtent, error) {
	return FileExtent{}, noSuchStorageHere()
}

func (noStorages) Save(context.Context, string, string, string, io.Reader) (SaveResult, error) {
	return SaveResult{}, noSuchStorageHere()
}

func (noStorages) SaveTemporary(context.Context, string, string, string, io.Reader) (SaveResult, error) {
	return SaveResult{}, noSuchStorageHere()
}

func (noStorages) Delete(context.Context, string, string, string) error { return noSuchStorageHere() }

func (noStorages) DeleteMany(context.Context, string, string, []string) error {
	return noSuchStorageHere()
}

func (noStorages) DownloadURL(context.Context, string, string, string) (string, bool, error) {
	return "", false, noSuchStorageHere()
}

func (noStorages) CreateFolder(context.Context, string, string, string) error {
	return noSuchStorageHere()
}

func (noStorages) Move(context.Context, string, string, string, string) error {
	return noSuchStorageHere()
}

func noSuchStorageHere() error {
	return fault.StorageNotFound.New("no storage of that name is serving")
}
