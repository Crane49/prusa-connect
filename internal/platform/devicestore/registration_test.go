package devicestore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
)

// aRegistration is one row with every column set, so a round trip that drops
// one is a failure rather than a value nobody looked at.
func aRegistration(connector, code, fingerprint string) admission.Record {
	at := time.Date(2026, time.September, 2, 11, 0, 0, 0, time.UTC)

	return admission.Record{
		Connector:   connector,
		Code:        code,
		Fingerprint: fingerprint,
		TokenHash:   "9f2c" + code,
		Serial:      "SN-" + code,
		Model:       "MK4",
		Firmware:    "6.2.4",
		Kind:        "printer",
		Approved:    true,
		Endorsed:    true,
		Completed:   true,
		AdmittedBy:  "user:ada",
		AdmittedAt:  at,
		IssuedAt:    at.Add(-time.Hour),
		Seq:         7,
	}
}

// The store is what a restart reads, so what it hands back has to be what was
// written, column for column.
func TestARegistrationRowSurvivesTheProcessThatWroteIt(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := NewRegistrations(db, e.Engine, nil)
		require.NoError(t, err)

		want := aRegistration("xbuddy", "ACDEF23456", "FP-1")
		require.NoError(t, store.Save(t.Context(), want))

		// A second store over the same database is what the next process is.
		next, err := NewRegistrations(db, e.Engine, nil)
		require.NoError(t, err)

		rows, err := next.Load(t.Context(), "xbuddy")
		require.NoError(t, err)
		require.Equal(t, []admission.Record{want}, rows)
	})
}

// A code is minted per connector, so a listing is per connector too: one
// connector must not read another's table.
func TestARegistrationIsReadBackOnlyForItsOwnConnector(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := NewRegistrations(db, e.Engine, nil)
		require.NoError(t, err)

		require.NoError(t, store.Save(t.Context(), aRegistration("xbuddy", "AAAAAAAAAA", "FP-1")))
		require.NoError(t, store.Save(t.Context(), aRegistration("second", "BBBBBBBBBB", "FP-2")))

		rows, err := store.Load(t.Context(), "xbuddy")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "AAAAAAAAAA", rows[0].Code)
	})
}

// Saving twice is one row. A printer that retries its registration must not
// pile up rows an operator then has to choose between.
func TestSavingOneRegistrationTwiceLeavesOneRow(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := NewRegistrations(db, e.Engine, nil)
		require.NoError(t, err)

		row := aRegistration("xbuddy", "ACDEF23456", "FP-1")
		require.NoError(t, store.Save(t.Context(), row))

		row.Firmware = "6.3.0"
		require.NoError(t, store.Save(t.Context(), row))

		rows, err := store.Load(t.Context(), "xbuddy")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "6.3.0", rows[0].Firmware)
	})
}

// A registration nobody decided carries no admission and an expiry, and the two
// must come back as they went in: an instant that is not there is not the zero
// instant.
func TestARegistrationNobodyDecidedCarriesNoAdmission(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := NewRegistrations(db, e.Engine, nil)
		require.NoError(t, err)

		issued := time.Date(2026, time.September, 2, 11, 0, 0, 0, time.UTC)
		waiting := admission.Record{
			Connector:   "xbuddy",
			Code:        "ACDEF23456",
			Fingerprint: "FP-1",
			IssuedAt:    issued,
			ExpiresAt:   issued.Add(24 * time.Hour),
			Seq:         1,
		}

		require.NoError(t, store.Save(t.Context(), waiting))

		rows, err := store.Load(t.Context(), "xbuddy")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.True(t, rows[0].AdmittedAt.IsZero(), "a row nobody decided claims a decision")
		require.Equal(t, issued.Add(24*time.Hour), rows[0].ExpiresAt)
		require.False(t, rows[0].Expired(issued.Add(time.Hour)))
		require.True(t, rows[0].Expired(issued.Add(25*time.Hour)))
	})
}

// Forgetting one is what a rejection is, and forgetting one that is not there
// is not an error: the caller wanted it gone and it is gone.
func TestForgettingARegistrationRemovesItAndRepeatsCleanly(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := NewRegistrations(db, e.Engine, nil)
		require.NoError(t, err)

		require.NoError(t, store.Save(t.Context(), aRegistration("xbuddy", "ACDEF23456", "FP-1")))
		require.NoError(t, store.Forget(t.Context(), "xbuddy", "ACDEF23456"))

		rows, err := store.Load(t.Context(), "xbuddy")
		require.NoError(t, err)
		require.Empty(t, rows)

		require.NoError(t, store.Forget(t.Context(), "xbuddy", "ACDEF23456"))
	})
}
