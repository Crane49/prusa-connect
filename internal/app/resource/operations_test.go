package resource_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// The memory history is read with view_resource, and a caller who holds
// nothing is told what a caller naming an instance that is not there is told.
func TestMemoryHistoryOfAnUnseenInstanceIsNotFound(t *testing.T) {
	t.Parallel()

	h, present := seeded(t)
	absent := mustRef(t, ref.KindSink, "nowhere")

	_, missingErr := h.store.MemoryHistory(t.Context(), stranger, absent, time.Now())
	require.Error(t, missingErr)
	require.Equal(t, http.StatusNotFound, statusOf(t, missingErr))

	_, hiddenErr := h.store.MemoryHistory(t.Context(), stranger, present, time.Now())
	require.Error(t, hiddenErr)

	require.Equal(t,
		strings.ReplaceAll(problem(t, missingErr), absent.ID(), "<name>"),
		strings.ReplaceAll(problem(t, hiddenErr), present.ID(), "<name>"))

	h.grant(present.Kind(), authz.RelViewer)

	history, err := h.store.MemoryHistory(t.Context(), stranger, present, time.Now())
	require.NoError(t, err)
	require.NotNil(t, history.Items, "an instance nothing watches answers null, not an empty list")
	require.NotNil(t, history.Stops)

	_, err = h.store.MemoryHistory(t.Context(), stranger, absent, time.Now())
	require.Error(t, err, "an undeclared instance answered with an empty history")
	require.Equal(t, http.StatusNotFound, statusOf(t, err))

	h.grant(present.Kind(), authz.RelManager)

	_, err = h.store.Log(t.Context(), stranger, absent)
	require.Error(t, err, "an undeclared instance answered with an empty log")
	require.Equal(t, http.StatusNotFound, statusOf(t, err))
}

// A history never reaches back further than an hour, whatever the caller asks.
func TestSinceIsHeldToTheLastHour(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)
	h.grant(instance.Kind(), authz.RelViewer)

	before := time.Now()
	_, err := h.store.MemoryHistory(t.Context(), stranger, instance, before.Add(-3*time.Hour))
	require.NoError(t, err)
	after := time.Now()

	asked := h.plane.sinceAsked()
	require.False(t, asked.Before(before.Add(-time.Hour)), "the plane was asked for more than an hour: %s", asked)
	require.False(t, asked.After(after.Add(-time.Hour)), "the plane was asked for less than an hour: %s", asked)

	recent := before.Add(-10 * time.Minute)
	_, err = h.store.MemoryHistory(t.Context(), stranger, instance, recent)
	require.NoError(t, err)
	require.True(t, h.plane.sinceAsked().Equal(recent), "a start inside the hour was moved")
}

// A module can print a setting it was given, so the log takes the grant that
// reads the settings (ADR-20261030-hrh).
func TestTheLogTakesConfigureResource(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)
	h.plane.log = map[ref.Ref]httpapi.ResourceLog{
		instance: {Lines: []httpapi.ResourceLogLine{{Text: "boom"}}},
	}
	h.grant(instance.Kind(), authz.RelViewer)

	_, err := h.store.Log(t.Context(), stranger, instance)
	require.Error(t, err, "a viewer read the log")
	require.Equal(t, http.StatusForbidden, statusOf(t, err))
	require.Contains(t, problem(t, err), authz.ActionConfigureResource.String())

	h.grant(instance.Kind(), authz.RelManager)

	log, err := h.store.Log(t.Context(), stranger, instance)
	require.NoError(t, err)
	require.Equal(t, "boom", log.Lines[0].Text)
}

func TestTheLogOfAnUnseenInstanceIsNotFound(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)

	_, err := h.store.Log(t.Context(), stranger, instance)
	require.Error(t, err)
	require.Equal(t, http.StatusNotFound, statusOf(t, err))
}
