package xbuddy

// Asking the printer what the print is now.
//
// Buddy firmware volunteers a JOB_INFO when a print starts and never again. It
// does not send one after PAUSE_PRINT, after RESUME_PRINT or after STOP_PRINT,
// and it does not send one when a print reaches its own end: those are state
// changes, the machine reports them at /state on the telemetry frame, and
// /job/state, /job/path and /job/display go on saying whatever the frame at the
// start of the print said. Measured on an MK4S running 6.5.7+12836: a print was
// paused, resumed and stopped through the command surface, /state followed all
// three, and the stored job kept the word PRINTING for ever (BUG-004).
//
// A print cancelled at the printer before it began leaves no STOPPED either:
// the machine goes from PRINTING or ATTENTION straight to IDLE, and the driver
// reads that move as the stop, see printWasAbandoned.
//
// So the host asks. SEND_JOB_INFO and SEND_STATE_INFO both exist in this
// dialect and neither was ever sent, which is the whole of the defect: nothing
// on this side of the wire ever put the question. This file is where the
// question is put, on the two occasions that can mean the print moved, a verb
// of ours that moves it, and the machine's own state changing under us.
//
// The second of those is a level and not an edge on the wire: telemetry
// restates /state four times a second, and a machine flapping between ATTENTION
// and PRINTING while an operator holds a dialog would be four requests a second
// on a link that carries one command at a time. Two things bound it. Only a
// change of value asks at all, and the pump below coalesces every request that
// arrives while one is in flight into the single question they are all asking,
// then refuses to ask again inside jobRefreshFloor.

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	// jobRefreshDelay is how long the request waits after the thing that
	// prompted it. The firmware answers a verb with its terminal event before
	// it has finished acting on it: a SEND_JOB_INFO sent in the same breath as
	// the reply to PAUSE_PRINT is answered out of the state the machine was in
	// when it was asked, which is the state we are trying to move off. It is
	// the settle delay requestInfo already takes after a socket attaches, at
	// twice the length, because a pause parks the head and the info request
	// only reads a table.
	jobRefreshDelay = 2 * infoDelay

	// jobRefreshFloor is the shortest gap between two refreshes. It is the
	// rate limit, and it is here rather than in the caller because the caller
	// is telemetry: a machine whose state is flapping produces requests at
	// whatever rate it flaps at, and this is what turns that into a fixed cost
	// of two frames a second on a link that carries one command at a time.
	jobRefreshFloor = time.Second

	// jobRefreshTimeout bounds one refresh. A refresh is worth nothing once it
	// is stale, and a request that waited on a printer for longer than this has
	// been overtaken by the next telemetry frame's answer anyway. Without it a
	// refresh queued for a printer that went away between the check and the
	// send would hold this pump until the printer came back.
	jobRefreshTimeout = 30 * time.Second

	// jobIDUnknown is the job number of a machine that has not named one. Job
	// numbers travel as uint16, so no negative value collides with one.
	jobIDUnknown = -1
)

// The words the machine and the print are described with, as the firmware
// spells them. /state is one of IDLE, PRINTING, PAUSED, FINISHED, STOPPED,
// READY, ERROR, BUSY, ATTENTION and UNKNOWN, which is the whole of
// to_str(DeviceState) in Prusa-Firmware-Buddy/src/state/printer_state.cpp:563;
// /job/state is one of PRINTING, FINISHED and IDLE (STATE.md, "Job state
// vocabulary", measured on an MK4S running 6.5.7+12836). BUSY is what a long
// move reports: an MK4 homing and levelling its bed held it for 80 to 100
// seconds (BUG-012). Whether a machine reports BUSY over a running print is
// unmeasured, so neither machineIsHoldingAPrint nor machineEndedThePrint names
// it: it is a word about the machine and not about the print, and
// machineSaysNothingAboutThePrint is what that means for /job/state.
// stateAttention is beside the dialog states in inbound.go, because a dialog is
// what that word means.
const (
	statePrinting = "PRINTING"
	statePaused   = "PAUSED"
	stateStopped  = "STOPPED"
	stateFinished = "FINISHED"
)

