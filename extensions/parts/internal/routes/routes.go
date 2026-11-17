// Package routes is the HTTP surface of this module: the handlers, and the
// errors they answer with.
//
// A module opens no port. The server receives the request, works out who is
// calling, and decides whether that person may call this module. Only then
// does it hand the request over. So a handler here acts as the person the
// server named, and checks no permission of its own beyond one: a comment is
// changed only by the person who wrote it.
//
// A handler refuses a request the server attributed to nobody, because it then
// has nobody to act as.
package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"github.com/prusa3d/parts/internal/catalog"
)

// MaxBodyBytes is the largest request body a handler reads. The server already
// bounds the whole call at routesapi.MaxModuleBody, and this is far below it.
//
// JSON may spell any character as \uXXXX, which is six bytes for a one-byte
// letter. So a description at its 16 KiB limit can arrive as 96 KiB. The
// bound is six times the largest field, plus room for every other field
// spelt the same way. A version naming its full set of files fits too.
const MaxBodyBytes = 6*catalog.MaxDescriptionBytes + 64<<10

// New is the whole HTTP surface of this module. Every path is relative to the
// mount the host assigned, so /parts here is /api/v1/modules/<namespace>/parts
// to a caller.
func New(held *catalog.Catalog) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /overview", acting(held, overview))
	mux.HandleFunc("POST /folders", acting(held, addFolder))
	mux.HandleFunc("PATCH /folders/{id}", acting(held, changeFolder))
	mux.HandleFunc("DELETE /folders/{id}", acting(held, deleteFolder))
	mux.HandleFunc("GET /parts", acting(held, listParts))
	mux.HandleFunc("POST /parts", acting(held, addPart))
	mux.HandleFunc("GET /parts/{id}", acting(held, getPart))
	mux.HandleFunc("PATCH /parts/{id}", acting(held, changePart))
	mux.HandleFunc("DELETE /parts/{id}", acting(held, deletePart))
	mux.HandleFunc("POST /parts/{id}/versions", acting(held, addVersion))
	mux.HandleFunc("PATCH /parts/{id}/versions/{number}", acting(held, changeVersion))
	mux.HandleFunc("DELETE /parts/{id}/versions/{number}", acting(held, deleteVersion))
	mux.HandleFunc("POST /parts/{id}/comments", acting(held, addComment))
	mux.HandleFunc("PATCH /parts/{id}/comments/{comment}", acting(held, changeComment))
	mux.HandleFunc("DELETE /parts/{id}/comments/{comment}", acting(held, deleteComment))

	// Everything else is this module's own 404, and the path it reports is
	// what proves the mount was taken off before the request arrived.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		answer(w, http.StatusNotFound, map[string]string{"module_saw": r.URL.Path})
	})

	return mux
}

// handler is one route, with the person the host named already read off the
// request.
type handler func(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string)

// acting refuses a request the server attributed to nobody. It hands every
// other request the name of the person it acts as.
//
// The server decides the permission before it calls. It takes view_resource on
// this module for a read and operate_resource for a write, which openapi.json
// states on each operation. This module widens neither.
func acting(held *catalog.Catalog, next handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		by := routesapi.PrincipalOf(r)
		if strings.TrimSpace(by) == "" {
			refuse(w, NoPrincipal, "the host named no person for this request", nil)

			return
		}

		if err := held.Ensure(r.Context()); err != nil {
			refused(w, err)

			return
		}

		next(w, r, held, by)
	}
}

// overview answers what the catalogue's own screen frames everything with:
// the folder tree, the tags, the room left, and where files go.
func overview(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	place := held.Place()

	answer(w, http.StatusOK, map[string]any{
		"folders":    held.Folders(),
		"root_parts": held.RootParts(),
		"tags":       held.Tags(),
		"usage":      held.Usage(r.Context()),
		"storage":    place.Storage,
		"root":       place.Root,
		// Who the host says is calling. It is the host's answer and not a
		// header this process trusted.
		"principal": by,
	})
}

// addFolder writes one new folder.
func addFolder(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	var draft catalog.FolderDraft
	if !decoded(w, r, &draft) {
		return
	}

	written, err := held.AddFolder(r.Context(), draft, by)
	if err != nil {
		refused(w, err)

		return
	}

	answer(w, http.StatusCreated, map[string]any{"folder": written, "principal": by})
}

