//go:build !noreference

package httpapi

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// scalarVersion is the @scalar/api-reference release the embedded bundle came
// from, and scalarSHA256 is the SHA-256 of the gzip stored beside this file.
// TestTheEmbeddedBundleIsTheReviewedOne pins both, so replacing the renderer
// also takes a reviewed change to these two values.
const (
	scalarVersion = "1.66.1"
	scalarSHA256  = "06d3f3d7a58f87e5bfdd8e83c99ec63d2bf599317ec5071d36862569379ba32a"
)

// referenceCSP is what the reference page is allowed to reach. Nothing leaves
// the origin: no fonts.scalar.com, no proxy.scalar.com, no CDN.
//
// worker-src is here because the bundle builds a module worker from
// URL.createObjectURL(new Blob([...])). worker-src falls back to script-src,
// which does not carry blob:, so without this line both branches of that code
// are blocked and the feature that uses it fails silently.
//
// 'unsafe-inline' is there for Scalar, which injects style elements at
// runtime. 'unsafe-eval' is absent. The bundle calls Function( only to test
// whether eval is allowed, and its validator falls back to a slower path when
// it is refused. The page renders OpenAPI content that operators and modules
// supply, and refusing eval is its strongest defence.
var referenceCSP = strings.Join([]string{
	"default-src 'none'",
	"base-uri 'none'",
	"connect-src 'self'",
	"img-src 'self' data: blob:",
	"font-src 'self'",
	"script-src 'self'",
	"worker-src 'self' blob:",
	"style-src 'self' 'unsafe-inline'",
	"frame-ancestors 'none'",
	"form-action 'none'",
}, "; ")

// The renderer is stored gzipped, about 1.1 MB against 3.8 MB raw. A client
// that accepts gzip receives the stored bytes as they are, and every browser
// that can run this renderer accepts gzip.
//
//go:embed assets/scalar-standalone.js.gz
var scalarBundleGzip []byte

//go:embed assets/reference.html
var referenceHTML []byte

// scalarETag identifies the gzip representation by content, because the path
// never changes and cannot tell a client that the bytes did.
var scalarETag = sync.OnceValue(func() string {
	return etagOf(hashOf(scalarBundleGzip))
})

// scalarIdentityETag identifies the decompressed representation. RFC 9110
// section 8.8.1 makes an entity-tag the identifier of one representation, and
// this path can send two different byte strings. With one ETag over both, a
// cache could give the gzip bytes to a client that cannot decompress them.
var scalarIdentityETag = sync.OnceValue(func() string {
	body, err := scalarBundle()
	if err != nil {
		return ""
	}

	return etagOf(hashOf(body))
})

func hashOf(body []byte) string {
	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}

// referenceModuleParam names the module whose document the page renders in
// place of the core's: /docs?module=<namespace>.
const referenceModuleParam = "module"

// referenceEmbeddedParam is set by a page that frames the reference. A browser
// sends Sec-Fetch-Dest only to a secure origin, so a server on plain HTTP at a
// LAN address learns of the frame from this parameter alone.
const referenceEmbeddedParam = "embedded"

// coreDocumentTitle is the name the switcher gives the core's own document.
const coreDocumentTitle = "Core API"

// referenceTitle is the page title, and the start of it for a module's page.
const referenceTitle = "Prusa Connect API"

// moduleReferenceCSP is referenceCSP for a module's document, with one change:
// a page of this server may frame it. A module's own page shows its routes in
// the renderer the core uses, and it can only do that in a frame, because a
// module's bundle never loads a second copy of the renderer.
var moduleReferenceCSP = strings.Replace(referenceCSP, "frame-ancestors 'none'", "frame-ancestors 'self'", 1)

