package httpapi

// The drawing of a device model, for a model a driver declared with one. The
// core's own models carry none: the web app draws those.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const vocabularyDeviceModelArtworkPath = vocabularyDeviceModelsPath + "/{model}/artwork"

// artworkCacheControl lets any cache keep the drawing and has it ask before
// every use. The drawing is the same for every caller, and it changes only
// with the binary, which the entity tag catches.
const artworkCacheControl = "public, no-cache"

// artworkInput bounds the model to connectorapi.MaxModelBytes, the longest
// spelling a driver may declare, so every declared drawing has a path that
// answers. TestTheArtworkPathTakesEveryDeclarableModel holds the two together.
type artworkInput struct {
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Model string `path:"model" maxLength:"128" doc:"The machine's own spelling, as the model member of a device-model row carries it." example:"Bench_One"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	IfNoneMatch string `header:"If-None-Match" doc:"The entity tag of the drawing this client already has. A drawing with the same tag answers 304 with no body."`
}

// artworkPathOf is where the drawing of one model is served.
func artworkPathOf(model string) string {
	return vocabularyDeviceModelsPath + "/" + url.PathEscape(model) + "/artwork"
}

// registerDeviceModelArtwork serves the drawing a driver declared for a model.
//
// The body is the image, so the response is written by hand, the way a
// stored file's thumbnail is. It is public like the vocabulary that links it,
// for the reason vocabularySecurity gives.
func registerDeviceModelArtwork(api huma.API, models profile.Catalogue) {
	// The tags are digests of drawings that do not change while the process
	// runs, so they are taken once and not on every request.
	tags := map[string]string{}

	for _, doc := range models.Docs() {
		if !doc.Artwork.IsZero() {
			tags[doc.Model] = artworkTag(doc.Artwork)
		}
	}

	register(api, huma.Operation{
		OperationID: "get-device-model-artwork",
		Method:      http.MethodGet,
		Path:        vocabularyDeviceModelArtworkPath,
		Summary:     "A drawing of a device model, from the driver that reaches it",
		Description: "The drawing a driver declared for a model the web app has no drawing of its own " +
			"for. The `artwork` member of a row in `get-device-model-vocabulary` links here, and only " +
			"a row that carries it has one. A model with no drawing answers 404 with " +
			"core.validation/RESOURCE_NOT_FOUND." +
			paragraphBreak +
			"The entity tag is a digest of the drawing, and a client that sends it back is answered " +
			"304 with no body while its copy is current. A browser runs nothing from the answer. It " +
			"carries `X-Content-Type-Options: nosniff` and a Content-Security-Policy that lets the " +
			"browser load and run nothing, because an SVG is a document it would otherwise run when " +
			"opened alone.",
		Tags:      []string{"vocabularies"},
		Security:  vocabularySecurity,
		Errors:    []int{http.StatusNotFound, http.StatusInternalServerError},
		Responses: artworkResponses(),
	}, func(_ context.Context, in *artworkInput) (*huma.StreamResponse, error) {
		art, ok := models.Artwork(in.Model)
		if !ok {
			return nil, httpfault.ResourceNotFound(httpfault.Reference("device-model-artwork", in.Model))
		}

		tag := tags[in.Model]

		// A drawing has no date, so only the entity tag is read.
		if thumbnailIsCurrent(tag, time.Time{}, in.IfNoneMatch, "") {
			return &huma.StreamResponse{Body: func(hctx huma.Context) {
				writeArtworkHeaders(hctx, tag)
				hctx.SetStatus(http.StatusNotModified)
			}}, nil
		}

		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			writeArtworkHeaders(hctx, tag)
			hctx.SetHeader("Content-Type", art.MediaType)
			hctx.SetHeader("Content-Length", strconv.Itoa(len(art.Bytes)))
			hctx.SetStatus(http.StatusOK)

			_, _ = hctx.BodyWriter().Write(art.Bytes)
		}}, nil
	})
}

// artworkTag is a digest of the drawing, so a rebuilt binary with another
// drawing answers another tag.
func artworkTag(art connectorapi.Artwork) string {
	sum := sha256.Sum256(art.Bytes)

	return etagOf(hex.EncodeToString(sum[:8]))
}

// writeArtworkHeaders writes what every answer on this route carries. The
// policy is the thumbnail's: a drawing needs no script, no style and no other
// document, and an SVG opened on its own would otherwise run what it carries.
// The thumbnail's own writer is not used, because a drawing has no date to
// send as Last-Modified and is cached on other terms.
func writeArtworkHeaders(hctx huma.Context, tag string) {
	hctx.SetHeader("X-Content-Type-Options", "nosniff")
	hctx.SetHeader("Content-Security-Policy", thumbnailPolicy)
	hctx.SetHeader("Cache-Control", artworkCacheControl)
	hctx.SetHeader("ETag", tag)
}

// artworkResponses documents what this operation writes, which huma cannot
// derive from a raw body.
func artworkResponses() map[string]*huma.Response {
	drawing := map[string]*huma.MediaType{}
	for _, mediaType := range []string{connectorapi.ArtworkSVG, connectorapi.ArtworkPNG, connectorapi.ArtworkWebP} {
		drawing[mediaType] = &huma.MediaType{Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}}
	}

	return map[string]*huma.Response{
		strconv.Itoa(http.StatusOK): {
			Description: "The drawing, in the type the driver declared.",
			Content:     drawing,
			Headers: map[string]*huma.Param{
				"ETag": {
					Description: "A digest of the drawing. Send it back in If-None-Match.",
				},
				"Cache-Control": {Description: "public, no-cache: any cache may keep it, and asks before every use."},
			},
		},
		strconv.Itoa(http.StatusNotModified): {
			Description: "The client's copy is still current. No body is sent.",
		},
	}
}