// machineIsHoldingAPrint is the machine states a print is still on the bed in.
//
// It decides two things. A move into the set or out of it is worth asking the
// printer about, and a move inside it is not: an operator holding a dialog over
// a running print flaps between ATTENTION and PRINTING, and neither word says
// anything new about which print it is. And a machine outside the set with a
// print that still claims to be running is a print that ended.
//
//nolint:gochecknoglobals // a fixed set, read-only after init.
var machineIsHoldingAPrint = []string{statePrinting, statePaused, stateAttention}

// machineEndedThePrint is the two words a machine ends a print with. They are
// the words /job/state keeps once the print is over, because the machine itself
// goes on to IDLE and the print's own last word is how it ended.
//
//nolint:gochecknoglobals // a fixed set, read-only after init.
var machineEndedThePrint = []string{stateStopped, stateFinished}

// printMovingVerbs are the verbs that change what is on the bed, and therefore
// the verbs after which the print the state tree describes is the print that
// was there before. START_PRINT is among them: the firmware does volunteer a
// JOB_INFO for it, and asking anyway is what makes the tree right on a firmware
// that does not.
//
//nolint:gochecknoglobals // a fixed set, read-only after init.
var printMovingVerbs = []connectorapi.Capability{
	VerbStartPrint, VerbPausePrint, VerbResumePrint, VerbStopPrint,
}

// movesThePrint reports whether carrying this verb out changes what the machine
// is printing.
func movesThePrint(verb connectorapi.Capability) bool {
	return slices.Contains(printMovingVerbs, verb)
}

// requestJobRefresh asks for one refresh, and never blocks.
//
// The channel holds one token, so a request that arrives while one is already
// waiting is dropped rather than queued: two requests are the same question,
// and the answer to the second is the answer to the first.
func (s *Session) requestJobRefresh() {
	select {
	case s.refresh <- struct{}{}:
	default:
	}
}

// noteMachineState records the machine's own state, writes the end of a print
// into the tree, and asks for a refresh when the print may have moved.
//
// It is called from the one read goroutine, for telemetry and for every event
// that carries a state, so the compare-and-store below is never concurrent with
// itself. The atomic is for the refresh pump's sake and not for this.
//
// A state this session has not seen before counts as a move, which is what
// makes the first telemetry frame of a connection ask what the machine is
// printing. A host that reattached to a printer mid-print otherwise holds
// whatever the record store restored and never learns better.
//
// A move from a print state straight to IDLE or READY, with /job/state still
// saying the print runs, is a print cancelled at the printer: it is recorded as
// STOPPED, see printWasAbandoned.
//
// After that, only a move into machineIsHoldingAPrint or out of it asks. A flap
// between ATTENTION and PRINTING is a dialog over one unchanged print, and
// asking about it produced a SEND_JOB_INFO and a SEND_STATE_INFO every second
// for as long as the operator left the dialog up.
func (s *Session) noteMachineState(state string) {
	if state == "" {
		return
	}

	previous := s.machineState.Swap(&state)
	s.stateHeard.Store(true)

	if previous != nil && *previous == state {
		return
	}

	end := state
	if s.printWasAbandoned(previous, state) {
		end = stateStopped
	}

	s.noteThePrintOutcome(end)
	s.noteThePrintEnded(end)
	s.noteThePrintIsRunning(state)

	if !worthAskingAbout(previous, state) {
		return
	}

	s.requestJobRefresh()
}

// printWasAbandoned reports whether a machine moving from one state to another
// ended the print without saying so.
//
// A print cancelled from the printer's own screen before it began, at the
// filament check for one, goes PRINTING or ATTENTION and then IDLE with no
// STOPPED between. Nothing else on the wire says the print ended, and the
// JOB_INFO a refresh brings back for IDLE is ignored, so /job/state would hold
// PRINTING for ever. The move is read as STOPPED, which is what a cancel is.
//
// The held /job/state is the guard: a print already holding an end word, or one
// that was never named, is not stopped a second time.
func (s *Session) printWasAbandoned(previous *string, state string) bool {
	if previous == nil || !slices.Contains(machineIsHoldingAPrint, *previous) {
		return false
	}

	if state != stateIdle && state != stateReady {
		return false
	}

	held := s.jobState.Load()

	return held != nil && (*held == statePrinting || *held == statePaused)
}