// The page is assets/reference.html with five markers replaced. It is not
// html/template: that package adds about 400 KB to every build, and the page
// writes five values. Every value goes through html.EscapeString, so a module
// title from a manifest reaches the page as text.
const (
	// referenceRenderer starts the renderer on one document.
	referenceRenderer = `<script
      id="api-reference"
      data-url="%s"
      data-configuration='{"withDefaultFonts":false,"proxyUrl":"","telemetry":false,"theme":"default","hideDownloadButton":false}'
    ></script>
    <script src="/docs/scalar.js"></script>`

	// referenceMissing stands in place of the renderer for a module document
	// the reader may not read, or which is not there.
	referenceMissing = `<main class="pcl-missing" role="alert">
      <h1>This API document cannot be shown</h1>
      <p>There is no API document for <code>%s</code> that you may read. ` +
		`The module is not installed, is switched off, has no API document, or your account may not see it.</p>
    </main>`

	// referenceHeaderHeight tells the renderer how tall the switcher is, so its
	// sticky sidebar starts below it.
	referenceHeaderHeight = `<style>
      :root {
        --scalar-custom-header-height: 48px;
      }
    </style>`
)

// referencePage is what the page shows.
type referencePage struct {
	// Title is the page title.
	Title string
	// Sources is the switcher: the core's document and every module document
	// the reader may read. It is nil when the page leaves the switcher out.
	Sources []referenceSource
	// Document is the address the renderer fetches, and empty when the page
	// shows Missing instead.
	Document string
	// Missing is the namespace whose document the reader asked for and may not
	// read, or which is not there.
	Missing string
}

// render writes the page.
func (p referencePage) render() []byte {
	content := fmt.Sprintf(referenceRenderer, html.EscapeString(p.Document))
	bodyClass := ""

	if p.Document == "" {
		content = fmt.Sprintf(referenceMissing, html.EscapeString(p.Missing))
		bodyClass = ` class="pcl-plain"`
	}

	header, switcher := "", ""

	if len(p.Sources) > 0 {
		header = referenceHeaderHeight

		var nav strings.Builder

		nav.WriteString(`<nav class="pcl-documents" aria-label="API documents">
      <span class="pcl-documents-label">API reference</span>
      <ul>`)

		for _, source := range p.Sources {
			current := ""
			if source.Current {
				current = ` aria-current="page"`
			}

			fmt.Fprintf(&nav, "\n        <li><a href=\"%s\"%s data-document=\"%s\">%s</a></li>",
				html.EscapeString(source.Href), current,
				html.EscapeString(source.Namespace), html.EscapeString(source.Title))
		}

		nav.WriteString("\n      </ul>\n    </nav>")
		switcher = nav.String()
	}

	return []byte(strings.NewReplacer(
		"{{title}}", html.EscapeString(p.Title),
		"{{header}}", header,
		"{{body-class}}", bodyClass,
		"{{switcher}}", switcher,
		"{{content}}", content,
	).Replace(string(referenceHTML)))
}

// referenceSource is one entry of the switcher.
type referenceSource struct {
	Title     string
	Namespace string
	Href      string
	Current   bool
}

// referenceHandler answers the reference page. Without a query it renders the
// core's document. With ?module=<namespace> it renders that module's document
// as /api/v1/modules/<namespace>/openapi.json serves it. Above the document it
// draws a switcher with the core and each module document the reader may
// read. The entries are plain links, so the browser's history works as on any
// other page.
func (s *Server) referenceHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		namespace := r.URL.Query().Get(referenceModuleParam)
		if namespace != "" && extension.Namespace(namespace).Validate() != nil {
			notFound(w, r)

			return
		}

		modules := s.readableModules(r, cfg)

		page := referencePage{Title: referenceTitle, Document: specJSONPath}
		csp := referenceCSP
		status := http.StatusOK

		if namespace != "" {
			csp = moduleReferenceCSP
			page.Document = ""
			page.Missing = namespace
			status = http.StatusNotFound

			for _, module := range modules {
				if module.Namespace == namespace {
					page.Title = module.Title + " - " + referenceTitle
					page.Document = ModuleOpenAPIPath(namespace)
					page.Missing = ""
					status = http.StatusOK
				}
			}
		}

		// A module's page that frames this one shows its own document. The
		// switcher is left out there, because a link to the core's page inside
		// the frame would open a page that refuses to be framed. A browser
		// sends Sec-Fetch-Dest: iframe for a framed page, on a secure origin.
		framed := r.Header.Get("Sec-Fetch-Dest") == "iframe" ||
			r.URL.Query().Has(referenceEmbeddedParam)
		if !framed && (len(modules) > 0 || namespace != "") {
			page.Sources = switcher(modules, namespace)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// The list depends on who reads it, so no cache keeps one reader's copy.
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_, _ = w.Write(page.render())
	}
}

