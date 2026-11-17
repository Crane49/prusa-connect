package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/authtest"
)

func TestTheMemoryStoreKeepsThePairingContract(t *testing.T) {
	t.Parallel()

	authtest.AppPairingContract(t, func(*testing.T) auth.AppPairingStore { return auth.NewMemoryAppPairings() })
}

// A person types the code the way they read it, and a dash or the case makes
// no difference.
func TestAUserCodeIsReadTheWayAPersonTypesIt(t *testing.T) {
	t.Parallel()

	require.Equal(t, "BCDFGHJK", auth.NormalizeUserCode(" bcdf-ghjk "))
	require.Equal(t, "BCDF-GHJK", auth.FormatUserCode("BCDFGHJK"))
}
