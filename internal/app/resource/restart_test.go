package resource_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// operate_resource is the level between reading an instance and rewriting it,
// and this is both directions of it. A viewer restarting a failed sink would
// make the read level a write level; an operator who cannot restart one is an
// operator with nothing to operate, which is what the action was declared for.
func TestRestartingAnInstanceTakesOperateResource(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)
	h.grant(instance.Kind(), authz.RelViewer)

	err := h.store.Restart(t.Context(), stranger, instance)
	require.Error(t, err, "a viewer restarted an instance")
	require.Equal(t, http.StatusForbidden, statusOf(t, err),
		"a caller who can already see the instance is told which grant is missing")
	require.Contains(t, problem(t, err), authz.ActionOperateResource.String(),
		"the refusal does not name the action the caller is missing")
	require.Empty(t, h.plane.restarts(), "the plane was reached by a caller who was refused")

	h.grant(instance.Kind(), authz.RelOperator)

	require.NoError(t, h.store.Restart(t.Context(), stranger, instance))
	require.Equal(t, []ref.Ref{instance}, h.plane.restarts())
}

// A manager holds operate_resource through the level that contains it, so the
// grant that rewrites an instance also restarts one. The reverse is held above.
func TestAManagerRestartsAnInstanceToo(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)
	h.grant(instance.Kind(), authz.RelManager)

	require.NoError(t, h.store.Restart(t.Context(), stranger, instance))
	require.Equal(t, []ref.Ref{instance}, h.plane.restarts())
}

// The absence rule reaches this operation too. A caller holding nothing on the
// kind is told what a caller naming an instance that is not there is told, in
// the same bytes, so a restart is not a way to enumerate the configuration.
func TestARestartRefusesAHiddenInstanceAsAnAbsentOne(t *testing.T) {
	t.Parallel()

	h, present := seeded(t)
	absent := mustRef(t, ref.KindSink, "nowhere")

	missingErr := h.store.Restart(t.Context(), stranger, absent)
	require.Error(t, missingErr)
	require.Equal(t, http.StatusNotFound, statusOf(t, missingErr))

	hiddenErr := h.store.Restart(t.Context(), stranger, present)
	require.Error(t, hiddenErr)

	require.Equal(t,
		strings.ReplaceAll(problem(t, missingErr), absent.ID(), "<name>"),
		strings.ReplaceAll(problem(t, hiddenErr), present.ID(), "<name>"),
		"the refusal for an instance the caller may not see must read as the refusal for one "+
			"that is not there")

	require.Empty(t, h.plane.restarts(), "the plane was reached by a caller who was refused")
}

// An instance that is declared and switched off is a conflict and not an
// absence. Hiding it would be wrong twice: the caller may read it, and the
// answer they need is that starting it is a write.
func TestRestartingADisabledInstanceIsAConflict(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	instance := mustRef(t, ref.KindSink, "webhook")

	off := spec("http", `{"url":"https://a.example"}`)
	off.Enabled = false

	_, err := h.create(instance, off)
	require.NoError(t, err)

	err = h.store.Restart(t.Context(), owner, instance)
	require.Error(t, err, "an instance nothing is supervising was restarted")
	require.Equal(t, http.StatusConflict, statusOf(t, err))
	require.Contains(t, problem(t, err), "RESOURCE_NOT_RUNNING")
}

// A restart writes nothing, so the token a write must quote does not move and
// the declaration is the one it was. An operation that quietly rewrote the
// overlay would invalidate every ETag a client is holding.
func TestARestartWritesNothing(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)
	h.grant(instance.Kind(), authz.RelOperator)

	before := h.version()
	declared := h.mustGet(instance)
	overlay := h.overlayBytes()

	require.NoError(t, h.store.Restart(t.Context(), stranger, instance))

	require.Equal(t, before, h.version(), "a restart moved the version a write has to quote")
	require.Equal(t, overlay, h.overlayBytes(), "a restart rewrote the overlay")
	require.Equal(t, declared, h.mustGet(instance), "a restart changed the declaration")
	require.Empty(t, h.stray(), "a restart left a staged file behind")
}

// A store built with no plane is the configuration-only deployment. Nothing is
// supervised there, so nothing can be restarted, and the answer says so rather
// than reporting success over a call that reached nobody.
func TestARestartWithNoSupervisionPlaneIsAConflict(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(cfg *resource.Config) { cfg.Plane = nil })
	instance := mustRef(t, ref.KindSink, "webhook")

	_, err := h.create(instance, spec("http", `{"url":"https://a.example"}`))
	require.NoError(t, err)

	err = h.store.Restart(t.Context(), owner, instance)
	require.Error(t, err)
	require.Equal(t, http.StatusConflict, statusOf(t, err))
}
