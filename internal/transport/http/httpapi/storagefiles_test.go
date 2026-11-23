package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// fakeStorages is one storage holding one object, for the two routes that do
// not marshal a struct.
//
// Save copies rather than reading the body whole, because what the upload route
// promises is that nothing on it holds the file, and a fake that read it into
// memory would pass whether or not the handler streamed.
type fakeStorages struct {
	object string
	saved  []byte
	closes int
	// reads is how many bytes were read out of the object.
	reads int
	// temporary says the last upload asked for a temporary copy.
	temporary bool
	// digest is what this fake claims it hashed the object to, so a test can
	// assert the route carries the storage's own digest rather than one it
	// computed for itself.
	digest string
	// picture is what the thumbnail route serves, and empty is a file that
	// carries none.
	picture []byte
	// modified is the file's modification time, which the thumbnail route
	// answers Last-Modified and a conditional request from.
	modified time.Time
	// pictureReads counts how often the picture itself was asked for, which is
	// the work a 304 must not do.
	pictureReads int
	// versionRefused is what FileVersion answers instead of the validators,
	// which is how the storage refuses a caller without read_storage.
	versionRefused error
	// location is the URL DownloadURL hands out, and empty is a storage that
	// presigns nothing.
	location string
	// deleted is every batch DeleteMany was given, and deleteRefused what it
	// answers instead of removing them.
	deleted       [][]string
	deleteRefused error
}

func (s *fakeStorages) List(context.Context, string) ([]Storage, error) {
	return []Storage{{Name: "bench", DisplayName: "bench", Browse: true, Write: true}}, nil
}

func (s *fakeStorages) Files(context.Context, string, string, string) ([]StorageEntry, error) {
	return nil, nil
}

func (s *fakeStorages) Open(_ context.Context, _ string, _ string, path string) (Object, error) {
	return Object{
		Name:     path,
		Modified: time.Unix(1_700_000_000, 0),
		Content:  &countingObject{Reader: strings.NewReader(s.object), closed: &s.closes, read: &s.reads},
	}, nil
}

func (s *fakeStorages) Stat(_ context.Context, _ string, _ string, path string) (StorageEntry, error) {
	return StorageEntry{Name: path, Path: path, Size: int64(len(s.object)), SHA256: s.digest}, nil
}

// Thumbnail serves whatever picture the fake was given, so the route's headers
// and its conditional answers can be driven without a real storage.
func (s *fakeStorages) Thumbnail(
	_ context.Context, _ string, _ string, _ string,
) (StorageThumbnail, error) {
	s.pictureReads++

	if len(s.picture) == 0 {
		return StorageThumbnail{}, fault.StorageNoThumbnail.New("the file carries no picture")
	}

	return StorageThumbnail{
		Bytes:     s.picture,
		MediaType: "image/png",
		Modified:  s.modified,
		Version:   s.digest,
	}, nil
}

// FileVersion answers the validators the thumbnail route decides a 304 from,
// and counts the calls, so a test can prove a 304 never reaches Thumbnail.
func (s *fakeStorages) FileVersion(
	_ context.Context, _ string, _ string, _ string,
) (FileVersion, error) {
	if s.versionRefused != nil {
		return FileVersion{}, s.versionRefused
	}

	return FileVersion{Modified: s.modified, Version: s.digest}, nil
}

// FileExtent answers the length of the object this fake holds. Nothing here
// keeps a directory, so the path never names one.
func (s *fakeStorages) FileExtent(
	_ context.Context, _ string, _ string, _ string,
) (FileExtent, error) {
	return FileExtent{Size: int64(len(s.object))}, nil
}

func (s *fakeStorages) Save(
	_ context.Context, _ string, _ string, path string, body io.Reader,
) (SaveResult, error) {
	written, err := io.Copy(&sink{into: &s.saved}, body)
	if err != nil {
		//nolint:wrapcheck // the reader's own failure is what the route maps.
		return SaveResult{Path: path, Size: written}, err
	}

	return SaveResult{Path: path, Size: written, SHA256: s.digest}, nil
}

// SaveTemporary records that the upload asked for a temporary copy, and
// answers the path the server would choose.
func (s *fakeStorages) SaveTemporary(
	ctx context.Context, principal string, storage string, name string, body io.Reader,
) (SaveResult, error) {
	s.temporary = true

	saved, err := s.Save(ctx, principal, storage, ".pcl-incoming/1-copy/"+name, body)
	saved.Temporary = err == nil

	return saved, err
}

func (s *fakeStorages) Delete(context.Context, string, string, string) error { return nil }

func (s *fakeStorages) DeleteMany(_ context.Context, _ string, _ string, paths []string) error {
	s.deleted = append(s.deleted, paths)

	return s.deleteRefused
}

func (s *fakeStorages) DownloadURL(context.Context, string, string, string) (string, bool, error) {
	return s.location, s.location != "", nil
}

func (s *fakeStorages) CreateFolder(context.Context, string, string, string) error { return nil }

