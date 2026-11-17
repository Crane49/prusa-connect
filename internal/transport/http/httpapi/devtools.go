//go:build !nodevtools

package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/devtools"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// devTag groups the developer operations in the reference, so a reader can tell
// the product's surface from the debugging one.
const devTag = "developer"

// developerTags describes the tag every developer operation carries. The
// printer build leaves the operations out, so it describes no such tag.
func developerTags() []*huma.Tag {
	return []*huma.Tag{{
		Name: devTag,
		Description: "What this build carries and what it has done, for somebody debugging this " +
			"server rather than a printer.",
	}}
}

// devPermission is the sentence every developer operation repeats in its own
// description, so a reader of one operation learns what it takes without
// opening the authorization reference.
const devPermission = "It takes develop_device on the driver resource kind, which an org administrator " +
	"holds on day one. The surface says what this binary is made of and what it sent to a machine, " +
	"so it is not conferred by an identity alone and it is not view_resource: reading that a " +
	"connector is declared is part of seeing the configuration, and this is the bench."

// devErrors are what a developer operation answers with. 403 is a caller who
// holds an identity and not develop_device. Every other page of the app
// answers such a caller, so the refusal is documented.
var devErrors = []int{http.StatusUnauthorized, http.StatusForbidden}

// requireDeveloper refuses a caller who does not reach the developer surface.
//
// The object is the driver resource kind, for the reason the registration
// operations name it: a connector is an instance of that kind, and the command
// log is what this server sent through one. There is no device to name, because
// the connector table belongs to the build.
func requireDeveloper(ctx context.Context, grants GrantStore) error {
	const what = "the developer surface"

	who, object, err := kindDecision(ctx, ref.KindDriver)
	if err != nil {
		return err
	}

	allowed, err := grants.Can(ctx, who, authz.ActionDevelopDevice, object)

	return decided(allowed, err, object, authz.ActionDevelopDevice, what)
}

// registerDevtools mounts the developer view and returns the fleet the printer
// operations are registered over.
//
// It returns a fleet because the command log is a decorator: a command that
// goes through this server is recorded on its way to the printer. No other
// endpoint can reconstruct that afterwards. Nothing here touches the frame
// path. The live frames the page draws come from the event hub's own stream,
// read like any other subscriber.
func registerDevtools(api huma.API, cfg Config) DeviceFleet {
	log := devtools.NewCommandLog(devtools.DefaultCommandLimit)

	registerDevPage(api, cfg.Grants)
	registerDevConnectors(api, cfg.Mounts, cfg.Grants)
	registerDevCommands(api, cfg.Devices, cfg.Grants, log)

	return recordingFleet{DeviceFleet: cfg.Devices, log: log}
}

// registerDevPage serves the page itself.
//
// It carries no data. Every panel on it is filled from an operation that
// narrows its answer to the caller, so what a person sees here is what their
// credentials already reach through /api/v1/devices, /api/v1/readiness and
// /api/v1/events, and never more. It still takes the permission, because the
// page is how somebody finds the surface.
func registerDevPage(api huma.API, grants GrantStore) {
	page := devtools.Rendered()

	register(api, huma.Operation{
		OperationID: "get-developer-view",
		Method:      http.MethodGet,
		Path:        devPagePath,
		Summary:     "The developer view",
		Description: "A page for somebody debugging a printer at a bench: the connectors this build " +
			"carries, the resource instances and their supervision state, the printers that have dialled " +
			"in, one printer in detail, and the event stream as it arrives. It holds no data of its own " +
			"and reads the same operations any other client reads, so it shows exactly what the caller's " +
			"credentials already reach.\n\n" + devPermission,
		Tags:      []string{devTag},
		Errors:    devErrors,
		Responses: devPageResponse(),
	}, func(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		if principalFrom(ctx) == "" {
			return nil, fault.AuthTokenInvalid.New("the request carries no usable identity")
		}

		if err := requireDeveloper(ctx, grants); err != nil {
			return nil, err
		}

		return &huma.StreamResponse{Body: func(hctx huma.Context) { writePage(hctx, page) }}, nil
	})
}

// writePage answers with the document, or with 304 for a client that already
// holds it. The bytes change only when the binary does, so the entity-tag is
// the build's and a reload during a debugging session costs one conditional
// request.
func writePage(hctx huma.Context, page devtools.Page) {
	request, writer := humachi.Unwrap(hctx)

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Content-Security-Policy", page.CSP)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("ETag", page.ETag)

	if notModified(request, page.ETag) {
		writer.WriteHeader(http.StatusNotModified)

		return
	}

	writer.WriteHeader(http.StatusOK)

	_, _ = writer.Write(page.Body)
}

// devPageResponse documents the body, which Huma cannot derive: a streaming
// operation's Body field is a function, so there is no struct to reflect over.
func devPageResponse() map[string]*huma.Response {
	return map[string]*huma.Response{
		"200": {
			Description: "The developer view, as one self-contained HTML document. It reaches nothing " +
				"outside this origin and its Content-Security-Policy names its own script and stylesheet " +
				"by content hash.",
			Content: map[string]*huma.MediaType{
				"text/html": {Schema: &huma.Schema{Type: huma.TypeString}},
			},
		},
	}
}

type devConnectorsOutput struct {
	Body devtools.Inventory
}

