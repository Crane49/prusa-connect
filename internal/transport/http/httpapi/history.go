package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// The paths the stored past is read at. Two are across the fleet and two are
// under one device, which is the shape earlier research settled: a client
// following one machine names it in the path, and a console searching the fleet
// names the origins it cares about in a query parameter.
const (
	historyFactsPath         = "/api/v1/history/facts"
	historySamplesPath       = "/api/v1/history/samples"
	deviceHistoryFactsPath   = devicePath + "/history/facts"
	deviceHistorySamplesPath = devicePath + "/history/samples"
)

// HistoryFact is one stored event, as it was published.
type HistoryFact struct {
	At     time.Time `json:"at"     doc:"When the event happened, as the origin's own clock reported it."`
	Origin string    `json:"origin" doc:"What the event came from, as kind:id."`
	Type   string    `json:"type"   doc:"The event type, reverse-DNS and past tense, exactly as it was published."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Sequence uint64 `json:"sequence" doc:"The event's place in its origin's numbering. It is unique inside one epoch and never reused."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Epoch string `json:"epoch" doc:"The generation of the origin's own store. It changes when that store is recreated, which is what makes sequence unambiguous."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Payload json.RawMessage `json:"payload" doc:"The envelope's data member, stored and returned unparsed. Its schema belongs to whoever published the event."`
}

// HistorySample is one stored reading.
//
// Exactly one of value and text is present, because a path reports a number or
// it reports a word and a reading is never both. /tools/0/nozzle_temp answers in
// value and /state answers in text, and a client reads whichever member is
// there rather than a discriminator beside them: a member that is absent is the
// half this reading is not.
type HistorySample struct {
	At     time.Time `json:"at"     doc:"When the measurement was taken."`
	Origin string    `json:"origin" doc:"What reported it, as kind:id."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Metric string `json:"metric" doc:"The state path the value was reported under, which is the name a series is queried by across firmware versions."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Value *float64 `json:"value,omitempty" doc:"The measurement, for a path that reports a number. Absent where the path reported a word, in which case text carries it."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Text *string `json:"text,omitempty" doc:"The reading, for a path that reports a word rather than a number: /state answers IDLE or PRINTING here. Absent where the path reported a number."`
}

// HistoryFactPage is one page of stored events.
type HistoryFactPage struct {
	Items []HistoryFact `json:"items"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Cursor string `json:"cursor" doc:"Opaque text the provider minted, to be sent back as the cursor parameter for the next page. Empty when this page is the last one. Read nothing out of its form."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Incomplete bool `json:"incomplete" doc:"Whether the server knows it did not see this window whole. A reader that fell behind loses events, and the window they fell behind in is marked here rather than answered as a quiet hole."`
}

// HistorySamplePage is one page of stored measurements.
type HistorySamplePage struct {
	Items []HistorySample `json:"items"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Cursor string `json:"cursor" doc:"Opaque text the provider minted, to be sent back as the cursor parameter for the next page. Empty when this page is the last one. Read nothing out of its form."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Incomplete bool `json:"incomplete" doc:"Whether the server knows it did not see this window whole."`
}

// HistoryWindow is what both queries carry: whose past, over which instants,
// and how much of it at a time.
//
// Origins is what the caller asked for and never what they may read. The
// implementation reduces it against the authorization graph, and an empty
// Origins means every origin the caller may read rather than every origin.
type HistoryWindow struct {
	Origins []event.Origin
	From    time.Time
	To      time.Time
	Limit   int
	Cursor  string
}

// HistoryFactQuery asks for a window of stored events.
type HistoryFactQuery struct {
	HistoryWindow

	// Types narrows to these event types. Empty is every type.
	Types []string

	// Classes is what the caller asked for and never what they may read. The
	// implementation intersects it with the classes their grants carry, so
	// naming the audit class widens nothing: a caller without audit sight of
	// every origin in the query reads no audit rows whether they asked for them
	// or not. Empty is every class the caller may read.
	//
	// It exists because the parameter was accepted and ignored. All four values
	// answered the same rows, which is a filter a client cannot tell from a
	// server that has none.
	Classes []event.Class
}

// HistorySampleQuery asks for a window of stored measurements.
type HistorySampleQuery struct {
	HistoryWindow

	// Metrics narrows to these state paths. Empty is every metric.
	Metrics []string
}

