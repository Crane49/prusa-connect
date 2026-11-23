package routes_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prusa3d/prusaslicer3-connector/internal/connect"
	"github.com/prusa3d/prusaslicer3-connector/internal/routes"
)

// A connect route answers 503 while the host has not configured the module
// yet. The me reply needs no configuration.
func TestARouteBeforeConfigureIsUnavailable(t *testing.T) {
	t.Parallel()

	for name, path := range map[string]string{
		"the printer list": routes.PathPrinters,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			handler := routes.New(routes.Host{
				Principal: func(*http.Request) string { return "user:7" },
				BodyFile:  func(*http.Request) string { return "" },
			}, routes.Services{
				Connect: func() *connect.Service { return nil },
			})

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status %d, want %d: %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body)
			}
		})
	}
}

// The avatar route is public: the host sends it no principal, and it answers
// before the module is configured.
func TestTheAvatarAnswersWithNoPrincipal(t *testing.T) {
	t.Parallel()

	handler := routes.New(routes.Host{
		Principal: func(*http.Request) string { return "" },
		BodyFile:  func(*http.Request) string { return "" },
	}, routes.Services{
		Connect: func() *connect.Service { return nil },
	})

	for query, want := range map[string]int{
		"i=D&c=fa6831": http.StatusOK,
		"i=D&c=ffffff": http.StatusBadRequest,
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			routes.PathAvatar+"?"+query, nil))

		if recorder.Code != want {
			t.Errorf("%s: answered %d, want %d", query, recorder.Code, want)
		}
	}
}
