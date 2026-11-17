package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
)

// askingModule is the instance every case here asks as.
const askingModule = "sink:queue-access"

// askingAbout is one question about one person and one printer.
func askingAbout(subject, action, device string) accessapi.CheckParams {
	return accessapi.CheckParams{
		Subject: subject, Action: action, ObjectType: "device", ObjectID: device,
	}
}

// accessFor is a module's view of the graph, with the ceiling the operator
// wrote for the instance.
func accessFor(graph *authz.Store, ceiling ...authz.Action) moduleAccess {
	return moduleAccess{
		grants:   graph,
		standing: moduleStanding{principal: askingModule, ceiling: ceiling},
	}
}

// A module asks about an action its permissions list names, and is refused for
// every other action. The refusal is a refusal and never a false: false is an
// answer about the graph, and a module reading it would learn the bit the
// ceiling exists to withhold.
func TestAModuleAsksOnlyAboutTheActionsItDeclared(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-ACCESS")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, "user:alpha")))

	// The permit. operate is in the ceiling, and the person holds it.
	held := accessFor(graph, authz.ActionObserve, authz.ActionOperate)

	decision, err := held.Check(t.Context(), askingAbout("user:alpha", "operate", id))
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	// The same question about somebody who holds nothing is a decision and not
	// a refusal, because the question itself was inside the ceiling.
	none, err := held.Check(t.Context(), askingAbout("user:beta", "operate", id))
	require.NoError(t, err)
	require.False(t, none.Allowed)

	// The refusal. configure is outside the ceiling, so the graph is never
	// asked and the module learns nothing about who may configure the machine.
	narrow := accessFor(graph, authz.ActionObserve)

	_, err = narrow.Check(t.Context(), askingAbout("user:alpha", "operate", id))
	require.ErrorIs(t, err, accessapi.ErrDenied)

	_, err = held.Check(t.Context(), askingAbout("user:alpha", "administer", id))
	require.ErrorIs(t, err, accessapi.ErrDenied)
}

// An action under checks may be asked about for a person, and the instance
// still cannot take it as itself, because checks never reach the ceiling
// (ADR-20261106-jnb).
func TestAModuleAsksAboutAnActionItOnlyChecks(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-ACCESS-CHECKS")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, "user:alpha")))

	checking := moduleAccess{grants: graph, standing: moduleStanding{
		principal: askingModule, ceiling: []authz.Action{authz.ActionObserve},
		checks: []authz.Action{authz.ActionOperate},
	}}

	decision, err := checking.Check(t.Context(), askingAbout("user:alpha", "operate", id))
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	_, err = checking.Check(t.Context(), askingAbout("user:alpha", "administer", id))
	require.ErrorIs(t, err, accessapi.ErrDenied)

	_, err = checking.standing.as("", authz.ActionOperate)
	require.ErrorIs(t, err, errOutsideCeiling, "a checked action became one the module takes as itself")
}

// An object this server does not have answers as one the subject reaches
// nothing on, so the call says nothing about which printers exist.
func TestAnObjectThisServerDoesNotHaveIsNotAnAnswerAboutIt(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	held := accessFor(graph, authz.ActionObserve)

	decision, err := held.Check(t.Context(), askingAbout("user:alpha", "observe", "p-nothing"))
	require.NoError(t, err)
	require.False(t, decision.Allowed)
}

// An empty subject is the instance asking about itself, which is the one
// principal a module always knows, and it is held to the same ceiling.
func TestAnEmptySubjectIsTheInstanceItself(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-ACCESS-SELF")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, askingModule)))

	held := accessFor(graph, authz.ActionOperate)

	decision, err := held.Check(t.Context(), askingAbout("", "operate", id))
	require.NoError(t, err)
	require.True(t, decision.Allowed, "the instance was not asked about as itself")
}

// A question the authorization vocabulary cannot read is refused rather than
// answered false, so a module cannot tell a name the graph does not hold from
// a permission nobody has.
func TestAQuestionTheVocabularyCannotReadIsRefused(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	held := accessFor(graph, authz.ActionObserve)

	_, err := held.Check(t.Context(), accessapi.CheckParams{
		Subject: "user:alpha", Action: "observe", ObjectType: "planet", ObjectID: "mars",
	})
	require.ErrorIs(t, err, accessapi.ErrInvalidQuestion)

	_, err = held.Check(t.Context(), accessapi.CheckParams{
		Subject: "not a principal", Action: "observe", ObjectType: "device", ObjectID: "p-1",
	})
	require.ErrorIs(t, err, accessapi.ErrInvalidQuestion)
}

// A role lowered in the graph shows in the next answer, because every
// question reads the graph and nothing keeps an answer (ADR-20260929-f4n rule 13).
func TestALoweredRoleShowsInTheNextModuleAnswer(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-LOWERED")

	operator := tuple(t, authz.TypeDevice, id, authz.RelOperator, "user:alpha")
	require.NoError(t, graph.Write(t.Context(), operator))

	held := accessFor(graph, authz.ActionObserve, authz.ActionOperate)

	decision, err := held.Check(t.Context(), askingAbout("user:alpha", "operate", id))
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	require.NoError(t, graph.Delete(t.Context(), operator))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelViewer, "user:alpha")))

	decision, err = held.Check(t.Context(), askingAbout("user:alpha", "operate", id))
	require.NoError(t, err)
	require.False(t, decision.Allowed, "a viewer was still answered yes for operate")

	decision, err = held.Check(t.Context(), askingAbout("user:alpha", "observe", id))
	require.NoError(t, err)
	require.True(t, decision.Allowed, "the lowered role lost what it still holds")
}
