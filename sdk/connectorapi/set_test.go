package connectorapi_test

import (
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	verbPrint = connectorapi.Capability("core.command.start_print")
	verbPause = connectorapi.Capability("core.command.pause_print")
	argPath   = connectorapi.Capability("core.command.start_print#path")
)

func flag() connectorapi.Value { return connectorapi.Value{Flag: true} }

// The property the whole three-tier model rests on: a lower tier subtracts and
// never adds. A profile that claims a capability the baseline does not have
// must not produce it.
func TestIntersectNeverAddsAKeyTheOtherSideLacks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		left  connectorapi.Set
		right connectorapi.Set
		want  []connectorapi.Capability
	}{
		{
			name:  "a key only the profile declares does not survive",
			left:  connectorapi.Set{verbPause: flag()},
			right: connectorapi.Set{verbPause: flag(), verbPrint: flag()},
			want:  []connectorapi.Capability{verbPause},
		},
		{
			name:  "a key only the baseline declares does not survive",
			left:  connectorapi.Set{verbPause: flag(), verbPrint: flag()},
			right: connectorapi.Set{verbPause: flag()},
			want:  []connectorapi.Capability{verbPause},
		},
		{
			name:  "nothing in common leaves nothing",
			left:  connectorapi.Set{verbPrint: flag()},
			right: connectorapi.Set{verbPause: flag()},
			want:  nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.left.Intersect(tc.right).Keys()
			if !slices.Equal(got, tc.want) {
				t.Errorf("keys = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIntersectNarrowsAValue(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		left  connectorapi.Value
		right connectorapi.Value
		want  connectorapi.Value
		gone  bool
	}{
		{
			name:  "a false flag on either side turns the capability off",
			left:  connectorapi.Value{Flag: true},
			right: connectorapi.Value{Flag: false},
			want:  connectorapi.Value{Flag: false},
		},
		{
			name:  "supported sets narrow to their common values",
			left:  connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Supported: []string{"a", "b", "c"}},
			right: connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Supported: []string{"b", "c", "d"}},
			want:  connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Supported: []string{"b", "c"}},
		},
		{
			name:  "an empty supported set is unconstrained, so the other side wins",
			left:  connectorapi.Value{Flag: true, Kind: connectorapi.KindString},
			right: connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Supported: []string{"b"}},
			want:  connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Supported: []string{"b"}},
		},
		{
			name:  "no common value leaves no usable capability",
			left:  connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Supported: []string{"a"}},
			right: connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Supported: []string{"b"}},
			gone:  true,
		},
		{
			name:  "two tiers that disagree about the kind leave nothing",
			left:  connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger},
			right: connectorapi.Value{Flag: true, Kind: connectorapi.KindString},
			gone:  true,
		},
		{
			name:  "the lower limit wins",
			left:  connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger, Limit: 100, HasLimit: true},
			right: connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger, Limit: 40, HasLimit: true},
			want:  connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger, Limit: 40, HasLimit: true},
		},
		{
			name:  "one side's limit binds when the other declares none",
			left:  connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger},
			right: connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger, Limit: 40, HasLimit: true},
			want:  connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger, Limit: 40, HasLimit: true},
		},
		// Min narrows upwards where Limit narrows downwards, and nothing
		// anywhere drove either of these three: every profile the conformance
		// suite builds is the baseline with a key added or removed, never with a
		// different bound, so max could have been min and both arms could have
		// been deleted with the whole suite still green.
		{
			name: "the higher lower bound wins, because it is the narrower one",
			left: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindNumber, Min: 0, HasMin: true, Limit: 300, HasLimit: true,
			},
			right: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindNumber, Min: -273, HasMin: true, Limit: 300, HasLimit: true,
			},
			want: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindNumber, Min: 0, HasMin: true, Limit: 300, HasLimit: true,
			},
		},
		{
			name: "one side's lower bound binds when the other declares none",
			left: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindNumber, Min: 0, HasMin: true, Limit: 300, HasLimit: true,
			},
			right: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindNumber, Limit: 300, HasLimit: true,
			},
			want: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindNumber, Min: 0, HasMin: true, Limit: 300, HasLimit: true,
			},
		},
		{
			name: "the same, with the sides swapped",
			left: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindNumber, Limit: 300, HasLimit: true,
			},
			right: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindNumber, Min: 0, HasMin: true, Limit: 300, HasLimit: true,
			},
			want: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindNumber, Min: 0, HasMin: true, Limit: 300, HasLimit: true,
			},
		},
		{
			name:  "required on either side stays required",
			left:  connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Required: true},
			right: connectorapi.Value{Flag: true, Kind: connectorapi.KindString},
			want:  connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Required: true},
		},
		{
			name:  "two tiers that disagree about the default leave the caller to say",
			left:  connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Default: "usb"},
			right: connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Default: "sd"},
			want:  connectorapi.Value{Flag: true, Kind: connectorapi.KindString},
		},
		{
			name: "a default that the narrowing removed is dropped",
			left: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindString, Supported: []string{"a", "b"}, Default: "a",
			},
			right: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindString, Supported: []string{"b"}, Default: "a",
			},
			want: connectorapi.Value{Flag: true, Kind: connectorapi.KindString, Supported: []string{"b"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The verb is on both sides because an argument key whose verb
			// nothing declares is not a descriptor: the intersection drops it
			// rather than leaving an argument of a verb that is not there.
			verb := connectorapi.Value{Flag: true}
			got := connectorapi.Set{verbPrint: verb, argPath: tc.left}.
				Intersect(connectorapi.Set{verbPrint: verb, argPath: tc.right})

			value, ok := got[argPath]
			if tc.gone {
				if ok {
					t.Fatalf("key survived as %+v, want it gone", value)
				}

				return
			}

			if !ok {
				t.Fatal("key did not survive")
			}

			if !equalValue(value, tc.want) {
				t.Errorf("value = %+v, want %+v", value, tc.want)
			}
		})
	}
}

