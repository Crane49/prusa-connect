package xbuddy

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
)

// memoryRegistrations is admission.Store over a map, so what a restart reads is
// exactly what the last process wrote and nothing else crosses.
type memoryRegistrations struct {
	rows map[string]admission.Record
	// order is the sequence the rows were written in, so Load answers the same
	// way twice.
	order []string
}

func newMemoryRegistrations() *memoryRegistrations {
	return &memoryRegistrations{rows: make(map[string]admission.Record)}
}

func (m *memoryRegistrations) Load(_ context.Context, connector string) ([]admission.Record, error) {
	out := make([]admission.Record, 0, len(m.order))

	for _, code := range m.order {
		row, held := m.rows[code]
		if held && row.Connector == connector {
			out = append(out, row)
		}
	}

	return out, nil
}

func (m *memoryRegistrations) Save(_ context.Context, record admission.Record) error {
	if _, held := m.rows[record.Code]; !held {
		m.order = append(m.order, record.Code)
	}

	m.rows[record.Code] = record

	return nil
}

func (m *memoryRegistrations) Forget(_ context.Context, _, code string) error {
	delete(m.rows, code)

	return nil
}

// A printer an operator approved is admitted by the next process without
// anybody deciding again. It is finding CL-02 at the level of the table.
func TestAnApprovedRegistrationIsRestoredByTheNextProcess(t *testing.T) {
	t.Parallel()

	store := newMemoryRegistrations()

	first := newRegistrar(ModeBoth, 0)
	require.NoError(t, first.restore(t.Context(), "xbuddy", "printer", store))

	code, _, err := first.begin(t.Context(), Registration{
		Fingerprint: "FP-1", Serial: "SN-1", PrinterType: "MK4", Firmware: "6.0.0",
	})
	require.NoError(t, err)

	_, err = first.Approve(t.Context(), code, "user:ada")
	require.NoError(t, err)

	admitted, err := first.complete(t.Context(), code)
	require.NoError(t, err)
	require.NotEmpty(t, admitted.Token)

	// The process comes back. Nothing crosses but the rows.
	second := newRegistrar(ModeBoth, 0)
	require.NoError(t, second.restore(t.Context(), "xbuddy", "printer", store))

	known, err := second.authenticate("FP-1", admitted.Token)
	require.NoError(t, err, "the printer approved before the restart was refused after it")
	require.Equal(t, "FP-1", known)

	// And what it said about itself came back with it, so a listing does not
	// report an anonymous machine.
	held, ok := second.registrationFor("FP-1")
	require.True(t, ok)
	require.Equal(t, "SN-1", held.Serial)
	require.Equal(t, "MK4", held.PrinterType)

	// The row records who let it through, which is the question an operator
	// asks after the fact.
	require.Equal(t, "user:ada", store.rows[code].AdmittedBy)
	require.False(t, store.rows[code].AdmittedAt.IsZero())
	require.NotEmpty(t, store.rows[code].TokenHash, "the row holds no digest")
}

// A token this process did not mint has a digest here and its plaintext only on
// the printer. A printer that comes back for it after a restart is given a
// fresh one rather than refused, and the fresh one is what the table then
// accepts.
func TestAPrinterThatPollsAfterARestartIsIssuedATokenItCanUse(t *testing.T) {
	t.Parallel()

	store := newMemoryRegistrations()

	first := newRegistrar(ModeToken, 0)
	require.NoError(t, first.restore(t.Context(), "xbuddy", "printer", store))

	code, _, err := first.begin(t.Context(), Registration{Fingerprint: "FP-2"})
	require.NoError(t, err)

	_, err = first.Approve(t.Context(), code, "user:ada")
	require.NoError(t, err)

	// The process dies before the printer collected anything.
	second := newRegistrar(ModeToken, 0)
	require.NoError(t, second.restore(t.Context(), "xbuddy", "printer", store))

	collected, err := second.complete(t.Context(), code)
	require.NoError(t, err, "an approved printer polling after a restart was turned away")
	require.NotEmpty(t, collected.Token)

	known, err := second.authenticate("FP-2", collected.Token)
	require.NoError(t, err)
	require.Equal(t, "FP-2", known)
}

// A rejected registration is gone, and stays gone.
func TestARejectedRegistrationIsRemovedFromTheStore(t *testing.T) {
	t.Parallel()

	store := newMemoryRegistrations()

	registry := newRegistrar(ModeToken, 0)
	require.NoError(t, registry.restore(t.Context(), "xbuddy", "printer", store))

	code, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-3"})
	require.NoError(t, err)
	require.Len(t, store.rows, 1)

	require.NoError(t, registry.Reject(t.Context(), code))
	require.Empty(t, store.rows)
}

// A code nobody typed runs out. It is a claim on the table and on the code
// space, and one left there for ever is a code somebody may still guess.
func TestACodeNobodyApprovedExpires(t *testing.T) {
	t.Parallel()

	store := newMemoryRegistrations()
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)

	registry := newRegistrar(ModeToken, 0)
	registry.now = func() time.Time { return now }
	require.NoError(t, registry.restore(t.Context(), "xbuddy", "printer", store))

	code, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-4"})
	require.NoError(t, err)
	require.Len(t, registry.pending(), 1)

	now = now.Add(defaultRegistrationTTL + time.Minute)

	require.Empty(t, registry.pending(), "an expired code is still offered to an operator")

	_, err = registry.complete(t.Context(), code)
	require.ErrorIs(t, err, ErrUnknownCode)

	_, err = registry.Approve(t.Context(), code, "user:ada")
	require.ErrorIs(t, err, ErrUnknownCode)

	// The next process does not load it at all.
	next := newRegistrar(ModeToken, 0)
	next.now = func() time.Time { return now }
	require.NoError(t, next.restore(t.Context(), "xbuddy", "printer", store))
	require.Empty(t, next.pending())
	require.Empty(t, store.rows, "the expired row was left in the store")
}

// An approved registration does not expire. It is the credential the printer
// dials with, and expiring it would turn a working fleet off on a timer.
func TestAnApprovedRegistrationDoesNotExpire(t *testing.T) {
	t.Parallel()

	store := newMemoryRegistrations()
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)

	registry := newRegistrar(ModeToken, 0)
	registry.now = func() time.Time { return now }
	require.NoError(t, registry.restore(t.Context(), "xbuddy", "printer", store))

	code, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-5"})
	require.NoError(t, err)

	_, err = registry.Approve(t.Context(), code, "user:ada")
	require.NoError(t, err)

	admitted, err := registry.complete(t.Context(), code)
	require.NoError(t, err)

	now = now.Add(30 * 24 * time.Hour)

	next := newRegistrar(ModeToken, 0)
	next.now = func() time.Time { return now }
	require.NoError(t, next.restore(t.Context(), "xbuddy", "printer", store))

	known, err := next.authenticate("FP-5", admitted.Token)
	require.NoError(t, err, "an approved printer was expired off the fleet")
	require.Equal(t, "FP-5", known)
}
