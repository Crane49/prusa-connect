package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

var benchDrawing = []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)

// drawnModels is a build whose bench driver declared a model with a drawing.
func drawnModels(t *testing.T) profile.Catalogue {
	t.Helper()

	return drawn(t, "Bench_One", benchDrawing)
}

// drawn places one declared model with an SVG drawing.
func drawn(t *testing.T, model string, drawing []byte) profile.Catalogue {
	t.Helper()

	catalogue, err := profile.Place(profile.ModelDoc{
		Model:       model,
		DisplayName: "Bench Works Tool Changer",
		Vendor:      "Bench Works",
		Icon:        profile.IconGeneric,
		Connector:   "bench",
		Artwork:     connectorapi.Artwork{MediaType: connectorapi.ArtworkSVG, Bytes: drawing},
	})
	require.NoError(t, err)
	require.Empty(t, catalogue.Refused())

	return catalogue
}

// TestADeclaredDrawingIsLinkedFromItsRow is how a page finds the drawing: the
// row carries the path, and a row with no drawing carries none.
func TestADeclaredDrawingIsLinkedFromItsRow(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWithModels(t, drawnModels(t)).Handler(), http.MethodGet, vocabularyDeviceModelsPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body modelVocabulary
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	links := map[string]string{}
	for _, item := range body.Items {
		links[item.Model] = item.Artwork
	}

	require.Equal(t, "/api/v1/vocabularies/device-models/Bench_One/artwork", links["Bench_One"])
	require.Empty(t, links["MK4S"], "a core row carries no drawing, the web app has its own")
}

// TestADeclaredDrawingRunsNothing is the promise the headers make. The bytes
// are the driver's, an SVG is a document, and a browser that opened the path
// on its own would run the script inside it without the policy.
func TestADeclaredDrawingRunsNothing(t *testing.T) {
	t.Parallel()

	handler := serverWithModels(t, drawnModels(t)).Handler()

	rec := call(t, handler, http.MethodGet, artworkPathOf("Bench_One"), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, benchDrawing, rec.Body.Bytes())
	require.Equal(t, connectorapi.ArtworkSVG, rec.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "default-src 'none'; sandbox", rec.Header().Get("Content-Security-Policy"))

	tag := rec.Header().Get("ETag")
	require.NotEmpty(t, tag)

	req := httptest.NewRequest(http.MethodGet, artworkPathOf("Bench_One"), nil)
	req.Header.Set("If-None-Match", tag)

	again := httptest.NewRecorder()
	handler.ServeHTTP(again, req)
	require.Equal(t, http.StatusNotModified, again.Code)
	require.Empty(t, again.Body.Bytes())
}

// TestAModelWithNoDrawingAnswersNotFound covers a core row, which the web app
// draws itself, and a spelling no row carries.
func TestAModelWithNoDrawingAnswersNotFound(t *testing.T) {
	t.Parallel()

	handler := serverWithModels(t, drawnModels(t)).Handler()

	for _, model := range []string{"MK4S", "Nobody"} {
		rec := call(t, handler, http.MethodGet, artworkPathOf(model), "")
		problem := requireProblem(t, rec, http.StatusNotFound)
		require.Equal(t, httpfault.CodeResourceNotFound(), problem.Code, model)
	}
}

// TestTheArtworkPathTakesEveryDeclarableModel holds the route's bound on the
// model to the SDK's. A spelling the SDK accepts and the path refused would be
// a drawing linked from its row and never served.
func TestTheArtworkPathTakesEveryDeclarableModel(t *testing.T) {
	t.Parallel()

	longest := strings.Repeat("a", connectorapi.MaxModelBytes)

	rec := call(t, serverWithModels(t, drawn(t, longest, benchDrawing)).Handler(),
		http.MethodGet, artworkPathOf(longest), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// TestEachDrawingHasItsOwnTag holds the tags taken at start to the drawing
// each one names. Two models in one server answer two tags, and a client
// holding one drawing is not told it holds the other.
func TestEachDrawingHasItsOwnTag(t *testing.T) {
	t.Parallel()

	doc := func(model string, drawing []byte) profile.ModelDoc {
		return profile.ModelDoc{
			Model: model, DisplayName: model, Vendor: "Bench Works", Icon: profile.IconGeneric,
			Connector: "bench", DeclaredBy: "com.example.aux.bench/bench",
			Artwork: connectorapi.Artwork{MediaType: connectorapi.ArtworkSVG, Bytes: drawing},
		}
	}

	catalogue, err := profile.Place(
		doc("Bench_One", benchDrawing),
		doc("Bench_Two", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)),
	)
	require.NoError(t, err)

	handler := serverWithModels(t, catalogue).Handler()

	one := call(t, handler, http.MethodGet, artworkPathOf("Bench_One"), "")
	two := call(t, handler, http.MethodGet, artworkPathOf("Bench_Two"), "")
	require.Equal(t, http.StatusOK, one.Code, one.Body.String())
	require.Equal(t, http.StatusOK, two.Code, two.Body.String())
	require.NotEqual(t, one.Header().Get("ETag"), two.Header().Get("ETag"))

	req := httptest.NewRequest(http.MethodGet, artworkPathOf("Bench_Two"), nil)
	req.Header.Set("If-None-Match", one.Header().Get("ETag"))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "a client holding one drawing was told it holds the other")
}

// TestATagFollowsTheDrawing is the property artworkTag states: a rebuilt
// binary with another drawing for the same model answers another tag, so a
// client holding the old drawing fetches the new one.
func TestATagFollowsTheDrawing(t *testing.T) {
	t.Parallel()

	tagOf := func(drawing []byte) string {
		rec := call(t, serverWithModels(t, drawn(t, "Bench_One", drawing)).Handler(),
			http.MethodGet, artworkPathOf("Bench_One"), "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		return rec.Header().Get("ETag")
	}

	require.NotEqual(t, tagOf(benchDrawing), tagOf([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)))
}
