//go:build diagnostics

package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// diagnosticsCarried says this build carries the support bundle.
const diagnosticsCarried = true

const diagnosticsPath = "/api/v1/diagnostics"

// unsafeFilename is what a version string may hold that a filename in a
// Content-Disposition header should not.
var unsafeFilename = regexp.MustCompile(`[^A-Za-z0-9._-]`)

type diagnosticsInput struct {
	Modules    bool `query:"modules"    doc:"Include what modules wrote to standard error, scrubbed. Off by default: a module can print anything." example:"true"`
	Extensions bool `query:"extensions" doc:"Include the files installed extensions add, such as the log lines a printer sent, scrubbed. Off by default: the files come from code this server does not review." example:"true"`
	RawBody    []byte
}

// DiagnosticsInfo is what a bundle can hold beyond the core's own files.
type DiagnosticsInfo struct {
	Extensions []string `json:"extensions" doc:"The title of each running extension that adds files to a bundle when extensions=true."`
}

type diagnosticsInfoOutput struct {
	Body DiagnosticsInfo
}

func registerDiagnostics(api huma.API, cfg Config) {
	building := make(chan struct{}, 1)

	register(api, huma.Operation{
		OperationID: "get-diagnostics",
		Method:      http.MethodGet,
		Path:        diagnosticsPath,
		Summary:     "What a diagnostics bundle can include",
		Description: "Names the running extensions that add files to a diagnostics bundle when it is built " +
			"with extensions=true, so a page can say what the choice includes. It takes the admin role on " +
			"the organization, as building the bundle does.",
		Tags:   []string{"system"},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, _ *struct{}) (*diagnosticsInfoOutput, error) {
		if cfg.Diagnostics == nil || !cfg.Diagnostics.Served() {
			return nil, httpfault.ResourceNotFound(httpfault.Reference("diagnostics", "bundle"))
		}

		if err := requireOrgAdmin(ctx, cfg, "diagnostics"); err != nil {
			return nil, err
		}

		extensions := cfg.Diagnostics.Extensions()
		if extensions == nil {
			extensions = []string{}
		}

		return &diagnosticsInfoOutput{Body: DiagnosticsInfo{Extensions: extensions}}, nil
	})

	register(api, huma.Operation{
		OperationID: "create-diagnostics-bundle",
		Method:      http.MethodPost,
		Path:        diagnosticsPath,
		Summary:     "A redacted diagnostics bundle",
		Description: "Builds a zip of recent logs, the last crash and the shape of this install, with secrets " +
			"and personal data removed, for an operator to attach to a support ticket. The body is the " +
			"browser's own error buffer, an empty array when it holds nothing; an unreadable one is " +
			"left out of the bundle rather than refused. Nothing is stored. One bundle is built at a time, and " +
			"a request while one is in progress answers 429.\n\n" +
			"It takes the admin role on the organization, because the bundle describes the whole server.",
		Tags: []string{"system"},
		RequestBody: &huma.RequestBody{
			Description: "The browser's recent errors, as its log buffer holds them.",
			Required:    true,
			Content: map[string]*huma.MediaType{
				"application/json": {
					Schema: &huma.Schema{Type: huma.TypeArray, Items: &huma.Schema{Type: huma.TypeObject}},
				},
			},
		},
		Responses: map[string]*huma.Response{"200": {
			Description: "The bundle.",
			Content: map[string]*huma.MediaType{
				"application/zip": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
			},
		}},
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests,
		},
	}, func(ctx context.Context, in *diagnosticsInput) (*huma.StreamResponse, error) {
		if cfg.Diagnostics == nil || !cfg.Diagnostics.Served() {
			return nil, httpfault.ResourceNotFound(httpfault.Reference("diagnostics", "bundle"))
		}

		if err := requireOrgAdmin(ctx, cfg, "diagnostics"); err != nil {
			return nil, err
		}

		select {
		case building <- struct{}{}:
			defer func() { <-building }()
		default:
			return nil, httpfault.DiagnosticsBusy()
		}

		bundle, err := cfg.Diagnostics.Bundle(ctx, principalFrom(ctx),
			DiagnosticsChoice{Modules: in.Modules, Extensions: in.Extensions}, in.RawBody)
		if err != nil {
			return nil, httpfault.Unexpected("build the diagnostics bundle").Wrap(err)
		}

		name := "pcl-diagnostics-" + unsafeFilename.ReplaceAllString(cfg.Version, "_") + "-" +
			time.Now().UTC().Format("20060102T150405Z") + ".zip"

		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			// The server's write deadline was set when the request arrived, and
			// asking the modules for their files can take longer than that. The
			// zip gets a deadline of its own, counted from now.
			_, raw := humachi.Unwrap(hctx)
			if err := http.NewResponseController(raw).SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil &&
				!errors.Is(err, http.ErrNotSupported) {
				cfg.Logger.Warn("the diagnostics bundle keeps the request's write deadline", "error", err.Error())
			}

			hctx.SetHeader("Content-Type", "application/zip")
			hctx.SetHeader("Content-Disposition", `attachment; filename="`+name+`"`)
			hctx.SetHeader("Cache-Control", "no-store")
			hctx.SetHeader("Content-Length", strconv.Itoa(len(bundle)))
			hctx.SetStatus(http.StatusOK)

			_, _ = hctx.BodyWriter().Write(bundle)
		}}, nil
	})
}
