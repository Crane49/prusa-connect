package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// slowBody hands out chunks with a pause before each, the way a client on a
// slow link sends an upload. after, when set, runs before the given chunk.
type slowBody struct {
	chunks  [][]byte
	pause   time.Duration
	at      int
	after   func()
	afterAt int
}

func (b *slowBody) Read(p []byte) (int, error) {
	if len(b.chunks) == 0 {
		return 0, io.EOF
	}

	time.Sleep(b.pause)

	if b.after != nil && b.at == b.afterAt {
		b.after()
	}

	b.at++

	n := copy(p, b.chunks[0])
	b.chunks = b.chunks[1:]

	return n, nil
}

// storesWithBudget is one supervised filesystem storage whose per-call budget
// is the one given, far below the time the test's bodies take to arrive.
func storesWithBudget(t *testing.T, root string, budget time.Duration) stores {
	t.Helper()

	host := stageOne(t, root).host

	target, err := ref.New(ref.KindStorage, "bench")
	require.NoError(t, err)

	running, found := host.Instance(target)
	require.True(t, found)

	policy := running.Policy()
	policy.CallTimeout = budget
	require.NoError(t, running.SetPolicy(policy))

	return stores{host: host}
}

func slowChunks() [][]byte {
	return [][]byte{[]byte("G28\n"), []byte("G1 X10\n"), []byte("G1 Y10\n"), []byte("M84\n")}
}

// An upload is bounded by the caller and not by the call budget, because a
// large file on a slow link is not a ten second operation.
func TestASlowUploadOutlivesTheCallBudget(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		s := storesWithBudget(t, root, 50*time.Millisecond)

		body := &slowBody{chunks: slowChunks(), pause: 50 * time.Millisecond}

		written, err := s.Save(t.Context(), "bench", "slow.gcode", body)
		require.NoError(t, err)
		require.Equal(t, int64(len("G28\nG1 X10\nG1 Y10\nM84\n")), written.Size)

		on, err := os.ReadFile(filepath.Join(root, "slow.gcode"))
		require.NoError(t, err)
		require.Equal(t, "G28\nG1 X10\nG1 Y10\nM84\n", string(on))
	})
}

// The same upload under the call budget a deployment really runs with. It
// failed with 504 after ten seconds, and it now takes forty and lands whole.
func TestAnUploadLongerThanTheDefaultCallBudgetLands(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		s := storesWithBudget(t, root, supervisor.DefaultCallTimeout)

		body := &slowBody{chunks: slowChunks(), pause: supervisor.DefaultCallTimeout}
		began := time.Now()

		written, err := s.Save(t.Context(), "bench", "long.gcode", body)
		require.NoError(t, err)
		require.Greater(t, time.Since(began), 3*supervisor.DefaultCallTimeout,
			"the upload took no longer than the budget, so it proves nothing")
		require.Equal(t, int64(len("G28\nG1 X10\nG1 Y10\nM84\n")), written.Size)

		on, err := os.ReadFile(filepath.Join(root, "long.gcode"))
		require.NoError(t, err)
		require.Equal(t, "G28\nG1 X10\nG1 Y10\nM84\n", string(on))
	})
}

// Uploads that each send a byte a minute cannot take every slot of the
// storage, so a list made with the ordinary request budget still gets an
// answer while they run.
func TestSlowUploadsLeaveTheStorageToEveryoneElse(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		s := stores{host: stageOne(t, root).host}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		uploads := make(chan error, supervisor.DefaultConcurrency)

		for n := range supervisor.DefaultConcurrency {
			body := &slowBody{chunks: slowChunks(), pause: 59 * time.Second}

			go func() {
				_, err := s.Save(ctx, "bench", fmt.Sprintf("slow-%d.gcode", n), body)
				uploads <- err
			}()
		}

		time.Sleep(time.Second)

		listing, stop := context.WithTimeout(t.Context(), config.DefaultRequestTimeout)
		defer stop()

		_, err := s.List(listing, "bench", "")
		require.NoError(t, err, "the uploads took the slots a list needs")

		cancel()

		for range supervisor.DefaultConcurrency {
			<-uploads
		}
	})
}

// A caller that goes away part way through gets an error, and nothing is
// published under the name.
func TestAnUploadWhoseCallerLeavesPublishesNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		s := storesWithBudget(t, root, 50*time.Millisecond)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		body := &slowBody{chunks: slowChunks(), pause: 50 * time.Millisecond, after: cancel, afterAt: 2}

		_, err := s.Save(ctx, "bench", "gone.gcode", body)
		require.Error(t, err)

		_, err = s.Stat(t.Context(), "bench", "gone.gcode")
		require.Error(t, err)

		_, err = os.Stat(filepath.Join(root, "gone.gcode"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

// failingBody hands out one chunk and then fails, the way a request body does
// when the client drops part way.
type failingBody struct{ sent bool }

func (b *failingBody) Read(p []byte) (int, error) {
	if b.sent {
		return 0, io.ErrUnexpectedEOF
	}

	b.sent = true

	return copy(p, "G28\n"), nil
}

// A body that fails on the client's side is the caller's failure, not the
// storage's, so uploads that keep dropping do not open the storage's breaker.
func TestAClientThatDropsDoesNotOpenTheBreaker(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		s := storesWithBudget(t, root, supervisor.DefaultCallTimeout)

		for range 2 * supervisor.DefaultBreakerMinRequests {
			_, err := s.Save(t.Context(), "bench", "dropped.gcode", &failingBody{})
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		}

		_, err := s.List(t.Context(), "bench", "")
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(root, "dropped.gcode"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

// ctxWriter records the state of the context its Abort ran under.
type ctxWriter struct{ abortedUnder error }

func (*ctxWriter) Write(p []byte) (int, error) { return len(p), nil }

func (*ctxWriter) Commit(context.Context) (storageapi.WriteResult, error) {
	return storageapi.WriteResult{}, nil
}

func (w *ctxWriter) Abort(ctx context.Context) error {
	w.abortedUnder = ctx.Err()

	return nil
}

// A write whose caller has gone is still aborted under a live context, so a
// remote store removes what it staged.
func TestAnAbortOutlivesTheCallersContext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := storesWithBudget(t, t.TempDir(), supervisor.DefaultCallTimeout)

		running, err := s.provider("bench")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		writer := &ctxWriter{}
		require.NoError(t, abort(ctx, running, writer))
		require.NoError(t, writer.abortedUnder)
	})
}
