package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// owner implements fractalnode.Owner for one interface instance of one device.
type owner struct {
	bridge *Bridge
	device string
	iface  *InterfaceMap
	// tool is the head or the slot of the instance, and empty for an
	// interface with one instance per device.
	tool connectorapi.ToolID
}

var _ fractalnode.Owner = (*owner)(nil)

// Snapshot returns every property of this interface as the device last
// reported it, and no time.
func (o *owner) Snapshot(context.Context) (fractal.Props, fractal.Timestamp, error) {
	return o.bridge.snapshot(o.device, o.iface, o.tool)
}

// Desire sends one command per property, in the order of the property names.
// It stops at the first failure and does not undo the commands before it. The
// node has already checked props against the interface definition. It returns
// no values: the device reports the new value in the next state report.
func (o *owner) Desire(ctx context.Context, props fractal.Props) (fractal.Props, error) {
	for _, name := range slices.Sorted(maps.Keys(props)) {
		value := props[name]

		mapped, ok := o.iface.Property(name)
		if !ok || mapped.Write == nil {
			return nil, fmt.Errorf("%w: %s.%s", schema.ErrUnknownProperty, o.iface.Name, name)
		}

		text, rendered := argumentFor(mapped, value)
		if !rendered {
			return nil, fmt.Errorf("%w: %s.%s", schema.ErrWrongType, o.iface.Name, name)
		}

		if mapped.Write.Round {
			text = rounded(text)
		}

		args := map[string]string{mapped.Write.Arg: text}
		if err := o.nameHead(mapped.Write, args); err != nil {
			return nil, err
		}

		result, err := o.bridge.command(ctx, o.device, mapped.Write.Verb, args)
		if err != nil {
			return nil, err
		}

		if !result.OK {
			return nil, refused(result)
		}
	}

	//nolint:nilnil // no property changed here: the device reports its own value.
	return nil, nil
}

// declares reports whether the device lists one argument of a verb among its
// capabilities. The host refuses an argument the device does not declare.
func (o *owner) declares(verb, arg string) bool {
	device, _ := o.bridge.device(o.device)

	return slices.Contains(device.Capabilities, string(connectorapi.ArgumentKey(connectorapi.Capability(verb), arg)))
}

// nameHead adds the head of this instance to args where the printer declares
// the tool argument. It refuses a head the command cannot reach.
func (o *owner) nameHead(write *WriteMap, args map[string]string) error {
	if write.ToolArg == "" {
		return nil
	}

	switch {
	case !o.declares(write.Verb, write.ToolArg):
		if !o.reachedWithoutTool() {
			return answerWith(CodeHeadNotActive)
		}
	case !o.isHead():
		return answerWith(CodeHeadNotActive)
	default:
		args[write.ToolArg] = string(o.tool)
	}

	return nil
}

// isHead reports whether the head of this instance is among the heads the
// printer has now: head 0 and every other enabled print head.
func (o *owner) isHead() bool {
	device, _ := o.bridge.device(o.device)

	return slices.ContainsFunc(heads(connectorapi.ReadTools(device.State)), func(head numbered) bool {
		return head.tool == o.tool
	})
}

// reachedWithoutTool reports whether a command that names no head acts on the
// head of this instance: the only head, or the head the printer has picked.
func (o *owner) reachedWithoutTool() bool {
	device, _ := o.bridge.device(o.device)

	return len(heads(connectorapi.ReadTools(device.State))) < 2 || isActiveHead(device.State, o.tool)
}

// argumentFor converts one requested value into the text of the argument.
// Null becomes the NullAs text of the row, so a null turns a heater off.
func argumentFor(mapped *PropertyMap, value json.RawMessage) (text string, ok bool) {
	if schema.IsNull(value) {
		if mapped.Write.NullAs == "" {
			return "", false
		}

		return mapped.Write.NullAs, true
	}

	return argument(value)
}

// Invoke sends one method call to the device as one command, after the state
// rules of the method allow it. The node has already checked the number and
// the types of the parameters.
func (o *owner) Invoke(ctx context.Context, call fractalnode.Call) (json.RawMessage, error) {
	mapped, ok := o.iface.Method(call.Method)
	if !ok {
		return nil, answerWith(CodeMethodUnknown)
	}

	// Fractal resolves the target before the parameters. Without this check
	// a state rule would refuse a printer that is gone as BLOCKED.
	if !o.bridge.listed(o.device) {
		return nil, answerWith(CodeDeviceGone)
	}

	if o.bridge.files != nil {
		if u, named, err := fractalURL(mapped, call.Params); named {
			if err != nil {
				return nil, err
			}

			if err = o.allowed(mapped); err != nil {
				return nil, err
			}

			return nil, o.printFractal(ctx, u, call.Caller)
		}
	}

	// Fractal checks the parameters before a deliberate refusal, so the
	// state rules come after the arguments.
	args, err := arguments(mapped, call.Params)

	nothingToDo := errors.Is(err, errNothingToDo)
	if err != nil && !nothingToDo {
		return nil, err
	}

	if err = o.allowed(mapped); err != nil {
		return nil, err
	}

	if nothingToDo {
		return nil, nil
	}

	result, err := o.bridge.command(ctx, o.device, mapped.Verb, args)
	if err != nil {
		return nil, err
	}

	if !result.OK {
		return nil, refused(result)
	}

	// No method of the Prusa vocabulary returns a value.
	return nil, nil
}

// errNothingToDo means a call asks for nothing, such as a Home with every axis
// false. No command is sent, because a home with no axis homes every axis.
var errNothingToDo = errors.New("the call asks for nothing to be done")

