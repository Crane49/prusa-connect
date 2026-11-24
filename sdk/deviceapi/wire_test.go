package deviceapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// fakeFleet is the host side under test. It records what it was asked and
// answers what the case wants.
type fakeFleet struct {
	page         deviceapi.Page
	device       deviceapi.Device
	result       deviceapi.Result
	registration deviceapi.Registration
	jobs         deviceapi.Jobs
	listing      deviceapi.Listing
	started      deviceapi.Started
	history      deviceapi.JobPage
	sawHistory   deviceapi.JobHistoryParams
	err          error
	sawList      deviceapi.ListParams
	sawFiles     deviceapi.FilesParams
	sawPrint     deviceapi.PrintParams
	sent         deviceapi.Sent
	sawSend      deviceapi.SendParams
	sawID        string
	sawVerb      string
	sawArgs      map[string]string
	sawCode      string
	// sawPrincipal is who the last call named, so a case holds that the
	// principal a module passed reaches the host as it was written.
	sawPrincipal string
}

func (f *fakeFleet) List(_ context.Context, params deviceapi.ListParams) (deviceapi.Page, error) {
	f.sawList, f.sawPrincipal = params, params.Principal

	return f.page, f.err
}

func (f *fakeFleet) Get(_ context.Context, params deviceapi.GetParams) (deviceapi.Device, error) {
	f.sawID, f.sawPrincipal = params.ID, params.Principal

	return f.device, f.err
}

func (f *fakeFleet) Command(_ context.Context, params deviceapi.CommandParams) (deviceapi.Result, error) {
	f.sawID, f.sawVerb, f.sawArgs, f.sawPrincipal = params.ID, params.Verb, params.Args, params.Principal

	return f.result, f.err
}

func (f *fakeFleet) Approve(
	_ context.Context, params deviceapi.ApproveParams,
) (deviceapi.Registration, error) {
	f.sawCode, f.sawPrincipal = params.Code, params.Principal

	return f.registration, f.err
}

func (f *fakeFleet) Jobs(_ context.Context, params deviceapi.JobsParams) (deviceapi.Jobs, error) {
	f.sawID, f.sawPrincipal = params.ID, params.Principal

	return f.jobs, f.err
}

func (f *fakeFleet) Files(_ context.Context, params deviceapi.FilesParams) (deviceapi.Listing, error) {
	f.sawFiles, f.sawID, f.sawPrincipal = params, params.ID, params.Principal

	return f.listing, f.err
}

func (f *fakeFleet) JobHistory(
	_ context.Context, params deviceapi.JobHistoryParams,
) (deviceapi.JobPage, error) {
	f.sawHistory, f.sawID, f.sawPrincipal = params, params.ID, params.Principal

	return f.history, f.err
}

func (f *fakeFleet) Print(_ context.Context, params deviceapi.PrintParams) (deviceapi.Started, error) {
	f.sawPrint, f.sawID, f.sawPrincipal = params, params.ID, params.Principal

	return f.started, f.err
}

func (f *fakeFleet) Send(_ context.Context, params deviceapi.SendParams) (deviceapi.Sent, error) {
	f.sawSend, f.sawID, f.sawPrincipal = params, params.ID, params.Principal

	return f.sent, f.err
}

// call runs one handler the way a session would, and returns what crossed.
func call(t *testing.T, methods wirerpc.Methods, method wirerpc.Method, params any) (any, error) {
	t.Helper()

	handler, ok := methods[method]
	if !ok {
		t.Fatalf("%s is not in the method set", method)
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode the params: %v", err)
	}

	if params == nil {
		encoded = nil
	}

	return handler(t.Context(), wirerpc.Call{Method: method, Params: encoded})
}

