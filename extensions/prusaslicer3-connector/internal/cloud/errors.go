// Package cloud writes replies in the shapes Prusa Connect and Prusa Account
// answer with, so PrusaSlicer reads a reply from this module the way it reads
// one from the cloud.
//
// Prusa Connect answers every refusal with a JSON object holding a message, a
// code and the extra fields of the error. The message is the English text of
// the code. When the cloud has no text for a code, the message is the code
// itself.
package cloud

import (
	"encoding/json"
	"maps"
	"net/http"
)

// Error is one Prusa Connect refusal.
type Error struct {
	// Status is the HTTP status.
	Status int
	// Code is the name of the ResponseCode, such as BAD_REQUEST.
	Code string
	// Message is the English text of the code.
	Message string
	// Extra are the keyword arguments the cloud adds to the body, such as
	// reason.
	Extra map[string]any
}

// Write renders the refusal.
func (e Error) Write(w http.ResponseWriter) {
	body := map[string]any{}
	maps.Copy(body, e.Extra)

	body["message"] = e.Message
	body["code"] = e.Code

	JSON(w, e.Status, body)
}

// JSON writes one JSON reply.
func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// BadRequest is the refusal of a request with a reason, as the cloud refuses
// an upload. The cloud has no English text for BAD_REQUEST, so the message is
// the code.
func BadRequest(reason string) Error {
	extra := map[string]any{}
	if reason != "" {
		extra["reason"] = reason
	}

	return Error{Status: http.StatusBadRequest, Code: "BAD_REQUEST", Message: "BAD_REQUEST", Extra: extra}
}

// InvalidExtension is InvalidFileType: the file is not a print file.
func InvalidExtension() Error {
	return Error{Status: http.StatusBadRequest, Code: "INVALID_EXTENSION", Message: "Invalid extension of a file"}
}

// PayloadTooLarge is the refusal of a file over the bound.
func PayloadTooLarge() Error {
	return Error{
		Status: http.StatusRequestEntityTooLarge, Code: "PAYLOAD_TOO_LARGE",
		Message: "Request payload is too large",
	}
}

// Unauthorized is the refusal of a request with no valid credential.
func Unauthorized() Error {
	return Error{Status: http.StatusUnauthorized, Code: "UNAUTHORIZED", Message: "Missing or invalid security token"}
}

// Forbidden is the refusal of a caller who may not do what they asked.
func Forbidden() Error {
	return Error{
		Status: http.StatusForbidden, Code: "FORBIDDEN",
		Message: "The server understood the request but refuses to authorize it",
	}
}

// NotFound is the refusal of a thing that does not exist or that the caller
// may not see.
func NotFound() Error {
	return Error{Status: http.StatusNotFound, Code: "NOT_FOUND", Message: "NOT_FOUND"}
}

// Unavailable is the refusal of a request the module cannot serve yet.
func Unavailable() Error {
	return Error{Status: http.StatusServiceUnavailable, Code: "SERVICE_UNAVAILABLE", Message: "SERVICE_UNAVAILABLE"}
}

// InvalidFileNameChars is the refusal of a name holding a character the
// printer's file system does not take.
func InvalidFileNameChars() Error {
	return Error{
		Status: http.StatusBadRequest, Code: "INVALID_FILENAME_CHARS",
		Message: "Invalid file's name due to invalid characters",
	}
}

// InvalidFileNameLength is the refusal of a name longer than the printer takes.
func InvalidFileNameLength() Error {
	return Error{
		Status: http.StatusBadRequest, Code: "INVALID_FILENAME_LENGTH",
		Message: "Invalid file/folder's name due to exceeding length",
	}
}

// PrinterNoTransfer is the refusal of a printer that takes no file.
func PrinterNoTransfer() Error {
	return Error{Status: http.StatusBadRequest, Code: "PRINTER_NO_TRASFER", Message: "PRINTER_NO_TRASFER"}
}