// HistoryStore is the stored past as this transport reads it. It is declared
// here because this is where it is consumed, and the composition root satisfies
// it with whatever history provider the deployment configured.
//
// Both methods take the principal the middleware resolved rather than an
// already-reduced set of origins. Which origins a caller may read is a question
// for the authorization graph, and it is answered on the way into the query: a
// page that was fetched and then filtered would leak through its own cursor,
// because a page of ten rows the caller may not read is a page of nothing that
// still says there is more.
type HistoryStore interface {
	// Facts answers one page of stored events, narrowed to what this principal
	// may read.
	Facts(ctx context.Context, principal string, query HistoryFactQuery) (HistoryFactPage, error)
	// Samples answers one page of stored measurements, narrowed the same way.
	Samples(ctx context.Context, principal string, query HistorySampleQuery) (HistorySamplePage, error)
}

// HistoryWindowParams is the query half every history operation shares: which
// instants to read between, how many rows at a time, and where to carry on from.
//
// It is exported because Huma reads request parameters by reflection and skips
// an embedded field whose type is unexported, so four flat copies of these four
// tags is the alternative. It names no schema in the published document:
// parameters are written onto the operation rather than into a component.
type HistoryWindowParams struct {
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	From time.Time `query:"from" doc:"The earliest instant to read, RFC 3339, inclusive. Omitted reads from the beginning of what is stored." example:"2026-08-26T00:00:00Z"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	To time.Time `query:"to" doc:"The instant to read up to, RFC 3339, exclusive. Omitted reads to the end of what is stored." example:"2026-08-27T00:00:00Z"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Limit int `query:"limit" minimum:"0" doc:"How many rows to return. Omitted or zero is the provider's default page size, and a value above its ceiling is answered with that ceiling." example:"100"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Cursor string `query:"cursor" doc:"Opaque text a previous page returned in its cursor member. It is minted by the provider and its form is not part of this contract." example:"c3RhcnQ6MTc1NjE5OTY0NQ"`
}

// historyFactsInput is a fleet-wide search of the stored events.
type historyFactsInput struct {
	HistoryWindowParams

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Origins []string `query:"origins" maxItems:"512" doc:"Narrow to these origins, each as kind:id, comma separated. Omitted means every origin the caller may read. An origin the caller may not read refuses the whole request, and the refusal names none of them." example:"device:9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Types []string `query:"types" maxItems:"64" doc:"Narrow to these event types, comma separated, each matched whole. Omitted means every type. The set this build publishes is at /api/v1/vocabularies/event-types." example:"com.prusa.connect.device.presence.changed"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Classes []string `query:"classes" maxItems:"4" enum:"telemetry,state,command,audit" doc:"Narrow to these event classes, comma separated. Omitted means every class the caller may read. Naming the audit class widens nothing: audit rows are answered only to a caller holding view_audit on every origin in the query. Telemetry is stored as a measurement rather than an event and matches no row here." example:"state,command"`
}

// historySamplesInput is the same search over the stored measurements.
type historySamplesInput struct {
	HistoryWindowParams

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Origins []string `query:"origins" maxItems:"512" doc:"Narrow to these origins, each as kind:id, comma separated. Omitted means every origin the caller may read. An origin the caller may not read refuses the whole request, and the refusal names none of them." example:"device:9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Metrics []string `query:"metrics" maxItems:"64" doc:"Narrow to these state paths, comma separated, each matched whole. Omitted means every metric this origin reported." example:"/tools/0/nozzle_temp,/temperature/bed"`
}

// deviceHistoryFactsInput is one device's stored events.
type deviceHistoryFactsInput struct {
	HistoryWindowParams

	ID string `path:"device_id" doc:"The device this server minted an id for when it dialled in." example:"9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Types []string `query:"types" maxItems:"64" doc:"Narrow to these event types, comma separated, each matched whole. Omitted means every type." example:"com.prusa.connect.device.presence.changed"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Classes []string `query:"classes" maxItems:"4" enum:"telemetry,state,command,audit" doc:"Narrow to these event classes, comma separated. Omitted means every class the caller may read. Naming the audit class widens nothing." example:"state,command"`
}

