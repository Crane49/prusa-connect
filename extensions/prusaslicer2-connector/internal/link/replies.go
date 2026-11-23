package link

import (
	"encoding/json"
	"errors"
	"net/http"
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

// refusal is the reply to what the host or the file system reported. A
// failure that is no refusal is 503, so the person may try again.
func refusal(err error) Reply {
	switch {
	case errors.Is(err, ErrNoPrinter):
		return Reply{
			status: http.StatusNotFound, title: "Not Found",
			message: "This server has no printer at this address, or the API key may not see it. " +
				"Copy the address again from the printer's page.",
		}
	case errors.Is(err, ErrNoStorage):
		return Reply{status: http.StatusNotFound, title: "Not Found", message: "The printer has no such storage."}
	case errors.Is(err, ErrNoShelf):
		return Reply{
			status: http.StatusServiceUnavailable, title: "Service Unavailable",
			message: "This server keeps no file without a print. " +
				"Use Upload and Print, or ask the administrator to set the storage of the PrusaSlicer 2 Connector extension.",
		}
	case errors.Is(err, ErrDenied):
		return Reply{
			status: http.StatusForbidden, title: "Forbidden",
			message: "The API key may not do this on the printer. " +
				"It needs the operate scope, and its person must be allowed to operate the printer.",
		}
	case errors.Is(err, ErrCannotKeep):
		return Reply{
			status: http.StatusForbidden, title: "Forbidden",
			message: "The API key's person may not write files into the storage of this server that Upload keeps " +
				"them in. Use Upload and Print, or ask the administrator for write access to that storage.",
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
	case errors.Is(err, errEmpty):
		return badRequest("The file is empty.")
	case errors.Is(err, errNotAForm):
		return badRequest("The upload is not a form with a file.")
	case errors.Is(err, errNoFilePart):
		return badRequest("The form holds no file.")
	default:
		return Reply{
			status: http.StatusServiceUnavailable, title: "Service Unavailable",
			message: "The server could not finish the upload. Try again.",
		}
	}
}