// changeFolder renames, moves or describes one folder.
func changeFolder(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	id, ok := number(w, r, "id")
	if !ok {
		return
	}

	var draft catalog.FolderDraft
	if !decoded(w, r, &draft) {
		return
	}

	written, err := held.ChangeFolder(r.Context(), id, draft, by)
	if err != nil {
		refused(w, err)

		return
	}

	answer(w, http.StatusOK, map[string]any{"folder": written, "principal": by})
}

// deleteFolder removes one empty folder.
func deleteFolder(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	id, ok := number(w, r, "id")
	if !ok {
		return
	}

	if err := held.DeleteFolder(r.Context(), id); err != nil {
		refused(w, err)

		return
	}

	answer(w, http.StatusOK, map[string]any{"deleted": id, "principal": by})
}

// listParts answers one page of the parts that match the query.
func listParts(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	query, err := queryOf(r)
	if err != nil {
		refuse(w, InvalidQuery, err.Error(), nil)

		return
	}

	page := held.List(query)

	answer(w, http.StatusOK, map[string]any{
		"parts":     page.Parts,
		"matched":   page.Matched,
		"held":      page.Held,
		"principal": by,
	})
}

// queryOf reads the query parameters of a listing.
func queryOf(r *http.Request) (catalog.Query, error) {
	values := r.URL.Query()
	query := catalog.Query{
		Text:  values.Get("q"),
		Tag:   values.Get("tag"),
		Order: values.Get("order"),
	}

	if len(query.Text) > catalog.MaxNameBytes || len(query.Tag) > catalog.MaxTagBytes {
		return catalog.Query{}, errors.New("q or tag is longer than anything the catalogue holds")
	}

	switch query.Order {
	case "", catalog.OrderName, catalog.OrderChanged:
	default:
		return catalog.Query{}, errors.New("order is name or changed, and it reads " + strconv.Quote(query.Order))
	}

	if written := values.Get("folder"); written != "" {
		folder, err := strconv.Atoi(written)
		if err != nil || folder < 0 || folder > catalog.MaxID {
			return catalog.Query{}, errors.New("folder is a folder id, or 0 for the top, and it reads " +
				strconv.Quote(written))
		}

		query.Folder = &folder
	}

	for field, into := range map[string]*int{"offset": &query.Offset, "limit": &query.Limit} {
		written := values.Get(field)
		if written == "" {
			continue
		}

		count, err := strconv.Atoi(written)
		if err != nil || count < 0 {
			return catalog.Query{}, errors.New(field + " is a whole number, and it reads " + strconv.Quote(written))
		}

		*into = count
	}

	if written := values.Get("deep"); written != "" {
		deep, err := strconv.ParseBool(written)
		if err != nil {
			return catalog.Query{}, errors.New("deep is true or false, and it reads " + strconv.Quote(written))
		}

		query.Deep = deep
	}

	return query, nil
}

// addPart writes one new part.
func addPart(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	var draft catalog.PartDraft
	if !decoded(w, r, &draft) {
		return
	}

	written, err := held.AddPart(r.Context(), draft, by)
	if err != nil {
		refused(w, err)

		return
	}

	detailed(w, held, written.ID, http.StatusCreated, by)
}

// getPart answers one part with its versions and its comments.
func getPart(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	id, ok := number(w, r, "id")
	if !ok {
		return
	}

	detailed(w, held, id, http.StatusOK, by)
}

// changePart writes the fields a draft carries onto a part.
func changePart(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	id, ok := number(w, r, "id")
	if !ok {
		return
	}

	var draft catalog.PartDraft
	if !decoded(w, r, &draft) {
		return
	}

	if _, err := held.ChangePart(r.Context(), id, draft, by); err != nil {
		refused(w, err)

		return
	}

	detailed(w, held, id, http.StatusOK, by)
}

// deletePart removes one part, and answers what the page deletes from the
// storage after it.
func deletePart(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	id, ok := number(w, r, "id")
	if !ok {
		return
	}

	removed, err := held.DeletePart(r.Context(), id)
	if err != nil {
		refused(w, err)

		return
	}

	answer(w, http.StatusOK, map[string]any{"removed": removed, "principal": by})
}

// versionRequest is what changing a version reads: its note.
type versionRequest struct {
	Note string `json:"note"`
}

// addVersion writes a new version of a part.
func addVersion(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	id, ok := number(w, r, "id")
	if !ok {
		return
	}

	var draft catalog.VersionDraft
	if !decoded(w, r, &draft) {
		return
	}

	if _, err := held.AddVersion(r.Context(), id, draft, by); err != nil {
		refused(w, err)

		return
	}

	detailed(w, held, id, http.StatusCreated, by)
}

