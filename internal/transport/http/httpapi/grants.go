package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The grant paths. The object is two segments, and not the one "type:id"
// string the graph renders. The resource path is kind and name for the same
// reason: a bad value is refused as the parameter it is, and a path a person
// types is the path they read back in a refusal.
const (
	grantsPath = "/api/v1/grants/{object_type}/{object_id}"
)

// administrableTypes is the enum the object_type parameter publishes: the
// object types the graph can decide an administrator for, which is what
// administering their grants requires. A struct tag cannot hold a call, so
// TestTheObjectTypeEnumMatchesWhatCanBeAdministered reads this copy against the
// vocabulary.
//
// org is here although nobody performs an action on an org. It is administered
// by the admin role written on it. It is the only object a fleet-wide grant
// can be written on: one org tuple covers every printer the org holds,
// including one admitted later.
const administrableTypes = "org,device,resource_kind"

// GrantStore is the authorization graph as the grant surface reads and writes
// it. It is declared here because this is where it is consumed, and
// *authz.Store satisfies it.
//
// Every method takes the caller as well as the grant. The permission to
// administer permissions is a grant like any other and is resolved in the
// graph, not here, so a second transport asking the same questions gets the
// same answers.
type GrantStore interface {
	// Can reports whether the subject holds the action on the object. Raw gcode
	// takes an action the device plane does not check, so the transport asks
	// the graph the question directly.
	Can(ctx context.Context, who authz.Subject, action authz.Action, object authz.Object) (bool, error)

	// CanVerb reports whether the subject reaches one verb on one device,
	// including the dangerous tier: a verb no class, role or level confers,
	// reached only by a row naming it or by owning the machine. It answers
	// true for a verb the tier does not hold, because the class already
	// carried that one.
	CanVerb(ctx context.Context, who authz.Subject, device authz.Object, verb string) (bool, error)

	// Grants returns the tuples written on one object.
	Grants(ctx context.Context, who authz.Subject, object authz.Object) ([]authz.Tuple, error)

	// WriteGrant stores one tuple and reports whether the graph did not already
	// hold it.
	WriteGrant(ctx context.Context, who authz.Subject, grant authz.Tuple) (bool, error)

	// WithdrawGrant removes one tuple. Removing one that is not there is not an
	// error.
	WithdrawGrant(ctx context.Context, who authz.Subject, grant authz.Tuple) error

	// SetOrgRole replaces every org role one account holds with the one named,
	// so the account holds exactly one afterwards. It is the one path that
	// writes the admin role, and it takes configure_resource on the auth
	// resource kind.
	SetOrgRole(ctx context.Context, who authz.Subject, account authz.Subject, role authz.Relation) error
}

// Grant is one edge of the graph on the wire: object#relation@subject, in the
// three fields the model is written in. object is rendered whole because that
// is how the graph spells an object everywhere else, including in the refusal a
// caller gets back.
type Grant struct {
	Object   string `json:"object"   doc:"The object the grant is written on, as type:id."`
	Relation string `json:"relation" doc:"The edge name, as the vocabulary defines it on this object type."`
	Subject  string `json:"subject"  doc:"Who holds it: type:id for a principal, or type:id#relation for everybody holding that relation on that object."`
}

// GrantRef is the object a grant is written on, as the path names it. It is
// exported because Huma binds parameters by reflection and cannot write into an
// unexported embedded field.
type GrantRef struct {
	ObjectType string `path:"object_type" enum:"org,device,resource_kind" doc:"The type of the object the grants are written on." example:"device"`
	ObjectID   string `path:"object_id"                                   doc:"The object's identifier, a DNS-1123 label."        example:"9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93" maxLength:"63"`
}

type listGrantsInput struct {
	GrantRef
}

type listGrantsOutput struct {
	Body struct {
		Items []Grant `json:"items"`
	}
}

// grantBody is what a write accepts. object is optional and is only compared
// against the path, so a grant read from the listing can be sent straight
// back.
type grantBody struct {
	Object   string `json:"object,omitempty" doc:"Ignored unless it disagrees with the path, which is refused."`
	Relation string `json:"relation"         doc:"The edge name, as the vocabulary defines it on this object type."                                   maxLength:"63"  minLength:"1"`
	Subject  string `json:"subject"          doc:"Who holds it: type:id for a principal, or type:id#relation for a userset such as group:eng#member." maxLength:"191" minLength:"1"`
}

type writeGrantInput struct {
	GrantRef

	Body grantBody
}

// grantOutput carries the grant back as it now stands. The status separates the
// write that added an edge from the one that found it already there, because
// only the first moved the permissions epoch.
type grantOutput struct {
	Status int
	Body   Grant
}

