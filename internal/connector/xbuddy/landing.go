package xbuddy

// The readback that turns the printer's word for a finished transfer into a
// file this driver has seen on the storage.
//
// TRANSFER_FINISHED is the board's own report and not an entry in a directory.
// A CORE One L at 6.8.1+16182 accepted two files whose long names shorten to
// one FAT 8.3 spelling, reported both finished with every byte taken, and
// listed only the first of them (BUG-014). Nothing on the transfer's own wire
// says which of the two the storage holds, so the driver asks the printer for
// the directory instead.

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	// landingBudget bounds the folder readback, including its retries, which
	// runs only when the question about the file itself proved nothing, so a
	// readback takes at most landingAskBudget plus landingBudget. It is short
	// because it runs after the file has arrived: a transfer must not hang on
	// the question of whether it landed, and a printer that will not answer
	// inside it leaves a warning rather than a failure.
	landingBudget = 5 * time.Second

	// landingAskBudget bounds the first question, the one about the file itself.
	// An MK4S answers it in about 1.5 s where a listing of a full USB root
	// takes 15 s. A question that runs out of it is not followed by the folder
	// readback: see judgeLanding.
	landingAskBudget = 2 * time.Second

	// landingAttempts is how many listings the readback takes before it reports
	// the file absent, and landingGap is the wait between them. A board writes
	// the file, renames it into place and only then lists it, so one listing
	// taken in that window would report a file that is on its way as missing.
	landingAttempts = 3
	landingGap      = 500 * time.Millisecond

	// absentEntry is what the fault reports as found for a name the directory
	// holds no entry for at all, which is BUG-014's own case.
	absentEntry = "absent"
	// folderEntry is what it reports for a name that is a directory. A transfer
	// whose staging directory was left behind lists as one.
	folderEntry = "folder"
	// stagingEntry is what the readback reports as found for a name the
	// firmware lists read-only: in the log of a staged transfer, and in the
	// fault for one listed at a length other than the one sent. The payload is
	// inside a staging directory the firmware renders as a read-only file of the
	// full length (render.cpp DirRenderer, the get_transfer_partial_file_stat
	// branch).
	stagingEntry = "staging"

	// stagedWarning is what a transfer the firmware has not moved into place
	// carries. Transfer::done renames the payload out of its staging directory,
	// and the rename fails while the file is open. A printer with a screen opens
	// every file it starts to receive in the print preview, so on an idle
	// printer the file is open when the transfer finishes. The firmware moves it
	// once the printer is idle and the file is closed, and until then it refuses
	// to delete or replace it. The rename can also fail for a reason that does
	// not go away, such as a leftover temporary file the firmware cannot
	// replace, so the warning says "usually".
	stagedWarning = "the printer has the whole file and has not moved it into place yet, " +
		"usually because the file is open, for example in the print preview. " +
		"It cannot be deleted or replaced until the print preview or the end-of-print screen on the printer is closed"
)

// verdict is what one readback established about the name a file was sent to.
type verdict int

const (
	// fileLanded: a listing held the name as a writable file of the sent length.
	fileLanded verdict = iota
	// fileMissing: every listing answered inside the budget, and the last of
	// them did not hold the name that way. This is the only verdict that fails a
	// transfer the printer reported finished.
	fileMissing
	// fileStaged: the last listing held the name read-only. The firmware has the
	// whole file and moves it into place later, so the transfer finishes with
	// stagedWarning.
	fileStaged
	// answerUnknown: the printer would not answer, or the budget ended before it
	// had answered as often as a file is called absent. Nothing was established
	// either way, so the transfer keeps the printer's word for it and carries a
	// warning.
	answerUnknown
	// callerGone: the caller stopped waiting, which is a stop pressed inside the
	// readback window. The listing it ended is the caller's doing and not the
	// printer's, so it is settled the way a stop before TRANSFER_FINISHED is.
	callerGone
)

// readback is one readback's outcome.
type readback struct {
	// seen is the last listing's word on the name, read only for fileMissing.
	seen landing
	// verdict is what was established.
	verdict verdict
	// why names the question that went unanswered, for the warning and the log.
	why string
	// err is the listing's failure or the caller's cancellation.
	err error
}

// directoryReader is how a readback asks what is in a directory. It is a
// function rather than the session's own method so that the budget, the retries
// and the verdicts can be tested against a printer that answers slowly, which
// is the case a real one takes and no wire reproduces on demand.
type directoryReader func(ctx context.Context, directory string) (connectorapi.Listing, error)

