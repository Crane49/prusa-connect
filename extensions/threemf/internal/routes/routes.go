// Package routes is the HTTP surface of this module: the project of a 3MF
// file, its pictures and one object as triangles (ADR-20261027-4tq).
//
// A module opens no port. The server receives the request, works out who is
// calling, takes view_resource on this module's instance and only then hands
// the request over. Each route then reads the file as the person the server
// named, so the storage takes read_storage for that person. A handler refuses
// a request the server attributed to nobody, because it then has nobody to
// read as.
package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"

	"github.com/prusa3d/threemf/internal/document"
	"github.com/prusa3d/threemf/internal/projects"
)

// The bounds of the query parameters, the ones storageapi publishes.
const (
	maxStorageBytes = storageapi.MaxStorageBytes
	maxPathBytes    = storageapi.MaxPathBytes
	// maxThumbnailIndex is the highest picture index a request may name.
	maxThumbnailIndex = 1024
	// maxObjectIndex is the highest object index a request may name.
	maxObjectIndex = 100_000
)

// New is the whole HTTP surface of this module. Every path is relative to the
// mount the host assigned, so /project here is
// /api/v1/modules/<namespace>/project to a caller.
func New(reader *projects.Reader) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /project", reading(reader, project))
	mux.HandleFunc("GET /project/thumbnail", reading(reader, thumbnail))
	mux.HandleFunc("GET /project/mesh", reading(reader, mesh))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		answer(w, http.StatusNotFound, map[string]string{"module_saw": r.URL.Path})
	})

	return mux
}

// handler is one route, with the file and the person already read off the
// request.
type handler func(w http.ResponseWriter, r *http.Request, reader *projects.Reader, file projects.Request)

// reading refuses a request with no person or no readable file reference,
// and hands every other request the file it names.
func reading(reader *projects.Reader, next handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		by := routesapi.PrincipalOf(r)
		if strings.TrimSpace(by) == "" {
			refuse(w, NoPrincipal, "the host named no person for this request", nil)

			return
		}

		query := r.URL.Query()
		file := projects.Request{Principal: by, Storage: query.Get("storage"), Path: query.Get("path")}

		switch {
		case file.Storage == "" || len(file.Storage) > maxStorageBytes:
			refuse(w, InvalidQuery, fmt.Sprintf("storage is required and at most %d bytes", maxStorageBytes),
				map[string]any{"field": "storage"})

			return
		case file.Path == "" || len(file.Path) > maxPathBytes:
			refuse(w, InvalidQuery, fmt.Sprintf("path is required and at most %d bytes", maxPathBytes),
				map[string]any{"field": "path"})

			return
		}

		next(w, r, reader, file)
	}
}

// project answers what one project holds.
func project(w http.ResponseWriter, r *http.Request, reader *projects.Reader, file projects.Request) {
	raw, ok := flag(w, r, "raw_settings")
	if !ok {
		return
	}

	read, _, err := reader.Project(r.Context(), file)
	if err != nil {
		failed(w, file, err)

		return
	}

	answerFile(w, file, document.Of(read, raw))
}

// thumbnail serves one picture of a project by its index.
func thumbnail(w http.ResponseWriter, r *http.Request, reader *projects.Reader, file projects.Request) {
	index, ok := whole(w, r, "index", 0, maxThumbnailIndex, false)
	if !ok {
		return
	}

	read, info, err := reader.Project(r.Context(), file)
	if err != nil {
		failed(w, file, err)

		return
	}

	if index >= len(read.Thumbnails) {
		refuse(w, NoThumbnail, fmt.Sprintf("the project carries %d pictures", len(read.Thumbnails)),
			fileParams(file, map[string]any{"index": index}))

		return
	}

	// The tag is the file's version and the index: a new version of the file
	// is a new tag, and the picture of one version never changes.
	tag := fmt.Sprintf(`"%d-%d-%d"`, info.Size, info.MTime, index)
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "private, no-cache")

	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	picture := read.Thumbnails[index]
	w.Header().Set("Content-Type", picture.MediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(picture.Data) // a picture is held to 1 MiB, the bound of the recorder
}

// mesh answers one object as triangles.
func mesh(w http.ResponseWriter, r *http.Request, reader *projects.Reader, file projects.Request) {
	object, ok := whole(w, r, "object", 0, maxObjectIndex, true)
	if !ok {
		return
	}

	read, err := reader.Mesh(r.Context(), file, object)
	if err != nil {
		failed(w, file, err)

		return
	}

	answerFile(w, file, document.MeshOf(object, read))
}

// flag reads an optional true or false.
func flag(w http.ResponseWriter, r *http.Request, name string) (bool, bool) {
	text := r.URL.Query().Get(name)
	if text == "" {
		return false, true
	}

	value, err := strconv.ParseBool(text)
	if err != nil {
		refuse(w, InvalidQuery, name+" is true or false", map[string]any{"field": name})

		return false, false
	}

	return value, true
}

// whole reads a whole number from low to high.
func whole(w http.ResponseWriter, r *http.Request, name string, low, high int, required bool) (int, bool) {
	text := r.URL.Query().Get(name)
	if text == "" && !required {
		return low, true
	}

	value, err := strconv.Atoi(text)
	if err != nil || value < low || value > high {
		refuse(w, InvalidQuery, fmt.Sprintf("%s is a whole number from %d to %d", name, low, high),
			map[string]any{"field": name})

		return 0, false
	}

	return value, true
}

// failed answers the refusal behind one read error.
func failed(w http.ResponseWriter, file projects.Request, err error) {
	code := codeFor(err)

	// A failure of the storage or of this module is told by its title alone,
	// because its text names the inside of this process.
	detail := code.Title
	if code != ReadFailed {
		detail = err.Error()
	}

	refuse(w, code, detail, paramsFor(err, fileParams(file, nil)))
}

// fileParams names the file in a refusal.
func fileParams(file projects.Request, params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}

	params["storage"] = file.Storage
	params["path"] = file.Path

	return params
}

// answerFile writes the JSON answer about one file. A module answers at most
// routesapi.MaxModuleBody. A project of thousands of copies can pass it, and a
// cut answer would not parse, so it is refused as too large.
func answerFile(w http.ResponseWriter, file projects.Request, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		failed(w, file, err)

		return
	}

	if len(encoded) > routesapi.MaxModuleBody {
		refuse(w, TooLarge, fmt.Sprintf("the answer is %d bytes, and a module answers at most %d",
			len(encoded), routesapi.MaxModuleBody), fileParams(file, map[string]any{
			"field": "answer", "max": routesapi.MaxModuleBody,
		}))

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded) // held to the recorder's bound above
}

// refuse writes one refusal.
func refuse(w http.ResponseWriter, code Code, detail string, params map[string]any) {
	body, err := json.Marshal(problemOf(code, detail, params))
	if err != nil {
		http.Error(w, code.Title, code.Status)

		return
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code.Status)
	_, _ = w.Write(body) // a refusal is a few hundred bytes
}

// answer writes one JSON document.
func answer(w http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		refuse(w, ReadFailed, "the answer could not be written", nil)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(encoded) // the recorder fails only past the module's bound
}