// A module that asks for no narrowing gets the first page, because a listing
// with no query is the question a module asks first.
func TestAListingWithNoParamsIsTheFirstPage(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{page: deviceapi.Page{Total: 3}}

	if _, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodList, nil); err != nil {
		t.Fatalf("list: %v", err)
	}

	if fleet.sawList.Limit != 0 || fleet.sawList.Offset != 0 {
		t.Fatalf("the host was asked for %+v, want the zero query", fleet.sawList)
	}
}

// A limit over the published maximum is refused and not clamped, so a module
// that asked for more than the contract serves learns that it did.
func TestAPageOverTheMaximumIsRefused(t *testing.T) {
	t.Parallel()

	params := deviceapi.ListParams{Limit: deviceapi.MaxPageLimit + 1}

	if err := params.Validate(); !errors.Is(err, deviceapi.ErrInvalidQuery) {
		t.Fatalf("Validate = %v, want ErrInvalidQuery", err)
	}

	_, err := call(t, deviceapi.Handlers(&fakeFleet{}), deviceapi.MethodList, params)

	var coded *wirerpc.Error
	if !errors.As(err, &coded) || coded.Code != deviceapi.CodeInvalidQuery {
		t.Fatalf("the handler answered %v, want code %d", err, deviceapi.CodeInvalidQuery)
	}
}

// The bound is checked on the host as well as in the client, because a limit
// checked only in a client is a limit a peer that is not this client never made.
func TestTheHostChecksTheBoundAClientAlreadyChecked(t *testing.T) {
	t.Parallel()

	// Straight at the handler, which is where a peer that is not this client
	// arrives.
	_, err := call(t, deviceapi.Handlers(&fakeFleet{}),
		deviceapi.MethodCommand, deviceapi.CommandParams{ID: "p1"})

	var coded *wirerpc.Error
	if !errors.As(err, &coded) || coded.Code != deviceapi.CodeVerbUnknown {
		t.Fatalf("a command with no verb answered %v, want code %d", err, deviceapi.CodeVerbUnknown)
	}
}

// A refusal the contract numbers crosses whole, so the module raises the same
// sentinel the host did.
func TestACodedRefusalReachesTheModuleAsItsSentinel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		code     int
		sentinel error
	}{
		{"no device", deviceapi.CodeNoDevice, deviceapi.ErrNoDevice},
		{"unknown verb", deviceapi.CodeVerbUnknown, deviceapi.ErrVerbUnknown},
		{"argument", deviceapi.CodeArgument, deviceapi.ErrArgument},
		{"not ready", deviceapi.CodeNotReady, deviceapi.ErrNotReady},
		{"busy", deviceapi.CodeBusy, deviceapi.ErrBusy},
		{"name taken", deviceapi.CodeNameTaken, deviceapi.ErrNameTaken},
		{"print refused", deviceapi.CodePrintRefused, deviceapi.ErrPrintRefused},
		{"print failed", deviceapi.CodePrintFailed, deviceapi.ErrPrintFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := deviceapi.NewClient(refusing{code: tc.code, message: "the host said so"})

			_, err := client.Get(t.Context(), deviceapi.GetParams{ID: "p1"})
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("Get = %v, want %v", err, tc.sentinel)
			}

			if !strings.Contains(err.Error(), "the host said so") {
				t.Fatalf("the host's message did not cross: %v", err)
			}
		})
	}
}

// A failure of the host's own does not name its internals to a module.
func TestAHostFailureDoesNotCross(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{err: errors.New("the sqlite file is locked by pid 4021")}

	_, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodGet, deviceapi.GetParams{ID: "p1"})

	var coded *wirerpc.Error
	if !errors.As(err, &coded) {
		t.Fatalf("the handler answered %v, want a coded error", err)
	}

	if strings.Contains(coded.Message, "sqlite") || strings.Contains(coded.Message, "4021") {
		t.Fatalf("the host's own failure crossed: %q", coded.Message)
	}

	if coded.Message != deviceapi.HostFailed {
		t.Fatalf("message = %q, want %q", coded.Message, deviceapi.HostFailed)
	}
}

