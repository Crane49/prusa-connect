package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// fakeAccessReview answers from a table keyed by the rendered object, which is
// what a caller names in the request. An object with no entry answers the empty
// set, which is what the graph answers for an object nobody holds anything on
// and for an object the server does not have.
type fakeAccessReview struct {
	mu     sync.Mutex
	held   map[string][]authz.Action
	epoch  uint64
	asked  []authz.Subject
	refuse error
}

var _ AccessReview = (*fakeAccessReview)(nil)

func (f *fakeAccessReview) ObjectActions(
	_ context.Context, who authz.Subject, object authz.Object,
) ([]authz.Action, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.refuse != nil {
		return nil, f.refuse
	}

	f.asked = append(f.asked, who)

	return f.held[object.String()], nil
}

func (f *fakeAccessReview) Epoch() uint64 { return f.epoch }

func (f *fakeAccessReview) subjects() []authz.Subject {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]authz.Subject(nil), f.asked...)
}

// accessDecisionBody is the answer as a client parses it, rather than as this
// package declares it, so a member renamed on the wire fails here.
type accessDecisionBody struct {
	Allowed bool     `json:"allowed"`
	Actions []string `json:"actions"`
	Epoch   uint64   `json:"authorization_epoch"`
}

// reviewingServer is a server whose only interesting wiring is the review.
func reviewingServer(t *testing.T, review AccessReview) *Server {
	t.Helper()

	cfg := exampleConfig(noPrinters{}, seededStore(), seededPresets())
	cfg.Access = review

	server, err := New(cfg)
	require.NoError(t, err)

	return server
}

// checkAccess sends one review request through the real router.
func checkAccess(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()

	return call(t, handler, http.MethodPost, accessCheckPath, body)
}

// TestTheCheckAnswersTheActionAskedAboutAndTheWholeSet drives the operation
// through the real router and reads what a client would read.
func TestTheCheckAnswersTheActionAskedAboutAndTheWholeSet(t *testing.T) {
	t.Parallel()

	review := &fakeAccessReview{
		epoch: 7,
		held: map[string][]authz.Action{
			"device:p-alpha": {authz.ActionObserve, authz.ActionOperate},
		},
	}

	handler := reviewingServer(t, review).Handler()

	rec := checkAccess(t, handler,
		`{"object_type":"device","object_id":"p-alpha","action":"operate"}`)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var body accessDecisionBody

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.True(t, body.Allowed)
	require.Equal(t, []string{"observe", "operate"}, body.Actions)
	require.Equal(t, uint64(7), body.Epoch)

	// The subject reaching the graph is the one the middleware resolved and
	// never one the request named.
	alice, err := authz.ParseSubject("user:alice")
	require.NoError(t, err)
	require.Equal(t, []authz.Subject{alice}, review.subjects())
}

// TestAnActionTheCallerDoesNotHoldIsAnsweredAndNotRefused is the property that
// separates a review from an attempt: the answer is 200 and false, because the
// caller asked a question rather than tried to do something.
func TestAnActionTheCallerDoesNotHoldIsAnsweredAndNotRefused(t *testing.T) {
	t.Parallel()

	review := &fakeAccessReview{
		epoch: 3,
		held:  map[string][]authz.Action{"device:p-alpha": {authz.ActionObserve}},
	}

	rec := checkAccess(t, reviewingServer(t, review).Handler(),
		`{"object_type":"device","object_id":"p-alpha","action":"operate"}`)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var body accessDecisionBody

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.False(t, body.Allowed)
	require.Equal(t, []string{"observe"}, body.Actions)
}

// TestAnAbsentObjectAndAnUnreachableOneAnswerTheSameBytes is the existence leak
// this operation is the obvious way to open. Both answers are compared as raw
// bytes rather than as decoded values, because a difference in a member's
// presence is exactly what a probe would read.
func TestAnAbsentObjectAndAnUnreachableOneAnswerTheSameBytes(t *testing.T) {
	t.Parallel()

	review := &fakeAccessReview{
		epoch: 11,
		held:  map[string][]authz.Action{"device:p-alpha": {authz.ActionObserve}},
	}

	handler := reviewingServer(t, review).Handler()

	unreachable := checkAccess(t, handler,
		`{"object_type":"device","object_id":"p-beta","action":"observe"}`)
	absent := checkAccess(t, handler,
		`{"object_type":"device","object_id":"p-not-a-printer","action":"observe"}`)

	require.Equal(t, http.StatusOK, unreachable.Code, "body: %s", unreachable.Body.String())
	require.Equal(t, absent.Code, unreachable.Code)
	require.Equal(t, absent.Body.String(), unreachable.Body.String())
	require.Equal(t, absent.Header().Get("Content-Type"), unreachable.Header().Get("Content-Type"))

	// The reachable object is what proves the two above are the review
	// answering rather than the operation failing the same way twice.
	reachable := checkAccess(t, handler,
		`{"object_type":"device","object_id":"p-alpha","action":"observe"}`)
	require.NotEqual(t, unreachable.Body.String(), reachable.Body.String())
}

// TestAnEmptyActionSetIsAnArrayAndNotNull holds the wire shape a client
// iterates without a null check.
func TestAnEmptyActionSetIsAnArrayAndNotNull(t *testing.T) {
	t.Parallel()

	rec := checkAccess(t, reviewingServer(t, &fakeAccessReview{}).Handler(),
		`{"object_type":"device","object_id":"p-alpha","action":"observe"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"actions":[]`)
	require.NotContains(t, rec.Body.String(), `"actions":null`)
}

