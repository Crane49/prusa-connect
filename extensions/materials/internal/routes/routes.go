// Package routes is the HTTP surface of this module: the handlers, and the
// errors they answer with.
//
// A module opens no port. The server receives the request, works out who is
// calling, and decides whether that person may call this module. Only then
// does it hand the request over. So a handler here acts as the person the
// server named and checks no permission of its own. The person filters only
// what is theirs: their custom materials and their favourites.
//
// A handler refuses a request the server attributed to nobody, because it then
// has nobody to act as.
package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"github.com/prusa3d/materials/internal/importer"
	"github.com/prusa3d/materials/internal/settings"
	"github.com/prusa3d/materials/internal/stock"
)

// server is what every handler reads.
type server struct {
	store    *stock.Store
	imp      *importer.Importer
	fetch    importer.Fetch
	live     *settings.Live
	fleet    Fleet
	seen     Seen
	operates *operators
}

// New is the whole HTTP surface of this module. Every path is relative to the
// mount the host assigned, so /materials here is
// /api/v1/modules/<namespace>/materials to a caller. fetch is what a download
// runs, which a test replaces. fleet is the printers as the follower read
// them, seen lists the printers a person may observe, and access is whom the
// routes ask whether a person may operate one.
func New(
	store *stock.Store, imp *importer.Importer, fetch importer.Fetch, live *settings.Live, fleet Fleet, seen Seen,
	access Access,
) http.Handler {
	s := &server{
		store: store, imp: imp, fetch: fetch, live: live, fleet: fleet, seen: seen,
		operates: &operators{access: access, kept: map[operator]kept{}},
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /catalogue", acting(s.catalogue))
	mux.HandleFunc("POST /catalogue/download", acting(s.download))
	mux.HandleFunc("GET /catalogue/brands", acting(s.brands))
	mux.HandleFunc("GET /types", acting(s.types))
	mux.HandleFunc("GET /materials", acting(s.list))
	mux.HandleFunc("GET /materials/{uuid}", acting(s.material))
	mux.HandleFunc("GET /materials/{uuid}/spools", acting(s.spools))
	mux.HandleFunc("POST /custom-materials", acting(s.createCustom))
	mux.HandleFunc("PATCH /custom-materials/{uuid}", acting(s.updateCustom))
	mux.HandleFunc("DELETE /custom-materials/{uuid}", acting(s.deleteCustom))
	mux.HandleFunc("PUT /favourites/{uuid}", acting(s.star(true)))
	mux.HandleFunc("DELETE /favourites/{uuid}", acting(s.star(false)))
	mux.HandleFunc("GET /locations", acting(s.locations))
	mux.HandleFunc("POST /locations", acting(s.createLocation))
	mux.HandleFunc("PATCH /locations/{id}", acting(s.renameLocation))
	mux.HandleFunc("DELETE /locations/{id}", acting(s.deleteLocation))
	mux.HandleFunc("GET /list-filters", acting(s.filters))
	mux.HandleFunc("PUT /list-filters", acting(s.keepFilters))
	mux.HandleFunc("GET /spools", acting(s.allSpools))
	mux.HandleFunc("POST /spools", acting(s.addSpools))
	mux.HandleFunc("PATCH /spools/{id}", acting(s.updateSpool))
	mux.HandleFunc("DELETE /spools/{id}", acting(s.deleteSpool))
	mux.HandleFunc("POST /spools/delete", acting(s.deleteSpools))
	mux.HandleFunc("GET /printers", acting(s.printers))
	mux.HandleFunc("GET /printers/tools", acting(s.allTools))
	mux.HandleFunc("GET /printers/{device}/tools", acting(s.tools))
	mux.HandleFunc("PUT /printers/{device}/tools/{tool}", acting(s.operating(s.assign, nil)))
	mux.HandleFunc("DELETE /printers/{device}/tools/{tool}", acting(s.operating(s.unassign, s.unassignGone)))
	mux.HandleFunc("POST /printers/{device}/tools/{tool}/spool", acting(s.operating(s.addReported, nil)))
	mux.HandleFunc("POST /spools/{id}/merge", acting(s.mergeSpool))

	// Everything else is this module's own 404, and the path it names is what
	// proves the mount was taken off before the request arrived.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		refuse(w, NotFound, "this module serves no route at "+r.URL.Path, map[string]any{"path": r.URL.Path})
	})

	return mux
}

// handler is one route, with the person the host named already read off the
// request.
type handler func(w http.ResponseWriter, r *http.Request, by string)

