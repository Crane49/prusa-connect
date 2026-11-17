package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
)

// An update that asks for the events the operator approved before keeps them.
// The declaration holds each event as a whole type and the release asks by
// the name after the namespace, so the two are compared as names.
func TestAnApprovedUpdateKeepsTheEventsItHeld(t *testing.T) {
	t.Parallel()

	const namespace = "com.github.aux.acme.materials"

	kept, added := keptEvents(
		map[string]string{"tool": "tool"},
		map[string]string{"tool": "tool"},
		map[string]string{"tool": install.EventTypeOf(namespace, "tool")},
		namespace, nil,
	)

	require.Equal(t, map[string]string{"tool": "tool"}, kept)
	require.Empty(t, added)
}
