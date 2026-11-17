package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The routes here call the host's fleet, storage, access and event APIs. Each
// takes a query parameter, as, that says who the call is made for:
//
//   - absent: the person the host authorized for this request. A module that
//     serves people passes the principal of the request on.
//   - self: the module itself, within what the operator granted it.
//   - anything else: that name. The host refuses a principal it did not give
//     this module. A real module never does this.
//
// Nothing here checks a permission. The host decides every call.

// firstWindowWait bounds how long /events waits for a window before it
// answers with what it has.
const firstWindowWait = 2 * time.Second

// principalFor is who a call is made for, from the request and its as
// parameter.
func principalFor(r *http.Request) string {
	switch as := r.URL.Query().Get("as"); as {
	case "":
		return routesapi.PrincipalOf(r)
	case "self":
		return ""
	default:
		return as
	}
}

// fleetRoutes are the calls of deviceapi, each as a route.
func fleetRoutes(mux *http.ServeMux, fleet *deviceapi.Client) {
	mux.HandleFunc("GET /devices", func(w http.ResponseWriter, r *http.Request) {
		page, err := fleet.List(r.Context(), deviceapi.ListParams{
			Principal: principalFor(r),
			Tags:      r.URL.Query()["tag"],
		})
		if err != nil {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, page)
	})

	mux.HandleFunc("GET /devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		device, err := fleet.Get(r.Context(), deviceapi.GetParams{
			Principal: principalFor(r),
			ID:        r.PathValue("id"),
		})
		if err != nil {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, device)
	})

	// The body is the verb's arguments as a JSON object. The verb is a query
	// parameter.
	mux.HandleFunc("POST /devices/{id}/command", func(w http.ResponseWriter, r *http.Request) {
		args, err := argumentsOf(r.Body)
		if err != nil {
			answer(w, http.StatusBadRequest, map[string]any{"error": err.Error()})

			return
		}

		result, err := fleet.Command(r.Context(), deviceapi.CommandParams{
			Principal: principalFor(r),
			ID:        r.PathValue("id"),
			Verb:      r.URL.Query().Get("verb"),
			Args:      args,
		})
		if err != nil {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, result)
	})

	mux.HandleFunc("GET /devices/{id}/jobs", func(w http.ResponseWriter, r *http.Request) {
		jobs, err := fleet.Jobs(r.Context(), deviceapi.JobsParams{
			Principal: principalFor(r),
			ID:        r.PathValue("id"),
		})
		if err != nil {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, jobs)
	})

	// An empty path is the root, which lists the storages.
	mux.HandleFunc("GET /devices/{id}/files", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		limit, _ := strconv.Atoi(query.Get("limit"))
		offset, _ := strconv.Atoi(query.Get("offset"))

		listing, err := fleet.Files(r.Context(), deviceapi.FilesParams{
			Principal: principalFor(r),
			ID:        r.PathValue("id"),
			Path:      query.Get("path"),
			Limit:     limit,
			Offset:    offset,
		})
		if err != nil {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, listing)
	})

	// A page of the prints that have ended. An empty device means every
	// device the caller may observe.
	mux.HandleFunc("GET /history", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		limit, _ := strconv.Atoi(query.Get("limit"))

		params := deviceapi.JobHistoryParams{
			Principal: principalFor(r),
			ID:        query.Get("device"),
			Limit:     limit,
			Cursor:    query.Get("cursor"),
		}

		var err error
		if params.From, params.To, err = rangeOf(query.Get("from"), query.Get("to")); err != nil {
			answer(w, http.StatusBadRequest, map[string]any{"error": err.Error()})

			return
		}

		page, err := fleet.JobHistory(r.Context(), params)
		if err != nil {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, page)
	})

	// Print a file from one of the server's storages. The host sends the file
	// and the start command. The answer names the transfer.
	mux.HandleFunc("POST /devices/{id}/print", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		started, err := fleet.Print(r.Context(), deviceapi.PrintParams{
			Principal: principalFor(r),
			ID:        r.PathValue("id"),
			Storage:   query.Get("storage"),
			Source:    query.Get("source"),
			Path:      query.Get("path"),
		})
		if err != nil {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, started)
	})

	mux.HandleFunc("POST /registrations/{code}/approve", func(w http.ResponseWriter, r *http.Request) {
		registration, err := fleet.Approve(r.Context(), deviceapi.ApproveParams{
			Principal: principalFor(r),
			Code:      r.PathValue("code"),
		})
		if err != nil {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, registration)
	})
}

// eventRoutes is one route that subscribes to the host's events and answers
// with the windows that arrived within the wait.
func eventRoutes(mux *http.ServeMux, conn *wirerpc.Conn) {
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		wanted, err := strconv.Atoi(r.URL.Query().Get("windows"))
		if err != nil || wanted <= 0 {
			wanted = 1
		}

		ctx, cancel := context.WithTimeout(r.Context(), firstWindowWait)
		defer cancel()

		var windows []busapi.Window

		err = busapi.Subscribe(ctx, conn, busapi.SubscribeParams{
			Principal: principalFor(r),
			Types:     r.URL.Query()["type"],
		}, func(window busapi.Window) error {
			windows = append(windows, window)

			if len(windows) >= wanted {
				cancel()
			}

			return nil
		})

		// The deadline and the cancel above both end the subscription
		// normally.
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, map[string]any{"windows": windows})
	})
}

// argumentsOf reads a command's arguments, and an empty body as none.
func argumentsOf(body io.Reader) (map[string]string, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	args := map[string]string{}
	if len(raw) == 0 {
		return args, nil
	}

	if err = json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}

	return args, nil
}