// withdrawGrantInput names the edge to remove. The relation and the subject are
// query parameters and not a body, because RFC 9110 gives a DELETE body no
// semantics. This operation refuses a query parameter it does not publish, so
// a misspelt one is refused and does not delete something else.
type withdrawGrantInput struct {
	GrantRef

	Relation string `query:"relation" required:"true" doc:"The edge name to remove."                               example:"operator"`
	Subject  string `query:"subject"  required:"true" doc:"Who to remove it from, as type:id or type:id#relation." example:"user:bob"`
}

type withdrawGrantOutput struct {
	Status int
}

// grantErrors are the refusals every operation on this surface can answer with.
// 400 is the vocabulary's own: a relation that does not exist on this object
// type, or a subject shape the relation does not accept, is refused with a code
// that names which. A generic request-invalid would tell a client the body was
// wrong without saying what the graph accepts.
//
// There is no 404 here. An object the caller may not administer and an object
// that is not there get the same refusal.
var grantErrors = []int{
	http.StatusBadRequest,
	http.StatusUnauthorized,
	http.StatusForbidden,
	http.StatusUnprocessableEntity,
}

// registerGrants publishes the authorization graph as a REST surface: the
// grants written on one object, read, added and withdrawn.
//
// The object is the unit because it is what the permission is checked on. A
// caller who may administer an object may see and change every grant on it, and
// one who may not is told the object is not there for them, in the same bytes
// whether it exists or not.
func registerGrants(api huma.API, store GrantStore) {
	registerListGrants(api, store)
	registerWriteGrant(api, store)
	registerWithdrawGrant(api, store)
}

func registerListGrants(api huma.API, store GrantStore) {
	register(api, huma.Operation{
		OperationID: "list-grants",
		Method:      http.MethodGet,
		Path:        grantsPath,
		Summary:     "The grants written on one object",
		Description: "Every tuple written on this object, whoever holds it. Reading them takes the " +
			"same grant that writing one takes: on a printer that is administer, on a resource kind it is " +
			"configure_resource, and on an org it is the admin role written on that org. A caller who " +
			"holds none of those is told the object is not there for them, in the same words they " +
			"would be told about an object that does not exist.",
		Tags:   []string{"grants"},
		Errors: grantErrors,
	}, func(ctx context.Context, in *listGrantsInput) (*listGrantsOutput, error) {
		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		object, err := in.parse()
		if err != nil {
			return nil, err
		}

		held, err := store.Grants(ctx, who, object)
		if err != nil {
			return nil, err
		}

		out := &listGrantsOutput{}
		out.Body.Items = grantsOf(held)

		return out, nil
	})
}

func registerWriteGrant(api huma.API, store GrantStore) {
	register(api, huma.Operation{
		OperationID: "write-grant",
		Method:      http.MethodPost,
		Path:        grantsPath,
		Summary:     "Write a grant",
		Description: "Adds one tuple to the authorization graph. It answers 201 when the graph did " +
			"not already hold it, which is also when the permissions epoch moved and every live event " +
			"subscription re-scoped, and 200 when it did. " +
			"Two rules decide it: the caller administers the object, and the grant confers nothing " +
			"the caller does not already hold there. The second is what stops a caller granting " +
			"themselves more than they have. " +
			"On an org both rules read wider. Administering an org is holding the admin role on it, " +
			"and an org tuple lands on every printer and every resource kind the org holds, so it is " +
			"refused unless it confers nothing the caller lacks on each of them. One org tuple is how " +
			"a fleet-wide grant is written, and org:<org>#viewer@sink:<name> is the whole of what " +
			"feeds an event sink a fleet, including a printer admitted afterwards. The admin role " +
			"itself cannot be conferred here: an org's first administrator is written at bootstrap.",
		Tags:          []string{"grants"},
		DefaultStatus: http.StatusCreated,
		Errors:        grantErrors,
	}, func(ctx context.Context, in *writeGrantInput) (*grantOutput, error) {
		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		object, err := in.parse()
		if err != nil {
			return nil, err
		}

		if err = in.Body.checkObject(object); err != nil {
			return nil, err
		}

		grant, err := tupleOf(object, in.Body.Relation, in.Body.Subject)
		if err != nil {
			return nil, err
		}

		wrote, err := store.WriteGrant(ctx, who, grant)
		if err != nil {
			return nil, err
		}

		status := http.StatusCreated
		if !wrote {
			status = http.StatusOK
		}

		return &grantOutput{Status: status, Body: grantOf(grant)}, nil
	})

	documentExistingGrantResponse(api, grantsPath)
}