// TestAMergeNeverLosesALowerBound is the same rule where a caller feels it: a
// merged descriptor is what Validate holds a command to, so a lower bound the
// merge dropped or loosened is a value a driver is handed that its own tier
// said no to. A nozzle target of -273 is the shape of it.
func TestAMergeNeverLosesALowerBound(t *testing.T) {
	t.Parallel()

	floored := connectorapi.Set{
		verbPrint: flag(),
		argPath: {
			Flag: true, Kind: connectorapi.KindNumber, Min: 0, HasMin: true, Limit: 300, HasLimit: true,
		},
	}

	for name, other := range map[string]connectorapi.Set{
		"a tier that says nothing about the lower bound": {
			verbPrint: flag(),
			argPath: {
				Flag: true, Kind: connectorapi.KindNumber, Limit: 300, HasLimit: true,
			},
		},
		"a tier whose lower bound is looser": {
			verbPrint: flag(),
			argPath: {
				Flag: true, Kind: connectorapi.KindNumber, Min: -273, HasMin: true, Limit: 300, HasLimit: true,
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for direction, merged := range map[string]connectorapi.Set{
				"floored ∩ other": floored.Intersect(other),
				"other ∩ floored": other.Intersect(floored),
			} {
				if _, err := merged.Validate(connectorapi.Command{
					Verb: verbPrint,
					Args: map[string]string{"path": "-273"},
				}); err == nil {
					t.Errorf("%s: the merged set accepted -273, so the floor did not survive", direction)
				}

				if _, err := merged.Validate(connectorapi.Command{
					Verb: verbPrint,
					Args: map[string]string{"path": "200"},
				}); err != nil {
					t.Errorf("%s: the merged set refused 200, which both tiers permit: %v", direction, err)
				}
			}
		})
	}
}

