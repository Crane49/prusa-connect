package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
)

var errRefused = errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")

// quickLadder keeps the cases below in milliseconds.
var quickLadder = supervisor.BackoffPolicy{Base: time.Millisecond, Cap: 2 * time.Millisecond}

// TestAServerWaitsForADatabaseThatIsStillStarting is a server started together
// with its database, which refuses the first connections and then answers.
func TestAServerWaitsForADatabaseThatIsStillStarting(t *testing.T) {
	t.Parallel()

	asked := 0
	ping := func(context.Context) error {
		asked++
		if asked < 3 {
			return errRefused
		}

		return nil
	}

	err := awaitDatabase(t.Context(), ping, "postgres://db/pcl", discard(), quickLadder, time.Minute)

	require.NoError(t, err)
	require.Equal(t, 3, asked)
}

// TestAServerGivesUpOnADatabaseThatNeverAnswers ends with the last refusal,
// so the reason is in the error and not only in the log above it.
func TestAServerGivesUpOnADatabaseThatNeverAnswers(t *testing.T) {
	t.Parallel()

	ping := func(context.Context) error { return errRefused }

	err := awaitDatabase(t.Context(), ping, "postgres://db/pcl", discard(), quickLadder, 20*time.Millisecond)

	require.ErrorIs(t, err, errRefused)
	require.ErrorContains(t, err, "postgres://db/pcl")
}

// TestAStoppedServerStopsWaitingForTheDatabase is a SIGTERM during the wait,
// which must end the start at once rather than after the limit.
func TestAStoppedServerStopsWaitingForTheDatabase(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	ping := func(context.Context) error {
		cancel()

		return errRefused
	}

	err := awaitDatabase(ctx, ping, "postgres://db/pcl", discard(), quickLadder, time.Minute)

	require.ErrorIs(t, err, context.Canceled)
}
