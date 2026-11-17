// Package mjpeg reads a camera that already speaks HTTP: a URL that answers
// one JPEG, and a URL that answers a multipart stream of them.
//
// It is a reference implementation of sdk/cameraapi, meant to be copied. It
// fits the Prusa ESP32-CAM firmware and any IP camera with a snapshot URL and
// a stream URL.
//
// It decodes no image. A snapshot is the bytes the device sent. A stream is
// relayed frame by frame with the boundary cameraapi fixes.
package mjpeg

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Type is the `type` of a camera instance that selects this driver.
const Type = "mjpeg"

const (
	defaultTimeout = 5 * time.Second
	// defaultMaxFrameBytes bounds one frame. A camera can answer with an
	// endless body.
	defaultMaxFrameBytes = 8 << 20
	// defaultMinInterval is the shortest gap between two fetches from the
	// device, so many viewers do not become many device fetches.
	defaultMinInterval = 10 * time.Second

	// streamIdleTimeout is the transport's idle connection timeout.
	streamIdleTimeout = 60 * time.Second

	// emittableFormats is how many formats this driver can declare: a still
	// image and a stream.
	emittableFormats = 2
)

var (
	// ErrNoURL rejects a spec that names neither endpoint.
	ErrNoURL = errors.New("mjpeg: config.snapshot_url or config.stream_url is required")
	// ErrInsecureURL rejects a scheme this driver cannot fetch.
	ErrInsecureURL = errors.New("mjpeg: a camera URL is http or https")
	// ErrBadFrame reports a device answer that is not an image.
	ErrBadFrame = errors.New("mjpeg: the camera did not answer with an image")
)

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { cameraapi.Register(Type, doc(), New) }

// doc is this driver's entry in the published extension reference.
func doc() extension.Doc {
	return extension.Doc{
		Summary: "Reads a camera that answers a JPEG on one URL and a multipart MJPEG stream on another.",
		Description: "The simplest camera there is, and the one most devices already are: an HTTP " +
			"snapshot endpoint and an HTTP multipart stream endpoint. Nothing is decoded or " +
			"re-encoded, so the cost is the transfer and nothing else. Frames from the device are " +
			"bounded and are refused unless they are JPEG, because a camera is an untrusted device on " +
			"somebody's network and its answer is served to a browser.",
		Version:   "1.0.0",
		Contract:  cameraapi.Contract,
		Stability: extension.StabilityStable,
		Options: []extension.Option{
			{
				Key:      "snapshot_url",
				Type:     extension.OptionURL,
				Required: false,
				Summary:  "The endpoint that answers one JPEG. Without it this camera declares no still image.",
			},
			{
				Key:     "stream_url",
				Type:    extension.OptionURL,
				Summary: "The endpoint that answers a multipart MJPEG stream. Without it this camera declares no stream.",
			},
			{
				Key:     "username",
				Type:    extension.OptionString,
				Summary: "The basic-auth user the camera requires. It is never logged, reported or echoed back.",
			},
			{
				Key:     "password",
				Type:    extension.OptionSecret,
				Summary: "The basic-auth password. It is never logged, reported or echoed back.",
			},
			{
				Key:     "timeout",
				Type:    extension.OptionDuration,
				Default: "5s",
				Summary: "How long one snapshot fetch waits before it is abandoned.",
			},
			{
				Key:     "min_interval",
				Type:    extension.OptionDuration,
				Default: "10s",
				Summary: "The shortest gap between two fetches from the device. Requests inside it are served the frame already held.",
			},
			{
				Key:     "max_frame_bytes",
				Type:    extension.OptionInt,
				Default: "8388608",
				Summary: "The ceiling on one frame. A device answering with an endless body is cut off here.",
			},
			{
				Key:     "insecure_skip_verify",
				Type:    extension.OptionBool,
				Default: "false",
				Summary: "Accept an https camera whose certificate does not verify. It is a camera on a LAN, so this is sometimes the only option, and it is off unless the operator says so.",
			},
		},
	}
}

// Config is what the operator writes under a camera instance's `config`.
//
// It has no credential field on purpose. A %v of the struct would print one
// in a log or an error. The credential lives in a closure, see basicAuth.
type Config struct {
	SnapshotURL   string
	StreamURL     string
	Timeout       time.Duration
	MinInterval   time.Duration
	MaxFrameBytes int64
	SkipVerify    bool
}

// credential adds the camera's authentication to one request.
type credential func(*http.Request)

