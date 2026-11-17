package local

import (
	"context"
	"runtime/metrics"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
)

// returns records when a memory returner gave memory back.
type returns struct {
	mu sync.Mutex
	at []time.Time
}

func (r *returns) give() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.at = append(r.at, time.Now())
}

func (r *returns) times() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]time.Time(nil), r.at...)
}

// TestABurstOfPassesReturnsMemoryOncePerInterval holds the throttle. Ten
// passes end 100 ms apart. The first one returns memory at once, the other
// nine share one return a second later, and that return comes after the last
// pass, so the burst leaves nothing behind.
func TestABurstOfPassesReturnsMemoryOncePerInterval(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var got returns

		returner := newMemoryReturner(ReturnMemoryEvery, got.give)
		start := time.Now()

		var lastPass time.Time

		for range 10 {
			lastPass = time.Now()

			returner.request()
			time.Sleep(100 * time.Millisecond)
		}

		time.Sleep(3 * ReturnMemoryEvery)
		synctest.Wait()

		require.Equal(t, []time.Time{start, start.Add(ReturnMemoryEvery)}, got.times(),
			"ten passes in one second should return memory at the first pass and once a second later")
		require.False(t, got.times()[1].Before(lastPass), "no return came after the last pass")
	})
}

// TestEveryPathThatHashesReturnsMemory holds the wiring. A setup, a login, a
// login for an unknown name, a password change and an administrator's reset
// each run an argon2id pass, and each is followed by one return.
func TestEveryPathThatHashesReturnsMemory(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		hasher, err := auth.NewHasher(auth.Cost{MemoryKiB: 7 * 1024, Passes: 5})
		require.NoError(t, err)

		var got returns

		accounts := New(NewMemoryStore(), nil, WithHasher(hasher))
		accounts.memory = newMemoryReturner(ReturnMemoryEvery, got.give)

		ctx := context.Background()
		password := []byte("correct-horse-battery")

		paths := []struct {
			name string
			run  func() error
		}{
			{"setup", func() error {
				_, createErr := accounts.CreateFirst(ctx, "admin", NewAccount{Username: "admin", Password: password})

				return createErr
			}},
			{"login", func() error {
				_, loginErr := accounts.VerifyCredentials(ctx, "admin", string(password))

				return loginErr
			}},
			{"unknown name", func() error {
				_, loginErr := accounts.VerifyCredentials(ctx, "nobody", string(password))
				require.Error(t, loginErr)

				return nil
			}},
			{"change", func() error {
				return accounts.ChangePassword(ctx, "admin", password, []byte("another-horse-battery"))
			}},
			{"reset", func() error {
				return accounts.SetPassword(ctx, "admin", password)
			}},
		}

		for i, path := range paths {
			require.NoError(t, path.run(), path.name)

			time.Sleep(ReturnMemoryEvery)
			synctest.Wait()

			require.Len(t, got.times(), i+1, "the %s path did not return memory", path.name)
		}
	})
}

// TestAReturnGivesTheHashBlockBack checks the real effect with the default
// returner. After a pass and its return, the heap Go keeps from the operating
// system is smaller than one argon2id block, so the block went back.
//
// It does not run in parallel. Another test allocating at the same time would
// add to the heap it reads.
//
//nolint:paralleltest // it reads the heap of the whole test process.
func TestAReturnGivesTheHashBlockBack(t *testing.T) {
	accounts := New(NewMemoryStore(), nil)
	block := uint64(accounts.hasher.Cost().MemoryKiB) << 10

	done := make(chan struct{})
	give := accounts.memory.give
	accounts.memory.give = sync.OnceFunc(func() {
		give()
		close(done)
	})

	_, err := accounts.CreateFirst(t.Context(), "admin", NewAccount{
		Username: "admin", Password: []byte("correct-horse-battery"),
	})
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("no return came after the pass")
	}

	kept := metrics.Sample{Name: "/memory/classes/heap/objects:bytes"}
	unused := metrics.Sample{Name: "/memory/classes/heap/unused:bytes"}
	free := metrics.Sample{Name: "/memory/classes/heap/free:bytes"}
	samples := []metrics.Sample{kept, unused, free}
	metrics.Read(samples)

	held := samples[0].Value.Uint64() + samples[1].Value.Uint64() + samples[2].Value.Uint64()
	require.Less(t, held, block,
		"Go still keeps %d bytes of heap after the return, more than the %d byte block of one pass", held, block)
}
