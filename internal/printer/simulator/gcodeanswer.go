package simulator

import (
	"fmt"
	"strings"
)

// AnswerKey is where a G-code answer travels on the ACCEPTED and FINISHED
// events: the key under `data` this simulator writes the machine's reply to.
//
// **Real firmware sends no such key.** A Buddy board answers a G frame with
// ACCEPTED and then FINISHED and nothing else, as
// docs/src/content/docs/prusa-xbuddy-websocket-protocol.md records. The reply
// a G-code line produces goes to the machine's serial port, and no recording
// shows it coming back over the WebSocket.
//
// Here the simulator gives more than the printer does. A person at a bench
// sends M105 to read a temperature, and a dev page that could only say
// "accepted" would give them no reading. A host reads the key when it is there
// and shows nothing when it is not, as with a real printer.
const AnswerKey = "answer"

// The G-code words this simulator answers with more than "ok". Everything else
// is answered "ok", which is what a machine says to a line it carried out.
const (
	gcodeTemperatureReport = "M105"
	gcodePositionReport    = "M114"
	gcodeLoadFilament      = "M701"
	gcodeFlash             = "M997"
)

// gcodeOK is the answer to a line the machine ran and had nothing to report
// about.
const gcodeOK = "ok"

// The numbers the position report is drawn from. The head moves with the
// print, so a person who sends M114 twice during a job sees two positions.
const (
	// zStepsPerMM turns the height into the stepper count Marlin prints beside
	// it. It is a plausible figure for a lead-screw Z and nothing reads it as a
	// measurement.
	zStepsPerMM = 400
	// bedSpan bounds the X and Y the head is reported at, so the numbers stay
	// inside a bed for the whole print.
	bedSpan = 200
	// yStride is how much faster Y travels than X in the reported position.
	// Any value but one keeps the head off a plain diagonal.
	yStride = 3
)

// gcodeAnswer is what the machine says back to one G-code line.
//
// The two lines a person at a bench sends most are the two that carry a
// reading: M105 for the temperatures and M114 for the position. Both are
// answered from the values telemetry reports, so somebody who sends M105 and
// then reads the state tree sees the same values.
//
// Every other line is answered "ok". A made-up reply for a word whose real
// answer nobody recorded would look like evidence.
func (p *Printer) gcodeAnswer(line []byte) string {
	word, rest, _ := strings.Cut(strings.TrimSpace(string(line)), " ")

	p.mu.Lock()
	defer p.mu.Unlock()

	p.endBusyLocked()

	switch strings.ToUpper(word) {
	case gcodeTemperatureReport:
		return p.temperatureReportLocked()
	case gcodePositionReport:
		return p.positionReportLocked()
	case gcodeLoadFilament:
		p.filamentToLoad = filamentNamed(rest)
		p.filamentAsked = true

		return gcodeOK
	case gcodeFlash:
		p.flashed = strings.TrimSpace(rest)
		p.flashAsked = true

		return gcodeOK
	default:
		return gcodeOK
	}
}

// temperatureReportLocked is M105's answer: the nozzle and the bed, each with
// the target beside it, in Marlin's format. The caller holds the lock.
func (p *Printer) temperatureReportLocked() string {
	if p.state == statePrinting || p.state == statePaused {
		return fmt.Sprintf("ok T:%.1f /%.1f B:%.1f /%.1f",
			printingNozzleTemp, printingNozzleTemp, printingBedTemp, printingBedTemp)
	}

	return fmt.Sprintf("ok T:%.1f /0.0 B:%.1f /0.0", ambientTemp, ambientTemp)
}

// positionReportLocked is M114's answer: where the head is, with the stepper
// counts beside it. Z follows the print, exactly as the axis_z telemetry field
// does. The caller holds the lock.
//
// It is two lines because that is what the serial port prints: the position,
// then "ok" on its own line.
func (p *Printer) positionReportLocked() string {
	height := layerHeight * float64(p.job.printed)

	return fmt.Sprintf("X:%.2f Y:%.2f Z:%.2f E:0.00 Count X:0 Y:0 Z:%d\nok",
		float64(p.job.printed%bedSpan), float64(p.job.printed*yStride%bedSpan),
		height, int(height*zStepsPerMM))
}

// filamentNamed reads the filament name M701 carries as S"<name>". It returns
// the empty string when the line names no filament, or when the name has no
// closing quote, because the firmware then loads with no name as well.
func filamentNamed(parameters string) string {
	_, quoted, found := strings.Cut(parameters, `S"`)
	if !found {
		return ""
	}

	name, _, closed := strings.Cut(quoted, `"`)
	if !closed {
		return ""
	}

	return name
}

// FilamentToLoad is the filament name the last M701 carried. loaded is false
// when the host has sent no M701, and name is empty when the last one named no
// filament, which makes a real machine ask for the type on its screen.
func (p *Printer) FilamentToLoad() (name string, loaded bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.filamentToLoad, p.filamentAsked
}

// isFlash reports whether a G-code line is M997.
func isFlash(line []byte) bool {
	word, _, _ := strings.Cut(strings.TrimSpace(string(line)), " ")

	return strings.EqualFold(word, gcodeFlash)
}

// Flashed is the path the last M997 named. asked is false when the host has sent
// no M997, and path is empty when the last one named no file, which makes a real
// machine flash the newest firmware on the stick when it restarts.
//
// The simulator does not restart for it, as it does not for M999.
func (p *Printer) Flashed() (path string, asked bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.flashed, p.flashAsked
}
