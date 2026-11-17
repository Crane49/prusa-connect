package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func tupleTexts(tuples []Tuple) []string {
	out := make([]string, 0, len(tuples))
	for _, tuple := range tuples {
		out = append(out, tuple.String())
	}

	return out
}

// TestSubjectGrantsListsTheDeviceTuplesOfOneSubject reads olga and sa-ui as the
// org admin. olga's org role is not a device tuple and stays out; her block is
// a device tuple and is in.
func TestSubjectGrantsListsTheDeviceTuplesOfOneSubject(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")

		olga, err := store.SubjectGrants(t.Context(), root, subjectOf(t, "user:olga"))
		require.NoError(t, err)
		require.Equal(t, []string{"device:p-gamma#blocked@user:olga"}, tupleTexts(olga))

		agent, err := store.SubjectGrants(t.Context(), root, subjectOf(t, "service_account:sa-ui"))
		require.NoError(t, err)
		require.Equal(t, []string{
			"device:p-alpha#operator@service_account:sa-ui",
			"device:p-beta#operator@service_account:sa-ui",
			"device:p-delta#operator@service_account:sa-ui",
		}, tupleTexts(agent))
	})
}

// TestSubjectGrantsKeepsOnlyDevicesTheCallerAdministers is the leak this read
// could open. service_account:audit manages p-alpha alone, so of sa-ui's three
// grants it sees one. mallory administers nothing and sees nothing, which is
// the same answer a subject with no grants gets.
func TestSubjectGrantsKeepsOnlyDevicesTheCallerAdministers(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		subject := subjectOf(t, "service_account:sa-ui")

		held, err := store.SubjectGrants(t.Context(), subjectOf(t, "service_account:audit"), subject)
		require.NoError(t, err)
		require.Equal(t, []string{"device:p-alpha#operator@service_account:sa-ui"}, tupleTexts(held))

		held, err = store.SubjectGrants(t.Context(), subjectOf(t, "user:mallory"), subject)
		require.NoError(t, err)
		require.Empty(t, held)
	})
}
