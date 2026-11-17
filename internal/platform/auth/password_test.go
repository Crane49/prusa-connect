package auth

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"

	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
)

// allocatedAtLoad is what the test binary had allocated when TestMain began,
// after every package loaded and before any test ran.
var allocatedAtLoad uint64

// printerCost is the cost the printer build hashes at. The tests use it in
// every build, so a normal run checks the printer's values too.
//
//nolint:gochecknoglobals // a fixed value the tests read and never write.
var printerCost = Cost{MemoryKiB: 7 * 1024, Passes: 5}

func TestMain(m *testing.M) {
	var stats runtime.MemStats

	runtime.ReadMemStats(&stats)
	allocatedAtLoad = stats.TotalAlloc

	os.Exit(m.Run())
}

// An argon2id pass takes its whole memory block at once. Running one while
// the package loads is enough to get the server killed on a small device.
func TestLoadingThePackageRunsNoArgon2Pass(t *testing.T) {
	t.Parallel()

	passBytes := uint64(printerCost.MemoryKiB) * 1024

	require.Less(t, allocatedAtLoad, passBytes/2,
		"loading the package allocated %d bytes, which means an argon2id pass ran", allocatedAtLoad)
}

// prefixOf is the start of a PHC string made at one cost.
func prefixOf(cost Cost) string {
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=1$", cost.MemoryKiB, cost.Passes)
}

// newHasher returns a hasher at a cost the test knows to be allowed.
func newHasher(t *testing.T, cost Cost) *Hasher {
	t.Helper()

	hasher, err := NewHasher(cost)
	require.NoError(t, err)

	return hasher
}

// A new hash uses the cost the hasher was given, and says so in its text.
func TestANewHashUsesTheConfiguredCost(t *testing.T) {
	t.Parallel()

	for _, cost := range []Cost{DefaultCost(), printerCost, {MemoryKiB: 12 * 1024, Passes: 3}} {
		t.Run(prefixOf(cost), func(t *testing.T) {
			t.Parallel()

			encoded, err := newHasher(t, cost).Hash([]byte("correct horse battery"))
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(encoded, prefixOf(cost)), "the hash is %s", encoded)

			memory, passes, lanes, salt, key, err := decodeHash(encoded)
			require.NoError(t, err)
			got := Cost{MemoryKiB: memory, Passes: passes}
			require.Equal(t, cost, got)
			require.Equal(t, Parallelism, lanes)
			require.Len(t, salt, saltLen)
			require.Len(t, key, keyLen)
			require.NoError(t, VerifyPassword(encoded, []byte("correct horse battery")))
		})
	}
}

// The normal build keeps the first setting OWASP lists.
func TestTheDefaultCostIsAnOWASPSetting(t *testing.T) {
	t.Parallel()

	require.NoError(t, DefaultCost().Check())
	require.Equal(t, 19, argon2.Version)
}

// The dummy hash carries the cost of a real hash. A verify against it then
// costs one full argon2id pass at that cost, and no password matches it.
func TestTheDummyHashCarriesTheConfiguredCost(t *testing.T) {
	t.Parallel()

	for _, cost := range []Cost{DefaultCost(), printerCost} {
		t.Run(prefixOf(cost), func(t *testing.T) {
			t.Parallel()

			hasher := newHasher(t, cost)

			require.True(t, strings.HasPrefix(hasher.dummy, prefixOf(cost)),
				"the dummy hash does not use the cost of a real one: %s", hasher.dummy)
			require.False(t, hasher.NeedsRehash(hasher.dummy))

			_, _, _, salt, key, err := decodeHash(hasher.dummy)
			require.NoError(t, err)
			require.Len(t, salt, saltLen)
			require.Len(t, key, keyLen)

			require.ErrorIs(t, VerifyPassword(hasher.dummy, []byte("wrong horse battery x")), ErrPasswordMismatch,
				"the dummy hash must be readable, or the verify against it does no work")
			require.ErrorIs(t, hasher.VerifyUnknownUser([]byte("wrong horse battery x")), ErrPasswordMismatch)
		})
	}
}

