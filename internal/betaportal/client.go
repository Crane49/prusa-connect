package betaportal

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/internal/kernel/httporigin"
)

const (
	ClientID = "device-connect-local"
	Scope    = "device"

	DeviceAuthorizationPath = "/oauth/device_authorization"
	TokenPath               = "/oauth/token" //nolint:gosec // an endpoint path, not a token
	RevocationPath          = "/oauth/revoke"
	// devicePath answers a paired device whether its pairing still works.
	devicePath = "/api/v1/device"

	deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"
	defaultInterval = 5
	slowDownStep    = 5
	maxResponse     = 1 << 20
	maxRedirects    = 5
	maxNameRunes    = 200
	// maxExpiresIn and maxInterval bound what the portal may ask for, in
	// seconds, so a hostile answer cannot overflow a time.Duration.
	maxExpiresIn = 24 * 60 * 60
	maxInterval  = 60 * 60
	// requestTimeout bounds one call to the portal, so a portal that stops
	// answering cannot hold a browser request or a pairing flight open.
	requestTimeout = 30 * time.Second
	// defaultRetryAfter is the wait used when a 429 carries no usable
	// Retry-After header.
	defaultRetryAfter = time.Minute
	// verifyInterval is how often an open Updates page may make a paired
	// server ask the portal whether the pairing still works.
	verifyInterval = time.Minute

	// approvalPending is the error_description the portal sends while a
	// confirmed device waits for the product team to approve it.
	approvalPending = "Waiting for the team to approve the device"
)

var (
	// ErrUpstream reports that the configured portal did not provide a valid
	// response. The error never includes a response body or a secret.
	ErrUpstream = errors.New("beta portal is unavailable")
	// ErrRevocationIncomplete reports that local authorization was cleared but
	// the portal did not confirm remote revocation.
	ErrRevocationIncomplete = errors.New("beta portal revocation was not confirmed")
	// ErrAlreadyPaired refuses a new pairing while this server is paired.
	ErrAlreadyPaired = errors.New("this server is already paired with the Beta Portal")
	// ErrRateLimited reports that the portal asked this server to wait before
	// it starts another pairing. RateLimitError carries the wait.
	ErrRateLimited = errors.New("the Beta Portal asked to wait before pairing again")
	// ErrAccessSuspended reports a portal that refused this paired server
	// because the tester's access to the product is suspended. The pairing
	// stays.
	ErrAccessSuspended = errors.New("the Beta Portal suspended the tester's access to the product")
	// ErrClockSkew reports that the portal refused a proof dated by this
	// server's clock, also after the proof was dated with the portal's clock.
	// ClockSkewError carries the difference.
	ErrClockSkew = errors.New("the Beta Portal refused the proof because this server's clock is wrong")
)

// RateLimitError is ErrRateLimited with the wait the portal asked for.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s: retry in %s", ErrRateLimited, e.RetryAfter)
}

func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// ClockSkewError is ErrClockSkew with the portal's clock minus this server's.
type ClockSkewError struct {
	Offset time.Duration
}

func (e *ClockSkewError) Error() string {
	return fmt.Sprintf("%s: the clocks differ by %s", ErrClockSkew, e.Offset.Round(time.Second).Abs())
}

func (e *ClockSkewError) Unwrap() error { return ErrClockSkew }

// Metadata identifies this Prusa Connect instance to the portal.
type Metadata struct {
	InstanceID string
	Name       string
	Platform   string
}

// Client runs the OAuth device flow while Store owns every secret. Its mutex
// guards only the decisions about what to send. No portal call runs under it.
type Client struct {
	mu       sync.Mutex
	starting *flight
	polling  bool
	// verifying and verifiedAt gate how often a paired server asks the
	// portal whether its pairing still works.
	verifying  bool
	verifiedAt time.Time
	retryAt    time.Time

	timeout time.Duration
	base    *url.URL
	http    *http.Client
	store   *Store
	meta    Metadata
	now     func() time.Time
}

// flight is one device authorization in progress. A Start that arrives while
// it runs waits for it instead of asking the portal for a second code.
type flight struct {
	done   chan struct{}
	status Status
	err    error
}

// credentials sign one request. A device-flow request carries only the key.
type credentials struct {
	token string
	key   *ecdsa.PrivateKey
}