// Intersection is the merge rule precisely because it needs no conflict policy.
// That only holds if it is commutative and associative, so the order the tiers
// are applied in cannot change the answer.
func TestIntersectIsCommutativeAssociativeAndBounded(t *testing.T) {
	t.Parallel()

	// A fixed seed, so a failure reproduces.
	random := rand.New(rand.NewPCG(0x5eed, 0xf00d))

	for range 500 {
		a, b, c := randomSet(random), randomSet(random), randomSet(random)

		if got, want := a.Intersect(b).Digest(), b.Intersect(a).Digest(); got != want {
			t.Fatalf("not commutative:\n a = %v\n b = %v", a, b)
		}

		if got, want := a.Intersect(b).Intersect(c).Digest(), a.Intersect(b.Intersect(c)).Digest(); got != want {
			t.Fatalf("not associative:\n a = %v\n b = %v\n c = %v", a, b, c)
		}

		for key := range a.Intersect(b) {
			if _, ok := a[key]; !ok {
				t.Fatalf("%s appeared from nowhere: not in the left operand", key)
			}

			if _, ok := b[key]; !ok {
				t.Fatalf("%s appeared from nowhere: not in the right operand", key)
			}
		}
	}
}

func TestDigestSeparatesEveryFieldOfAValue(t *testing.T) {
	t.Parallel()

	base := connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger, Supported: []string{"1"}, Default: "1"}

	changed := map[string]connectorapi.Value{
		"flag":      {Flag: false, Kind: connectorapi.KindInteger, Supported: []string{"1"}, Default: "1"},
		"kind":      {Flag: true, Kind: connectorapi.KindNumber, Supported: []string{"1"}, Default: "1"},
		"supported": {Flag: true, Kind: connectorapi.KindInteger, Supported: []string{"2"}, Default: "1"},
		"default":   {Flag: true, Kind: connectorapi.KindInteger, Supported: []string{"1"}},
		"required": {
			Flag: true, Kind: connectorapi.KindInteger, Supported: []string{"1"}, Default: "1", Required: true,
		},
		"limit": {
			Flag: true, Kind: connectorapi.KindInteger, Supported: []string{"1"}, Default: "1", HasLimit: true,
		},
		"min": {
			Flag: true, Kind: connectorapi.KindInteger, Supported: []string{"1"}, Default: "1", HasMin: true,
		},
		"unit": {
			Flag: true, Kind: connectorapi.KindInteger, Supported: []string{"1"}, Default: "1",
			Unit: connectorapi.UnitCelsius,
		},
	}

	want := connectorapi.Set{argPath: base}.Digest()

	for field, value := range changed {
		if got := (connectorapi.Set{argPath: value}).Digest(); got == want {
			t.Errorf("changing %s did not change the digest", field)
		}
	}
}

// Length-prefixing makes the field list unambiguous. It says nothing about the
// record list, because the supported run had no count and no record ended with
// a terminator: a supported value could stand where the fields after it belong.
// That it is not a live collision rests on strconv.FormatBool output sorting
// above everything strconv.FormatFloat produces, which is an accident of two
// unrelated encodings, undocumented and untested. The count settles it.
func TestDigestCountsTheSupportedRunSoRecordsCannotRunTogether(t *testing.T) {
	t.Parallel()

	// Every value here is a field the encoding writes for its own reasons: the
	// bool a Required writes, the empty string a missing default and a missing
	// limit write, and a key that could start the next record.
	adversarial := []string{"", "false", "true", "0", "300", verbPause.String(), "flag", "string"}

	seen := map[string]string{}

	for _, split := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8} {
		set := connectorapi.Set{
			verbPrint: flag(),
			argPath: {
				Flag: true, Kind: connectorapi.KindString,
				Supported: slices.Clone(adversarial[:split]),
			},
		}

		digest := set.Digest()
		if where, clash := seen[digest]; clash {
			t.Errorf("a run of %d supported values hashes the same as %s", split, where)
		}

		seen[digest] = strconv.Itoa(split) + " supported values"
	}
}

// A value listed twice permits exactly what it permits once, so two sets that
// permit the same things must not have different digests. A digest that two
// sides compare to agree on what is in force has to answer that question and no
// other.
func TestDigestIgnoresADuplicatedSupportedValue(t *testing.T) {
	t.Parallel()

	duplicated := connectorapi.Set{
		verbPrint: flag(),
		argPath:   {Flag: true, Kind: connectorapi.KindString, Supported: []string{"y", "x", "x"}},
	}
	once := connectorapi.Set{
		verbPrint: flag(),
		argPath:   {Flag: true, Kind: connectorapi.KindString, Supported: []string{"x", "y"}},
	}

	if duplicated.Digest() != once.Digest() {
		t.Error("two sets permitting exactly the same values have different digests")
	}
}

