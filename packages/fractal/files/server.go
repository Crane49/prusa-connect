package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// Objects finds an object this server publishes. *Publisher implements it.
type Objects interface {
	Lookup(ctx context.Context, id sdkfiles.Identity) (Published, bool)
}

// Copies is the copies this server may serve its children. *Cache implements
// it. Open pins the copy it opens until unpin runs.
type Copies interface {
	Open(id sdkfiles.Identity) (copied *os.File, size int64, unpin func(), ok bool)
}

// ServerConfig is the input of NewServer.
type ServerConfig struct {
	// Publisher finds what this server publishes.
	Publisher Objects
	// Storage reads the bytes of a published file.
	Storage Storage
	// Cache holds the copies this server may serve. Nil holds none.
	Cache Copies
	// Children reports whether an identity is an admitted child that is not
	// revoked. Nil admits nobody.
	Children func(fractal.Identity) bool
	// BasePath is the path of files.base_url, such as "/fractal".
	BasePath string
	// Tolerance is files.proof_tolerance.
	Tolerance time.Duration
	// WriteTimeout bounds each write of an answer (files.relay_timeout). Zero
	// bounds nothing.
	WriteTimeout time.Duration
	// Log receives the lines of the server. Nil discards them.
	Log *slog.Logger
	// Parent returns the base URL of the parent's files entry, and false with
	// no parent or no entry.
	Parent func() (baseURL string, ok bool)
	// Client asks the parent.
	Client *Client
}

// retryLater is the Retry-After of a 502 or 503 this server answers for
// itself.
const retryLater = 30 * time.Second

// minutesLater is the Retry-After of a 503 a relay answers because its parent
// refused the relay's own request.
const minutesLater = 5 * time.Minute

// passedDown are the headers a relay passes down unchanged.
var passedDown = []string{"Content-Range", "Cache-Control", "Expires"}

// openEnd marks a range with no last byte.
const openEnd = -1

// errChanged means a published file no longer has the length the last scan
// saw, so its bytes may not hash to the identity.
var errChanged = errors.New("the file changed after the last scan")

// Server answers GET and HEAD {base}/objects/<identity>.
type Server struct {
	cfg    ServerConfig
	prefix string
	log    *slog.Logger
}

// NewServer builds a Server.
func NewServer(cfg ServerConfig) *Server {
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Server{cfg: cfg, prefix: strings.TrimSuffix(cfg.BasePath, "/") + "/objects/", log: log}
}

// Handler returns the server itself. A ServeMux would clean the path and
// redirect, and the identity must be read as it arrived.
func (s *Server) Handler() http.Handler { return s }

// ServeHTTP checks the proof, the child, the identity and the Range in that
// order, then answers.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.admitted(r) {
		refuse(w, http.StatusUnauthorized)

		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		refuse(w, http.StatusMethodNotAllowed)

		return
	}

	id, failure := s.identity(r.RequestURI)
	if failure != 0 {
		refuse(w, failure)

		return
	}

	asked, ok := parseRange(r.Header)
	if !ok {
		refuse(w, http.StatusBadRequest)

		return
	}

	if published, found := s.cfg.Publisher.Lookup(r.Context(), id); found {
		s.servePublished(w, r, published, asked)

		return
	}

	fresh := wantsFresh(r.Header)
	if !fresh && s.serveCopy(w, r, id, asked) {
		return
	}

	s.relay(w, r, id, fresh)
}

// admitted reports whether the request carries a valid proof of an admitted
// child.
func (s *Server) admitted(r *http.Request) bool {
	identity, err := VerifyProof(r.Header.Get(ProofHeader), time.Now(), s.cfg.Tolerance)
	if err != nil {
		s.log.DebugContext(r.Context(), "a request to /objects/ carried no valid proof", "error", err.Error())

		return false
	}

	return s.cfg.Children != nil && s.cfg.Children(identity)
}