// acting refuses a request the server attributed to nobody. It hands every
// other request the name of the person it acts as.
//
// The server decides the permission before it calls. It takes view_resource on
// this module for a read and operate_resource for a write, which openapi.json
// states on each operation. This module widens neither.
func acting(next handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		by := routesapi.PrincipalOf(r)
		if strings.TrimSpace(by) == "" {
			refuse(w, NoPrincipal, "the host named no person for this request", nil)

			return
		}

		next(w, r, by)
	}
}

// importView is the state of the last or current import.
type importView struct {
	Phase     importer.Phase  `json:"phase"`
	Reason    importer.Reason `json:"reason,omitempty"`
	Started   time.Time       `json:"started,omitzero"`
	Finished  time.Time       `json:"finished,omitzero"`
	Unchanged bool            `json:"unchanged,omitempty"`
}

// catalogueView is what the database holds and what the importer is doing.
type catalogueView struct {
	Loaded bool                 `json:"loaded"`
	Meta   *stock.CatalogueMeta `json:"meta,omitempty"`
	Import importView           `json:"import"`
}

func (s *server) catalogue(w http.ResponseWriter, r *http.Request, _ string) {
	s.answerCatalogue(w, r, http.StatusOK)
}

// download starts an import on its own goroutine, and answers at once.
func (s *server) download(w http.ResponseWriter, r *http.Request, _ string) {
	if err := s.imp.Start(s.fetch); err != nil {
		refused(w, r, err, InvalidQuery)

		return
	}

	s.answerCatalogue(w, r, http.StatusAccepted)
}

func (s *server) answerCatalogue(w http.ResponseWriter, r *http.Request, status int) {
	meta, loaded, err := s.store.Catalogue(r.Context())
	if err != nil {
		refused(w, r, err, InvalidQuery)

		return
	}

	st := s.imp.Status()
	view := catalogueView{Loaded: loaded, Import: importView{
		Phase: st.Phase, Reason: st.Reason, Started: st.Started, Finished: st.Finished, Unchanged: st.Unchanged,
	}}

	if loaded {
		view.Meta = &meta
	}

	answer(w, status, view)
}

func (s *server) brands(w http.ResponseWriter, r *http.Request, _ string) {
	brands, err := s.store.Brands(r.Context())
	if err != nil {
		refused(w, r, err, InvalidQuery)

		return
	}

	answer(w, http.StatusOK, map[string]any{"brands": brands})
}

func (s *server) types(w http.ResponseWriter, r *http.Request, by string) {
	types, err := s.store.Types(r.Context(), by)
	if err != nil {
		refused(w, r, err, InvalidQuery)

		return
	}

	answer(w, http.StatusOK, map[string]any{"types": types})
}

// locations answers every location with its live spools, for the location
// field and the Locations settings tab.
func (s *server) locations(w http.ResponseWriter, r *http.Request, _ string) {
	locations, err := s.store.Locations(r.Context())
	if err != nil {
		refused(w, r, err, InvalidQuery)

		return
	}

	answer(w, http.StatusOK, map[string]any{"locations": locations})
}

// filters answers the list filters the caller kept last.
func (s *server) filters(w http.ResponseWriter, r *http.Request, by string) {
	kept, err := s.store.Filters(r.Context(), by)
	if err != nil {
		refused(w, r, err, InvalidQuery)

		return
	}

	answer(w, http.StatusOK, map[string]any{"filters": kept})
}

// keepFilters keeps the caller's list filters, the body being the JSON
// object itself.
func (s *server) keepFilters(w http.ResponseWriter, r *http.Request, by string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, stock.MaxFilters+1))
	if err == nil {
		err = s.store.KeepFilters(r.Context(), by, body)
	} else {
		err = fmt.Errorf("%w: %w", stock.ErrInvalid, err)
	}

	if err != nil {
		refused(w, r, err, InvalidQuery)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *server) list(w http.ResponseWriter, r *http.Request, by string) {
	query, err := queryOf(r)
	if err != nil {
		refuse(w, InvalidQuery, err.Error(), nil)

		return
	}

	page, err := s.store.List(r.Context(), by, query)
	if err != nil {
		refused(w, r, err, InvalidQuery)

		return
	}

	answer(w, http.StatusOK, page)
}

