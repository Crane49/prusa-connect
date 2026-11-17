package xbuddy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// ErrArgumentUnusable reports an argument combination the wire form cannot
// carry, such as a MOVE with neither axis.
//
// It is not a re-check of anything the descriptor declares. The
// descriptor says which arguments exist and what each one accepts on its own,
// and it has no way to say "at least one of these two", so the encoder is where
// that shows up. Every use of it is a gap in what Value can express, and each
// one is named where it is raised.
var ErrArgumentUnusable = errors.New("xbuddy: the printer's wire form cannot carry these arguments")

// The gcode payload formats, transcribed from buildGCodeCommand in
// legacy/connector/xbuddy/commands/builder.go. The line separator is CRLF
// because the firmware reads a multi-line payload that way.
const (
	moveFormat       = "G91\r\nG1 F%.0f%s\r\nG90"
	moveZFormat      = "G91\r\nG1 F%.0f Z%.3f\r\nG90"
	moveEFormat      = "M83\r\nG1 F%.0f E%.3f"
	zPowerMoveFormat = "M906 Z900\r\nM569 S0 Z\r\nM211 S0\r\nG0 Z%.3f F480\r\n" +
		"M400\r\nM906 Z600\r\nM569 S1 Z\r\nM211 S1"
)

// arguments is one command's argument values beside the descriptor that
// declared them, which is what says how a text value is read.
type arguments struct {
	verb     connectorapi.Capability
	declared map[string]connectorapi.Value
	values   map[string]string
}

// number reads a numeric argument. ok is false when the caller did not supply
// it; a value the host let through is readable by construction.
func (a arguments) number(name string) (value float64, ok bool) {
	raw, present := a.values[name]
	if !present {
		return 0, false
	}

	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}

	return parsed, true
}

// text reads a string argument.
func (a arguments) text(name string) (value string, ok bool) {
	raw, present := a.values[name]

	return raw, present
}

// kwargs renders the arguments as the JSON object a J frame carries.
//
// The kind comes from the descriptor rather than from the text, because "1" is
// a legal string and a legal integer and the firmware reads the two
// differently. An argument the descriptor does not declare cannot reach here:
// the host refuses it before Do.
func (a arguments) kwargs() (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(a.values))

	for name, raw := range a.values {
		encoded, err := encodeArgument(a.declared[name].Kind, raw)
		if err != nil {
			return nil, fmt.Errorf("%s argument %q: %w", a.verb, name, err)
		}

		out[name] = encoded
	}

	return out, nil
}

// encodeArgument renders one argument value as the JSON the firmware reads.
func encodeArgument(kind connectorapi.Kind, raw string) (json.RawMessage, error) {
	switch kind {
	case connectorapi.KindString:
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("encode as text: %w", err)
		}

		return encoded, nil
	case connectorapi.KindInteger, connectorapi.KindNumber:
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return nil, fmt.Errorf("%w: %q is not a number", ErrArgumentUnusable, raw)
		}

		return json.RawMessage(raw), nil
	case connectorapi.KindBoolean:
		if raw != boolTrue && raw != boolFalse {
			return nil, fmt.Errorf("%w: %q is not a boolean", ErrArgumentUnusable, raw)
		}

		return json.RawMessage(raw), nil
	case connectorapi.KindOpaque:
		if !json.Valid([]byte(raw)) {
			return nil, fmt.Errorf("%w: the opaque value is not JSON", ErrArgumentUnusable)
		}

		return json.RawMessage(raw), nil
	case connectorapi.KindFlag:
		return nil, fmt.Errorf("%w: a flag carries no value and one was supplied", ErrArgumentUnusable)
	default:
		return nil, fmt.Errorf("%w: kind %d has no wire form", ErrArgumentUnusable, kind)
	}
}

// fixedGCode is a verb whose payload never varies.
func fixedGCode(code string) gcodeTemplate {
	return func(arguments) (string, error) { return code, nil }
}

// gcodeSingle is a verb whose payload is one numeric argument in one format.
func gcodeSingle(format, name string) gcodeTemplate {
	return func(args arguments) (string, error) {
		value, ok := args.number(name)
		if !ok {
			return "", fmt.Errorf("%w: %q is missing", ErrArgumentUnusable, name)
		}

		return fmt.Sprintf(format, value), nil
	}
}

// gcodeNozzleTemp renders the nozzle setpoint. Without a tool it heats the
// active head. The firmware and the tool argument both count heads from 0, so
// tool 1 is T1.
func gcodeNozzleTemp(args arguments) (string, error) {
	target, ok := args.number("nozzle_temperature")
	if !ok {
		return "", fmt.Errorf("%w: %q is missing", ErrArgumentUnusable, "nozzle_temperature")
	}

	tool, ok := args.number("tool")
	if !ok {
		return fmt.Sprintf("M104 S%.0f", target), nil
	}

	return fmt.Sprintf("M104 T%.0f S%.0f", tool, target), nil
}

