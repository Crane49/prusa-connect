package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// accessCheckPath answers what the caller may do to one object. The subject is
// never in the request: it is the principal the middleware resolved. A caller
// may always ask about themselves, so this operation needs no permission of
// its own.
const accessCheckPath = "/api/v1/access/check"

// reviewableTypes is the enum the object_type member publishes: the object
// types an action is defined on. A struct tag cannot hold a call, so
// TestTheReviewableTypeEnumMatchesTheVocabulary reads this copy against the
// authorization vocabulary.
const reviewableTypes = "device,resource_kind"

// AccessReview is the authorization graph as the permission review reads it. It
// is declared here because this is where it is consumed, and *authz.Store
// satisfies it.
//
// There is no method taking a subject. A caller may ask what they themselves
// may do and nothing else, so the only subject this surface can reach is the
// one the middleware resolved.
type AccessReview interface {
	// ObjectActions returns every action this subject may perform on this
	// object. An object the subject holds nothing on and an object the server
	// does not have are the same empty answer.
	ObjectActions(ctx context.Context, who authz.Subject, object authz.Object) ([]authz.Action, error)

	// Epoch is the generation of the authorization graph the answer was
	// computed at. Any grant write moves it.
	Epoch() uint64
}

// accessCheckBody names the object and the action to decide.
//
// Subject is published so that a client who sends one reads a refusal. If the
// server ignored the member, the client would get an answer about the caller
// and take it for an answer about somebody else. A caller who could ask about
// another principal could enumerate the graph one bit at a time.
type accessCheckBody struct {
	ObjectType string `json:"object_type" enum:"device,resource_kind" doc:"The type of the object to decide about."`

	ObjectID string `json:"object_id" maxLength:"63" minLength:"1" doc:"The object's identifier, a DNS-1123 label. A printer id is what the fleet listing reports."`

	Action string `json:"action" maxLength:"63" minLength:"1" doc:"The action to decide, spelled as GET /api/v1/vocabularies/authorization names it. A role is not an action."`

	Subject string `json:"subject,omitempty" maxLength:"191" doc:"Must be absent. The subject is always the caller, and a request naming anybody else is refused rather than answered about the caller."`
}

// AccessDecision is what one caller may do to one object.
//
// Actions is the whole set and not only the action that was asked about,
// because both come from the one row this answer reads. A client rendering a
// panel for a printer therefore needs one request rather than one per control.
//
// The empty set is the refusal, and it is also the answer for an object this
// server does not have. Nothing here distinguishes the two.
type AccessDecision struct {
	Allowed bool `json:"allowed" doc:"Whether the caller may perform the action named in the request."`

	Actions []string `json:"actions" doc:"Every action the caller may perform on this object, in the order the authorization vocabulary declares them. Empty means the caller reaches nothing on it, which is also what an object this server does not have answers."`

	Epoch uint64 `json:"authorization_epoch" doc:"The generation of the authorization graph this answer was computed at. Any grant write moves it, so an answer cached under an older epoch describes permissions that have since changed."`
}

type accessCheckInput struct {
	Body accessCheckBody
}

type accessCheckOutput struct {
	Body AccessDecision
}

// accessErrors are the refusals this surface can answer with. 400 is the
// vocabulary's own: an action outside the closed set, an object type no action
// is defined on, or an id that is not a DNS-1123 label.
//
// There is no 403 and no 404. A caller may always ask what they themselves may
// do, and the answer for an object they reach nothing on is a decision rather
// than a refusal.
var accessErrors = []int{
	http.StatusBadRequest,
	http.StatusUnauthorized,
	http.StatusUnprocessableEntity,
}

// registerAccess publishes the permission review. Nil publishes nothing, which
// is a build wired with no authorization graph.
func registerAccess(api huma.API, review AccessReview) {
	if review == nil {
		return
	}

	register(api, huma.Operation{
		OperationID: "check-access",
		Method:      http.MethodPost,
		Path:        accessCheckPath,
		Summary:     "What the caller may do to one object",
		Description: "Answers whether the caller may perform one action on one object, and lists " +
			"every action they may perform on it. The subject is always the caller: this operation " +
			"cannot ask about anybody else, and it needs no permission of its own, because a caller " +
			"may always ask what they themselves may do.\n" +
			"\n" +
			"Use it to render an interface: grey a control the caller cannot use, instead of " +
			"attempting the action to find out. The fleet listing already carries the same set on " +
			"every printer row, so a client drawing a list of printers does not need this operation " +
			"at all.\n" +
			"\n" +
			"The answer is a hint and never the decision. The server checks the permission again " +
			"when the action is attempted, and a grant written in between changes what it says. " +
			"Compare `authorization_epoch` against the value on a cached answer to see whether one " +
			"has been.\n" +
			"\n" +
			"An object this server does not have answers exactly as an object the caller reaches " +
			"nothing on: `allowed` false and an empty `actions`. That is deliberate, and it is why " +
			"this operation cannot be used to find out which printers exist.\n" +
			"\n" +
			"A body naming a `subject` is refused. Asking what another principal may do is a " +
			"separate operation with a permission of its own, and this server has none.",
		Tags:   []string{"access"},
		Errors: accessErrors,
	}, func(ctx context.Context, in *accessCheckInput) (*accessCheckOutput, error) {
		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		object, action, err := in.Body.parse()
		if err != nil {
			return nil, err
		}

		held, err := review.ObjectActions(ctx, who, object)
		if err != nil {
			return nil, err
		}

		return &accessCheckOutput{Body: decisionOf(held, action, review.Epoch())}, nil
	})
}

// parse turns the body into the pair the graph decides about. The action is
// checked against the closed set and the object against the vocabulary and
// DNS-1123, so the graph never receives something the rest of the system could
// not have produced.
//
// Nothing here reads the directory. An id that is well formed is answered,
// whether or not this server has a printer by that name, so the answer does
// not reveal which printers exist.
func (b accessCheckBody) parse() (authz.Object, authz.Action, error) {
	if b.Subject != "" {
		return authz.Object{}, "", httpfault.RequestInvalid("body.subject",
			"expected the member to be absent",
			"this operation decides about the caller and about nobody else")
	}

	action := authz.Action(b.Action)
	if err := authz.ValidateAction(action); err != nil {
		return authz.Object{}, "", err
	}

	object, err := authz.NewObject(authz.Type(b.ObjectType), b.ObjectID)
	if err != nil {
		return authz.Object{}, "", err
	}

	return object, action, nil
}

// decisionOf renders the row the review read. allowed is derived from the same
// set the answer carries, so the two halves cannot disagree.
func decisionOf(held []authz.Action, action authz.Action, epoch uint64) AccessDecision {
	out := AccessDecision{Actions: make([]string, 0, len(held)), Epoch: epoch}

	for _, name := range held {
		out.Actions = append(out.Actions, name.String())

		if name == action {
			out.Allowed = true
		}
	}

	return out
}
