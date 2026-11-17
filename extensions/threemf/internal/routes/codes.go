package routes

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"

	"github.com/prusa3d/threemf/internal/projects"
	"github.com/prusa3d/threemf/threemf"
)

// Every error this module can answer with, in one place.
//
// The core keeps its own registry of codes and a module's codes are strings
// outside it, so each one starts with this module's short name to stay
// readable in a log beside the core's. A client branches on the code and shows
// the title, so the title is one phrase that never varies and the detail is
// what went wrong this time.

// Code is one error this module answers with.
type Code struct {
	// Name is the identifier, such as threemf/NOT_A_PROJECT.
	Name string
	// Status is the HTTP status this refusal answers with.
	Status int
	// Title is the English phrase for this code. It never varies.
	Title string
	// Retryable says whether sending the same request again could work.
	Retryable bool
}

// The codes this module answers with.
//
//nolint:gochecknoglobals // the code table; a const cannot hold a struct.
var (
	// InvalidQuery reports a query parameter this module cannot read.
	InvalidQuery = Code{
		Name:   "threemf/INVALID_QUERY",
		Status: http.StatusBadRequest,
		Title:  "the request could not be read",
	}

	// NoPrincipal reports a request the host attributed to nobody. Every
	// route reads the file as the person the host named.
	NoPrincipal = Code{
		Name:   "threemf/NO_PRINCIPAL",
		Status: http.StatusForbidden,
		Title:  "the request carries no signed-in person",
	}

	// Denied reports a file the person may not read.
	Denied = Code{
		Name:   "threemf/DENIED",
		Status: http.StatusForbidden,
		Title:  "you may not read this file",
	}

	// NoFile reports a storage or a file that is not there.
	NoFile = Code{
		Name:   "threemf/NO_FILE",
		Status: http.StatusNotFound,
		Title:  "the storage holds no such file",
	}

	// NotAProject reports a file whose name does not end in .3mf, and one the
	// reader refuses as a format. Asking again changes nothing.
	NotAProject = Code{
		Name:   "threemf/NOT_A_PROJECT",
		Status: http.StatusUnprocessableEntity,
		Title:  "the file is not a 3MF project this module can read",
	}

	// TooLarge reports a file past the reader's limits, and a project whose
	// answer is larger than a module may send. params.field names the limit.
	TooLarge = Code{
		Name:   "threemf/PROJECT_TOO_LARGE",
		Status: http.StatusUnprocessableEntity,
		Title:  "the project is larger than this module reads",
	}

	// NoObject reports an object index the project does not have.
	NoObject = Code{
		Name:   "threemf/NO_OBJECT",
		Status: http.StatusNotFound,
		Title:  "the project has no object at that index",
	}

	// NoThumbnail reports a picture index the project does not have.
	NoThumbnail = Code{
		Name:   "threemf/NO_THUMBNAIL",
		Status: http.StatusNotFound,
		Title:  "the project carries no picture at that index",
	}

	// ReadFailed reports a storage that could not answer, or a read that ran
	// out of time. The read goes on, so the same request can work a moment
	// later.
	ReadFailed = Code{
		Name:      "threemf/READ_FAILED",
		Status:    http.StatusServiceUnavailable,
		Title:     "the project could not be read now",
		Retryable: true,
	}
)

// Codes answers every code this module can return, sorted by name. The test
// that holds openapi.json to this list reads it.
func Codes() []Code {
	held := []Code{
		InvalidQuery, NoPrincipal, Denied, NoFile, NotAProject, TooLarge, NoObject, NoThumbnail, ReadFailed,
	}

	sort.Slice(held, func(a, b int) bool { return held[a].Name < held[b].Name })

	return held
}

// codeFor is the code behind one error of a read. An error with no mapping
// is a failure of the storage or of this module, which a later request may
// not meet.
func codeFor(err error) Code {
	for _, pair := range []struct {
		err  error
		code Code
	}{
		{projects.ErrNotAProject, NotAProject},
		{projects.ErrTooLarge, TooLarge},
		{projects.ErrNoObject, NoObject},
		{storageapi.ErrDenied, Denied},
		{storageapi.ErrNoFile, NoFile},
		{storageapi.ErrInvalidPath, NoFile},
		{context.DeadlineExceeded, ReadFailed},
	} {
		if errors.Is(err, pair.err) {
			return pair.code
		}
	}

	return ReadFailed
}

// paramsFor is what a code fills in beyond the file: the limit a file went
// past.
func paramsFor(err error, params map[string]any) map[string]any {
	if limit, ok := errors.AsType[*threemf.LimitError](err); ok {
		params["field"] = limit.Limit
		params["max"] = limit.Max
	}

	return params
}

// problem is one error, in the body shape RFC 9457 defines for a failed HTTP
// request. The client SDK reads it: code reaches ApiError.code and detail
// reaches ApiError.message.
type problem struct {
	Type      string         `json:"type"`
	Status    int            `json:"status"`
	Code      string         `json:"code"`
	Title     string         `json:"title"`
	Detail    string         `json:"detail"`
	Params    map[string]any `json:"params"`
	Retryable bool           `json:"retryable"`
}

// problemOf renders one error, with the values the code fills in.
func problemOf(code Code, detail string, params map[string]any) problem {
	if params == nil {
		params = map[string]any{}
	}

	return problem{
		Type:      "about:blank",
		Status:    code.Status,
		Code:      code.Name,
		Title:     code.Title,
		Detail:    detail,
		Params:    params,
		Retryable: code.Retryable,
	}
}
