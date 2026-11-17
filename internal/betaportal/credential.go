// Package betaportal owns the local half of Beta Portal device pairing.
package betaportal

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/prusa3d/prusa-connect/internal/kernel/atomicfile"
)

const (
	fileMode        = 0o600
	dirMode         = 0o700
	pairingFile     = "beta-portal-pairing.json"
	documentVersion = 2
	maxSecret       = 8192
	maxMetadata     = 512
	brokenTimestamp = "20060102T150405.000000000Z"
)

// State is the safe, public state of the portal pairing.
type State string

const (
	StateUnpaired State = "unpaired"
	StatePending  State = "pending"
	StatePaired   State = "paired"
	StateDenied   State = "denied"
	StateExpired  State = "expired"
	// StateRevoked means the portal refused the access token this server
	// holds. The server has to pair again to get a new one.
	StateRevoked State = "revoked"
)

// Step says what a pending pairing waits for.
type Step string

const (
	// StepConfirmation waits for the tester to confirm the code on the portal.
	StepConfirmation Step = "awaiting_confirmation"
	// StepApproval waits for the product team to approve the confirmed device.
	StepApproval Step = "awaiting_approval"
)

// Identity is the non-secret account description returned by the portal.
type Identity struct {
	Subject string `json:"subject,omitempty" doc:"The stable portal account identifier."`
	Name    string `json:"name,omitempty"    doc:"The safe portal account display name."`
}

// Product is the portal product this server is registered under.
type Product struct {
	Slug string `json:"slug,omitempty"`
	Name string `json:"name,omitempty"`
}

// Device is the portal's record of this server.
type Device struct {
	Identifier string `json:"identifier,omitempty"`
}

// Status is the only pairing data safe to return through the local API.
type Status struct {
	State                   State     `json:"state"`
	Step                    Step      `json:"step,omitempty"`
	UserCode                string    `json:"user_code,omitempty"`
	VerificationURI         string    `json:"verification_uri,omitempty"`
	VerificationURIComplete string    `json:"verification_uri_complete,omitempty"`
	ExpiresAt               time.Time `json:"expires_at,omitzero"`
	IntervalSeconds         int       `json:"interval_seconds,omitempty"`
	Identity                *Identity `json:"identity,omitempty"`
	Product                 *Product  `json:"product,omitempty"`
	Device                  *Device   `json:"device,omitempty"`
	// AccessSuspended says the portal refused this paired server because the
	// tester's access to the product is suspended. The pairing stays and
	// works again once the team restores the access.
	AccessSuspended bool `json:"access_suspended,omitempty"`
}

// Store keeps one P-256 private key, the opaque access token bound to it and
// the current pairing state in one atomically replaced private file.
type Store struct {
	mu   sync.RWMutex
	path string
	doc  document
	key  *ecdsa.PrivateKey
	// skew is the portal's clock minus this server's clock, in nanoseconds,
	// learned from a refused proof. Every proof is dated with it.
	skew      atomic.Int64
	recovered string
	// suspended is the last thing the portal said about the tester's access
	// (`access_suspended`). It is not saved: the next request learns it again.
	suspended atomic.Bool
}

type document struct {
	Version     int              `json:"version"`
	PrivateKey  string           `json:"private_key"`
	AccessToken string           `json:"access_token,omitempty"`
	State       State            `json:"state"`
	Pairing     *pairingDocument `json:"pairing,omitempty"`
	Identity    *Identity        `json:"identity,omitempty"`
	Product     *Product         `json:"product,omitempty"`
	Device      *Device          `json:"device,omitempty"`
}

type pairingDocument struct {
	DeviceCode              string    `json:"device_code"`
	UserCode                string    `json:"user_code"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete,omitempty"`
	Step                    Step      `json:"step,omitempty"`
	ExpiresAt               time.Time `json:"expires_at"`
	IntervalSeconds         int       `json:"interval_seconds"`
	NextPollAt              time.Time `json:"next_poll_at"`
}