func (s *fakeStorages) Move(context.Context, string, string, string, string) error { return nil }

// sink collects what was written without pretending to be a file.
type sink struct{ into *[]byte }

func (s *sink) Write(p []byte) (int, error) {
	*s.into = append(*s.into, p...)

	return len(p), nil
}

// countingObject is an object that says how often it was closed, which is the
// thing a download has to get right on every path out.
type countingObject struct {
	*strings.Reader

	closed *int
	read   *int
}

func (o *countingObject) Read(p []byte) (int, error) {
	n, err := o.Reader.Read(p)
	*o.read += n

	return n, err
}

func (o *countingObject) Close() error {
	*o.closed++

	return nil
}

// serverWithStores is a server whose storage is the caller's.
func serverWithStores(t *testing.T, stores Storages) *Server {
	t.Helper()

	return serverWithStoresBounded(t, stores, 0)
}

// serverWithStoresBounded is the same server with the upload bound named, for
// the refusal a body over it earns.
func serverWithStoresBounded(t *testing.T, stores Storages, uploadBytes int64) *Server {
	t.Helper()

	server, err := New(Config{
		Version:        "test",
		Principals:     fixedPrincipal("user:operator"),
		Resources:      seededStore(),
		Presets:        seededPresets(),
		Endpoints:      seededEndpoints(),
		Grants:         &fakeGrantStore{},
		Sessions:       newFakeSessions(),
		Accounts:       &fakeAccounts{},
		Readiness:      &fakeReadiness{report: readyReport()},
		Devices:        noPrinters{},
		Stores:         stores,
		Events:         noEvents{},
		MaxUploadBytes: uploadBytes,
	})
	require.NoError(t, err)

	return server
}

// A body over the upload bound is refused with the bound in the refusal, so a
// client is told the number it has to fit inside rather than that something
// went wrong.
func TestAnUploadOverTheBoundIsRefusedWithTheBound(t *testing.T) {
	t.Parallel()

	const bound = 8

	handler := serverWithStoresBounded(t, &fakeStorages{}, bound).Handler()

	req := httptest.NewRequest(http.MethodPut,
		storagesPathPrefix+"/bench/content?path=part.gcode",
		strings.NewReader(strings.Repeat("G", 4096)))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	require.Equal(t, problemMediaType, rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Body.String(), httpfault.CodeBodyTooLarge().String())
	require.Contains(t, rec.Body.String(), "8", "the refusal does not name the bound")

	t.Logf("observed: %s", rec.Body.String())
}

// An upload inside the bound is written, so the bound refuses a body and not a
// route.
func TestAnUploadInsideTheBoundIsWritten(t *testing.T) {
	t.Parallel()

	stores := &fakeStorages{}
	handler := serverWithStoresBounded(t, stores, 4096).Handler()

	req := httptest.NewRequest(http.MethodPut,
		storagesPathPrefix+"/bench/content?path=part.gcode", strings.NewReader("G28\n"))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "G28\n", string(stores.saved))
	require.Contains(t, rec.Body.String(), `"bytes":4`)
}

// An upload with temporary=true is handed to SaveTemporary, and the answer
// names the path the server chose and says the copy is temporary. One without
// it is an ordinary save, and its answer says nothing of the kind.
func TestATemporaryUploadAnswersWhereTheServerPutIt(t *testing.T) {
	t.Parallel()

	stores := &fakeStorages{}
	handler := serverWithStores(t, stores).Handler()

	req := httptest.NewRequest(http.MethodPut,
		storagesPathPrefix+"/bench/content?path=part.gcode&temporary=true", strings.NewReader("G28\n"))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.True(t, stores.temporary, "the route did not ask for a temporary copy")
	require.Contains(t, rec.Body.String(), `"path":".pcl-incoming/1-copy/part.gcode"`)
	require.Contains(t, rec.Body.String(), `"temporary":true`)

	kept := &fakeStorages{}
	req = httptest.NewRequest(http.MethodPost,
		storagesPathPrefix+"/bench/content?path=models/part.gcode", strings.NewReader("G28\n"))
	rec = httptest.NewRecorder()
	serverWithStores(t, kept).Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.False(t, kept.temporary)
	require.Contains(t, rec.Body.String(), `"path":"models/part.gcode"`)
	require.NotContains(t, rec.Body.String(), `"temporary"`)
}

