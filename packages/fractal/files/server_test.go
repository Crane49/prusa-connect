package files_test

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/files"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

func TestServer(t *testing.T) {
	t.Parallel()

	child, childID := newSigner(t)
	stranger, _ := newSigner(t)

	cube := []byte("GCDE cube")
	empty := []byte{}
	big := bytes.Repeat([]byte("x"), storageapi.MaxReadBytes+10)
	kept := []byte("a kept copy")

	listing, err := sdkfiles.Listing{}.Encode()
	if err != nil {
		t.Fatalf("encode the empty listing: %v", err)
	}

	store := &chunkStore{files: map[string][]byte{
		"library/prints/cube.bgcode": cube,
		"library/prints/empty.gcode": empty,
		"library/prints/big.gcode":   big,
	}}
	published := shelf{
		sdkfiles.Sum(cube):    {Storage: "library", Path: "prints/cube.bgcode", Size: int64(len(cube))},
		sdkfiles.Sum(empty):   {Storage: "library", Path: "prints/empty.gcode", Size: 0},
		sdkfiles.Sum(big):     {Storage: "library", Path: "prints/big.gcode", Size: int64(len(big))},
		sdkfiles.EmptyListing: {Listing: listing},
	}

	srv := httptest.NewTLSServer(files.NewServer(files.ServerConfig{
		Publisher: published, Storage: store, Cache: keepCopies(t, kept),
		Children: childOnly(childID), BasePath: "/fractal/", Tolerance: time.Minute,
	}).Handler())
	t.Cleanup(srv.Close)

	at := func(data []byte) string { return "/fractal/objects/" + sdkfiles.Sum(data).Text() }
	ranged := func(value string) http.Header { return http.Header{"Range": {value}} }
	stale, err := files.SignProof(child, time.Now().Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("sign a stale proof: %v", err)
	}

	unauthorized := answer{Status: http.StatusUnauthorized, Header: refusedWith("WWW-Authenticate", "Fractal-Proof")}
	badRequest := answer{Status: http.StatusBadRequest, Header: refusedWith()}

	for name, tc := range map[string]struct {
		signer joinwire.Signer
		method string
		target string
		header http.Header
		want   answer
	}{
		"a whole file": {child, http.MethodGet, at(cube), nil,
			answer{Status: http.StatusOK, Header: served("9"), Body: "GCDE cube"}},
		"head": {child, http.MethodHead, at(cube), nil,
			answer{Status: http.StatusOK, Header: served("9")}},
		"a closed range": {child, http.MethodGet, at(cube), ranged("bytes=0-3"),
			answer{Status: http.StatusPartialContent, Header: served("4", "Content-Range", "bytes 0-3/9"), Body: "GCDE"}},
		"an open range": {child, http.MethodGet, at(cube), ranged("bytes=5-"),
			answer{Status: http.StatusPartialContent, Header: served("4", "Content-Range", "bytes 5-8/9"), Body: "cube"}},
		"a range past the end": {child, http.MethodGet, at(cube), ranged("bytes=5-99"),
			answer{Status: http.StatusPartialContent, Header: served("4", "Content-Range", "bytes 5-8/9"), Body: "cube"}},
		"If-Range is ignored": {child, http.MethodGet, at(cube),
			http.Header{"Range": {"bytes=0-3"}, "If-Range": {`"x"`}},
			answer{Status: http.StatusPartialContent, Header: served("4", "Content-Range", "bytes 0-3/9"), Body: "GCDE"}},
		"a start at the end": {child, http.MethodGet, at(cube), ranged("bytes=9-"), answer{Status: http.StatusRequestedRangeNotSatisfiable, Header: refusedWith()}},
		"a suffix range":     {child, http.MethodGet, at(cube), ranged("bytes=-3"), badRequest},
		"two ranges":         {child, http.MethodGet, at(cube), ranged("bytes=0-1,3-4"), badRequest},
		"a reversed range":   {child, http.MethodGet, at(cube), ranged("bytes=3-1"), badRequest},
		"another unit":       {child, http.MethodGet, at(cube), ranged("items=0-1"), badRequest},
		"a range of an empty file": {child, http.MethodGet, at(empty), ranged("bytes=4-"),
			answer{Status: http.StatusOK, Header: served("0")}},
		"a file over several reads": {child, http.MethodGet, at(big), nil,
			answer{Status: http.StatusOK, Header: served(strconv.Itoa(len(big))), Body: shown(big)}},
		"a listing": {child, http.MethodGet, "/fractal/objects/" + sdkfiles.EmptyListing.Text(), nil,
			answer{Status: http.StatusOK, Header: served(strconv.Itoa(len(listing))), Body: string(listing)}},
		"a kept copy": {child, http.MethodGet, at(kept), nil,
			answer{Status: http.StatusOK, Header: served("11"), Body: "a kept copy"}},
		"a query is ignored": {child, http.MethodGet, at(cube) + "?name=x", nil,
			answer{Status: http.StatusOK, Header: served("9"), Body: "GCDE cube"}},
		"own objects ignore no-cache": {child, http.MethodGet, at(cube), http.Header{"Cache-Control": {"no-cache"}},
			answer{Status: http.StatusOK, Header: served("9"), Body: "GCDE cube"}},
		"no proof":                      {nil, http.MethodGet, at(cube), nil, unauthorized},
		"a stale proof":                 {nil, http.MethodGet, at(cube), http.Header{files.ProofHeader: {stale}}, unauthorized},
		"not a child":                   {stranger, http.MethodGet, at(cube), nil, unauthorized},
		"the proof is checked first":    {nil, http.MethodGet, "/fractal/objects/sha256:abc", ranged("bytes=-1"), unauthorized},
		"a percent-encoded identity":    {child, http.MethodGet, "/fractal/objects/sha256%3A" + sdkfiles.Sum(cube).Text()[7:], nil, badRequest},
		"dot segments are not resolved": {child, http.MethodGet, "/fractal/objects/../objects/" + sdkfiles.Sum(cube).Text(), nil, badRequest},
		"a malformed identity":          {child, http.MethodGet, "/fractal/objects/sha256:abc", nil, badRequest},
		"outside the base":              {child, http.MethodGet, "/objects/" + sdkfiles.Sum(cube).Text(), nil, answer{Status: http.StatusNotFound, Header: refusedWith()}},
		"unknown with no parent":        {child, http.MethodGet, at([]byte("nobody shares this")), nil, answer{Status: http.StatusNotFound, Header: refusedWith()}},
		"another method": {child, http.MethodPost, at(cube), nil,
			answer{Status: http.StatusMethodNotAllowed, Header: refusedWith("Allow", "GET, HEAD")}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := askAs(t, srv.Client(), srv.URL, tc.signer, tc.method, tc.target, tc.header)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("the answer is %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestServerCannotReadAPublishedFile(t *testing.T) {
	t.Parallel()

	child, childID := newSigner(t)
	cube := []byte("GCDE cube")
	big := bytes.Repeat([]byte("x"), storageapi.MaxReadBytes+10)

	for name, tc := range map[string]struct {
		store     *chunkStore
		published files.Published
		data      []byte
		want      answer
	}{
		"the storage cannot read it": {
			&chunkStore{files: map[string][]byte{"library/cube.bgcode": cube}, failFrom: 1},
			files.Published{Storage: "library", Path: "cube.bgcode", Size: 9}, cube,
			answer{Status: http.StatusServiceUnavailable, Header: refusedWith("Retry-After", "seconds")},
		},
		"it changed after the scan": {
			&chunkStore{files: map[string][]byte{"library/cube.bgcode": cube}},
			files.Published{Storage: "library", Path: "cube.bgcode", Size: 10}, cube,
			answer{Status: http.StatusServiceUnavailable, Header: refusedWith("Retry-After", "seconds")},
		},
		"a read fails half way": {
			&chunkStore{files: map[string][]byte{"library/big.gcode": big}, failFrom: 2},
			files.Published{Storage: "library", Path: "big.gcode", Size: int64(len(big))}, big,
			answer{Status: http.StatusOK, Header: served(strconv.Itoa(len(big))),
				Body: shown(big[:storageapi.MaxReadBytes]), Cut: true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewTLSServer(files.NewServer(files.ServerConfig{
				Publisher: shelf{sdkfiles.Sum(tc.data): tc.published}, Storage: tc.store,
				Children: childOnly(childID), Tolerance: time.Minute,
			}).Handler())
			t.Cleanup(srv.Close)

			got := askAs(t, srv.Client(), srv.URL, child, http.MethodGet, "/objects/"+sdkfiles.Sum(tc.data).Text(), nil)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("the answer is %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestServerAfterUnsharing: a server with no parent answers 404 for a file that
// was never shared and for one whose folder was unshared (ADR-20261023-7r3 rule 22).
func TestServerAfterUnsharing(t *testing.T) {
	t.Parallel()

	child, childID := newSigner(t)
	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "shared", true)
	p.st.put("local", "private/b.gcode", "never shared", true)
	p.share(t, "local", "lib")
	p.scan(t)

	srv := httptest.NewTLSServer(files.NewServer(files.ServerConfig{
		Publisher: p.publisher, Storage: p.st, Children: childOnly(childID), Tolerance: time.Minute,
	}).Handler())
	t.Cleanup(srv.Close)

	statusOf := func(data string) int {
		return askAs(t, srv.Client(), srv.URL, child, http.MethodGet,
			"/objects/"+sdkfiles.Sum([]byte(data)).Text(), nil).Status
	}

	got := []int{statusOf("shared"), statusOf("never shared")}

	if err := p.shares.Unshare("local", "lib"); err != nil {
		t.Fatalf("unshare: %v", err)
	}

	got = append(got, statusOf("shared"))
	if want := []int{http.StatusOK, http.StatusNotFound, http.StatusNotFound}; !slices.Equal(got, want) {
		t.Errorf("the statuses are %v, want %v", got, want)
	}
}

// TestServerRefusesAFileChangedInPlace: a published file rewritten with the
// same length is answered 503 until a rescan publishes its new identity.
func TestServerRefusesAFileChangedInPlace(t *testing.T) {
	t.Parallel()

	child, childID := newSigner(t)
	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "old", true)
	p.share(t, "local", "lib")
	p.scan(t)

	srv := httptest.NewTLSServer(files.NewServer(files.ServerConfig{
		Publisher: p.publisher, Storage: p.st, Children: childOnly(childID), Tolerance: time.Minute,
	}).Handler())
	t.Cleanup(srv.Close)

	ask := func(data string) answer {
		return askAs(t, srv.Client(), srv.URL, child, http.MethodGet,
			"/objects/"+sdkfiles.Sum([]byte(data)).Text(), nil)
	}

	p.st.rewrite("local", "lib/a.gcode", "new")
	got := []answer{ask("old")}

	p.scan(t)
	got = append(got, ask("new"))

	want := []answer{
		{Status: http.StatusServiceUnavailable, Header: refusedWith("Retry-After", "seconds")},
		{Status: http.StatusOK, Header: served("3"), Body: "new"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the answers are %+v, want %+v", got, want)
	}
}

// releasing is a downward cache with one kept copy that reports when the
// server lets go of it.
type releasing struct {
	heldCopies

	released chan struct{}
}

func (c releasing) Open(id sdkfiles.Identity) (*os.File, int64, func(), bool) {
	opened, size, _, ok := c.heldCopies.Open(id)

	return opened, size, func() { close(c.released) }, ok
}

// A child that stops reading an answer loses its connection after the
// write timeout, so it holds neither a handler nor the copy it pinned.
func TestServerLetsGoOfAChildThatStopsReading(t *testing.T) {
	t.Parallel()

	child, childID := newSigner(t)
	// More than the small receive buffer below and the server's send buffer
	// hold, so the server has to wait for the child on any kernel.
	big := bytes.Repeat([]byte("GCDE "), 8<<20/5)
	copies := releasing{heldCopies: keepCopies(t, big), released: make(chan struct{})}

	srv := httptest.NewTLSServer(files.NewServer(files.ServerConfig{
		Publisher: shelf{}, Storage: &chunkStore{}, Cache: copies,
		Children: childOnly(childID), Tolerance: time.Minute, WriteTimeout: 300 * time.Millisecond,
	}).Handler())
	t.Cleanup(srv.Close)

	proof, err := files.SignProof(child, time.Now())
	if err != nil {
		t.Fatalf("sign a proof: %v", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	var dialer net.Dialer

	raw, err := dialer.DialContext(t.Context(), "tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// A small receive buffer, set before the handshake so the window never
	// grows, keeps the kernel from taking most of the answer for the child.
	if err = raw.(*net.TCPConn).SetReadBuffer(64 << 10); err != nil {
		t.Fatalf("shrink the receive buffer: %v", err)
	}

	conn := tls.Client(raw, &tls.Config{RootCAs: pool, ServerName: "127.0.0.1"})
	defer conn.Close()

	if err = conn.HandshakeContext(t.Context()); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	request := "GET /objects/" + sdkfiles.Sum(big).Text() + " HTTP/1.1\r\nHost: files\r\n" +
		files.ProofHeader + ": " + proof + "\r\n\r\n"
	if _, err = conn.Write([]byte(request)); err != nil {
		t.Fatalf("send the request: %v", err)
	}

	select {
	case <-copies.released:
	// net/http closes a connection whose write failed, and crypto/tls then
	// gives its close alert 5 seconds of its own before the handler returns.
	case <-time.After(10 * time.Second):
		t.Fatal("the server still holds the copy for a child that stopped reading")
	}
}

// deadlineRecorder is a ResponseWriter that records each write, each write
// deadline and each flush, in order, as http.ResponseController reaches them.
type deadlineRecorder struct {
	header http.Header
	events []string
}

func (d *deadlineRecorder) Header() http.Header { return d.header }

func (d *deadlineRecorder) WriteHeader(int) {}

func (d *deadlineRecorder) Write(p []byte) (int, error) {
	d.events = append(d.events, "write")

	return len(p), nil
}

func (d *deadlineRecorder) SetWriteDeadline(at time.Time) error {
	if at.IsZero() {
		d.events = append(d.events, "clear")
	} else {
		d.events = append(d.events, "deadline")
	}

	return nil
}

func (d *deadlineRecorder) FlushError() error {
	d.events = append(d.events, "flush")

	return nil
}

// TestServerFlushesBeforeItLiftsTheDeadline: the last bytes of an answer are
// flushed while a write deadline holds, so a child that stops reading at the
// end cannot hold the handler on that flush.
func TestServerFlushesBeforeItLiftsTheDeadline(t *testing.T) {
	t.Parallel()

	child, childID := newSigner(t)
	cube := []byte("GCDE cube")

	handler := files.NewServer(files.ServerConfig{
		Publisher: shelf{sdkfiles.Sum(cube): {Storage: "library", Path: "cube.bgcode", Size: int64(len(cube))}},
		Storage:   &chunkStore{files: map[string][]byte{"library/cube.bgcode": cube}},
		Children:  childOnly(childID), Tolerance: time.Minute, WriteTimeout: time.Minute,
	}).Handler()

	proof, err := files.SignProof(child, time.Now())
	if err != nil {
		t.Fatalf("sign a proof: %v", err)
	}

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/objects/"+sdkfiles.Sum(cube).Text(), nil)
	request.Header.Set(files.ProofHeader, proof)

	recorded := &deadlineRecorder{header: http.Header{}}
	handler.ServeHTTP(recorded, request)

	if want := []string{"deadline", "write", "deadline", "flush", "clear"}; !slices.Equal(recorded.events, want) {
		t.Errorf("the server did %v, want %v", recorded.events, want)
	}
}
