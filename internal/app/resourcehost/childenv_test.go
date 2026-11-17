package resourcehost

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChildEnvCarriesTheBudget(t *testing.T) {
	t.Parallel()

	require.Contains(t, childEnv(nil, 134217728, false), "PCL_MEMORY_BUDGET_BYTES=134217728")
	require.Empty(t, childEnv(nil, 0, false), "without a budget neither GOMEMLIMIT nor the budget is set")
}
