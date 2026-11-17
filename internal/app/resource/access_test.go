package resource_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// grant writes one role on one resource kind, which is the object a resource
// grant is held on.
func (h *harness) grant(kind ref.Kind, relation authz.Relation) {
	h.t.Helper()

	require.NoError(h.t, h.graph.Write(h.t.Context(), h.tuple(kind, relation, stranger)))
}

// revoke removes it again.
func (h *harness) revoke(kind ref.Kind, relation authz.Relation) {
	h.t.Helper()

	require.NoError(h.t, h.graph.Delete(h.t.Context(), h.tuple(kind, relation, stranger)))
}

func (h *harness) tuple(kind ref.Kind, relation authz.Relation, principal string) authz.Tuple {
	h.t.Helper()

	object, err := authz.NewObject(authz.TypeResourceKind, kind.String())
	require.NoError(h.t, err)

	subject, err := authz.ParseSubject(principal)
	require.NoError(h.t, err)

	tuple, err := authz.NewTuple(object, relation, subject)
	require.NoError(h.t, err)

	return tuple
}

// seeded is a harness with one sink the owner created, which is the instance
// every case below either can or cannot reach.
func seeded(t *testing.T) (*harness, ref.Ref) {
	t.Helper()

	h := newHarness(t)
	instance := mustRef(t, ref.KindSink, "webhook")

	_, err := h.create(instance, spec("http", `{"url":"https://a.example"}`))
	require.NoError(t, err)

	return h, instance
}

// statusOf is the status a transport renders for this refusal, which is the
// half of it the comparison below cannot see.
func statusOf(t *testing.T, err error) int {
	t.Helper()

	var refusal *fault.Fault

	require.ErrorAs(t, err, &refusal)

	return refusal.Status()
}

// problem renders a refusal the way a client receives it, so two refusals are
// compared as the bytes a caller can look at rather than as two Go values.
func problem(t *testing.T, err error) string {
	t.Helper()

	var refusal *fault.Fault

	require.ErrorAs(t, err, &refusal)

	encoded, marshalErr := json.Marshal(refusal.ProblemJSON("/api/v1/resources"))
	require.NoError(t, marshalErr)

	return string(encoded)
}

// TestAbsenceAndNoAccessAreIndistinguishable is this project's third standing
// suspicion held at the policy rather than at the transport. The transport's
// copy of this test drives a store that was told which instance to hide; this
// one tells it nothing and lets the graph decide, so it fails if the decision
// is not actually being taken.
//
// The two refusals are compared as the rendered problem document, because a
// difference in the code, the status, the title or one parameter is the whole
// leak.
func TestAbsenceAndNoAccessAreIndistinguishable(t *testing.T) {
	t.Parallel()

	h, present := seeded(t)
	absent := mustRef(t, ref.KindSink, "nowhere")

	for name, call := range map[string]func(instance ref.Ref) error{
		"get": func(instance ref.Ref) error {
			_, err := h.store.Get(t.Context(), stranger, instance)

			return err
		},
		"apply": func(instance ref.Ref) error {
			_, err := h.store.Apply(t.Context(), stranger, instance,
				spec("http", `{"url":"https://b.example"}`), httpapi.Precondition{MustNotExist: true})

			return err
		},
		"delete": func(instance ref.Ref) error {
			return h.store.Delete(t.Context(), stranger, instance, httpapi.Precondition{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// The status is pinned as well as compared. Two refusals that agree
			// with each other and are both ACCESS_DENIED would pass the
			// comparison below while telling a caller they exist.
			require.Equal(t, http.StatusNotFound, statusOf(t, call(absent)),
				"an instance that is not there must refuse as one that is not there")

			hidden := problem(t, call(present))
			missing := problem(t, call(absent))

			require.Equal(t,
				strings.ReplaceAll(missing, absent.ID(), "<name>"),
				strings.ReplaceAll(hidden, present.ID(), "<name>"),
				"the refusal for an instance the caller may not see must read as the refusal for one "+
					"that is not there")
		})
	}
}

// TestTheListShowsOnlyWhatTheCallerMaySee is the second enforcement surface.
// The instance is there, the caller holds nothing on it, and it is absent from
// the answer rather than filtered out of a count that still names it.
func TestTheListShowsOnlyWhatTheCallerMaySee(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)

	held, err := h.store.List(t.Context(), owner, "")
	require.NoError(t, err)
	require.Len(t, held.Items, 1)

	blind, err := h.store.List(t.Context(), stranger, "")
	require.NoError(t, err)
	require.Empty(t, blind.Items, "an instance the caller holds no grant on is not in the list")

	h.grant(instance.Kind(), authz.RelViewer)

	seen, err := h.store.List(t.Context(), stranger, "")
	require.NoError(t, err)
	require.Len(t, seen.Items, 1, "a viewer grant on the kind puts the instance in the list")
}

