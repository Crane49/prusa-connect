package xbuddy

import (
	"errors"
	"testing"
)

// M997 copies at most twelve characters after "/usb/" as the name of a file in
// the root of the stick, so a path it would cut short never leaves.
func TestAFlashNamesAShortNameInTheRootOfTheStick(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, path, want string
	}{
		{name: "a short name in the root", path: "/usb/COREON~1.BBF", want: "M997 /usb/COREON~1.BBF"},
		{name: "no path", path: "", want: "M997"},
		{name: "the root of the stick", path: "/usb", want: "M997"},
		{name: "a file in a folder", path: "/usb/FW/COREON~1.BBF"},
		{name: "a long name", path: "/usb/COREONE_firmware_8.0.0.bbf"},
		{name: "another storage", path: "/internal/COREON~1.BBF"},
		{name: "a module number in the name", path: "/usb/XLS5~1.BBF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code, err := gcodeFlash(arguments{values: map[string]string{"path": tc.path}})
			if tc.want == "" {
				if !errors.Is(err, ErrArgumentUnusable) {
					t.Fatalf("%q rendered %q, %v, and M997 cannot carry it", tc.path, code, err)
				}

				return
			}

			if err != nil || code != tc.want {
				t.Errorf("%q renders %q, %v, want %q", tc.path, code, err, tc.want)
			}
		})
	}
}

// A state the printer reported before it reconnected is not taken as its state
// now: it may have started a print from its own screen while it was away.
func TestAFlashWaitsForAStateFromTheCurrentConnection(t *testing.T) {
	t.Parallel()

	idle := stateIdle

	session := &Session{}
	session.machineState.Store(&idle)

	if err := session.refuseInState(VerbFlash); err == nil {
		t.Error("a flash went out on a state from the connection before")
	}

	session.stateHeard.Store(true)

	if err := session.refuseInState(VerbFlash); err != nil {
		t.Errorf("a flash to a printer that reported IDLE on this connection was refused: %v", err)
	}
}