// approval is what the portal says about this server when it hands out a token.
type approval struct {
	token    string
	identity *Identity
	product  *Product
	device   *Device
}

// Path returns the pairing file beside the resource overlay. The operator's
// configuration may sit in a folder the server cannot write.
func Path(overlay string) string {
	return filepath.Join(filepath.Dir(overlay), pairingFile)
}

// Open loads a pairing store and creates its local P-256 key when absent. A
// file this build cannot read is renamed to "<name>.broken-<time>" and the
// store starts unpaired, so a damaged pairing never stops the server.
// Recovered names the renamed file.
func Open(path string) (*Store, error) {
	store := &Store{path: path}

	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err = store.initialize(); err != nil {
			return nil, err
		}

		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Beta Portal pairing: %w", err)
	}

	doc, key, damage := decodeDocument(content)
	if damage != nil {
		aside := path + ".broken-" + time.Now().UTC().Format(brokenTimestamp)
		// The damaged file may still hold the key and the token.
		if chmodErr := os.Chmod(path, fileMode); chmodErr != nil && !errors.Is(chmodErr, os.ErrInvalid) {
			return nil, fmt.Errorf("protect the unreadable Beta Portal pairing: %w", chmodErr)
		}
		if err = os.Rename(path, aside); err != nil {
			return nil, fmt.Errorf("move the unreadable Beta Portal pairing aside (%w): %w", damage, err)
		}
		store.recovered = aside
		if err = store.initialize(); err != nil {
			return nil, err
		}

		return store, nil
	}
	store.doc, store.key = doc, key

	if err = protect(path); err != nil {
		return nil, err
	}

	return store, nil
}

// ErrNotProtected says that a pairing file cannot be set to its file mode.
var ErrNotProtected = errors.New("the Beta Portal pairing cannot be protected")

// chmod is os.Chmod. A test replaces it to stand for a read-only file system.
var chmod = os.Chmod

// protect sets the pairing file to fileMode. A file that already has it is
// not changed, so a protected file on a read-only file system opens.
func protect(path string) error {
	info, err := os.Stat(path)
	if err == nil && info.Mode().Perm() == fileMode {
		return nil
	}
	if err = chmod(path, fileMode); err != nil && !errors.Is(err, os.ErrInvalid) {
		return fmt.Errorf("%w: %w", ErrNotProtected, err)
	}

	return nil
}

// HoldsToken reports whether the pairing file at path holds an access token,
// which the portal accepts until a disconnect revokes it. It reads the file
// and writes nothing. A missing file holds none. A file it cannot read or
// decode is an error, because it may still hold one.
func HoldsToken(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read the Beta Portal pairing: %w", err)
	}

	doc, _, err := decodeDocument(content)
	if err != nil {
		return false, err
	}

	return doc.AccessToken != "", nil
}

// Recovered names the file an unreadable pairing was moved to when the store
// opened. It is empty when the pairing file was read as it was.
func (s *Store) Recovered() string {
	return s.recovered
}

func decodeDocument(content []byte) (document, *ecdsa.PrivateKey, error) {
	var doc document
	if err := json.Unmarshal(content, &doc); err != nil {
		return document{}, nil, fmt.Errorf("decode the Beta Portal pairing: %w", err)
	}
	if doc.Version != documentVersion {
		return document{}, nil, fmt.Errorf("decode the Beta Portal pairing: unsupported format %d", doc.Version)
	}
	key, err := decodeKey(doc.PrivateKey)
	if err != nil {
		return document{}, nil, fmt.Errorf("decode the Beta Portal pairing key: %w", err)
	}
	if err = validateDocument(doc); err != nil {
		return document{}, nil, fmt.Errorf("decode the Beta Portal pairing: %w", err)
	}

	return doc, key, nil
}

func (s *Store) initialize() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate the Beta Portal pairing key: %w", err)
	}
	encoded, err := encodeKey(key)
	if err != nil {
		return err
	}

	s.key = key
	s.doc = document{Version: documentVersion, PrivateKey: encoded, State: StateUnpaired}

	return s.save(s.doc)
}

