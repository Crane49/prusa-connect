package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// printerGCodePath is where a caller sends raw gcode. It is its own path and
// not a shape of the command body, so the operation carries its own
// description of what it does not promise: the sequence is ordered, and it is
// neither atomic nor exclusive.
const printerGCodePath = "/api/v1/devices/{device_id}/gcode"

// MaxGCodeSteps bounds a sequence. It is checked here as well as declared in
// the schema: a generated client reads the schema, and the server is held to
// this check. It is exported because a stored preset is a sequence and is held
// to the same number.
const MaxGCodeSteps = 64

// gcodeArgument is the name the raw gcode verbs carry their payload under.
const gcodeArgument = "gcode"

// The step outcomes. A step that was not attempted says so. It is never left
// out of the answer and never reported as a success.
const (
	stepAccepted     = "accepted"
	stepRefused      = "refused"
	stepNotAttempted = "not_attempted"
)

// PrinterGCodeRequest is one ordered run of raw gcode.
//
// It is always a list. A single command is a list of one, so the failure rule
// applies to every request in the same way.
type PrinterGCodeRequest struct {
	// Lines are the commands, in order. Each one is a single gcode command in
	// printable ASCII, and several commands are several lines and not one
	// line with a break in it.
	Lines []string `json:"lines" doc:"The commands, in order, one gcode command each." maxItems:"64" minItems:"1"`
	// Force sends each command out of band, so the printer runs it without
	// waiting for the print queue. It is what an emergency stop needs and what
	// an ordinary setting does not.
	Force bool `json:"force,omitempty" doc:"Run each command out of band, ahead of the print queue."`
}

// PrinterGCodeStep is what became of one line.
//
// A step carries the two refusals apart, the way the command endpoint carries
// them apart across a status code. A line this server declined to send has Code
// and Detail, which are this API's own vocabulary. A line the printer refused
// has MachineReason, which is the machine's, and the Code the printer's answer
// carried.
type PrinterGCodeStep struct {
	GCode  string `json:"gcode"            doc:"The line, as it was sent."`
	Status string `json:"status"           doc:"accepted, refused, or not_attempted for a line after the one that stopped the run."                                       enum:"accepted,refused,not_attempted"`
	Code   string `json:"code,omitempty"   doc:"The error code for a line this server refused, as the error reference names it. A line the printer refused carries none."`
	Detail string `json:"detail,omitempty" doc:"What went wrong with a refused line, in this API's words."`

	// MachineReason is the printer's own word for refusing this line. It is the
	// machine's vocabulary and not this API's: nothing here defines the set of
	// words, a firmware may spell one this build has never seen, and two vendors
	// may spell one condition differently. So it is shown to a person, and a
	// client branches on Code.
	MachineReason string `json:"machine_reason,omitempty" doc:"Why the printer refused this line, in the printer's own words, passed through as it spelled them. It is the machine's vocabulary rather than this API's: no list of values is published, a firmware may send one this server has never seen, and it is meant to be shown to a person rather than branched on. Absent on a line the printer accepted, on a line this server refused, and for a printer whose dialect has no such word. To branch on a failure, read code instead."`

	// Answer is what the printer printed back, for the machines that print
	// anything. It is the other half of MachineReason: that field carries a
	// refusal and this one carries a reading.
	Answer string `json:"answer,omitempty" doc:"What the printer said back to this line, in its own words. Shipping Buddy firmware answers nothing, so it is empty on a real machine; the simulator answers the way a serial port would, so M105 comes back as the temperatures and M114 as the position. It is the machine's vocabulary rather than this API's, so show it to a person and branch on code instead."`
}

// PrinterGCodeOutcome is what became of the whole run.
//
// Stopped and the per-step statuses say the same thing twice on purpose: a
// client that reads only the summary still learns that the run did not finish,
// and one that reads the steps learns where.
type PrinterGCodeOutcome struct {
	Completed int                `json:"completed" doc:"How many lines the printer accepted, counting from the first."`
	Stopped   bool               `json:"stopped"   doc:"Whether the run stopped before the end. Nothing is undone: the lines that ran, ran."`
	Steps     []PrinterGCodeStep `json:"steps"     doc:"One entry per line submitted, in the order they were sent."`
}

type sendGCodeInput struct {
	DeviceRef

	Body PrinterGCodeRequest
}

type sendGCodeOutput struct {
	Body PrinterGCodeOutcome
}

// gcodeErrors are what this operation answers with. 403 is the caller holding
// operate and not send_gcode, which is a refusal worth spelling out
// because they can see the printer and command it in every other way.
var gcodeErrors = append([]int{http.StatusForbidden, http.StatusBadRequest}, deviceErrors...)

// registerGCode mounts the raw gcode operation and returns the fleet the
// printer operations are then registered over.
//
// It returns a fleet for the reason registerDevtools does: the extra
// authorization raw gcode takes has to hold on every path that can reach the
// verb, and the generic command endpoint can reach it by naming the verb in its
// body. One decorator keeps the rule in one place.
func registerGCode(api huma.API, cfg Config) DeviceFleet {
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	fleet := gcodeFleet{DeviceFleet: cfg.Devices, guard: cfg.Grants, log: log}

	registerSendGCode(api, fleet)
	registerRunGCodePreset(api, fleet, cfg.Presets)

	return fleet
}

