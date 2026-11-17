package xbuddy

// How this firmware's words for the end of a print map onto the closed set the
// host publishes.
//
// The map is here because only this driver knows what its machine means. The
// host reads /job/outcome and nothing else: it holds no table of any dialect's
// words, so a print that ends with nothing under that path is published as
// unknown, which is what a power cut looks like.

import (
	"encoding/json"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// jobOutcomePath is where this driver writes how the print ended.
const jobOutcomePath = connectorapi.JobOutcomePath

// jobOutcomes is this firmware's three terminal words.
//
// STOPPED is cancelled, and the firmware spells a stop by a person and a stop
// this host asked for with that one word, so neither is separated here.
//
// ERROR is failed, and it is the one that is not in machineEndedThePrint. A
// machine in ERROR has stopped and the firmware decides what that did to the
// print, so /job/state keeps the print's own last word; the outcome still says
// the print did not reach the end of its file.
//
//nolint:gochecknoglobals // a lookup table, read-only after init.
var jobOutcomes = map[string]connectorapi.JobOutcome{
	stateFinished: connectorapi.JobFinished,
	stateStopped:  connectorapi.JobCancelled,
	stateError:    connectorapi.JobFailed,
}

// noteThePrintOutcome writes how the print ended, once per print.
//
// The first terminal word wins. A machine that goes ERROR and then STOPPED
// stopped because it failed, and a second write would report the tidying up
// rather than the failure.
//
// It asks for a print to be named, because a machine that has named none has no
// print to end. A machine left in STOPPED that this session has just connected
// to is that case: the word is about a print nobody here ever saw.
func (s *Session) noteThePrintOutcome(machine string) {
	outcome, terminal := jobOutcomes[machine]
	if !terminal {
		return
	}

	if s.job.Load() == jobIDUnknown {
		return
	}

	if s.jobOutcome.Load() != nil || s.jobStateHoldsAnEnd() {
		return
	}

	s.jobOutcome.Store(&outcome)
	s.reportText(jobOutcomePath, outcome)

	if outcome == connectorapi.JobFinished {
		s.reportTheWholePrint()
	}
}

// reportTheWholePrint writes 100 per cent and no time left for a print that
// reached the end of its file. The firmware puts progress on a telemetry frame
// only while it is printing, so the last figure in the tree is the one from a
// second before the end. A stopped or failed print keeps that figure.
func (s *Session) reportTheWholePrint() {
	s.report(connectorapi.StatePatch{Path: telemetryPaths["progress"], Value: json.RawMessage("100")})
	s.report(connectorapi.StatePatch{Path: telemetryPaths["time_remaining"], Value: json.RawMessage("0")})
}

// forgetTheOutcome drops the word, from this session and from the state tree
// both, for a machine that has started a different print. A patch of null is
// how this dialect says a node is gone.
func (s *Session) forgetTheOutcome() {
	if s.jobOutcome.Load() == nil {
		return
	}

	s.jobOutcome.Store(nil)
	s.report(connectorapi.StatePatch{Path: jobOutcomePath, Value: json.RawMessage(dialogAbsent)})
}
