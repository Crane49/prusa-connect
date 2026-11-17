package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
)

// Errors reported while hashing or checking a password. A caller turns all of
// them into one refusal. They are separate here so a log line can say which.
var (
	// ErrPasswordTooShort reports a password below MinPasswordBytes.
	ErrPasswordTooShort = errors.New("auth: password too short")
	// ErrPasswordTooLong reports a password above MaxPasswordBytes.
	ErrPasswordTooLong = errors.New("auth: password too long")
	// ErrPasswordMismatch reports a password that does not verify.
	ErrPasswordMismatch = errors.New("auth: password does not match")
	// ErrUnsupportedHash reports a stored hash this build cannot read.
	ErrUnsupportedHash = errors.New("auth: unsupported password hash")
)

// Parallelism is one lane, so the cost is predictable on a single-core
// machine.
const Parallelism uint8 = 1

// The bounds on a password and on the hash of it.
const (
	// MinPasswordBytes and MaxPasswordBytes bound what is hashed. The upper
	// bound is bcrypt's, kept so a row from an existing install can be
	// re-hashed without truncating the password.
	MinPasswordBytes = 8
	MaxPasswordBytes = 72

	// MaxCostMemoryKiB and MaxCostPasses are the largest cost a hasher takes.
	// Every login spends the memory block at once, so a larger block is a
	// mistake in the configuration and not a stronger hash.
	MaxCostMemoryKiB uint32 = 1 << 20
	MaxCostPasses    uint32 = 100

	saltLen = 16
	keyLen  = 32

	hashAlgorithm = "argon2id"
	hashFields    = 6
)

// ErrWeakCost reports an argon2id cost below every setting OWASP lists, or
// above the upper limits this package sets.
var ErrWeakCost = errors.New("auth: the argon2id cost is not allowed")

// Cost is the work one argon2id hash does. MemoryKiB is the block of memory
// the hash fills, and Passes is how many times it goes over that block.
type Cost struct {
	MemoryKiB uint32
	Passes    uint32
}

// DefaultCost is the cost this build hashes at when the configuration names
// none. The printer build uses less memory and more passes.
func DefaultCost() Cost {
	return Cost{MemoryKiB: buildprofile.PasswordMemoryKiB, Passes: buildprofile.PasswordPasses}
}

// owaspCosts are the argon2id settings in the OWASP password storage cheat
// sheet, largest memory first. OWASP lists them as equal in strength, each
// with one lane.
//
//nolint:gochecknoglobals,mnd // the table OWASP publishes, read and never written.
var owaspCosts = []Cost{
	{MemoryKiB: 46 * 1024, Passes: 1},
	{MemoryKiB: 19 * 1024, Passes: 2},
	{MemoryKiB: 12 * 1024, Passes: 3},
	{MemoryKiB: 9 * 1024, Passes: 4},
	{MemoryKiB: 7 * 1024, Passes: 5},
}

// Check refuses a cost that is below every OWASP setting. A cost passes when
// it has at least the memory and at least the passes of one of them.
func (c Cost) Check() error {
	if c.MemoryKiB > MaxCostMemoryKiB {
		return fmt.Errorf("%w: %d KiB of memory is more than the upper limit of %d KiB",
			ErrWeakCost, c.MemoryKiB, MaxCostMemoryKiB)
	}

	if c.Passes > MaxCostPasses {
		return fmt.Errorf("%w: %d passes is more than the upper limit of %d",
			ErrWeakCost, c.Passes, MaxCostPasses)
	}

	// The table is ordered by memory, largest first, so the first row this
	// memory reaches asks for the fewest passes.
	var passes uint32

	for _, minimum := range owaspCosts {
		if c.MemoryKiB >= minimum.MemoryKiB {
			passes = minimum.Passes

			break
		}
	}

	smallest := owaspCosts[len(owaspCosts)-1]

	switch {
	case passes == 0:
		return fmt.Errorf("%w: argon2id with %d KiB with %s is below the OWASP minimum. "+
			"It needs at least %d KiB of memory, with at least %s",
			ErrWeakCost, c.MemoryKiB, passCount(c.Passes), smallest.MemoryKiB, passCount(smallest.Passes))
	case c.Passes < passes:
		return fmt.Errorf("%w: argon2id with %d KiB with %s is below the OWASP minimum. "+
			"At %d KiB it needs at least %s",
			ErrWeakCost, c.MemoryKiB, passCount(c.Passes), c.MemoryKiB, passCount(passes))
	default:
		return nil
	}
}