// NewClient builds a portal client. The URL must be HTTPS, except loopback
// HTTP used by a local portal or a test.
func NewClient(baseURL string, httpClient *http.Client, store *Store, metadata Metadata) (*Client, error) {
	base, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("build Beta Portal client: pairing store is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	metadata.Name = truncateRunes(metadata.Name, maxNameRunes)

	return &Client{
		timeout: requestTimeout, base: base, http: httpClient, store: store, meta: metadata, now: time.Now,
	}, nil
}

// Start begins a device authorization or returns the current pending request.
// It refuses with ErrAlreadyPaired while the server is paired.
func (c *Client) Start(ctx context.Context) (Status, error) {
	c.mu.Lock()
	now := c.now()
	status := c.store.Status()
	if status.State == StatePaired {
		c.mu.Unlock()

		return Status{}, ErrAlreadyPaired
	}
	if pairing, ok := c.store.pairing(); ok && now.Before(pairing.ExpiresAt) {
		c.mu.Unlock()

		return status, nil
	}
	if running := c.starting; running != nil {
		c.mu.Unlock()
		select {
		case <-running.done:
			return running.status, running.err
		case <-ctx.Done():
			return Status{}, fmt.Errorf("wait for the Beta Portal pairing: %w", ctx.Err())
		}
	}
	if now.Before(c.retryAt) {
		wait := c.retryAt.Sub(now)
		c.mu.Unlock()

		return Status{}, &RateLimitError{RetryAfter: wait.Round(time.Second)}
	}
	running := &flight{done: make(chan struct{})}
	c.starting = running
	c.mu.Unlock()

	// The flight belongs to every caller waiting on it, so the first caller
	// leaving does not cancel it. The per-call timeout still bounds it.
	running.status, running.err = c.start(context.WithoutCancel(ctx))

	c.mu.Lock()
	c.starting = nil
	c.mu.Unlock()
	close(running.done)

	return running.status, running.err
}

func (c *Client) start(ctx context.Context) (Status, error) {
	form := url.Values{
		"client_id":   {ClientID},
		"scope":       {Scope},
		"instance_id": {c.meta.InstanceID},
		"name":        {c.meta.Name},
		"platform":    {c.meta.Platform},
	}
	// The portal binds the code to this key. A disconnect during the call
	// rotates the key, and store.start then refuses the stale code.
	key := c.store.keyOnly()
	var response deviceAuthorizationResponse
	got, err := c.send(ctx, DeviceAuthorizationPath, form, credentials{key: key}, &response)
	if err != nil {
		return Status{}, err
	}
	if got.status == http.StatusTooManyRequests {
		wait := retryAfter(got.header.Get("Retry-After"), c.now())
		c.mu.Lock()
		c.retryAt = c.now().Add(wait)
		c.mu.Unlock()

		return Status{}, &RateLimitError{RetryAfter: wait}
	}
	if got.status != http.StatusOK {
		return Status{}, fmt.Errorf("%w: portal answered %s", ErrUpstream, http.StatusText(got.status))
	}
	if err = response.validate(c.base); err != nil {
		return Status{}, fmt.Errorf("%w: invalid device authorization response", ErrUpstream)
	}

	now := c.now()
	interval := response.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	pairing := pairingDocument{
		DeviceCode: response.DeviceCode, UserCode: response.UserCode,
		VerificationURI:         response.VerificationURI,
		VerificationURIComplete: response.verificationURIComplete(),
		Step:                    StepConfirmation,
		ExpiresAt:               now.Add(time.Duration(response.ExpiresIn) * time.Second),
		IntervalSeconds:         interval,
		NextPollAt:              now.Add(time.Duration(interval) * time.Second),
	}

	// The store refuses when a pairing finished while the portal answered.
	status, err := c.store.start(pairing, key)
	if errors.Is(err, ErrAlreadyPaired) {
		return Status{}, err
	}
	if err != nil {
		return Status{}, fmt.Errorf("store the Beta Portal pairing: %w", err)
	}

	return status, nil
}

// Status returns the persisted safe state without making a network request.
func (c *Client) Status(_ context.Context) (Status, error) {
	if pairing, pending := c.store.pairing(); pending && !c.now().Before(pairing.ExpiresAt) {
		if err := c.store.terminal(pairing.DeviceCode, StateExpired); err != nil {
			return Status{}, fmt.Errorf("store the expired Beta Portal pairing: %w", err)
		}
	}

	return c.store.Status(), nil
}

// Refresh polls only when the saved interval is due, and never twice at once.
// This keeps polling on the server and makes the pending request recoverable
// without a worker goroutine. On a paired server it asks the portal, at most
// once a minute, whether the pairing still works, so a pairing revoked or
// suspended in the portal shows on the page without waiting for an update
// check.
func (c *Client) Refresh(ctx context.Context) (Status, error) {
	c.mu.Lock()
	pairing, pending := c.store.pairing()
	if !pending {
		now := c.now()
		due := c.store.Status().State == StatePaired && !c.verifying && !now.Before(c.verifiedAt.Add(verifyInterval))
		if due {
			c.verifying, c.verifiedAt = true, now
		}
		c.mu.Unlock()
		if due {
			c.verify(ctx)
			c.mu.Lock()
			c.verifying = false
			c.mu.Unlock()
		}

		return c.store.Status(), nil
	}
	now := c.now()
	if !now.Before(pairing.ExpiresAt) {
		c.mu.Unlock()
		if err := c.store.terminal(pairing.DeviceCode, StateExpired); err != nil {
			return Status{}, fmt.Errorf("store the expired Beta Portal pairing: %w", err)
		}

		return c.store.Status(), nil
	}
	if now.Before(pairing.NextPollAt) || c.polling {
		c.mu.Unlock()

		return c.store.Status(), nil
	}
	c.polling = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.polling = false
		c.mu.Unlock()
	}()

	return c.poll(ctx, pairing)
}

