package simulator

import (
	"cmp"
	"slices"
	"strconv"
)

// The two states a dialog is up in. A printer that leaves either has closed
// the dialog. That is the only signal, because the firmware sends no message
// that says so.
const (
	stateAttention = "ATTENTION"
	stateError     = "ERROR"
)

// Dialog is a question on the printer's display, waiting on a person.
//
// The machine repeats its id in telemetry for as long as it is up and says what
// it asks only in the STATE_CHANGED event that raised it, so a host that missed
// that event knows a dialog is waiting and not what it says.
type Dialog struct {
	// ID is what core.command.dialog_action names. The printer assigns it.
	ID int64
	// Code is the firmware's own number for the condition, as a string.
	Code string
	// Title and Text are what is on the display.
	Title string
	Text  string
	// Buttons are the answers the display offers, and the only values
	// DIALOG_ACTION accepts.
	Buttons []string
	// State is the state the dialog is raised in. It is ATTENTION unless the
	// caller names ERROR.
	State string
}

// RaiseDialog puts a question on the display and moves the printer into the
// state that dialog is up in. The event carrying it reaches the host the way
// firmware sends one: a STATE_CHANGED naming the id and what it asks.
//
// It reports whether the frame reached the wire. A printer with no session has
// nowhere to send it, and the dialog is still raised, so the host learns of it
// from the next telemetry frame the way a reconnecting one does.
func (p *Printer) RaiseDialog(dialog Dialog) bool {
	// A dialog raised by hand replaces one an M334 was waiting on, and
	// nothing waits on it.
	return p.raise(dialog, nil)
}

// raise puts the dialog and what waits on its answer in place under one lock,
// so a dialog raised at the same moment never takes the other one's answer.
func (p *Printer) raise(dialog Dialog, then func(button string)) bool {
	if dialog.State != stateError {
		dialog.State = stateAttention
	}

	p.mu.Lock()
	// A dialog that replaces another keeps the state from before the first,
	// because ATTENTION is not a state to return to.
	if p.dialog == nil {
		p.beforeDialog = p.state
	}

	p.dialog = &dialog
	p.dialogAnswered = then
	p.state = dialog.State
	conn := p.conn
	p.mu.Unlock()

	if conn == nil {
		return false
	}

	id := dialog.ID
	buttons := make([]any, 0, len(dialog.Buttons))

	for _, button := range dialog.Buttons {
		buttons = append(buttons, button)
	}

	return p.send(conn, event{
		Event:    eventStateChanged,
		State:    dialog.State,
		DialogID: &id,
		Data: map[string]any{
			"code":    dialog.Code,
			"title":   dialog.Title,
			"text":    dialog.Text,
			"buttons": buttons,
		},
	}) == nil
}

// Dialog reports the question the printer is waiting on, or nil when there is
// none.
func (p *Printer) Dialog() *Dialog {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.dialog == nil {
		return nil
	}

	held := *p.dialog

	return &held
}

// answerDialog closes the dialog on the display and reports why it could not
// be, in the firmware's own three refusals: there is no dialog, the id names a
// different one, or the display does not offer that button
// (marlin_printer.cpp).
//
// The printer returns to the state it was in before the dialog, so a print
// that a dialog stopped goes on printing.
func (p *Printer) answerDialog(kwargs map[string]any) (reason string, answered bool) {
	id, ok := kwargs["dialog_id"]
	if !ok {
		return ReasonMissingArgument, false
	}

	button, _ := kwargs["button"].(string)

	reason, then, answered := p.closeDialog(id, button)
	if answered && then != nil {
		then(button)
	}

	return reason, answered
}

// raiseAnswered raises a dialog whose answer the printer acts on, as M334
// does: the change waits for the button a person presses.
func (p *Printer) raiseAnswered(dialog Dialog, then func(button string)) {
	p.raise(dialog, then)
}

// closeDialog takes the dialog off the display, and answers what waited on
// it so the caller runs it outside the lock.
func (p *Printer) closeDialog(id any, button string) (string, func(string), bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.dialog == nil {
		return ReasonNoDialog, nil, false
	}

	if strconv.FormatInt(p.dialog.ID, 10) != dialogIDText(id) {
		return ReasonInvalidDialogID, nil, false
	}

	if !slices.Contains(p.dialog.Buttons, button) {
		return ReasonInvalidButton, nil, false
	}

	then := p.dialogAnswered

	// A state that moved while the dialog was up, such as SET_READY, is the
	// newer word and stays.
	if p.state == p.dialog.State {
		p.state = cmp.Or(p.beforeDialog, stateIdle)
	}

	p.dialog = nil
	p.dialogAnswered = nil
	p.beforeDialog = ""

	return "", then, true
}

// dialogIDText renders the id a caller sent so it compares to the printer's own
// whatever JSON made of it. A number arrives as a float64 and the shipping
// connector sends the same value as a string, so both spellings name one
// dialog.
func dialogIDText(value any) string {
	if number, isNumber := value.(float64); isNumber && number == float64(int64(number)) {
		return strconv.FormatInt(int64(number), 10)
	}

	text, isText := value.(string)
	if !isText {
		return ""
	}

	return text
}
