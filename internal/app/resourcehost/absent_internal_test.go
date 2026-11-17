package resourcehost

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
)

// graphFake is the graph behind the vouching: it answers from a table of
// grants the case can change between questions, and counts what reached it.
type graphFake struct {
	mu      sync.Mutex
	grants  map[string]bool
	ceiling map[string]bool
	asked   int
}

func (g *graphFake) Check(_ context.Context, params accessapi.CheckParams) (accessapi.Decision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.asked++

	if g.ceiling != nil && !g.ceiling[params.Action] {
		return accessapi.Decision{}, accessapi.ErrDenied
	}

	return accessapi.Decision{Allowed: g.grants[params.Subject+" "+params.Action]}, nil
}

func (g *graphFake) set(key string, held bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.grants[key] = held
}

// peopleFake is the account list: the people it holds, and everybody else is
// somebody this server does not hold. kept false is a server that keeps no
// list, and err is a lookup that fails.
type peopleFake struct {
	held   map[string]bool
	listed []accessapi.Person
	kept   bool
	err    error
}

func (p peopleFake) Kept() bool { return p.kept }

func (p peopleFake) Holds(_ context.Context, principal string) (bool, error) {
	return p.held[principal], p.err
}

func (p peopleFake) List(context.Context) ([]accessapi.Person, error) {
	return p.listed, p.err
}

// absentCase is a host with one instance, its graph, its people and its log.
type absentCase struct {
	host   *Host
	graph  *graphFake
	access accessapi.Access
	log    *bytes.Buffer
}

// askingAboutAbsent builds the case. allowed is whether the operator approved
// check_absent_people for the instance.
func askingAboutAbsent(t *testing.T, allowed bool, people People) absentCase {
	t.Helper()

	log := &bytes.Buffer{}
	host := &Host{cfg: Config{
		People: people,
		Logger: slog.New(slog.NewTextHandler(log, nil)),
	}}
	graph := &graphFake{grants: map[string]bool{
		"user:alice operate": true,
		"user:gone operate":  true,
	}}

	module := Module{Instance: sinkRef(t, "queue"), CheckAbsentPeople: allowed}

	return absentCase{host: host, graph: graph, access: host.vouchAccessFor(module, graph), log: log}
}

// theBoard is the account list every case reads: alice and bob are here, and
// gone was deleted with her grants left in the graph.
func theBoard() peopleFake {
	return peopleFake{held: map[string]bool{"user:alice": true, "user:bob": true}, kept: true}
}

func asking(subject, action string) accessapi.CheckParams {
	return accessapi.CheckParams{Subject: subject, Action: action, ObjectType: "device", ObjectID: "p1"}
}

// Without the permission a person the instance is not serving is refused, and
// the graph is never read (ADR-20260929-f4n rule 8).
func TestWithoutThePermissionAnAbsentPersonIsRefused(t *testing.T) {
	t.Parallel()

	held := askingAboutAbsent(t, false, theBoard())

	_, err := held.access.Check(t.Context(), asking("user:alice", "operate"))
	require.ErrorIs(t, err, accessapi.ErrUnknownSubject)
	require.Zero(t, held.graph.asked)
	require.Empty(t, held.log.String(), "a question the instance may not ask wrote an audit line")
}

// With the permission the host answers about a person it holds: yes with the
// grant, no without it (ADR-20260929-f4n rules 6 and 12).
func TestWithThePermissionTheHostAnswersFromTheGraph(t *testing.T) {
	t.Parallel()

	held := askingAboutAbsent(t, true, theBoard())

	decision, err := held.access.Check(t.Context(), asking("user:alice", "operate"))
	require.NoError(t, err)
	require.True(t, decision.Allowed, "a person with the grant was answered no")

	decision, err = held.access.Check(t.Context(), asking("user:bob", "operate"))
	require.NoError(t, err)
	require.False(t, decision.Allowed, "a person without the grant was answered yes")
}

