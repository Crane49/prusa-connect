package link

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
)

// Reply is one refusal as PrusaLink writes it: a JSON object with a title and
// a message. PrusaSlicer shows the status and the body to the person, so the
// message says what to do.
type Reply struct {
	status  int
	title   string
	message string
}

// Write writes the reply.
func (r Reply) Write(w http.ResponseWriter) {
	writeJSON(w, r.status, map[string]string{"title": r.title, "message": r.message})
}

// writeJSON writes one JSON reply.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func badRequest(message string) Reply {
	return Reply{status: http.StatusBadRequest, title: "Bad Request", message: message}
}

// Unauthorized is the reply to a request no person was resolved for.
func Unauthorized() Reply {
	return Reply{
		status:  http.StatusUnauthorized,
		title:   "Unauthorized",
		message: "Type an API credential of this server as the API key of the physical printer.",
	}
}

// Unavailable is the reply while the module starts.
func Unavailable() Reply {
	return Reply{
		status:  http.StatusServiceUnavailable,
		title:   "Service Unavailable",
		message: "The PrusaSlicer 2 Connector extension is starting. Try again in a moment.",
	}
}

// refusal is the reply to PrusaSlicer for what the host or the file system
// reported about printer. A failure that is no refusal is 503, so the person
// may try again, and its cause is logged with the printer.
func refusal(printer string, err error) Reply {
	switch {
	case errors.Is(err, ErrNoPrinter):
		return Reply{
			status: http.StatusNotFound, title: "Not Found",
			message: "This server has no printer at this address, or the API key may not see it. " +
				"Copy the address again from the printer's page.",
		}
	case errors.Is(err, ErrNoStorage):
		return Reply{status: http.StatusNotFound, title: "Not Found", message: "The printer has no such storage."}
	case errors.Is(err, ErrDenied):
		return Reply{
			status: http.StatusForbidden, title: "Forbidden",
			message: "The API key may not do this on the printer. " +
				"It needs the operate scope, and its person must be allowed to operate the printer.",
		}
	case errors.Is(err, ErrNotReady):
		return Reply{
			status: http.StatusConflict, title: "Conflict",
			message: "The printer is not ready to print. " +
				"Check that it is connected and idle, and that no print is on it.",
		}
	case errors.Is(err, ErrNameTaken):
		return Reply{
			status: http.StatusConflict, title: "Conflict",
			message: "The printer already holds files of this name. Rename the file in the upload dialog.",
		}
	case errors.Is(err, ErrPrintRefused):
		return Reply{status: http.StatusConflict, title: "Conflict", message: "The printer refused to start the print."}
	case errors.Is(err, ErrNoShelf):
		return Reply{
			status: http.StatusServiceUnavailable, title: "Service Unavailable",
			message: "The storage this printer saves uploads into is gone. " +
				"Pick another one, or turn off Save to storage, on the printer's Printer tab.",
		}
	case errors.Is(err, ErrNoFreeName):
		return Reply{
			status: http.StatusConflict, title: "Conflict",
			message: "The storage folder this printer saves uploads into already holds files of this name. " +
				"Rename the file in the upload dialog.",
		}
	case errors.Is(err, ErrCannotCheckKeep):
		return Reply{
			status: http.StatusForbidden, title: "Forbidden",
			message: "This server does not let the PrusaSlicer 2 Connector check who may operate a printer, " +
				"so nobody may change Save to storage. Ask the administrator to grant the extension its operate check.",
		}
	case errors.Is(err, ErrCannotKeep):
		return Reply{
			status: http.StatusForbidden, title: "Forbidden",
			message: "The API key's person may not write into the storage this printer saves uploads into. " +
				"Ask for write access to it, or turn off Save to storage on the printer's Printer tab.",
		}
	case errors.Is(err, errEmpty):
		return badRequest("The file is empty.")
	case errors.Is(err, errNotAForm):
		return badRequest("The upload is not a form with a file.")
	case errors.Is(err, errNoFilePart):
		return badRequest("The form holds no file.")
	default:
		// The slicer is told to try again and nothing more, so the cause is
		// written where the operator reads it. The host logs what the module
		// writes to stderr.
		fmt.Fprintf(os.Stderr, "a PrusaSlicer request for printer %s failed: %v\n", printer, err)

		return Reply{
			status: http.StatusServiceUnavailable, title: "Service Unavailable",
			message: "The server could not finish the upload. Try again.",
		}
	}
}

// pageRefusal is the reply to the printer's card for its own routes, keep
// and seen. It speaks to a person on the page and not to PrusaSlicer.
func pageRefusal(printer string, err error) Reply {
	switch {
	case errors.Is(err, ErrNoPrinter):
		return Reply{status: http.StatusNotFound, title: "Not Found", message: "This server has no such printer."}
	case errors.Is(err, ErrDenied):
		return Reply{
			status: http.StatusForbidden, title: "Forbidden",
			message: "Only a person who may operate this printer changes this.",
		}
	case errors.Is(err, ErrCannotCheckKeep):
		return refusal(printer, err)
	default:
		fmt.Fprintf(os.Stderr, "the card of printer %s could not read or save its setting: %v\n", printer, err)

		return Reply{
			status: http.StatusServiceUnavailable, title: "Service Unavailable",
			message: "The setting could not be read or saved. Try again.",
		}
	}
}
