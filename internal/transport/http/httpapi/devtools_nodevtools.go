//go:build nodevtools

package httpapi

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// registerDevtools registers nothing in the printer build, which leaves the
// developer page, the connector list and the command log out to save memory.
// It returns the fleet unchanged, so no command is recorded.
func registerDevtools(_ huma.API, cfg Config) DeviceFleet { return cfg.Devices }

// developerTags describes no tag, because no operation carries one.
func developerTags() []*huma.Tag { return nil }

// leftOutRoutes are the routes this build registers only to answer 404, so
// the route and document check does not ask the document for them.
func leftOutRoutes() []string { return []string{http.MethodGet + " " + devPagePath} }

// mountDevPage answers 404 at the developer page, so the web app does not
// answer it with its entry page.
func (s *Server) mountDevPage() { s.mux.Get(devPagePath, notFound) }
