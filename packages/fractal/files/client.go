package files

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// Response is a parent's answer as it arrived. The caller closes its Body.
type Response = http.Response

// ClientConfig is the input of NewClient.
type ClientConfig struct {
	// Signer signs the proof of every request with this node's identity.
	Signer joinwire.Signer
	// Anchors are the trust anchors of the parent's certificate. Nil means
	// the system's.
	Anchors *x509.CertPool
	// AllowCleartext lets the client dial an http base URL
	// (files.allow_cleartext_parent).
	AllowCleartext bool
	// MaxResumes bounds the resumes of one transfer (files.max_resumes).
	MaxResumes int
	// Timeout bounds the dial, the handshake, each wait for the answer and
	// the life of an idle connection (files.relay_timeout).
	Timeout time.Duration
	// Rejoin runs the join exchange with the parent once. The client calls
	// it after a 401, at most once per Tolerance. Nil means never.
	Rejoin func(ctx context.Context) error
	// Tolerance is files.proof_tolerance.
	Tolerance time.Duration
	// Log receives what the client sees of a 401. Nil means no log.
	Log *slog.Logger
}

// GetOptions are what a request to the parent may carry beside the proof.
type GetOptions struct {
	// Range is passed unchanged.
	Range string
	// NoCache asks every server up the chain to skip its copies.
	NoCache bool
	// Head asks for the headers alone, as a relayed HEAD does.
	Head bool
}

// ErrCleartext means the parent's base URL is http and
// files.allow_cleartext_parent is false.
var ErrCleartext = errors.New("the parent's base URL is http and a cleartext parent is not allowed")

// Client asks this node's parent for objects.
type Client struct {
	cfg  ClientConfig
	http *http.Client

	// rejoinMu guards the exchange that runs and the end and outcome of the
	// last one that ran to its end.
	rejoinMu  sync.Mutex
	joining   chan struct{}
	rejoined  time.Time
	rejoinErr error
}

// NewClient builds a Client. It checks the parent's certificate and host,
// asks for no content coding and follows no redirect.
func NewClient(cfg ClientConfig) *Client {
	dialer := &net.Dialer{Timeout: cfg.Timeout}

	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}

	return &Client{cfg: cfg, http: &http.Client{
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSClientConfig:       natsbind.ClientTLS(cfg.Anchors, ""),
			TLSHandshakeTimeout:   cfg.Timeout,
			ResponseHeaderTimeout: cfg.Timeout,
			IdleConnTimeout:       cfg.Timeout,
			DisableCompression:    true,
			ForceAttemptHTTP2:     true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// CloseIdleConnections closes the connections to the parent that wait for a
// request.
func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

// Get sends one request for id with a new proof and returns the answer as it
// arrived. After a 401 it runs the join exchange again and asks once more.
// It joins at most once per files.proof_tolerance, and returns the 401
// otherwise.
func (c *Client) Get(ctx context.Context, baseURL string, id sdkfiles.Identity, opts GetOptions) (*Response, error) {
	sentAt := time.Now()

	answer, err := c.send(ctx, baseURL, id, opts)
	if err != nil || answer.StatusCode != http.StatusUnauthorized {
		return answer, err
	}

	c.checkClock(ctx, answer.Header)

	if !c.rejoin(ctx, sentAt) {
		return answer, nil
	}

	_ = answer.Body.Close() // a 401 has no body this client reads

	return c.send(ctx, baseURL, id, opts)
}

// checkClock logs when the Date of an answer is further from this server's
// clock than files.proof_tolerance, the likely cause of a 401.
func (c *Client) checkClock(ctx context.Context, h http.Header) {
	parent, err := http.ParseTime(h.Get("Date"))
	if err != nil {
		return
	}

	now := time.Now()
	if (now.Sub(parent)).Abs() > c.cfg.Tolerance {
		c.cfg.Log.WarnContext(ctx, "the clock of this server differs from the clock of the parent by more than "+
			"files.proof_tolerance", "local", now.UTC().Format(http.TimeFormat), "parent", h.Get("Date"))
	}
}

// rejoin reports whether a join exchange was granted after sentAt. It waits
// for one that is running, and starts one only when none ended within the
// last files.proof_tolerance. A cancelled exchange does not count.
func (c *Client) rejoin(ctx context.Context, sentAt time.Time) bool {
	if c.cfg.Rejoin == nil {
		return false
	}

	for {
		c.rejoinMu.Lock()

		if c.rejoined.After(sentAt) {
			granted := c.rejoinErr == nil
			c.rejoinMu.Unlock()

			return granted
		}

		if running := c.joining; running != nil {
			c.rejoinMu.Unlock()

			select {
			case <-running:
				continue
			case <-ctx.Done():
				return false
			}
		}

		if !c.rejoined.IsZero() && time.Since(c.rejoined) < c.cfg.Tolerance {
			c.rejoinMu.Unlock()

			return false
		}

		done := make(chan struct{})
		c.joining = done
		c.rejoinMu.Unlock()

		err := c.runRejoin(ctx)

		c.rejoinMu.Lock()
		c.joining = nil

		if ctx.Err() == nil {
			c.rejoined, c.rejoinErr = time.Now(), err
		}

		c.rejoinMu.Unlock()
		close(done)

		return err == nil
	}
}

// runRejoin runs the join exchange once, within files.relay_timeout, and
// logs how it ended.
func (c *Client) runRejoin(ctx context.Context) error {
	if c.cfg.Timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, c.cfg.Timeout)
		defer cancel()
	}

	if err := c.cfg.Rejoin(ctx); err != nil {
		c.cfg.Log.WarnContext(ctx, "this server could not run the join exchange again after a 401 from the parent",
			"error", err.Error())

		return err
	}

	c.cfg.Log.InfoContext(ctx, "this server ran the join exchange again after a 401 from the parent")

	return nil
}