// Status returns no device code, token or private key.
func (s *Store) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := statusOf(s.doc)
	status.AccessSuspended = status.State == StatePaired && s.suspended.Load()

	return status
}

func statusOf(doc document) Status {
	status := Status{
		State: doc.State, Identity: clone(doc.Identity),
		Product: clone(doc.Product), Device: clone(doc.Device),
	}
	if doc.Pairing != nil && doc.State == StatePending {
		status.Step = doc.Pairing.Step
		if status.Step == "" {
			status.Step = StepConfirmation
		}
		status.UserCode = doc.Pairing.UserCode
		status.VerificationURI = doc.Pairing.VerificationURI
		status.VerificationURIComplete = doc.Pairing.VerificationURIComplete
		status.ExpiresAt = doc.Pairing.ExpiresAt
		status.IntervalSeconds = doc.Pairing.IntervalSeconds
	}

	return status
}

func clone[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copied := *value

	return &copied
}

func (s *Store) pairing() (pairingDocument, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.doc.Pairing == nil || s.doc.State != StatePending {
		return pairingDocument{}, false
	}

	return *s.doc.Pairing, true
}

// start saves a new pending pairing. It refuses while the server is paired, so
// only an explicit disconnect ends a working pairing. A code asked for with a
// key the store has since replaced is dropped, because the portal binds the
// code to that key.
func (s *Store) start(pairing pairingDocument, key *ecdsa.PrivateKey) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.doc.State == StatePaired {
		return Status{}, ErrAlreadyPaired
	}
	if key != nil && s.key != key {
		return statusOf(s.doc), nil
	}
	next := s.doc
	next.AccessToken = ""
	next.Identity = nil
	next.Product = nil
	next.Device = nil
	next.State = StatePending
	next.Pairing = &pairing
	if err := s.save(next); err != nil {
		return Status{}, err
	}
	s.doc = next

	return statusOf(s.doc), nil
}

// current reports whether deviceCode still names the pending pairing. A poll
// answered after a disconnect or a new start must not change the newer state.
func (s *Store) current(deviceCode string) bool {
	return s.doc.State == StatePending && s.doc.Pairing != nil && s.doc.Pairing.DeviceCode == deviceCode
}

func (s *Store) schedule(deviceCode string, nextPoll time.Time, interval int, step Step) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.current(deviceCode) {
		return nil
	}
	next := s.doc
	pairing := *s.doc.Pairing
	pairing.IntervalSeconds = interval
	pairing.NextPollAt = nextPoll
	if step != "" {
		pairing.Step = step
	}
	next.Pairing = &pairing
	if err := s.save(next); err != nil {
		return err
	}
	s.doc = next

	return nil
}

func (s *Store) approve(deviceCode string, granted approval) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.current(deviceCode) {
		return nil
	}
	next := s.doc
	next.AccessToken = granted.token
	next.Identity = clone(granted.identity)
	next.Product = clone(granted.product)
	next.Device = clone(granted.device)
	next.State = StatePaired
	next.Pairing = nil
	if err := s.save(next); err != nil {
		return err
	}
	s.doc = next

	return nil
}

func (s *Store) terminal(deviceCode string, state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.current(deviceCode) {
		return nil
	}
	next := s.doc
	next.AccessToken = ""
	next.Identity = nil
	next.Product = nil
	next.Device = nil
	next.State = state
	next.Pairing = nil
	if err := s.save(next); err != nil {
		return err
	}
	s.doc = next

	return nil
}

// revoke drops a token the portal refused. The account the server was paired
// as stays, so the page can say which pairing stopped working.
func (s *Store) revoke(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.doc.State != StatePaired || s.doc.AccessToken != token {
		return nil
	}
	next := s.doc
	next.AccessToken = ""
	next.State = StateRevoked
	if err := s.save(next); err != nil {
		return err
	}
	s.doc = next
	s.suspended.Store(false)

	return nil
}

func (s *Store) access() (string, *ecdsa.PrivateKey) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.doc.AccessToken, s.key
}