// passCount writes a number of passes the way a person reads it.
func passCount(passes uint32) string {
	if passes == 1 {
		return "1 pass"
	}

	return fmt.Sprintf("%d passes", passes)
}

// keyDerivation has the signature of argon2.IDKey. A hasher holds one so a
// test in this package can count the derivations a verify runs and read the
// cost of each. Every hasher outside the tests holds argon2.IDKey.
type keyDerivation func(password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte

// Hasher makes password hashes at one cost. It also holds a dummy hash at the
// same cost, which a login for an unknown account is checked against.
type Hasher struct {
	cost   Cost
	dummy  string
	derive keyDerivation
}

// NewHasher returns a hasher at the given cost, or ErrWeakCost.
func NewHasher(cost Cost) (*Hasher, error) {
	if err := cost.Check(); err != nil {
		return nil, err
	}

	return &Hasher{cost: cost, dummy: randomDummyHash(cost), derive: argon2.IDKey}, nil
}

// NewDefaultHasher returns a hasher at DefaultCost.
func NewDefaultHasher() *Hasher {
	cost := DefaultCost()

	return &Hasher{cost: cost, dummy: randomDummyHash(cost), derive: argon2.IDKey}
}

// Cost is the cost this hasher makes new hashes at.
func (h *Hasher) Cost() Cost { return h.cost }

// Hash returns a self-describing argon2id hash in PHC string format. The cost
// travels with the hash, so it can change while rows written at the old cost
// still verify.
func (h *Hasher) Hash(password []byte) (string, error) {
	if err := boundPassword(password); err != nil {
		return "", err
	}

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}

	key := argon2.IDKey(password, salt, h.cost.Passes, h.cost.MemoryKiB, Parallelism, keyLen)

	return encodeHash(h.cost, salt, key), nil
}

// VerifyPassword checks a password against a stored hash, at the cost the hash
// carries and not the cost a hasher is configured with.
//
// A password outside the bounds cannot be the right one. It still pays for the
// derivation before it is refused. A caller controls the length, and a refusal
// that skipped the derivation would answer at once. The unknown-user path
// would then be faster than the known-user one for that input, and a caller
// could tell which accounts exist.
func VerifyPassword(encoded string, password []byte) error {
	return verifyPassword(argon2.IDKey, encoded, password)
}

// verifyPassword is VerifyPassword with the derivation passed in, so the
// unknown-user path runs the same code through the hasher's derivation.
func verifyPassword(derive keyDerivation, encoded string, password []byte) error {
	memory, time, lanes, salt, want, err := decodeHash(encoded)
	if err != nil {
		return err
	}

	// decodeHash refuses an empty key and the encoded form bounds the rest, so
	// the length already fits in a uint32. The check is repeated here so a
	// later edit to decodeHash cannot break the conversion below.
	if len(want) == 0 || uint64(len(want)) > math.MaxUint32 {
		return ErrUnsupportedHash
	}

	//nolint:gosec // the line above bounds len(want) to [1, MaxUint32].
	got := derive(clampPassword(password), salt, time, memory, lanes, uint32(len(want)))

	if boundErr := boundPassword(password); boundErr != nil {
		return boundErr
	}

	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrPasswordMismatch
	}

	return nil
}

