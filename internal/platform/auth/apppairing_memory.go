package auth

import (
	"context"
	"slices"
	"sync"
	"time"
)

// MemoryAppPairings keeps pairings for the life of the process, for the tests
// and for a server that keeps its credentials in memory too.
type MemoryAppPairings struct {
	mu   sync.Mutex
	rows map[string]AppPairing
}

var _ AppPairingStore = (*MemoryAppPairings)(nil)

// NewMemoryAppPairings returns an empty store.
func NewMemoryAppPairings() *MemoryAppPairings {
	return &MemoryAppPairings{rows: map[string]AppPairing{}}
}

// CreateAppPairing writes a new row.
func (m *MemoryAppPairings) CreateAppPairing(_ context.Context, pairing AppPairing) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, row := range m.rows {
		if row.UserCode == pairing.UserCode {
			return ErrUserCodeTaken
		}
	}

	m.rows[pairing.DeviceDigest] = clonePairing(pairing)

	return nil
}

// CountAppPairings counts the rows not yet expired.
func (m *MemoryAppPairings) CountAppPairings(_ context.Context, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0

	for _, row := range m.rows {
		if now.Before(row.ExpiresAt) {
			count++
		}
	}

	return count, nil
}

// AppPairingByUserCode finds a row by its user code.
func (m *MemoryAppPairings) AppPairingByUserCode(_ context.Context, userCode string) (AppPairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, row := range m.rows {
		if row.UserCode == userCode {
			return clonePairing(row), nil
		}
	}

	return AppPairing{}, ErrAppPairingUnknown
}

// AppPairingByDevice finds a row by the digest of the code an app polls with.
func (m *MemoryAppPairings) AppPairingByDevice(_ context.Context, deviceDigest string) (AppPairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	row, ok := m.rows[deviceDigest]
	if !ok {
		return AppPairing{}, ErrAppPairingUnknown
	}

	return clonePairing(row), nil
}

// DecideAppPairing moves a pending, live row to the decision's state.
func (m *MemoryAppPairings) DecideAppPairing(_ context.Context, decision AppPairing, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for digest, row := range m.rows {
		if row.UserCode != decision.UserCode {
			continue
		}

		if row.State != AppPairingPending || !now.Before(row.ExpiresAt) {
			return false, nil
		}

		row.State = decision.State
		row.DecidedBy = decision.DecidedBy
		row.DecidedScopes = slices.Clone(decision.DecidedScopes)
		row.DecidedGeneration = decision.DecidedGeneration
		row.DecidedRoot = decision.DecidedRoot
		row.RootGeneration = decision.RootGeneration
		m.rows[digest] = row

		return true, nil
	}

	return false, nil
}

// PolledAppPairing records a poll.
func (m *MemoryAppPairings) PolledAppPairing(
	_ context.Context, deviceDigest string, polled time.Time, interval time.Duration,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	row, ok := m.rows[deviceDigest]
	if !ok {
		return nil
	}

	row.PolledAt = polled
	row.Interval = interval
	m.rows[deviceDigest] = row

	return nil
}

// TakeAppPairing deletes a row in the given state.
func (m *MemoryAppPairings) TakeAppPairing(_ context.Context, deviceDigest, state string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	row, ok := m.rows[deviceDigest]
	if !ok || row.State != state {
		return false, nil
	}

	delete(m.rows, deviceDigest)

	return true, nil
}

// DeleteExpiredAppPairings removes the rows past their expiry.
func (m *MemoryAppPairings) DeleteExpiredAppPairings(_ context.Context, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for digest, row := range m.rows {
		if !now.Before(row.ExpiresAt) {
			delete(m.rows, digest)
		}
	}

	return nil
}

// clonePairing copies a row, so a caller holding one cannot change the store.
func clonePairing(row AppPairing) AppPairing {
	row.Scopes = slices.Clone(row.Scopes)
	row.DecidedScopes = slices.Clone(row.DecidedScopes)

	return row
}
