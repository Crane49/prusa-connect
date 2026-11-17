// Package accessapi is how a module asks the host whether one principal may
// perform one action on one object.
//
// A module that acts later than the request that asked for it, such as a
// print queue, calls Check before it acts.
//
// The host refuses a question outside two bounds. The action must be in the
// instance's own permissions list. The subject must be the instance itself or
// one the host passed to it on routes.serve since it started. With the
// check_absent_people permission the subject may be any person of the server,
// user:<id>. A link may also ask about a server it reported, node:<id>.
//
// An unknown object answers false, as an object the subject cannot reach. A
// deleted person answers false too.
//
// With the list_people permission a module may list the people with People.
package accessapi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MethodCheck asks whether one subject may perform one action on one object.
const MethodCheck wirerpc.Method = "access.check"

// MethodPeople lists the people of this server, with their names.
const MethodPeople wirerpc.Method = "access.people"

// MaxPeople is the most rows of one MethodPeople answer. A server with more
// people answers the first MaxPeople by name, and a client refuses a longer
// answer.
const MaxPeople = 5000

// KindUser is the kind of a row for a person who signs in to this server.
const KindUser = "user"

// The bounds on one question.
const (
	// MaxSubjectBytes bounds a principal.
	MaxSubjectBytes = 256

	// MaxActionBytes bounds an action name.
	MaxActionBytes = 64

	// MaxObjectTypeBytes bounds an object's type.
	MaxObjectTypeBytes = 64

	// MaxObjectBytes bounds an object's identifier.
	MaxObjectBytes = 128
)

// The error codes of this contract. They are outside the range JSON-RPC 2.0
// reserves and outside the ranges of the other contracts. MethodPeople has no
// code of its own.
const (
	// CodeDenied is ErrDenied.
	CodeDenied = 6000
	// CodeUnknownSubject is ErrUnknownSubject.
	CodeUnknownSubject = 6001
	// CodeInvalidQuestion is ErrInvalidQuestion.
	CodeInvalidQuestion = 6002
	// CodeTooManyQuestions is ErrTooManyQuestions.
	CodeTooManyQuestions = 6003
)

// The refusals of this contract. A module branches on them with errors.Is.
var (
	// ErrDenied reports a question the module may not ask: an action outside
	// the permissions list the operator installed the instance with.
	ErrDenied = errors.New("accessapi: the instance may not ask about that action")
	// ErrUnknownSubject reports a subject the host has not passed to this
	// instance since it started. It is not an answer about the subject. The
	// host forgets subjects when the instance restarts.
	ErrUnknownSubject = errors.New("accessapi: this instance has not served that subject")
	// ErrInvalidQuestion reports a question outside the bounds of this
	// contract, and one the authorization vocabulary cannot read.
	ErrInvalidQuestion = errors.New("accessapi: the question is outside the published bounds")
	// ErrTooManyQuestions reports an instance that asked about people it is
	// not serving faster than the host allows. It is not an answer about the
	// subject. Ask again later.
	ErrTooManyQuestions = errors.New("accessapi: the instance asked too many questions about people it is not serving")
)

// CheckParams is the params of MethodCheck. Subject is the principal to
// decide about, and empty is the instance itself. Action is a name of the
// authorization vocabulary. ObjectType and ObjectID name the object: a device
// by the id the fleet listing reports, or a resource kind by its name.
type CheckParams struct {
	Subject string `json:"subject,omitempty"`

	Action string `json:"action"`

	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
}

// Validate checks one question against the bounds of this contract. The host
// checks them again.
func (p CheckParams) Validate() error {
	switch {
	case len(p.Subject) > MaxSubjectBytes:
		return fmt.Errorf("%w: the subject is %d bytes and the limit is %d",
			ErrInvalidQuestion, len(p.Subject), MaxSubjectBytes)
	case p.Action == "":
		return fmt.Errorf("%w: the question names no action", ErrInvalidQuestion)
	case len(p.Action) > MaxActionBytes:
		return fmt.Errorf("%w: the action is %d bytes and the limit is %d",
			ErrInvalidQuestion, len(p.Action), MaxActionBytes)
	case p.ObjectType == "":
		return fmt.Errorf("%w: the question names no object type", ErrInvalidQuestion)
	case len(p.ObjectType) > MaxObjectTypeBytes:
		return fmt.Errorf("%w: the object type is %d bytes and the limit is %d",
			ErrInvalidQuestion, len(p.ObjectType), MaxObjectTypeBytes)
	case p.ObjectID == "":
		return fmt.Errorf("%w: the question names no object", ErrInvalidQuestion)
	case len(p.ObjectID) > MaxObjectBytes:
		return fmt.Errorf("%w: the object id is %d bytes and the limit is %d",
			ErrInvalidQuestion, len(p.ObjectID), MaxObjectBytes)
	}

	return nil
}

// Decision is the answer of MethodCheck: one yes or no, never the set of
// actions the subject has.
type Decision struct {
	Allowed bool `json:"allowed"`
}

// Access is the host's side of this contract. The host supplies it and a
// module never constructs one.
type Access interface {
	Check(ctx context.Context, params CheckParams) (Decision, error)
}