// A deleted person's grants can stay in the graph. The person still answers no,
// as does one who never had an account (ADR-20260929-f4n rule 11).
func TestADeletedPersonAnswersNo(t *testing.T) {
	t.Parallel()

	held := askingAboutAbsent(t, true, theBoard())

	for _, subject := range []string{"user:gone", "user:never"} {
		decision, err := held.access.Check(t.Context(), asking(subject, "operate"))
		require.NoError(t, err, subject)
		require.False(t, decision.Allowed, "%s was answered yes", subject)
	}
}

// A person the module served and who was then deleted answers no too, because
// deletion leaves the grants in the graph (ADR-20260929-f4n rule 11). The account is
// looked up on this path whether or not the instance holds the permission.
func TestADeletedPersonTheModuleServedAnswersNo(t *testing.T) {
	t.Parallel()

	for _, allowed := range []bool{false, true} {
		held := askingAboutAbsent(t, allowed, theBoard())
		held.host.flights.board(sinkRef(t, "queue"), "user:gone", nil)()
		held.host.flights.board(sinkRef(t, "queue"), "user:alice", nil)()

		decision, err := held.access.Check(t.Context(), asking("user:gone", "operate"))
		require.NoError(t, err)
		require.False(t, decision.Allowed, "a deleted person the module served was answered yes")

		decision, err = held.access.Check(t.Context(), asking("user:alice", "operate"))
		require.NoError(t, err)
		require.True(t, decision.Allowed, "a present person the module served was answered no")
	}
}

// Without a list of people the served path answers from the graph, as it does
// on a server with an OpenID Connect login.
func TestTheServedPathWithNoListAnswersFromTheGraph(t *testing.T) {
	t.Parallel()

	held := askingAboutAbsent(t, false, nil)
	held.host.flights.board(sinkRef(t, "queue"), "user:gone", nil)()

	decision, err := held.access.Check(t.Context(), asking("user:gone", "operate"))
	require.NoError(t, err)
	require.True(t, decision.Allowed)
}

// The action is held to the instance's list before the person is looked up,
// so a removed person reads ErrDenied like anybody else, and the refusal does
// not say who exists (ADR-20260929-f4n rule 13).
func TestAnActionOutsideTheListIsDeniedForAnyPerson(t *testing.T) {
	t.Parallel()

	held := askingAboutAbsent(t, true, theBoard())
	held.graph.ceiling = map[string]bool{"operate": true}

	for _, subject := range []string{"user:alice", "user:gone"} {
		_, err := held.access.Check(t.Context(), asking(subject, "administer"))
		require.ErrorIs(t, err, accessapi.ErrDenied, subject)
	}
}

// A lowered role shows in the very next answer, because the graph is read for
// every question (ADR-20260929-f4n rule 12).
func TestALoweredRoleShowsInTheNextAnswer(t *testing.T) {
	t.Parallel()

	held := askingAboutAbsent(t, true, theBoard())

	decision, err := held.access.Check(t.Context(), asking("user:alice", "operate"))
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	held.graph.set("user:alice operate", false)

	decision, err = held.access.Check(t.Context(), asking("user:alice", "operate"))
	require.NoError(t, err)
	require.False(t, decision.Allowed, "the answer kept the role the person held before")
	require.Equal(t, 2, held.graph.asked)
}

// Only one person may be asked about. A service account or a set of people is
// refused as a subject the instance has not served, and the refusal is audited
// (ADR-20260929-f4n rules 9 and 14).
func TestOnlyAPersonMayBeAskedAboutWhileAway(t *testing.T) {
	t.Parallel()

	held := askingAboutAbsent(t, true, theBoard())

	for _, subject := range []string{"service_account:backup", "group:staff#member", "node:hq", "alice"} {
		_, err := held.access.Check(t.Context(), asking(subject, "operate"))
		require.ErrorIs(t, err, accessapi.ErrUnknownSubject, subject)
	}

	require.Zero(t, held.graph.asked)
	require.Equal(t, 4, strings.Count(held.log.String(), "answer=\"refused: not a person\""))
}

