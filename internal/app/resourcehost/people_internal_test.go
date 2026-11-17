package resourcehost

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
)

// theListedBoard is the account list with the rows a listing answers.
func theListedBoard() peopleFake {
	board := theBoard()
	board.listed = []accessapi.Person{
		{Subject: "user:alice", Name: "Alice", Kind: accessapi.KindUser},
		{Subject: "user:bob", Name: "bob@example.com", Kind: accessapi.KindUser},
	}

	return board
}

// listingHost is a host whose list of people is the one given, with its log.
func listingHost(people People) (*Host, *bytes.Buffer) {
	log := &bytes.Buffer{}

	return &Host{cfg: Config{People: people, Logger: slog.New(slog.NewTextHandler(log, nil))}}, log
}

// The method is installed for an instance whose operator approved list_people,
// and for no other, so any other module reads CodeMethodNotFound (ADR-20261021-x2b).
func TestAccessPeopleIsServedOnlyWhenTheOperatorApprovedIt(t *testing.T) {
	t.Parallel()

	for approved, want := range map[bool]bool{true: true, false: false} {
		declared := declaredAs(t, ref.KindLink, "planner", "acme.example/planner", install.RuntimeService)
		declared.Spec.Service = &install.Service{ListPeople: approved}

		host := supervisingDeclarations(declared)
		host.cfg.People = theListedBoard()

		methods, err := host.servesTo(declared)
		require.NoError(t, err)

		_, served := methods[accessapi.MethodPeople]
		require.Equal(t, want, served, "approved %v", approved)
	}
}

// An approved instance reads every person the server keeps, and each listing
// writes one audit line naming the instance and the count.
func TestAnApprovedModuleListsThePeople(t *testing.T) {
	t.Parallel()

	host, log := listingHost(theListedBoard())

	directory := host.DirectoryFor(Module{Instance: sinkRef(t, "planner"), ListPeople: true})
	require.NotNil(t, directory)

	listed, err := directory.People(t.Context())
	require.NoError(t, err)
	require.Len(t, listed, 2)

	require.Equal(t, 1, strings.Count(log.String(), "a module listed the people this server holds"))
	require.Contains(t, log.String(), "rows=2")
	require.Contains(t, log.String(), "sink:planner")

	require.Nil(t, host.DirectoryFor(Module{Instance: sinkRef(t, "planner")}),
		"an instance without the approval was given a directory")
}

// A server that keeps no list of people, or none at all, lists nobody and
// says so in the audit line.
func TestAServerThatKeepsNoListListsNobody(t *testing.T) {
	t.Parallel()

	for name, people := range map[string]People{
		"no list kept": peopleFake{listed: theListedBoard().listed},
		"no list":      nil,
	} {
		host, log := listingHost(people)

		listed, err := host.DirectoryFor(Module{Instance: sinkRef(t, "planner"), ListPeople: true}).People(t.Context())
		require.NoError(t, err, name)
		require.Empty(t, listed, name)
		require.Contains(t, log.String(), "keeps no list of people", name)
	}
}

// A listing spends a token from the bucket questions about absent people
// spend, so a module cannot make the host read the accounts without end.
func TestAListingSpendsFromTheSameBound(t *testing.T) {
	t.Parallel()

	host, _ := listingHost(theListedBoard())
	directory := host.DirectoryFor(Module{Instance: sinkRef(t, "planner"), ListPeople: true})

	var refused error

	for range 2 * absentBurst {
		if _, err := directory.People(t.Context()); err != nil {
			refused = err

			break
		}
	}

	require.ErrorIs(t, refused, accessapi.ErrTooManyQuestions)
}

// A store that fails is reported to the caller and in the audit line.
func TestAListingThatFailsIsAudited(t *testing.T) {
	t.Parallel()

	board := theListedBoard()
	board.err = errors.New("the database is locked")

	host, log := listingHost(board)

	_, err := host.DirectoryFor(Module{Instance: sinkRef(t, "planner"), ListPeople: true}).People(t.Context())
	require.Error(t, err)
	require.Contains(t, log.String(), "failed: the people could not be read")
}
