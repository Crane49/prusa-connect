//go:build noreference

package httpapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// The printer build answers 404 at the reference page and at its script, even
// with the web app mounted at the origin root. It still serves the API
// document, because phone apps and generated clients read it.
func TestThePrinterBuildAnswersNotFoundAtTheReference(t *testing.T) {
	t.Parallel()

	handler := appFixture(t)

	for _, path := range []string{docsPath, docsScriptPath} {
		rec := call(t, handler, http.MethodGet, path, "")

		require.Equalf(t, http.StatusNotFound, rec.Code, "GET %s", path)
		require.Containsf(t, rec.Header().Get("Content-Type"), "problem+json", "GET %s", path)
	}

	rec := call(t, handler, http.MethodGet, specJSONPath, "")
	require.Equal(t, http.StatusOK, rec.Code)
}