// A server that keeps no list of people cannot tell a removed person from a
// present one, so it refuses before it reads the graph, whatever the action,
// and says so in the audit line. The refusal spends a token like any other
// question (ADR-20260929-f4n rules 10, 14 and 15).
func TestAServerWithNoListOfPeopleRefuses(t *testing.T) {
	t.Parallel()

	for name, people := range map[string]People{"no source": nil, "open mode": peopleFake{}} {
		held := askingAboutAbsent(t, true, people)

		_, err := held.access.Check(t.Context(), asking("user:alice", "administer"))
		require.ErrorIs(t, err, accessapi.ErrUnknownSubject, name)
		require.Zero(t, held.graph.asked, "%s: the graph was read", name)
		require.Contains(t, held.log.String(), "keeps no list of people", name)
		require.InDelta(t, absentBurst-1, held.host.absent.held[sinkRef(t, "queue")].tokens, 0.01,
			"%s: the refusal spent no token", name)
	}
}

// A module that floods the host with refusals that need neither the graph nor
// the database writes no more audit lines than the bucket lets through. That
// holds for a subject that is not a person and for a server with no list
// (ADR-20260929-f4n rules 14 and 15).
func TestRefusalsCannotFloodTheLog(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		long := "service_account:" + strings.Repeat("a", 200)

		for name, people := range map[string]People{"not a person": theBoard(), "no list": peopleFake{}} {
			held := askingAboutAbsent(t, true, people)

			subject := long
			if name == "no list" {
				subject = "user:" + strings.Repeat("a", 200)
			}

			for range 20 * absentBurst {
				_, err := held.access.Check(t.Context(), asking(subject, "operate"))
				require.Error(t, err)
			}

			lines := strings.Count(held.log.String(), "\n")
			require.LessOrEqual(t, lines, absentBurst+1, "%s: the refusals wrote %d lines", name, lines)
			require.Equal(t, 1, strings.Count(held.log.String(), "passed its bound"), name)
		}
	})
}

// At a steady rate a module passes one question and is refused the next, again
// and again. The warning is written once for that run and then at most once a
// minute, with the count of the ones left out (ADR-20260929-f4n rule 15).
func TestTheBoundIsWarnedOnceARun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		held := askingAboutAbsent(t, true, theBoard())

		for range absentBurst {
			_, err := held.access.Check(t.Context(), asking("user:alice", "operate"))
			require.NoError(t, err)
		}

		for range 100 {
			time.Sleep(100 * time.Millisecond)

			_, err := held.access.Check(t.Context(), asking("user:alice", "operate"))
			require.NoError(t, err)

			_, err = held.access.Check(t.Context(), asking("user:alice", "operate"))
			require.ErrorIs(t, err, accessapi.ErrTooManyQuestions)
		}

		require.Equal(t, 1, strings.Count(held.log.String(), "passed its bound"))

		time.Sleep(quietInterval)

		for range 2 * absentBurst {
			_, _ = held.access.Check(t.Context(), asking("user:alice", "operate"))
		}

		require.Equal(t, 2, strings.Count(held.log.String(), "passed its bound"))
		require.Contains(t, held.log.String(), "left_out=99")
	})
}

// A lookup that fails on the served path is logged, capped and at most once a
// minute, so it is seen and cannot flood the log.
func TestAFailedLookupOnTheServedPathIsLoggedQuietly(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		broken := theBoard()
		broken.err = errors.New("the disk is gone")
		held := askingAboutAbsent(t, false, broken)
		held.host.flights.board(sinkRef(t, "queue"), "user:alice", nil)()

		for range 100 {
			_, err := held.access.Check(t.Context(), asking("user:alice", "operate"))
			require.Error(t, err)
		}

		require.Equal(t, 1, strings.Count(held.log.String(), "the person could not be looked up"))
	})
}

// A lookup that fails is a failure of the host and never a no, which a queue
// would read as the person losing access. It is audited like any answer
// (ADR-20260929-f4n rule 14).
func TestAFailedLookupIsNotAnAnswer(t *testing.T) {
	t.Parallel()

	broken := theBoard()
	broken.err = errors.New("the disk is gone")
	held := askingAboutAbsent(t, true, broken)

	_, err := held.access.Check(t.Context(), asking("user:alice", "operate"))
	require.Error(t, err)
	require.NotErrorIs(t, err, accessapi.ErrUnknownSubject)
	require.Contains(t, held.log.String(), "the person could not be looked up")
}

