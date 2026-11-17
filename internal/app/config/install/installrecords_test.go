package install_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// moduleWithRecords renders a service block granted mib of room for its
// records, and one with no grant for a zero.
func moduleWithRecords(mib int) []byte {
	block := strings.Builder{}

	block.Write(moduleService("com.github.aux.acme.planner/spool-tracker"))

	if mib != 0 {
		block.WriteString(lines("        records_mib: " + strconv.Itoa(mib)))
	}

	return []byte(block.String())
}

// The room an operator granted is the limit the namespace is held to, and no
// grant is the default (ADR-20261007-br4).
func TestTheRecordLimitIsWhatTheOperatorGranted(t *testing.T) {
	t.Parallel()

	for name, held := range map[string]struct {
		mib  int
		want int64
	}{
		"no grant":       {0, stateapi.DefaultNamespaceBytes},
		"more":           {128, 128 << 20},
		"less":           {2, 2 << 20},
		"the most there": {install.MaxRecordsMiB, stateapi.MaxGrantedNamespaceBytes},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", moduleWithRecords(held.mib))
			require.NoError(t, err)
			require.Empty(t, doc.Failures)
			require.Len(t, doc.Instances, 1)
			require.Equal(t, held.mib, doc.Instances[0].Service.RecordsMiB)
			require.Equal(t, held.want, doc.Instances[0].Service.RecordLimit())
		})
	}
}

// A grant the record store would not hold is refused at load, naming the key,
// rather than when the module first writes.
func TestARecordGrantPastTheCeilingIsRefusedAtLoad(t *testing.T) {
	t.Parallel()

	for _, mib := range []int{-1, install.MaxRecordsMiB + 1} {
		t.Run(strconv.Itoa(mib), func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", moduleWithRecords(mib))
			require.NoError(t, err)
			require.Empty(t, doc.Instances)
			require.Len(t, doc.Failures, 1)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
			require.Contains(t, doc.Failures[0].Err.Error(), "service.records_mib")
		})
	}
}