// identity reads the identity from the request target as it arrived, with no
// percent-decoding and no dot-segment resolution. The query is ignored.
func (s *Server) identity(target string) (sdkfiles.Identity, int) {
	path, _, _ := strings.Cut(target, "?")

	text, ok := strings.CutPrefix(path, s.prefix)
	if !ok {
		return sdkfiles.Identity{}, http.StatusNotFound
	}

	id, err := sdkfiles.ParseText(text)
	if err != nil {
		return sdkfiles.Identity{}, http.StatusBadRequest
	}

	return id, 0
}

// servePublished answers an object this server publishes, with a retention
// of unlimited.
func (s *Server) servePublished(w http.ResponseWriter, r *http.Request, published Published, asked byteRange) {
	if published.Listing != nil {
		s.serveFrom(w, r, bytes.NewReader(published.Listing), int64(len(published.Listing)), asked)

		return
	}

	start, end, status := asked.window(published.Size)
	if status == http.StatusRequestedRangeNotSatisfiable {
		refuse(w, status)

		return
	}

	file := storedFile{storage: s.cfg.Storage, published: published}

	err := file.unchanged(r.Context())

	var chunk []byte
	if err == nil {
		chunk, err = file.read(r.Context(), start, end)
	}

	if err != nil {
		s.log.WarnContext(r.Context(), "a published file cannot be read now",
			"storage", published.Storage, "path", published.Path, "error", err.Error())
		refuse(w, http.StatusServiceUnavailable)

		return
	}

	writeHead(w, status, start, end, published.Size)

	if r.Method == http.MethodHead {
		return
	}

	body, unbound := s.bounded(w)
	defer unbound()

	for offset := start; ; {
		if _, err = body.Write(chunk); err != nil {
			return
		}

		offset += int64(len(chunk))
		if offset >= end {
			return
		}

		if chunk, err = file.read(r.Context(), offset, end); err != nil {
			s.log.WarnContext(r.Context(), "a published file could not be read to its end",
				"storage", published.Storage, "path", published.Path, "error", err.Error())
			cutShort(w)
		}
	}
}

// serveCopy answers from a kept copy, and reports false when there is none.
func (s *Server) serveCopy(w http.ResponseWriter, r *http.Request, id sdkfiles.Identity, asked byteRange) bool {
	if s.cfg.Cache == nil {
		return false
	}

	copied, size, unpin, ok := s.cfg.Cache.Open(id)
	if !ok {
		return false
	}
	defer unpin()
	defer copied.Close()

	s.serveFrom(w, r, copied, size, asked)

	return true
}

// relay asks the parent with a proof of its own and passes the answer down as
// it arrives. With no parent it answers 404.
func (s *Server) relay(w http.ResponseWriter, r *http.Request, id sdkfiles.Identity, fresh bool) {
	base, ok := s.parent()
	if !ok {
		refuse(w, http.StatusNotFound)

		return
	}

	answer, err := s.cfg.Client.Get(r.Context(), base, id, GetOptions{
		Range: r.Header.Get("Range"), NoCache: fresh, Head: r.Method == http.MethodHead,
	})
	if err != nil {
		s.log.WarnContext(r.Context(), "the parent could not be reached for an object",
			"identity", id.Text(), "error", err.Error())
		refuse(w, http.StatusBadGateway)

		return
	}
	defer answer.Body.Close()

	switch answer.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
		s.passDown(w, r, id, answer)
	case http.StatusBadRequest, http.StatusUnauthorized:
		s.log.WarnContext(r.Context(), "the parent refused this server's own request",
			"identity", id.Text(), "status", answer.StatusCode)
		w.Header().Set("Retry-After", time.Now().Add(minutesLater).UTC().Format(http.TimeFormat))
		refuse(w, http.StatusServiceUnavailable)
	case http.StatusNotFound, http.StatusRequestedRangeNotSatisfiable:
		s.notRelayed(r, id, answer)
		refuse(w, answer.StatusCode)
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		s.notRelayed(r, id, answer)
		w.Header().Set("Retry-After", answer.Header.Get("Retry-After"))
		refuse(w, answer.StatusCode)
	default:
		s.notRelayed(r, id, answer)
		refuse(w, http.StatusBadGateway)
	}
}

func (s *Server) notRelayed(r *http.Request, id sdkfiles.Identity, answer *http.Response) {
	s.log.InfoContext(r.Context(), "an object was not relayed from the parent",
		"identity", id.Text(), "status", answer.StatusCode)
}