// Every question on this path writes one audit line, and a question about a
// person the instance is serving writes none (ADR-20260929-f4n rules 8 and 14).
func TestEachQuestionAboutAnAbsentPersonIsAudited(t *testing.T) {
	t.Parallel()

	held := askingAboutAbsent(t, true, theBoard())

	_, err := held.access.Check(t.Context(), asking("user:alice", "operate"))
	require.NoError(t, err)

	line := held.log.String()
	require.Equal(t, 1, strings.Count(line, "a module asked about a person it is not serving"))

	for _, want := range []string{
		"resource=sink:queue", "subject=user:alice", "action=operate", "object=device:p1", "answer=yes",
	} {
		require.Contains(t, line, want)
	}

	held.log.Reset()
	held.host.flights.board(sinkRef(t, "queue"), "user:bob", nil)()

	_, err = held.access.Check(t.Context(), asking("user:bob", "operate"))
	require.NoError(t, err)
	require.Empty(t, held.log.String(), "a question about a served person was audited as absent")
}

// A value longer than the published bounds can still reach the host from
// inside this process. The audit line caps every field, so one question
// cannot write a long line (ADR-20260929-f4n rule 14).
func TestTheAuditLineIsCapped(t *testing.T) {
	t.Parallel()

	held := askingAboutAbsent(t, true, theBoard())
	huge := strings.Repeat("x", 900<<10)

	_, err := held.access.Check(t.Context(), accessapi.CheckParams{
		Subject: "user:alice", Action: huge, ObjectType: huge, ObjectID: huge,
	})
	require.NoError(t, err)

	_, err = held.access.Check(t.Context(), asking("user:"+huge+"#", "operate"))
	require.Error(t, err)

	require.Less(t, held.log.Len(), 8*maxLoggedBytes, "an audit line carries what the module sent in full")
}

// One instance holds absentBurst questions and earns absentPerSecond more each
// second. Past that it is refused with its own code and logged once. A burst
// cannot be doubled at a window edge, because there is no window (ADR-20260929-f4n
// rule 15).
func TestQuestionsAboutAbsentPeopleAreBounded(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		held := askingAboutAbsent(t, true, theBoard())

		passed := func() int {
			count := 0

			for range 2 * absentBurst {
				_, err := held.access.Check(t.Context(), asking("user:alice", "operate"))
				if err == nil {
					count++

					continue
				}

				require.ErrorIs(t, err, accessapi.ErrTooManyQuestions)
			}

			return count
		}

		require.Equal(t, absentBurst, passed())
		require.Equal(t, absentBurst, held.graph.asked, "a refused question reached the graph")
		require.Equal(t, 1, strings.Count(held.log.String(), "passed its bound"))

		// A tenth of a second earns one question, and no more.
		time.Sleep(100 * time.Millisecond)
		require.Equal(t, 1, passed())

		// A served person is not counted against the bound.
		held.host.flights.board(sinkRef(t, "queue"), "user:bob", nil)()

		_, err := held.access.Check(t.Context(), asking("user:bob", "operate"))
		require.NoError(t, err)

		// A full minute refills the whole bucket, and not more.
		time.Sleep(time.Minute)
		require.Equal(t, absentBurst, passed())
	})
}

// The declaration's key reaches the module the doors see, and an instance
// declared without it carries none.
func TestTheDeclarationCarriesThePermissionToTheModule(t *testing.T) {
	t.Parallel()

	declared := declaredAs(t, "sink", "queue", "com.github.acme.queue/queue", install.RuntimeService)
	declared.Spec.Service = &install.Service{ContractMajor: 1}

	require.False(t, moduleOf(declared).CheckAbsentPeople)

	declared.Spec.Service.CheckAbsentPeople = true
	require.True(t, moduleOf(declared).CheckAbsentPeople)
}