func TestHashPasswordSaltsEveryRow(t *testing.T) {
	t.Parallel()

	hasher := NewDefaultHasher()

	first, err := hasher.Hash([]byte("correct horse battery"))
	require.NoError(t, err)

	second, err := hasher.Hash([]byte("correct horse battery"))
	require.NoError(t, err)

	require.NotEqual(t, first, second, "the same password hashed twice must differ")
	require.NoError(t, VerifyPassword(first, []byte("correct horse battery")))
	require.NoError(t, VerifyPassword(second, []byte("correct horse battery")))
}

func TestVerifyPasswordBothWays(t *testing.T) {
	t.Parallel()

	encoded, err := NewDefaultHasher().Hash([]byte("correct horse battery"))
	require.NoError(t, err)

	require.NoError(t, VerifyPassword(encoded, []byte("correct horse battery")))
	require.ErrorIs(t, VerifyPassword(encoded, []byte("correct horse batterz")), ErrPasswordMismatch)
}

func TestPasswordLengthBound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		password []byte
		want     error
	}{
		{name: "one below the floor", password: make([]byte, MinPasswordBytes-1), want: ErrPasswordTooShort},
		{name: "at the floor", password: make([]byte, MinPasswordBytes)},
		{name: "at the upper limit", password: make([]byte, MaxPasswordBytes)},
		{name: "one above the upper limit", password: make([]byte, MaxPasswordBytes+1), want: ErrPasswordTooLong},
	}

	hasher := NewDefaultHasher()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := hasher.Hash(tc.password)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)

				return
			}

			require.NoError(t, err)
		})
	}
}

// Without the dummy hash the unknown-user path returns before any argon2 pass
// runs, and the difference is the whole derivation, which is easy to measure
// over the network. So an unknown user must run the same derivation as a known
// user with a wrong password: one argon2id pass, at the hasher's cost, with the
// same salt and key lengths. The printer's cost makes more passes over less
// memory, so it is checked too.
//
// The test counts the derivations and reads their parameters. It does not time
// them, so a loaded machine cannot make it fail. A password outside the bounds
// is checked as well, because a refusal that skipped the pass for it would
// answer for free.
func TestAnUnknownUserRunsTheSameDerivationAsAWrongPassword(t *testing.T) {
	t.Parallel()

	passwords := []struct {
		name     string
		password []byte
		known    error
	}{
		{name: "wrong", password: []byte("wrong horse battery x"), known: ErrPasswordMismatch},
		{name: "below the floor", password: make([]byte, MinPasswordBytes-1), known: ErrPasswordTooShort},
		{name: "above the upper limit", password: make([]byte, MaxPasswordBytes+1), known: ErrPasswordTooLong},
	}

	for _, cost := range []Cost{DefaultCost(), printerCost} {
		t.Run(prefixOf(cost), func(t *testing.T) {
			t.Parallel()

			want := derivation{
				saltLen: saltLen,
				passes:  cost.Passes,
				memory:  cost.MemoryKiB,
				threads: Parallelism,
				keyLen:  keyLen,
			}

			hasher := newHasher(t, cost)

			encoded, err := hasher.Hash([]byte("correct horse battery"))
			require.NoError(t, err)

			for _, tc := range passwords {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					var known, unknown derivations

					require.ErrorIs(t, verifyPassword(known.derive, encoded, tc.password), tc.known)

					counted := *hasher
					counted.derive = unknown.derive
					require.ErrorIs(t, counted.VerifyUnknownUser(tc.password), ErrPasswordMismatch)

					require.Equal(t, []derivation{want}, known.seen,
						"a known user with a wrong password must run one argon2id pass at the hasher's cost")
					require.Equal(t, []derivation{want}, unknown.seen,
						"an unknown user must run one argon2id pass at the hasher's cost, "+
							"or the time it takes enumerates accounts")
				})
			}
		})
	}
}

