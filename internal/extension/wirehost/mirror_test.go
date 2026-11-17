package wirehost

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// Standard error is the only place a child's own messages appear, because
// writing them to standard output would corrupt the wire. Losing them leaves
// nothing to diagnose that child with.
//
// These cases call mirror directly and not through Launch. A line over the
// bound needs a child that writes one, and the child the other cases run writes
// short lines.

// captureLines runs mirror over what a child wrote and returns the log.
func captureLines(t *testing.T, stderr string) string {
	t.Helper()

	var logged bytes.Buffer

	mirror(
		context.Background(),
		slog.New(slog.NewTextHandler(&logged, nil)),
		"blob-example",
		strings.NewReader(stderr),
	)

	return logged.String()
}

// TestALineOverTheBoundDoesNotEndTheCopy covers the defect this file exists
// for. A bufio.Scanner bounded at logLineBytes returns false on a longer line
// and never scans again, so one long line would cost every later line from that
// child, for as long as it runs.
func TestALineOverTheBoundDoesNotEndTheCopy(t *testing.T) {
	t.Parallel()

	const after = "the line after the long one"

	logged := captureLines(t, strings.Repeat("x", logLineBytes+1)+"\n"+after+"\n")

	if !strings.Contains(logged, after) {
		t.Errorf("a line of %d bytes ended the copy, so %q never reached the log:\n%s",
			logLineBytes+1, after, logged)
	}
}

// TestALineOverTheBoundIsSplitAndNotDropped covers the other half. Reaching the
// line after the long one is worth nothing if the long line itself is gone, and
// a reader has to be able to tell a fragment from a line the child ended.
func TestALineOverTheBoundIsSplitAndNotDropped(t *testing.T) {
	t.Parallel()

	head := strings.Repeat("a", logLineBytes)
	tail := "the rest of the same line"

	logged := captureLines(t, head+tail+"\n")

	for _, want := range []string{head, tail} {
		if !strings.Contains(logged, want) {
			t.Errorf("%d bytes of the long line are missing from the log", len(want))
		}
	}

	if !strings.Contains(logged, splitAttribute+"=true") {
		t.Errorf("no record says it is a fragment, so a reader takes two records for two lines:\n%s",
			logged[:min(len(logged), 400)])
	}
}

// TestAWholeLineIsOneRecordAndSaysNothingAboutSplitting is the other direction.
// A test that only checks the long line is split would also pass against an
// implementation that splits every line.
func TestAWholeLineIsOneRecordAndSaysNothingAboutSplitting(t *testing.T) {
	t.Parallel()

	logged := captureLines(t, "first line\r\nsecond line\n")

	if got := strings.Count(logged, "msg="); got != 2 {
		t.Errorf("two whole lines produced %d records:\n%s", got, logged)
	}

	if strings.Contains(logged, splitAttribute) {
		t.Errorf("a whole line was reported as a fragment:\n%s", logged)
	}

	// Without the trim, a record from a Windows child ends its message text
	// with a carriage return.
	if strings.Contains(logged, "\\r") {
		t.Errorf("a CRLF line kept its carriage return:\n%s", logged)
	}
}

// TestALastLineWithNoNewlineStillReaches covers what a child killed mid-line
// leaves behind. That line is usually the one that says why.
func TestALastLineWithNoNewlineStillReaches(t *testing.T) {
	t.Parallel()

	logged := captureLines(t, "panic: the child stopped here")

	if !strings.Contains(logged, "the child stopped here") {
		t.Errorf("the last line was dropped for having no newline:\n%s", logged)
	}
}

// TestAChildThatWroteNothingLogsNothing keeps the empty record out. A reader
// scanning for a blank message would otherwise find one for every child that
// exits without writing anything.
func TestAChildThatWroteNothingLogsNothing(t *testing.T) {
	t.Parallel()

	if logged := captureLines(t, ""); logged != "" {
		t.Errorf("a child that wrote nothing produced a record:\n%s", logged)
	}
}

// TestMirrorWithNoLoggerStillDrainsTheChild covers the branch a host without a
// logger takes. A child whose standard error is never read blocks on the pipe
// buffer and stops answering the wire.
func TestMirrorWithNoLoggerStillDrainsTheChild(t *testing.T) {
	t.Parallel()

	stderr := strings.NewReader(strings.Repeat("noise\n", 1000))

	mirror(context.Background(), nil, "blob-example", stderr)

	if left := stderr.Len(); left != 0 {
		t.Errorf("%d bytes were left unread, so the child would block on its own pipe", left)
	}
}
