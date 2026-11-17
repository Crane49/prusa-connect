package app

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// A module asking for one name after another writes one line each interval,
// and the line that opens the next one says how many asks went unwritten. A
// line per ask would fill an operator's disk, and no line at all would leave
// the operator with nothing: the answer the module reads names nothing.
func TestAModulesRefusalsReachTheLogOnceAnInterval(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		written := &bytes.Buffer{}
		logs := newRefusalLog(slog.New(slog.NewTextHandler(written, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})))

		raised := errors.New("no storage of that name is serving")

		for range 3 {
			logs.refused(t.Context(), "a module was not given a file", "sink:filament", raised)
		}

		require.Equal(t, 1, strings.Count(written.String(), "sink:filament"),
			"an enumeration loop wrote a line per ask")

		// Another module is another line: the limit is per module, so a noisy
		// one cannot hide a quiet one.
		logs.refused(t.Context(), "a module was not given a file", "sink:queue", raised)
		require.Contains(t, written.String(), "sink:queue")

		time.Sleep(refusalLogInterval + time.Second)

		logs.refused(t.Context(), "a module was not given a file", "sink:filament", raised)
		require.Contains(t, written.String(), "unwritten_refusals=2",
			"the line opening the interval does not say what it stands for")
	})
}