// TestAnEmptyListingCarriesNoVersion closes the last thing the listing told a
// caller who may see nothing. The version is the ETag of the whole overlay, so
// a caller with no grant was handed a token that moved every time somebody else
// wrote an instance: a poll on this route reported that the configuration had
// changed, to somebody the graph grants nothing.
//
// A viewer grant is the arming half. Without it the case passes on a listing
// that never carries a version at all, which would be the same defect written
// the other way round.
func TestAnEmptyListingCarriesNoVersion(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)

	blind, err := h.store.List(t.Context(), stranger, "")
	require.NoError(t, err)
	require.Empty(t, blind.Items)
	require.Empty(t, blind.Version, "a caller who may see no instance was given the overlay's token")

	h.grant(instance.Kind(), authz.RelViewer)

	seen, err := h.store.List(t.Context(), stranger, "")
	require.NoError(t, err)
	require.Len(t, seen.Items, 1)
	require.NotEmpty(t, seen.Version, "a caller who may see an instance needs the token to write it")
	require.Equal(t, seen.Version, seen.Items[0].Version,
		"the listing and its items report one version, because one file is the concurrency unit")
}

// TestAGrantWrittenAndWithdrawnMovesTheAnswerBothWays is the permit half and
// the refusal half of one rule, which is what an authorization test has to be.
func TestAGrantWrittenAndWithdrawnMovesTheAnswerBothWays(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)

	_, err := h.store.Get(t.Context(), stranger, instance)
	require.Error(t, err)

	h.grant(instance.Kind(), authz.RelViewer)

	found, err := h.store.Get(t.Context(), stranger, instance)
	require.NoError(t, err)
	require.Equal(t, instance.ID(), found.Name)

	h.revoke(instance.Kind(), authz.RelViewer)

	_, err = h.store.Get(t.Context(), stranger, instance)
	require.Error(t, err, "a withdrawn grant hides the instance again")
}

// TestAViewerMayReadAndMayNotWrite keeps the two resource verbs apart. A grant
// that reads and a grant that rewrites the settings block a connection string
// lives in are different levels, and a viewer who can write is the whole point
// of the level.
func TestAViewerMayReadAndMayNotWrite(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)
	h.grant(instance.Kind(), authz.RelViewer)

	version := h.version()

	_, err := h.store.Apply(t.Context(), stranger, instance,
		spec("http", `{"url":"https://b.example"}`), httpapi.Precondition{IfMatch: []string{version}})
	require.Error(t, err)

	var refusal *fault.Fault

	require.ErrorAs(t, err, &refusal)
	require.Equal(t, 403, refusal.Status(),
		"a caller who can already see the instance is told which grant is missing, not that it is absent")

	require.JSONEq(t, `{"url":"https://a.example"}`, string(h.mustGet(instance).Config),
		"a refused write leaves the declaration alone")
}

// TestAnOperatorMayNotRewriteTheConfiguration holds the line between operating
// an instance and configuring one: only a manager rewrites the settings.
func TestAnOperatorMayNotRewriteTheConfiguration(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)
	h.grant(instance.Kind(), authz.RelOperator)

	err := h.store.Delete(t.Context(), stranger, instance, httpapi.Precondition{})
	require.Error(t, err)

	var refusal *fault.Fault

	require.ErrorAs(t, err, &refusal)
	require.Equal(t, 403, refusal.Status())

	h.grant(instance.Kind(), authz.RelManager)
	require.NoError(t, h.store.Delete(t.Context(), stranger, instance, httpapi.Precondition{}))
}

// TestAGrantOnOneKindDoesNotReachAnother is the closed-vocabulary half: the
// object a resource grant is written on is one kind, and a manager of sinks is
// not a manager of storage.
func TestAGrantOnOneKindDoesNotReachAnother(t *testing.T) {
	t.Parallel()

	h, sink := seeded(t)

	storage := mustRef(t, ref.KindStorage, "disk")
	_, err := h.create(storage, spec("filesystem", `{"root":"`+filepath.ToSlash(h.dir)+`"}`))
	require.NoError(t, err)

	h.grant(sink.Kind(), authz.RelManager)

	_, err = h.store.Get(t.Context(), stranger, sink)
	require.NoError(t, err)

	_, err = h.store.Get(t.Context(), stranger, storage)
	require.Error(t, err, "a grant on one kind reaches no instance of another")
}