// VerifyUnknownUser charges the same argon2id pass a real verify costs, and
// always refuses. It is called on the path where no user was found, so that
// path and a wrong password take the same time.
//
// The dummy hash holds random bytes in place of a salt and a key. A verify
// derives a key at the cost the hash names, whatever the stored key holds, so
// the pass still costs the full time. No password derives a random 256-bit
// key, so nothing can match it.
func (h *Hasher) VerifyUnknownUser(password []byte) error {
	// The result is discarded. Only the work matters here, and the answer is a
	// refusal whatever it comes out as.
	_ = verifyPassword(h.derive, h.dummy, password)

	return ErrPasswordMismatch
}

// NeedsRehash reports whether a stored hash was made at another cost than this
// hasher's, so it can be replaced on the next successful login. A stronger cost
// is replaced too. Every login against that row would otherwise spend its
// larger memory block, and an unknown account would answer faster than it.
func (h *Hasher) NeedsRehash(encoded string) bool {
	memory, passes, lanes, _, _, err := decodeHash(encoded)
	if err != nil {
		return true
	}

	return memory != h.cost.MemoryKiB || passes != h.cost.Passes || lanes != Parallelism
}

// clampPassword bounds what reaches the derivation. Argon2's cost comes from
// its memory and its passes, so truncating a password over the upper limit keeps
// the work a caller can force fixed. What the truncated input derives is never
// compared: boundPassword refuses that password on the next line.
func clampPassword(password []byte) []byte {
	if len(password) > MaxPasswordBytes {
		return password[:MaxPasswordBytes]
	}

	return password
}

func boundPassword(password []byte) error {
	switch {
	case len(password) < MinPasswordBytes:
		return ErrPasswordTooShort
	case len(password) > MaxPasswordBytes:
		return ErrPasswordTooLong
	default:
		return nil
	}
}

func encodeHash(cost Cost, salt, key []byte) string {
	raw := base64.RawStdEncoding

	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		hashAlgorithm, argon2.Version, cost.MemoryKiB, cost.Passes, Parallelism,
		raw.EncodeToString(salt), raw.EncodeToString(key))
}

//nolint:nonamedreturns // six results are unreadable positionally.
func decodeHash(encoded string) (
	memory uint32, time uint32, lanes uint8, salt []byte, key []byte, err error,
) {
	parts := strings.Split(encoded, "$")
	if len(parts) != hashFields || parts[0] != "" || parts[1] != hashAlgorithm {
		return 0, 0, 0, nil, nil, ErrUnsupportedHash
	}

	var version int
	if _, scanErr := fmt.Sscanf(parts[2], "v=%d", &version); scanErr != nil || version != argon2.Version {
		return 0, 0, 0, nil, nil, ErrUnsupportedHash
	}

	if _, scanErr := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &lanes); scanErr != nil {
		return 0, 0, 0, nil, nil, ErrUnsupportedHash
	}

	// A stored row decides the work of a verify. argon2 stops the program on
	// zero passes or zero lanes, and a planted row with a huge cost would make
	// every login against it allocate that much. So both are refused here.
	if time < 1 || lanes < 1 || memory > MaxCostMemoryKiB || time > MaxCostPasses {
		return 0, 0, 0, nil, nil, ErrUnsupportedHash
	}

	raw := base64.RawStdEncoding

	salt, err = raw.DecodeString(parts[4])
	if err != nil {
		return 0, 0, 0, nil, nil, ErrUnsupportedHash
	}

	key, err = raw.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return 0, 0, 0, nil, nil, ErrUnsupportedHash
	}

	return memory, time, lanes, salt, key, nil
}

// randomDummyHash makes the dummy hash for one cost. It takes no argon2id
// pass, so making a hasher does not take its memory block at once.
func randomDummyHash(cost Cost) string {
	salt := make([]byte, saltLen)
	key := make([]byte, keyLen)

	// crypto/rand.Read never returns an error. It stops the program when the
	// system cannot supply random bytes.
	_, _ = rand.Read(salt)
	_, _ = rand.Read(key)

	return encodeHash(cost, salt, key)
}