// worthAskingAbout reports whether a machine moving from one state to another
// can mean the print moved.
//
// A state this session has not seen before always is: it is the first frame of
// a connection, and a host that reattached to a printer mid-print holds
// whatever the record store restored until it asks. After that only a move
// across machineIsHoldingAPrint's edge is, because a move inside the set is one
// print with a dialog over it and a move outside it is a machine tidying up
// after a print that has already ended.
func worthAskingAbout(previous *string, state string) bool {
	if previous == nil {
		return true
	}

	if *previous == state {
		return false
	}

	return slices.Contains(machineIsHoldingAPrint, *previous) != slices.Contains(machineIsHoldingAPrint, state)
}

// noteThePrintEnded writes the machine's terminal word into /job/state, so that
// the tree holds how the print ended for as long as the row is about it.
//
// /job/state is this driver's to own: it comes out of JOB_INFO, the firmware
// volunteers one only at the start of a print, and the machine then reports
// STOPPED or FINISHED at /state and goes back to IDLE a moment later. A
// machine that goes to IDLE with no STOPPED is written as STOPPED by
// noteMachineState, which passes the word it means here. A reader
// deriving the job's state from the tree therefore saw PRINTING again as soon
// as the machine idled, and the record froze a finished print as PRINTING
// (BUG-004). Writing it here makes the terminal word sticky: it lands before
// the machine idles, both readers of the tree agree, and the answer survives
// the record flush that closes the row.
//
// The write is refused once the print has a terminal word, because the refresh
// this same move asks for is answered with a JOB_INFO saying IDLE on a machine
// that has no job any more, and that answer must not erase how the print ended.
func (s *Session) noteThePrintEnded(machine string) {
	if !slices.Contains(machineEndedThePrint, machine) {
		return
	}

	if held := s.jobState.Load(); held != nil && !slices.Contains(machineIsHoldingAPrint, *held) {
		return
	}

	if s.jobState.Load() == nil && s.job.Load() == jobIDUnknown {
		// A machine this session has heard nothing about a print from. STOPPED
		// on the first frame of a connection is the state a machine was left
		// in, and inventing a job state for a print nobody named would put a
		// row on a device that has never printed.
		return
	}

	s.reportJobState(machine)
}

// noteThePrintIsRunning writes the machine's own word into /job/state while
// there is a print on the bed, which is what keeps a running print from being
// stored as a paused one.
//
// JOB_INFO is a weak witness for this and the firmware source says why: the
// JobInfo renderer writes "PRINTING" only when the device state is exactly
// Printing and "PAUSED" for every other one, paused, a dialog on the screen,
// busy (Prusa-Firmware-Buddy, src/connect/render.cpp, the EventType::JobInfo
// arm). A print started through the command surface raises the print-preview
// dialog, so the SEND_JOB_INFO the pump sends at the start of a print is
// answered in ATTENTION and says PAUSED about a print nobody paused, and the
// operator answering the dialog moves the machine ATTENTION -> PRINTING, which
// is one print with a dialog over it and not worth asking about. /job/state
// therefore held that PAUSED for the whole visible middle of the print
// (BUG-013, thirteen minutes of it on an MK4S with progress climbing).
//
// The machine's own /state is the strong witness: it arrives on every telemetry
// frame, it is the field the firmware derives that PAUSED from, and it is the
// only one that moves when a print is paused or resumed at the machine at all.
//
// ATTENTION writes nothing. It is a dialog raised over a print whose state has
// not changed, so the word already held is the right one; overwriting it with
// PAUSED would report the dialog as a pause, and with PRINTING would report a
// paused print as running.
//
// The terminal word wins, because a print that has ended keeps how it ended
// (BUG-004) and only a new job number retires that: see noteJobID.
func (s *Session) noteThePrintIsRunning(machine string) {
	if machine != statePrinting && machine != statePaused {
		return
	}

	if s.jobStateHoldsAnEnd() {
		return
	}

	if s.jobState.Load() == nil && s.job.Load() == jobIDUnknown {
		// Nothing has named a print on this machine yet, and a state alone is
		// not a print: writing one would put a row on a device whose first
		// telemetry frame happened to say PRINTING before it said which job.
		return
	}

	s.reportJobState(machine)
}