// deviceHistorySamplesInput is one device's stored measurements.
type deviceHistorySamplesInput struct {
	HistoryWindowParams

	ID string `path:"device_id" doc:"The device this server minted an id for when it dialled in." example:"9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Metrics []string `query:"metrics" maxItems:"64" doc:"Narrow to these state paths, comma separated, each matched whole. Omitted means every metric this device reported." example:"/tools/0/nozzle_temp,/temperature/bed"`
}

type historyFactsOutput struct {
	Body HistoryFactPage
}

type historySamplesOutput struct {
	Body HistorySamplePage
}

// The prose every history operation shares, so the four descriptions differ
// only in what each one answers.
const (
	historyIntro = "Reads what this server stored, which is what a history provider caught from the " +
		"event bus. It is not the live stream: a deployment that configures no provider keeps no " +
		"past at all and answers 501 here.\n\n"

	historyNarrowing = "\n\nThe answer is narrowed to what the caller may read, from the same " +
		"authorization graph the event stream narrows by, so the past of a printer a caller cannot " +
		"see in the present is not readable here either. Paging follows the cursor member: send it " +
		"back unchanged and read nothing out of it. A window the server knows it did not see whole " +
		"is marked incomplete rather than answered as a quiet hole."
)

// registerHistory publishes the four read paths over the stored past.
//
// They are registered whatever a deployment wired, like the storage listing and
// for the same reason: the document is a contract and what a client generates
// from must not depend on how this process was configured. A build with no
// provider answers them with the refusal that says so.
func registerHistory(api huma.API, cfg Config) {
	registerHistoryFacts(api, cfg)
	registerHistorySamples(api, cfg)
	registerDeviceHistoryFacts(api, cfg)
	registerDeviceHistorySamples(api, cfg)
}

func registerHistoryFacts(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "list-history-facts",
		Method:      http.MethodGet,
		Path:        historyFactsPath,
		Summary:     "Search the stored events",
		Description: historyIntro + "Every stored event across the fleet, newest last, narrowed by " +
			"origin, by event type and by time. A state change and an audit record are both kept; a " +
			"command is not, because an instruction that expired has no value and the record of who " +
			"authorized it is already an audit event." + historyNarrowing,
		Tags:   []string{"history"},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotImplemented},
	}, func(ctx context.Context, in *historyFactsInput) (*historyFactsOutput, error) {
		past, window, err := historyWindowOf(cfg, in.HistoryWindowParams, in.Origins)
		if err != nil {
			return nil, err
		}

		classes, err := classesFrom(in.Classes)
		if err != nil {
			return nil, err
		}

		page, err := past.Facts(ctx, principalFrom(ctx),
			HistoryFactQuery{HistoryWindow: window, Types: in.Types, Classes: classes})
		if err != nil {
			return nil, err
		}

		return &historyFactsOutput{Body: emptyFacts(page)}, nil
	})
}

func registerHistorySamples(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "list-history-samples",
		Method:      http.MethodGet,
		Path:        historySamplesPath,
		Summary:     "Read the stored measurements",
		Description: historyIntro + "Every stored measurement across the fleet, oldest first, narrowed " +
			"by origin, by metric and by time. A metric is named by the state path the value was " +
			"reported under, so one series is queried by one name across firmware versions. Values " +
			"are raw: this build's provider computes no buckets and refuses a query that asks for " +
			"one rather than answering something it did not compute." + historyNarrowing,
		Tags:   []string{"history"},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotImplemented},
	}, func(ctx context.Context, in *historySamplesInput) (*historySamplesOutput, error) {
		past, window, err := historyWindowOf(cfg, in.HistoryWindowParams, in.Origins)
		if err != nil {
			return nil, err
		}

		page, err := past.Samples(ctx, principalFrom(ctx),
			HistorySampleQuery{HistoryWindow: window, Metrics: in.Metrics})
		if err != nil {
			return nil, err
		}

		return &historySamplesOutput{Body: emptySamples(page)}, nil
	})
}

func registerDeviceHistoryFacts(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "list-device-history-facts",
		Method:      http.MethodGet,
		Path:        deviceHistoryFactsPath,
		Summary:     "Read one device's stored events",
		Description: historyIntro + "One device's stored events, which is the fleet-wide search with " +
			"the origin fixed to this device. A device this server has never met and one the caller " +
			"may not read are the same refusal, so this path never says which devices exist." +
			historyNarrowing,
		Tags:   []string{"history"},
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusNotImplemented},
	}, func(ctx context.Context, in *deviceHistoryFactsInput) (*historyFactsOutput, error) {
		past, window, err := deviceHistoryWindowOf(ctx, cfg, in.HistoryWindowParams, in.ID)
		if err != nil {
			return nil, err
		}

		classes, err := classesFrom(in.Classes)
		if err != nil {
			return nil, err
		}

		page, err := past.Facts(ctx, principalFrom(ctx),
			HistoryFactQuery{HistoryWindow: window, Types: in.Types, Classes: classes})
		if err != nil {
			return nil, err
		}

		return &historyFactsOutput{Body: emptyFacts(page)}, nil
	})
}

func registerDeviceHistorySamples(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "list-device-history-samples",
		Method:      http.MethodGet,
		Path:        deviceHistorySamplesPath,
		Summary:     "Read one device's stored measurements",
		Description: historyIntro + "One device's stored measurements, which is what a chart of a " +
			"past print is drawn from. A device this server has never met and one the caller may not " +
			"read are the same refusal." + historyNarrowing,
		Tags:   []string{"history"},
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusNotImplemented},
	}, func(ctx context.Context, in *deviceHistorySamplesInput) (*historySamplesOutput, error) {
		past, window, err := deviceHistoryWindowOf(ctx, cfg, in.HistoryWindowParams, in.ID)
		if err != nil {
			return nil, err
		}

		page, err := past.Samples(ctx, principalFrom(ctx),
			HistorySampleQuery{HistoryWindow: window, Metrics: in.Metrics})
		if err != nil {
			return nil, err
		}

		return &historySamplesOutput{Body: emptySamples(page)}, nil
	})
}

// HistoryResolver answers the store that serves a history query, computed per
// request rather than captured at boot.
//
// It reports its own refusal rather than a bare nil, because one boolean cannot
// carry two absences (ADR-20260901-rf2 rule 16): no serving instance is the 501 that
// says this deployment keeps no past, and two serving instances both claiming
// the asked verb is a configuration conflict the server refuses rather than
// guessing its way through.
type HistoryResolver func() (HistoryStore, error)

// resolveHistory is the store this request reads, or the refusal that stands in
// for it. A nil resolver is a build that wired none, which reads to a caller
// exactly like a deployment that declared none.
func resolveHistory(cfg Config) (HistoryStore, error) {
	if cfg.History == nil {
		return nil, httpfault.HistoryNotConfigured()
	}

	past, err := cfg.History()
	if err != nil {
		return nil, err
	}

	if past == nil {
		return nil, httpfault.HistoryNotConfigured()
	}

	return past, nil
}

// historyWindowOf reads a fleet-wide query, resolving the store before the
// query is built so a deployment that keeps no past refuses without parsing
// one.
func historyWindowOf(
	cfg Config, in HistoryWindowParams, requested []string,
) (HistoryStore, HistoryWindow, error) {
	past, err := resolveHistory(cfg)
	if err != nil {
		return nil, HistoryWindow{}, err
	}

	origins, err := originsFrom(requested)
	if err != nil {
		return nil, HistoryWindow{}, err
	}

	return past, HistoryWindow{
		Origins: origins, From: in.From, To: in.To, Limit: in.Limit, Cursor: in.Cursor,
	}, nil
}

// deviceHistoryWindowOf reads a query under one device, and resolves that
// device the way every other path under /api/v1/devices/{device_id} resolves it.
//
// The lookup is the device plane's own, so a device the caller may not read and
// one this server has never met produce the same 404 here as they do on the
// device itself. Deciding it a second way is how two paths under one segment
// end up disagreeing about which devices exist.
func deviceHistoryWindowOf(
	ctx context.Context, cfg Config, in HistoryWindowParams, id string,
) (HistoryStore, HistoryWindow, error) {
	past, err := resolveHistory(cfg)
	if err != nil {
		return nil, HistoryWindow{}, err
	}

	if _, err = cfg.Devices.Get(ctx, principalFrom(ctx), id); err != nil {
		return nil, HistoryWindow{}, err
	}

	origin, err := event.NewOrigin(event.KindDevice, id)
	if err != nil {
		return nil, HistoryWindow{}, httpfault.RequestInvalid("path.device_id",
			"it is not an id this server could have minted", err.Error())
	}

	return past, HistoryWindow{
		Origins: []event.Origin{origin}, From: in.From, To: in.To, Limit: in.Limit, Cursor: in.Cursor,
	}, nil
}

// classesFrom reads the class parameter, refusing a word this build has no
// class for rather than answering the query as if it had not been asked.
//
// A silently ignored filter is accepted on the way in, bound to nothing, and
// all four values answer the same rows. A client cannot tell that from a
// server that filtered and found everything.
func classesFrom(asked []string) ([]event.Class, error) {
	if len(asked) == 0 {
		return nil, nil
	}

	classes := make([]event.Class, 0, len(asked))

	for _, one := range asked {
		class := event.Class(one)
		if !class.Valid() {
			return nil, httpfault.RequestInvalid("query.classes",
				"it is not a class this build publishes", one)
		}

		classes = append(classes, class)
	}

	return classes, nil
}

// emptyFacts renders a page with no rows as an empty array rather than as null,
// so a client that iterates the member without a null check is correct on every
// answer.
func emptyFacts(page HistoryFactPage) HistoryFactPage {
	if page.Items == nil {
		page.Items = []HistoryFact{}
	}

	return page
}

// emptySamples is emptyFacts over the other half.
func emptySamples(page HistorySamplePage) HistorySamplePage {
	if page.Items == nil {
		page.Items = []HistorySample{}
	}

	return page
}