// Camera implements cameraapi.Camera. It keeps the last frame and answers every
// viewer from it, so many viewers cost one device fetch per interval. The host
// decides who may see the frame.
type Camera struct {
	client    *http.Client
	cfg       Config
	authorize credential
	formats   []cameraapi.Format

	// fetching serialises the device fetch, so a burst of viewers produces
	// one request.
	fetching sync.Mutex

	mu   sync.Mutex
	held cameraapi.Snapshot

	closeOnce sync.Once
	closed    chan struct{}
}

var (
	_ cameraapi.Camera = (*Camera)(nil)
	_ cameraapi.Paced  = (*Camera)(nil)
)

// New builds a Camera from its spec.
func New(spec cameraapi.ProviderSpec) (cameraapi.Camera, error) {
	cfg, authorize, err := readConfig(spec.Config)
	if err != nil {
		return nil, err
	}

	formats := make([]cameraapi.Format, 0, emittableFormats)
	if cfg.SnapshotURL != "" {
		formats = append(formats, cameraapi.FormatJPEG)
	}

	if cfg.StreamURL != "" {
		formats = append(formats, cameraapi.FormatMJPEG)
	}

	return &Camera{
		client:    newClient(cfg),
		cfg:       cfg,
		authorize: authorize,
		formats:   formats,
		closed:    make(chan struct{}),
	}, nil
}

// Formats is what this camera can emit: a still image when a snapshot URL is
// configured, a stream when a stream URL is.
func (c *Camera) Formats() []cameraapi.Format {
	// A copy, because the host may sort it.
	return append([]cameraapi.Format(nil), c.formats...)
}

// MinInterval is the configured min_interval, the shortest gap between two
// fetches from the device. A request inside it gets the frame already held.
func (c *Camera) MinInterval() time.Duration { return c.cfg.MinInterval }

// Snapshot returns the frame held for this camera, fetching a new one when the
// held one is older than the configured interval. A fetch that fails while a
// frame is held returns the held frame.
func (c *Camera) Snapshot(ctx context.Context) (cameraapi.Snapshot, error) {
	if c.cfg.SnapshotURL == "" {
		return cameraapi.Snapshot{}, fmt.Errorf("%w: this camera declares no still image",
			cameraapi.ErrFormatNotSupported)
	}

	if held, fresh := c.fresh(); fresh {
		return held, nil
	}

	c.fetching.Lock()
	defer c.fetching.Unlock()

	// Check again under the fetch lock. The previous holder may have fetched.
	if held, fresh := c.fresh(); fresh {
		return held, nil
	}

	shot, err := c.fetch(ctx)
	if err != nil {
		if held, ok := c.last(); ok {
			return held, nil
		}

		return cameraapi.Snapshot{}, err
	}

	c.mu.Lock()
	c.held = shot
	c.mu.Unlock()

	return shot, nil
}

// Open starts a stream. Only MJPEG is supported, and only when a stream URL is
// configured.
func (c *Camera) Open(ctx context.Context, format cameraapi.Format) (cameraapi.Stream, error) {
	if format != cameraapi.FormatMJPEG || c.cfg.StreamURL == "" {
		return cameraapi.Stream{}, fmt.Errorf("%w: this camera emits %v",
			cameraapi.ErrFormatNotSupported, c.formats)
	}

	// The stream outlives the call that opened it, so its context is detached
	// from the caller's and cancelled by Close.
	streamCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	go func() {
		select {
		case <-c.closed:
			cancel()
		case <-streamCtx.Done():
		}
	}()

	req, err := c.request(streamCtx, c.cfg.StreamURL)
	if err != nil {
		cancel()

		return cameraapi.Stream{}, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		cancel()

		return cameraapi.Stream{}, fmt.Errorf("open the camera stream: %w", scrub(err))
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()

		return cameraapi.Stream{}, fmt.Errorf("%w: the stream endpoint answered %d",
			ErrBadFrame, resp.StatusCode)
	}

	boundary, err := boundaryOf(resp.Header.Get("Content-Type"))
	if err != nil {
		_ = resp.Body.Close()
		cancel()

		return cameraapi.Stream{}, err
	}

	relay := newRelay(resp.Body, boundary, c.cfg.MaxFrameBytes, cancel)

	return cameraapi.Stream{Body: relay, MediaType: cameraapi.FormatMJPEG.MediaType()}, nil
}

// Close releases the device and ends every stream this camera opened. It is
// safe to call more than once.
func (c *Camera) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.client.CloseIdleConnections()
	})

	return nil
}

// fresh returns the held frame when it is inside the configured interval.
func (c *Camera) fresh() (cameraapi.Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.held.Taken.IsZero() || time.Since(c.held.Taken) >= c.cfg.MinInterval {
		return cameraapi.Snapshot{}, false
	}

	return c.held, true
}

