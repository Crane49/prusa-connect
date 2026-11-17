package e2e

import (
	"net/http"
	"strings"
	"testing"
)

// allowedOrigins continues the fixture's api block with the key a deployment
// that serves a page from another origin writes.
const allowedOrigins = "    allowed_origins:\n      - https://printers.example"

// TestAPageOnAnAllowedOriginMayCallTheAPI is the browser's side of
// core.api.allowed_origins over a real socket: the preflight a browser sends
// before a write, and the answer it reads before it lets the page see one.
//
// An origin off the list reads what every deployment answered before the key
// existed, which is no Access-Control header at all, and the browser then
// refuses the page the response.
func TestAPageOnAnAllowedOriginMayCallTheAPI(t *testing.T) {
	t.Parallel()

	server := startServerCore(t, allowedOrigins, "")
	server.awaitReady(t)

	preflight := server.do(t, http.MethodOptions, "/api/v1/resources", "", map[string]string{
		"Origin":                         "https://printers.example",
		"Access-Control-Request-Method":  http.MethodPut,
		"Access-Control-Request-Headers": "content-type, if-none-match",
	})
	if preflight.status != http.StatusNoContent {
		t.Fatalf("the preflight answered %d: %s", preflight.status, preflight.body)
	}

	if got := preflight.header.Get("Access-Control-Allow-Origin"); got != "https://printers.example" {
		t.Fatalf("the preflight allowed %q, expected the page's own origin", got)
	}

	if !strings.Contains(preflight.header.Get("Access-Control-Allow-Methods"), http.MethodPut) {
		t.Fatalf("the preflight does not allow PUT: %q", preflight.header.Get("Access-Control-Allow-Methods"))
	}

	answered := server.do(t, http.MethodGet, "/api/v1/resources", "", map[string]string{
		"Origin": "https://printers.example",
	})
	if answered.status != http.StatusOK {
		t.Fatalf("/api/v1/resources answered %d: %s", answered.status, answered.body)
	}

	if got := answered.header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("the page may not send its credential: Access-Control-Allow-Credentials is %q", got)
	}

	refused := server.do(t, http.MethodGet, "/api/v1/resources", "", map[string]string{
		"Origin": "https://elsewhere.example",
	})
	if refused.status != http.StatusOK {
		t.Fatalf("/api/v1/resources answered %d to the other origin: %s", refused.status, refused.body)
	}

	for name := range refused.header {
		if strings.HasPrefix(name, "Access-Control-") {
			t.Fatalf("an origin off the list was answered %s", name)
		}
	}

	t.Logf("observed: %s answered the printers.example page and told elsewhere.example nothing", server.baseURL)
}
