// Package store keeps the cameras this module paired: which core camera each
// one feeds, its token and the fingerprint it bound, in one JSON file in the
// module's data directory.
//
// What a camera last reported is kept in memory only. It changes with every
// frame, and writing it would wear the card of a small server for nothing a
// restart needs.
package store

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/buddy3d-camera/internal/camproto"
)

// The cloud's rules for a token, which the camera's firmware expects.
const (
	tokenLength    = 20
	tokenAlphabet  = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	minFingerprint = 16
)

// PairingWindow is how long a new token waits for the camera's first request.
// A token no camera used in that time is refused, as the cloud refuses it.
const PairingWindow = 15 * time.Minute

const fileName = "cameras.json"

// The refusals a camera's request can meet.
var (
	ErrUnknownToken   = errors.New("store: no camera holds this token")
	ErrFingerprint    = errors.New("store: the fingerprint is not the one this token is bound to")
	ErrWindowClosed   = errors.New("store: the token was not used within the pairing window")
	ErrNoSuchCamera   = errors.New("store: no camera of that name is paired")
	ErrBadFingerprint = errors.New("store: the fingerprint is too short")
)

// SetSettings keeps what a person chose for the camera name. The snapshot
// interval is 0 for the module's default, 10 to 254 seconds, or SnapshotsOff.
func (s *Store) SetSettings(name string, settings Settings) error {
	if seconds := settings.SnapshotInterval; seconds > SnapshotsOff || seconds != 0 && seconds < MinSnapshotInterval {
		return ErrBadInterval
	}

	if !slices.Contains(streams, settings.Stream) {
		return ErrBadStream
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index := slices.IndexFunc(s.cameras, func(c Camera) bool { return c.Name == name })
	if index < 0 {
		return ErrNoSuchCamera
	}

	next := slices.Clone(s.cameras)
	next[index].Settings = settings

	if err := s.save(next); err != nil {
		return err
	}

	s.cameras = next

	return nil
}

// SetFeatures keeps what the camera name said it supports. It writes only
// when the list changed, since the camera reports it on every session.
func (s *Store) SetFeatures(name string, features []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := slices.IndexFunc(s.cameras, func(c Camera) bool { return c.Name == name })
	if index < 0 {
		return ErrNoSuchCamera
	}

	if slices.Equal(s.cameras[index].Features, features) {
		return nil
	}

	next := slices.Clone(s.cameras)
	next[index].Features = slices.Clone(features)

	if err := s.save(next); err != nil {
		return err
	}

	s.cameras = next

	return nil
}

// Settings are what a person chose for the camera name, zero for a camera
// that is not paired.
func (s *Store) Settings(name string) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := slices.IndexFunc(s.cameras, func(c Camera) bool { return c.Name == name })
	if index < 0 {
		return Settings{}
	}

	return s.cameras[index].Settings
}

// VideoQuality is the video quality the camera name last reported, or empty.
func (s *Store) VideoQuality(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if seen := s.seen[name]; seen != nil && seen.Status != nil {
		return seen.Status.VideoQuality
	}

	return ""
}

// Camera is one paired camera.
type Camera struct {
	Settings

	// Name is the core camera this one feeds, a push camera.
	Name    string `json:"name"`
	Printer string `json:"printer"`
	Token   string `json:"token"`
	// Fingerprint is empty until the camera's first request binds it.
	Fingerprint string    `json:"fingerprint,omitempty"`
	Created     time.Time `json:"created"`
	// Features are what the camera said it supports, such as RotationZ.
	Features []string `json:"features,omitempty"`
}

// The snapshot intervals a camera takes: seconds from MinSnapshotInterval to
// MaxSnapshotInterval, and SnapshotsOff, which the firmware reads as sending
// none. The firmware sends no faster than every 10 seconds.
const (
	MinSnapshotInterval = 10
	MaxSnapshotInterval = 254
	SnapshotsOff        = 255
)

// The refusals of a setting the camera does not take.
var (
	ErrBadInterval = errors.New("store: snapshot_interval is 0 for the default, 10 to 254 seconds, or 255 for none")
	ErrBadStream   = errors.New("store: stream is webrtc, rtsp or off")
)

// streams are the streams a camera runs: empty and webrtc are WebRTC.
var streams = []string{"", "webrtc", camproto.StreamRTSP, camproto.StreamOff}

// Seen is what a camera last reported, kept in memory.
type Seen struct {
	LastSeen  time.Time        `json:"last_seen,omitzero"`
	LastFrame time.Time        `json:"last_frame,omitzero"`
	Model     string           `json:"model,omitempty"`
	Status    *camproto.Status `json:"status,omitempty"`
	// Problem is why the last frame did not reach the core, empty once one
	// did.
	Problem string `json:"problem,omitempty"`
}

// Store is the paired cameras and what each last reported.
type Store struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	cameras []Camera
	seen    map[string]*Seen
}

// Open reads the store in dir, or starts an empty one.
func Open(dir string, now func() time.Time) (*Store, error) {
	s := &Store{path: filepath.Join(dir, fileName), now: now, seen: map[string]*Seen{}}

	held, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}

	if err != nil {
		return nil, fmt.Errorf("store: read %s: %w", fileName, err)
	}

	if err = json.Unmarshal(held, &s.cameras); err != nil {
		return nil, fmt.Errorf("store: %s is not a list of cameras: %w", fileName, err)
	}

	return s, nil
}

// ErrBadToken reports a token that is not the cloud's shape.
var (
	ErrBadToken   = errors.New("store: a token is 20 letters and digits")
	ErrTokenTaken = errors.New("store: another camera holds this token")
)