// confirmLanding reports the transfer as failed when the destination directory
// does not hold the file under the name it was sent to, and nil when it does.
//
// A readback this driver cannot take is not a failure: the printer has said the
// transfer finished, and a firmware that cannot be asked for a listing has said
// everything it is going to say. Such a transfer finishes with a warning on the
// host's last report and a line in the log.
func (s *Session) confirmLanding(ctx context.Context, pending *transfer) error {
	directory, name, addressable := splitDestination(pending.path)
	if !addressable {
		s.cannotConfirm(ctx, pending, "the destination names no directory to read back")

		return nil
	}

	if !s.caps.Has(connectorapi.VerbSendFileInfo) {
		s.cannotConfirm(ctx, pending, "this printer cannot be asked what is in a directory")

		return nil
	}

	read := func(ctx context.Context, directory string) (connectorapi.Listing, error) {
		return s.listDirectory(ctx, directory, pending.listingFloor)
	}

	return s.judgeLanding(ctx, pending, directory, name, read)
}

// landedAsAsked is whether the printer's answer about the file itself names it
// as sent, writable and of the sent length. It matches DisplayName, or Name when
// DisplayName is empty, and never Path: rooted rewrites Path to the one asked,
// so a Path always matches.
func landedAsAsked(listing connectorapi.Listing, name string, size int64) bool {
	file := listing.Directory

	spelling := file.DisplayName
	if spelling == "" {
		spelling = file.Name
	}

	asked := landing{
		present: strings.EqualFold(spelling, name), folder: file.Folder, readOnly: file.ReadOnly, size: file.Size,
	}

	return asked.landed(size)
}

// judgeLanding answers the transfer with what the printer says about the file
// itself, and falls back to a readback of the directory when the printer
// answered and the answer does not prove the landing. Staged and absent
// verdicts are always the directory's.
func (s *Session) judgeLanding(
	ctx context.Context, pending *transfer, directory, name string, read directoryReader,
) error {
	asked, done := context.WithTimeout(ctx, landingAskBudget)
	listing, err := read(asked, directory+"/"+name)
	unanswered := err != nil && ctx.Err() == nil && asked.Err() != nil

	done()

	if err == nil && landedAsAsked(listing, name, pending.total) {
		return nil
	}

	// A printer that did not answer about one file inside landingAskBudget
	// does not list its folder inside landingBudget either, and a listing
	// that reached the wire would hold it for as long as the folder takes.
	if unanswered {
		s.cannotConfirm(ctx, pending, "the printer did not say whether it holds "+name,
			slog.String("error", err.Error()))

		return nil
	}

	result := readBackLanding(ctx, read, directory, name, pending.total)

	switch result.verdict {
	case fileLanded:
		return nil
	case fileStaged:
		pending.warn(stagedWarning)

		s.logInfo(ctx, "the printer reported a transfer finished and has not moved its file into place yet",
			slog.String("path", pending.path),
			slog.String("directory", directory),
			slog.Int64("expected_size", pending.total),
			slog.String("found", stagingEntry))

		return nil
	case answerUnknown:
		detail := []slog.Attr{}
		if result.err != nil {
			detail = append(detail, slog.String("error", result.err.Error()))
		}

		s.cannotConfirm(ctx, pending, result.why, detail...)

		return nil
	case callerGone:
		s.logDebug(ctx, "a transfer's readback ended with the caller and not with the printer",
			slog.String("path", pending.path), slog.String("reason", result.why))

		return fmt.Errorf("xbuddy: confirming %s reached the storage: %w", pending.path, result.err)
	case fileMissing:
	}

	s.logInfo(ctx, "the printer reported a transfer finished and its file is not on the storage",
		slog.String("path", pending.path),
		slog.String("directory", directory),
		slog.Int64("expected_size", pending.total),
		slog.String("found", result.seen.describe()))

	return connectorapi.TransferNotOnStorage.
		New("the printer reported the transfer finished and the file is not on its storage").
		With(
			fault.StringParam("path", pending.path),
			fault.StringParam("expected_size", strconv.FormatInt(pending.total, 10)),
			fault.StringParam("found", result.seen.describe()),
		).
		Wrap(connectorapi.ErrTransferFailed)
}