// A host that serves no fleet answers CodeMethodNotFound rather than binding a
// nil that panics inside a supervised start.
func TestAHostWithNoFleetPublishesNoMethods(t *testing.T) {
	t.Parallel()

	if methods := deviceapi.Handlers(nil); len(methods) != 0 {
		t.Fatalf("Handlers(nil) published %d methods, want none", len(methods))
	}
}

// A device that refused the verb is an answer and not a transport failure: the
// call reached the machine and the machine spoke.
func TestARefusedCommandIsAnAnswer(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{result: deviceapi.Result{OK: false, MachineReason: "FILE_ALREADY_THERE"}}

	answer, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodCommand,
		deviceapi.CommandParams{ID: "p1", Verb: "core.command.start_print"})
	if err != nil {
		t.Fatalf("command: %v", err)
	}

	result, ok := answer.(deviceapi.Result)
	if !ok {
		t.Fatalf("answer is %T, want deviceapi.Result", answer)
	}

	if result.OK || result.MachineReason != "FILE_ALREADY_THERE" {
		t.Fatalf("result = %+v, want the machine's own refusal carried through", result)
	}
}

// The arguments reach the host as the caller wrote them.
func TestTheArgumentsReachTheHostUnchanged(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{}
	args := map[string]string{"path": "usb/a.bgcode"}

	if _, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodCommand,
		deviceapi.CommandParams{ID: "p1", Verb: "core.command.start_print", Args: args}); err != nil {
		t.Fatalf("command: %v", err)
	}

	if fleet.sawArgs["path"] != "usb/a.bgcode" {
		t.Fatalf("the host saw %+v, want the caller's arguments", fleet.sawArgs)
	}
}

// refusing is a session that answers every call with one coded error.
type refusing struct {
	code    int
	message string
}

func (r refusing) Call(context.Context, wirerpc.Method, any) (json.RawMessage, error) {
	return nil, &wirerpc.Error{Code: r.code, Message: r.message}
}

// The code the caller names reaches the host as it wrote it, and what the host
// answers comes back whole. A registration is what an operator matches against
// the machine in front of them, so a field dropped in transit is a decision made
// on less than was reported.
func TestAnApprovalCarriesTheCodeAndAnswersTheRegistration(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{registration: deviceapi.Registration{
		Code:        "K7PQ2XR4MN",
		Connector:   "prusa-xbuddy",
		Fingerprint: "f1",
		Serial:      "SN1",
		Model:       "MK4",
		Firmware:    "6.0.0",
		Approved:    true,
	}}

	answer, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodApprove,
		deviceapi.ApproveParams{Code: "K7PQ2XR4MN"})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	if fleet.sawCode != "K7PQ2XR4MN" {
		t.Fatalf("the host saw code %q, want the caller's", fleet.sawCode)
	}

	registration, ok := answer.(deviceapi.Registration)
	if !ok {
		t.Fatalf("answer is %T, want deviceapi.Registration", answer)
	}

	if registration != fleet.registration {
		t.Fatalf("registration = %+v, want %+v", registration, fleet.registration)
	}
}

// An approval names a registration, and one that does not is refused before it
// costs a round trip.
func TestAnApprovalNamingNoRegistrationIsRefused(t *testing.T) {
	t.Parallel()

	for name, params := range map[string]deviceapi.ApproveParams{
		"empty": {Code: ""},
		"long":  {Code: strings.Repeat("c", deviceapi.MaxCodeBytes+1)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := params.Validate(); !errors.Is(err, deviceapi.ErrNoRegistration) {
				t.Fatalf("Validate() = %v, want ErrNoRegistration", err)
			}
		})
	}
}