// Person is one row of a MethodPeople answer. Subject is the principal
// access.check takes, user:<id>. Name is the display name, or the username
// when there is none. Kind is KindUser. A row has no e-mail address, no role
// and no grant.
type Person struct {
	Subject string `json:"subject"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
}

// People is what MethodPeople answers.
type People struct {
	People []Person `json:"people"`
}

// Directory is the host's side of MethodPeople. The host supplies it and a
// module never constructs one. It answers every person of the server, and
// the handler sorts and bounds the rows.
type Directory interface {
	People(ctx context.Context) ([]Person, error)
}

// Caller is the part of a wirerpc session this contract needs.
type Caller interface {
	Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error)
}

// Client asks the host on behalf of a module.
type Client struct {
	conn Caller
}

// NewClient returns the client a module asks through. It makes no call.
func NewClient(conn Caller) *Client {
	return &Client{conn: conn}
}

// Check answers whether the subject may perform the action on the object. The
// answer is not a guarantee: the server checks the permission again when the
// action is attempted. An object the server does not have answers false.
func (c *Client) Check(ctx context.Context, params CheckParams) (Decision, error) {
	if err := params.Validate(); err != nil {
		return Decision{}, fmt.Errorf("accessapi: %s: %w", MethodCheck, err)
	}

	var decision Decision
	if err := c.call(ctx, MethodCheck, params, &decision); err != nil {
		return Decision{}, err
	}

	return decision, nil
}

// People lists the people of this server, sorted by name. The list can change
// after the call, so check a kept subject with Check before acting for it.
func (c *Client) People(ctx context.Context) ([]Person, error) {
	var answer People
	if err := c.call(ctx, MethodPeople, struct{}{}, &answer); err != nil {
		return nil, err
	}

	if len(answer.People) > MaxPeople {
		return nil, fmt.Errorf("accessapi: %s: the answer holds %d people and the limit is %d",
			MethodPeople, len(answer.People), MaxPeople)
	}

	return answer.People, nil
}

// call makes one round trip and decodes the answer, mapping a coded refusal
// back to the sentinel the host raised.
func (c *Client) call(ctx context.Context, method wirerpc.Method, params, into any) error {
	if c.conn == nil {
		return fmt.Errorf("accessapi: %s: the client holds no session", method)
	}

	body, err := c.conn.Call(ctx, method, params)
	if err != nil {
		if coded, ok := errors.AsType[*wirerpc.Error](err); ok {
			if sentinel := sentinelFor(coded.Code); sentinel != nil {
				return fmt.Errorf("%s: %w", method, &remote{sentinel: sentinel, message: coded.Message})
			}
		}

		return fmt.Errorf("accessapi: %s: %w", method, err)
	}

	if len(body) == 0 {
		return nil
	}

	if err = json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("accessapi: %s: read the answer: %w", method, err)
	}

	return nil
}

// remote is the host's message with this contract's sentinel.
type remote struct {
	sentinel error
	message  string
}

func (r *remote) Error() string { return r.message }
func (r *remote) Unwrap() error { return r.sentinel }

func sentinelFor(code int) error {
	switch code {
	case CodeDenied:
		return ErrDenied
	case CodeUnknownSubject:
		return ErrUnknownSubject
	case CodeInvalidQuestion:
		return ErrInvalidQuestion
	case CodeTooManyQuestions:
		return ErrTooManyQuestions
	}

	return nil
}

// HostFailed is the message a module gets when a call failed inside the host.
const HostFailed = "accessapi: the host could not answer"

// refusal renders a host-side error as a code of this contract.
func refusal(err error) error {
	coded := []struct {
		code     int
		sentinel error
	}{
		{CodeUnknownSubject, ErrUnknownSubject},
		{CodeDenied, ErrDenied},
		{CodeInvalidQuestion, ErrInvalidQuestion},
		{CodeTooManyQuestions, ErrTooManyQuestions},
	}

	for _, known := range coded {
		if errors.Is(err, known.sentinel) {
			return &wirerpc.Error{Code: known.code, Message: err.Error()}
		}
	}

	return &wirerpc.Error{Code: wirerpc.CodeInternal, Message: HostFailed}
}

// Handlers is the host's method set for this contract, bound to one module.
// The set is empty for a nil access.
func Handlers(access Access) wirerpc.Methods {
	if access == nil {
		return wirerpc.Methods{}
	}

	return wirerpc.Methods{MethodCheck: checkHandler(access)}
}

// PeopleHandlers is the host's MethodPeople method, bound to one module. The
// host installs it only for an instance whose operator approved list_people.
// The set is empty for a nil directory.
func PeopleHandlers(directory Directory) wirerpc.Methods {
	if directory == nil {
		return wirerpc.Methods{}
	}

	return wirerpc.Methods{MethodPeople: peopleHandler(directory)}
}

func peopleHandler(directory Directory) wirerpc.Handler {
	return func(ctx context.Context, _ wirerpc.Call) (any, error) {
		held, err := directory.People(ctx)
		if err != nil {
			return nil, refusal(err)
		}

		rows := slices.Clone(held)
		slices.SortStableFunc(rows, func(a, b Person) int {
			if order := cmp.Compare(a.Name, b.Name); order != 0 {
				return order
			}

			return cmp.Compare(a.Subject, b.Subject)
		})

		// The bound counts rows, so the cut drops whole people and never cuts
		// a name.
		if len(rows) > MaxPeople {
			rows = slices.Delete(rows, MaxPeople, len(rows))
		}

		if rows == nil {
			rows = []Person{}
		}

		return People{People: rows}, nil
	}
}

func checkHandler(access Access) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var params CheckParams

		if len(call.Params) == 0 {
			return nil, badParams(MethodCheck)
		}

		if err := json.Unmarshal(call.Params, &params); err != nil {
			return nil, badParams(MethodCheck)
		}

		if err := params.Validate(); err != nil {
			return nil, refusal(err)
		}

		decision, err := access.Check(ctx, params)
		if err != nil {
			return nil, refusal(err)
		}

		return decision, nil
	}
}

func badParams(method wirerpc.Method) error {
	return &wirerpc.Error{
		Code:    wirerpc.CodeInvalidParams,
		Message: string(method) + " carries a params object",
	}
}
