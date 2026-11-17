package ref

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestUIDRoundTrip(t *testing.T) {
	t.Parallel()

	var m Minter
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)

	for i := range 64 {
		u, err := m.Mint(now.Add(time.Duration(i) * time.Millisecond))
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}

		s := u.String()
		if len(s) != uidStrLen {
			t.Fatalf("String() = %q, want %d characters", s, uidStrLen)
		}

		back, err := ParseUID(s)
		if err != nil {
			t.Fatalf("ParseUID(%q): %v", s, err)
		}
		if back != u {
			t.Fatalf("ParseUID(%q) = %v, want %v", s, back, u)
		}

		text, err := u.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText: %v", err)
		}
		var unmarshalled UID
		if uerr := unmarshalled.UnmarshalText(text); uerr != nil {
			t.Fatalf("UnmarshalText(%q): %v", text, uerr)
		}
		if unmarshalled != u {
			t.Fatalf("UnmarshalText(%q) = %v, want %v", text, unmarshalled, u)
		}
	}
}

func TestUIDTime(t *testing.T) {
	t.Parallel()

	var m Minter
	// A time with sub-millisecond precision, so truncation is exercised.
	now := time.Date(2026, time.August, 24, 12, 0, 0, 123_456_789, time.UTC)

	u, err := m.Mint(now)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if got, want := u.Time(), now.Truncate(time.Millisecond).UTC(); !got.Equal(want) {
		t.Fatalf("Time() = %v, want %v", got, want)
	}
	if u.IsZero() {
		t.Fatal("IsZero() = true for a minted UID")
	}
	if !(UID{}).IsZero() {
		t.Fatal("IsZero() = false for the zero UID")
	}
}

// The canonical ULID from the specification, with all 128 bits worked out by
// hand from the Crockford alphabet rather than by this package's own encoder,
// so a symmetric permutation of the entropy bits cannot pass.
//
// The 26 characters are the base32 digits 0,1,10,24,31,3,21,13,14,19 (the
// 48-bit timestamp, 1469922850259 = 0x01563E3AB5D3) followed by
// 26,25,27,4,24,24,15,15,23,6,9,16,5,15,10,27, whose 80-bit concatenation is
// 0xD6764C61EFB99302BD5B.
func TestParseUIDMatchesTheULIDSpecVector(t *testing.T) {
	t.Parallel()

	const (
		vector = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
		millis = 1469922850259
	)
	want := UID{
		0x01, 0x56, 0x3E, 0x3A, 0xB5, 0xD3,
		0xD6, 0x76, 0x4C, 0x61, 0xEF, 0xB9, 0x93, 0x02, 0xBD, 0x5B,
	}

	u, err := ParseUID(vector)
	if err != nil {
		t.Fatalf("ParseUID(%q): %v", vector, err)
	}
	if u != want {
		t.Fatalf("ParseUID(%q) = % X, want % X", vector, u[:], want[:])
	}
	if got := u.Time().UnixMilli(); got != millis {
		t.Fatalf("Time().UnixMilli() = %d, want %d", got, millis)
	}
	if got := want.String(); got != vector {
		t.Fatalf("String() = %q, want %q", got, vector)
	}
}

func TestParseUIDAcceptsLowercase(t *testing.T) {
	t.Parallel()

	const vector = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

	upper, err := ParseUID(vector)
	if err != nil {
		t.Fatalf("ParseUID: %v", err)
	}
	lower, err := ParseUID("01arz3ndektsv4rrffq69g5fav")
	if err != nil {
		t.Fatalf("ParseUID lowercase: %v", err)
	}
	if lower != upper {
		t.Fatalf("lowercase parsed to %v, want %v", lower, upper)
	}
}

func TestParseUIDRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "too short", in: "01ARZ3NDEKTSV4RRFFQ69G5FA"},
		{name: "too long", in: "01ARZ3NDEKTSV4RRFFQ69G5FAVV"},
		{name: "overflows 128 bits", in: "81ARZ3NDEKTSV4RRFFQ69G5FAV"},
		{name: "overflows at the top of the alphabet", in: "Z1ARZ3NDEKTSV4RRFFQ69G5FAV"},
		{name: "excluded letter I", in: "01ARZ3NDEKTSV4RRFFQ69G5FAI"},
		{name: "excluded letter L", in: "01ARZ3NDEKTSV4RRFFQ69G5FAL"},
		{name: "excluded letter O", in: "01ARZ3NDEKTSV4RRFFQ69G5FAO"},
		{name: "excluded letter U", in: "01ARZ3NDEKTSV4RRFFQ69G5FAU"},
		{name: "punctuation", in: "01ARZ3NDEKTSV4RRFFQ69G5FA!"},
		{name: "hyphenated", in: "01ARZ3NDEK-TSV4RRFFQ69G5F"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := ParseUID(tc.in); !errors.Is(err, ErrInvalidUID) {
				t.Fatalf("ParseUID(%q) error = %v, want ErrInvalidUID", tc.in, err)
			}
		})
	}
}

func TestMintSortsByCreationOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		step time.Duration
	}{
		{name: "same millisecond", step: 0},
		{name: "sub-millisecond steps", step: 100 * time.Microsecond},
		{name: "one millisecond apart", step: time.Millisecond},
		{name: "a second apart", step: time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var m Minter
			now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)

			const count = 512

			got := make([]string, 0, count)
			for i := range count {
				u, err := m.Mint(now.Add(time.Duration(i) * tc.step))
				if err != nil {
					t.Fatalf("Mint: %v", err)
				}
				got = append(got, u.String())
			}

			for i := 1; i < len(got); i++ {
				if got[i] <= got[i-1] {
					t.Fatalf("UID %d is %q, want strictly greater than %q", i, got[i], got[i-1])
				}
			}
		})
	}
}

func TestMintIsMonotonicWhenTheClockGoesBackwards(t *testing.T) {
	t.Parallel()

	var m Minter
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)

	first, err := m.Mint(now)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	second, err := m.Mint(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if second.String() <= first.String() {
		t.Fatalf("Mint after a backwards clock returned %q, want > %q", second, first)
	}
}

func TestMintCarriesIntoTheNextMillisecondWhenEntropyIsExhausted(t *testing.T) {
	t.Parallel()

	var m Minter
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)

	exhausted, err := m.Mint(now)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	for i := timeLen; i < uidLen; i++ {
		exhausted[i] = 0xFF
	}
	m.last = exhausted

	next, err := m.Mint(now)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if got, want := next.Time().UnixMilli(), exhausted.Time().UnixMilli()+1; got != want {
		t.Fatalf("Time().UnixMilli() = %d, want %d", got, want)
	}
	if next.String() <= exhausted.String() {
		t.Fatalf("Mint returned %q, want > %q", next, exhausted)
	}
}

func TestMintRejectsATimestampOutOfRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		now  time.Time
	}{
		{name: "before the Unix epoch", now: time.UnixMilli(-1)},
		{name: "one millisecond past the 48-bit range", now: time.UnixMilli(maxMillis).Add(time.Millisecond)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var m Minter
			if _, err := m.Mint(tc.now); !errors.Is(err, ErrTimeRange) {
				t.Fatalf("Mint(%v) error = %v, want ErrTimeRange", tc.now, err)
			}
		})
	}
}

func TestMintIsUniqueUnderConcurrency(t *testing.T) {
	t.Parallel()

	const (
		goroutines = 8
		perRoutine = 256
	)

	var (
		m    Minter
		now  = time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = make(map[UID]struct{}, goroutines*perRoutine)
	)

	for range goroutines {
		wg.Go(func() {
			for range perRoutine {
				u, err := m.Mint(now)
				if err != nil {
					t.Errorf("Mint: %v", err)
					return
				}
				mu.Lock()
				seen[u] = struct{}{}
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	if len(seen) != goroutines*perRoutine {
		t.Fatalf("minted %d distinct UIDs, want %d", len(seen), goroutines*perRoutine)
	}
}