// last returns whatever is held, however old.
func (c *Camera) last() (cameraapi.Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.held.Taken.IsZero() {
		return cameraapi.Snapshot{}, false
	}

	return c.held, true
}

// fetch reads one image from the device.
func (c *Camera) fetch(ctx context.Context) (cameraapi.Snapshot, error) {
	select {
	case <-c.closed:
		return cameraapi.Snapshot{}, cameraapi.ErrNoFrame
	default:
	}

	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	req, err := c.request(ctx, c.cfg.SnapshotURL)
	if err != nil {
		return cameraapi.Snapshot{}, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return cameraapi.Snapshot{}, fmt.Errorf("fetch a snapshot: %w", scrub(err))
	}

	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNoContent:
		return cameraapi.Snapshot{}, cameraapi.ErrNoFrame
	default:
		return cameraapi.Snapshot{}, fmt.Errorf("%w: the snapshot endpoint answered %d",
			ErrBadFrame, resp.StatusCode)
	}

	body, err := readBounded(resp.Body, c.cfg.MaxFrameBytes)
	if err != nil {
		return cameraapi.Snapshot{}, err
	}

	if !isJPEG(body) {
		return cameraapi.Snapshot{}, fmt.Errorf("%w: the body does not start with a JPEG marker", ErrBadFrame)
	}

	return cameraapi.Snapshot{
		Bytes:     body,
		MediaType: cameraapi.FormatJPEG.MediaType(),
		Taken:     takenAt(resp),
	}, nil
}

// request builds one authenticated GET.
func (c *Camera) request(ctx context.Context, endpoint string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build a camera request: %w", scrub(err))
	}

	c.authorize(req)

	return req, nil
}

// relay is one viewer's stream. It reads multipart parts from the device and
// writes them back with the boundary cameraapi fixes. It reuses one frame buffer.
type relay struct {
	source io.ReadCloser
	parts  *multipart.Reader
	cancel context.CancelFunc
	limit  int64

	frame   []byte
	pending []byte

	closeOnce sync.Once
	closeErr  error
}

func newRelay(source io.ReadCloser, boundary string, limit int64, cancel context.CancelFunc) *relay {
	return &relay{
		source: source,
		parts:  multipart.NewReader(bufio.NewReader(source), boundary),
		cancel: cancel,
		limit:  limit,
	}
}

// Read returns the next bytes of the reframed stream.
func (r *relay) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		if err := r.next(); err != nil {
			return 0, err
		}
	}

	n := copy(p, r.pending)
	r.pending = r.pending[n:]

	return n, nil
}

// next reads one part from the device and renders it as one frame.
func (r *relay) next() error {
	part, err := r.parts.NextPart()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return io.EOF
		}

		return fmt.Errorf("read a camera frame: %w", scrub(err))
	}

	defer func() { _ = part.Close() }()

	body, err := readBounded(part, r.limit)
	if err != nil {
		return err
	}

	if !isJPEG(body) {
		return fmt.Errorf("%w: a stream frame does not start with a JPEG marker", ErrBadFrame)
	}

	r.frame = r.frame[:0]
	r.frame = append(r.frame, "--"+cameraapi.MJPEGBoundary+"\r\nContent-Type: image/jpeg\r\nContent-Length: "...)
	r.frame = appendInt(r.frame, len(body))
	r.frame = append(r.frame, "\r\n\r\n"...)
	r.frame = append(r.frame, body...)
	r.frame = append(r.frame, "\r\n"...)

	r.pending = r.frame

	return nil
}

// Close ends the stream and releases the connection to the device. Cancelling
// the request context unblocks a Read that waits on the device.
func (r *relay) Close() error {
	r.closeOnce.Do(func() {
		r.cancel()
		r.closeErr = r.source.Close()
	})

	return r.closeErr
}

