package files_test

import (
	"cmp"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/files"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// farDate is a Retry-After or Expires a relay must pass on as written.
const farDate = "Fri, 01 Jan 2100 00:00:00 GMT"

// relayTo starts a server with no objects of its own whose parent is parent.
func relayTo(
	t *testing.T,
	parentURL string,
	anchors files.ClientConfig,
	childID fractal.Identity,
	copies files.Copies,
	log *slog.Logger,
) *httptest.Server {
	t.Helper()

	srv := httptest.NewTLSServer(files.NewServer(files.ServerConfig{
		Publisher: shelf{}, Storage: &chunkStore{}, Cache: copies,
		Children: childOnly(childID), Tolerance: time.Minute,
		Parent: func() (string, bool) { return parentURL, true },
		Client: files.NewClient(anchors), Log: log,
	}).Handler())
	t.Cleanup(srv.Close)
	t.Cleanup(srv.Client().CloseIdleConnections)

	return srv
}

func TestRelay(t *testing.T) {
	t.Parallel()

	child, childID := newSigner(t)
	relaySigner, relayID := newSigner(t)
	data := []byte("GCDE abcdef")
	target := "/objects/" + sdkfiles.Sum(data).Text()
	once := func(header http.Header, cacheControl string) []seen {
		return []seen{{Method: http.MethodGet, Range: header.Get("Range"), CacheControl: cacheControl, Caller: relayID}}
	}
	failure := func(code int, pairs ...string) answer { return answer{Status: code, Header: refusedWith(pairs...)} }

	for name, tc := range map[string]struct {
		steps   []http.HandlerFunc
		method  string
		timeout time.Duration
		header  http.Header
		copy    bool
		want    answer
		asked   []seen
	}{
		"a whole object": {
			steps: []http.HandlerFunc{whole(data, keptFor())},
			want:  answer{Status: http.StatusOK, Header: served("11"), Body: string(data)},
			asked: once(nil, ""),
		},
		"a HEAD passes up as a HEAD": {
			steps:  []http.HandlerFunc{whole(data, keptFor())},
			method: http.MethodHead,
			want:   answer{Status: http.StatusOK, Header: served("11")},
			asked:  []seen{{Method: http.MethodHead, Caller: relayID}},
		},
		"a range passes up and its part down": {
			steps:  []http.HandlerFunc{part(data, 5, keptFor())},
			header: http.Header{"Range": {"bytes=5-"}},
			want:   answer{Status: http.StatusPartialContent, Header: served("6", "Content-Range", "bytes 5-10/11"), Body: "abcdef"},
			asked:  once(http.Header{"Range": {"bytes=5-"}}, ""),
		},
		"an instant passes unchanged": {
			steps: []http.HandlerFunc{whole(data, http.Header{"Cache-Control": {"private"}, "Expires": {farDate}})},
			want: answer{Status: http.StatusOK, Body: string(data), Header: map[string]string{
				"Content-Type": "application/octet-stream", "Accept-Ranges": "bytes", "Content-Length": "11",
				"Cache-Control": "private", "Expires": farDate,
			}},
			asked: once(nil, ""),
		},
		"no-cache passes up and skips the copy": {
			steps:  []http.HandlerFunc{whole(data, keptFor())},
			header: http.Header{"Cache-Control": {"no-cache"}},
			copy:   true,
			want:   answer{Status: http.StatusOK, Header: served("11"), Body: string(data)},
			asked:  once(nil, "no-cache"),
		},
		"a copy answers without the parent": {
			steps: []http.HandlerFunc{status(http.StatusInternalServerError, nil)},
			copy:  true,
			want:  answer{Status: http.StatusOK, Header: served("11"), Body: string(data)},
		},
		"400 becomes 503 for minutes": {steps: []http.HandlerFunc{status(http.StatusBadRequest, nil)}, want: failure(http.StatusServiceUnavailable, "Retry-After", "minutes"), asked: once(nil, "")},
		"401 becomes 503 for minutes": {steps: []http.HandlerFunc{status(http.StatusUnauthorized, nil)}, want: failure(http.StatusServiceUnavailable, "Retry-After", "minutes"), asked: once(nil, "")},
		"404 passes":                  {steps: []http.HandlerFunc{status(http.StatusNotFound, nil)}, want: failure(http.StatusNotFound), asked: once(nil, "")},
		"416 passes":                  {steps: []http.HandlerFunc{status(http.StatusRequestedRangeNotSatisfiable, nil)}, want: failure(http.StatusRequestedRangeNotSatisfiable), asked: once(nil, "")},
		"502 passes with its date":    {steps: []http.HandlerFunc{status(http.StatusBadGateway, http.Header{"Retry-After": {farDate}})}, want: failure(http.StatusBadGateway, "Retry-After", farDate), asked: once(nil, "")},
		"503 passes with its date":    {steps: []http.HandlerFunc{status(http.StatusServiceUnavailable, http.Header{"Retry-After": {farDate}})}, want: failure(http.StatusServiceUnavailable, "Retry-After", farDate), asked: once(nil, "")},
		"503 without a date gets one": {steps: []http.HandlerFunc{status(http.StatusServiceUnavailable, nil)}, want: failure(http.StatusServiceUnavailable, "Retry-After", "seconds"), asked: once(nil, "")},
		"another failure becomes 502": {steps: []http.HandlerFunc{status(http.StatusInternalServerError, nil)}, want: failure(http.StatusBadGateway, "Retry-After", "seconds"), asked: once(nil, "")},
		"a redirect is not followed":  {steps: []http.HandlerFunc{status(http.StatusFound, http.Header{"Location": {target}})}, want: failure(http.StatusBadGateway, "Retry-After", "seconds"), asked: once(nil, "")},
		"a 206 with no Content-Range": {steps: []http.HandlerFunc{status(http.StatusPartialContent, nil)}, want: failure(http.StatusBadGateway, "Retry-After", "seconds"), asked: once(nil, "")},
		"a slow parent is 502": {
			timeout: 300 * time.Millisecond,
			steps: []http.HandlerFunc{func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
				case <-time.After(10 * time.Second):
				}

				whole(data, keptFor())(w, r)
			}},
			want:  failure(http.StatusBadGateway, "Retry-After", "seconds"),
			asked: once(nil, ""),
		},
		"a cut answer is cut down": {
			steps: []http.HandlerFunc{trickle(data, 4, keptFor())},
			want:  answer{Status: http.StatusOK, Header: served("11"), Body: "GCDE", Cut: true},
			asked: once(nil, ""),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			parent := newScripted(t, tc.steps...)

			var copies files.Copies = heldCopies{}
			if tc.copy {
				copies = keepCopies(t, data)
			}

			relay := relayTo(t, parent.srv.URL, files.ClientConfig{
				Signer: relaySigner, Anchors: parent.anchors(), Timeout: cmp.Or(tc.timeout, 10*time.Second),
			}, childID, copies, nil)

			got := askAs(t, relay.Client(), relay.URL, child, cmp.Or(tc.method, http.MethodGet), target, tc.header)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("the answer is %+v, want %+v", got, tc.want)
			}

			if asked := parent.requests(); !reflect.DeepEqual(asked, tc.asked) {
				t.Errorf("the parent saw %+v, want %+v", asked, tc.asked)
			}
		})
	}
}

