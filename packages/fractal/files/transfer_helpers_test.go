package files_test

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/files"
	"github.com/prusa3d/fractal-link/natsbind"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// newSigner creates an identity key and returns it with its public half.
func newSigner(t *testing.T) (joinwire.Signer, fractal.Identity) {
	t.Helper()

	signer, _, err := natsbind.GenerateSigner()
	if err != nil {
		t.Fatalf("create an identity: %v", err)
	}

	public, err := signer.PublicKey()
	if err != nil {
		t.Fatalf("read the identity: %v", err)
	}

	return signer, fractal.Identity(public)
}

// unlimitedDirectives is the one spelling of a retention of unlimited.
const unlimitedDirectives = "private, max-age=31536000, immutable"

// shelf is what a fake Publisher publishes.
type shelf map[sdkfiles.Identity]files.Published

func (s shelf) Lookup(_ context.Context, id sdkfiles.Identity) (files.Published, bool) {
	held, ok := s[id]

	return held, ok
}

// chunkStore is a fake storage plane. A read numbered failFrom or later fails,
// counting from one; zero never fails.
type chunkStore struct {
	mu       sync.Mutex
	files    map[string][]byte
	failFrom int
	reads    int
}

func (s *chunkStore) Read(_ context.Context, p storageapi.ReadParams) (storageapi.Chunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reads++

	data, ok := s.files[p.Storage+"/"+p.Path]
	if !ok || (s.failFrom > 0 && s.reads >= s.failFrom) {
		return storageapi.Chunk{}, storageapi.ErrUnreadable
	}

	length := int64(p.Length)
	if length == 0 {
		length = storageapi.DefaultReadBytes
	}

	size := int64(len(data))
	start := min(p.Offset, size)
	end := min(start+length, size)

	return storageapi.Chunk{Data: data[start:end], Size: size, EOF: end == size}, nil
}

func (s *chunkStore) Stat(_ context.Context, p storageapi.StatParams) (storageapi.FileInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, ok := s.files[p.Storage+"/"+p.Path]
	if !ok {
		return storageapi.FileInfo{}, storageapi.ErrNoFile
	}

	return storageapi.FileInfo{Path: p.Path, Size: int64(len(data))}, nil
}

func (s *chunkStore) List(context.Context, storageapi.ListParams) (storageapi.Listing, error) {
	return storageapi.Listing{}, storageapi.ErrUnreadable
}

// heldCopies is a fake downward cache: the file of each kept copy.
type heldCopies map[sdkfiles.Identity]string

func keepCopies(t *testing.T, contents ...[]byte) heldCopies {
	t.Helper()

	held := heldCopies{}

	for i, data := range contents {
		path := filepath.Join(t.TempDir(), strconv.Itoa(i))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write a copy: %v", err)
		}

		held[sdkfiles.Sum(data)] = path
	}

	return held
}

func (c heldCopies) Open(id sdkfiles.Identity) (*os.File, int64, func(), bool) {
	path, ok := c[id]
	if !ok {
		return nil, 0, nil, false
	}

	opened, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, false
	}

	info, err := opened.Stat()
	if err != nil {
		_ = opened.Close()

		return nil, 0, nil, false
	}

	return opened, info.Size(), func() {}, true
}

// childOnly admits one identity as a child.
func childOnly(child fractal.Identity) func(fractal.Identity) bool {
	return func(asked fractal.Identity) bool { return asked == child }
}

// answer is what a caller of /objects/ sees: the status, the headers a test
// cares about, the body, and whether the body was cut short.
type answer struct {
	Status int
	Header map[string]string
	Body   string
	Cut    bool
}

var watched = []string{
	"Content-Type", "Accept-Ranges", "Content-Length", "Content-Range", "Cache-Control",
	"Expires", "Retry-After", "WWW-Authenticate", "Allow",
}

func answerOf(resp *http.Response) answer {
	body, err := io.ReadAll(resp.Body)

	header := map[string]string{}

	for _, name := range watched {
		if value := resp.Header.Get(name); value != "" {
			header[name] = value
		}
	}

	for name := range resp.Header {
		if strings.HasPrefix(name, "Fractal-") {
			header[name] = resp.Header.Get(name)
		}
	}

	if value, ok := header["Retry-After"]; ok {
		header["Retry-After"] = retryClass(value)
	}

	return answer{Status: resp.StatusCode, Header: header, Body: shown(body), Cut: errors.Is(err, io.ErrUnexpectedEOF)}
}

// shown keeps a short body readable and names a long one by its identity.
func shown(body []byte) string {
	if len(body) <= 64 {
		return string(body)
	}

	return "bytes of " + sdkfiles.Sum(body).Text()
}

