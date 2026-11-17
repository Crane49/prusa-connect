package accessapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// accessFake is the host side under test. It records the question and answers
// what the case wants.
type accessFake struct {
	decision accessapi.Decision
	err      error
	saw      accessapi.CheckParams
	calls    int
}

func (a *accessFake) Check(
	_ context.Context, params accessapi.CheckParams,
) (accessapi.Decision, error) {
	a.saw = params
	a.calls++

	return a.decision, a.err
}

// call runs the handler the way a session would, and returns what crossed.
func call(t *testing.T, methods wirerpc.Methods, params any) (any, error) {
	t.Helper()

	handler, ok := methods[accessapi.MethodCheck]
	if !ok {
		t.Fatal("access.check is not in the method set")
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode the params: %v", err)
	}

	if params == nil {
		encoded = nil
	}

	return handler(t.Context(), wirerpc.Call{Method: accessapi.MethodCheck, Params: encoded})
}

// A question missing a part of itself, or over a published bound, is refused
// on both sides and never reaches the graph.
func TestAQuestionOutsideTheBoundsIsRefusedOnBothSides(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", accessapi.MaxObjectBytes+1)

	cases := map[string]accessapi.CheckParams{
		"no action":      {ObjectType: "device", ObjectID: "p1"},
		"no object type": {Action: "observe", ObjectID: "p1"},
		"no object":      {Action: "observe", ObjectType: "device"},
		"a long object":  {Action: "observe", ObjectType: "device", ObjectID: long},
		"a long object type": {
			Action:     "observe",
			ObjectType: strings.Repeat("d", accessapi.MaxObjectTypeBytes+1),
			ObjectID:   "p1",
		},
		"a long subject": {
			Subject:    strings.Repeat("a", accessapi.MaxSubjectBytes+1),
			Action:     "observe",
			ObjectType: "device",
			ObjectID:   "p1",
		},
	}

	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := params.Validate(); !errors.Is(err, accessapi.ErrInvalidQuestion) {
				t.Fatalf("the params were held to be %v, want %v", err, accessapi.ErrInvalidQuestion)
			}

			access := &accessFake{}

			_, err := call(t, accessapi.Handlers(access), params)

			var coded *wirerpc.Error
			if !errors.As(err, &coded) || coded.Code != accessapi.CodeInvalidQuestion {
				t.Fatalf("the host answered %v, want code %d", err, accessapi.CodeInvalidQuestion)
			}

			if access.calls != 0 {
				t.Fatal("a question outside the published bounds reached the graph")
			}
		})
	}
}

// The host reads the question as the module wrote it, and the answer is one
// bit.
func TestTheHostReadsTheQuestionAsItWasWritten(t *testing.T) {
	t.Parallel()

	access := &accessFake{decision: accessapi.Decision{Allowed: true}}

	params := accessapi.CheckParams{
		Subject: "user:alpha", Action: "operate", ObjectType: "device", ObjectID: "p1",
	}

	answered, err := call(t, accessapi.Handlers(access), params)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	if access.saw != params {
		t.Fatalf("the host was asked %+v, want %+v", access.saw, params)
	}

	decision, ok := answered.(accessapi.Decision)
	if !ok || !decision.Allowed {
		t.Fatalf("the answer is %+v, want the decision the host made", answered)
	}
}

// Every refusal the host raises crosses as the code this contract numbers.
func TestAnAccessRefusalCrossesAsItsSentinel(t *testing.T) {
	t.Parallel()

	cases := map[error]int{
		accessapi.ErrDenied:           accessapi.CodeDenied,
		accessapi.ErrUnknownSubject:   accessapi.CodeUnknownSubject,
		accessapi.ErrTooManyQuestions: accessapi.CodeTooManyQuestions,
	}

	for sentinel, code := range cases {
		t.Run(sentinel.Error(), func(t *testing.T) {
			t.Parallel()

			access := &accessFake{err: sentinel}

			_, err := call(t, accessapi.Handlers(access), accessapi.CheckParams{
				Subject: "user:alpha", Action: "operate", ObjectType: "device", ObjectID: "p1",
			})

			var coded *wirerpc.Error
			if !errors.As(err, &coded) || coded.Code != code {
				t.Fatalf("the refusal crossed as %v, want code %d", err, code)
			}
		})
	}
}

