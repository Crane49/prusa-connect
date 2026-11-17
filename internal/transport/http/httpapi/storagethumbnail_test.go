package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// thumbnailModified is the file's modification time these tests answer
// Last-Modified from. It is a fixed instant so the header is the same on every
// run.
//
//nolint:gochecknoglobals // a fixed instant shared by the cases below.
var thumbnailModified = time.Unix(1_700_000_000, 0).UTC()

func thumbnailStores() *fakeStorages {
	return &fakeStorages{
		object:   "G28\n",
		digest:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		picture:  []byte("\x89PNG\r\n\x1a\n and the rest of a picture"),
		modified: thumbnailModified,
	}
}

func thumbnailRequest(t *testing.T, stores Storages, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	handler := serverWithStores(t, stores).Handler()

	req := httptest.NewRequest(http.MethodGet,
		storagesPathPrefix+"/bench/thumbnail?path=models%2Fbracket.bgcode", nil)

	for name, value := range headers {
		req.Header.Set(name, value)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

// The picture is served with the headers a page needs to cache it: its own
// media type, the file's modification time, and an entity tag taken from the
// storage's digest.
func TestTheThumbnailIsServedWithItsValidators(t *testing.T) {
	t.Parallel()

	stores := thumbnailStores()
	rec := thumbnailRequest(t, stores, nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Equal(t, `"`+stores.digest+`"`, rec.Header().Get("ETag"))
	require.Equal(t, thumbnailModified.Format(http.TimeFormat), rec.Header().Get("Last-Modified"))
	require.Equal(t, "private, no-cache", rec.Header().Get("Cache-Control"))
	require.Equal(t, "default-src 'none'; sandbox", rec.Header().Get("Content-Security-Policy"))
	require.Equal(t, stores.picture, rec.Body.Bytes())
}

// Two people can share one browser, and its cache keeps the picture. The
// answer tells the browser to ask the server before every use, so a person
// without read_storage is refused rather than shown the copy another person
// left in the cache.
func TestACachedThumbnailIsCheckedForEveryCaller(t *testing.T) {
	t.Parallel()

	stores := thumbnailStores()
	held := thumbnailRequest(t, stores, nil)
	require.Equal(t, "private, no-cache", held.Header().Get("Cache-Control"))

	instance, err := ref.New(ref.KindStorage, "bench")
	require.NoError(t, err)

	stores.versionRefused = authz.Denied(authz.ActionReadStorage, instance)
	rec := thumbnailRequest(t, stores, map[string]string{
		"If-None-Match": held.Header().Get("ETag"),
	})

	requireProblem(t, rec, http.StatusForbidden)
	require.Equal(t, 1, stores.pictureReads, "a refused caller was read the picture")
}

// A client that already holds the picture is answered from the file's record
// alone. Reading the picture to write a response with no body in it would parse
// a print file for nothing, which is the whole cost of this route.
func TestAConditionalRequestNeverReadsThePicture(t *testing.T) {
	t.Parallel()

	stores := thumbnailStores()

	rec := thumbnailRequest(t, stores, map[string]string{
		"If-None-Match": `"` + stores.digest + `"`,
	})

	require.Equal(t, http.StatusNotModified, rec.Code, rec.Body.String())
	require.Zero(t, stores.pictureReads, "a 304 asked the storage for the picture")

	// The validators are on the 304 as well, so a client that holds a copy
	// keeps holding it rather than dropping the tag it just sent.
	require.Equal(t, `"`+stores.digest+`"`, rec.Header().Get("ETag"))
	require.Equal(t, thumbnailModified.Format(http.TimeFormat), rec.Header().Get("Last-Modified"))
	require.Equal(t, "default-src 'none'; sandbox", rec.Header().Get("Content-Security-Policy"))
	require.Equal(t, "private, no-cache", rec.Header().Get("Cache-Control"))
}

// A client that sends back what it holds is answered 304 with no body, which is
// what makes a folder of two hundred models cheap to reopen.
func TestTheThumbnailAnswersAConditionalRequest(t *testing.T) {
	t.Parallel()

	stores := thumbnailStores()

	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{
			name:    "the same entity tag",
			headers: map[string]string{"If-None-Match": `"` + stores.digest + `"`},
			want:    http.StatusNotModified,
		},
		{
			name:    "any entity tag",
			headers: map[string]string{"If-None-Match": "*"},
			want:    http.StatusNotModified,
		},
		{
			name:    "a different entity tag",
			headers: map[string]string{"If-None-Match": `"something else"`},
			want:    http.StatusOK,
		},
		{
			name: "a date no older than the file",
			headers: map[string]string{
				"If-Modified-Since": thumbnailModified.Format(http.TimeFormat),
			},
			want: http.StatusNotModified,
		},
		{
			name: "a date older than the file",
			headers: map[string]string{
				"If-Modified-Since": thumbnailModified.Add(-time.Hour).Format(http.TimeFormat),
			},
			want: http.StatusOK,
		},
		{
			name:    "a date the server cannot read",
			headers: map[string]string{"If-Modified-Since": "yesterday afternoon"},
			want:    http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := thumbnailRequest(t, thumbnailStores(), tc.headers)

			require.Equal(t, tc.want, rec.Code, rec.Body.String())

			if tc.want == http.StatusNotModified {
				require.Empty(t, rec.Body.Bytes(), "a 304 carries no body")
			}
		})
	}
}

// A file with no picture answers 404 with the registered code, so a page can
// tell "no picture" apart from "no file".
func TestAFileWithNoPictureAnswersTheRegisteredCode(t *testing.T) {
	t.Parallel()

	stores := thumbnailStores()
	stores.picture = nil

	rec := thumbnailRequest(t, stores, nil)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Equal(t, problemMediaType, rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Body.String(), "core.storage/NO_THUMBNAIL")
}

// The route takes a path and refuses a request with none, so nothing reads a
// file the caller did not name.
func TestTheThumbnailRequiresAPath(t *testing.T) {
	t.Parallel()

	handler := serverWithStores(t, thumbnailStores()).Handler()

	req := httptest.NewRequest(http.MethodGet, storagesPathPrefix+"/bench/thumbnail", nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// The stat route carries what the slicer said, under an optional object, so a
// client asks once per file it is about to show.
func TestStatCarriesWhatTheSlicerSaid(t *testing.T) {
	t.Parallel()

	stores := &statedStorages{
		entry: StorageEntry{
			Name: "bracket.bgcode", Path: "models/bracket.bgcode", Size: 12_526,
			Print: &PrintDetails{
				EstimatedSeconds: 136,
				FilamentMM:       133.16,
				FilamentG:        0.4,
				LayerHeightMM:    0.3,
				NozzleMM:         0.4,
				Material:         "PLA",
				PrinterModel:     "MK4IS",
				ThumbnailWidth:   313,
				ThumbnailHeight:  173,
			},
		},
	}

	handler := serverWithStores(t, stores).Handler()

	req := httptest.NewRequest(http.MethodGet,
		storagesPathPrefix+"/bench/stat?path=models%2Fbracket.bgcode", nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"estimated_seconds":136`)
	require.Contains(t, rec.Body.String(), `"material":"PLA"`)
	require.Contains(t, rec.Body.String(), `"printer_model":"MK4IS"`)
	require.Contains(t, rec.Body.String(), `"thumbnail_width":313`)
}

// A file the server read nothing out of leaves the object out, rather than
// sending a row of zeroes a page would render as a print that uses no filament.
func TestStatLeavesThePrintObjectOutWhenTheFileSaidNothing(t *testing.T) {
	t.Parallel()

	stores := &statedStorages{entry: StorageEntry{Name: "notes.txt", Path: "notes.txt", Size: 12}}
	handler := serverWithStores(t, stores).Handler()

	req := httptest.NewRequest(http.MethodGet, storagesPathPrefix+"/bench/stat?path=notes.txt", nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), `"print"`)
}

// statedStorages answers one prepared entry, so a test can drive what the stat
// route renders without a storage behind it.
type statedStorages struct {
	fakeStorages

	entry StorageEntry
}

func (s *statedStorages) Stat(context.Context, string, string, string) (StorageEntry, error) {
	return s.entry, nil
}