// gcodeHome renders HOME. A bare G28 homes every axis, which is what an absent
// argument means.
func gcodeHome(args arguments) (string, error) {
	axis, ok := args.text("axis")
	if !ok || axis == "" {
		return "G28", nil
	}

	return "G28 " + axis, nil
}

// gcodeLoadFilament renders LOAD_FILAMENT. The firmware reads S"<name>" on M701.
// When the name matches a filament type the printer knows, exactly and with
// the same capitals, the printer loads that type without asking. The built-in
// names are capitals, such as PLA and PETG. For any other name the printer
// ignores S and asks for the type on its screen, as it does for a bare M701.
// The printer reports neither case, so the result cannot say which happened.
//
// W2 asks the firmware to preheat the nozzle for the filament before it loads,
// with a return option on the dialog, as the printer's own menu does. Without
// W the firmware sets no target and waits on the loading screen with a cold
// nozzle.
//
// The name travels inside a gcode line, so a quote or a line break in it would
// end the name early or start a second command. The firmware allows letters,
// digits, dash and underscore in a filament name, and the driver refuses any
// other character. A descriptor cannot declare a set of characters, so the
// check is here.
func gcodeLoadFilament(args arguments) (string, error) {
	name, ok := args.text("filament")
	if !ok || name == "" {
		return "M701 W2", nil
	}

	for _, char := range name {
		if !filamentNameCharacter(char) {
			return "", connectorapi.ArgumentMalformed.New(
				"A filament name holds only letters A to Z and a to z, digits, dash and underscore, "+
					"and at most seven of them.").
				With(fault.StringParam("verb", args.verb.String()),
					fault.StringParam("argument", "filament"),
					fault.StringParam("allowed_characters", filamentNameAllowed),
					fault.IntParam("limit_characters", maxFilamentNameLength))
		}
	}

	return `M701 S"` + name + `" W2`, nil
}

// filamentNameAllowed is the set of characters a filament name may hold, in
// the form the refusal gives to the caller.
const filamentNameAllowed = "A-Z a-z 0-9 - _"

// filamentNameCharacter reports whether the firmware allows char in a filament
// name.
func filamentNameCharacter(char rune) bool {
	switch {
	case char >= 'A' && char <= 'Z', char >= 'a' && char <= 'z', char >= '0' && char <= '9':
		return true
	default:
		return char == '-' || char == '_'
	}
}

// gcodeMove renders MOVE.
//
// x and y are each optional and at least one is required, which no descriptor
// can say: Value declares one argument at a time and has no way to relate two.
func gcodeMove(args arguments) (string, error) {
	feedrate, ok := args.number("feedrate")
	if !ok {
		return "", fmt.Errorf("%w: %q is missing", ErrArgumentUnusable, "feedrate")
	}

	var distance strings.Builder

	if x, hasX := args.number("x"); hasX {
		fmt.Fprintf(&distance, " X%.3f", x)
	}

	if y, hasY := args.number("y"); hasY {
		fmt.Fprintf(&distance, " Y%.3f", y)
	}

	if distance.Len() == 0 {
		return "", fmt.Errorf("%w: MOVE needs x, y or both, and neither was supplied", ErrArgumentUnusable)
	}

	return fmt.Sprintf(moveFormat, feedrate, distance.String()), nil
}

// gcodeMoveE renders MOVE_E.
func gcodeMoveE(args arguments) (string, error) {
	feedrate, hasFeedrate := args.number("feedrate")
	length, hasLength := args.number("length")

	if !hasFeedrate || !hasLength {
		return "", fmt.Errorf("%w: MOVE_E needs feedrate and length", ErrArgumentUnusable)
	}

	return fmt.Sprintf(moveEFormat, feedrate, length), nil
}

// gcodeMoveZ renders MOVE_Z.
func gcodeMoveZ(args arguments) (string, error) {
	feedrate, hasFeedrate := args.number("feedrate")
	distance, hasDistance := args.number("distance")

	if !hasFeedrate || !hasDistance {
		return "", fmt.Errorf("%w: MOVE_Z needs feedrate and distance", ErrArgumentUnusable)
	}

	return fmt.Sprintf(moveZFormat, feedrate, distance), nil
}

// gcodeZPowerMove renders Z_POWER_MOVE, a Z move that raises the Z current
// for the duration and puts it back.
func gcodeZPowerMove(args arguments) (string, error) {
	distance, ok := args.number("distance")
	if !ok {
		return "", fmt.Errorf("%w: %q is missing", ErrArgumentUnusable, "distance")
	}

	return fmt.Sprintf(zPowerMoveFormat, distance), nil
}

// The two texts a boolean argument may carry. They are the JSON spellings and
// nothing else: the value is written into the payload as raw JSON, so anything
// strconv.ParseBool would also accept ("1", "T", "TRUE") would leave the
// payload unparseable.
//
// They sit at the end of the file rather than beside the formats above because
// docs/src/content/docs/internals/printers-and-drivers.md cites this file by
// line, and a constant inserted at the top moves every citation below it.
const (
	boolTrue  = "true"
	boolFalse = "false"
)
