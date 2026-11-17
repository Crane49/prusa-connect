package deviceapi

import "encoding/json"

// Issue is one thing the host has found wrong with a device.
type Issue struct {
	// Reason is the token a module branches on. The set is closed and listed
	// in the device-issues vocabulary.
	Reason string `json:"reason"`
	// Severity is info, warning or error. The reason determines it.
	Severity string `json:"severity"`
	// Detail is the machine's own words, such as the title of a dialog. Show
	// it to a person. Do not branch on it.
	Detail string `json:"detail,omitempty"`
}

// Dialog is what a device shows on its screen while it waits for a person, as
// the device sent it. A module that reacts to one problem branches on Code.
type Dialog struct {
	// ID is the dialog_id argument of core.command.dialog_action, as the
	// device encoded it. Pass it back unchanged to answer the dialog.
	ID json.RawMessage `json:"id"`
	// Code is the firmware's code for the dialog, as text, and empty when
	// the device sent none.
	Code string `json:"code,omitempty"`
	// Title and Text are the heading and the body the screen shows.
	Title string `json:"title,omitempty"`
	Text  string `json:"text,omitempty"`
	// Buttons are the labels of the buttons, in the order the screen shows
	// them. core.command.dialog_action takes one of them as button.
	Buttons []string `json:"buttons,omitempty"`
}
