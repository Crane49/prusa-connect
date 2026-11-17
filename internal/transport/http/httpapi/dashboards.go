//go:build !nodashboards

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// The dashboard paths. The default is a path of its own and not a member of a
// dashboard, because choosing it changes two dashboards at once and takes a
// permission no other change of a dashboard takes.
const (
	dashboardsPath       = "/api/v1/dashboards"
	dashboardPath        = dashboardsPath + "/{dashboard}"
	defaultDashboardPath = dashboardsPath + "/default"
)

// DashboardCard is one tile of a dashboard: what it shows, where it sits on
// the grid, and its own settings.
type DashboardCard struct {
	ID    string `json:"id"    doc:"The card's name inside its dashboard: 1 to 16 lower-case letters and digits."                                                       example:"p1"`
	Type  string `json:"type"  doc:"What the card shows: a namespace, a slash and a name. The core's own cards are core/<name>, and a module's are <namespace>/<name>." example:"core/printer"`
	X     int    `json:"x"     doc:"The first column the card covers, counted from 0."                                                                                                         minimum:"0" maximum:"23"`
	Y     int    `json:"y"     doc:"The first row the card covers, counted from 0."                                                                                                            minimum:"0" maximum:"511"`
	W     int    `json:"w"     doc:"How many columns the card covers."                                                                                                                         minimum:"1" maximum:"24"`
	H     int    `json:"h"     doc:"How many rows the card covers."                                                                                                                            minimum:"1" maximum:"32"`
	Title string `json:"title" doc:"The heading a person wrote over the card. Empty draws the heading its type gives it."                                                                                                required:"false" maxLength:"120"`

	Config map[string]any `json:"config" required:"false" doc:"The card's own settings, such as which printer it shows. Left out, they are an empty object. The server holds them to being one JSON object of at most 4096 bytes written out, and never reads a field: they belong to the card type."`
}

// Dashboard is one page of cards.
type Dashboard struct {
	ID         int64           `json:"id"         doc:"The number the server counted up. It is never given out twice, so a link to a deleted dashboard never opens another one."        example:"3"`
	Name       string          `json:"name"       doc:"What the dashboard is called."                                                                                                   example:"Print farm"`
	Visibility string          `json:"visibility" doc:"Who sees the dashboard: everyone, operators or admins."                                                                                               enum:"everyone,operators,admins"`
	IsDefault  bool            `json:"is_default" doc:"Whether this is the dashboard the home page draws."`
	Columns    int             `json:"columns"    doc:"How many columns the grid has."                                                                                                                                                           minimum:"4" maximum:"24"`
	Density    string          `json:"density"    doc:"How tall one row of the grid is."                                                                                                                     enum:"compact,comfortable,spacious"`
	Cards      []DashboardCard `json:"cards"      doc:"Every card, in the order they were placed."                                                                                                                                                                        nullable:"false"`
	Revision   int64           `json:"revision"   doc:"The version of the dashboard. A change sends it back, and one made against an older revision is refused with DASHBOARD_CHANGED."`
	Created    time.Time       `json:"created"    doc:"When the dashboard was made."`
	CreatedBy  string          `json:"created_by" doc:"Who made it, as the authorization graph names them."`
	Changed    time.Time       `json:"changed"    doc:"When it was last changed."`
	ChangedBy  string          `json:"changed_by" doc:"Who changed it last."`
}

// DashboardRow is one dashboard as the listing shows it: no cards, only how
// many.
type DashboardRow struct {
	ID         int64     `json:"id"         doc:"The dashboard's id."`
	Name       string    `json:"name"       doc:"What the dashboard is called."`
	Visibility string    `json:"visibility" doc:"Who sees it: everyone, operators or admins."        enum:"everyone,operators,admins"`
	IsDefault  bool      `json:"is_default" doc:"Whether this is the dashboard the home page draws."`
	Cards      int       `json:"cards"      doc:"How many cards it holds."`
	Changed    time.Time `json:"changed"    doc:"When it was last changed."`
	ChangedBy  string    `json:"changed_by" doc:"Who changed it last."`
}