// reportJobState patches /job/state and remembers the word, which is what makes
// the rule in noteThePrintEnded readable without the state tree.
func (s *Session) reportJobState(state string) {
	if state == "" {
		return
	}

	s.jobState.Store(&state)
	s.reportText(jobStatePath, state)
}

// machineWord is the machine's own state as of this frame: the state the frame
// carries on its envelope, and the newest one this session was told when the
// frame carries none. Every frame on this dialect carries it, so the fallback is
// for a firmware that leaves it out.
func (s *Session) machineWord(carried string) string {
	if carried != "" {
		return carried
	}

	if held := s.machineState.Load(); held != nil {
		return *held
	}

	return ""
}

// machineSaysNothingAboutThePrint reports whether the machine's own word leaves
// the print's word alone, which is when a JOB_INFO's state may not be believed.
//
// The vocabulary splits three ways and this is the third part of it.
// machineIsHoldingAPrint is a print on the bed and the machine's word decides
// /job/state from it. machineEndedThePrint is the machine saying how the print
// ended, and a JOB_INFO answered then describes the same end. Everything else
// is the machine talking about itself rather than about the print: BUSY above
// all, which an MK4 held for 80 to 100 seconds while it homed and levelled its
// bed (BUG-012), and IDLE, READY, ERROR and UNKNOWN with it. A print that went
// to IDLE straight from a print state was written as STOPPED by noteMachineState
// before this is asked, so the word held is its end and not a stale PRINTING.
//
// It matters because the firmware's answer is coarse. A refresh crosses
// machineIsHoldingAPrint's edge on the way into a long move, and the JOB_INFO
// it asks for is rendered PAUSED for every device state that is not exactly
// Printing, so believing it would report a long move as a pause, which is
// BUG-013's symptom in BUG-012's state. The word already at /job/state is the
// last thing the machine did say about the print, and it is the better answer.
//
// An empty word is not one of the three: a firmware that put no state on the
// envelope and has told this session none has said nothing at all, and the
// frame's own word is then all there is.
//
// It asks for a print to be named, because a machine that has named none has no
// word of its own to keep.
func (s *Session) machineSaysNothingAboutThePrint(machine string) bool {
	if machine == "" || slices.Contains(machineEndedThePrint, machine) {
		return false
	}

	return s.job.Load() != jobIDUnknown
}

// jobStateHoldsAnEnd reports whether the print's own last word is a terminal
// one, so a JOB_INFO that arrives after the print ended cannot overwrite it.
func (s *Session) jobStateHoldsAnEnd() bool {
	held := s.jobState.Load()

	return held != nil && slices.Contains(machineEndedThePrint, *held)
}

// noteJobID records the job number the machine named, so that a later
// SEND_JOB_INFO can name it too, and asks for a refresh when the number moved.
//
// A new job number is the strongest statement a telemetry frame makes about the
// print: the machine has a different print on the bed from the one the state
// tree describes. It also closes the window at the start of a print. The state
// change to PRINTING arrives before the first frame that carries a job number,
// so the refresh that change asks for has no print to name yet, and without
// this the file would not reach the tree until the machine's state moved again.
//
// A number the machine has not used before also retires the word the previous
// print ended with. The terminal word is sticky on purpose, the machine goes
// back to IDLE and the print's own last word is how it ended, and it is sticky
// for one print: without this, the STOPPED of the print before would refuse
// every state the next print reports and the second print of a session would
// never leave STOPPED. The objects of the print before go with it, for the same
// reason: see forgetTheObjects.
//
// Only a print of its own retires it, and 0 is not one. Job numbers travel as
// uint16 and the firmware reports 0 between prints, for the machine that has
// none; a rule that read every number but jobIDUnknown as a new print therefore
// gave the first idle frame after a print the power to clear how it ended, and
// the refresh that same frame asks for wrote IDLE over it. That is BUG-004
// again, one frame later.
func (s *Session) noteJobID(id int64) {
	if s.job.Swap(id) == id {
		return
	}

	if id > 0 {
		s.forgetTheOutcome()
		s.forgetTheObjects(id)

		if s.jobStateHoldsAnEnd() {
			s.forgetHowThePrintEnded()
		}
	}

	s.requestJobRefresh()
}