func registerWithdrawGrant(api huma.API, store GrantStore) {
	register(api, huma.Operation{
		OperationID: "withdraw-grant",
		Method:      http.MethodDelete,
		Path:        grantsPath,
		Summary:     "Withdraw a grant",
		Description: "Removes one tuple. It takes the same grant a write takes and answers 204 " +
			"whether or not the graph held it, so a client that retries a withdrawal is told the same " +
			"thing twice. A caller may withdraw the grant they are administering with; nothing gives " +
			"it back, because the first grant is written once and a restart does not write it again.",
		Tags:          []string{"grants"},
		DefaultStatus: http.StatusNoContent,
		Errors:        grantErrors,
	}, func(ctx context.Context, in *withdrawGrantInput) (*withdrawGrantOutput, error) {
		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		object, err := in.parse()
		if err != nil {
			return nil, err
		}

		grant, err := tupleOf(object, in.Relation, in.Subject)
		if err != nil {
			return nil, err
		}

		if err = store.WithdrawGrant(ctx, who, grant); err != nil {
			return nil, err
		}

		return &withdrawGrantOutput{Status: http.StatusNoContent}, nil
	})
}

// documentExistingGrantResponse publishes the 200 a write answers when the
// graph already held the grant. huma.Operation carries one DefaultStatus, so
// the second success status has to be added to the document by hand or a
// generated client will not know it.
func documentExistingGrantResponse(api huma.API, path string) {
	op := api.OpenAPI().Paths[path].Post

	created := op.Responses[strconv.Itoa(http.StatusCreated)]
	if created == nil {
		return
	}

	existing := *created
	existing.Description = "The graph already held this grant. Nothing changed and the epoch did not move."

	// Its own media types, so the example attachExamples writes onto the 201
	// answer is not shared with this one: a write that changed nothing does
	// not answer the example of one that did.
	existing.Content = make(map[string]*huma.MediaType, len(created.Content))
	for media, content := range created.Content {
		own := *content
		existing.Content[media] = &own
	}

	op.Responses[strconv.Itoa(http.StatusOK)] = &existing
}

// caller reads the principal the middleware resolved as the subject the graph
// is keyed by. The composition root writes the string, because only it knows
// which credential named this id and so which type it belongs to. A string
// that does not parse is a wiring fault and is reported as one.
func caller(ctx context.Context) (authz.Subject, error) {
	principal := principalFrom(ctx)
	if principal == "" {
		return authz.Subject{}, fault.AuthTokenInvalid.New("the request carries no usable identity")
	}

	who, err := authz.ParseSubject(principal)
	if err != nil {
		return authz.Subject{}, httpfault.Unexpected("read the caller's principal")
	}

	return who, nil
}

// parse turns the path into the object grants are written on. The type is
// checked against the vocabulary and the id against DNS-1123, so the graph
// never receives an object the rest of the system could not have produced.
func (r GrantRef) parse() (authz.Object, error) {
	typ := authz.Type(r.ObjectType)
	if !authz.Administrable(typ) {
		return authz.Object{}, httpfault.RequestInvalid("path.object_type",
			"expected one of "+administrableTypes,
			"nothing in the vocabulary says who administers that object type, so its grants "+
				"cannot be administered")
	}

	object, err := authz.NewObject(typ, r.ObjectID)
	if err != nil {
		return authz.Object{}, err
	}

	return object, nil
}

// checkObject refuses a body that names a different object from the path. The
// body is not a second way to choose what is written. It is echoed back from a
// read, and a disagreement is a client bug worth reporting.
func (b grantBody) checkObject(object authz.Object) error {
	if b.Object != "" && b.Object != object.String() {
		return httpfault.RequestInvalid("body.object",
			"expected "+object.String(), "the body names a different object from the path")
	}

	return nil
}

// tupleOf builds the edge through the vocabulary's own constructors, which is
// where a relation the object type does not have and a subject shape the
// relation does not accept are refused.
func tupleOf(object authz.Object, relation, subject string) (authz.Tuple, error) {
	who, err := authz.ParseSubject(subject)
	if err != nil {
		return authz.Tuple{}, err
	}

	return authz.NewTuple(object, authz.Relation(relation), who)
}

func grantsOf(tuples []authz.Tuple) []Grant {
	out := make([]Grant, 0, len(tuples))
	for _, tuple := range tuples {
		out = append(out, grantOf(tuple))
	}

	return out
}

func grantOf(tuple authz.Tuple) Grant {
	return Grant{
		Object:   tuple.Object().String(),
		Relation: tuple.Relation().String(),
		Subject:  tuple.Subject().String(),
	}
}