// parent returns the parent's base URL when this server may relay.
func (s *Server) parent() (string, bool) {
	if s.cfg.Parent == nil || s.cfg.Client == nil {
		return "", false
	}

	return s.cfg.Parent()
}

// passDown streams a 200 or 206 of the parent to the caller with its
// Content-Range and retention directives unchanged.
func (s *Server) passDown(w http.ResponseWriter, r *http.Request, id sdkfiles.Identity, answer *http.Response) {
	if answer.StatusCode == http.StatusPartialContent && answer.Header.Get("Content-Range") == "" {
		refuse(w, http.StatusBadGateway)

		return
	}

	h := w.Header()
	for _, name := range passedDown {
		if values := answer.Header.Values(name); len(values) > 0 {
			h[name] = values
		}
	}

	h.Set("Content-Type", "application/octet-stream")
	h.Set("Accept-Ranges", "bytes")

	if answer.ContentLength >= 0 {
		h.Set("Content-Length", strconv.FormatInt(answer.ContentLength, 10))
	}

	w.WriteHeader(answer.StatusCode)

	if r.Method == http.MethodHead {
		return
	}

	body, unbound := s.bounded(w)
	defer unbound()

	sent, err := io.Copy(body, answer.Body)
	if err != nil {
		s.log.WarnContext(r.Context(), "a relayed object was cut short", "error", err.Error())
		cutShort(w)

		return
	}

	s.log.InfoContext(r.Context(), "an object was relayed from the parent",
		"identity", id.Text(), "status", answer.StatusCode, "bytes", sent)
}

// serveFrom answers from bytes this server has, with a retention of
// unlimited.
func (s *Server) serveFrom(w http.ResponseWriter, r *http.Request, from io.ReaderAt, size int64, asked byteRange) {
	start, end, status := asked.window(size)
	if status == http.StatusRequestedRangeNotSatisfiable {
		refuse(w, status)

		return
	}

	writeHead(w, status, start, end, size)

	if r.Method == http.MethodHead {
		return
	}

	body, unbound := s.bounded(w)
	defer unbound()

	if _, err := io.Copy(body, io.NewSectionReader(from, start, end-start)); err != nil {
		s.log.WarnContext(r.Context(), "an object could not be sent to its end", "error", err.Error())
		cutShort(w)
	}
}

// bounded returns w with each write bound to WriteTimeout, and a function
// that flushes under one more deadline and then lifts the bound. net/http
// would flush the last bytes after the handler with no deadline.
func (s *Server) bounded(w http.ResponseWriter) (io.Writer, func()) {
	if s.cfg.WriteTimeout <= 0 {
		return w, func() {}
	}

	controller := http.NewResponseController(w)

	return deadlineWriter{to: w, controller: controller, timeout: s.cfg.WriteTimeout},
		func() {
			// The answer is complete; a flush that fails only loses a closed connection.
			_ = controller.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
			_ = controller.Flush()
			_ = controller.SetWriteDeadline(time.Time{})
		}
}

// deadlineWriter sets a new write deadline before every write.
type deadlineWriter struct {
	to         io.Writer
	controller *http.ResponseController
	timeout    time.Duration
}

func (d deadlineWriter) Write(p []byte) (int, error) {
	_ = d.controller.SetWriteDeadline(time.Now().Add(d.timeout)) // a writer without deadlines still writes

	return d.to.Write(p) //nolint:wrapcheck // the error of the connection, which the caller reads as a cut
}

// writeHead writes the status and the headers of an answer with bytes.
func writeHead(w http.ResponseWriter, status int, start, end, size int64) {
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Accept-Ranges", "bytes")
	h.Set("Content-Length", strconv.FormatInt(end-start, 10))

	if status == http.StatusPartialContent {
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, size))
	}

	unlimited.set(h)
	w.WriteHeader(status)
}

