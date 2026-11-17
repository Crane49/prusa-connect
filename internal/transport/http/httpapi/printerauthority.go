package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

const printerAuthorityPath = "/api/v1/printer-authority"

func registerPrinterAuthority(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "get-printer-authority",
		Method:      http.MethodGet,
		Path:        printerAuthorityPath,
		Summary:     "The certificate of this server's own authority",
		Description: "The DER file a printer loads from its USB drive as connect.der, so it trusts the " +
			"certificate this server shows. It answers 404 when printers are not set up to trust this " +
			"server's own authority, or when the wizard has not made one. It takes admit_device on the " +
			"driver resource kind.",
		Tags: []string{"registrations"},
		Responses: map[string]*huma.Response{"200": {
			Description: "The certificate, DER encoded.",
			Content: map[string]*huma.MediaType{
				"application/pkix-cert": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
			},
		}},
		Errors: codedRegistrationErrors,
	}, func(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		if err := requireRegistrationAdmin(ctx, cfg); err != nil {
			return nil, err
		}

		missing := httpfault.ResourceNotFound(httpfault.Reference("printer-authority", "connect.der"))
		if cfg.PrinterAuthority == nil {
			return nil, missing
		}

		der, err := cfg.PrinterAuthority()
		if errors.Is(err, fs.ErrNotExist) {
			return nil, missing
		}

		if err != nil {
			return nil, httpfault.Unexpected("read the printer authority").Wrap(err)
		}

		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "application/pkix-cert")
			hctx.SetHeader("Content-Disposition", `attachment; filename="connect.der"`)
			hctx.SetHeader("Content-Length", strconv.Itoa(len(der)))
			hctx.SetStatus(http.StatusOK)

			_, _ = hctx.BodyWriter().Write(der)
		}}, nil
	})
}
