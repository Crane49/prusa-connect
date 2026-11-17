package dbstore_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/authtest"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
)

// The table keeps the pairing contract on both engines, so a pairing one
// process opened can be approved and collected through another.
func TestTheDatabaseKeepsThePairingContract(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		authtest.AppPairingContract(t, func(t *testing.T) auth.AppPairingStore {
			t.Helper()

			store, err := dbstore.NewAppPairings(e.Open(t), e.Engine)
			require.NoError(t, err)

			return store
		})
	})
}
