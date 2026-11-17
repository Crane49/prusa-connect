package xbuddy

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// A refusal this firmware sent no word for.
//
// Buddy 6.5.7 and 6.8.1 answer a command aimed at a busy queue with a bare
// REJECTED: no reason, no machine_reason, nothing but the event name and the
// command id. A dialect that passes that emptiness through hands a caller
// "not accepted" and nothing else, and the stored row carries neither a
// machine reason nor a fault code. That is BUG-005, and the cases below are the
// floor under it.
//
// The shape each one holds is the driver contract on connectorapi.Result: a
// refusal is a result carrying its code, and a failure is an error.

// TestABareRejectionIsGivenTheRefusalCode is the unit case at the decoder. The
// firmware sent nothing, so the dialect says the one thing it does know in a
// code: the device refused, and it gave no reason. It says it in the code and
// not in the machine reason, because that field is the printer's own words and
// there were none.
func TestABareRejectionIsGivenTheRefusalCode(t *testing.T) {
	t.Parallel()

	answer := resultOf(printerEvent{Event: eventRejected}, nil, nil, "core.command.beep")

	if answer.result.OK {
		t.Fatal("a bare REJECTED reported the command was accepted")
	}

	if answer.result.MachineReason != "" {
		t.Errorf("a bare REJECTED carries machine reason %q, which no firmware sent",
			answer.result.MachineReason)
	}

	if got, want := answer.result.FaultCode,
		connectorapi.CommandRefusedWithoutReason.Code().String(); got != want {
		t.Errorf("a bare REJECTED carries the code %q, want %q", got, want)
	}

	if answer.err != nil {
		t.Errorf("a refusal is handed back as an error, so the record will store it as failed: %v",
			answer.err)
	}
}

// TestABareFailureIsNotARefusal is the event the first fix folded into the one
// above. REJECTED is a command the printer would not take and FAILED is one it
// took and could not finish, and a record that stored the second as refused
// said the machine had declined something it had in fact attempted. It leaves
// as an error, which is the shape a refusal deliberately does not take: a
// failure is not an answer the caller reads as a no, it is a command that did
// not happen.
func TestABareFailureIsNotARefusal(t *testing.T) {
	t.Parallel()

	answer := resultOf(printerEvent{Event: eventFailed}, nil, nil, "core.command.beep")

	if answer.result.OK {
		t.Fatal("a bare FAILED reported the command was accepted")
	}

	if !fault.HasCode(answer.err, connectorapi.CommandFailedWithoutReason.Code()) {
		t.Errorf("a bare FAILED is carried as %v, not as the registered failure code", answer.err)
	}

	if answer.result.FaultCode != "" {
		t.Errorf("a bare FAILED carries the refusal code %q on its result, so the record will "+
			"store it as refused", answer.result.FaultCode)
	}

	if answer.result.MachineReason != "" {
		t.Errorf("a bare FAILED carries machine reason %q, which no firmware sent",
			answer.result.MachineReason)
	}
}

// TestAFailureThePrinterExplainedKeepsItsWords is the same split one step over.
// The word the firmware sent is passed through, and the code beside it still
// says failure rather than refusal.
func TestAFailureThePrinterExplainedKeepsItsWords(t *testing.T) {
	t.Parallel()

	answer := resultOf(
		printerEvent{Event: eventFailed, MachineReason: "OUT_OF_FILAMENT"}, nil, nil, "core.command.beep")

	if answer.result.MachineReason != "OUT_OF_FILAMENT" {
		t.Errorf("machine reason = %q, want OUT_OF_FILAMENT", answer.result.MachineReason)
	}

	if !fault.HasCode(answer.err, connectorapi.CommandFailed.Code()) {
		t.Errorf("an explained failure is carried as %v, not as the registered failure code", answer.err)
	}

	if answer.result.FaultCode != "" {
		t.Errorf("an explained failure carries the refusal code %q on its result",
			answer.result.FaultCode)
	}
}

// TestARefusalThePrinterExplainedKeepsItsWordsAndGainsACode is the other half.
// The firmware's own word is passed through untouched, and the code beside it
// is the one every explained refusal carries, so a caller branches on a code
// for both refusals rather than on the absence of one.
func TestARefusalThePrinterExplainedKeepsItsWordsAndGainsACode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		event printerEvent
		want  string
	}{
		{
			name:  "a machine reason",
			event: printerEvent{Event: eventRejected, MachineReason: "FILE_EXISTS"},
			want:  "FILE_EXISTS",
		},
		{
			name:  "a sentence for a person",
			event: printerEvent{Event: eventRejected, Reason: "Printer is busy"},
			want:  "Printer is busy",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			answer := resultOf(tc.event, nil, nil, "core.command.beep")

			if answer.result.MachineReason != tc.want {
				t.Errorf("machine reason = %q, want %q", answer.result.MachineReason, tc.want)
			}

			if got, want := answer.result.FaultCode,
				connectorapi.CommandRefused.Code().String(); got != want {
				t.Errorf("an explained refusal carries the code %q, want %q", got, want)
			}

			if answer.err != nil {
				t.Errorf("an explained refusal is handed back as an error: %v", answer.err)
			}
		})
	}
}

// TestAnAcceptedCommandCarriesNothing keeps the synthesis inside the refusal
// events. Every other event that carries the id completes the command, and one
// of those given a refusal code would report a printer refusing what it did.
func TestAnAcceptedCommandCarriesNothing(t *testing.T) {
	t.Parallel()

	answer := resultOf(printerEvent{Event: "FINISHED"}, nil, nil, "core.command.beep")

	if !answer.result.OK {
		t.Errorf("a finished command reads as refused")
	}

	if answer.err != nil {
		t.Errorf("a finished command carries an error: %v", answer.err)
	}

	if answer.result.FaultCode != "" {
		t.Errorf("a finished command carries the fault code %q", answer.result.FaultCode)
	}
}