// The two refusals this call adds have to arrive as the sentinels a module
// branches on, rather than as the text of a message.
func TestTheApprovalRefusalsCrossTheWire(t *testing.T) {
	t.Parallel()

	for code, want := range map[int]error{
		deviceapi.CodeNoRegistration: deviceapi.ErrNoRegistration,
		deviceapi.CodeDenied:         deviceapi.ErrDenied,
	} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()

			client := deviceapi.NewClient(refusing{code: code, message: "no"})

			_, err := client.Approve(t.Context(), deviceapi.ApproveParams{Code: "K7PQ2XR4MN"})
			if !errors.Is(err, want) {
				t.Fatalf("Approve() = %v, want %v", err, want)
			}
		})
	}
}

// The principal a module names reaches the host on every method as it was
// written, and a call that names none reaches it as none. The host decides
// what each means; what this holds is that the wire carries the difference.
func TestThePrincipalReachesTheHostOnEveryMethod(t *testing.T) {
	t.Parallel()

	calls := map[wirerpc.Method]func(principal string) any{
		deviceapi.MethodList: func(p string) any { return deviceapi.ListParams{Principal: p} },
		deviceapi.MethodGet:  func(p string) any { return deviceapi.GetParams{Principal: p, ID: "p1"} },
		deviceapi.MethodCommand: func(p string) any {
			return deviceapi.CommandParams{Principal: p, ID: "p1", Verb: "core.command.stop_print"}
		},
		deviceapi.MethodApprove: func(p string) any {
			return deviceapi.ApproveParams{Principal: p, Code: "K7PQ2XR4MN"}
		},
		deviceapi.MethodJobs:  func(p string) any { return deviceapi.JobsParams{Principal: p, ID: "p1"} },
		deviceapi.MethodFiles: func(p string) any { return deviceapi.FilesParams{Principal: p, ID: "p1"} },
	}

	for method, params := range calls {
		for _, principal := range []string{"user:alice", ""} {
			t.Run(string(method)+"/"+principal, func(t *testing.T) {
				t.Parallel()

				fleet := &fakeFleet{}

				if _, err := call(t, deviceapi.Handlers(fleet), method, params(principal)); err != nil {
					t.Fatalf("%s: %v", method, err)
				}

				if fleet.sawPrincipal != principal {
					t.Fatalf("the host saw principal %q, want %q", fleet.sawPrincipal, principal)
				}
			})
		}
	}
}

// A principal the host will not act as is refused with the sentinel a module
// branches on, on every method, and the host's reason crosses with it.
func TestAPrincipalTheHostRefusesIsDeniedOnEveryMethod(t *testing.T) {
	t.Parallel()

	for method, params := range map[wirerpc.Method]any{
		deviceapi.MethodList:    deviceapi.ListParams{Principal: "user:mallory"},
		deviceapi.MethodGet:     deviceapi.GetParams{Principal: "user:mallory", ID: "p1"},
		deviceapi.MethodCommand: deviceapi.CommandParams{Principal: "user:mallory", ID: "p1", Verb: "v"},
		deviceapi.MethodApprove: deviceapi.ApproveParams{Principal: "user:mallory", Code: "K7PQ2XR4MN"},
		deviceapi.MethodJobs:    deviceapi.JobsParams{Principal: "user:mallory", ID: "p1"},
		deviceapi.MethodFiles:   deviceapi.FilesParams{Principal: "user:mallory", ID: "p1", Path: "usb"},
	} {
		t.Run(string(method), func(t *testing.T) {
			t.Parallel()

			// One fake per subtest: the handlers write what they saw, and the
			// subtests run at once.
			fleet := &fakeFleet{
				err: fmt.Errorf("%w: user:mallory was never handed to this module", deviceapi.ErrDenied),
			}

			_, err := call(t, deviceapi.Handlers(fleet), method, params)

			var coded *wirerpc.Error
			if !errors.As(err, &coded) || coded.Code != deviceapi.CodeDenied {
				t.Fatalf("%s answered %v, want code %d", method, err, deviceapi.CodeDenied)
			}

			if !strings.Contains(coded.Message, "user:mallory") {
				t.Fatalf("the host's reason did not cross: %q", coded.Message)
			}
		})
	}
}

