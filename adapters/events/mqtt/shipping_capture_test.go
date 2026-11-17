package mqtt

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file holds a capture of what the shipping server does that this
// adapter replaces. A capture is a committed data file with a test that fails when the code
// diverges from it. The fixture came off a running process, so the capture and
// its authority never share an origin.
//
// The job state file is the vocabulary a subscriber of the frozen per-job
// channel reads.
//
// The fixture had a second source while the shipping server existed, and the
// gate read both. That server is deleted, so the fixture is the record and the
// gate below holds it against this adapter, which is the side that can still
// drift.
const shippingJobStates = "testdata/shipping-job-states.tsv"

// The floor under the gate. Two empty sets agree.
const shippingJobStateFloor = 8

// TestThisAdapterAnswersWhatTheJobStateCaptureRecords is the divergence test on
// the job states. The fixture's last column is what connectJobState answers for
// the stored state in its first, so a mapping that moves fails here whichever
// side moved it. A `-` is a state this adapter publishes nothing for.
func TestThisAdapterAnswersWhatTheJobStateCaptureRecords(t *testing.T) {
	t.Parallel()

	rows := shippingCaptureRows(t, shippingJobStates)
	require.GreaterOrEqualf(t, len(rows), shippingJobStateFloor,
		"%s holds %d rows and the floor is %d", shippingJobStates, len(rows), shippingJobStateFloor)

	for _, row := range rows {
		require.Lenf(t, row, 5,
			"%s: every row is the stored state, two published vocabularies, terminal and this adapter's answer",
			shippingJobStates)

		answer := connectJobState(row[0])
		if answer == "" {
			answer = "-"
		}

		assert.Equalf(t, row[4], answer,
			"%s records %s for the stored state %s and this adapter answers %s",
			shippingJobStates, row[4], row[0], answer)
	}
}

// shippingCaptureRows reads one fixture beside this package into cells. A line
// opening with # is the header that names the run.
func shippingCaptureRows(t *testing.T, name string) [][]string {
	t.Helper()

	handle, err := os.Open(filepath.Clean(name))
	require.NoError(t, err)

	defer func() { require.NoError(t, handle.Close()) }()

	var rows [][]string

	scanner := bufio.NewScanner(handle)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line != "" && !strings.HasPrefix(line, "#") {
			rows = append(rows, strings.Split(line, "\t"))
		}
	}

	require.NoError(t, scanner.Err())

	return rows
}