// DashboardDraft is a dashboard as a person wrote it. The name is always sent.
// A new dashboard takes the default for visibility, columns, density or cards
// the draft leaves out. A change keeps what the dashboard had for those four,
// and carries the revision it was made against.
type DashboardDraft struct {
	Name       string           `json:"name"                 doc:"What the dashboard is called: at most 80 bytes, one line."                                maxLength:"80"`
	Visibility *string          `json:"visibility,omitempty" doc:"Who sees it. everyone when a new dashboard leaves it out."                                               enum:"everyone,operators,admins"`
	Columns    *int             `json:"columns,omitempty"    doc:"How many columns the grid has. 12 when a new dashboard leaves it out."                                                                       minimum:"4" maximum:"24"`
	Density    *string          `json:"density,omitempty"    doc:"How tall one row is. comfortable when a new dashboard leaves it out."                                    enum:"compact,comfortable,spacious"`
	Cards      *[]DashboardCard `json:"cards,omitempty"      doc:"Every card, at most 64. None when a new dashboard leaves it out."                                                                                                     maxItems:"64"`
	Revision   *int64           `json:"revision,omitempty"   doc:"The revision the change was made against. A change of an existing dashboard requires it."`
}

// DashboardListing is every dashboard the caller sees, and the default one.
type DashboardListing struct {
	Items     []DashboardRow `json:"items"      nullable:"false" doc:"Every dashboard the caller sees, by name."`
	Total     int            `json:"total"                       doc:"How many dashboards the caller sees."`
	DefaultID *int64         `json:"default_id" nullable:"true"  doc:"The id of the dashboard the home page draws, or null when there is none."`
}

// DashboardStore is the dashboards a person lays out on the home page. The
// store authorizes: it receives the principal and decides what they see
// against the resource kind dashboard.
type DashboardStore interface {
	// ListDashboards answers every dashboard the caller sees. A caller who
	// sees none is answered an empty listing and not a refusal.
	ListDashboards(ctx context.Context, principal string) (DashboardListing, error)
	// GetDashboard answers one dashboard. One the caller may not see and one
	// that is not there are the same refusal.
	GetDashboard(ctx context.Context, principal string, id int64) (Dashboard, error)
	// CreateDashboard writes a new dashboard from a draft.
	CreateDashboard(ctx context.Context, principal string, draft DashboardDraft) (Dashboard, error)
	// ReplaceDashboard writes a draft over one dashboard, against the revision
	// the draft names.
	ReplaceDashboard(ctx context.Context, principal string, id int64, draft DashboardDraft) (Dashboard, error)
	// DeleteDashboard removes one dashboard.
	DeleteDashboard(ctx context.Context, principal string, id int64) error
	// SetDefaultDashboard makes one dashboard the default, or none for nil,
	// and answers the default after the change.
	SetDefaultDashboard(ctx context.Context, principal string, id *int64) (*int64, error)
}

// DefaultDashboardBody names the dashboard the home page draws, or none.
type DefaultDashboardBody struct {
	ID *int64 `json:"id" doc:"The dashboard the home page draws, or null for none." nullable:"true"`
}

// defaultDashboardAnswer is the default as it stands after a choice.
type defaultDashboardAnswer = struct {
	DefaultID *int64 `json:"default_id" doc:"The dashboard the home page draws now, or null." nullable:"true"`
}

// dashboardRequest is the input of all six dashboard operations, and
// dashboardResponse is their output.
//
// huma.Register is generic, and every distinct pair of input and output shapes
// compiles its own copy of the whole request pipeline, about 22 KB on arm. Six
// more copies do not fit the binary size budget. Other routes already compile
// the pair of an empty input and an output that writes itself, so the
// dashboards add no copy. registerDashboardRoute documents what each operation
// reads and answers, because the types do not. Huma still validates a body
// against that document before the handler runs, and keepDashboardCall hands
// the handler the path parameter and the body.
type dashboardRequest struct{}