// derivation is the cost of one argon2id pass, as the derivation was called.
type derivation struct {
	saltLen int
	passes  uint32
	memory  uint32
	threads uint8
	keyLen  uint32
}

// derivations records every argon2id pass it is asked for and runs each one.
type derivations struct {
	seen []derivation
}

func (d *derivations) derive(password, salt []byte, passes, memory uint32, threads uint8, keyLen uint32) []byte {
	d.seen = append(d.seen, derivation{
		saltLen: len(salt), passes: passes, memory: memory, threads: threads, keyLen: keyLen,
	})

	return argon2.IDKey(password, salt, passes, memory, threads, keyLen)
}

// A hash stored at another cost still verifies, and NeedsRehash asks for it to
// be replaced. That holds for a weaker cost, and also for a stronger one,
// because a stored hash at a larger memory block costs a small device that
// block on every login.
func TestAHashAtAnotherCostVerifiesAndNeedsRehash(t *testing.T) {
	t.Parallel()

	printer := newHasher(t, printerCost)

	current, err := printer.Hash([]byte("correct horse battery"))
	require.NoError(t, err)
	require.False(t, printer.NeedsRehash(current))

	others := []Cost{
		{MemoryKiB: 19 * 1024, Passes: 2},
		{MemoryKiB: 7 * 1024, Passes: 6},
		{MemoryKiB: 8 * 1024, Passes: 5},
	}

	for _, other := range others {
		t.Run(prefixOf(other), func(t *testing.T) {
			t.Parallel()

			stored, hashErr := newHasher(t, other).Hash([]byte("correct horse battery"))
			require.NoError(t, hashErr)

			require.NoError(t, VerifyPassword(stored, []byte("correct horse battery")),
				"a hash at another cost no longer verifies")
			require.True(t, printer.NeedsRehash(stored), "a hash at another cost is kept")
		})
	}

	weaker := strings.Replace(current, "m=7168,t=5,p=1", "m=4096,t=1,p=1", 1)
	require.True(t, printer.NeedsRehash(weaker))
	require.True(t, printer.NeedsRehash("not a hash at all"))
}

// A cost below every setting OWASP lists is refused.
func TestACostBelowTheOWASPMinimumIsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cost Cost
		ok   bool
	}{
		{name: "46 MiB with 1 pass", cost: Cost{MemoryKiB: 46 * 1024, Passes: 1}, ok: true},
		{name: "19 MiB with 2 passes", cost: Cost{MemoryKiB: 19 * 1024, Passes: 2}, ok: true},
		{name: "12 MiB with 3 passes", cost: Cost{MemoryKiB: 12 * 1024, Passes: 3}, ok: true},
		{name: "9 MiB with 4 passes", cost: Cost{MemoryKiB: 9 * 1024, Passes: 4}, ok: true},
		{name: "7 MiB with 5 passes", cost: Cost{MemoryKiB: 7 * 1024, Passes: 5}, ok: true},
		{name: "64 MiB with 10 passes", cost: Cost{MemoryKiB: 64 * 1024, Passes: 10}, ok: true},
		{name: "46 MiB less 1 KiB with 1 pass", cost: Cost{MemoryKiB: 46*1024 - 1, Passes: 1}},
		{name: "19 MiB with 1 pass", cost: Cost{MemoryKiB: 19 * 1024, Passes: 1}},
		{name: "8 MiB with 4 passes", cost: Cost{MemoryKiB: 8 * 1024, Passes: 4}},
		{name: "7 MiB less 1 KiB with 20 passes", cost: Cost{MemoryKiB: 7*1024 - 1, Passes: 20}},
		{name: "no memory", cost: Cost{Passes: 5}},
		{name: "no passes", cost: Cost{MemoryKiB: 64 * 1024}},
		{name: "memory over the upper limit", cost: Cost{MemoryKiB: MaxCostMemoryKiB + 1, Passes: 5}},
		{name: "passes over the upper limit", cost: Cost{MemoryKiB: 19 * 1024, Passes: MaxCostPasses + 1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.cost.Check()

			_, newErr := NewHasher(tc.cost)
			if tc.ok {
				require.NoError(t, err)
				require.NoError(t, newErr)

				return
			}

			require.ErrorIs(t, err, ErrWeakCost)
			require.ErrorIs(t, newErr, ErrWeakCost)
		})
	}
}