// A principal over the bound is refused before it costs a round trip, on the
// same sentinel the host would refuse it under.
func TestAPrincipalOverTheBoundIsRefusedBeforeTheCall(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("p", deviceapi.MaxPrincipalBytes+1)

	for name, params := range map[string]interface{ Validate() error }{
		"list":    deviceapi.ListParams{Principal: long},
		"get":     deviceapi.GetParams{Principal: long, ID: "p1"},
		"command": deviceapi.CommandParams{Principal: long, ID: "p1", Verb: "v"},
		"approve": deviceapi.ApproveParams{Principal: long, Code: "K7PQ2XR4MN"},
		"jobs":    deviceapi.JobsParams{Principal: long, ID: "p1"},
		"files":   deviceapi.FilesParams{Principal: long, ID: "p1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := params.Validate(); !errors.Is(err, deviceapi.ErrDenied) {
				t.Fatalf("Validate() = %v, want ErrDenied", err)
			}
		})
	}
}

// The two prints cross whole, and a machine that has never printed answers
// neither. Ended is what tells the current print from a finished one, so it
// has to arrive as absent and not as the zero time.
func TestTheJobsAnswerCrossesWhole(t *testing.T) {
	t.Parallel()

	ended := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	started := ended.Add(-time.Hour)

	fleet := &fakeFleet{jobs: deviceapi.Jobs{
		Current: &deviceapi.Job{
			ID:          "0199e3a4-5bb5-7aac-8a2c-9c3982e6373f",
			DeviceJobID: "8",
			Path:        "usb/a.bgcode",
			State:       "PRINTING",
			Progress:    40,
			Started:     ended,
		},
		Last: &deviceapi.Job{
			ID:          "0199e2c1-0405-7e48-b0af-e812537ad808",
			DeviceJobID: "7",
			Path:        "usb/b.bgcode",
			State:       "FINISHED",
			Progress:    100,
			Started:     started,
			Ended:       &ended,
		},
	}}

	answer, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodJobs, deviceapi.JobsParams{ID: "p1"})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}

	if fleet.sawID != "p1" {
		t.Fatalf("the host saw device %q, want the caller's", fleet.sawID)
	}

	encoded, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("encode the answer: %v", err)
	}

	var jobs deviceapi.Jobs
	if err = json.Unmarshal(encoded, &jobs); err != nil {
		t.Fatalf("decode the answer: %v", err)
	}

	if jobs.Current == nil || jobs.Current.Ended != nil ||
		jobs.Current.ID != "0199e3a4-5bb5-7aac-8a2c-9c3982e6373f" || jobs.Current.DeviceJobID != "8" {
		t.Fatalf("current = %+v, want the running print with no end", jobs.Current)
	}

	if jobs.Last == nil || jobs.Last.Ended == nil || !jobs.Last.Ended.Equal(ended) {
		t.Fatalf("last = %+v, want the finished print with its end", jobs.Last)
	}

	if strings.Contains(string(encoded), `"ended":"0001`) {
		t.Fatalf("a print with no end crossed as the zero time: %s", encoded)
	}

	// Nothing printed yet is neither member, not two empty rows.
	quiet, err := call(t, deviceapi.Handlers(&fakeFleet{}), deviceapi.MethodJobs, deviceapi.JobsParams{ID: "p1"})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}

	if encoded, err = json.Marshal(quiet); err != nil || string(encoded) != "{}" {
		t.Fatalf("a machine that never printed answered %s, want {}", encoded)
	}
}