type dashboardResponse struct {
	Body func(huma.Context)
}

// dashboardCall is what one call to a dashboard operation carries.
type dashboardCall struct {
	// dashboard is the {dashboard} path parameter as it arrived, and empty on
	// a path without one.
	dashboard string
	// body is the request body, which Huma has validated against the
	// operation's schema by the time a handler reads it.
	body []byte
}

type dashboardCallKey struct{}

// dashboardAnswer is what a handler answers: a status, and the body and the
// Location header when there are any.
type dashboardAnswer struct {
	status   int
	location string
	body     any
}

// dashboardIDPattern is what the {dashboard} parameter is held to. Eighteen
// digits fit an int64.
const dashboardIDPattern = "^[1-9][0-9]{0,17}$"

// id is the dashboard the path names. Huma cannot check a parameter the input
// type does not declare, so this applies the pattern the document states.
func (c *dashboardCall) id() (int64, error) {
	id, err := strconv.ParseInt(c.dashboard, 10, 64)
	if err != nil || id < 1 || len(c.dashboard) > 18 || c.dashboard[0] == '+' || c.dashboard[0] == '0' {
		return 0, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Location: "path.dashboard",
			Message:  "expected string to match pattern " + dashboardIDPattern,
			Value:    c.dashboard,
		})
	}

	return id, nil
}

// decode reads the validated body into out.
func (c *dashboardCall) decode(out any) error {
	// UseNumber keeps a number in a card's settings as it was written, where a
	// float64 would round an integer past 2^53.
	decoder := json.NewDecoder(bytes.NewReader(c.body))
	decoder.UseNumber()

	if err := decoder.Decode(out); err != nil {
		return httpfault.RequestInvalid("body", "expected the body the operation documents", err.Error())
	}

	return nil
}

// keepDashboardCall reads what the handler needs off the request before Huma
// does: the path parameter, and a copy of the body Huma then reads and
// validates as usual.
func keepDashboardCall(ctx huma.Context, next func(huma.Context)) {
	call := &dashboardCall{dashboard: ctx.Param("dashboard")}
	kept := huma.WithValue(ctx, dashboardCallKey{}, call)

	if ctx.Operation().RequestBody == nil {
		next(kept)

		return
	}

	body, err := io.ReadAll(ctx.BodyReader())
	call.body = body

	next(rereadBody{humaContext: kept, body: io.MultiReader(bytes.NewReader(body), failedRead{err})})
}

// rereadBody is a request whose body is read a second time, from a copy.
type rereadBody struct {
	humaContext

	body io.Reader
}

// humaContext names huma.Context for embedding: the interface has a method
// called Context, which an embedded field of that name would hide.
type humaContext = huma.Context

func (r rereadBody) BodyReader() io.Reader { return r.body }

// failedRead ends a copied body with the error reading the original ended
// with, such as one past the size limit, so Huma answers it as it would have.
type failedRead struct{ err error }

func (f failedRead) Read([]byte) (int, error) {
	if f.err != nil {
		return 0, f.err
	}

	return 0, io.EOF
}

// dashboardRoute is one dashboard operation: its document, the types it reads
// and answers, and its handler.
type dashboardRoute struct {
	op huma.Operation
	// request is the body type, or nil for an operation that takes none.
	request reflect.Type
	// answer is the response body type, or nil for a 204.
	answer reflect.Type
	// answerHint names an answer type that has no name of its own.
	answerHint string
	handle     func(ctx context.Context, call *dashboardCall) (dashboardAnswer, error)
}

// dashboardErrors is what every dashboard operation may answer besides its
// own success.
var dashboardErrors = []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound}

// dashboardWriteErrors adds a stale revision and a server holding as many
// dashboards as it keeps.
var dashboardWriteErrors = []int{
	http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict,
}

