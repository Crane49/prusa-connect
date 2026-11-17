package routes

import (
	"errors"
	"fmt"
	"net/http"
	"sort"

	"modernc.org/sqlite"

	"github.com/prusa3d/materials/internal/importer"
	"github.com/prusa3d/materials/internal/stock"
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
	// Name is the identifier, such as materials/NOT_FOUND.
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
	// InvalidQuery reports a query parameter or a path segment this module
	// cannot read, such as a spool id that is not a number.
	InvalidQuery = Code{
		Name:   "materials/INVALID_QUERY",
		Status: http.StatusBadRequest,
		Title:  "the request could not be read",
	}

	// NotFound reports a material, a spool or a path the caller cannot see.
	NotFound = Code{
		Name:   "materials/NOT_FOUND",
		Status: http.StatusNotFound,
		Title:  "there is nothing here with that id",
	}

	// MaterialInvalid reports a custom material without a name or a known class.
	MaterialInvalid = Code{
		Name:   "materials/MATERIAL_INVALID",
		Status: http.StatusUnprocessableEntity,
		Title:  "the material was not accepted",
	}

	// MaterialInUse reports a delete of a custom material a live spool holds.
	MaterialInUse = Code{
		Name:   "materials/MATERIAL_IN_USE",
		Status: http.StatusConflict,
		Title:  "a spool in stock is still of this material",
	}

	// SpoolInvalid reports a spool with a weight or a quantity it cannot have.
	SpoolInvalid = Code{
		Name:   "materials/SPOOL_INVALID",
		Status: http.StatusUnprocessableEntity,
		Title:  "the spool was not accepted",
	}

	// LocationInvalid reports a location name that is empty, more than one
	// line or too long.
	LocationInvalid = Code{
		Name:   "materials/LOCATION_INVALID",
		Status: http.StatusUnprocessableEntity,
		Title:  "the location was not accepted",
	}

	// LocationTaken reports a rename to a name another location has.
	LocationTaken = Code{
		Name:   "materials/LOCATION_TAKEN",
		Status: http.StatusConflict,
		Title:  "another location already has this name",
	}

	// LocationsFull reports a new location past the most the module keeps.
	LocationsFull = Code{
		Name:   "materials/LOCATIONS_FULL",
		Status: http.StatusConflict,
		Title:  "the module already keeps as many locations as it may",
	}

	// ShelfFull reports an add that would pass max_spools.
	ShelfFull = Code{
		Name:   "materials/SHELF_FULL",
		Status: http.StatusConflict,
		Title:  "the module already holds as many spools as it may",
	}

	// ImportRunning reports a download asked for while one is running.
	ImportRunning = Code{
		Name:   "materials/IMPORT_RUNNING",
		Status: http.StatusConflict,
		Title:  "the catalogue is already being downloaded",
	}

	// Busy reports a write SQLite refused because another write, such as a
	// catalogue import, holds its lock. It works again once that one is done.
	Busy = Code{
		Name:      "materials/BUSY",
		Status:    http.StatusServiceUnavailable,
		Title:     "the database is busy with another write",
		Retryable: true,
	}

	// NoPrincipal reports a request the host attributed to nobody. Every route
	// here acts as the person the host named, so there is nobody to act as.
	NoPrincipal = Code{
		Name:   "materials/NO_PRINCIPAL",
		Status: http.StatusForbidden,
		Title:  "the request carries no signed-in person",
	}

	// NotOperator reports a change to a printer tool by a person who may not
	// operate that printer.
	NotOperator = Code{
		Name:   "materials/NOT_OPERATOR",
		Status: http.StatusForbidden,
		Title:  "you may not operate this printer",
	}

	// AccessUnknown reports a change to a printer tool when the host gave no
	// answer on whether the person may operate the printer. The change is not
	// made.
	AccessUnknown = Code{
		Name:      "materials/ACCESS_UNKNOWN",
		Status:    http.StatusServiceUnavailable,
		Title:     "the server could not say whether you may operate this printer",
		Retryable: true,
	}

	// ToolRefused reports a change the rules refuse in the printer's
	// state or for what it reports. params.reason is the rule, and
	// params.state is the printer's state.
	ToolRefused = Code{
		Name:   "materials/TOOL_REFUSED",
		Status: http.StatusConflict,
		Title:  "the material on this tool cannot be changed now",
	}

	// SpoolOnAnotherTool reports a spool assigned while it is on another tool.
	SpoolOnAnotherTool = Code{
		Name:   "materials/SPOOL_ON_ANOTHER_TOOL",
		Status: http.StatusConflict,
		Title:  "the spool is on another tool",
	}

	// SpoolArchived reports an archived spool put on a tool or merged.
	SpoolArchived = Code{
		Name:   "materials/SPOOL_ARCHIVED",
		Status: http.StatusConflict,
		Title:  "the spool is archived",
	}

	// SpoolAssigned reports a change to a spool that is on a tool.
	SpoolAssigned = Code{
		Name:   "materials/SPOOL_ASSIGNED",
		Status: http.StatusConflict,
		Title:  "the spool is on a printer tool",
	}

	// NotReported reports an add of a reported spool on a tool whose spool
	// carries no NFC tag.
	NotReported = Code{
		Name:   "materials/NOT_REPORTED",
		Status: http.StatusNotFound,
		Title:  "the printer reports no tagged spool on this tool",
	}

	// MergeRefused reports a merge that is not of a manual spool into a
	// printer spool of the same material.
	MergeRefused = Code{
		Name:   "materials/MERGE_REFUSED",
		Status: http.StatusUnprocessableEntity,
		Title:  "only a spool added by hand merges, into a spool a printer reported of the same material",
	}

	// StoreFailed reports a database that could not answer. Nothing the
	// caller sent is at fault and the same request can work a moment later.
	StoreFailed = Code{
		Name:      "materials/STORE_FAILED",
		Status:    http.StatusServiceUnavailable,
		Title:     "the database could not be read or written",
		Retryable: true,
	}
)

