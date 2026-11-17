package simulator

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A person at a bench sends a line and reads what came back. These are the
// answers they get.
func TestTheMachineAnswersAGCodeLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		line     string
		printing bool
		want     string
	}{
		{
			name: "a line it carried out",
			line: "G1 Z5 F600",
			want: "ok",
		},
		{
			name: "the temperatures of a machine with nothing on the bed",
			line: "M105",
			want: "ok T:25.0 /0.0 B:25.0 /0.0",
		},
		{
			name:     "the temperatures of a machine that is printing",
			line:     "M105",
			printing: true,
			want:     "ok T:215.0 /215.0 B:60.0 /60.0",
		},
		{
			name: "a word in lower case is the same word",
			line: "m105",
			want: "ok T:25.0 /0.0 B:25.0 /0.0",
		},
		{
			name: "a line with no arguments",
			line: "M84",
			want: "ok",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			printer, err := New(Config{Model: "MK4"})
			require.NoError(t, err)

			if testCase.printing {
				printer.state = statePrinting
			}

			require.Equal(t, testCase.want, printer.gcodeAnswer([]byte(testCase.line)))
		})
	}
}

// The position follows the print, because a head that is always at the origin
// tells a person watching a job nothing they did not already know.
func TestThePositionReportFollowsThePrint(t *testing.T) {
	t.Parallel()

	printer, err := New(Config{Model: "MK4"})
	require.NoError(t, err)

	idle := printer.gcodeAnswer([]byte("M114"))
	require.Contains(t, idle, "Z:0.00")
	require.True(t, strings.HasSuffix(idle, "\nok"),
		"the serial port prints ok on the line after the position")

	printer.mu.Lock()
	printer.job.printed = 17
	printer.mu.Unlock()

	moved := printer.gcodeAnswer([]byte("M114"))
	require.Contains(t, moved, "Z:3.40", "seventeen layers of 0.2 mm is 3.4 mm")
	require.NotEqual(t, idle, moved)
}

// M701 carries the filament name as S"<name>", and a test reads the name the
// last M701 carried to see that it arrived. A bare M701 carries no name, and
// the machine then asks for the type on its screen.
func TestTheMachineRecordsTheFilamentM701Names(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
		want string
	}{
		{name: "a name", line: `M701 S"PETG"`, want: "PETG"},
		{name: "a word in lower case is the same word", line: `m701 S"PLA"`, want: "PLA"},
		{name: "no name", line: "M701", want: ""},
		{name: "a name with no closing quote", line: `M701 S"PETG`, want: ""},
		{name: "another parameter before the name", line: `M701 T0 S"ASA"`, want: "ASA"},
		{name: "the preheat parameter after the name", line: `M701 S"PLA" W2`, want: "PLA"},
		{name: "the preheat parameter with no name", line: "M701 W2", want: ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			printer, err := New(Config{Model: "MK4"})
			require.NoError(t, err)

			_, loaded := printer.FilamentToLoad()
			require.False(t, loaded, "a machine that was sent no M701 reports none")

			require.Equal(t, "ok", printer.gcodeAnswer([]byte(testCase.line)))

			name, loaded := printer.FilamentToLoad()
			require.True(t, loaded)
			require.Equal(t, testCase.want, name)
		})
	}
}
