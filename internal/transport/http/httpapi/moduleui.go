package httpapi

import (
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// moduleUISegment is the subtree under a module's mount its browser files are
// served from. It is the one address under the group that carries files and
// not routes, and the module's own routes never claim it because an asset
// module has no routes.
const moduleUISegment = "/ui/"

// ModuleUIPath is where a module's files are served, for the listing to render
// beside the instance.
func ModuleUIPath(namespace string) string {
	return ModuleMount(namespace) + moduleUISegment
}

// UnderModuleUI reports whether a path below a module's mount names one of its
// files and not one of its routes. The path is the one the mount already took
// the group prefix off, which is what ModuleUIHandler reads.
//
// A module that ships both halves is one instance under one namespace, so the
// plane needs this boundary to send a request to the bundle or to the child. A
// module's own routes never claim the subtree: it is this server's, and a route
// declared there is refused.
func UnderModuleUI(path string) bool { return strings.HasPrefix(path, moduleUISegment) }

// moduleUIFamilyPath is the family of addresses the bundle answers, as the
// document names it.
const moduleUIFamilyPath = modulesPathPrefix + "/{" + namespaceParam + "}/ui/{file}"

// describeModuleUI puts the file family in the document as a path item that
// carries its two parameters and no operation.
//
// A described path item is how a family of addresses is published without
// being published as a call. An operation here would take a catch-all
// parameter, and a generator would turn that into a client method a caller
// cannot use: the file names are the bundle's, not this server's, so there is
// nothing for a caller to pass. A reader who needs the address in a page
// reads it here, and a client that fetches a file builds the URL from the
// namespace the listing already carries.
func describeModuleUI(api huma.API) {
	api.OpenAPI().Paths[moduleUIFamilyPath] = &huma.PathItem{
		Summary: "A module's browser files",
		Description: "The files an asset module ships for the browser, served from the bundle the host " +
			"verified against its recorded digest at start. Each file is typed from its extension, " +
			"carries " + moduleUICacheControl + " and an entity tag that moves only when the bundle " +
			"is reinstalled, and takes view_resource on the module's instance the way every other " +
			"path under the module's mount does. A name that leaves the bundle, a directory and a " +
			"file that is not there all answer 404.\n\n" +
			"There is no operation here on purpose. The names are the bundle's own, so a generated " +
			"client method taking a catch-all path would be one nothing could call. Build the " +
			"address from the namespace the listing at /api/v1/resources carries.",
		Parameters: []*huma.Param{
			{
				Name:        namespaceParam,
				In:          "path",
				Description: "The module's namespace, derived from its type.",
				Required:    true,
				Schema:      &huma.Schema{Type: huma.TypeString, MaxLength: &moduleUINamespaceMax},
				Example:     "com.github.aux.acme.planner",
			},
			{
				Name:        "file",
				In:          "path",
				Description: "The file's path inside the bundle, which may carry slashes.",
				Required:    true,
				Schema:      &huma.Schema{Type: huma.TypeString},
				Example:     "index.js",
			},
		},
	}
}

// moduleUINamespaceMax is the longest namespace a type can carry, which is a
// DNS name's length. It is a variable because huma.Schema takes the bound by
// pointer.
//
//nolint:gochecknoglobals // the bound a schema field points at, written once.
var moduleUINamespaceMax = 253

// ModuleUI is one verified bundle of files as the plane hands it over.
type ModuleUI struct {
	// Files is the bundle, rooted so a name cannot leave it. The plane opens
	// it after the digest was checked and never before.
	Files fs.FS
	// Digest is the recorded digest the bundle was verified against, in the
	// form sha256:<64 hex>. It is the entity tag of every file, because a
	// file changes only when the bundle is reinstalled under a new digest.
	Digest string
}

// moduleUICacheControl lets a browser keep a bundle's files but ask the
// server before every use. The route takes view_resource on the module's
// instance, and several people can share one browser, so the server checks the
// person asking each time. A copy that is still current costs a 304 with no
// body, because the entity tag moves only when the bundle is reinstalled. A
// shared proxy keeps nothing.
const moduleUICacheControl = "private, no-cache"

// contentTypes is the media type of each extension a browser bundle carries.
// The table is written out rather than read from the host, because
// mime.TypeByExtension consults the registry on Windows and answers a
// different string for .js on two machines. Anything outside it falls back to
// the host's table, then to an opaque type.
//
//nolint:gochecknoglobals // a closed table read per request, never written.
var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".webmanifest": "application/manifest+json",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".gif":         "image/gif",
	".webp":        "image/webp",
	".ico":         "image/x-icon",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".txt":         "text/plain; charset=utf-8",
	".wasm":        "application/wasm",
}

// contentTypeOf is the media type a file is served under, from its extension
// and never from its bytes.
func contentTypeOf(name string) string {
	ext := strings.ToLower(path.Ext(name))

	if known, ok := contentTypes[ext]; ok {
		return known
	}

	if host := mime.TypeByExtension(ext); host != "" {
		return host
	}

	return "application/octet-stream"
}

// ModuleUIHandler serves one verified bundle under a module's mount. It is
// what the plane hands the group route for an asset instance, so a request
// reaches it the way a module's own route is reached: after moduleHandler
// resolved the caller, decided the permission on the instance and refused a
// dot segment.
//
// What is left to decide here is the file. A name is held to fs.ValidPath,
// which refuses an empty element, a dot element and a leading slash, and to
// having no backslash, which a URL path never legitimately carries. A name
// that names no regular file answers 404, and nothing lists a directory.
func ModuleUIHandler(ui ModuleUI) http.Handler {
	etag := etagOf(strings.TrimPrefix(ui.Digest, "sha256:"))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, r)

			return
		}

		name, inside := strings.CutPrefix(r.URL.Path, moduleUISegment)
		if !inside || name == "" || !fs.ValidPath(name) || strings.Contains(name, `\`) {
			notFound(w, r)

			return
		}

		file, err := ui.Files.Open(name)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) {
				notFound(w, r)

				return
			}

			writeProblem(w, r, httpfault.Unexpected("open a file of the module's bundle").Wrap(err))

			return
		}

		defer func() { _ = file.Close() }()

		info, err := file.Stat()
		if err != nil || info.IsDir() {
			notFound(w, r)

			return
		}

		w.Header().Set("Content-Type", contentTypeOf(name))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", moduleUICacheControl)
		w.Header().Set("ETag", etag)

		if notModified(r, etag) {
			w.WriteHeader(http.StatusNotModified)

			return
		}

		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		w.WriteHeader(http.StatusOK)

		_, _ = io.Copy(w, file)
	})
}