func (c *Client) poll(ctx context.Context, pairing pairingDocument) (Status, error) {
	form := url.Values{
		"grant_type":  {deviceGrantType},
		"device_code": {pairing.DeviceCode},
		"client_id":   {ClientID},
	}
	var response tokenResponse
	got, err := c.send(ctx, TokenPath, form, credentials{}, &response)
	if err != nil {
		return Status{}, c.postpone(pairing, err)
	}
	// The answer may arrive after a disconnect or a new start. Every store
	// write below names this pairing's device code and leaves a newer one alone.
	now := c.now()

	if got.status == http.StatusOK {
		granted, approvalErr := response.approval()
		if approvalErr != nil {
			return Status{}, approvalErr
		}
		if err = c.store.approve(pairing.DeviceCode, granted); err != nil {
			return Status{}, fmt.Errorf("store the Beta Portal approval: %w", err)
		}

		return c.store.Status(), nil
	}

	switch response.Error {
	case "authorization_pending":
		step := StepConfirmation
		if response.Description == approvalPending {
			step = StepApproval
		}
		err = c.store.schedule(pairing.DeviceCode,
			now.Add(time.Duration(pairing.IntervalSeconds)*time.Second), pairing.IntervalSeconds, step)
	case "slow_down":
		interval := pairing.IntervalSeconds + slowDownStep
		err = c.store.schedule(pairing.DeviceCode, now.Add(time.Duration(interval)*time.Second), interval, "")
	case "access_denied":
		err = c.store.terminal(pairing.DeviceCode, StateDenied)
	case "expired_token":
		err = c.store.terminal(pairing.DeviceCode, StateExpired)
	default:
		return Status{}, c.postpone(pairing,
			fmt.Errorf("%w: token endpoint answered %s", ErrUpstream, http.StatusText(got.status)))
	}
	if err != nil {
		return Status{}, fmt.Errorf("store the Beta Portal pairing state: %w", err)
	}

	return c.store.Status(), nil
}

// postpone keeps the poll interval after a failed poll, so a portal that
// fails is not asked again by every caller of Refresh at once.
func (c *Client) postpone(pairing pairingDocument, failure error) error {
	next := c.now().Add(time.Duration(pairing.IntervalSeconds) * time.Second)
	if err := c.store.schedule(pairing.DeviceCode, next, pairing.IntervalSeconds, ""); err != nil {
		return errors.Join(failure, fmt.Errorf("store the Beta Portal pairing state: %w", err))
	}

	return failure
}

