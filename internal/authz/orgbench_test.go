package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// BenchmarkOrgGrantWriteAndWithdraw measures a fleet-wide mutation against the
// 400-printer fixture, beside BenchmarkGrantSurfaceWriteAndWithdraw. The
// difference between the two is what authorizeOnOrg costs: one object compiled
// for a device grant against every device and every resource kind the tuple set
// names for an org grant.
func BenchmarkOrgGrantWriteAndWithdraw(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			ctx := b.Context()

			admin := subjectOf(b, fleetOrgAdmin)
			grant := mustTuple(b, "org:acme#viewer@user:u199")

			for b.Loop() {
				if _, err := store.WriteGrant(ctx, admin, grant); err != nil {
					require.NoError(b, err)
				}

				if err := store.WithdrawGrant(ctx, admin, grant); err != nil {
					require.NoError(b, err)
				}
			}
		})
	}
}