// forgetHowThePrintEnded drops the terminal word, from this session and from the
// state tree both.
//
// The tree is the half that was missing. A reader takes the print's state out of
// /job/state and nowhere else, so clearing only the session's copy left the
// previous print's STOPPED standing at the new print's job number for as long as
// it took a JOB_INFO to arrive, and a record flush in that window stored the new
// print as stopped. A patch of null is how this dialect says a node is gone,
// the same word a closed dialog is reported with, dialogAbsent in inbound.go,
// and the record drops the path rather than storing the word.
func (s *Session) forgetHowThePrintEnded() {
	s.jobState.Store(nil)
	s.report(connectorapi.StatePatch{Path: jobStatePath, Value: json.RawMessage(dialogAbsent)})
}

// noteReportedJobID reads a job number off a telemetry value. A frame that
// carries something other than a number has said nothing about which print it
// means, and the number this session already holds is better than none.
func (s *Session) noteReportedJobID(raw json.RawMessage) {
	var id int64
	if err := json.Unmarshal(raw, &id); err != nil {
		return
	}

	s.noteJobID(id)
}

// refreshPump serves one refresh at a time for the session's whole life.
//
// One goroutine is what makes the coalescing and the rate limit true by
// construction. Every request lands in a channel of one, the pump waits out the
// settle delay, throws away anything that arrived during it as the same
// question, asks, and then holds the floor before it will look at the channel
// again. A machine changing state a hundred times a second costs one pair of
// frames per jobRefreshFloor and no goroutine per change.
func (s *Session) refreshPump() {
	for {
		select {
		case <-s.done:
			return
		case <-s.refresh:
		}

		if !s.pause(jobRefreshDelay) {
			return
		}

		// Whatever arrived while the delay ran is asking what this is about to
		// ask, and the answer will be as new as theirs would have been.
		select {
		case <-s.refresh:
		default:
		}

		s.askWhatThePrintIs()

		if !s.pause(jobRefreshFloor) {
			return
		}
	}
}

// pause waits, and reports false when the session ended first.
func (s *Session) pause(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-s.done:
		return false
	}
}

// askWhatThePrintIs sends the two read verbs, and sends neither to a printer
// that is not there.
//
// A printer that is away is skipped rather than queued. The queue is for work a
// caller asked for and is waiting on; a refresh is this driver's own reading,
// it is worthless by the time the printer comes back, and the first telemetry
// frame of the new connection asks for a fresh one anyway.
//
// Either verb may be missing from the resolved set, SEND_STATE_INFO wants the
// DIALOGS feature, which the older firmware in the matrix does not have, and a
// missing one is skipped rather than being a reason to skip the other.
func (s *Session) askWhatThePrintIs() {
	if !s.Connected() {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), jobRefreshTimeout)
	defer cancel()

	if job := s.job.Load(); job != jobIDUnknown && s.canSend(VerbSendJobInfo) {
		s.ask(ctx, connectorapi.Command{
			Verb: VerbSendJobInfo,
			Args: map[string]string{"job_id": strconv.FormatInt(job, 10)},
		})
	}

	if s.canSend(VerbSendStateInfo) {
		s.ask(ctx, connectorapi.Command{Verb: VerbSendStateInfo})
	}
}

// canSend reports whether this session may put a verb on the wire at all.
func (s *Session) canSend(verb connectorapi.Capability) bool {
	return s.caps.Has(verb) || s.sends.Has(verb)
}

// ask sends one read verb and keeps nothing. What it is for is the frames the
// printer sends back: the JOB_INFO and the STATE_CHANGED reach the state tree
// through dispatchEvent like any other, whether they carry this command's id or
// not. A refusal is a debug line, because a firmware that will not answer
// SEND_JOB_INFO is a printer whose job state stops moving and not a fault the
// operator can act on.
func (s *Session) ask(ctx context.Context, cmd connectorapi.Command) {
	if _, err := s.Do(ctx, cmd); err != nil {
		s.logDebug(ctx, "the printer was not asked what it is printing",
			slog.String("verb", cmd.Verb.String()), slog.String("error", err.Error()))
	}
}