func registerSendGCode(api huma.API, fleet gcodeFleet) {
	register(api, huma.Operation{
		OperationID: "send-printer-gcode",
		Method:      http.MethodPost,
		Path:        printerGCodePath,
		Summary:     "Run raw gcode on a printer",
		Description: "Sends one or more gcode commands, in order, and reports what became of each. " +
			"It takes send_gcode on the printer, which an operator does not hold: raw gcode " +
			"reconfigures a machine, and the capability model that checks every other command's " +
			"arguments has nothing to say about a payload it cannot read.\n\n" +
			"The run is ordered. It is not atomic and it is not exclusive: it stops at the first line " +
			"the server or the printer did not accept, nothing undoes the lines that already ran, and " +
			"another caller's command may land between two lines.\n\n" +
			"The status is 200 whenever the run was started. Whether each line was carried out is in " +
			"the body, one entry per line, and a line after the one that stopped the run is reported " +
			"as not_attempted.\n\n" +
			"Some words are never sent whatever the caller holds: every D code, and M997, M28, M29 " +
			"and M30. Files are written and deleted through the file capabilities, which check the " +
			"path, and firmware is not updated through this API.",
		Tags:   []string{"devices"},
		Errors: gcodeErrors,
	}, func(ctx context.Context, in *sendGCodeInput) (*sendGCodeOutput, error) {
		outcome, err := fleet.run(ctx, principalFrom(ctx), in.DeviceID, in.Body)
		if err != nil {
			return nil, err
		}

		return &sendGCodeOutput{Body: outcome}, nil
	})
}

// gcodeFleet holds every path that can reach a raw gcode verb to the action
// that verb takes.
//
// The fleet is embedded and not held in a named field, so a method added to
// DeviceFleet reaches the device plane through this decorator with no change
// here.
type gcodeFleet struct {
	DeviceFleet

	// guard is the authorization graph. It is never nil: Config.Grants is
	// required, so a server that cannot answer the question is refused when it
	// is built, before anybody sends gcode.
	guard GrantStore
	log   *slog.Logger
}

// Command refuses a raw gcode verb to a caller who does not reach it, and
// forwards everything else unchanged.
//
// The device plane checks the verb's class for every verb. That is not enough
// for the raw gcode verbs: they are marked opaque_payload, so no class, role
// or level confers them, and reaching one takes a row naming the verb or
// ownership of the machine. The device plane does not make that check, so it
// is made here.
func (f gcodeFleet) Command(
	ctx context.Context, principal string, id string, cmd PrinterCommand,
) (PrinterResult, error) {
	if !slices.Contains(authz.RawGCodeVerbs(), cmd.Verb) {
		return f.DeviceFleet.Command(ctx, principal, id, cmd)
	}

	if err := f.authorize(ctx, principal, id, cmd.Verb); err != nil {
		return PrinterResult{}, err
	}

	result, err := f.DeviceFleet.Command(ctx, principal, id, cmd)

	f.record(ctx, principal, id, cmd.Verb, cmd.Args[gcodeArgument], result, err)

	return result, err
}

// run carries out one ordered sequence.
//
// The action is checked once here as well as once per step. Once here, so that
// a caller who holds nothing is refused before anything reaches a printer;
// once per step, because the generic command path reaches the same verbs and a
// check that lived only in this handler would not cover it.
func (f gcodeFleet) run(
	ctx context.Context, principal string, id string, request PrinterGCodeRequest,
) (PrinterGCodeOutcome, error) {
	// The schema says the same thing and a generated client reads that; this is
	// what the server is held to, so a body that reached the handler another
	// way is still bounded.
	if len(request.Lines) == 0 || len(request.Lines) > MaxGCodeSteps {
		return PrinterGCodeOutcome{}, httpfault.RequestInvalid("body.lines",
			"a run carries between one and 64 lines",
			"lines: "+strconv.Itoa(len(request.Lines)))
	}

	verb := authz.RawGCodeVerbs()[0]
	if request.Force {
		verb = authz.RawGCodeVerbs()[1]
	}

	if err := f.authorize(ctx, principal, id, verb); err != nil {
		return PrinterGCodeOutcome{}, err
	}

	outcome := PrinterGCodeOutcome{Steps: make([]PrinterGCodeStep, 0, len(request.Lines))}

	for index, line := range request.Lines {
		if outcome.Stopped {
			outcome.Steps = append(outcome.Steps,
				PrinterGCodeStep{GCode: line, Status: stepNotAttempted})

			continue
		}

		step, err := f.step(ctx, principal, id, verb, line)

		// A printer nothing can reach is a request that never started, not a
		// line that was refused, so it answers the way every other operation on
		// this surface answers. Only on the first line: after that there is a
		// record of what did run, and an error status would discard it.
		if index == 0 && fault.HasCode(err, fault.PrinterNotFound.Code()) {
			return PrinterGCodeOutcome{}, err
		}

		outcome.Steps = append(outcome.Steps, step)

		if step.Status == stepAccepted {
			outcome.Completed = index + 1

			continue
		}

		outcome.Stopped = true
	}

	return outcome, nil
}

