// Package routes wires the module's paths to the link package.
//
// Every path is relative to the module's mount, so a printer's address is
// /api/v1/modules/<namespace>/printers/<id>/. A person types that address
// into PrusaSlicer, and PrusaSlicer adds the PrusaLink paths to it:
//
//	GET  /printers/{printer}/api/version                       the connection test
//	GET  /printers/{printer}/api/v1/storage                    the storage list
//	PUT  /printers/{printer}/api/v1/files/{storage}/{name}     the file, staged by the host
//	POST /printers/{printer}/api/files/{storage}               the file in a form, staged by the host
//	GET  /printers/{printer}/keep                              where the printer's uploads are saved
//	PUT  /printers/{printer}/keep                              the same, set from the printer's page
//	GET  /printers/{printer}/seen                              when PrusaSlicer last reached the printer
package routes

import (
	"net/http"

	"github.com/prusa3d/prusaslicer2-connector/internal/link"
)

// Paths, as module.yaml lists the staged ones.
const (
	PathVersion = "/printers/{printer}/api/version"
	PathStorage = "/printers/{printer}/api/v1/storage"
	PathPut     = "/printers/{printer}/api/v1/files/{storage}/{name}"
	PathPost    = "/printers/{printer}/api/files/{storage}"
	PathKeep    = "/printers/{printer}/keep"
	PathSeen    = "/printers/{printer}/seen"
)

// Host is what the routes read from the host for one request.
type Host struct {
	// Principal is who the host authorized.
	Principal func(r *http.Request) string
	// BodyFile is the staged body's path relative to the data folder.
	BodyFile func(r *http.Request) string
}

// New returns the handler. service may answer nil while the module starts,
// and every route answers 503 until then.
func New(host Host, service func() *link.Service) http.Handler {
	mux := http.NewServeMux()

	with := func(serve func(*link.Service, http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			held := service()
			if held == nil {
				link.Unavailable().Write(w)

				return
			}

			// Every route speaks for a person. The host refuses a request
			// that carries no credential, so an empty principal means a
			// route was made public by hand.
			principal := host.Principal(r)
			if principal == "" {
				link.Unauthorized().Write(w)

				return
			}

			serve(held, w, r, principal)
		}
	}

	mux.HandleFunc("GET "+PathVersion, with(func(s *link.Service, w http.ResponseWriter, r *http.Request, who string) {
		s.Version(w, r, who, r.PathValue("printer"))
	}))
	mux.HandleFunc("GET "+PathStorage, with(func(s *link.Service, w http.ResponseWriter, r *http.Request, who string) {
		s.Storage(w, r, who, r.PathValue("printer"))
	}))
	mux.HandleFunc("PUT "+PathPut, with(func(s *link.Service, w http.ResponseWriter, r *http.Request, who string) {
		s.Put(w, r, link.Upload{
			Principal: who, Printer: r.PathValue("printer"), Storage: r.PathValue("storage"),
			Name: r.PathValue("name"), File: host.BodyFile(r),
		})
	}))
	mux.HandleFunc("POST "+PathPost, with(func(s *link.Service, w http.ResponseWriter, r *http.Request, who string) {
		s.Post(w, r, link.Upload{
			Principal: who, Printer: r.PathValue("printer"), Storage: r.PathValue("storage"),
			File: host.BodyFile(r),
		})
	}))

	mux.HandleFunc("GET "+PathKeep, with(func(s *link.Service, w http.ResponseWriter, r *http.Request, who string) {
		s.GetKeep(w, r, who, r.PathValue("printer"))
	}))
	mux.HandleFunc("PUT "+PathKeep, with(func(s *link.Service, w http.ResponseWriter, r *http.Request, who string) {
		s.PutKeep(w, r, who, r.PathValue("printer"))
	}))

	mux.HandleFunc("GET "+PathSeen, with(func(s *link.Service, w http.ResponseWriter, r *http.Request, who string) {
		s.GetSeen(w, r, who, r.PathValue("printer"))
	}))

	return mux
}