// switcher is the core's entry followed by the modules, with the one on show
// marked.
func switcher(modules []referenceSource, current string) []referenceSource {
	sources := make([]referenceSource, 0, len(modules)+1)
	sources = append(sources, referenceSource{
		Title: coreDocumentTitle, Href: docsPath, Current: current == "",
	})

	for _, module := range modules {
		module.Current = module.Namespace == current
		sources = append(sources, module)
	}

	return sources
}

// readableModules lists the module documents the reader of r may read, sorted
// by title. The page is not an operation, so it resolves the caller itself,
// the way the principal middleware does. A reader with no identity gets no
// modules: the core's document is public and the page still shows it.
//
// The listing says which instances are modules and what they are called.
// moduleDocument decides whether to name one. The document route makes the
// same check, so the switcher never names a module whose document the
// renderer would be refused.
func (s *Server) readableModules(r *http.Request, cfg Config) []referenceSource {
	if cfg.Modules == nil || cfg.ModuleAccess == nil || cfg.Resources == nil {
		return nil
	}

	who, err := cfg.Principals.Caller(r)
	if err != nil {
		return nil
	}

	ctx := withCaller(r.Context(), who)

	listing, err := cfg.Resources.List(ctx, who.Principal, "")
	if err != nil {
		s.logger.WarnContext(ctx, "the reference page could not list the modules", slog.Any("error", err))

		return nil
	}

	var modules []referenceSource

	for _, instance := range listing.Items {
		if instance.Namespace == "" || !instance.Enabled {
			continue
		}

		if _, refused := moduleDocument(ctx, cfg, instance.Namespace); refused != nil {
			continue
		}

		title := instance.Name
		if instance.Module != nil && instance.Module.Title != "" {
			title = instance.Module.Title
		}

		modules = append(modules, referenceSource{
			Title:     title,
			Namespace: instance.Namespace,
			Href:      docsPath + "?" + url.Values{referenceModuleParam: {instance.Namespace}}.Encode(),
		})
	}

	slices.SortFunc(modules, func(a, b referenceSource) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)),
			cmp.Compare(a.Namespace, b.Namespace),
		)
	})

	return modules
}

func serveReferenceScript(w http.ResponseWriter, r *http.Request) {
	gzipped := acceptsGzip(r)

	etag := scalarIdentityETag()
	if gzipped {
		etag = scalarETag()
	}

	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	// On the 304 as well as the 200: a cache that stored the response without
	// it would serve the gzip bytes to the next client that cannot read them.
	w.Header().Add("Vary", "Accept-Encoding")

	if etag != "" {
		w.Header().Set("ETag", etag)
	}

	if notModified(r, etag) {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	if gzipped {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(scalarBundleGzip)

		return
	}

	body, err := scalarBundle()
	if err != nil {
		writeProblem(w, r, httpfault.Unexpected("decompress the reference renderer").Wrap(err))

		return
	}

	_, _ = w.Write(body)
}

// scalarBundle decompresses the embedded renderer once, for a client that does
// not accept gzip. The path takes no credential, and without the kept copy
// every such request would decompress and allocate about 3.6 MiB. The copy is
// made only if a client without gzip ever asks.
var scalarBundle = sync.OnceValues(func() ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(scalarBundleGzip))
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller frames it as a fault; a second layer of context adds nothing.
	}

	defer func() { _ = reader.Close() }()

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller frames it as a fault; a second layer of context adds nothing.
	}

	return body, nil
})

// ReferencePage returns the path of the API reference page, and false in a
// build that leaves the page out.
func ReferencePage() (string, bool) { return docsPath, true }
