package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// warnings is a log a test reads back. The router serves concurrently, so
// writes take a lock.
type warnings struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *warnings) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.Write(p)
}

func (w *warnings) lines(substr string) int {
	w.mu.Lock()
	defer w.mu.Unlock()

	count := 0

	for line := range strings.SplitSeq(w.buf.String(), "\n") {
		if strings.Contains(line, substr) {
			count++
		}
	}

	return count
}

// A printer that dials a server with no printer connector is answered 404,
// whether or not the web app catches every other path, and the operator reads
// one line saying so instead of a printer showing "Srv error" for no visible
// reason.
func TestAPrinterRouteNobodyServesIsRefusedAndLoggedOnce(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mounts []Mount
	}{
		{name: "no mount at all"},
		{name: "the web app at the root", mounts: []Mount{
			{Pattern: "/*", Handler: mountedText("frontend"), Reason: "the frontend at the root"},
		}},
	}

	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()

			out := &warnings{}
			cfg := mountConfig(each.mounts)
			cfg.Logger = slog.New(slog.NewJSONHandler(out, nil))

			server, err := New(cfg)
			require.NoError(t, err)

			for _, path := range []string{"/p/ws", "/p/ws", "/p/register", "/p/auth"} {
				rec := call(t, server.Handler(), http.MethodGet, path, "")

				require.Equalf(t, http.StatusNotFound, rec.Code, "%s was answered as if it were served", path)
				require.Equal(t, problemMediaType, rec.Header().Get("Content-Type"))
			}

			require.Equal(t, 1, out.lines(`"path":"/p/ws"`),
				"four printer requests in one minute wrote other than one line")
			require.Equal(t, 1, out.lines("no printer connector serves it"))

			if len(each.mounts) > 0 {
				rec := call(t, server.Handler(), http.MethodGet, "/printers", "")
				require.Equal(t, "frontend", rec.Body.String(), "the web app lost a path of its own")
			}
		})
	}
}

// The clock lets one line through a minute, whichever printer route asked.
func TestTheUnservedPrinterRouteLineWaitsAMinute(t *testing.T) {
	t.Parallel()

	var unserved unservedPrinterRoutes

	start := time.Unix(1_000, 0)

	require.True(t, unserved.due(start))
	require.False(t, unserved.due(start.Add(unservedPrinterRouteEvery-time.Second)))
	require.True(t, unserved.due(start.Add(unservedPrinterRouteEvery)))
}