// readBackLanding takes up to landingAttempts listings of the destination
// directory inside landingBudget and reports what they established.
func readBackLanding(
	ctx context.Context, read directoryReader, directory, name string, size int64,
) readback {
	bounded, done := context.WithTimeout(ctx, landingBudget)
	defer done()

	var seen landing

	for attempt := 1; ; attempt++ {
		listing, err := read(bounded, directory)
		if err != nil {
			if stopped := callerStopped(ctx); stopped != nil {
				return *stopped
			}

			return readback{
				verdict: answerUnknown,
				why:     "the printer would not say what is in " + directory,
				err:     err,
			}
		}

		seen = landingOf(listing, name)
		if seen.landed(size) {
			return readback{verdict: fileLanded}
		}

		// Only a reading taken on the last attempt is conclusive. A file the
		// board has not listed yet is the ordinary case for the first two.
		if attempt == landingAttempts {
			if seen.staged(size) {
				return readback{seen: seen, verdict: fileStaged}
			}

			return readback{seen: seen, verdict: fileMissing}
		}

		if !waitFor(bounded, landingGap) {
			break
		}
	}

	// The budget ended with the question open. The readings taken so far are the
	// ones that were inconclusive by construction, so they establish nothing:
	// failing the transfer on them would report a file the board was still
	// writing as one that never arrived.
	if stopped := callerStopped(ctx); stopped != nil {
		return *stopped
	}

	// A staged reading needs no further listing to be told apart: the printer
	// has the file, and the warning that says why it is locked is the true one.
	if seen.staged(size) {
		return readback{seen: seen, verdict: fileStaged}
	}

	return readback{
		verdict: answerUnknown,
		why:     "this printer did not answer often enough inside the readback's budget",
		err:     bounded.Err(),
	}
}

// callerStopped is the verdict for a readback the caller ended, and nil for one
// the caller is still waiting for. A stop pressed inside the readback window
// cancels the listing, and the printer is not what failed.
func callerStopped(ctx context.Context) *readback {
	if ctx.Err() == nil {
		return nil
	}

	return &readback{
		verdict: callerGone,
		why:     "the caller stopped waiting while the readback was in flight",
		err:     ctx.Err(),
	}
}

// cannotConfirm records that the file was not confirmed, for a transfer the
// printer has already reported finished.
func (s *Session) cannotConfirm(ctx context.Context, pending *transfer, why string, detail ...slog.Attr) {
	pending.warn("the file was sent and this printer could not be asked whether it is there: " + why)

	attrs := append([]slog.Attr{
		slog.String("path", pending.path),
		slog.String("reason", why),
	}, detail...)

	s.logDebug(ctx, "a finished transfer was not confirmed against the printer's own listing", attrs...)
}

// landing is what one listing said about the name a file was sent to.
type landing struct {
	present bool
	folder  bool
	// readOnly is the firmware's read_only flag, which on a directory listing
	// marks a payload still inside its staging directory.
	readOnly bool
	size     int64
}

// landed is whether this reading is the file at rest under the name it was sent
// to. A read-only entry is not one: see staged.
func (l landing) landed(size int64) bool {
	return l.present && !l.folder && !l.readOnly && l.size == size
}

// staged is whether this reading is a payload of the sent length still inside
// its staging directory. The printer has reported every byte taken, and the firmware moves
// the payload into place once nothing holds the file open, so the file is there
// and only the move is still to come.
func (l landing) staged(size int64) bool {
	return l.present && !l.folder && l.readOnly && l.size == size
}

// describe is the value the fault reports as found, for a caller that has to
// tell an absent file from one of the wrong length.
func (l landing) describe() string {
	switch {
	case !l.present:
		return absentEntry
	case l.folder:
		return folderEntry
	case l.readOnly:
		return stagingEntry
	default:
		return strconv.FormatInt(l.size, 10)
	}
}

// landingOf finds the entry a name addresses in a directory the printer
// reported.
//
// The match is case-insensitive because the storage is FAT, where one name in
// two spellings is one entry, and it is taken against both names the firmware
// reports: the long one is what the file was sent under, and a board that
// reports only the short one reports it as the name.
func landingOf(listing connectorapi.Listing, name string) landing {
	for _, entry := range listing.Entries {
		if !namesEntry(entry, name) {
			continue
		}

		return landing{
			present:  true,
			folder:   entry.Folder,
			readOnly: entry.ReadOnly,
			size:     entry.Size,
		}
	}

	return landing{}
}

func namesEntry(entry connectorapi.File, name string) bool {
	for _, spelling := range []string{entry.DisplayName, entry.Name, baseName(entry.Path)} {
		if spelling != "" && strings.EqualFold(spelling, name) {
			return true
		}
	}

	return false
}

// splitDestination is the directory a transfer's destination lands in and the
// name it lands under. It reports false for a path that names no directory,
// which is a destination outside every storage and one no readback addresses.
func splitDestination(path string) (directory, name string, addressable bool) {
	trimmed := relativePath(path)

	index := strings.LastIndex(trimmed, "/")
	if index <= 0 || index == len(trimmed)-1 {
		return "", "", false
	}

	return trimmed[:index], trimmed[index+1:], true
}

// baseName is the last segment of a path.
func baseName(path string) string {
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}

	return path
}

// waitFor sleeps between two listings and reports false for a context that
// ended first, so a readback never outlives its budget.
func waitFor(ctx context.Context, gap time.Duration) bool {
	timer := time.NewTimer(gap)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