// dashboardsCarried says this build carries the dashboards. The build that
// leaves them out, with the nodashboards tag, says false.
const dashboardsCarried = true

// registerDashboards publishes the dashboards. Its caller registers them only
// in a build that carries them and while Config.DashboardsOff is false. A nil
// store then answers an empty listing and a 404 for any id.
func registerDashboards(api huma.API, store DashboardStore) {
	if store == nil {
		store = noDashboards{}
	}

	for _, route := range dashboardRoutes(store) {
		registerDashboardRoute(api, route)
	}
}

// registerDashboardRoute documents what the shared types cannot say, registers
// the operation, and then takes out what Huma documented from those types.
func registerDashboardRoute(api huma.API, route dashboardRoute) {
	registry := api.OpenAPI().Components.Schemas
	op := route.op
	op.Tags = []string{dashboardsTag}
	op.Errors = slices.Clone(op.Errors)

	identified := strings.Contains(op.Path, "{dashboard}")
	if identified {
		op.Parameters = []*huma.Param{dashboardParameter()}
	}

	// Huma validates the body against the application/json schema it finds
	// here, so the document and the check use the same schema.
	if route.request != nil {
		op.RequestBody = &huma.RequestBody{
			Required: true,
			Content: map[string]*huma.MediaType{
				jsonMedia: {Schema: registry.Schema(route.request, true, "")},
			},
		}
	} else if identified {
		// Huma answers 422 for a refused body or parameter, and documents it
		// only for one it can see.
		op.Errors = append(op.Errors, http.StatusUnprocessableEntity)
	}

	status := op.DefaultStatus
	if status == 0 {
		status = http.StatusOK
	}

	answered := &huma.Response{Description: http.StatusText(status)}
	if route.answer != nil {
		answered.Content = map[string]*huma.MediaType{
			jsonMedia: {Schema: registry.Schema(route.answer, true, route.answerHint)},
		}
	}

	if status == http.StatusCreated {
		answered.Headers = map[string]*huma.Param{locationHeader: {Schema: &huma.Schema{
			Type:        huma.TypeString,
			Description: "Where the new dashboard lives. Sent only with 201.",
		}}}
	}

	op.Responses = map[string]*huma.Response{strconv.Itoa(status): answered}
	op.Middlewares = huma.Middlewares{keepDashboardCall}

	handle := route.handle

	register(api, op, func(ctx context.Context, _ *dashboardRequest) (*dashboardResponse, error) {
		call, _ := ctx.Value(dashboardCallKey{}).(*dashboardCall)
		if call == nil {
			call = &dashboardCall{}
		}

		answered, err := handle(ctx, call)
		if err != nil {
			return nil, err
		}

		return &dashboardResponse{Body: func(ctx huma.Context) { writeDashboardAnswer(api, ctx, answered) }}, nil
	})
}

// writeDashboardAnswer writes what a handler answered, the way Huma writes a
// typed body.
func writeDashboardAnswer(api huma.API, ctx huma.Context, answered dashboardAnswer) {
	if answered.location != "" {
		ctx.SetHeader(locationHeader, answered.location)
	}

	if answered.body == nil {
		ctx.SetStatus(answered.status)

		return
	}

	body, err := api.Transform(ctx, strconv.Itoa(answered.status), answered.body)
	if err == nil {
		ctx.SetHeader("Content-Type", jsonMedia)
		ctx.SetStatus(answered.status)
		err = api.Marshal(ctx.BodyWriter(), jsonMedia, body)
	}

	if err != nil {
		// Huma panics the same way for a typed body it cannot write. The
		// status is already sent, so the recovery middleware records why.
		panic(fmt.Errorf("write the dashboard answer: %w", err))
	}
}

const locationHeader = "Location"