// queryOf reads the query parameters of the list. The store checks order.
func queryOf(r *http.Request) (stock.ListQuery, error) {
	values := r.URL.Query()
	query := stock.ListQuery{
		Q: values.Get("q"), Brand: values.Get("brand"), Type: values.Get("type"), Class: values.Get("class"),
		Color: values.Get("color"), Order: values.Get("order"),
	}

	for field, into := range map[string]*bool{
		"favourites": &query.Favourite, "custom": &query.Custom, "in_stock": &query.InStock,
		"assigned": &query.Assigned, "desc": &query.Desc,
	} {
		written := values.Get(field)
		if written == "" {
			continue
		}

		on, err := strconv.ParseBool(written)
		if err != nil {
			return query, errors.New(field + " is true or false, and it reads " + strconv.Quote(written))
		}

		*into = on
	}

	if written := values.Get("location"); written != "" {
		id, err := strconv.ParseInt(written, 10, 64)
		if err != nil || id < 1 {
			return query, errors.New("location is a location id, and it reads " + strconv.Quote(written))
		}

		query.Location = id
	}

	for field, into := range map[string]*int{"offset": &query.Offset, "limit": &query.Limit} {
		written := values.Get(field)
		if written == "" {
			continue
		}

		count, err := strconv.Atoi(written)
		if err != nil || count < 0 {
			return query, errors.New(field + " is a whole number, and it reads " + strconv.Quote(written))
		}

		*into = count
	}

	return query, nil
}

func (s *server) material(w http.ResponseWriter, r *http.Request, by string) {
	detail, err := s.store.Material(r.Context(), by, r.PathValue("uuid"))
	if err != nil {
		refused(w, r, err, InvalidQuery)

		return
	}

	answer(w, http.StatusOK, detail)
}

// spools answers one page of a material's spools; it reads offset and limit
// as the list does.
func (s *server) spools(w http.ResponseWriter, r *http.Request, by string) {
	query, err := queryOf(r)
	if err != nil {
		refuse(w, InvalidQuery, err.Error(), nil)

		return
	}

	page, err := s.store.Spools(r.Context(), r.PathValue("uuid"), query.Offset, query.Limit)
	if err != nil {
		refused(w, r, err, InvalidQuery)

		return
	}

	s.placed(r.Context(), by, page.Spools)

	answer(w, http.StatusOK, page)
}

// allSpools answers one page of every live spool, or of one material's.
func (s *server) allSpools(w http.ResponseWriter, r *http.Request, by string) {
	r.SetPathValue("uuid", r.URL.Query().Get("material"))
	s.spools(w, r, by)
}

func (s *server) createCustom(w http.ResponseWriter, r *http.Request, by string) {
	var fields stock.MaterialFields
	if !decoded(w, r, &fields, MaterialInvalid) {
		return
	}

	custom, err := s.store.CreateCustom(r.Context(), by, fields)
	if err != nil {
		refused(w, r, err, MaterialInvalid)

		return
	}

	answer(w, http.StatusCreated, custom)
}

// updateCustom replaces every field of the caller's custom material.
func (s *server) updateCustom(w http.ResponseWriter, r *http.Request, by string) {
	var fields stock.MaterialFields
	if !decoded(w, r, &fields, MaterialInvalid) {
		return
	}

	custom, err := s.store.UpdateCustom(r.Context(), by, r.PathValue("uuid"), fields)
	if err != nil {
		refused(w, r, err, MaterialInvalid)

		return
	}

	answer(w, http.StatusOK, custom)
}