// arguments builds the arguments of one command from the parameters of one
// call. An optional argument with no value is left out.
func arguments(mapped *MethodMap, params []json.RawMessage) (map[string]string, error) {
	if len(mapped.Args) == 0 {
		//nolint:nilnil // a verb with no arguments carries none.
		return nil, nil
	}

	args := make(map[string]string, len(mapped.Args))

	for _, arg := range mapped.Args {
		text, ok := argumentOf(arg, params)
		if !ok {
			switch {
			case arg.Default != "":
				text = arg.Default
			case arg.Optional:
				continue
			default:
				return nil, answerWith(CodeParamMissing)
			}
		}

		if len(arg.Letters) > 0 && text == "" {
			return nil, errNothingToDo
		}

		sent, err := sentValue(arg, text)
		if err != nil {
			return nil, err
		}

		args[arg.Arg] = sent
	}

	return args, nil
}

// sentValue applies the row to one value: round, refuse, rename to the
// printer's own name, or turn a file URL into a path.
func sentValue(arg ArgMap, text string) (string, error) {
	if arg.Round {
		text = rounded(text)
	}

	if slices.Contains(arg.Refuse, text) {
		return "", answerWith(CodeValueRefused)
	}

	if printerName, named := arg.printerNames[text]; named {
		text = printerName
	}

	if arg.URL {
		return printerPath(text)
	}

	return text, nil
}

// rounded returns a number in text rounded to a whole number, and any other
// text unchanged, which the host then checks against the command.
func rounded(text string) string {
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return text
	}

	return strconv.FormatFloat(math.Round(number), 'f', -1, 64)
}

// fileScheme names a file the printer already holds, such as
// file:///usb/part.bgcode (RFC 8089).
const fileScheme = "file"

// printerPath returns the path on the printer that a file URL names, decoded
// and without its leading slash. The only host allowed is localhost.
func printerPath(text string) (string, error) {
	if !strings.EqualFold(schemeOf(text), fileScheme) {
		return "", answerWith(CodeURLNotFetched)
	}

	u, err := url.Parse(text)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return "", answerWith(CodeURLMalformed)
	}

	path := strings.TrimPrefix(u.Path, "/")
	if path == "" || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") ||
		slices.Contains(strings.Split(path, "/"), "..") {
		return "", answerWith(CodeURLMalformed)
	}

	return path, nil
}

// schemeOf returns the scheme s starts with (RFC 3986, section 3.1), and ""
// when s has none. A colon after a slash is part of a path.
func schemeOf(s string) string {
	colon := strings.IndexByte(s, ':')
	if colon < 1 {
		return ""
	}

	for i, c := range s[:colon] {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z':
		case i > 0 && ('0' <= c && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return ""
		}
	}

	return s[:colon]
}

// argumentOf returns the text of one argument, built from the parameters the
// caller sent. It returns false when the parameter is missing or null.
func argumentOf(arg ArgMap, params []json.RawMessage) (text string, ok bool) {
	if len(arg.Letters) > 0 {
		return letters(arg, params)
	}

	if arg.Param == NoParam || arg.Param >= len(params) {
		return "", false
	}

	return argument(params[arg.Param])
}

// letters builds one text argument from boolean parameters, such as "XYZ" for
// a home. It returns false when a parameter is missing or not a boolean.
func letters(arg ArgMap, params []json.RawMessage) (text string, ok bool) {
	built := make([]byte, 0, len(arg.Letters))

	for _, letter := range arg.Letters {
		if letter.Param >= len(params) {
			return "", false
		}

		set, err := schema.ReadBool(params[letter.Param])
		if err != nil {
			return "", false
		}

		if set {
			built = append(built, letter.Letter...)
		}
	}

	return string(built), true
}

// command sends one command to one device through the host. When the host
// refuses, it logs the host's error and returns a fractal.Error.
func (b *Bridge) command(
	ctx context.Context, id, verb string, args map[string]string,
) (deviceapi.Result, error) {
	if _, held := b.device(id); !held {
		return deviceapi.Result{}, answerWith(CodeDeviceGone)
	}

	result, err := b.fleet.Command(ctx, deviceapi.CommandParams{
		Principal: b.principal(),
		ID:        id,
		Verb:      verb,
		Args:      args,
	})
	if err != nil {
		b.log.WarnContext(ctx, "a command was refused",
			"device", id, "verb", verb, "error", err.Error())

		return deviceapi.Result{}, failure(err)
	}

	return result, nil
}

// failure converts an error of the host into a fractal.Error. The host's text
// is not sent, because it can name things inside this server.
func failure(err error) error {
	switch {
	case errors.Is(err, deviceapi.ErrDenied):
		return answerWith(CodeCallNotAllowed)
	case errors.Is(err, deviceapi.ErrNoDevice):
		// The bridge calls only for a printer it registered, so the host's
		// "no such device" means the printer is gone.
		return answerWith(CodeDeviceGone)
	case errors.Is(err, deviceapi.ErrVerbUnknown):
		return answerWith(CodeCommandUnknown)
	case errors.Is(err, deviceapi.ErrArgument), errors.Is(err, deviceapi.ErrInvalidQuery):
		return answerWith(CodeValueRefused)
	case errors.Is(err, deviceapi.ErrNotReady), errors.Is(err, deviceapi.ErrBusy):
		return answerWith(CodePrinterUnreachable)
	}

	return answerWith(CodeBridgeFailed)
}

// refused builds the answer for a command the printer refused, with the
// printer's own reason appended.
func refused(result deviceapi.Result) error {
	answer := answerWith(CodePrinterRefused)
	if result.MachineReason != "" {
		answer.Message += ": " + result.MachineReason
	}

	return answer
}