// send sends one request for id with a new proof.
func (c *Client) send(ctx context.Context, baseURL string, id sdkfiles.Identity, opts GetOptions) (*Response, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("read the parent's base URL: %w", err)
	}

	if base.Scheme == "http" && !c.cfg.AllowCleartext {
		return nil, ErrCleartext
	}

	proof, err := SignProof(c.cfg.Signer, time.Now())
	if err != nil {
		return nil, err
	}

	target := strings.TrimSuffix(baseURL, "/") + "/objects/" + id.Text()

	method := http.MethodGet
	if opts.Head {
		method = http.MethodHead
	}

	ctx, cancel := context.WithCancel(ctx)

	//nolint:gosec // G704: the URL is the parent's own files entry, not a caller's input.
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		cancel()

		return nil, fmt.Errorf("build the request to the parent: %w", err)
	}

	req.Header.Set(ProofHeader, proof)

	if opts.Range != "" {
		req.Header.Set("Range", opts.Range)
	}

	if opts.NoCache {
		req.Header.Set("Cache-Control", "no-cache")
	}

	answer, err := c.http.Do(req) //nolint:gosec,bodyclose // G704: the parent's own entry; the caller closes the body
	if err != nil {
		cancel()

		return nil, fmt.Errorf("ask the parent: %w", err)
	}

	bound := &stallBound{ReadCloser: answer.Body, timeout: c.cfg.Timeout, cancel: cancel}
	if c.cfg.Timeout > 0 {
		bound.timer = time.AfterFunc(c.cfg.Timeout, cancel)
		bound.timer.Stop()
	}

	answer.Body = bound

	return answer, nil
}

// stallBound ends the request when one read of its body waits longer than
// timeout. The time between two reads is not bounded. A nil timer bounds
// nothing.
type stallBound struct {
	io.ReadCloser

	timer   *time.Timer
	timeout time.Duration
	cancel  context.CancelFunc
}

func (b *stallBound) Read(p []byte) (int, error) {
	if b.timer != nil {
		b.timer.Reset(b.timeout)
		defer b.timer.Stop()
	}

	n, err := b.ReadCloser.Read(p)

	return n, err //nolint:wrapcheck // a body passes its own errors, io.EOF among them
}

func (b *stallBound) Close() error {
	if b.timer != nil {
		b.timer.Stop()
	}

	b.cancel()

	return b.ReadCloser.Close() //nolint:wrapcheck // the body's own error
}

// The causes of a FetchError.
var (
	// ErrNotAvailable means the parent answered 404.
	ErrNotAvailable = errors.New("the parent says the object is not available")
	// ErrMismatch means the bytes do not hash to the identity.
	ErrMismatch = errors.New("the bytes do not hash to the identity")
	// ErrResumesSpent means the transfer was cut more often than
	// files.max_resumes allows. It may be tried again later.
	ErrResumesSpent = errors.New("the transfer was cut more often than files.max_resumes allows")
	// ErrUnusablePart means a 206 did not start where the client asked or
	// did not end within the object.
	ErrUnusablePart = errors.New("the parent answered a part the client did not ask for")
	// ErrRefused means the parent answered another failure status.
	ErrRefused = errors.New("the parent refused the request")
)

// Sink is where Fetch writes. A 200 to a request with a Range, and the second
// try after a mismatch, start it again from its first byte.
type Sink interface {
	io.Writer
	io.Seeker
	Truncate(size int64) error
}

// FetchError is a fetch that did not end with the object. Status is the
// parent's status, or zero when no status says why.
type FetchError struct {
	Status     int
	RetryAfter time.Time
	Err        error
}

func (e *FetchError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("status %d: %v", e.Status, e.Err)
	}

	return e.Err.Error()
}

func (e *FetchError) Unwrap() error { return e.Err }

// Retryable reports whether the caller may ask again.
func (e *FetchError) Retryable() bool {
	if errors.Is(e.Err, ErrCleartext) {
		return false
	}

	switch e.Status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound,
		http.StatusRequestedRangeNotSatisfiable:
		return false
	}

	return true
}

