package httpapi

import (
	"net/http"
	"slices"
	"sync/atomic"
	"time"
)

// printerRoutes are the paths Prusa printer firmware dials. A printer connector
// mounts them; this list exists only so the server can say why a printer is
// answered 404 when none is mounted here.
//
//nolint:gochecknoglobals // a closed list read per request, never written.
var printerRoutes = []string{"/p/ws", "/p/register", "/p/auth"}

// unservedPrinterRouteEvery is how often the line below is written. A printer
// that is refused dials again every few seconds.
const unservedPrinterRouteEvery = time.Minute

// unservedPrinterRoutes remembers when a printer route last reached this
// server with nothing serving it. It is one clock for the whole server: the
// fact it reports is about this server, and the same for every printer.
type unservedPrinterRoutes struct {
	last atomic.Int64
}

// due reports whether a line may be written at now, and records it when it
// may.
func (u *unservedPrinterRoutes) due(now time.Time) bool {
	last := u.last.Load()
	if last != 0 && now.UnixNano()-last < int64(unservedPrinterRouteEvery) {
		return false
	}

	return u.last.CompareAndSwap(last, now.UnixNano())
}

// unroutedPrinter answers a printer route with the router's own 404 and writes
// a Warn line, at most once a minute, saying no connector serves it here. Any
// other path goes to next. Without it the printer shows "Srv error" and the
// log shows nothing.
func (s *Server) unroutedPrinter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(printerRoutes, r.URL.Path) {
			next.ServeHTTP(w, r)

			return
		}

		if s.unserved.due(time.Now()) {
			s.logger.WarnContext(r.Context(),
				"a printer route was answered 404: no printer connector serves it on this listener",
				"path", r.URL.Path,
				"method", r.Method)
		}

		notFound(w, r)
	})
}