// Disconnect clears local authorization first and then attempts remote
// revocation with the token it cleared. ErrRevocationIncomplete tells an
// administrator that the local clear succeeded while the remote portal could
// not be reached.
func (c *Client) Disconnect(ctx context.Context) error {
	c.mu.Lock()
	token, key := c.store.access()
	err := c.store.ClearLocal()
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("clear the local Beta Portal pairing: %w", err)
	}
	if token == "" {
		return nil
	}

	form := url.Values{"token": {token}, "client_id": {ClientID}}
	var ignored struct{}
	got, err := c.send(ctx, RevocationPath, form, credentials{token: token, key: key}, &ignored)
	if err == nil && got.status != http.StatusOK && got.status != http.StatusNoContent {
		err = fmt.Errorf("%w: revocation endpoint answered %s", ErrUpstream, http.StatusText(got.status))
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRevocationIncomplete, ErrUpstream)
	}

	return nil
}

type deviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func (r deviceAuthorizationResponse) validate(base *url.URL) error {
	page, err := url.Parse(r.VerificationURI)
	if err != nil || !portalAddress(r.VerificationURI) || !httporigin.Same(base, page) {
		return errors.New("invalid verification URI")
	}
	if r.DeviceCode == "" || len(r.DeviceCode) > maxSecret ||
		invalidHeaderValue(r.UserCode, maxMetadata) ||
		len(r.VerificationURI) > maxSecret || r.ExpiresIn <= 0 || r.ExpiresIn > maxExpiresIn ||
		r.Interval > maxInterval {
		return errors.New("missing device authorization field")
	}

	return nil
}

// verificationURIComplete keeps the portal's address with the code filled in
// only when it is a safe address on the same origin as verification_uri.
func (r deviceAuthorizationResponse) verificationURIComplete() string {
	if r.VerificationURIComplete == "" || len(r.VerificationURIComplete) > maxSecret ||
		!portalAddress(r.VerificationURIComplete) {
		return ""
	}
	page, err := url.Parse(r.VerificationURI)
	if err != nil {
		return ""
	}
	complete, err := url.Parse(r.VerificationURIComplete)
	if err != nil || !httporigin.Same(page, complete) {
		return ""
	}

	return r.VerificationURIComplete
}

func portalAddress(raw string) bool {
	address, err := url.Parse(raw)

	return err == nil && address.Hostname() != "" && address.User == nil &&
		(address.Scheme == "https" || address.Scheme == "http" && loopbackHost(address.Hostname()))
}

type tokenResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	Error       string    `json:"error"`
	Description string    `json:"error_description"`
	Identity    *Identity `json:"identity,omitempty"`
	Product     *Product  `json:"product,omitempty"`
	Device      *Device   `json:"device,omitempty"`
}

func (r tokenResponse) approval() (approval, error) {
	if invalidHeaderValue(r.AccessToken, maxSecret) || !strings.EqualFold(r.TokenType, "DPoP") {
		return approval{}, fmt.Errorf("%w: invalid token response", ErrUpstream)
	}
	identity := r.Identity
	if identity != nil && identity.Subject == "" && identity.Name == "" {
		identity = nil
	}
	if err := validateMetadata(identity, r.Product, r.Device); err != nil {
		return approval{}, fmt.Errorf("%w: invalid token response", ErrUpstream)
	}

	return approval{token: r.AccessToken, identity: identity, product: r.Product, device: r.Device}, nil
}

// proofError is the part of an OAuth error a refused proof is read from.
type proofError struct {
	Error string `json:"error"`
}

// answer is what a caller needs from a portal response besides its body.
type answer struct {
	status int
	header http.Header
}

// send posts one form with its own timeout. When the portal refuses the proof
// and its Date header shows this server's clock is wrong, send dates a new
// proof with the portal's time and tries once more.
func (c *Client) send(
	ctx context.Context, path string, form url.Values, signer credentials, into any,
) (answer, error) {
	for attempt := 0; ; attempt++ {
		sent := c.now()
		got, content, err := c.post(ctx, path, form, signer)
		if err != nil {
			return answer{}, err
		}
		if !proofRefused(got.status, content) {
			if len(content) > 0 && got.status != http.StatusTooManyRequests {
				if err = json.Unmarshal(content, into); err != nil {
					return answer{}, fmt.Errorf("%w: decode portal response", ErrUpstream)
				}
			}

			return got, nil
		}

		offset, learned := c.store.learnSkew(sent, got.header.Get("Date"))
		if learned && attempt == 0 {
			continue
		}
		// Only a proof dated with the portal's own clock and still refused is
		// the clock's fault. A refusal with the correction in use is not.
		if attempt > 0 && offset.Abs() >= clockTolerance {
			return answer{}, &ClockSkewError{Offset: offset}
		}

		return answer{}, fmt.Errorf("%w: the portal refused the proof", ErrUpstream)
	}
}