// refuse answers a failure. Every failure has no-store, a 401 names the
// scheme, and a 502 or 503 has a Retry-After date.
func refuse(w http.ResponseWriter, status int) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")

	switch status {
	case http.StatusUnauthorized:
		h.Set("WWW-Authenticate", "Fractal-Proof")
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		if h.Get("Retry-After") == "" {
			h.Set("Retry-After", time.Now().Add(retryLater).UTC().Format(http.TimeFormat))
		}
	}

	w.WriteHeader(status)
}

// cutShort sends what was written and ends the response before the length it
// announced, so the caller sees it was cut.
func cutShort(w http.ResponseWriter) {
	_ = http.NewResponseController(w).Flush() // the abort below cuts the answer either way

	panic(http.ErrAbortHandler)
}

// wantsFresh reports whether the caller sent Cache-Control: no-cache.
func wantsFresh(h http.Header) bool {
	for _, value := range h.Values("Cache-Control") {
		for part := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "no-cache") {
				return true
			}
		}
	}

	return false
}

// byteRange is the one Range the HTTP binding allows. last is openEnd for
// bytes=a-.
type byteRange struct {
	first, last int64
	asked       bool
}

// parseRange reads bytes=a-b or bytes=a-, and refuses every other form.
func parseRange(h http.Header) (byteRange, bool) {
	values := h.Values("Range")

	switch len(values) {
	case 0:
		return byteRange{}, true
	case 1:
	default:
		return byteRange{}, false
	}

	spec, ok := strings.CutPrefix(values[0], "bytes=")
	if !ok {
		return byteRange{}, false
	}

	firstText, lastText, ok := strings.Cut(spec, "-")
	if !ok {
		return byteRange{}, false
	}

	first, ok := digits(firstText)
	if !ok {
		return byteRange{}, false
	}

	if lastText == "" {
		return byteRange{first: first, last: openEnd, asked: true}, true
	}

	last, ok := digits(lastText)
	if !ok || last < first {
		return byteRange{}, false
	}

	return byteRange{first: first, last: last, asked: true}, true
}

// digits reads a non-empty run of ASCII digits.
func digits(text string) (int64, bool) {
	if text == "" || strings.Trim(text, "0123456789") != "" {
		return 0, false
	}

	n, err := strconv.ParseInt(text, 10, 64)

	return n, err == nil
}

// window returns the bytes to send of an object of size bytes, and 200, 206
// or 416.
func (b byteRange) window(size int64) (start, end int64, status int) {
	switch {
	case !b.asked || size == 0:
		return 0, size, http.StatusOK
	case b.first >= size:
		return 0, 0, http.StatusRequestedRangeNotSatisfiable
	case b.last == openEnd || b.last >= size:
		return b.first, size, http.StatusPartialContent
	}

	return b.first, b.last + 1, http.StatusPartialContent
}

// storedFile reads a published file through the storage plane.
type storedFile struct {
	storage   Storage
	published Published
}

// unchanged returns errChanged when the length or the time of change differs
// from the last scan. The identity of such content has not been computed.
func (f storedFile) unchanged(ctx context.Context) error {
	info, err := f.storage.Stat(ctx, storageapi.StatParams{Storage: f.published.Storage, Path: f.published.Path})
	if err != nil {
		return fmt.Errorf("stat %s in %s: %w", f.published.Path, f.published.Storage, err)
	}

	if info.Size != f.published.Size || info.MTime != f.published.MTime {
		return errChanged
	}

	return nil
}

// read returns the next bytes from offset, at most one storage read and never
// past end. A file whose length is not the scanned one is errChanged.
func (f storedFile) read(ctx context.Context, offset, end int64) ([]byte, error) {
	length := min(end-offset, storageapi.MaxReadBytes)

	chunk, err := f.storage.Read(ctx, storageapi.ReadParams{
		Storage: f.published.Storage,
		Path:    f.published.Path,
		Offset:  offset,
		Length:  int(max(length, 1)),
	})
	if err != nil {
		return nil, fmt.Errorf("read %s in %s: %w", f.published.Path, f.published.Storage, err)
	}

	if chunk.Size != f.published.Size || (length > 0 && len(chunk.Data) == 0) {
		return nil, errChanged
	}

	return chunk.Data[:min(int64(len(chunk.Data)), length)], nil
}