// registerDevConnectors publishes what this build carries in its device plane.
//
// What it answers is build metadata: which dialects were compiled in, which
// routes this origin serves and why, and how large each driver's baseline is.
// There is no device to name, so the object is the driver resource kind, which
// is the object every other decision about a connector is taken on.
func registerDevConnectors(api huma.API, mounts []Mount, grants GrantStore) {
	// The code that wired the connectors has already drained the registry.
	// Draining it again returns the same registrations, so reading it here
	// seals nothing early and sees the table the host mounted from.
	types, err := connectorapi.NewRegistry()

	var inventory devtools.Inventory
	if err == nil {
		inventory = devtools.ReadInventory(types, routesOf(mounts))
	}

	register(api, huma.Operation{
		OperationID: "get-developer-connectors",
		Method:      http.MethodGet,
		Path:        devConnectorsPath,
		Summary:     "The connectors this build carries",
		Description: "Every printer dialect compiled into this binary, whether its routes are being " +
			"served, and how many capabilities its baseline declares, joined to every path this origin " +
			"answers that the document does not publish. A dialect that could not be built is reported " +
			"as failed rather than left out, because a missing row reads as a build that does not carry " +
			"the dialect at all.\n\n" + devPermission,
		Tags:   []string{devTag},
		Errors: devErrors,
	}, func(ctx context.Context, _ *struct{}) (*devConnectorsOutput, error) {
		if principalFrom(ctx) == "" {
			return nil, fault.AuthTokenInvalid.New("the request carries no usable identity")
		}

		if denied := requireDeveloper(ctx, grants); denied != nil {
			return nil, denied
		}

		if err != nil {
			return nil, httpfault.Unexpected("read the connector type registry").Wrap(err)
		}

		return &devConnectorsOutput{Body: inventory}, nil
	})
}

type devCommandsInput struct {
	DeviceRef
}

type devCommandsOutput struct {
	Body struct {
		Items   []devtools.Command `json:"items"   doc:"The commands this process still holds for the device, oldest first."`
		Dropped uint64             `json:"dropped" doc:"How many commands have fallen out of the log since the process started. The bound is on the whole log, so a busy fleet scrolls it faster than one printer would."`
	}
}

// registerDevCommands publishes the commands this process carried to one
// device. It is reached through the same lookup the device operations use, so a
// device the caller may not act on and one that is not there are the same
// refusal, and no command log is ever handed out for a device somebody cannot
// already see.
//
// The developer permission is checked first and the device lookup second. A
// caller who does not reach this surface is told so. A caller who does reach
// it learns nothing about a device they cannot see, because the lookup answers
// not found for both.
func registerDevCommands(
	api huma.API, fleet DeviceFleet, grants GrantStore, log *devtools.CommandLog,
) {
	register(api, huma.Operation{
		OperationID: "get-developer-device-commands",
		Method:      http.MethodGet,
		Path:        devCommandsPath,
		Summary:     "The commands this process carried to a device",
		Description: "What was sent, what came back, and how long it took, for the commands still in " +
			"this process's bounded log. It records nothing a printer did on its own and nothing that " +
			"arrived through another server: it is what this binary sent while it has been running.\n\n" +
			devPermission,
		Tags:   []string{devTag},
		Errors: append([]int{http.StatusForbidden}, deviceErrors...),
	}, func(ctx context.Context, in *devCommandsInput) (*devCommandsOutput, error) {
		if err := requireDeveloper(ctx, grants); err != nil {
			return nil, err
		}

		if _, err := fleet.Get(ctx, principalFrom(ctx), in.DeviceID); err != nil {
			return nil, err
		}

		items, dropped := log.Recent(in.DeviceID)

		out := &devCommandsOutput{}
		out.Body.Items = items
		out.Body.Dropped = dropped

		return out, nil
	})
}

// routesOf is the origin's non-operation paths in the shape the developer view
// reads them.
func routesOf(mounts []Mount) []devtools.Route {
	out := make([]devtools.Route, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, devtools.Route{Pattern: mount.Pattern, Reason: mount.Reason})
	}

	return out
}

// recordingFleet records every command that goes through this server and
// hands the answer back unchanged.
//
// It sits here and not in the device plane because a command crosses this
// boundary once, in one HTTP request. The log's lock is taken once per command
// and never once per frame or once per subscriber. A command for a printer the
// lookup did not find is not recorded, so a caller cannot write rows into the
// log of a printer they cannot see by guessing at ids.
//
// The fleet is embedded, so a method added to DeviceFleet reaches the device
// plane through this decorator without a forwarding method.
type recordingFleet struct {
	DeviceFleet

	log *devtools.CommandLog
}

func (f recordingFleet) Command(
	ctx context.Context, principal string, id string, cmd PrinterCommand,
) (PrinterResult, error) {
	started := time.Now()

	result, err := f.DeviceFleet.Command(ctx, principal, id, cmd)

	if fault.HasCode(err, fault.PrinterNotFound.Code()) {
		return result, err
	}

	entry := devtools.Command{
		At:        started.UTC(),
		DeviceID:  id,
		Verb:      cmd.Verb,
		Args:      cmd.Args,
		ElapsedMS: time.Since(started).Milliseconds(),
	}
	entry.Outcome(result.OK, result.MachineReason, result.Ignored, result.Substituted, err)

	f.log.Record(entry)

	return result, err
}

// mountDevPage adds nothing. The developer page is an operation, and the
// operations are registered with the others.
func (s *Server) mountDevPage() {}

// leftOutRoutes is empty, because this build leaves no developer route out.
func leftOutRoutes() []string { return nil }