// Codes answers every code this module can return, sorted by name. The test
// that holds openapi.json to this list reads it, so a code added here without
// a line there fails the build.
func Codes() []Code {
	held := []Code{
		InvalidQuery, NotFound, MaterialInvalid, MaterialInUse, SpoolInvalid, ShelfFull, LocationInvalid, LocationTaken,
		LocationsFull,
		ImportRunning, Busy, NoPrincipal, StoreFailed, NotOperator, AccessUnknown, ToolRefused, SpoolOnAnotherTool,
		SpoolAssigned, SpoolArchived, NotReported, MergeRefused,
	}

	sort.Slice(held, func(a, b int) bool { return held[a].Name < held[b].Name })

	return held
}

// SQLite's primary result codes for a lock another connection holds.
const (
	sqliteBusy   = 5
	sqliteLocked = 6
)

// codeFor is the code behind one error from the store. invalid is what
// stock.ErrInvalid means on the route that met it. An error with no mapping is
// a failure of this module's own, so it answers StoreFailed and the detail the
// caller reads names no statement.
func codeFor(err error, invalid Code) Code {
	var lite *sqlite.Error
	if errors.As(err, &lite) && (lite.Code()&0xff == sqliteBusy || lite.Code()&0xff == sqliteLocked) {
		return Busy
	}

	if stock.Refused(err) {
		return invalid
	}

	for _, pair := range []struct {
		err  error
		code Code
	}{
		{stock.ErrInvalid, invalid},
		{stock.ErrNotFound, NotFound},
		{stock.ErrInUse, MaterialInUse},
		{stock.ErrTaken, LocationTaken},
		{stock.ErrFull, ShelfFull},
		{stock.ErrTooManyLocations, LocationsFull},
		{stock.ErrSpoolAssigned, SpoolAssigned},
		{stock.ErrSpoolArchived, SpoolArchived},
		{stock.ErrSpoolOnAnotherTool, SpoolOnAnotherTool},
		{stock.ErrMergeRefused, MergeRefused},
		{importer.ErrRunning, ImportRunning},
		// A request that met the importer a configure replaced: the next one meets the new.
		{importer.ErrClosed, Busy},
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

// safeDetail is the sentence a caller reads for one error. A store failure
// carries a fixed sentence: the store's own text names the engine and the
// statement, and neither is the caller's business.
func safeDetail(code Code, err error) string {
	if code.Retryable {
		return fmt.Sprintf("%s. Try the same request again in a moment.", code.Title)
	}

	return err.Error()
}