// retryClass turns a Retry-After into something a table can compare: a date
// more than ten minutes ahead stays as written, a nearer one is "minutes" or
// "seconds".
func retryClass(value string) string {
	at, err := http.ParseTime(value)
	if err != nil {
		return "not a date: " + value
	}

	switch wait := time.Until(at); {
	case wait > 10*time.Minute:
		return value
	case wait >= time.Minute:
		return "minutes"
	default:
		return "seconds"
	}
}

// served is the header set of an answer with bytes and a retention of
// unlimited, with any extra pairs.
func served(length string, pairs ...string) map[string]string {
	header := map[string]string{
		"Content-Type": "application/octet-stream", "Accept-Ranges": "bytes",
		"Content-Length": length, "Cache-Control": unlimitedDirectives,
	}

	for i := 0; i+1 < len(pairs); i += 2 {
		header[pairs[i]] = pairs[i+1]
	}

	return header
}

// refusedWith is the header set of a failure, with any extra pairs.
func refusedWith(pairs ...string) map[string]string {
	header := map[string]string{"Cache-Control": "no-store", "Content-Length": "0"}

	for i := 0; i+1 < len(pairs); i += 2 {
		header[pairs[i]] = pairs[i+1]
	}

	return header
}

// askAs sends one request with the target written exactly as given. A signer
// adds a fresh proof unless header already carries one.
func askAs(
	t *testing.T, client *http.Client, base string, signer joinwire.Signer,
	method, target string, header http.Header,
) answer {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, base, nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}

	req.URL.Opaque = target

	maps.Copy(req.Header, header)

	if signer != nil && req.Header.Get(files.ProofHeader) == "" {
		proof, signErr := files.SignProof(signer, time.Now())
		if signErr != nil {
			t.Fatalf("sign a proof: %v", signErr)
		}

		req.Header.Set(files.ProofHeader, proof)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send %s %s: %v", method, target, err)
	}
	defer resp.Body.Close()

	return answerOf(resp)
}

// seen is what a parent saw of one request.
type seen struct {
	Method       string
	Range        string
	CacheControl string
	Caller       fractal.Identity
}

// scripted is a parent that answers each request with the next step and
// repeats the last one.
type scripted struct {
	srv   *httptest.Server
	mu    sync.Mutex
	steps []http.HandlerFunc
	asked []seen
}

func newScripted(t *testing.T, steps ...http.HandlerFunc) *scripted {
	t.Helper()

	parent := &scripted{steps: steps}
	parent.srv = httptest.NewTLSServer(parent)
	t.Cleanup(parent.srv.Close)

	return parent
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	caller, _ := files.VerifyProof(r.Header.Get(files.ProofHeader), time.Now(), time.Minute)

	s.mu.Lock()
	s.asked = append(
		s.asked,
		seen{
			Method:       r.Method,
			Range:        r.Header.Get("Range"),
			CacheControl: r.Header.Get("Cache-Control"),
			Caller:       caller,
		},
	)
	step := s.steps[min(len(s.asked), len(s.steps))-1]
	s.mu.Unlock()

	step(w, r)
}

func (s *scripted) requests() []seen {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]seen(nil), s.asked...)
}

func (s *scripted) anchors() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(s.srv.Certificate())

	return pool
}

// keptFor is the directives of a retention of unlimited.
func keptFor() http.Header { return http.Header{"Cache-Control": {unlimitedDirectives}} }

// whole answers 200 with data.
func whole(data []byte, directives http.Header) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		maps.Copy(w.Header(), directives)

		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}
}

// part answers 206 with data from the byte at from.
func part(data []byte, from int, directives http.Header) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		maps.Copy(w.Header(), directives)

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)-from))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[from:])
	}
}

// trickle answers what it was asked for, 200 or 206, but sends only n bytes
// and then cuts the response.
func trickle(data []byte, n int, directives http.Header) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		maps.Copy(w.Header(), directives)

		from := 0
		if spec, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
			from, _ = strconv.Atoi(strings.TrimSuffix(spec, "-"))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
			w.Header().Set("Content-Length", strconv.Itoa(len(data)-from))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		}

		_, _ = w.Write(data[from:min(from+n, len(data))])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}
}

// stall answers 200 with the first n bytes of data and then sends nothing
// until the caller goes.
func stall(data []byte, n int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data[:n])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
}

// status answers code with the given headers and no body.
func status(code int, header http.Header) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		maps.Copy(w.Header(), header)

		w.WriteHeader(code)
	}
}

func fractalPath(path string) fractal.NodePath { return fractal.NodePath(path) }