func (s *Store) keyOnly() *ecdsa.PrivateKey {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.key
}

// ClearLocal removes local authorization and rotates the proof key. If the
// remote revocation fails afterwards, this process still cannot use the old
// token, because the store holds neither the token nor the key it is bound to.
func (s *Store) ClearLocal() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("rotate the Beta Portal pairing key: %w", err)
	}
	encoded, err := encodeKey(key)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next := document{Version: documentVersion, PrivateKey: encoded, State: StateUnpaired}
	if err = s.save(next); err != nil {
		return err
	}
	s.doc = next
	s.key = key
	s.suspended.Store(false)

	return nil
}

func (s *Store) save(doc document) error {
	if err := validateDocument(doc); err != nil {
		return fmt.Errorf("validate the Beta Portal pairing: %w", err)
	}
	content, err := json.Marshal(doc) //nolint:gosec // written only to the private pairing file
	if err != nil {
		return fmt.Errorf("encode the Beta Portal pairing: %w", err)
	}
	content = append(content, '\n')

	if err = os.MkdirAll(filepath.Dir(s.path), dirMode); err != nil {
		return fmt.Errorf("create the Beta Portal pairing directory: %w", err)
	}
	if err = atomicfile.Write(s.path, content, fileMode); err != nil {
		return fmt.Errorf("store the Beta Portal pairing: %w", err)
	}

	return nil
}

func encodeKey(key *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", fmt.Errorf("encode the Beta Portal pairing key: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(der), nil
}

func decodeKey(encoded string) (*ecdsa.PrivateKey, error) {
	der, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("invalid private key encoding")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, errors.New("invalid private key")
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("private key is not P-256")
	}

	return key, nil
}

func validateDocument(doc document) error {
	if doc.AccessToken != "" && invalidHeaderValue(doc.AccessToken, maxSecret) {
		return errors.New("invalid access token")
	}
	if err := validateMetadata(doc.Identity, doc.Product, doc.Device); err != nil {
		return err
	}

	switch doc.State {
	case StatePending:
		return validatePending(doc)
	case StatePaired:
		if doc.AccessToken == "" || doc.Pairing != nil {
			return errors.New("invalid approved pairing")
		}
	case StateRevoked:
		if doc.AccessToken != "" || doc.Pairing != nil {
			return errors.New("invalid revoked pairing")
		}
	case StateUnpaired, StateDenied, StateExpired:
		if doc.AccessToken != "" || doc.Pairing != nil || doc.Identity != nil ||
			doc.Product != nil || doc.Device != nil {
			return errors.New("invalid terminal pairing")
		}
	default:
		return errors.New("invalid pairing state")
	}

	return nil
}

func validatePending(doc document) error {
	pairing := doc.Pairing
	if pairing == nil || doc.AccessToken != "" || pairing.DeviceCode == "" ||
		len(pairing.DeviceCode) > maxSecret || pairing.UserCode == "" ||
		len(pairing.UserCode) > maxMetadata || pairing.VerificationURI == "" ||
		len(pairing.VerificationURIComplete) > maxSecret ||
		pairing.ExpiresAt.IsZero() || pairing.IntervalSeconds <= 0 ||
		pairing.Step != "" && pairing.Step != StepConfirmation && pairing.Step != StepApproval {
		return errors.New("invalid pending pairing")
	}

	return nil
}

func validateMetadata(identity *Identity, product *Product, device *Device) error {
	if identity != nil && (len(identity.Subject) > maxMetadata || len(identity.Name) > maxMetadata) {
		return errors.New("invalid identity metadata")
	}
	if product != nil && (len(product.Slug) > maxMetadata || len(product.Name) > maxMetadata) {
		return errors.New("invalid product metadata")
	}
	if device != nil && len(device.Identifier) > maxMetadata {
		return errors.New("invalid device metadata")
	}

	return nil
}

func invalidHeaderValue(value string, limit int) bool {
	if value == "" || len(value) > limit || strings.TrimSpace(value) != value {
		return true
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return true
		}
	}

	return false
}
