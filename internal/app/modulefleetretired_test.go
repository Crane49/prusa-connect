package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// A module reads a retired printer as retired, so it can let go of what it
// kept for it.
func TestAModuleReadsARetiredPrinterAsRetired(t *testing.T) {
	t.Parallel()

	retired := httpapi.Device{ID: "gone", Retired: &httpapi.DeviceRetirement{Reason: retirementRemovedByOperator}}

	require.True(t, moduleDevice(retired).Retired)
	require.False(t, moduleDevice(httpapi.Device{ID: "here"}).Retired)
}