// Pair makes a new token for the core camera name, which watches printer, or
// takes token when it is not empty, for a camera that already holds one. A
// camera already paired under that name gets the new token and must bind
// again.
func (s *Store) Pair(name, printer, token string) (Camera, error) {
	if token == "" {
		made, err := newToken()
		if err != nil {
			return Camera{}, err
		}

		token = made
	}

	if !validToken(token) {
		return Camera{}, ErrBadToken
	}

	paired := Camera{Name: name, Printer: printer, Token: token, Created: s.now().UTC()}

	s.mu.Lock()
	defer s.mu.Unlock()

	// A camera paired again keeps what a person chose for it.
	if index := slices.IndexFunc(s.cameras, func(c Camera) bool { return c.Name == name }); index >= 0 {
		paired.Settings = s.cameras[index].Settings
	}

	next := slices.DeleteFunc(slices.Clone(s.cameras), func(c Camera) bool { return c.Name == name })
	if slices.ContainsFunc(next, func(c Camera) bool { return c.Token == token }) {
		return Camera{}, ErrTokenTaken
	}

	next = append(next, paired)

	if err := s.save(next); err != nil {
		return Camera{}, err
	}

	s.cameras = next
	delete(s.seen, name)

	return paired, nil
}

// Remove forgets the camera name and its token.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := slices.DeleteFunc(slices.Clone(s.cameras), func(c Camera) bool { return c.Name == name })
	if len(next) == len(s.cameras) {
		return ErrNoSuchCamera
	}

	if err := s.save(next); err != nil {
		return err
	}

	s.cameras = next
	delete(s.seen, name)

	return nil
}

// Authenticate finds the camera holding token. The first request binds its
// fingerprint, and every later one must carry the same.
func (s *Store) Authenticate(token, fingerprint string) (Camera, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := slices.IndexFunc(s.cameras, func(c Camera) bool {
		return subtle.ConstantTimeCompare([]byte(c.Token), []byte(token)) == 1
	})
	if token == "" || index < 0 {
		return Camera{}, ErrUnknownToken
	}

	held := s.cameras[index]

	if held.Fingerprint != "" {
		if subtle.ConstantTimeCompare([]byte(held.Fingerprint), []byte(fingerprint)) != 1 {
			return Camera{}, ErrFingerprint
		}

		return held, nil
	}

	if s.now().Sub(held.Created) > PairingWindow {
		return Camera{}, ErrWindowClosed
	}

	if len(fingerprint) < minFingerprint {
		return Camera{}, ErrBadFingerprint
	}

	next := slices.Clone(s.cameras)
	next[index].Fingerprint = fingerprint

	if err := s.save(next); err != nil {
		return Camera{}, err
	}

	s.cameras = next

	return next[index], nil
}

// Note records what a camera reported. It changes nothing on disk.
func (s *Store) Note(name string, change func(*Seen)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	held := s.seen[name]
	if held == nil {
		held = &Seen{}
		s.seen[name] = held
	}

	held.LastSeen = s.now().UTC()
	change(held)
}

// Entry is one camera as the module's routes list it. The token is shown
// only while no camera has bound it, which is when a person still needs it.
type Entry struct {
	Seen

	Name    string    `json:"name"`
	Printer string    `json:"printer"`
	Bound   bool      `json:"bound"`
	Token   string    `json:"token,omitempty"`
	Created time.Time `json:"created"`
	// Settings are what a person chose for the camera.
	Settings Settings `json:"settings"`
	// Features are what the camera said it supports, such as RotationZ.
	Features []string `json:"features,omitempty"`
}

// Settings are the choices a person makes for one camera.
type Settings struct {
	// SnapshotInterval is 0 for the module's default, 10 to 254 seconds, or
	// 255 for no pictures.
	SnapshotInterval uint32 `json:"snapshot_interval"`
	// Stream is the one stream the camera runs: webrtc, which the live view
	// needs, rtsp, or off. Empty is webrtc.
	Stream string `json:"stream,omitempty"`
}

// List is every paired camera, by name.
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Entry, 0, len(s.cameras))

	for _, c := range s.cameras {
		entry := Entry{
			Name: c.Name, Printer: c.Printer, Bound: c.Fingerprint != "", Created: c.Created,
			Settings: c.Settings, Features: c.Features,
		}
		if !entry.Bound {
			entry.Token = c.Token
		}

		if seen := s.seen[c.Name]; seen != nil {
			entry.Seen = *seen
		}

		out = append(out, entry)
	}

	slices.SortFunc(out, func(a, b Entry) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		}

		return 0
	})

	return out
}

// Has reports whether name is paired.
func (s *Store) Has(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.ContainsFunc(s.cameras, func(c Camera) bool { return c.Name == name })
}

// save writes the list through a temporary file, so a crash leaves the old
// list or the new one.
func (s *Store) save(cameras []Camera) error {
	held, err := json.MarshalIndent(cameras, "", "  ")
	if err != nil {
		return err
	}

	temporary := s.path + ".tmp"
	if err = os.WriteFile(temporary, held, 0o600); err != nil {
		return fmt.Errorf("store: write %s: %w", fileName, err)
	}

	if err = os.Rename(temporary, s.path); err != nil {
		return fmt.Errorf("store: replace %s: %w", fileName, err)
	}

	return nil
}

func validToken(token string) bool {
	if len(token) != tokenLength {
		return false
	}

	for _, r := range token {
		if !strings.ContainsRune(tokenAlphabet, r) {
			return false
		}
	}

	return true
}

func newToken() (string, error) {
	out := make([]byte, tokenLength)

	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(tokenAlphabet))))
		if err != nil {
			return "", fmt.Errorf("store: make a token: %w", err)
		}

		out[i] = tokenAlphabet[n.Int64()]
	}

	return string(out), nil
}