// readConfig reads the spec's map into typed settings. The credential is
// returned separately, so a Config never contains a password.
func readConfig(raw map[string]any) (Config, credential, error) {
	cfg := Config{
		Timeout:       defaultTimeout,
		MinInterval:   defaultMinInterval,
		MaxFrameBytes: defaultMaxFrameBytes,
	}

	cfg.SnapshotURL = stringAt(raw, "snapshot_url")
	cfg.StreamURL = stringAt(raw, "stream_url")
	cfg.SkipVerify = boolAt(raw, "insecure_skip_verify")

	if value, ok := durationAt(raw, "timeout"); ok {
		cfg.Timeout = value
	}

	if value, ok := durationAt(raw, "min_interval"); ok {
		cfg.MinInterval = value
	}

	if value, ok := intAt(raw, "max_frame_bytes"); ok && value > 0 {
		cfg.MaxFrameBytes = value
	}

	if cfg.SnapshotURL == "" && cfg.StreamURL == "" {
		return Config{}, nil, ErrNoURL
	}

	// Take the credential out of each URL. Operators write
	// http://user:pass@camera/snapshot.jpg, and a %v would print it.
	var username, password string

	for _, endpoint := range []*string{&cfg.SnapshotURL, &cfg.StreamURL} {
		bare, user, secret, err := withoutCredential(*endpoint)
		if err != nil {
			return Config{}, nil, err
		}

		*endpoint = bare

		if username == "" && password == "" {
			username, password = user, secret
		}
	}

	// The username and password options win over a credential in a URL.
	if user, secret := stringAt(raw, "username"), stringAt(raw, "password"); user != "" || secret != "" {
		username, password = user, secret
	}

	return cfg, basicAuth(username, password), nil
}

// basicAuth keeps the credential in a closure, so printing a Camera prints a
// function pointer and not the password.
func basicAuth(username, password string) credential {
	if username == "" && password == "" {
		return func(*http.Request) {}
	}

	return func(req *http.Request) { req.SetBasicAuth(username, password) }
}

// withoutCredential rejects a scheme that is not http or https and returns the
// endpoint without its userinfo. No error quotes the URL, which may contain a
// password.
func withoutCredential(endpoint string) (bare, username, password string, err error) {
	if endpoint == "" {
		return "", "", "", nil
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: the URL could not be parsed", ErrInsecureURL)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", "", fmt.Errorf("%w: the scheme is %q", ErrInsecureURL, parsed.Scheme)
	}

	if parsed.Host == "" {
		return "", "", "", fmt.Errorf("%w: the URL names no host", ErrInsecureURL)
	}

	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}

	parsed.User = nil

	return parsed.String(), username, password, nil
}

// boundaryOf reads the multipart boundary out of the device's content type.
func boundaryOf(contentType string) (string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", fmt.Errorf("%w: the stream content type could not be parsed", ErrBadFrame)
	}

	if !strings.HasPrefix(mediaType, "multipart/") {
		return "", fmt.Errorf("%w: the stream endpoint answered %s and a stream is multipart",
			ErrBadFrame, mediaType)
	}

	boundary := params["boundary"]
	if boundary == "" {
		return "", fmt.Errorf("%w: the stream content type carries no boundary", ErrBadFrame)
	}

	return boundary, nil
}

// readBounded reads at most limit bytes and rejects a longer body. It reads one
// byte past the limit to tell a body at the limit from one over it.
func readBounded(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read a camera frame: %w", scrub(err))
	}

	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: the frame is over the %d byte ceiling", ErrBadFrame, limit)
	}

	return body, nil
}

// isJPEG reports whether the bytes start with the JPEG start-of-image marker.
// A camera is untrusted, and its answer is served to a browser on this origin.
func isJPEG(body []byte) bool {
	return len(body) >= 3 && bytes.Equal(body[:3], []byte{0xFF, 0xD8, 0xFF})
}

// takenAt is the device's Last-Modified time, or the time of the read.
func takenAt(resp *http.Response) time.Time {
	if stamp := resp.Header.Get("Last-Modified"); stamp != "" {
		if parsed, err := http.ParseTime(stamp); err == nil {
			return parsed.UTC()
		}
	}

	return time.Now().UTC()
}

// scrub removes the URL from a transport error. net/http puts the whole URL in
// its errors, and a camera URL may contain a credential.
func scrub(err error) error {
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("%s the camera: %w", urlErr.Op, urlErr.Err)
	}

	return err
}

func appendInt(dst []byte, value int) []byte {
	if value == 0 {
		return append(dst, '0')
	}

	var digits [20]byte

	i := len(digits)

	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}

	return append(dst, digits[i:]...)
}

func stringAt(raw map[string]any, key string) string {
	value, _ := raw[key].(string)

	return value
}

func boolAt(raw map[string]any, key string) bool {
	value, _ := raw[key].(bool)

	return value
}

func durationAt(raw map[string]any, key string) (time.Duration, bool) {
	switch value := raw[key].(type) {
	case string:
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return 0, false
		}

		return parsed, true
	case time.Duration:
		return value, true
	}

	return 0, false
}

func intAt(raw map[string]any, key string) (int64, bool) {
	switch value := raw[key].(type) {
	case int:
		return int64(value), true
	case int64:
		return value, true
	case float64:
		return int64(value), true
	}

	return 0, false
}