// callerFake is a session that answers every call with one refusal.
type callerFake struct{ err error }

func (c callerFake) Call(context.Context, wirerpc.Method, any) (json.RawMessage, error) {
	return nil, c.err
}

// A module reads each coded refusal back as the sentinel the host raised, so
// it branches with errors.Is and never on the message.
func TestAClientReadsEachCodeAsItsSentinel(t *testing.T) {
	t.Parallel()

	cases := map[int]error{
		accessapi.CodeDenied:           accessapi.ErrDenied,
		accessapi.CodeUnknownSubject:   accessapi.ErrUnknownSubject,
		accessapi.CodeInvalidQuestion:  accessapi.ErrInvalidQuestion,
		accessapi.CodeTooManyQuestions: accessapi.ErrTooManyQuestions,
	}

	for code, sentinel := range cases {
		t.Run(sentinel.Error(), func(t *testing.T) {
			t.Parallel()

			client := accessapi.NewClient(callerFake{err: &wirerpc.Error{Code: code, Message: "refused"}})

			_, err := client.Check(t.Context(), accessapi.CheckParams{
				Subject: "user:alpha", Action: "operate", ObjectType: "device", ObjectID: "p1",
			})
			if !errors.Is(err, sentinel) {
				t.Fatalf("code %d reached the module as %v, want %v", code, err, sentinel)
			}
		})
	}
}

// A failure of the host's own crosses as one sentence and never as its text.
func TestAnAccessHostFailureCrossesAsOneSentence(t *testing.T) {
	t.Parallel()

	access := &accessFake{err: errors.New("the sqlite file is locked by pid 4021")}

	_, err := call(t, accessapi.Handlers(access), accessapi.CheckParams{
		Subject: "user:alpha", Action: "operate", ObjectType: "device", ObjectID: "p1",
	})

	var coded *wirerpc.Error
	if !errors.As(err, &coded) || coded.Message != accessapi.HostFailed {
		t.Fatalf("the failure crossed as %v, want %q", err, accessapi.HostFailed)
	}
}

// A host that answers no such question installs no method.
func TestAHostWithNoAccessInstallsNoMethod(t *testing.T) {
	t.Parallel()

	if held := accessapi.Handlers(nil); len(held) != 0 {
		t.Fatalf("a host that answers no such question installed %v", held)
	}
}

// directoryFake is the host's list of people under test.
type directoryFake struct {
	people []accessapi.Person
	err    error
}

func (d directoryFake) People(context.Context) ([]accessapi.Person, error) {
	return d.people, d.err
}

// sessionFake hands a client's call straight to a host method set, so a test
// makes the whole round trip through JSON without a session.
type sessionFake struct{ methods wirerpc.Methods }