func proofRefused(status int, content []byte) bool {
	if status != http.StatusBadRequest && status != http.StatusUnauthorized {
		return false
	}
	var refused proofError

	return json.Unmarshal(content, &refused) == nil && refused.Error == "invalid_dpop_proof"
}

func (c *Client) post(
	ctx context.Context, path string, form url.Values, signer credentials,
) (answer, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	endpoint := *c.base
	endpoint.Path = strings.TrimRight(c.base.Path, "/") + path
	endpoint.RawPath = ""
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint.String(), strings.NewReader(form.Encode()),
	)
	if err != nil {
		return answer{}, nil, fmt.Errorf("%w: build portal request", ErrUpstream)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if err = c.sign(request, signer); err != nil {
		return answer{}, nil, fmt.Errorf("%w: build portal proof", ErrUpstream)
	}

	response, err := c.client(signer).Do(request)
	if err != nil {
		return answer{}, nil, fmt.Errorf("%w: portal request failed", ErrUpstream)
	}
	defer func() { _ = response.Body.Close() }()

	content, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || len(content) > maxResponse {
		return answer{}, nil, fmt.Errorf("%w: read portal response", ErrUpstream)
	}

	return answer{status: response.StatusCode, header: response.Header}, content, nil
}

func (c *Client) sign(request *http.Request, signer credentials) error {
	request.Header.Del("Authorization")
	request.Header.Del("DPoP")
	key := signer.key
	if key == nil {
		key = c.store.keyOnly()
	}
	proof, err := makeProof(key, request.Method, request.URL, signer.token, c.store.portalTime(c.now()))
	if err != nil {
		return err
	}
	if signer.token != "" {
		request.Header.Set("Authorization", "DPoP "+signer.token)
	}
	request.Header.Set("DPoP", proof)

	return nil
}

func (c *Client) client(signer credentials) *http.Client {
	client := *c.http
	previous := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("%w: too many redirects", ErrUpstream)
		}
		if !httporigin.Same(c.base, next.URL) {
			return fmt.Errorf("%w: redirect left the configured origin", ErrUpstream)
		}
		if previous != nil {
			if err := previous(next, via); err != nil {
				return err
			}
		}

		return c.sign(next, signer)
	}

	return &client
}

// retryAfter reads a Retry-After header as seconds or as an HTTP date.
func retryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(min(seconds, maxInterval)) * time.Second
	}
	if at, err := http.ParseTime(header); err == nil && at.After(now) {
		return min(at.Sub(now).Round(time.Second), maxInterval*time.Second)
	}

	return defaultRetryAfter
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}

	return string([]rune(value)[:limit])
}

func parseBaseURL(raw string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || base.Hostname() == "" || base.User != nil ||
		base.RawQuery != "" || base.Fragment != "" ||
		base.Scheme != "https" && (base.Scheme != "http" || !loopbackHost(base.Hostname())) {
		return nil, errors.New("build Beta Portal client: server is not an HTTPS or loopback HTTP base URL")
	}

	return base, nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// verify asks the portal whether the pairing still works (GET /api/v1/device).
// What it learns lands in the store through Rejected; a portal it cannot reach
// changes nothing.
func (c *Client) verify(ctx context.Context) {
	for range 2 {
		if !c.verifyOnce(ctx) {
			return
		}
	}
}

// verifyOnce sends one check and says whether to send it once more.
func (c *Client) verifyOnce(ctx context.Context) bool {
	token, key := c.store.access()
	if token == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	endpoint := *c.base
	endpoint.Path = strings.TrimRight(c.base.Path, "/") + devicePath
	endpoint.RawPath = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return false
	}
	request.Header.Set("Accept", "application/json")
	signer := credentials{token: token, key: key}
	if err = c.sign(request, signer); err != nil {
		return false
	}
	response, err := c.client(signer).Do(request)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponse))
	_ = response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
		c.store.suspended.Store(false)
	case http.StatusUnauthorized:
		retry, _ := c.store.Rejected(response)

		return retry
	}

	return false
}