// TestABodyNamingAnotherSubjectIsRefused keeps the operation self-scoped. A
// member this server ignored while answering about the caller would read to its
// sender as an answer about the principal they named.
func TestABodyNamingAnotherSubjectIsRefused(t *testing.T) {
	t.Parallel()

	review := &fakeAccessReview{
		held: map[string][]authz.Action{"device:p-alpha": {authz.ActionAdminister}},
	}

	rec := checkAccess(t, reviewingServer(t, review).Handler(),
		`{"object_type":"device","object_id":"p-alpha","action":"administer","subject":"user:root"}`)

	body := requireProblem(t, rec, http.StatusUnprocessableEntity)
	require.Equal(t, httpfault.CodeRequestInvalid(), body.Code)
	require.Contains(t, body.Params.Keys(), "location",
		"the refusal names the member, so a client knows which one to drop")
	require.Empty(t, review.subjects(), "the graph was asked about a subject the body named")
}

// TestAnActionOutsideTheVocabularyIsRefusedByName is what tells a client it
// misspelt an action rather than that it holds nothing.
func TestAnActionOutsideTheVocabularyIsRefusedByName(t *testing.T) {
	t.Parallel()

	rec := checkAccess(t, reviewingServer(t, &fakeAccessReview{}).Handler(),
		`{"object_type":"device","object_id":"p-alpha","action":"read_filament_sensor"}`)

	body := requireProblem(t, rec, http.StatusBadRequest)
	require.Equal(t, authz.CodeUnknownAction(), body.Code)
}

// TestAnObjectTypeCarryingNoActionsIsRefusedByTheEnum stops a caller reading
// "you may do nothing on org:acme" as an answer about an org.
func TestAnObjectTypeCarryingNoActionsIsRefusedByTheEnum(t *testing.T) {
	t.Parallel()

	rec := checkAccess(t, reviewingServer(t, &fakeAccessReview{}).Handler(),
		`{"object_type":"org","object_id":"acme","action":"administer"}`)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body: %s", rec.Body.String())
}

// TestAnIdentifierOutsideTheGrammarIsRefused holds the one validation this
// operation does perform. It reads the id's spelling and never the directory,
// so it says nothing about what exists.
func TestAnIdentifierOutsideTheGrammarIsRefused(t *testing.T) {
	t.Parallel()

	rec := checkAccess(t, reviewingServer(t, &fakeAccessReview{}).Handler(),
		`{"object_type":"device","object_id":"NOT A LABEL","action":"administer"}`)

	body := requireProblem(t, rec, http.StatusBadRequest)
	require.Equal(t, authz.CodeInvalidID(), body.Code)
}

// TestTheCheckTakesAnIdentity is the half that makes a self-scoped operation
// useful: it is unprivileged, not unauthenticated. Without an identity there is
// no self to ask about, and the handler refuses before the graph is reached.
//
// Two mechanisms produce the refusal and either alone is enough: the middleware
// reads the operation's security requirement, and the handler refuses a context
// carrying no principal. Breaking one leaves this green, so a reader checking
// the gate has to break both.
func TestTheCheckTakesAnIdentity(t *testing.T) {
	t.Parallel()

	review := &fakeAccessReview{
		held: map[string][]authz.Action{"device:p-alpha": {authz.ActionAdminister}},
	}

	cfg := exampleConfig(noPrinters{}, seededStore(), seededPresets())
	cfg.Access = review
	cfg.Principals = fixedPrincipal("")

	server, err := New(cfg)
	require.NoError(t, err)

	rec := checkAccess(t, server.Handler(),
		`{"object_type":"device","object_id":"p-alpha","action":"administer"}`)
	requireProblem(t, rec, http.StatusUnauthorized)
	require.Empty(t, review.subjects(), "the graph answered a request carrying no identity")
}

// TestTheCheckOperationCarriesTheRootSecurityRequirement holds the published
// document to what the middleware enforces. An operation declaring the empty
// requirement is one principalMiddleware lets through unauthenticated.
func TestTheCheckOperationCarriesTheRootSecurityRequirement(t *testing.T) {
	t.Parallel()

	op := reviewingServer(t, &fakeAccessReview{}).api.OpenAPI().Paths[accessCheckPath].Post
	require.NotNil(t, op)
	require.Nil(t, op.Security, "the check inherits the root requirement and declares none of its own")
}

// TestTheReviewableTypeEnumMatchesTheVocabulary reads the enum the body
// publishes against the object types an action is defined on. A type added to
// the vocabulary and not to the enum is an object a caller cannot ask about.
func TestTheReviewableTypeEnumMatchesTheVocabulary(t *testing.T) {
	t.Parallel()

	var carry []string

	for _, typ := range authz.Types() {
		if _, ok := authz.AdministerAction(typ); ok {
			carry = append(carry, string(typ))
		}
	}

	require.NotEmpty(t, carry, "no object type carries an action, so this gate reads nothing")
	require.Equal(t, carry, strings.Split(reviewableTypes, ","))
}

// TestTheReviewIsNotPublishedWithoutAGraph holds the nil case: a build wired
// with no review serves no check path, rather than one that answers about
// nothing.
func TestTheReviewIsNotPublishedWithoutAGraph(t *testing.T) {
	t.Parallel()

	cfg := exampleConfig(noPrinters{}, seededStore(), seededPresets())
	cfg.Access = nil

	server, err := New(cfg)
	require.NoError(t, err)

	require.NotContains(t, server.api.OpenAPI().Paths, accessCheckPath)

	rec := checkAccess(t, server.Handler(),
		`{"object_type":"device","object_id":"p-alpha","action":"administer"}`)
	require.Equal(t, http.StatusNotFound, rec.Code)
}