// TestOnlyTheUploadRoutesCarryTheUploadBound holds carriesAFile against the
// routes this server actually registers.
//
// The function matches a method and a path prefix by hand, because it runs
// before chi has matched anything. That is only safe while the set of routes
// under the prefix is the set that carries a file, so the check is against the
// router rather than against a list written beside it: a new route under
// /api/v1/storages that is not an upload, or a new upload somewhere else, fails
// here rather than being silently given the wrong bound.
func TestOnlyTheUploadRoutesCarryTheUploadBound(t *testing.T) {
	t.Parallel()

	// Both spellings of a storage upload and the signed core staging route.
	// A body bound that omitted one would cap a release or print file at the
	// document bound.
	uploads := []string{
		http.MethodPut + " " + storageContentPath,
		http.MethodPost + " " + storageContentPath,
		http.MethodPost + " " + upgradeStaged,
		http.MethodPost + " " + upgradeBundle,
	}

	var (
		carrying []string
		walked   int
	)

	err := chi.Walk(newTestServer(t).mux,
		func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			walked++

			req := httptest.NewRequest(method, concretePath(pattern), nil)
			if carriesAFile(req) {
				carrying = append(carrying, method+" "+pattern)
			}

			return nil
		})
	require.NoError(t, err)
	require.NotEmpty(t, walked, "this gate walked no routes")

	require.ElementsMatch(t, uploads, carrying,
		"a route takes the wrong body bound: an upload capped at the document bound refuses a "+
			"print file, and a document route given the upload bound accepts megabytes of JSON")

	t.Logf("observed: %d routes walked, %d carrying the upload bound", walked, len(carrying))
}

// concretePath fills a chi pattern's parameters in, because carriesAFile reads a
// URL and not a pattern.
func concretePath(pattern string) string {
	segments := strings.Split(pattern, "/")
	for i, segment := range segments {
		if strings.HasPrefix(segment, "{") || segment == "*" {
			segments[i] = "x"
		}
	}

	return strings.Join(segments, "/")
}

// carriesAFile matches on the route and never on what the client said it was
// sending, so a caller cannot widen its own bound by relabelling the body.
func TestTheUploadBoundFollowsTheRouteAndNotTheHeader(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		method string
		path   string
		want   bool
	}{
		"the upload route": {
			method: http.MethodPut, path: storagesPathPrefix + "/bench/content?path=a.gcode", want: true,
		},
		"a post under the same prefix": {
			method: http.MethodPost, path: storagesPathPrefix + "/bench/folders", want: false,
		},
		"a get under the same prefix": {
			method: http.MethodGet, path: storagesPathPrefix + "/bench/files", want: false,
		},
		"a put somewhere else": {
			method: http.MethodPut, path: "/api/v1/resources/storage/bench", want: false,
		},
		"a path that only looks like the prefix": {
			method: http.MethodPut, path: "/api/v1/storagesomething/content", want: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Content-Type", "application/json")

			require.Equal(t, tc.want, carriesAFile(req))
		})
	}
}

// A download from a storage that hands out a URL is a redirect to it, and no
// byte of the file passes through this server. One that hands out none is
// served here as before.
func TestADownloadFromAPresigningStorageIsARedirect(t *testing.T) {
	t.Parallel()

	const location = "https://objects.example.com/bench/part.gcode?signature=abc"

	presigning := &fakeStorages{object: "G28\n", location: location}
	rec := call(t, serverWithStores(t, presigning).Handler(), http.MethodGet,
		storagesPathPrefix+"/bench/content?path=part.gcode", "")

	require.Equal(t, http.StatusTemporaryRedirect, rec.Code, rec.Body.String())
	require.Equal(t, location, rec.Header().Get("Location"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Empty(t, rec.Body.String())
	require.Zero(t, presigning.closes, "the file was opened for a download the storage serves itself")

	relayed := &fakeStorages{object: "G28\n"}
	rec = call(t, serverWithStores(t, relayed).Handler(), http.MethodGet,
		storagesPathPrefix+"/bench/content?path=part.gcode", "")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "G28\n", rec.Body.String())
	require.Empty(t, rec.Header().Get("Location"))
}

// A batch delete hands the storage the list the client sent, and a batch where
// some entries stayed keeps its own code on the way out.
func TestABatchDeleteReachesTheStorageAndKeepsItsRefusal(t *testing.T) {
	t.Parallel()

	stores := &fakeStorages{}
	handler := serverWithStores(t, stores).Handler()

	rec := call(t, handler, http.MethodPost, storagesPathPrefix+"/bench/deletions",
		`{"paths":["models/a.gcode","models/old"]}`)

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, [][]string{{"models/a.gcode", "models/old"}}, stores.deleted)

	rec = call(t, handler, http.MethodPost, storagesPathPrefix+"/bench/deletions", `{"paths":[]}`)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Len(t, stores.deleted, 1, "an empty list reached the storage")

	partial := &fakeStorages{deleteRefused: fault.StorageDeleteIncomplete.New("some of the entries are still there")}
	rec = call(t, serverWithStores(t, partial).Handler(), http.MethodPost,
		storagesPathPrefix+"/bench/deletions", `{"paths":["a.gcode","b.gcode"]}`)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), fault.StorageDeleteIncomplete.Code().String())
}

// A refusal from the storage keeps its own code on the way out, so a caller
// branches on what the storage said rather than on a status.
func TestAStorageRefusalKeepsItsCode(t *testing.T) {
	t.Parallel()

	handler := serverWith(t, seededStore()).Handler()

	rec := call(t, handler, http.MethodGet, storagesPathPrefix+"/nowhere/files", "")

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), fault.StorageNotFound.Code().String())
}
