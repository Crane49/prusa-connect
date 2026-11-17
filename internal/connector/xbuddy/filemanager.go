package xbuddy

import (
	"context"
	"encoding/json"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

var _ connectorapi.FileManager = (*Session)(nil)

// CreateFolder makes one folder on the printer's storage.
//
// The three methods here are one J frame each and go through Do, so the path
// rewrite, the traversal refusal and the capability check are the ones every
// other verb gets. What they add is a generic name: a caller asks the core to
// make a folder and never learns that this dialect spells it CREATE_FOLDER.
func (s *Session) CreateFolder(ctx context.Context, path string) error {
	return s.manage(ctx, VerbCreateFolder, path)
}

// DeleteFile removes one file. The firmware refuses a path naming a folder,
// which is why DeleteFolder is a separate call rather than a flag.
func (s *Session) DeleteFile(ctx context.Context, path string) error {
	return s.manage(ctx, VerbDeleteFile, path)
}

// DeleteFolder removes one folder. Whether the entries inside it go with it is
// the firmware's rule: the shipped profiles declare
// core.feature.recursive_delete on every model, and this driver does not
// second-guess it.
func (s *Session) DeleteFolder(ctx context.Context, path string) error {
	return s.manage(ctx, VerbDeleteFolder, path)
}

// manage sends one path-bearing file command and reads the refusal the printer
// answered with.
//
// The machine reason is carried rather than flattened, because the refusals that
// arrive here mean different things to a caller: a name already taken, a path
// naming nothing, a read-only device and a stick that is not there.
func (s *Session) manage(ctx context.Context, verb connectorapi.Capability, path string) error {
	result, err := s.Do(ctx, connectorapi.Command{Verb: verb, Args: map[string]string{"path": path}})
	if err != nil {
		return err
	}

	if !result.OK {
		// Both words, because this firmware does not always send the machine
		// one: a delete aimed at the wrong kind of entry comes back with the
		// human "File not found" and no machine_reason at all, and dropping it
		// left a caller a refusal with nothing in it. The machine reason stays
		// the field to branch on and the human one is there to show.
		return connectorapi.FileChangeRefused.
			New("the printer refused that change to its storage").
			With(
				fault.StringParam("path", path),
				fault.StringParam("machine_reason", result.MachineReason),
				fault.StringParam("reason", explained(humanReason(result.Raw))),
			).
			Wrap(connectorapi.ErrFileChangeRefused)
	}

	return nil
}

// heldReason is the firmware's word for a delete aimed at a file it still holds
// as a transfer (print_utils.cpp remove_file, DeleteResult::ActiveTransfer).
// That is a transfer still running, or a finished one whose file the firmware
// has not moved out of its staging folder because the file is open: a printer
// with a screen opens every file it receives in the print preview. A send that
// replaces a file deletes it first, so this is also what such a send fails with,
// and so does one aimed at a received file that is being printed.
const heldReason = "File is being transferred"

// explained is the firmware's sentence with what a person does about it, for the
// one refusal whose words alone do not say.
//
// That refusal's own words are replaced rather than kept in front. Beta testers
// read "File is being transferred" as the file arriving, under a heading that
// said it had not, and as naming the wrong thing for a file being printed. Only
// this exact sentence is replaced, so the firmware's words follow from it.
func explained(reason string) string {
	if reason != heldReason {
		return reason
	}

	return "The printer kept its file at this path because it still has it open, " +
		"in a transfer, the print preview, a print or the end-of-print screen. " +
		"Close it or let it end on the printer, then try again"
}

// humanReason is the sentence the firmware wrote for a person, empty when it
// sent none. It is read out of the raw event rather than off Result, which
// carries the machine reason alone by design.
func humanReason(raw json.RawMessage) string {
	var event printerEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return ""
	}

	return event.Reason
}
