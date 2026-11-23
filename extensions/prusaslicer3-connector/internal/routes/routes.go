// Package routes wires the module's paths to the account and connect
// packages.
//
// Every path is relative to the module's mount, so the first one is reached
// at /api/v1/modules/<namespace>/account/api/v1/me/.
//
//	GET  /account/api/v1/me/                        the person behind the credential
//	GET  /account/api/v1/me/avatar.png              the person's initials, public
//	GET  /connect/slicer/v2/printers                the printer list
//	POST /connect/app/users/teams/{team}/uploads    register an upload
//	PUT  /connect/app/teams/{team}/files/raw        the file, staged by the host
package routes

import (
	"net/http"

	"github.com/prusa3d/prusaslicer3-connector/internal/account"
	"github.com/prusa3d/prusaslicer3-connector/internal/cloud"
	"github.com/prusa3d/prusaslicer3-connector/internal/connect"
)

// Paths, as module.yaml lists the staged one.
const (
	PathMe       = "/account/api/v1/me/"
	PathAvatar   = account.PathAvatar
	PathPrinters = "/connect/slicer/v2/printers"
	PathUploads  = "/connect/app/users/teams/{team}/uploads"
	PathRaw      = "/connect/app/teams/{team}/files/raw"
)

// Host is what the routes read from the host for one request.
type Host struct {
	// Principal is who the host authorized.
	Principal func(r *http.Request) string
	// BodyFile is the staged body's path relative to the data folder.
	BodyFile func(r *http.Request) string
	// People is the host's directory.
	People account.People
}

// Services are what the routes answer with. Connect may answer nil while the
// module starts, and its routes answer 503 until then.
type Services struct {
	Connect func() *connect.Service
}

// New returns the handler.
func New(host Host, services Services) http.Handler {
	mux := http.NewServeMux()

	withConnect := func(serve func(*connect.Service, http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			held := services.Connect()
			if held == nil {
				cloud.Unavailable().Write(w)

				return
			}

			// Every connect route speaks for a person. The host refuses a
			// request that carries no credential, so an empty principal
			// means a route was listed public by mistake.
			if host.Principal(r) == "" {
				cloud.Unauthorized().Write(w)

				return
			}

			serve(held, w, r)
		}
	}

	mux.HandleFunc("GET "+PathMe, func(w http.ResponseWriter, r *http.Request) {
		account.Me(w, r, host.Principal(r), host.People)
	})
	// Public: PrusaSlicer downloads the avatar with no credential, and the
	// picture is only what the query says.
	mux.HandleFunc("GET "+PathAvatar, account.Avatar)
	mux.HandleFunc("GET "+PathPrinters, withConnect(func(s *connect.Service, w http.ResponseWriter, r *http.Request) {
		s.PrinterList(w, r, host.Principal(r))
	}))
	mux.HandleFunc("POST "+PathUploads, withConnect(func(s *connect.Service, w http.ResponseWriter, r *http.Request) {
		s.Register(w, r, host.Principal(r), r.PathValue("team"))
	}))
	mux.HandleFunc("PUT "+PathRaw, withConnect(func(s *connect.Service, w http.ResponseWriter, r *http.Request) {
		s.Receive(w, r, host.Principal(r), r.PathValue("team"), host.BodyFile(r))
	}))

	return mux
}