// dashboardParameter is the {dashboard} path parameter as the document states
// it.
func dashboardParameter() *huma.Param {
	const doc = "The dashboard's id, as the listing answers it."

	return &huma.Param{
		Name:        "dashboard",
		In:          "path",
		Description: doc,
		Required:    true,
		Schema: &huma.Schema{
			Type:        huma.TypeString,
			Description: doc,
			Pattern:     dashboardIDPattern,
			Examples:    []any{exampleDashboardID},
		},
	}
}

func dashboardRoutes(store DashboardStore) []dashboardRoute {
	return []dashboardRoute{
		{
			op: huma.Operation{
				OperationID: "list-dashboards",
				Method:      http.MethodGet,
				Path:        dashboardsPath,
				Summary:     "Every dashboard",
				Description: "Every dashboard the caller sees, by name, and the one the home page draws. A " +
					"dashboard visible to everyone takes view_resource on the dashboard kind, one visible to " +
					"operators takes operate_resource, and one visible to admins takes configure_resource. The " +
					"count never includes a dashboard the caller may not see.",
				Errors: []int{http.StatusUnauthorized},
			},
			answer: reflect.TypeFor[DashboardListing](),
			handle: func(ctx context.Context, _ *dashboardCall) (dashboardAnswer, error) {
				return answer(store.ListDashboards(ctx, principalFrom(ctx)))
			},
		},
		{
			op: huma.Operation{
				OperationID: "create-dashboard",
				Method:      http.MethodPost,
				Path:        dashboardsPath,
				Summary:     "Make a dashboard",
				Description: "Writes one new dashboard with the cards the draft carries. A copy of a " +
					"dashboard is made the same way. It takes operate_resource on the dashboard kind, and a " +
					"visibility the caller would see.",
				DefaultStatus: http.StatusCreated,
				Errors:        dashboardWriteErrors,
			},
			request: reflect.TypeFor[DashboardDraft](),
			answer:  reflect.TypeFor[Dashboard](),
			handle: func(ctx context.Context, call *dashboardCall) (dashboardAnswer, error) {
				var draft DashboardDraft
				if err := call.decode(&draft); err != nil {
					return dashboardAnswer{}, err
				}

				board, err := store.CreateDashboard(ctx, principalFrom(ctx), draft)
				if err != nil {
					return dashboardAnswer{}, err
				}

				return dashboardAnswer{
					status:   http.StatusCreated,
					location: dashboardsPath + "/" + strconv.FormatInt(board.ID, 10),
					body:     board,
				}, nil
			},
		},
		{
			op: huma.Operation{
				OperationID: "set-default-dashboard",
				Method:      http.MethodPut,
				Path:        defaultDashboardPath,
				Summary:     "Choose the dashboard the home page draws",
				Description: "Makes one dashboard the default, or none for a null id. Only a dashboard " +
					"visible to everyone may be the default. It takes configure_resource on the dashboard kind.",
				Errors: dashboardErrors,
			},
			request:    reflect.TypeFor[DefaultDashboardBody](),
			answer:     reflect.TypeFor[defaultDashboardAnswer](),
			answerHint: "SetDefaultDashboardOutputBody",
			handle: func(ctx context.Context, call *dashboardCall) (dashboardAnswer, error) {
				var choice DefaultDashboardBody
				if err := call.decode(&choice); err != nil {
					return dashboardAnswer{}, err
				}

				held, err := store.SetDefaultDashboard(ctx, principalFrom(ctx), choice.ID)

				return answer(defaultDashboardAnswer{DefaultID: held}, err)
			},
		},
		{
			op: huma.Operation{
				OperationID: "get-dashboard",
				Method:      http.MethodGet,
				Path:        dashboardPath,
				Summary:     "One dashboard, with every card",
				Description: "Everything a dashboard draws. A dashboard the caller may not see answers " +
					"DASHBOARD_NOT_FOUND, the same as one that is not there.",
				Errors: dashboardErrors,
			},
			answer: reflect.TypeFor[Dashboard](),
			handle: func(ctx context.Context, call *dashboardCall) (dashboardAnswer, error) {
				id, err := call.id()
				if err != nil {
					return dashboardAnswer{}, err
				}

				return answer(store.GetDashboard(ctx, principalFrom(ctx), id))
			},
		},
		{
			op: huma.Operation{
				OperationID: "replace-dashboard",
				Method:      http.MethodPut,
				Path:        dashboardPath,
				Summary:     "Change a dashboard",
				Description: "Writes the draft over the dashboard, cards included. The draft always carries the " +
					"name. Visibility, columns, density or cards the draft leaves out keep what the " +
					"dashboard had. The draft carries the revision it was made " +
					"against, and one made against an older revision is refused with DASHBOARD_CHANGED. It " +
					"takes operate_resource on the dashboard kind, and a dashboard and a new visibility the " +
					"caller sees.",
				Errors: dashboardWriteErrors,
			},
			request: reflect.TypeFor[DashboardDraft](),
			answer:  reflect.TypeFor[Dashboard](),
			handle: func(ctx context.Context, call *dashboardCall) (dashboardAnswer, error) {
				id, err := call.id()
				if err != nil {
					return dashboardAnswer{}, err
				}

				var draft DashboardDraft
				if err = call.decode(&draft); err != nil {
					return dashboardAnswer{}, err
				}

				return answer(store.ReplaceDashboard(ctx, principalFrom(ctx), id, draft))
			},
		},
		{
			op: huma.Operation{
				OperationID: "delete-dashboard",
				Method:      http.MethodDelete,
				Path:        dashboardPath,
				Summary:     "Delete a dashboard",
				Description: "Removes the dashboard. Its id is never given out again. A deleted default " +
					"leaves the home page drawing another dashboard. It takes operate_resource on the " +
					"dashboard kind.",
				DefaultStatus: http.StatusNoContent,
				Errors:        dashboardErrors,
			},
			handle: func(ctx context.Context, call *dashboardCall) (dashboardAnswer, error) {
				id, err := call.id()
				if err != nil {
					return dashboardAnswer{}, err
				}

				if err = store.DeleteDashboard(ctx, principalFrom(ctx), id); err != nil {
					return dashboardAnswer{}, err
				}

				return dashboardAnswer{status: http.StatusNoContent}, nil
			},
		},
	}
}