// changeVersion rewrites the note of one version.
func changeVersion(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	id, ok := number(w, r, "id")
	if !ok {
		return
	}

	version, ok := number(w, r, "number")
	if !ok {
		return
	}

	var body versionRequest
	if !decoded(w, r, &body) {
		return
	}

	if _, err := held.ChangeVersion(r.Context(), id, version, body.Note, by); err != nil {
		refused(w, err)

		return
	}

	detailed(w, held, id, http.StatusOK, by)
}

// deleteVersion removes one version, and answers the files no other version
// names, which the page deletes from the storage after it.
func deleteVersion(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	id, ok := number(w, r, "id")
	if !ok {
		return
	}

	version, ok := number(w, r, "number")
	if !ok {
		return
	}

	removed, err := held.DeleteVersion(r.Context(), id, version)
	if err != nil {
		refused(w, err)

		return
	}

	detail, err := held.Detail(id)
	if err != nil {
		refused(w, err)

		return
	}

	answer(w, http.StatusOK, map[string]any{"removed": removed, "part": detail, "principal": by})
}

// commentRequest is what writing a comment reads.
type commentRequest struct {
	Body string `json:"body"`
}

// addComment writes one comment on a part.
func addComment(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	id, ok := number(w, r, "id")
	if !ok {
		return
	}

	var body commentRequest
	if !decoded(w, r, &body) {
		return
	}

	written, err := held.AddComment(r.Context(), id, body.Body, by)
	if err != nil {
		refused(w, err)

		return
	}

	answer(w, http.StatusCreated, map[string]any{"comment": written, "principal": by})
}

// changeComment rewrites one comment, for the person who wrote it.
func changeComment(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	id, ok := number(w, r, "id")
	if !ok {
		return
	}

	comment, ok := number(w, r, "comment")
	if !ok {
		return
	}

	var body commentRequest
	if !decoded(w, r, &body) {
		return
	}

	written, err := held.ChangeComment(r.Context(), id, comment, body.Body, by)
	if err != nil {
		refused(w, err)

		return
	}

	answer(w, http.StatusOK, map[string]any{"comment": written, "principal": by})
}

// deleteComment removes one comment, for the person who wrote it.
func deleteComment(w http.ResponseWriter, r *http.Request, held *catalog.Catalog, by string) {
	id, ok := number(w, r, "id")
	if !ok {
		return
	}

	comment, ok := number(w, r, "comment")
	if !ok {
		return
	}

	if err := held.DeleteComment(r.Context(), id, comment, by); err != nil {
		refused(w, err)

		return
	}

	answer(w, http.StatusOK, map[string]any{"deleted": comment, "principal": by})
}

// detailed answers one part with everything its page draws.
func detailed(w http.ResponseWriter, held *catalog.Catalog, id, status int, by string) {
	detail, err := held.Detail(id)
	if err != nil {
		refused(w, err)

		return
	}

	answer(w, status, map[string]any{"part": detail, "principal": by})
}

// number reads one id or version number out of the path.
func number(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	written := r.PathValue(name)

	id, err := strconv.Atoi(written)
	if err != nil || id < 1 || id > catalog.MaxID {
		refuse(w, InvalidQuery,
			name+" is a number between 1 and "+strconv.Itoa(catalog.MaxID)+", and it reads "+strconv.Quote(written),
			map[string]any{name: written})

		return 0, false
	}

	return id, true
}

// decoded reads one bounded JSON body into into, and answers whether the
// handler should carry on. A field the module does not read is refused, so a
// misspelt field is reported rather than ignored.
func decoded(w http.ResponseWriter, r *http.Request, into any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err != nil {
		refuse(w, InvalidQuery, "the request body could not be read", nil)

		return false
	}

	if len(body) > MaxBodyBytes {
		refuse(w, BodyTooLarge, "the body is over "+strconv.Itoa(MaxBodyBytes)+" bytes",
			map[string]any{"limit_bytes": MaxBodyBytes})

		return false
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()

	if err = decoder.Decode(into); err != nil {
		refuse(w, Invalid, "the body is not what this route reads: "+err.Error(), nil)

		return false
	}

	return true
}

// refused answers one error from the catalogue under the code for it.
func refused(w http.ResponseWriter, err error) {
	code := codeFor(err)
	refuse(w, code, safeDetail(code, err), nil)
}

// refuse writes one error.
func refuse(w http.ResponseWriter, code Code, detail string, params map[string]any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code.Status)

	_ = json.NewEncoder(w).Encode(document(code, detail, params))
}

// answer writes one JSON body, which is every route above.
func answer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(body)
}
