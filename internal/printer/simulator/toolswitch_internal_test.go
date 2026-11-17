package simulator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheMMUAndTheHeadsSwitchWhileThePrinterRuns holds the two setters to the
// INFO they change. A printer with no session has nowhere to volunteer it.
func TestTheMMUAndTheHeadsSwitchWhileThePrinterRuns(t *testing.T) {
	t.Parallel()

	mk4s, err := New(Config{Model: "MK4S", MMU: true, BaseURL: "http://127.0.0.1:1"})
	require.NoError(t, err)

	assert.Equal(t, 5, mk4s.infoData()["slots"])
	assert.False(t, mk4s.SetMMU(false), "an INFO went out with no session")
	assert.Equal(t, 1, mk4s.infoData()["slots"])
	assert.Equal(t, map[string]any{"enabled": false, "version": noMMUVersion}, mk4s.infoData()["mmu"])

	xl, err := New(Config{Model: "XL", BaseURL: "http://127.0.0.1:1"})
	require.NoError(t, err)

	assert.Contains(t, xl.infoData()["tools"], "2")
	xl.SetDisabledHeads(1)
	assert.NotContains(t, xl.infoData()["tools"], "2", "head 1 is tool 2 in the numbered shape")
	assert.Contains(t, xl.infoData()["tools"], "1")
}