// Fetch assembles the object id from the parent into w and returns the most
// restrictive retention of its answers. A copy that does not hash to id is
// dropped, and the client asks once more with no-cache. fresh sends no-cache
// from the first request.
func (c *Client) Fetch(
	ctx context.Context,
	baseURL string,
	id sdkfiles.Identity,
	w Sink,
	fresh bool,
) (Retention, error) {
	kept, err := c.assemble(ctx, baseURL, id, w, fresh)
	if !fresh && errors.Is(err, ErrMismatch) {
		kept, err = c.assemble(ctx, baseURL, id, w, true)
	}

	return kept, err
}

// transfer is one object being assembled.
type transfer struct {
	sink  Sink
	hash  *sdkfiles.Hasher
	held  int64
	total int64
	kept  Retention
}

// restart drops every byte held.
func (t *transfer) restart() error {
	if err := t.sink.Truncate(0); err != nil {
		return fmt.Errorf("drop the bytes held: %w", err)
	}

	if _, err := t.sink.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("drop the bytes held: %w", err)
	}

	t.hash, t.held, t.total, t.kept = sdkfiles.NewHasher(), 0, -1, unlimited

	return nil
}

func (c *Client) assemble(
	ctx context.Context,
	baseURL string,
	id sdkfiles.Identity,
	w Sink,
	fresh bool,
) (Retention, error) {
	got := &transfer{sink: w}
	if err := got.restart(); err != nil {
		return Retention{}, err
	}

	for resumes := 0; ; resumes++ {
		done, err := c.step(ctx, baseURL, id, got, fresh)
		if err != nil {
			return Retention{}, err
		}

		if done {
			break
		}

		if resumes == c.cfg.MaxResumes {
			return Retention{}, &FetchError{Err: ErrResumesSpent}
		}
	}

	if got.hash.Identity() != id {
		if err := got.restart(); err != nil {
			return Retention{}, err
		}

		return Retention{}, &FetchError{Err: ErrMismatch}
	}

	return got.kept, nil
}

// step asks once from the first byte not held and appends what arrives. It
// reports true when the object is whole.
func (c *Client) step(
	ctx context.Context,
	baseURL string,
	id sdkfiles.Identity,
	t *transfer,
	fresh bool,
) (bool, error) {
	opts := GetOptions{NoCache: fresh}
	if t.held > 0 {
		opts.Range = "bytes=" + strconv.FormatInt(t.held, 10) + "-"
	}

	answer, err := c.Get(ctx, baseURL, id, opts)
	if err != nil {
		return false, &FetchError{Err: err}
	}
	defer answer.Body.Close()

	switch answer.StatusCode {
	case http.StatusOK:
		if t.held > 0 {
			if err = t.restart(); err != nil {
				return false, err
			}
		}

		t.total = answer.ContentLength
		t.kept = retentionOf(answer.Header)
	case http.StatusPartialContent:
		if err = t.takePart(answer.Header); err != nil {
			return false, err
		}
	default:
		return false, statusError(answer)
	}

	sink := &sinkWriter{to: t.sink}
	n, err := io.Copy(io.MultiWriter(sink, t.hash), answer.Body)
	t.held += n

	switch {
	case sink.err != nil:
		return false, fmt.Errorf("write the object: %w", sink.err)
	case err != nil:
		return false, nil
	case t.total >= 0 && t.held > t.total:
		return false, &FetchError{Err: ErrUnusablePart}
	}

	return t.total < 0 || t.held == t.total, nil
}

// takePart accepts a 206 only when it starts at the first byte not held and
// ends within the object.
func (t *transfer) takePart(h http.Header) error {
	first, last, size, ok := contentRange(h.Get("Content-Range"))
	if !ok || first != t.held || last >= size || (t.total >= 0 && size != t.total) {
		return &FetchError{Err: ErrUnusablePart}
	}

	t.total = size
	t.kept = t.kept.restrict(retentionOf(h))

	return nil
}

// contentRange reads "bytes a-b/n".
func contentRange(value string) (first, last, size int64, ok bool) {
	spec, ok := strings.CutPrefix(value, "bytes ")
	if !ok {
		return 0, 0, 0, false
	}

	span, total, ok := strings.Cut(spec, "/")
	if !ok {
		return 0, 0, 0, false
	}

	firstText, lastText, ok := strings.Cut(span, "-")
	if !ok {
		return 0, 0, 0, false
	}

	first, okFirst := digits(firstText)
	last, okLast := digits(lastText)
	size, okSize := digits(total)

	return first, last, size, okFirst && okLast && okSize && first <= last
}

// statusError is the FetchError of a failure status, with its Retry-After.
func statusError(answer *http.Response) *FetchError {
	failure := &FetchError{Status: answer.StatusCode, Err: ErrRefused}
	if answer.StatusCode == http.StatusNotFound {
		failure.Err = ErrNotAvailable
	}

	if at, err := http.ParseTime(answer.Header.Get("Retry-After")); err == nil {
		failure.RetryAfter = at
	}

	return failure
}

// sinkWriter keeps a write error apart from a read error, so a full disk is
// not taken for a cut transfer.
type sinkWriter struct {
	to  io.Writer
	err error
}

func (s *sinkWriter) Write(p []byte) (int, error) {
	n, err := s.to.Write(p)
	if err != nil {
		s.err = err
	}

	return n, err
}