// The path and the window reach the host as the caller wrote them, and the
// listing comes back whole: the entry asked for, the window, and the total the
// window was cut from.
func TestAFilesReadCarriesThePathAndTheWindow(t *testing.T) {
	t.Parallel()

	free := int64(4096)
	fleet := &fakeFleet{listing: deviceapi.Listing{
		Entry:    &deviceapi.File{Path: "usb", Name: "usb", Folder: true},
		Storages: []deviceapi.Storage{{Mountpoint: "/usb", Name: "usb", Type: "USB", FreeSpace: &free}},
		Items:    []deviceapi.File{{Path: "usb/part.bgcode", Name: "PART~1.BGC", DisplayName: "part.bgcode", Size: 12}},
		Total:    3,
		Limit:    1,
		Offset:   2,
	}}

	answer, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodFiles,
		deviceapi.FilesParams{ID: "p1", Path: "usb", Limit: 1, Offset: 2})
	if err != nil {
		t.Fatalf("files: %v", err)
	}

	if fleet.sawFiles.Path != "usb" || fleet.sawFiles.Limit != 1 || fleet.sawFiles.Offset != 2 {
		t.Fatalf("the host saw %+v, want the caller's path and window", fleet.sawFiles)
	}

	encoded, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("encode the answer: %v", err)
	}

	var listing deviceapi.Listing
	if err = json.Unmarshal(encoded, &listing); err != nil {
		t.Fatalf("decode the answer: %v", err)
	}

	if listing.Entry == nil || listing.Entry.Path != "usb" || !listing.Entry.Folder {
		t.Fatalf("entry = %+v, want the folder asked for", listing.Entry)
	}

	if len(listing.Items) != 1 || listing.Items[0].DisplayName != "part.bgcode" || listing.Items[0].Size != 12 {
		t.Fatalf("items = %+v, want the one entry the host answered", listing.Items)
	}

	if len(listing.Storages) != 1 || listing.Storages[0].FreeSpace == nil || *listing.Storages[0].FreeSpace != free {
		t.Fatalf("storages = %+v, want the storage with its free space", listing.Storages)
	}

	if listing.Total != 3 || listing.Limit != 1 || listing.Offset != 2 {
		t.Fatalf("window = %d/%d/%d, want total 3, limit 1, offset 2", listing.Total, listing.Limit, listing.Offset)
	}
}

// A files read outside the bounds is refused before it costs a round trip,
// and again on the host, under the sentinel each bound belongs to.
func TestAFilesReadOutsideTheBoundsIsRefused(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		params deviceapi.FilesParams
		want   error
		code   int
	}{
		"no device": {deviceapi.FilesParams{}, deviceapi.ErrNoDevice, deviceapi.CodeNoDevice},
		"long id": {
			deviceapi.FilesParams{ID: strings.Repeat("i", deviceapi.MaxIDBytes+1)},
			deviceapi.ErrNoDevice, deviceapi.CodeNoDevice,
		},
		"long path": {
			deviceapi.FilesParams{ID: "p1", Path: strings.Repeat("p", deviceapi.MaxPathBytes+1)},
			deviceapi.ErrInvalidQuery, deviceapi.CodeInvalidQuery,
		},
		"page over the maximum": {
			deviceapi.FilesParams{ID: "p1", Limit: deviceapi.MaxPageLimit + 1},
			deviceapi.ErrInvalidQuery, deviceapi.CodeInvalidQuery,
		},
		"negative offset": {
			deviceapi.FilesParams{ID: "p1", Offset: -1},
			deviceapi.ErrInvalidQuery, deviceapi.CodeInvalidQuery,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := tc.params.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}

			// One fake per subtest: the handler writes what it saw.
			fleet := &fakeFleet{}

			_, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodFiles, tc.params)

			var coded *wirerpc.Error
			if !errors.As(err, &coded) || coded.Code != tc.code {
				t.Fatalf("the handler answered %v, want code %d", err, tc.code)
			}

			if fleet.sawID != "" {
				t.Fatalf("a call outside the bounds reached the host as %q", fleet.sawID)
			}
		})
	}
}