// A bound of zero and no bound at all are different descriptors: the first
// permits nothing but zero, the second permits anything. HasLimit and HasMin
// exist to say so, and the digest has to carry the distinction.
func TestDigestSeparatesAZeroBoundFromNoBound(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		bound connectorapi.Value
		none  connectorapi.Value
	}{
		{
			name:  "limit",
			bound: connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger, HasLimit: true},
			none:  connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger},
		},
		{
			name:  "lower bound",
			bound: connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger, HasMin: true},
			none:  connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bounded := connectorapi.Set{verbPrint: flag(), argPath: tc.bound}
			unbounded := connectorapi.Set{verbPrint: flag(), argPath: tc.none}

			if bounded.Digest() == unbounded.Digest() {
				t.Errorf("a %s of zero hashes the same as no %s at all", tc.name, tc.name)
			}
		})
	}
}

func TestDigestIgnoresTheOrderValuesWereWrittenIn(t *testing.T) {
	t.Parallel()

	left := connectorapi.Set{
		verbPrint: flag(),
		argPath:   {Flag: true, Kind: connectorapi.KindString, Supported: []string{"b", "a"}},
	}
	right := connectorapi.Set{
		argPath:   {Flag: true, Kind: connectorapi.KindString, Supported: []string{"a", "b"}},
		verbPrint: flag(),
	}

	if left.Digest() != right.Digest() {
		t.Error("two sets permitting the same things have different digests")
	}
}

func TestCloneDoesNotShareItsSupportedSet(t *testing.T) {
	t.Parallel()

	original := connectorapi.Set{argPath: {Flag: true, Supported: []string{"a", "b"}}}
	clone := original.Clone()
	clone[argPath].Supported[0] = "mutated"

	if original[argPath].Supported[0] != "a" {
		t.Error("mutating the clone changed the original")
	}
}

func equalValue(a, b connectorapi.Value) bool {
	return a.Flag == b.Flag && a.Kind == b.Kind && a.Default == b.Default &&
		a.Required == b.Required && a.Limit == b.Limit && a.HasLimit == b.HasLimit &&
		a.Min == b.Min && a.HasMin == b.HasMin &&
		slices.Equal(a.Supported, b.Supported)
}

// randomSet builds a small descriptor out of a fixed vocabulary, so two sets
// overlap often enough for the intersection to be interesting.
func randomSet(random *rand.Rand) connectorapi.Set {
	keys := []connectorapi.Capability{verbPrint, verbPause, argPath, "core.feature.dialogs"}
	values := []string{"a", "b", "c"}

	out := make(connectorapi.Set)

	for _, key := range keys {
		if random.IntN(4) == 0 {
			continue
		}

		var supported []string

		for _, value := range values {
			if random.IntN(2) == 0 {
				supported = append(supported, value)
			}
		}

		out[key] = connectorapi.Value{
			Flag:      random.IntN(8) != 0,
			Kind:      connectorapi.Kind(random.IntN(2)),
			Supported: supported,
			Default:   values[random.IntN(len(values))],
			Required:  random.IntN(2) == 0,
			Limit:     float64(random.IntN(100)),
			HasLimit:  random.IntN(2) == 0,
		}
	}

	return out
}

// A digest is what two sides compare to agree on what is in force, so two sets
// that permit different things must never hash the same. A supported value
// containing the encoding's separator is the cheapest way to collide one.
func TestDigestDoesNotCollideOnASeparatorInsideAValue(t *testing.T) {
	t.Parallel()

	oneValue := connectorapi.Set{argPath: {
		Flag: true, Kind: connectorapi.KindString, Supported: []string{"a,b"},
	}}
	twoValues := connectorapi.Set{argPath: {
		Flag: true, Kind: connectorapi.KindString, Supported: []string{"a", "b"},
	}}

	if oneValue.Digest() == twoValues.Digest() {
		t.Error("a set accepting the single value \"a,b\" hashes the same as one accepting \"a\" or \"b\"")
	}
}