// TestADeletedInstanceLeavesTheGraphsDirectory is what keeps absence and
// no-access one answer over time. The directory is what an instance decision
// joins against, so a row left behind for an instance nobody declares any more
// is an instance that exists to the graph and to nothing else.
func TestADeletedInstanceLeavesTheGraphsDirectory(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)

	registered, err := h.graph.Resources(t.Context())
	require.NoError(t, err)
	require.Contains(t, registered, instance)

	require.NoError(t, h.store.Delete(t.Context(), owner, instance, httpapi.Precondition{}))

	registered, err = h.graph.Resources(t.Context())
	require.NoError(t, err)
	require.NotContains(t, registered, instance,
		"a deleted instance stays registered, so the graph still says it exists")
}

// TestADirectoryRowThatOutlivedItsInstanceIsRemovedOnTheNextLoad covers the
// case a diff against the last reconciled document cannot: the instance went
// away while the process was not running, so there is no previous document to
// notice it against.
func TestADirectoryRowThatOutlivedItsInstanceIsRemovedOnTheNextLoad(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	stale := mustRef(t, ref.KindSink, "departed")

	require.NoError(t, h.graph.RegisterResources(t.Context(), stale))
	require.NoError(t, h.store.Reload(t.Context()))

	registered, err := h.graph.Resources(t.Context())
	require.NoError(t, err)
	require.NotContains(t, registered, stale)
}

// TestReconcilingTheDirectoryMovesTheEpochOnce is what makes a reconcile one
// change rather than one per instance. The epoch is the generation every cached
// decision and every live subscription is stamped with, and a full recompile of
// both projections rides on each move: 126 ms at fleet scale, measured on the old backlog.
//
// A reconcile that removed rows one at a time paid that per removed instance,
// and each removal was its own transaction, so a process that stopped in the
// loop left the directory holding rows nobody declares.
//
// Three rows go and one arrives, which is a reconcile that both removes and
// registers. One move is the whole assertion.
func TestReconcilingTheDirectoryMovesTheEpochOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, name := range []string{"first", "second", "third"} {
		require.NoError(t, h.graph.RegisterResources(t.Context(), mustRef(t, ref.KindSink, name)))
	}

	registered, err := h.graph.Resources(t.Context())
	require.NoError(t, err)
	require.Len(t, registered, 3, "the stale rows are not there, so this measures nothing")

	h.writeFile(`resources:
  sink:
    audit:
      type: webhook
`)

	before := h.graph.Epoch()
	require.NoError(t, h.store.Reload(t.Context()))

	require.Equal(t, before+1, h.graph.Epoch(),
		"reconciling the directory moved the epoch more than once")

	registered, err = h.graph.Resources(t.Context())
	require.NoError(t, err)
	require.Len(t, registered, 1)
	require.Equal(t, "audit", registered[0].ID())
}

// TestAPrincipalTheGraphCannotKeyOnReachesNothing is the fail-closed end of the
// identity seam. The composition root renders every caller as type:id; anything
// else arriving here is a wiring fault, and it is refused rather than resolved
// as whatever it resembles.
func TestAPrincipalTheGraphCannotKeyOnReachesNothing(t *testing.T) {
	t.Parallel()

	h, instance := seeded(t)

	_, err := h.store.Get(t.Context(), "operator", instance)
	require.Error(t, err)

	_, err = h.store.List(t.Context(), "operator", "")
	require.Error(t, err)
}

// mustGet reads an instance as the owner, for the assertions that only need the
// declaration back.
func (h *harness) mustGet(instance ref.Ref) httpapi.ResourceInstance {
	h.t.Helper()

	found, err := h.get(instance)
	require.NoError(h.t, err)

	return found
}

// TestAStoreWithNoGraphIsNotBuilt keeps the decision from being optional. A
// store that could be constructed without a graph is a store that could be
// wired to answer the question with something else, which is the shape decision
// A5 exists to make impossible.
func TestAStoreWithNoGraphIsNotBuilt(t *testing.T) {
	t.Parallel()

	_, err := resource.New(resource.Config{
		Paths: resource.Paths{Overlay: filepath.Join(t.TempDir(), "resources.json")},
	})
	require.ErrorIs(t, err, resource.ErrConfigInvalid)
	require.ErrorContains(t, err, "no authorization graph")
}
