package routes

import (
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/prusa3d/parts/internal/catalog"
)

// Every error this module can answer with, in one place.
//
// The core keeps its own registry of codes and a module's codes are strings
// outside it, so each one starts with this module's short name to stay
// readable in a log beside the core's. A client branches on the code and shows
// the title, so the title is one phrase that never varies and the detail is
// what went wrong this time.

// Code is one error this module answers with. A program that calls the module
// branches on Name. A person reads Title.
type Code struct {
	// Name is the identifier, such as parts/PART_NOT_FOUND.
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
	// Invalid reports a value a person cannot have meant: a field over its
	// limit, an empty name, a path outside the part's folder.
	Invalid = Code{
		Name:   "parts/INVALID",
		Status: http.StatusBadRequest,
		Title:  "the catalogue did not accept what was sent",
	}

	// InvalidQuery reports a query parameter or a path segment this module
	// cannot read, such as an id that is not a number.
	InvalidQuery = Code{
		Name:   "parts/INVALID_QUERY",
		Status: http.StatusBadRequest,
		Title:  "the request could not be read",
	}

	// FolderNotFound reports a folder id the catalogue does not hold.
	FolderNotFound = Code{
		Name:   "parts/FOLDER_NOT_FOUND",
		Status: http.StatusNotFound,
		Title:  "the catalogue holds no folder with that id",
	}

	// PartNotFound reports a part id the catalogue does not hold.
	PartNotFound = Code{
		Name:   "parts/PART_NOT_FOUND",
		Status: http.StatusNotFound,
		Title:  "the catalogue holds no part with that id",
	}

	// VersionNotFound reports a version number the part does not have.
	VersionNotFound = Code{
		Name:   "parts/VERSION_NOT_FOUND",
		Status: http.StatusNotFound,
		Title:  "the part has no version with that number",
	}

	// CommentNotFound reports a comment id the part does not have.
	CommentNotFound = Code{
		Name:   "parts/COMMENT_NOT_FOUND",
		Status: http.StatusNotFound,
		Title:  "the part has no comment with that id",
	}

	// NameTaken reports a folder name its parent already holds.
	NameTaken = Code{
		Name:   "parts/NAME_TAKEN",
		Status: http.StatusConflict,
		Title:  "the folder already holds a folder with that name",
	}

	// FolderNotEmpty reports a delete of a folder that still holds something.
	FolderNotEmpty = Code{
		Name:   "parts/FOLDER_NOT_EMPTY",
		Status: http.StatusConflict,
		Title:  "the folder still holds a folder or a part",
	}

	// Changed reports a write against a record somebody else changed first.
	// The caller reads it again and sends the change again.
	Changed = Code{
		Name:      "parts/CHANGED",
		Status:    http.StatusConflict,
		Title:     "somebody else changed this while the request was being served",
		Retryable: true,
	}

	// NotAuthor reports a change to a comment somebody else wrote.
	NotAuthor = Code{
		Name:   "parts/NOT_AUTHOR",
		Status: http.StatusForbidden,
		Title:  "only the person who wrote a comment may change it",
	}

	// FileMissing reports a file a version names that the storage does not
	// hold, or one the person may not read.
	FileMissing = Code{
		Name:   "parts/FILE_MISSING",
		Status: http.StatusUnprocessableEntity,
		Title:  "the storage holds no such file",
	}

	// Full reports a write over a limit: how many versions, files or comments
	// one part keeps.
	Full = Code{
		Name:   "parts/FULL",
		Status: http.StatusInsufficientStorage,
		Title:  "the catalogue already holds as much as it may",
	}

	// BodyTooLarge reports a request body over the bound this module reads.
	BodyTooLarge = Code{
		Name:   "parts/BODY_TOO_LARGE",
		Status: http.StatusRequestEntityTooLarge,
		Title:  "the request body is larger than this module reads",
	}

	// NoPrincipal reports a request the host attributed to nobody. Every route
	// here acts as the person the host named, so there is nobody to act as.
	NoPrincipal = Code{
		Name:   "parts/NO_PRINCIPAL",
		Status: http.StatusForbidden,
		Title:  "the request carries no signed-in person",
	}

	// StoreFailed reports a database or a storage that could not answer, and
	// a module that could not move the records of an earlier release yet. Nothing the caller sent
	// is at fault and the same request can work a moment later.
	StoreFailed = Code{
		Name:      "parts/STORE_FAILED",
		Status:    http.StatusServiceUnavailable,
		Title:     "the catalogue could not be read or written",
		Retryable: true,
	}
)

// Codes answers every code this module can return, sorted by name. The test
// that holds openapi.json and the page's catalogue to this list reads it, so a
// code added here without a line in either fails the build.
func Codes() []Code {
	held := []Code{
		Invalid, InvalidQuery, FolderNotFound, PartNotFound, VersionNotFound, CommentNotFound,
		NameTaken, FolderNotEmpty, Changed, NotAuthor, FileMissing, Full, BodyTooLarge,
		NoPrincipal, StoreFailed,
	}

	sort.Slice(held, func(a, b int) bool { return held[a].Name < held[b].Name })

	return held
}

// codeFor is the code behind one error from the catalogue. An error with no
// mapping is a failure of this module's own, so it answers StoreFailed and the
// detail the caller reads names no path and no statement.
func codeFor(err error) Code {
	for _, pair := range []struct {
		err  error
		code Code
	}{
		{catalog.ErrInvalid, Invalid},
		{catalog.ErrFolderNotFound, FolderNotFound},
		{catalog.ErrPartNotFound, PartNotFound},
		{catalog.ErrVersionNotFound, VersionNotFound},
		{catalog.ErrCommentNotFound, CommentNotFound},
		{catalog.ErrNameTaken, NameTaken},
		{catalog.ErrFolderNotEmpty, FolderNotEmpty},
		{catalog.ErrConflict, Changed},
		{catalog.ErrNotAuthor, NotAuthor},
		{catalog.ErrFileMissing, FileMissing},
		{catalog.ErrFull, Full},
		{catalog.ErrStore, StoreFailed},
	} {
		if errors.Is(err, pair.err) {
			return pair.code
		}
	}

	return StoreFailed
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

// document renders one error, with the values the code fills in.
func document(code Code, detail string, params map[string]any) problem {
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

// safeDetail is the sentence a caller reads for one error.
//
// An error the caller caused carries the catalogue's own sentence, which names
// the field and the limit the caller has to change. A store failure carries a
// fixed sentence instead. The store's own text names the host's engine and its
// statement, and neither is the caller's business.
func safeDetail(code Code, err error) string {
	if code.Name == StoreFailed.Name {
		return fmt.Sprintf("%s. Try the same request again in a moment.", code.Title)
	}

	return err.Error()
}
