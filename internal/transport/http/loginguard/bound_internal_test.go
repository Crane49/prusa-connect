package loginguard

import (
	"context"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The keys a caller picks. A username is whatever they put in the body, and
// behind a declared proxy the address is whatever they put in the header, so
// both tables are grown by a caller with no credential and neither may grow
// without a bound.
const (
	// tableBound is the small cap these tests run under, so the flood is a
	// number a test can send rather than a number an attacker can.
	tableBound = 32
	// flood is how many distinct keys are sent at it.
	flood = 5000
	// unqueued is an address allowance no test here spends, for a test about
	// the account table that sends everything from one address.
	unqueued = 2 * flood
)

// nothingIsHeld is the wait with the waiting taken out, for a test that is
// about what is stored rather than about what it costs.
func nothingIsHeld(context.Context, time.Duration) error { return nil }

// TestACallerVaryingTheUsernameCannotGrowTheAccountTable is the memory bound
// d7f6baf's own fix was about, kept from reappearing here. The username is
// whatever the caller sent, so a table with one entry per name sent is a table
// the caller sizes.
func TestACallerVaryingTheUsernameCannotGrowTheAccountTable(t *testing.T) {
	t.Parallel()

	guard := New(Options{MaxKeys: tableBound, AddressFree: unqueued, Sleep: nothingIsHeld})

	for i := range flood {
		require.NoError(t, guard.Hold(t.Context(), "198.51.100.7", "guess-"+strconv.Itoa(i)+"@example.com"))
	}

	guard.mu.Lock()
	defer guard.mu.Unlock()

	require.LessOrEqual(t, len(guard.accounts.entries), tableBound+1,
		"a caller who varies the username decides how much this holds, past the one overflow key")
}

// TestACallerVaryingTheForwardedAddressCannotGrowTheAddressTable is the same
// bound on the other table. Behind a declared proxy the client address comes
// out of a header, and a caller can write a different one on every request.
func TestACallerVaryingTheForwardedAddressCannotGrowTheAddressTable(t *testing.T) {
	t.Parallel()

	guard := New(Options{
		MaxKeys:        tableBound,
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		Sleep:          nothingIsHeld,
	})

	for i := range flood {
		address := netip.AddrFrom4([4]byte{203, 0, byte(i / 256), byte(i % 256)}).String()
		require.NoError(t, guard.Hold(t.Context(), address, "owner@example.com"))
	}

	guard.mu.Lock()
	defer guard.mu.Unlock()

	require.LessOrEqual(t, len(guard.addresses.entries), tableBound+1,
		"a caller who varies the forwarded address decides how much this holds, past the one overflow key")
}

// TestAFullTableStillHoldsWhatItAlreadyRecorded is why the table is capped
// rather than evicted from. An eviction policy is a way to erase the record of
// the account being guessed at: flood the table, and the record that mattered
// is gone.
func TestAFullTableStillHoldsWhatItAlreadyRecorded(t *testing.T) {
	t.Parallel()

	guard := New(Options{
		MaxKeys: tableBound, AccountFree: 1, AddressFree: unqueued, Base: time.Second, Sleep: nothingIsHeld,
	})

	for range 4 {
		require.NoError(t, guard.Hold(t.Context(), "198.51.100.7", "owner@example.com"))
	}

	for i := range flood {
		require.NoError(t, guard.Hold(t.Context(), "198.51.100.7", "guess-"+strconv.Itoa(i)+"@example.com"))
	}

	guard.mu.Lock()
	defer guard.mu.Unlock()

	require.Contains(t, guard.accounts.entries, "owner@example.com",
		"a flood of new keys erased the record of the account being guessed at")
}

// TestAStaleTableIsSweptRatherThanKept is the other half of the bound. A cap
// alone leaves a table full for the life of the process once an attack has
// filled it; the sweep runs from every attempt, so the memory goes back when
// the guessing stops.
func TestAStaleTableIsSweptRatherThanKept(t *testing.T) {
	t.Parallel()

	wound := time.Date(2026, time.August, 28, 9, 0, 0, 0, time.UTC)
	guard := New(Options{
		MaxKeys:     tableBound,
		AddressFree: unqueued,
		Window:      15 * time.Minute,
		Now:         func() time.Time { return wound },
		Sleep:       nothingIsHeld,
	})

	for i := range tableBound {
		require.NoError(t, guard.Hold(t.Context(), "198.51.100.7", "guess-"+strconv.Itoa(i)+"@example.com"))
	}

	guard.mu.Lock()
	filled := len(guard.accounts.entries)
	guard.mu.Unlock()

	require.Positive(t, filled)

	wound = wound.Add(16 * time.Minute)

	require.NoError(t, guard.Hold(t.Context(), "198.51.100.7", "somebody@example.com"))

	guard.mu.Lock()
	defer guard.mu.Unlock()

	require.Len(t, guard.accounts.entries, 1, "the table kept keys nothing had been charged to for a window")
}

// TestANewKeyIsStillHeldWhileTheTableIsFull is the gap a cap without an
// overflow record left. A caller who filled the account table with made-up
// usernames got every attempt on an account nobody had recorded yet for
// nothing. Every new key now shares one record, and that record is held like
// any other.
func TestANewKeyIsStillHeldWhileTheTableIsFull(t *testing.T) {
	t.Parallel()

	var last time.Duration

	guard := New(Options{
		MaxKeys:     tableBound,
		AccountFree: 1,
		AddressFree: unqueued,
		Base:        time.Second,
		Sleep: func(_ context.Context, held time.Duration) error {
			last = held

			return nil
		},
	})

	for i := range tableBound {
		require.NoError(t, guard.Hold(t.Context(), "198.51.100.7", "filler-"+strconv.Itoa(i)+"@example.com"))
	}

	for i := range 3 {
		require.NoError(t, guard.Hold(t.Context(), "198.51.100.7", "target-"+strconv.Itoa(i)+"@example.com"))
	}

	require.Positive(t, last, "an attempt on a key the full table could not record was not held")
}

// TestTheOverflowRecordHoldsAndNeverRefuses is what keeps a full address table
// from being a lockout. Every address the table could not record shares the
// overflow record, the owner's among them. A queue there would let a caller
// who filled the table keep it full and refuse all of them, so it holds the
// way the account record does and refuses nothing.
func TestTheOverflowRecordHoldsAndNeverRefuses(t *testing.T) {
	t.Parallel()

	var last time.Duration

	wound := time.Date(2026, time.October, 27, 9, 0, 0, 0, time.UTC)
	guard := New(Options{
		MaxKeys:     tableBound,
		AccountFree: unqueued,
		AddressFree: 1,
		Base:        time.Second,
		Ceiling:     2 * time.Second,
		Now:         func() time.Time { return wound },
		Sleep: func(_ context.Context, held time.Duration) error {
			last = held

			return nil
		},
	})

	for i := range tableBound {
		require.NoError(
			t,
			guard.Hold(t.Context(), netip.AddrFrom4([4]byte{203, 0, 0, byte(i)}).String(), "a@example.com"),
		)
	}

	// The clock does not move, so all of these arrive together. On a queued
	// record the third would already be refused.
	for i := range flood {
		address := netip.AddrFrom4([4]byte{198, 51, byte(i / 256), byte(i % 256)}).String()
		require.NoError(t, guard.Hold(t.Context(), address, "owner@example.com"),
			"an address the full table could not record was refused")
	}

	require.Equal(t, 2*time.Second, last, "the overflow record stopped holding")
}
