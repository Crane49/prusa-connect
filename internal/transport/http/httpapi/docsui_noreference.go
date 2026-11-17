//go:build noreference

package httpapi

import "net/http"

// The printer build leaves the API reference page out. The page embeds a
// renderer of about 1 MiB, and a printer has little memory. Both paths answer
// 404 here, so the web app does not answer them in the page's place. The
// document at /openapi.json is still served.

func (*Server) referenceHandler(Config) http.HandlerFunc { return notFound }

func serveReferenceScript(w http.ResponseWriter, r *http.Request) { notFound(w, r) }

// ReferencePage returns false, because this build leaves the page out.
func ReferencePage() (string, bool) { return "", false }