func TestRelayCannotReachTheParent(t *testing.T) {
	t.Parallel()

	child, childID := newSigner(t)
	relaySigner, _ := newSigner(t)

	gone := httptest.NewTLSServer(http.NotFoundHandler())
	gone.Close()

	relay := relayTo(
		t,
		gone.URL,
		files.ClientConfig{Signer: relaySigner, Timeout: time.Second},
		childID,
		heldCopies{},
		nil,
	)

	got := askAs(t, relay.Client(), relay.URL, child, http.MethodGet, "/objects/"+sdkfiles.EmptyListing.Text(), nil)
	if want := (answer{Status: http.StatusBadGateway, Header: refusedWith("Retry-After", "seconds")}); !reflect.DeepEqual(
		got,
		want,
	) {
		t.Errorf("the answer is %+v, want %+v", got, want)
	}
}

// A relay says in one line what the parent answered, so the server in the
// middle shows that a print went through it, or why it did not.
func TestRelayLogsWhatTheParentAnswered(t *testing.T) {
	t.Parallel()

	data := []byte("GCDE abcdef")
	id := sdkfiles.Sum(data).Text()

	for name, tc := range map[string]struct {
		parent http.HandlerFunc
		want   string
	}{
		"passed down": {
			parent: whole(data, keptFor()),
			want:   `level=INFO msg="an object was relayed from the parent" identity=` + id + ` status=200 bytes=11`,
		},
		"not found": {
			parent: status(http.StatusNotFound, nil),
			want:   `level=INFO msg="an object was not relayed from the parent" identity=` + id + ` status=404`,
		},
		"range not satisfiable": {
			parent: status(http.StatusRequestedRangeNotSatisfiable, nil),
			want:   `level=INFO msg="an object was not relayed from the parent" identity=` + id + ` status=416`,
		},
		"bad gateway": {
			parent: status(http.StatusBadGateway, nil),
			want:   `level=INFO msg="an object was not relayed from the parent" identity=` + id + ` status=502`,
		},
		"unavailable": {
			parent: status(http.StatusServiceUnavailable, nil),
			want:   `level=INFO msg="an object was not relayed from the parent" identity=` + id + ` status=503`,
		},
		"another failure": {
			parent: status(http.StatusInternalServerError, nil),
			want:   `level=INFO msg="an object was not relayed from the parent" identity=` + id + ` status=500`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			child, childID := newSigner(t)
			relaySigner, _ := newSigner(t)

			logged := make(logLines, 4)

			log := slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{
				ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
					if a.Key == slog.TimeKey {
						return slog.Attr{}
					}

					return a
				},
			}))

			parent := newScripted(t, tc.parent)
			relay := relayTo(t, parent.srv.URL, files.ClientConfig{
				Signer: relaySigner, Anchors: parent.anchors(), Timeout: 10 * time.Second,
			}, childID, heldCopies{}, log)

			askAs(t, relay.Client(), relay.URL, child, http.MethodGet, "/objects/"+id, nil)

			// The relay logs after the last byte left, so the line may come
			// after the child read the answer.
			var got string

			select {
			case got = <-logged:
			case <-time.After(5 * time.Second):
			}

			if got != tc.want+"\n" {
				t.Errorf("the relay logged %q, want %q", got, tc.want+"\n")
			}
		})
	}
}

// logLines hands each log line to the test as it is written.
type logLines chan string

func (l logLines) Write(p []byte) (int, error) {
	l <- string(p)

	return len(p), nil
}