// rangeOf reads the two instants that bound a history read. An empty value is
// no bound on that end.
func rangeOf(from, to string) (time.Time, time.Time, error) {
	var out [2]time.Time

	for index, written := range []string{from, to} {
		if written == "" {
			continue
		}

		held, err := time.Parse(time.RFC3339, written)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("read %q as RFC 3339: %w", written, err)
		}

		out[index] = held
	}

	return out[0], out[1], nil
}

// fleetStatusFor maps a refusal of the host to an HTTP status. The mapping is
// the module's own choice.
func fleetStatusFor(err error) int {
	switch {
	case errors.Is(err, deviceapi.ErrDenied), errors.Is(err, busapi.ErrDenied),
		errors.Is(err, storageapi.ErrDenied), errors.Is(err, accessapi.ErrDenied),
		errors.Is(err, accessapi.ErrUnknownSubject):
		return http.StatusForbidden
	case errors.Is(err, deviceapi.ErrNoDevice), errors.Is(err, deviceapi.ErrNoRegistration),
		errors.Is(err, deviceapi.ErrNoFile), errors.Is(err, storageapi.ErrNoFile):
		return http.StatusNotFound
	case errors.Is(err, deviceapi.ErrInvalidQuery), errors.Is(err, deviceapi.ErrVerbUnknown),
		errors.Is(err, deviceapi.ErrArgument), errors.Is(err, busapi.ErrInvalidSubscription),
		errors.Is(err, storageapi.ErrInvalidRange), errors.Is(err, storageapi.ErrInvalidPath),
		errors.Is(err, accessapi.ErrInvalidQuestion), errors.Is(err, storageapi.ErrShortWrite):
		return http.StatusBadRequest
	// ErrUnreadable can mean a storage that cannot be read at this moment, so
	// it is a 503 and the client asks again.
	case errors.Is(err, deviceapi.ErrNotReady), errors.Is(err, deviceapi.ErrBusy),
		errors.Is(err, storageapi.ErrUnreadable), errors.Is(err, busapi.ErrNotReady):
		return http.StatusServiceUnavailable
	}

	return http.StatusBadGateway
}

// storageRoutes are two reads of a file in one of the server's storages, and
// one write. A module that analyses a file stats it and reads a range. The
// host bounds the range, so a module never has a whole file in memory.
func storageRoutes(mux *http.ServeMux, files *storageapi.Client) {
	mux.HandleFunc("GET /files/{storage}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		info, err := files.Stat(r.Context(), storageapi.StatParams{
			Principal: principalFor(r),
			Storage:   r.PathValue("storage"),
			Path:      r.PathValue("path"),
		})
		if err != nil {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, info)
	})

	mux.HandleFunc("GET /bytes/{storage}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		offset, _ := strconv.ParseInt(query.Get("offset"), 10, 64)
		length, _ := strconv.Atoi(query.Get("length"))

		chunk, err := files.Read(r.Context(), storageapi.ReadParams{
			Principal: principalFor(r),
			Storage:   r.PathValue("storage"),
			Path:      r.PathValue("path"),
			Offset:    offset,
			Length:    length,
		})
		if err != nil {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, map[string]any{
			"text": string(chunk.Data), "size": chunk.Size, "eof": chunk.EOF,
		})
	})

	// The write needs the size up front. ContentLength gives it, and the
	// body is streamed.
	mux.HandleFunc("PUT /files/{storage}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		written, err := files.Write(r.Context(), storageapi.WriteParams{
			Principal: principalFor(r),
			Storage:   r.PathValue("storage"),
			Path:      r.PathValue("path"),
			Size:      r.ContentLength,
		}, r.Body)
		if err != nil {
			answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		answer(w, http.StatusOK, written)
	})
}

// accessRoutes asks the host whether a subject may do an action on an object.
// A queue asks it before it starts a job somebody submitted hours earlier.
func accessRoutes(mux *http.ServeMux, access *accessapi.Client) {
	mux.HandleFunc("GET /may/{subject}/{action}/{type}/{object}",
		func(w http.ResponseWriter, r *http.Request) {
			decision, err := access.Check(r.Context(), accessapi.CheckParams{
				Subject:    r.PathValue("subject"),
				Action:     r.PathValue("action"),
				ObjectType: r.PathValue("type"),
				ObjectID:   r.PathValue("object"),
			})
			if err != nil {
				answer(w, fleetStatusFor(err), map[string]any{"error": err.Error()})

				return
			}

			answer(w, http.StatusOK, decision)
		})
}

// callRoutes is one module calling another through the host. The manifest
// must name the other module's namespace under requires or suggests.
func callRoutes(mux *http.ServeMux, modules *routesapi.Client) {
	mux.HandleFunc("GET /call", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		answered, err := modules.Call(r.Context(), routesapi.CallParams{
			Module: query.Get("module"),
			Method: http.MethodGet,
			Path:   query.Get("path"),
		})
		if err != nil {
			answer(w, callStatusFor(err), map[string]any{"error": err.Error()})

			return
		}

		body := json.RawMessage(answered.Body)
		if !json.Valid(body) {
			body, _ = json.Marshal(string(answered.Body))
		}

		answer(w, http.StatusOK, map[string]any{"status": answered.Status, "body": body})
	})
}

func callStatusFor(err error) int {
	switch {
	case errors.Is(err, routesapi.ErrCallNotDeclared), errors.Is(err, routesapi.ErrCallDenied):
		return http.StatusForbidden
	case errors.Is(err, routesapi.ErrCallUnavailable):
		return http.StatusServiceUnavailable
	}

	return http.StatusBadRequest
}