// step runs one line and renders what became of it.
//
// A refusal is reported as a step and not as the request's own failure, because
// the lines before it did run and an error status would discard the only
// record of that. The code is carried so a client branches on the code and
// not on the sentence, and the sentence is dropped for a code registered as
// redacted.
//
// A line the printer refused carries the machine's own reason beside the code
// of its answer. "The printer did not accept it" is true of every such line
// and gives an operator nothing to act on, and the machine has usually said
// which reason it was.
func (f gcodeFleet) step(
	ctx context.Context, principal string, id string, verb string, line string,
) (PrinterGCodeStep, error) {
	result, err := f.DeviceFleet.Command(ctx, principal, id,
		PrinterCommand{Verb: verb, Args: map[string]string{gcodeArgument: line}})

	f.record(ctx, principal, id, verb, line, result, err)

	switch {
	case err != nil:
		code, detail := describe(err)

		return PrinterGCodeStep{GCode: line, Status: stepRefused, Code: code, Detail: detail}, err
	case !result.OK:
		// The code comes from the answer and not from an error, because a
		// printer's refusal is a 200 whose body says no. A refusal the firmware
		// gave no word for carries only the code, so a step that read only the
		// machine reason would render it as "not accepted".
		return PrinterGCodeStep{
			GCode:         line,
			Status:        stepRefused,
			Code:          result.FaultCode,
			Detail:        "the printer did not accept it",
			MachineReason: result.MachineReason,
		}, nil
	default:
		return PrinterGCodeStep{GCode: line, Status: stepAccepted, Answer: result.Answer}, nil
	}
}

// authorize refuses a caller who does not hold send_gcode on the printer.
//
// It is 403 and not the 404 the rest of this surface answers with. The printer
// is already in the caller's listing, because holding observe is what
// put it there, so hiding it here would withhold nothing and would tell a
// person with a real grant that their printer had gone away.
func (f gcodeFleet) authorize(ctx context.Context, principal string, id string, verb string) error {
	who, err := authz.ParseSubject(principal)
	if err != nil {
		return refuseGCode(id, verb)
	}

	object, err := authz.NewObject(authz.TypeDevice, id)
	if err != nil {
		return gcodePrinterAbsent(id)
	}

	allowed, err := f.guard.CanVerb(ctx, who, object, verb)
	if err != nil {
		return err
	}

	if !allowed {
		return refuseGCode(id, verb)
	}

	return nil
}

// record is the audit line. Raw gcode is the one command an operator wants a
// record of afterwards, and the payload is all of what happened. The outcome
// includes why the printer said no, which is what an operator reads the log
// for.
func (f gcodeFleet) record(
	ctx context.Context, principal string, id string, verb string, line string,
	result PrinterResult, err error,
) {
	attrs := []any{
		slog.String("principal", principal),
		slog.String("printer", id),
		slog.String("verb", verb),
		slog.String("gcode", line),
		slog.Bool("accepted", result.OK && err == nil),
	}

	// Clipped, because the reason is text a driver chose and the xBuddy dialect
	// admits a frame of one mebibyte, so an unclipped line lets one printer
	// decide what a run of 64 lines costs the log.
	if err == nil && !result.OK && result.MachineReason != "" {
		attrs = append(attrs, slog.String("machine_reason",
			machinetext.Clip(result.MachineReason, machinetext.MaxBytes)))
	}

	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}

	f.log.InfoContext(ctx, "raw gcode was sent to a printer", attrs...)
}

// describe renders an error as the pair a step carries: the registered code,
// and the sentence unless the code is registered as redacted. An error with no
// registered code contributes neither, because nothing about an undocumented
// failure has been reviewed for a client to see.
//
// A refusal that names a reason carries it, because "this server never sends
// that gcode word" tells an operator nothing they can act on and "firmware is
// not flashed through this API" tells them what to do instead. The step body is
// not problem+json, so a param that is not read here is a param the caller of
// this operation never sees.
func describe(err error) (code string, detail string) {
	var carried *fault.Fault
	if !errors.As(err, &carried) || carried == nil {
		return "", "the server refused the command"
	}

	if carried.Redacted() {
		return carried.Code().String(), carried.Title()
	}

	detail = carried.Message()

	if reason, named := carried.Params().Get("reason"); named && reason.String() != "" {
		detail += ": " + reason.String()
	}

	return carried.Code().String(), detail
}

func refuseGCode(id string, verb string) error {
	return fault.New(authz.CodeAccessDenied(),
		"this caller may command the printer and may not send it raw gcode. A payload nothing "+
			"inspected is conferred by no role: it takes a grant naming the verb, or ownership of "+
			"the machine").With(
		fault.StringParam("object", "device:"+id),
		fault.StringParam("verb", verb))
}

func gcodePrinterAbsent(id string) error {
	return fault.PrinterNotFound.New("no connected printer answers to that id").
		With(fault.StringParam("device_id", id))
}
