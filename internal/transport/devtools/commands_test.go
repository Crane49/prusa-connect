package devtools_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/transport/devtools"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func entryFor(printer, verb string) devtools.Command {
	return devtools.Command{At: time.Now().UTC(), DeviceID: printer, Verb: verb, OK: true}
}

// TestOnePrintersWordsCannotGrowTheLogWithoutBound is the other half of the
// bound below.
//
// The entry count bounds how many commands the log keeps and says nothing about
// what one of them costs. A refusal reason is text the printer chose, and this
// dialect admits a frame of one mebibyte, so an unbounded reason makes a full
// log of refusals cost 256 mebibytes: measured at exactly that.
func TestOnePrintersWordsCannotGrowTheLogWithoutBound(t *testing.T) {
	t.Parallel()

	log := devtools.NewCommandLog(0)

	// What one frame of this dialect can carry.
	reason := strings.Repeat("A", 1<<20)

	for range devtools.DefaultCommandLimit {
		entry := entryFor("p1", "core.command.start_print")
		entry.Outcome(false, reason, nil, nil, nil)
		log.Record(entry)
	}

	entries, _ := log.Recent("p1")
	require.Len(t, entries, devtools.DefaultCommandLimit)

	retained := 0
	for _, entry := range entries {
		retained += len(entry.MachineReason)
	}

	t.Logf("observed: %d entries retaining %d bytes of the printer's own words", len(entries), retained)

	require.LessOrEqual(t, retained, devtools.DefaultCommandLimit*(machinetext.MaxBytes+4),
		"a printer can grow this log by as much as it likes")
}

// TestAClippedReasonIsStillText keeps the bound from cutting a character in
// half. A refusal a firmware wrote in Czech is the ordinary case here, and a
// half rune reaches a JSON encoder as the replacement character.
//
// The width of the character is the whole of the case. A two-byte rune divides
// the bound, so a cut that counted bytes and ignored runes lands on a boundary
// anyway and a case written with one passes against it. The euro sign is three
// bytes, which the bound does not divide, and it is what reaches the cut.
func TestAClippedReasonIsStillText(t *testing.T) {
	t.Parallel()

	for _, character := range []string{"ř", "€"} {
		t.Run(character, func(t *testing.T) {
			t.Parallel()

			// One rune per byte of the bound, so every case is over it whatever
			// the character costs.
			entry := devtools.Command{}
			entry.Outcome(false, strings.Repeat(character, machinetext.MaxBytes), nil, nil, nil)

			t.Logf("observed: %d bytes of a %d-byte character survived the bound",
				len(entry.MachineReason), len(character))

			require.True(t, utf8.ValidString(entry.MachineReason),
				"the bound cut a character in half")
			require.LessOrEqual(t, len(entry.MachineReason), machinetext.MaxBytes+4)
			require.Contains(t, entry.MachineReason, character)
		})
	}
}

// TestTheCommandLogKeepsTheNewestAndSaysWhatItDropped is the bound that makes
// this safe to leave running. A log that grew would be a developer surface that
// costs a long-lived process memory, and one that dropped silently would read as
// a printer that was never commanded.
func TestTheCommandLogKeepsTheNewestAndSaysWhatItDropped(t *testing.T) {
	t.Parallel()

	const limit = 4

	log := devtools.NewCommandLog(limit)

	for i := range 10 {
		log.Record(entryFor("p1", fmt.Sprintf("verb-%d", i)))
	}

	entries, dropped := log.Recent("p1")

	require.Len(t, entries, limit)
	require.Equal(t, uint64(6), dropped)
	require.Equal(t, "verb-6", entries[0].Verb, "the oldest kept entry")
	require.Equal(t, "verb-9", entries[limit-1].Verb, "the newest, last")
}

func TestTheCommandLogAnswersForOnePrinterOnly(t *testing.T) {
	t.Parallel()

	log := devtools.NewCommandLog(0)

	log.Record(entryFor("p1", "stop"))
	log.Record(entryFor("p2", "pause"))
	log.Record(entryFor("p1", "resume"))

	mine, _ := log.Recent("p1")
	require.Len(t, mine, 2)
	require.Equal(t, "stop", mine[0].Verb)
	require.Equal(t, "resume", mine[1].Verb)

	theirs, _ := log.Recent("p2")
	require.Len(t, theirs, 1)

	nobody, _ := log.Recent("p3")
	require.Empty(t, nobody)
}

// TestTheCommandLogCopiesWhatItRecordsAndWhatItAnswers holds the record still.
// The caller's argument map outlives the call, and a handler is free to sort or
// annotate what it was handed; either writing through the other is a history
// that changes after the fact.
func TestTheCommandLogCopiesWhatItRecordsAndWhatItAnswers(t *testing.T) {
	t.Parallel()

	log := devtools.NewCommandLog(0)

	args := map[string]string{"path": "/usb/a.bgcode"}
	log.Record(devtools.Command{DeviceID: "p1", Verb: "start_print", Args: args})

	args["path"] = "/usb/rewritten.bgcode"

	first, _ := log.Recent("p1")
	require.Equal(t, "/usb/a.bgcode", first[0].Args["path"], "the caller wrote through into the record")

	first[0].Args["path"] = "/usb/again.bgcode"

	second, _ := log.Recent("p1")
	require.Equal(t, "/usb/a.bgcode", second[0].Args["path"], "a reader wrote through into the record")
}

func TestAnOutcomeRendersWhatTheCallerWasTold(t *testing.T) {
	t.Parallel()

	t.Run("a result", func(t *testing.T) {
		t.Parallel()

		entry := devtools.Command{}
		entry.Outcome(true, "", []string{"speed"}, map[string]string{"path": "/usb/a"}, nil)

		require.True(t, entry.OK)
		require.Equal(t, []string{"speed"}, entry.Ignored)
		require.Equal(t, map[string]string{"path": "/usb/a"}, entry.Substituted)
		require.Empty(t, entry.Failure)
		require.Empty(t, entry.FaultCode)
	})

	t.Run("a refusal the printer explained", func(t *testing.T) {
		t.Parallel()

		entry := devtools.Command{}
		entry.Outcome(false, "MEDIA_NOT_INSERTED", nil, nil, nil)

		require.False(t, entry.OK)
		require.Equal(t, "MEDIA_NOT_INSERTED", entry.MachineReason)
		require.Empty(t, entry.FaultCode, "the printer's own word was recorded as one of this API's codes")
		require.Empty(t, entry.Failure)
	})

	t.Run("a registered refusal", func(t *testing.T) {
		t.Parallel()

		refusal := fault.PrinterNotFound.New("no connected printer answers to that id")

		entry := devtools.Command{}
		entry.Outcome(false, "", nil, nil, fmt.Errorf("send the command: %w", refusal))

		require.Equal(t, fault.PrinterNotFound.Code().String(), entry.FaultCode)
		require.Equal(t, "no connected printer answers to that id", entry.Failure)
		require.False(t, entry.OK)
	})

	t.Run("an error carrying no code", func(t *testing.T) {
		t.Parallel()

		entry := devtools.Command{}
		entry.Outcome(false, "", nil, nil, fmt.Errorf("open /var/lib/secret.db: %w", errFactory))

		require.Empty(t, entry.FaultCode)
		require.NotContains(t, entry.Failure, "/var/lib/secret.db")
		require.NotContains(t, entry.Failure, errFactory.Error())
		require.NotEmpty(t, entry.Failure)
	})
}
