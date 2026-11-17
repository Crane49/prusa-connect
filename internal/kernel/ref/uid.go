package ref

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// Crockford base32: the digits, the letters, and none of I, L, O or U.
	alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

	uidLen    = 16 // bytes
	uidStrLen = 26 // Crockford base32 characters
	timeLen   = 6  // leading bytes holding Unix milliseconds

	bitsPerByte = 8
	bitsPerChar = 5
	charMask    = 0x1f
	// alignedShift is the largest bit offset at which five bits still fit in one
	// byte; past it the value straddles two.
	alignedShift = bitsPerByte - bitsPerChar
	// maxLeadChar is the largest value the leading character can hold: it covers
	// only three of its five bits, the rest falling outside the 128-bit value.
	maxLeadChar = 0x07
	maxMillis   = int64(1)<<(timeLen*bitsPerByte) - 1
)

var (
	// ErrInvalidUID reports a string that is not a rendered UID.
	ErrInvalidUID = errors.New("invalid uid")
	// ErrTimeRange reports a mint timestamp outside the 48-bit millisecond range.
	ErrTimeRange = errors.New("uid timestamp out of range")
)

// UID is the host-minted identity of an instance: 48 bits of Unix milliseconds
// followed by 80 bits of randomness, rendered as 26 Crockford base32 characters
// that sort lexicographically by mint time. Data rows reference it and nothing
// can change it.
type UID [uidLen]byte

// ParseUID reads the 26-character form, accepting either case.
func ParseUID(s string) (UID, error) {
	if len(s) != uidStrLen {
		return UID{}, fmt.Errorf("%w: %q is %d characters, want %d", ErrInvalidUID, echo(s), len(s), uidStrLen)
	}
	var u UID
	for i := range len(s) {
		v, err := decodeChar(s[i])
		if err != nil {
			return UID{}, fmt.Errorf("%w: %q: %w", ErrInvalidUID, echo(s), err)
		}
		if i == 0 && v > maxLeadChar {
			return UID{}, fmt.Errorf("%w: %q overflows 128 bits", ErrInvalidUID, echo(s))
		}
		setBits(&u, (uidStrLen-1-i)*bitsPerChar, v)
	}
	return u, nil
}

// Time returns the mint timestamp, truncated to the millisecond it encodes.
func (u UID) Time() time.Time { return time.UnixMilli(u.millis()).UTC() }

// IsZero reports whether u was never minted.
func (u UID) IsZero() bool { return u == UID{} }

// String renders the 26-character Crockford base32 form.
func (u UID) String() string {
	var out [uidStrLen]byte
	for i := range out {
		out[i] = alphabet[bitsAt(u, (uidStrLen-1-i)*bitsPerChar)]
	}
	return string(out[:])
}

// MarshalText renders the String form.
func (u UID) MarshalText() ([]byte, error) { return []byte(u.String()), nil }

// UnmarshalText parses the String form.
func (u *UID) UnmarshalText(text []byte) error {
	parsed, err := ParseUID(string(text))
	if err != nil {
		return err
	}
	*u = parsed
	return nil
}

func (u UID) millis() int64 {
	var ms int64
	for _, b := range u[:timeLen] {
		ms = ms<<bitsPerByte | int64(b)
	}
	return ms
}

// increment adds one to the entropy, reporting false when it has wrapped and
// this millisecond has no unused value left.
func (u UID) increment() (UID, bool) {
	for i := uidLen - 1; i >= timeLen; i-- {
		u[i]++
		if u[i] != 0 {
			return u, true
		}
	}
	return UID{}, false
}

// Minter mints UIDs. The zero value is ready to use and safe for concurrent use.
//
// Two UIDs minted in the same millisecond would sort at random, so a mint that
// does not advance the clock increments the previous entropy instead of drawing
// fresh bytes. That is the ULID monotonic rule, and it keeps creation order and
// sort order the same.
type Minter struct {
	mu   sync.Mutex
	last UID
}

// Mint returns a UID timestamped at now, strictly greater than every UID this
// Minter has already returned.
func (m *Minter) Mint(now time.Time) (UID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ms := now.UnixMilli()
	if !m.last.IsZero() && ms <= m.last.millis() {
		if next, ok := m.last.increment(); ok {
			m.last = next
			return next, nil
		}
		ms = m.last.millis() + 1
	}
	if ms < 0 || ms > maxMillis {
		return UID{}, fmt.Errorf("%w: %d milliseconds since the Unix epoch", ErrTimeRange, ms)
	}

	var u UID
	putMillis(&u, ms)
	if _, err := rand.Read(u[timeLen:]); err != nil {
		return UID{}, fmt.Errorf("mint uid: %w", err)
	}
	m.last = u
	return u, nil
}

func putMillis(u *UID, ms int64) {
	for i := timeLen - 1; i >= 0; i-- {
		//nolint:gosec // G115: the loop packs one byte at a time and shifts, so the truncation is the encoding.
		u[i] = byte(ms)
		ms >>= bitsPerByte
	}
}

// bitsAt reads the five bits starting pos bits from the least significant end of
// the 128-bit big-endian value.
func bitsAt(u UID, pos int) byte {
	idx := uidLen - 1 - pos/bitsPerByte
	shift := pos % bitsPerByte
	v := u[idx] >> shift
	if shift > alignedShift && idx > 0 {
		//nolint:gosec // G602: the idx > 0 on the line above is the bound, and UID is a fixed-size array.
		v |= u[idx-1] << (bitsPerByte - shift)
	}
	return v & charMask
}

func setBits(u *UID, pos int, v byte) {
	idx := uidLen - 1 - pos/bitsPerByte
	shift := pos % bitsPerByte
	u[idx] |= v << shift
	if shift > alignedShift && idx > 0 {
		u[idx-1] |= v >> (bitsPerByte - shift)
	}
}

// decodeChar rejects I, L, O and U and does not fold them onto 1, 0 and V.
// One UID must have exactly one string form, or two rows can disagree about
// whether they reference the same instance.
func decodeChar(c byte) (byte, error) {
	if c >= 'a' && c <= 'z' {
		c -= 'a' - 'A'
	}
	i := strings.IndexByte(alphabet, c)
	if i < 0 {
		return 0, fmt.Errorf("%q is not a Crockford base32 digit", string(c))
	}
	//nolint:gosec // G115: i indexes alphabet, which is 32 bytes long, and a negative i returned above.
	return byte(i), nil
}