// The message names the smallest setting OWASP allows at the memory given, so
// an operator can correct the value without reading the source.
func TestTheWeakCostMessageSaysWhatIsAllowed(t *testing.T) {
	t.Parallel()

	err := Cost{MemoryKiB: 8 * 1024, Passes: 4}.Check()
	require.ErrorContains(t, err, "8192 KiB with 4 passes")
	require.ErrorContains(t, err, "at least 5 passes")

	err = Cost{MemoryKiB: 6 * 1024, Passes: 9}.Check()
	require.ErrorContains(t, err, "at least 7168 KiB")

	err = Cost{MemoryKiB: 19 * 1024, Passes: 1}.Check()
	require.ErrorContains(t, err, "19456 KiB with 1 pass is below")
	require.ErrorContains(t, err, "at least 2 passes")
}

func TestVerifyPasswordRefusesAHashItCannotRead(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",
		"plaintext",
		"$bcrypt$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=13$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$aGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$",
	}

	for _, encoded := range cases {
		t.Run(encoded, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, VerifyPassword(encoded, []byte("correct horse battery")), ErrUnsupportedHash)
		})
	}
}

// A stored row sets the work of every verify against it. A row planted with
// no passes or no lanes would stop the program inside argon2, and a row with
// a huge cost would allocate that much on every login. Each is refused before
// any derivation, and NeedsRehash asks for it to be replaced.
func TestAStoredCostOutsideTheLimitsIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"no passes":                   "$argon2id$v=19$m=19456,t=0,p=1$c2FsdA$aGFzaA",
		"no lanes":                    "$argon2id$v=19$m=19456,t=2,p=0$c2FsdA$aGFzaA",
		"memory over the upper limit": fmt.Sprintf("$argon2id$v=19$m=%d,t=2,p=1$c2FsdA$aGFzaA", MaxCostMemoryKiB+1),
		"passes over the upper limit": fmt.Sprintf("$argon2id$v=19$m=19456,t=%d,p=1$c2FsdA$aGFzaA", MaxCostPasses+1),
		"memory far over the limit":   "$argon2id$v=19$m=4294967295,t=2,p=1$c2FsdA$aGFzaA",
	}

	hasher := NewDefaultHasher()

	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var err error

			require.NotPanics(t, func() { err = VerifyPassword(encoded, []byte("correct horse battery")) })
			require.ErrorIs(t, err, ErrUnsupportedHash)
			require.True(t, hasher.NeedsRehash(encoded))
		})
	}
}

// A build hashes at its own profile's cost when the configuration names none.
// The printer build uses 7 MiB with 5 passes, and its dummy hash carries the
// same values. make test-printer runs this under the printer tag.
func TestTheBuildHashesAtItsProfileCost(t *testing.T) {
	t.Parallel()

	want := Cost{MemoryKiB: 19 * 1024, Passes: 2}
	if buildprofile.Name == "printer" {
		want = printerCost
	}

	require.Equal(t, want, DefaultCost())

	hasher := NewDefaultHasher()

	encoded, err := hasher.Hash([]byte("correct horse battery"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(encoded, prefixOf(want)), "the hash is %s", encoded)
	require.True(t, strings.HasPrefix(hasher.dummy, prefixOf(want)), "the dummy hash is %s", hasher.dummy)
}
