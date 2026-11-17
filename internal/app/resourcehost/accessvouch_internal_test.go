package resourcehost

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
)

// askedAccess is what sits behind the vouching: it records the questions that
// reached it and answers yes to all of them.
type askedAccess struct{ saw []string }

func (a *askedAccess) Check(
	_ context.Context, params accessapi.CheckParams,
) (accessapi.Decision, error) {
	a.saw = append(a.saw, params.Subject)

	return accessapi.Decision{Allowed: true}, nil
}

// A module asks about a person it has served, and about nobody else. The
// question survives the person's request, because a queue starts a print at
// night and the request that submitted it closed in the afternoon.
func TestAModuleAsksAboutAPersonItHasServedAndNoOther(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "queue")
	h := &Host{}
	inner := &askedAccess{}
	access := h.vouchAccess(instance, inner)

	// Never served: refused before the graph is reached.
	_, err := access.Check(t.Context(), accessapi.CheckParams{Subject: "user:alice"})
	require.ErrorIs(t, err, accessapi.ErrUnknownSubject)
	require.Empty(t, inner.saw, "a refused question reached the graph")

	// Served, and the request has ended.
	h.flights.board(instance, "user:alice", nil)()

	_, err = access.Check(t.Context(), accessapi.CheckParams{Subject: "user:alice"})
	require.NoError(t, err, "a person the module served could not be asked about after their request")

	// Somebody another instance served is not this instance's to ask about.
	h.flights.board(sinkRef(t, "audit"), "user:bob", nil)()

	_, err = access.Check(t.Context(), accessapi.CheckParams{Subject: "user:bob"})
	require.ErrorIs(t, err, accessapi.ErrUnknownSubject)

	require.Equal(t, []string{"user:alice"}, inner.saw)
}

// A link asks about a server it reported, the principal it already calls the
// fleet as, and about no other server. Any other kind of instance asks about
// no server.
func TestALinkAsksAboutAServerItReported(t *testing.T) {
	t.Parallel()

	link, err := ref.New(ref.KindLink, "fractal")
	require.NoError(t, err)

	queue := sinkRef(t, "queue")
	host := &Host{running: map[ref.Ref]*instance{link: {}}}
	require.Empty(t, host.flights.nodes.report(link, []string{"hq"}, host.supervised()))

	inner := &askedAccess{}
	asked := map[string]error{}

	for name, question := range map[string]struct {
		instance ref.Ref
		subject  string
	}{
		"a reported server":  {link, "node:hq"},
		"another server":     {link, "node:zagreb"},
		"a server of a link": {queue, "node:hq"},
	} {
		_, asked[name] = host.vouchAccess(question.instance, inner).Check(t.Context(),
			accessapi.CheckParams{Subject: question.subject})
	}

	require.NoError(t, asked["a reported server"])
	require.ErrorIs(t, asked["another server"], accessapi.ErrUnknownSubject)
	require.ErrorIs(t, asked["a server of a link"], accessapi.ErrUnknownSubject)
	require.Equal(t, []string{"node:hq"}, inner.saw)
}

// An empty subject is the instance asking about itself, which needs no memory.
func TestAModuleAsksAboutItselfWithNoMemory(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "queue")
	h := &Host{}
	inner := &askedAccess{}

	_, err := h.vouchAccess(instance, inner).Check(t.Context(), accessapi.CheckParams{})
	require.NoError(t, err)
	require.Equal(t, []string{""}, inner.saw)
}

// The memory is bounded, so a module that has served thousands of people does
// not make this host hold a row per person for as long as it runs. The oldest
// is dropped and the newest is kept.
func TestTheMemoryOfServedPeopleIsBounded(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "queue")
	h := &Host{}

	for index := range maxServedPrincipals + 10 {
		h.flights.board(instance, "user:person-"+strconv.Itoa(index), nil)()
	}

	h.flights.mu.Lock()
	held := len(h.flights.served[instance])
	h.flights.mu.Unlock()

	require.Equal(t, maxServedPrincipals, held)

	require.False(t, h.flights.hasServed(instance, "user:person-0"),
		"the oldest person was kept past the bound")
	require.True(t, h.flights.hasServed(instance,
		"user:person-"+strconv.Itoa(maxServedPrincipals+9)))
}

// A restart of the instance empties the memory, so a module reinstalled under
// a name another module held cannot ask about the people that one served.
func TestAStartEmptiesTheMemoryOfServedPeople(t *testing.T) {
	t.Parallel()

	instance := sinkRef(t, "queue")
	h := &Host{}

	h.flights.board(instance, "user:alice", nil)()
	require.True(t, h.flights.hasServed(instance, "user:alice"))

	h.flights.forget(instance)
	require.False(t, h.flights.hasServed(instance, "user:alice"))
}

// A host that answers no such question installs no method, so a module reads
// CodeMethodNotFound on its first call rather than reaching a nil.
func TestAHostThatAnswersNoAccessQuestionInstallsNoMethod(t *testing.T) {
	t.Parallel()

	h := &Host{}

	require.Nil(t, h.vouchAccess(sinkRef(t, "queue"), nil))
	require.Empty(t, accessapi.Handlers(h.vouchAccess(sinkRef(t, "queue"), nil)))
}
