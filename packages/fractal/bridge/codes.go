package bridge

import "github.com/prusa3d/prusa-connect/sdk/fractal"

// The error codes this bridge answers a method call with.
//
// Each code refines a Fractal base code in its low four bits, from 1 to 15.
// The English text is also sent, for a server that does not know the number.
const (
	// CodeParamMissing means the call left out a parameter the method needs.
	CodeParamMissing fractal.Code = 0x4012
	// CodeValueRefused means the printer would not take one of the values the
	// call sent.
	CodeValueRefused fractal.Code = 0x4013
	// CodeCommandUnknown means the printer does not run the command this method
	// maps to.
	CodeCommandUnknown fractal.Code = 0x4014
	// CodeURLNotFetched means the call named a file by a value with no
	// scheme, or with a scheme this server does not take. A file the printer
	// already holds is named by a file URL.
	CodeURLNotFetched fractal.Code = 0x4015
	// CodeURLMalformed means the call named a fractal or file URL that is not
	// well formed.
	CodeURLMalformed fractal.Code = 0x4016

	// CodeMethodUnknown means this printer has no method by that name.
	CodeMethodUnknown fractal.Code = 0x4041
	// CodeDeviceGone means this server held the printer and no longer holds it.
	CodeDeviceGone fractal.Code = 0x4042
	// CodeDeviceUnknown means this server has never held the printer the call
	// names.
	CodeDeviceUnknown fractal.Code = 0x4043
	// CodeObjectUnreachable means this server neither publishes the file, nor
	// holds a copy, nor has a server above to ask.
	CodeObjectUnreachable fractal.Code = 0x4044

	// CodeCallNotAllowed means this server checked the permissions of the
	// server above and refused the call.
	CodeCallNotAllowed fractal.Code = 0x2011
	// CodePrinterRefused means the printer itself answered no.
	CodePrinterRefused fractal.Code = 0x2012
	// CodeStateRefused means the state of the printer does not allow the
	// method, such as a print on a printer that is not READY.
	CodeStateRefused fractal.Code = 0x2013
	// CodeNoTargetTemperature means the head has no nozzle target and no
	// filament with a known temperature, so Extrude has nothing to heat to.
	CodeNoTargetTemperature fractal.Code = 0x2014
	// CodeHeadNotActive means the command of the method acts on the active
	// head, and the call names another one.
	CodeHeadNotActive fractal.Code = 0x2015

	// CodePrinterUnreachable means this server could not reach the printer at
	// this moment. The server above may send the call again.
	CodePrinterUnreachable fractal.Code = 0x0011
	// CodeBridgeFailed means this server failed for a reason it writes only to
	// its own log.
	CodeBridgeFailed fractal.Code = 0x0012
)

// codeText is the table of this bridge's codes: the code, the base code it
// refines, and the English sentence of the answer.
var codeText = [...]struct {
	code fractal.Code
	base fractal.Code
	text string
}{
	{CodeParamMissing, fractal.CodeInvalidParams, "the call left out a parameter the method needs"},
	{CodeValueRefused, fractal.CodeInvalidParams, "the printer would not take one of the values"},
	{CodeCommandUnknown, fractal.CodeInvalidParams, "the printer does not run that command"},
	{
		CodeURLNotFetched, fractal.CodeInvalidParams,
		"this server does not take that URL; name a file on the printer as file:///usb/part.bgcode",
	},
	{CodeURLMalformed, fractal.CodeInvalidParams, "the URL is not well formed"},
	{CodeMethodUnknown, fractal.CodeObjectNotFound, "this printer has no method by that name"},
	{CodeDeviceGone, fractal.CodeObjectNotFound, "this server no longer holds that printer"},
	{CodeDeviceUnknown, fractal.CodeObjectNotFound, "this server holds no such printer"},
	{CodeObjectUnreachable, fractal.CodeObjectNotFound, "this server has no way to the file the URL names"},
	{CodeCallNotAllowed, fractal.CodeDenied, "this server did not allow that call"},
	{CodePrinterRefused, fractal.CodeDenied, "the printer refused the command"},
	{CodeStateRefused, fractal.CodeDenied, "the state of the printer does not allow that call"},
	{
		CodeNoTargetTemperature, fractal.CodeDenied,
		"the head has no nozzle target and no known filament, so there is no temperature to heat to",
	},
	{
		CodeHeadNotActive, fractal.CodeDenied,
		"this server can send that call only to the head the printer has picked",
	},
	{CodePrinterUnreachable, fractal.CodeInternal, "this server could not reach the printer just now"},
	{CodeBridgeFailed, fractal.CodeInternal, "this server could not carry out the call"},
}

// Codes returns every code this bridge answers with, in the order of the table.
// The caller owns the returned slice.
func Codes() []fractal.Code {
	out := make([]fractal.Code, 0, len(codeText))
	for _, row := range codeText {
		out = append(out, row.code)
	}

	return out
}

// TextOf returns the English sentence this bridge sends with the code, and
// false for a code this bridge does not send.
func TextOf(code fractal.Code) (text string, ok bool) {
	for _, row := range codeText {
		if row.code == code {
			return row.text, true
		}
	}

	return "", false
}

// BaseOf returns the Fractal base code that the code refines, and false for a
// code this bridge does not send.
func BaseOf(code fractal.Code) (base fractal.Code, ok bool) {
	for _, row := range codeText {
		if row.code == code {
			return row.base, true
		}
	}

	return 0, false
}

func answerWith(code fractal.Code) *fractal.Error {
	text, _ := TextOf(code)

	return &fractal.Error{Code: code, Message: text}
}