func (s *server) deleteCustom(w http.ResponseWriter, r *http.Request, by string) {
	if err := s.store.DeleteCustom(r.Context(), by, r.PathValue("uuid")); err != nil {
		refused(w, r, err, MaterialInvalid)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *server) star(on bool) handler {
	return func(w http.ResponseWriter, r *http.Request, by string) {
		if err := s.store.Star(r.Context(), by, r.PathValue("uuid"), on); err != nil {
			refused(w, r, err, InvalidQuery)

			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// spoolsRequest is a spool draft and how many spools of it to add.
type spoolsRequest struct {
	stock.SpoolDraft

	Quantity int `json:"quantity"`
}

func (s *server) addSpools(w http.ResponseWriter, r *http.Request, by string) {
	var body spoolsRequest
	if !decoded(w, r, &body, SpoolInvalid) {
		return
	}

	spools, err := s.store.AddSpools(r.Context(), by, body.SpoolDraft, body.Quantity, s.live.Get().MaxSpools)
	if err != nil {
		refused(w, r, err, SpoolInvalid)

		return
	}

	// The ids alone: 100 whole spools could pass the host's 1 MiB.
	ids := make([]int64, len(spools))
	for i, sp := range spools {
		ids[i] = sp.ID
	}

	answer(w, http.StatusCreated, map[string]any{"added": len(spools), "ids": ids})
}

func (s *server) updateSpool(w http.ResponseWriter, r *http.Request, _ string) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var patch stock.SpoolPatch
	if !decoded(w, r, &patch, SpoolInvalid) {
		return
	}

	spool, err := s.store.UpdateSpool(r.Context(), id, patch)
	if err != nil {
		refused(w, r, err, SpoolInvalid)

		return
	}

	answer(w, http.StatusOK, spool)
}

func (s *server) deleteSpool(w http.ResponseWriter, r *http.Request, _ string) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	if err := s.store.DeleteSpool(r.Context(), id, time.Now()); err != nil {
		refused(w, r, err, SpoolInvalid)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// mergeRequest names the printer spool a manual spool merges into.
type mergeRequest struct {
	Into int64 `json:"into"`
}

// mergeSpool merges a manual spool into the printer spool it is. It changes
// no printer, so the host's operate_resource on this module is all it takes.
func (s *server) mergeSpool(w http.ResponseWriter, r *http.Request, by string) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var body mergeRequest
	if !decoded(w, r, &body, SpoolInvalid) {
		return
	}

	spool, err := s.store.MergeSpool(r.Context(), id, body.Into, time.Now())
	if err != nil {
		refused(w, r, err, SpoolInvalid)

		return
	}

	s.placed(r.Context(), by, []stock.Spool{spool})

	answer(w, http.StatusOK, spool)
}

// spoolIDs is the body of a batch delete.
type spoolIDs struct {
	IDs []int64 `json:"ids"`
}

func (s *server) deleteSpools(w http.ResponseWriter, r *http.Request, _ string) {
	var body spoolIDs
	if !decoded(w, r, &body, SpoolInvalid) {
		return
	}

	if err := s.store.DeleteSpools(r.Context(), body.IDs, time.Now()); err != nil {
		refused(w, r, err, SpoolInvalid)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// locationRequest is the one field a location is made or renamed with.
type locationRequest struct {
	Name string `json:"name"`
}

func (s *server) createLocation(w http.ResponseWriter, r *http.Request, _ string) {
	var body locationRequest
	if !decoded(w, r, &body, LocationInvalid) {
		return
	}

	l, err := s.store.CreateLocation(r.Context(), body.Name)
	if err != nil {
		refused(w, r, err, LocationInvalid)

		return
	}

	answer(w, http.StatusOK, l)
}

func (s *server) renameLocation(w http.ResponseWriter, r *http.Request, _ string) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var body locationRequest
	if !decoded(w, r, &body, LocationInvalid) {
		return
	}

	l, err := s.store.RenameLocation(r.Context(), id, body.Name)
	if err != nil {
		refused(w, r, err, LocationInvalid)

		return
	}

	answer(w, http.StatusOK, l)
}

func (s *server) deleteLocation(w http.ResponseWriter, r *http.Request, _ string) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	if err := s.store.DeleteLocation(r.Context(), id); err != nil {
		refused(w, r, err, LocationInvalid)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// pathID reads the id of a spool or a location out of the path.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	written := r.PathValue("id")

	id, err := strconv.ParseInt(written, 10, 64)
	if err != nil || id < 1 {
		refuse(
			w,
			InvalidQuery,
			"id is a whole number, and it reads "+strconv.Quote(written),
			map[string]any{"id": written},
		)

		return 0, false
	}

	return id, true
}

// decoded reads one JSON body into into, and answers whether the handler
// should carry on. The host bounds the body at routesapi.MaxModuleBody. A
// field the module does not read is refused under invalid, so a misspelt
// field is reported rather than ignored.
func decoded(w http.ResponseWriter, r *http.Request, into any, invalid Code) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(into); err != nil {
		refuse(w, invalid, "the body is not what this route reads: "+err.Error(), nil)

		return false
	}

	return true
}

// refused answers one error from the store under the code for it. The
// caller learns only that the store failed; why goes to standard error, which
// the host keeps as this module's log.
func refused(w http.ResponseWriter, r *http.Request, err error, invalid Code) {
	code := codeFor(err, invalid)
	detail := safeDetail(code, err)

	switch {
	case code == StoreFailed:
		fmt.Fprintf(os.Stderr, "materials: %s %s: %v\n", r.Method, r.URL.Path, err)
	case stock.Refused(err):
		// The engine's text names the statement, which is not the caller's business.
		detail = code.Title + ": a number is out of range, or the request names something that is gone."
	}

	refuse(w, code, detail, nil)
}

// refuse writes one error.
func refuse(w http.ResponseWriter, code Code, detail string, params map[string]any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code.Status)

	_ = json.NewEncoder(w).Encode(document(code, detail, params))
}

// answer writes one JSON body. It leaves <, > and & as they are, which keeps
// a text at most twice its length in JSON; the page parses the answer, never
// embeds it in HTML.
func answer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(body)
}

// Unconfigured answers a request that arrives before the database is open.
//
//nolint:gochecknoglobals // a fixed handler.
var Unconfigured http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	refuse(w, StoreFailed, "the module has not opened its database yet. Try again in a moment.", nil)
})