// answer is a store's answer as a 200.
func answer(body any, err error) (dashboardAnswer, error) {
	if err != nil {
		return dashboardAnswer{}, err
	}

	return dashboardAnswer{status: http.StatusOK, body: body}, nil
}

// noDashboards is the store of a server built with none: no dashboard, and a
// 404 for any id.
type noDashboards struct{}

func (noDashboards) ListDashboards(context.Context, string) (DashboardListing, error) {
	return DashboardListing{Items: []DashboardRow{}}, nil
}

func (noDashboards) GetDashboard(_ context.Context, _ string, id int64) (Dashboard, error) {
	return Dashboard{}, httpfault.DashboardNotFound(id)
}

// CreateDashboard is never reached in a server that serves the routes with a
// store, so a call here is a wiring fault and not a dashboard that is missing.
func (noDashboards) CreateDashboard(context.Context, string, DashboardDraft) (Dashboard, error) {
	return Dashboard{}, httpfault.Unexpected("this server keeps no dashboards")
}

func (noDashboards) ReplaceDashboard(_ context.Context, _ string, id int64, _ DashboardDraft) (Dashboard, error) {
	return Dashboard{}, httpfault.DashboardNotFound(id)
}

func (noDashboards) DeleteDashboard(_ context.Context, _ string, id int64) error {
	return httpfault.DashboardNotFound(id)
}

func (noDashboards) SetDefaultDashboard(_ context.Context, _ string, id *int64) (*int64, error) {
	if id == nil {
		return nil, nil //nolint:nilnil // no default is an answer, not a failure.
	}

	return nil, httpfault.DashboardNotFound(*id)
}