func (s sessionFake) Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error) {
	handler, ok := s.methods[method]
	if !ok {
		return nil, &wirerpc.Error{Code: wirerpc.CodeMethodNotFound, Message: "no such method"}
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	answer, err := handler(ctx, wirerpc.Call{Method: method, Params: encoded})
	if err != nil {
		return nil, err
	}

	return json.Marshal(answer)
}

// A module reads the people the host holds, sorted by name, with the three
// fields the contract publishes and nothing else.
func TestAModuleListsThePeopleSortedByName(t *testing.T) {
	t.Parallel()

	directory := directoryFake{people: []accessapi.Person{
		{Subject: "user:2", Name: "Zdena", Kind: accessapi.KindUser},
		{Subject: "user:3", Name: "Adam", Kind: accessapi.KindUser},
		{Subject: "user:1", Name: "Adam", Kind: accessapi.KindUser},
	}}

	client := accessapi.NewClient(sessionFake{methods: accessapi.PeopleHandlers(directory)})

	people, err := client.People(t.Context())
	if err != nil {
		t.Fatalf("people: %v", err)
	}

	want := []accessapi.Person{
		{Subject: "user:1", Name: "Adam", Kind: accessapi.KindUser},
		{Subject: "user:3", Name: "Adam", Kind: accessapi.KindUser},
		{Subject: "user:2", Name: "Zdena", Kind: accessapi.KindUser},
	}

	if len(people) != len(want) {
		t.Fatalf("the module read %+v, want %+v", people, want)
	}

	for i := range want {
		if people[i] != want[i] {
			t.Fatalf("row %d is %+v, want %+v", i, people[i], want[i])
		}
	}

	encoded, err := json.Marshal(accessapi.People{People: want[:1]})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if string(encoded) != `{"people":[{"subject":"user:1","name":"Adam","kind":"user"}]}` {
		t.Fatalf("the answer crosses as %s", encoded)
	}
}

// A host that holds nobody answers an empty list, never null, so a module
// reads no people rather than a failure.
func TestAHostThatHoldsNobodyAnswersAnEmptyList(t *testing.T) {
	t.Parallel()

	answered, err := accessapi.PeopleHandlers(directoryFake{})[accessapi.MethodPeople](
		t.Context(), wirerpc.Call{Method: accessapi.MethodPeople})
	if err != nil {
		t.Fatalf("people: %v", err)
	}

	encoded, err := json.Marshal(answered)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if string(encoded) != `{"people":[]}` {
		t.Fatalf("an empty server answered %s", encoded)
	}
}

// The host answers at most MaxPeople rows, and a client refuses an answer
// longer than that.
func TestTheListOfPeopleIsBoundedOnBothSides(t *testing.T) {
	t.Parallel()

	many := make([]accessapi.Person, accessapi.MaxPeople+3)
	for i := range many {
		many[i] = accessapi.Person{Subject: "user:x", Name: "x", Kind: accessapi.KindUser}
	}

	client := accessapi.NewClient(sessionFake{methods: accessapi.PeopleHandlers(directoryFake{people: many})})

	people, err := client.People(t.Context())
	if err != nil {
		t.Fatalf("people: %v", err)
	}

	if len(people) != accessapi.MaxPeople {
		t.Fatalf("the host answered %d rows, want %d", len(people), accessapi.MaxPeople)
	}

	long, err := json.Marshal(accessapi.People{People: many})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	_, err = accessapi.NewClient(rawCaller{body: long}).People(t.Context())
	if err == nil || !strings.Contains(err.Error(), "the limit is") {
		t.Fatalf("a client read an answer over the bound as %v", err)
	}
}

// rawCaller answers every call with the same body.
type rawCaller struct{ body json.RawMessage }

func (r rawCaller) Call(context.Context, wirerpc.Method, any) (json.RawMessage, error) {
	return r.body, nil
}

// A failure of the host's own crosses as one sentence, and a host that lists
// nobody installs no method, so the module reads CodeMethodNotFound.
func TestAListingRefusalCrossesAsTheContractSaysIt(t *testing.T) {
	t.Parallel()

	failing := accessapi.PeopleHandlers(directoryFake{err: errors.New("the sqlite file is locked by pid 4021")})

	_, err := failing[accessapi.MethodPeople](t.Context(), wirerpc.Call{Method: accessapi.MethodPeople})

	var coded *wirerpc.Error
	if !errors.As(err, &coded) || coded.Message != accessapi.HostFailed {
		t.Fatalf("the failure crossed as %v, want %q", err, accessapi.HostFailed)
	}

	if held := accessapi.PeopleHandlers(nil); len(held) != 0 {
		t.Fatalf("a host that lists nobody installed %v", held)
	}

	_, err = accessapi.NewClient(sessionFake{methods: accessapi.Handlers(&accessFake{})}).People(t.Context())
	if !errors.As(err, &coded) || coded.Code != wirerpc.CodeMethodNotFound {
		t.Fatalf("an instance not served the method read %v, want code %d", err, wirerpc.CodeMethodNotFound)
	}
}
