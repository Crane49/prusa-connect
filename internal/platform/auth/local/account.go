package local

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// The refusals a store gives. Callers inspect them with errors.Is, and the
// login path turns every one of them into the single refusal in accounts.go.
var (
	// ErrNoAccount reports a username or an id no row answers to.
	ErrNoAccount = errors.New("local: no such account")
	// ErrUsernameTaken reports a username another account already holds.
	ErrUsernameTaken = errors.New("local: that username is taken")
	// ErrIDTaken reports an id another account already holds. The first
	// account's id is fixed, so a second attempt to create the first
	// administrator collides on it.
	ErrIDTaken = errors.New("local: that account id is taken")
	// ErrLastAccount reports deleting the only account there is. Nothing could
	// then log in, and the setup path would reopen for whoever reached it
	// first.
	ErrLastAccount = errors.New("local: the last account cannot be deleted")
	// ErrPasswordChanged reports a password write that expected one hash and
	// found another. Somebody changed the password between the check and the
	// write.
	ErrPasswordChanged = errors.New("local: the password changed before the write")
)

// Bounds on a username. The upper one is RFC 5321 section 4.5.3.1.3's limit on
// a mail path, which is the longest thing anybody types into this field.
const (
	// MinUsernameBytes is one printable character.
	MinUsernameBytes = 1
	// MaxUsernameBytes bounds what is stored and compared.
	MaxUsernameBytes = 254
	// MaxDisplayNameBytes bounds the label a UI renders.
	MaxDisplayNameBytes = 128
)

// idAlphabet and idBodyLen mint an account id. The characters are the DNS-1123
// label set without the hyphen, because the id is a resource identifier
// everywhere else in this system. Twenty of them carry just over 103 bits.
const (
	idAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	idBodyLen  = 20
	// idPrefix keeps a minted id from starting with a digit and marks it as
	// this store's in a log line.
	idPrefix = "u"

	idRadix       = len(idAlphabet)
	rejectionBase = 256
)

// Account is one person this server holds a password for.
//
// ID is the subject the authorization graph is keyed by, and it never changes:
// renaming a person must not move their grants. Username is what they type, and
// it is stored folded so the comparison at login is a plain equality.
type Account struct {
	ID          string
	Username    string
	DisplayName string
	// Hash is the argon2id PHC string. It is the only secret here and is never
	// rendered anywhere.
	Hash       string
	CreatedAt  time.Time
	PasswordAt time.Time
	// Generation is the generation of the account's credentials, read in the
	// same statement as the hash. A password change raises it, and a session
	// minted from this read carries it.
	Generation int64
}

// Store holds account rows. It is a port: the in-memory implementation here
// serves the tests, and a persistent one plugs in without this package
// changing.
//
// Every lookup reports a miss as ErrNoAccount. Any other error means the store
// could not answer, and a login treats that as a refusal.
type Store interface {
	Count(ctx context.Context) (int, error)
	ByUsername(ctx context.Context, username string) (Account, error)
	ByID(ctx context.Context, id string) (Account, error)
	List(ctx context.Context) ([]Account, error)
	// Create writes a row, refusing an id or a username already held.
	Create(ctx context.Context, account Account) error
	// SetPassword replaces one row's hash and records when. It ends no
	// credential, so it is for a rehash of the same password. A non-empty
	// expected hash makes the write land only over that hash, and otherwise
	// report ErrPasswordChanged.
	SetPassword(ctx context.Context, id, expected, hash string, at time.Time) error
	// ReplacePassword replaces one row's hash and raises the generation of the
	// account's credentials, in one transaction. Every credential minted before
	// is then refused. expected works as it does for SetPassword. It returns the
	// generation it wrote, read in the same transaction.
	ReplacePassword(ctx context.Context, id, expected, hash string, at time.Time) (int64, error)
	// Delete removes a row, refusing the last one there is.
	Delete(ctx context.Context, id string) error
}

