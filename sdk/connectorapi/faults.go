package connectorapi

import (
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// ErrUnknownKind reports a kind name that the vocabulary does not define.
var ErrUnknownKind = errors.New("connectorapi: unknown value kind")

const (
	statusBadRequest = 400
	statusConflict   = 409
)

// The codes a driver and the host's validation return. The host decides the
// validation codes before a driver sees the command. A driver reports the rest
// about the device.
var (
	// CapabilityNotSupported refuses a verb the printer does not have, and names
	// the verbs it has.
	CapabilityNotSupported = fault.Register(
		fault.DomainPrinter(), "CAPABILITY_NOT_SUPPORTED", statusConflict, fault.Document(
			"The printer does not have that capability",
			"Send one of the verbs this error carries as verbs, which is the capability set this "+
				"server resolved for the printer. It is a JSON array of strings and not a joined list, "+
				"so a key reads back whole. verb_count is how many verbs the printer has: a count above "+
				"the length of the array means the array is the first 128 and the printer's own "+
				"capability set is the rest. "+
				"The set is handed out with an epoch, and the epoch moves whenever the printer reconnects, "+
				"so a set kept from an earlier epoch describes a printer that is no longer there: "+
				"read it again rather than caching it across a reconnect.",
			fault.RetryNever,
			fault.ShowDetail,
			"verb",
			"verbs",
			"verb_count",
			"command_id",
		))

	// PathNotAllowed refuses a device path that leaves the storage it names.
	PathNotAllowed = fault.Register(
		fault.DomainPrinter(), "PATH_NOT_ALLOWED", statusBadRequest, fault.Document(
			"The path leaves the storage it names",
			"Send a path relative to the storage, with no .. segment. A path is checked as it reaches "+
				"the device, so prefixing a storage onto a path that climbs does not make it legal.",
			fault.RetryNever,
			fault.ShowDetail,
			"path",
			"command_id",
		))

	// PrinterChanged reports a command that was waiting for a device and was
	// never sent, because the device reconnected with a different identity.
	// The caller reads the capability set again. The two firmware parameters
	// are equal when another part of the identity changed.
	PrinterChanged = fault.Register(
		fault.DomainPrinter(), "PRINTER_CHANGED", statusConflict, fault.Document(
			"The printer changed while the command was waiting, so it was not sent",
			"Read the printer's capability set again and send the command against it. The command was "+
				"composed for the firmware the printer had before, the printer came back reporting "+
				"another one, and nothing was sent to the machine.",
			fault.RetryAllowed,
			fault.ShowDetail,
			"verb",
			"previous_firmware",
			"firmware",
			"command_id",
		))

	// CommandUnanswered reports a command that reached the device and lost the
	// connection before any event answered it. The command may have run, so
	// nobody may resend it. The caller reads the device's state once it is
	// reachable.
	CommandUnanswered = fault.Register(
		fault.DomainPrinter(), "COMMAND_UNANSWERED", statusConflict, fault.Document(
			"The printer lost its connection before it answered the command",
			"The command reached the printer, so it may have been carried out. Read the printer's state "+
				"when it connects again and decide from that, rather than sending the command a second "+
				"time. This server does not resend it for you.",
			fault.RetryNever,
			fault.ShowDetail,
			"verb",
			"command_id",
		))

	// CommandQueueFull refuses a command because the device already has as
	// many commands waiting as it accepts. Nothing was sent, so the caller may
	// send it again later.
	CommandQueueFull = fault.Register(
		fault.DomainPrinter(), "COMMAND_QUEUE_FULL", statusConflict, fault.Document(
			"The printer is already holding as many commands as it takes",
			"Wait for the printer to answer what it is holding and send this command again. Nothing was "+
				"sent, so the command was not carried out twice. A printer that is away holds its queue "+
				"until it comes back, so a queue that stays full is a printer that is not answering.",
			fault.RetryAllowed,
			fault.ShowDetail,
			"verb",
			"command_id",
		))

	// CommandRefused reports a device that refused a command and gave its own
	// word for why, in machine_reason. A caller branches on the code and reads
	// machine_reason to decide whether a retry can work.
	CommandRefused = fault.Register(
		fault.DomainPrinter(), "COMMAND_REFUSED", statusConflict, fault.Document(
			"The printer refused the command",
			"Read the machine reason this refusal carries, which is the printer's own word for why. "+
				"Whether sending it again can work depends on that word and not on the refusal: a file "+
				"that is already there is answered by printing it, and a printer busy with a job is "+
				"answered by waiting. Nothing was carried out, so nothing was carried out twice.",
			fault.RetryNever,
			fault.ShowDetail,
			"verb",
			"machine_reason",
			"command_id",
		))

	// CommandRefusedWithoutReason reports a device that refused a command and
	// gave no reason. The text a person reads says the printer is busy with
	// another command. A driver must not put words of its own in
	// machine_reason. The command did not run, so a retry is safe.
	CommandRefusedWithoutReason = fault.Register(
		fault.DomainPrinter(), "COMMAND_REFUSED_WITHOUT_REASON", statusConflict, fault.Document(
			"The printer is busy with another command and refused this one",
			"The printer did not carry the command out because it is still working on a command it "+
				"received earlier. Wait for that command to finish and send this one again. If it keeps "+
				"refusing, read the printer's state and the job it is running.",
			fault.RetryAllowed,
			fault.ShowDetail,
			"verb",
			"command_id",
		))

	// CommandFailed reports a command the device took, tried and did not carry
	// out, with the device's own word for why. Unlike a refusal, the command
	// may have run part way, so a retry is not safe. The caller reads the
	// device's state.
	CommandFailed = fault.Register(
		fault.DomainPrinter(), "COMMAND_FAILED", statusConflict, fault.Document(
			"The printer took the command and could not carry it out",
			"Read the machine reason this carries, which is the printer's own word for what went "+
				"wrong, then read the printer's state. The printer accepted the command and failed "+
				"part way through it, so it may have changed something: this is not resent for you "+
				"and sending it again is a second attempt rather than a first.",
			fault.RetryNever,
			fault.ShowDetail,
			"verb",
			"machine_reason",
			"command_id",
		))

	// CommandFailedWithoutReason reports a command the device took, tried and
	// did not carry out, and gave no reason. It has no machine_reason, and a
	// retry is not safe.
	CommandFailedWithoutReason = fault.Register(
		fault.DomainPrinter(), "COMMAND_FAILED_WITHOUT_REASON", statusConflict, fault.Document(
			"The printer took the command, could not carry it out, and gave no reason for it",
			"Read the printer's state and the job it is running, because the failure itself says "+
				"nothing more than that it happened. The printer accepted the command and did not "+
				"finish it, so it may have changed something: sending it again is a second attempt "+
				"rather than a first.",
			fault.RetryNever,
			fault.ShowDetail,
			"verb",
			"command_id",
		))

	// FileChangeRefused reports a device that would not make a change to its
	// own storage: a folder it would not create, a file it would not remove.
	// machine_reason is the device's own word for why, unchanged.
	FileChangeRefused = fault.Register(
		fault.DomainPrinter(), "FILE_CHANGE_REFUSED", statusConflict, fault.Document(
			"The printer refused that change to its storage",
			"Read the machine reason this error carries. A create refused because the name is taken is "+
				"answered by choosing another; a delete refused because the path names nothing is "+
				"answered by listing the directory first.",
			fault.RetryNever,
			fault.ShowDetail,
			"path",
			"machine_reason",
			"reason",
		))

	// ListingRefused reports a device that would not report what is at a path.
	// machine_reason is the device's own word for why, unchanged. A driver
	// must return this code for a refused listing. Any other error reaches the
	// caller as an internal server error.
	ListingRefused = fault.Register(
		fault.DomainPrinter(), "LISTING_REFUSED", statusConflict, fault.Document(
			"The printer refused to report what is at that path",
			"Read the machine reason this error carries, which is the printer's own word for why. "+
				"For a path that names nothing, list the directory above it. For a printer that takes "+
				"no command in its state, clear that state at the machine and ask again.",
			fault.RetryNever,
			fault.ShowDetail,
			"path",
			"machine_reason",
			"reason",
		))

	// TransferRefused reports a device that would not take the file.
	// machine_reason is the device's own word for why, unchanged, and a caller
	// branches on it. reason is the device's sentence for a person.
	TransferRefused = fault.Register(
		fault.DomainPrinter(), "TRANSFER_REFUSED", statusConflict, fault.Document(
			"The printer would not take the file",
			"Read the machine reason this error carries. A printer refuses a transfer its storage or its "+
				"current state does not allow, so the answer depends on which: make room, remove the file "+
				"that is already there, or wait for the printer to finish what it is doing.",
			fault.RetryNever,
			fault.ShowDetail,
			"path",
			"machine_reason",
			"reason",
		))

	// TransferFailed reports a transfer the device started and did not finish.
	// machine_reason and reason are the device's own. Either may be empty.
	TransferFailed = fault.Register(
		fault.DomainPrinter(), "TRANSFER_FAILED", statusConflict, fault.Document(
			"The transfer did not finish",
			"Send the file again. A transfer that stops part way leaves nothing usable on the printer, "+
				"and the event this reports is the printer's own word for how it ended.",
			fault.RetryAllowed,
			fault.ShowDetail,
			"path",
			"event",
			"machine_reason",
			"reason",
		))

	// TransferNotOnStorage reports a transfer the device said it finished, and
	// whose file is not on the storage under the name it was sent to. The host
	// reads the destination directory back after a transfer.
	//
	// found is what the readback saw: the size the device listed, "absent",
	// "folder", or "staging" for a read-only entry of another length.
	TransferNotOnStorage = fault.Register(
		fault.DomainPrinter(), "TRANSFER_NOT_ON_STORAGE", statusConflict, fault.Document(
			"The printer reported the transfer finished and the file is not on its storage",
			"Send the file under a name that no entry in that directory already shares. A FAT storage "+
				"shortens every long name to an 8.3 spelling, and two long names that shorten to the "+
				"same one are one entry: the second file has nowhere to land. Sending the same name "+
				"again lands in the same place.",
			fault.RetryNever,
			fault.ShowDetail,
			"path",
			"expected_size",
			"found",
		))

	// TransferBusy refuses a second transfer to a device already running one.
	TransferBusy = fault.Register(
		fault.DomainPrinter(), "TRANSFER_BUSY", statusConflict, fault.Document(
			"The printer is already fetching a file",
			"Wait for the transfer in flight to finish, or stop it, then send this one. A printer fetches "+
				"one file at a time and starting a second would abandon the first.",
			fault.RetryAllowed,
			fault.ShowDetail,
			"path",
		))

	// TransferTooLarge refuses a file longer than the device's protocol can
	// describe. limit_bytes is the size the device could have taken.
	TransferTooLarge = fault.Register(
		fault.DomainPrinter(), "TRANSFER_TOO_LARGE", statusBadRequest, fault.Document(
			"The file is longer than a transfer to this printer can describe",
			"Send a file below limit_bytes. The length travels to the printer as an "+
				"unsigned 32-bit count of bytes, so 4 GiB is the first size that has no spelling on the wire.",
			fault.RetryNever,
			fault.ShowDetail,
			"size",
			"limit_bytes",
		))

	// ArgumentUnknown refuses an argument the verb does not accept.
	ArgumentUnknown = fault.Register(
		fault.DomainValidation(), "ARGUMENT_UNKNOWN", statusBadRequest, fault.Document(
			"The command does not accept that argument",
			"Send only the arguments the capability declares. An argument that used to exist is not silently ignored, "+
				"because a command carried out with a dropped argument is not the command that was asked for.",
			fault.RetryNever,
			fault.ShowDetail,
			"verb",
			"argument",
			"command_id",
		))

	// ArgumentMissing refuses a command that omits a required argument for
	// which the descriptor names no default.
	ArgumentMissing = fault.Register(
		fault.DomainValidation(), "ARGUMENT_MISSING", statusBadRequest, fault.Document(
			"The command needs an argument that was not supplied",
			"Supply the argument. It has no default, so there is nothing for the server to fall back to.",
			fault.RetryNever,
			fault.ShowDetail,
			"verb",
			"argument",
			"command_id",
		))

	// ArgumentNotSupported refuses a value outside the argument's supported set.
	ArgumentNotSupported = fault.Register(
		fault.DomainValidation(), "ARGUMENT_NOT_SUPPORTED", statusBadRequest, fault.Document(
			"The printer does not support that value for the argument",
			"Choose one of the supported values, which this error carries as a JSON array of strings. "+
				"It is an array and not a joined list so that a value containing a comma reads back whole.",
			fault.RetryNever,
			fault.ShowDetail,
			"verb",
			"argument",
			"supported",
			"command_id",
		))

	// ArgumentMalformed refuses a value that is not in the form its argument
	// accepts. An unreadable number or boolean has kind. A text argument whose
	// characters a driver restricts has allowed_characters and
	// limit_characters instead.
	ArgumentMalformed = fault.Register(
		fault.DomainValidation(), "ARGUMENT_MALFORMED", statusBadRequest, fault.Document(
			"The value is not in the form the argument accepts",
			"For a number, send a whole or a fractional number. For a boolean, send true or false. "+
				"For text, send only the characters allowed_characters names, and no more than limit_characters of them.",
			fault.RetryNever,
			fault.ShowDetail,
			"verb",
			"argument",
			"kind",
			"allowed_characters",
			"limit_characters",
			"command_id",
		))

	// GCodeMalformed refuses a raw gcode payload that is not one gcode command:
	// two commands, none, or a byte outside printable ASCII.
	GCodeMalformed = fault.Register(
		fault.DomainValidation(), "GCODE_MALFORMED", statusBadRequest, fault.Document(
			"The payload is not one gcode command",
			"Send one command: a G, M or T followed immediately by its number, then its parameters, "+
				"in printable ASCII and on one line. Several commands are a sequence, not a payload, "+
				"so split them and send them as separate steps.",
			fault.RetryNever,
			fault.ShowDetail,
			"gcode",
			"command_id",
		))

	// GCodeNotAllowed refuses a gcode word this server never sends: one that
	// can damage a machine irreversibly or bypass the path checks of the file
	// verbs. No grant lifts it.
	GCodeNotAllowed = fault.Register(
		fault.DomainPrinter(), "GCODE_NOT_ALLOWED", statusConflict, fault.Document(
			"This server never sends that gcode word",
			"Use the verb that covers what the code does. Files are written and deleted through the file "+
				"capabilities, which check the path, and firmware is not updated through this API at all. "+
				"No grant lifts this refusal.",
			fault.RetryNever,
			fault.ShowDetail,
			"gcode",
			"word",
			"reason",
			"command_id",
		))

	// ArgumentOutOfRange refuses a value outside the argument's declared range:
	// further from zero than its limit, or below its lower bound. The refusal
	// names the one bound the value broke, max or min.
	ArgumentOutOfRange = fault.Register(
		fault.DomainValidation(), "ARGUMENT_OUT_OF_RANGE", statusBadRequest, fault.Document(
			"The argument is outside the range the printer accepts",
			"Send a value no further from zero than max, and at or above min. "+
				"The refusal carries whichever of the two the value broke.",
			fault.RetryNever,
			fault.ShowDetail,
			"verb",
			"argument",
			"max",
			"min",
			"command_id",
		))
)

func errUnknownKind(name string) error {
	return fmt.Errorf("%w: %q", ErrUnknownKind, name)
}
