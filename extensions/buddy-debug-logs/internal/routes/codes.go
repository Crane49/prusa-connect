package routes

import (
	"encoding/json"
	"net/http"
	"sort"
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
	// Name is the identifier, such as buddy-debug-logs/INVALID_QUERY.
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
	// cannot read, such as a time that is not RFC 3339.
	InvalidQuery = Code{
		Name: "buddy-debug-logs/INVALID_QUERY", Status: http.StatusBadRequest,
		Title: "the request could not be read",
	}

	// Invalid reports a body a person cannot have meant, such as a host
	// longer than the printer takes.
	Invalid = Code{
		Name: "buddy-debug-logs/INVALID", Status: http.StatusBadRequest,
		Title: "the module did not accept what was sent",
	}

	// BodyTooLarge reports a request body over the bound this module reads.
	BodyTooLarge = Code{
		Name: "buddy-debug-logs/BODY_TOO_LARGE", Status: http.StatusRequestEntityTooLarge,
		Title: "the request body is larger than this module reads",
	}

	// NoPrincipal reports a request the host attributed to nobody.
	NoPrincipal = Code{
		Name: "buddy-debug-logs/NO_PRINCIPAL", Status: http.StatusForbidden,
		Title: "the request carries no signed-in person",
	}

	// NotReady reports a request that arrived before the host sent the
	// settings.
	NotReady = Code{
		Name: "buddy-debug-logs/NOT_READY", Status: http.StatusServiceUnavailable,
		Title: "the module is still starting", Retryable: true,
	}

	// NoStorage reports a module the operator granted no data directory, so
	// it keeps nothing to read.
	NoStorage = Code{
		Name: "buddy-debug-logs/NO_STORAGE", Status: http.StatusServiceUnavailable,
		Title: "the module was granted no data directory, so it keeps nothing",
	}

	// SourceNotFound reports a MAC no datagram has come from.
	SourceNotFound = Code{
		Name: "buddy-debug-logs/SOURCE_NOT_FOUND", Status: http.StatusNotFound,
		Title: "no datagram has come from that MAC address",
	}

	// DeviceNotFound reports a device the caller may not see or that does
	// not exist. They are one refusal, as the host makes them one.
	DeviceNotFound = Code{
		Name: "buddy-debug-logs/DEVICE_NOT_FOUND", Status: http.StatusNotFound,
		Title: "there is no such printer",
	}

	// NotListening reports a printer set up to send a stream on a port this
	// module was not granted, where nothing would receive it.
	NotListening = Code{
		Name: "buddy-debug-logs/NOT_LISTENING", Status: http.StatusConflict,
		Title: "the module was granted no port for that stream",
	}

	// GCodeNotAllowed reports a person who may not send raw G-code to the
	// printer, which setting it up takes.
	GCodeNotAllowed = Code{
		Name: "buddy-debug-logs/GCODE_NOT_ALLOWED", Status: http.StatusForbidden,
		Title: "the person may not send raw G-code to this printer",
	}

	// PrinterRefused reports a printer that did not take the setting, such as
	// one that takes no raw G-code.
	PrinterRefused = Code{
		Name: "buddy-debug-logs/PRINTER_REFUSED", Status: http.StatusUnprocessableEntity,
		Title: "the printer did not take the setting",
	}

	// PrinterUnavailable reports a printer that is away or busy.
	PrinterUnavailable = Code{
		Name: "buddy-debug-logs/PRINTER_UNAVAILABLE", Status: http.StatusServiceUnavailable,
		Title: "the printer cannot take a command now", Retryable: true,
	}

	// PurgeFailed reports a purge that could not remove every record it
	// should. What it removed before the failure stays removed.
	PurgeFailed = Code{
		Name: "buddy-debug-logs/PURGE_FAILED", Status: http.StatusServiceUnavailable,
		Title: "some records could not be removed", Retryable: true,
	}

	// CursorStale reports a cursor written before a purge moved the records,
	// or before the module restarted. The caller reads the newest page again.
	CursorStale = Code{
		Name: "buddy-debug-logs/CURSOR_STALE", Status: http.StatusConflict,
		Title: "the kept records changed since that page was read",
	}

	// FleetUnavailable reports that the server did not answer which printers
	// the caller may see, so no record can be shown.
	FleetUnavailable = Code{
		Name: "buddy-debug-logs/FLEET_UNAVAILABLE", Status: http.StatusServiceUnavailable,
		Title: "the printers you may see could not be read", Retryable: true,
	}

	// PurgeNotAllowed reports a purge by a person who may not see every
	// printer, whose records the purge would remove.
	PurgeNotAllowed = Code{
		Name: "buddy-debug-logs/PURGE_NOT_ALLOWED", Status: http.StatusForbidden,
		Title: "only a person who may see every printer may delete the kept records",
	}

	// StoreFailed reports a read of the kept records that failed.
	StoreFailed = Code{
		Name: "buddy-debug-logs/STORE_FAILED", Status: http.StatusServiceUnavailable,
		Title: "the kept records could not be read", Retryable: true,
	}
)

// Codes answers every code this module can return, sorted by name.
func Codes() []Code {
	held := []Code{
		InvalidQuery, Invalid, BodyTooLarge, NoPrincipal, NotReady, NoStorage, SourceNotFound,
		DeviceNotFound, NotListening, GCodeNotAllowed, PrinterRefused, PrinterUnavailable, StoreFailed,
		PurgeFailed, CursorStale, FleetUnavailable, PurgeNotAllowed,
	}

	sort.Slice(held, func(a, b int) bool { return held[a].Name < held[b].Name })

	return held
}

// problem is one error, in the body shape RFC 9457 defines. The client SDK
// reads it: code reaches ApiError.code and detail reaches ApiError.message.
type problem struct {
	Type      string         `json:"type"`
	Status    int            `json:"status"`
	Code      string         `json:"code"`
	Title     string         `json:"title"`
	Detail    string         `json:"detail"`
	Params    map[string]any `json:"params"`
	Retryable bool           `json:"retryable"`
}

// refuse writes one refusal.
func refuse(w http.ResponseWriter, code Code, detail string, params map[string]any) {
	if params == nil {
		params = map[string]any{}
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code.Status)

	_ = json.NewEncoder(w).Encode(problem{ //nolint:errchkjson // the caller has gone when this fails.
		Type: "about:blank", Status: code.Status, Code: code.Name, Title: code.Title,
		Detail: detail, Params: params, Retryable: code.Retryable,
	})
}