// FoldUsername is the one spelling a username is stored and compared in. It is
// case-folded and trimmed, so a person who capitalised their email at the
// keyboard still logs in, and two accounts cannot differ by case alone.
func FoldUsername(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// ValidateUsername reports whether a username is one this store will take.
func ValidateUsername(folded string) error {
	switch {
	case len(folded) < MinUsernameBytes:
		return fmt.Errorf("%w: the username is empty", ErrInvalidAccount)
	case len(folded) > MaxUsernameBytes:
		return fmt.Errorf("%w: the username is %d bytes, the limit is %d",
			ErrInvalidAccount, len(folded), MaxUsernameBytes)
	case strings.ContainsAny(folded, " \t\r\n"):
		return fmt.Errorf("%w: the username carries whitespace", ErrInvalidAccount)
	default:
		return nil
	}
}

// NewID mints an account id. It is random and not derived from the username, so
// an id that reached a log, a grant or a URL says nothing about who the person
// is, and it survives them changing their address.
func NewID() (string, error) {
	body, err := randomAlphabet(idBodyLen)
	if err != nil {
		return "", err
	}

	return idPrefix + body, nil
}

// MemoryStore keeps accounts for the life of the process, for the tests. A
// deployment stores them, because an account store that empties on restart
// reopens the setup path every time.
type MemoryStore struct {
	mu   sync.RWMutex
	rows map[string]Account
	fail error
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{rows: make(map[string]Account)} }

var _ Store = (*MemoryStore)(nil)

// Count returns how many accounts exist.
func (s *MemoryStore) Count(context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.fail != nil {
		return 0, s.fail
	}

	return len(s.rows), nil
}

// ByUsername returns the account holding a folded username.
func (s *MemoryStore) ByUsername(_ context.Context, username string) (Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.fail != nil {
		return Account{}, s.fail
	}

	for _, row := range s.rows {
		if row.Username == username {
			return row, nil
		}
	}

	return Account{}, ErrNoAccount
}

// ByID returns one account.
func (s *MemoryStore) ByID(_ context.Context, id string) (Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.fail != nil {
		return Account{}, s.fail
	}

	row, ok := s.rows[id]
	if !ok {
		return Account{}, ErrNoAccount
	}

	return row, nil
}

// List returns every account, sorted by username so a page is stable.
func (s *MemoryStore) List(context.Context) ([]Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.fail != nil {
		return nil, s.fail
	}

	out := slices.Collect(maps.Values(s.rows))
	slices.SortFunc(out, func(a, b Account) int { return strings.Compare(a.Username, b.Username) })

	return out, nil
}

// Create writes a row, refusing an id or a username already held.
func (s *MemoryStore) Create(_ context.Context, account Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fail != nil {
		return s.fail
	}

	if _, held := s.rows[account.ID]; held {
		return fmt.Errorf("%w: %s", ErrIDTaken, account.ID)
	}

	for _, row := range s.rows {
		if row.Username == account.Username {
			return ErrUsernameTaken
		}
	}

	s.rows[account.ID] = account

	return nil
}

// SetPassword replaces one row's hash, over the expected one when it is named.
func (s *MemoryStore) SetPassword(_ context.Context, id, expected, hash string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fail != nil {
		return s.fail
	}

	row, ok := s.rows[id]
	if !ok {
		return ErrNoAccount
	}

	if expected != "" && row.Hash != expected {
		return ErrPasswordChanged
	}

	row.Hash = hash
	row.PasswordAt = at
	s.rows[id] = row

	return nil
}

// ReplacePassword replaces one row's hash and raises its generation.
func (s *MemoryStore) ReplacePassword(ctx context.Context, id, expected, hash string, at time.Time) (int64, error) {
	if err := s.SetPassword(ctx, id, expected, hash, at); err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.rows[id]
	row.Generation++
	s.rows[id] = row

	return row.Generation, nil
}

// Delete removes a row, refusing the last one there is.
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fail != nil {
		return s.fail
	}

	if _, ok := s.rows[id]; !ok {
		return ErrNoAccount
	}

	if len(s.rows) == 1 {
		return ErrLastAccount
	}

	delete(s.rows, id)

	return nil
}

// Break makes every method report err, standing in for a store this node cannot
// read. Passing nil restores it.
func (s *MemoryStore) Break(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.fail = err
}

// randomAlphabet draws characters straight from crypto/rand with rejection
// sampling, because a modulo over a byte biases the low characters.
func randomAlphabet(n int) (string, error) {
	// The largest multiple of the radix below 256. A byte at or above it is
	// redrawn, so every character is equally likely.
	ceiling := rejectionBase - rejectionBase%idRadix

	out := make([]byte, 0, n)
	buf := make([]byte, n)

	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("local: read random: %w", err)
		}

		for _, b := range buf {
			if int(b) >= ceiling {
				continue
			}

			out = append(out, idAlphabet[int(b)%idRadix])
			if len(out) == n {
				break
			}
		}
	}

	return string(out), nil
}
