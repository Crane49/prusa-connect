package ref

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestValidateID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		ok   bool
	}{
		{name: "single letter", id: "a", ok: true},
		{name: "single digit", id: "0", ok: true},
		{name: "letters and digits", id: "s3main2", ok: true},
		{name: "interior hyphen", id: "s3-main", ok: true},
		{name: "consecutive interior hyphens", id: "a--b", ok: true},
		{name: "digit start", id: "1storage", ok: true},
		{name: "at the length limit", id: strings.Repeat("a", maxIDLen), ok: true},

		{name: "empty", id: "", ok: false},
		{name: "one past the length limit", id: strings.Repeat("a", maxIDLen+1), ok: false},
		{name: "leading hyphen", id: "-main", ok: false},
		{name: "trailing hyphen", id: "main-", ok: false},
		{name: "only a hyphen", id: "-", ok: false},
		{name: "uppercase", id: "Main", ok: false},
		{name: "underscore", id: "s3_main", ok: false},
		{name: "dot", id: "s3.main", ok: false},
		{name: "slash", id: "s3/main", ok: false},
		{name: "colon", id: "s3:main", ok: false},
		{name: "space", id: "s3 main", ok: false},
		{name: "messaging wildcard", id: "s3*", ok: false},
		{name: "non-ascii", id: "café", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateID(tc.id)
			if tc.ok && err != nil {
				t.Fatalf("ValidateID(%q) = %v, want nil", tc.id, err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatalf("ValidateID(%q) = nil, want an error", tc.id)
				}
				if !errors.Is(err, ErrInvalidID) {
					t.Fatalf("ValidateID(%q) = %v, want ErrInvalidID", tc.id, err)
				}
			}
		})
	}
}

// An oversized value must not turn into an oversized error and then an
// oversized log line, so every path that quotes a rejected value bounds it.
func TestRejectionErrorsAreBounded(t *testing.T) {
	t.Parallel()

	const (
		oversized = 1 << 20
		bound     = 256
	)

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "id past the length limit",
			call: func() error { return ValidateID(strings.Repeat("a", oversized)) },
		},
		{
			name: "id of illegal characters",
			call: func() error { return ValidateID(strings.Repeat("A", oversized)) },
		},
		{
			name: "id wrapped in hyphens",
			call: func() error { return ValidateID("-" + strings.Repeat("a", oversized) + "-") },
		},
		{
			name: "ref with no separator",
			call: func() error { _, err := Parse(strings.Repeat("x", oversized)); return err },
		},
		{
			name: "uid of the wrong length",
			call: func() error { _, err := ParseUID(strings.Repeat("A", oversized)); return err },
		},
		{
			name: "id of multi-byte runes",
			call: func() error { return ValidateID(strings.Repeat("é", oversized)) },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.call()
			if err == nil {
				t.Fatal("the oversized value was accepted")
			}
			if got := len(err.Error()); got > bound {
				t.Fatalf("the error is %d bytes, the bound is %d: it quotes the rejected value", got, bound)
			}
		})
	}
}

func TestEchoCutsOnARuneBoundary(t *testing.T) {
	t.Parallel()

	got := echo(strings.Repeat("é", maxEchoLen))
	if !utf8.ValidString(got) {
		t.Fatalf("echo produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, ellipsis) {
		t.Fatalf("echo(%q) did not mark the cut", got)
	}
}

func TestKindValid(t *testing.T) {
	t.Parallel()

	for _, k := range Kinds() {
		if !k.Valid() {
			t.Errorf("Kind(%q).Valid() = false, want true", k)
		}
	}
	for _, k := range []Kind{"", "Storage", "widget", "storage "} {
		if k.Valid() {
			t.Errorf("Kind(%q).Valid() = true, want false", k)
		}
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    Kind
		id      string
		wantErr error
	}{
		{name: "storage", kind: KindStorage, id: "s3-main"},
		{name: "db", kind: KindDB, id: "primary"},
		{name: "unknown kind", kind: "widget", id: "main", wantErr: ErrUnknownKind},
		{name: "empty kind", kind: "", id: "main", wantErr: ErrUnknownKind},
		{name: "invalid id", kind: KindSink, id: "Main", wantErr: ErrInvalidID},
		{name: "kind checked before id", kind: "widget", id: "Main", wantErr: ErrUnknownKind},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := New(tc.kind, tc.id)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("New(%q, %q) error = %v, want %v", tc.kind, tc.id, err, tc.wantErr)
				}
				if !got.IsZero() {
					t.Fatalf("New(%q, %q) = %v, want the zero Ref on error", tc.kind, tc.id, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%q, %q) = %v, want nil", tc.kind, tc.id, err)
			}
			if got.Kind() != tc.kind || got.ID() != tc.id {
				t.Fatalf("New(%q, %q) = %v/%v", tc.kind, tc.id, got.Kind(), got.ID())
			}
			if got.IsZero() {
				t.Fatal("IsZero() = true for a constructed Ref")
			}
		})
	}
}

func TestRefRoundTrip(t *testing.T) {
	t.Parallel()

	for _, kind := range Kinds() {
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()

			want, err := New(kind, "s3-main-01")
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			if got := want.String(); got != kind.String()+":s3-main-01" {
				t.Fatalf("String() = %q", got)
			}
			if got := want.Path(); got != kind.String()+"/s3-main-01" {
				t.Fatalf("Path() = %q", got)
			}

			got, err := Parse(want.String())
			if err != nil {
				t.Fatalf("Parse(%q): %v", want.String(), err)
			}
			if got != want {
				t.Fatalf("Parse(%q) = %v, want %v", want.String(), got, want)
			}

			text, err := want.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText: %v", err)
			}
			var back Ref
			if uerr := back.UnmarshalText(text); uerr != nil {
				t.Fatalf("UnmarshalText(%q): %v", text, uerr)
			}
			if back != want {
				t.Fatalf("UnmarshalText(%q) = %v, want %v", text, back, want)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantErr error
	}{
		{name: "no separator", in: "storage", wantErr: ErrMalformed},
		{name: "empty", in: "", wantErr: ErrMalformed},
		{name: "path form", in: "storage/main", wantErr: ErrMalformed},
		{name: "missing id", in: "storage:", wantErr: ErrInvalidID},
		{name: "missing kind", in: ":main", wantErr: ErrUnknownKind},
		{name: "unknown kind", in: "widget:main", wantErr: ErrUnknownKind},
		{name: "second separator lands in the id", in: "storage:a:b", wantErr: ErrInvalidID},
		{name: "uppercase id", in: "storage:MAIN", wantErr: ErrInvalidID},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Parse(tc.in); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Parse(%q) error = %v, want %v", tc.in, err, tc.wantErr)
			}
		})
	}
}

func TestUnmarshalTextRejectsInvalid(t *testing.T) {
	t.Parallel()

	valid, err := New(KindAuth, "oidc")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := valid
	if uerr := got.UnmarshalText([]byte("widget:main")); uerr == nil {
		t.Fatal("UnmarshalText accepted an unknown kind")
	}
	if got != valid {
		t.Fatalf("UnmarshalText overwrote the Ref on error: %v", got)
	}
}

// TestKindPartition pins the two halves of the closed set. A configurable kind
// is declared in configuration and created, edited and deleted through the
// resource API; an intrinsic kind names something the system already knows and
// is never declared. The lists are written out rather than derived, so widening
// Configurable cannot widen this with it.
func TestKindPartition(t *testing.T) {
	t.Parallel()

	configurable := []Kind{
		KindStorage, KindAuth, KindSink, KindDriver, KindDB, KindCamera, KindHistory, KindLink, KindAsset,
		KindGCodePreset, KindDashboard,
	}
	intrinsic := []Kind{KindPrinter, KindNode}

	for _, k := range Kinds() {
		want := slices.Contains(configurable, k)
		if want == slices.Contains(intrinsic, k) {
			t.Fatalf("Kind(%q) is in neither half of the partition, or in both", k)
		}
		if got := k.Configurable(); got != want {
			t.Errorf("Kind(%q).Configurable() = %v, want %v", k, got, want)
		}
	}

	if got := ConfigurableKinds(); !slices.Equal(got, configurable) {
		t.Fatalf("ConfigurableKinds() = %v, want %v", got, configurable)
	}
}

// TestPrinterAndNodeAreNamed is the addressing half of the gap this closed: the
// event hub can carry a printer, a camera or a node origin, and a kind the
// registry does not name is a kind authorization cannot describe.
func TestPrinterAndNodeAreNamed(t *testing.T) {
	t.Parallel()

	for _, kind := range []Kind{KindPrinter, KindCamera, KindNode} {
		if !kind.Valid() {
			t.Errorf("Kind(%q).Valid() = false, want true", kind)
		}
	}

	instance, err := New(KindPrinter, "01jb8xn2k7qw9zr4mv3t6h0pda")
	if err != nil {
		t.Fatalf("New(printer): %v", err)
	}
	if got := instance.String(); got != "printer:01jb8xn2k7qw9zr4mv3t6h0pda" {
		t.Fatalf("String() = %q", got)
	}
}

// TestEveryKindCarriesOneClass holds the classification total over the closed
// set. A kind added without a class reaches Classify's unknown branch and is
// then refused everywhere, which is a kind that compiles and never works.
func TestEveryKindCarriesOneClass(t *testing.T) {
	t.Parallel()

	for _, kind := range Kinds() {
		class, err := kind.Classify()
		if err != nil {
			t.Errorf("Kind(%q).Classify(): %v", kind, err)

			continue
		}

		if !slices.Contains([]Class{ClassSupervised, ClassStored, ClassIntrinsic, ClassDevice}, class) {
			t.Errorf("Kind(%q).Classify() = %q, which is not one of the four", kind, class)
		}
	}

	if len(classOfKind) != len(Kinds()) {
		t.Fatalf("the class table holds %d kinds and the closed set holds %d",
			len(classOfKind), len(Kinds()))
	}
}

// TestClassifySeparatesAnUnknownKindFromADeclaredOne holds the one answer that
// separates the two cases. Configurable and Supervised are both false for
// printer and false for a kind nobody declared, so a caller that refuses on
// either alone cannot say which of the two it has.
func TestClassifySeparatesAnUnknownKindFromADeclaredOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		kind  Kind
		class Class
		known bool
	}{
		{name: "supervised", kind: KindStorage, class: ClassSupervised, known: true},
		{name: "stored", kind: KindGCodePreset, class: ClassStored, known: true},
		{name: "intrinsic", kind: KindPrinter, class: ClassIntrinsic, known: true},
		{name: "device", kind: KindCamera, class: ClassDevice, known: true},
		{name: "outside the set", kind: "spool", known: false},
		{name: "empty", kind: "", known: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			class, err := tc.kind.Classify()
			switch {
			case tc.known && err != nil:
				t.Fatalf("Kind(%q).Classify(): %v", tc.kind, err)
			case !tc.known && !errors.Is(err, ErrUnknownKind):
				t.Fatalf("Kind(%q).Classify() error = %v, want ErrUnknownKind", tc.kind, err)
			case tc.known && class != tc.class:
				t.Fatalf("Kind(%q).Classify() = %q, want %q", tc.kind, class, tc.class)
			}

			if tc.kind.Configurable() != (tc.class == ClassSupervised || tc.class == ClassStored ||
				tc.class == ClassDevice) {
				t.Errorf("Kind(%q).Configurable() disagrees with its class %q", tc.kind, tc.class)
			}

			if tc.kind.Supervised() != (tc.class == ClassSupervised) {
				t.Errorf("Kind(%q).Supervised() disagrees with its class %q", tc.kind, tc.class)
			}
		})
	}
}

// TestAnUnknownKindNamesTheClosedSet holds the message an operator acts on. The
// refusal is worth nothing if it does not say what the legal values are.
func TestAnUnknownKindNamesTheClosedSet(t *testing.T) {
	t.Parallel()

	_, err := Kind("spool").Classify()
	if err == nil {
		t.Fatal("Classify(spool) returned no error")
	}

	for _, kind := range Kinds() {
		if !strings.Contains(err.Error(), kind.String()) {
			t.Errorf("the refusal does not name %q: %s", kind, err)
		}
	}
}

// TestEverySupervisedKindSaysWhetherAnythingReadsItBack keeps the reader table
// total. A supervised kind missing from it reads false by the zero value, which
// would refuse every declaration of a kind that works.
func TestEverySupervisedKindSaysWhetherAnythingReadsItBack(t *testing.T) {
	t.Parallel()

	for _, kind := range SupervisedKinds() {
		if _, named := readerOfKind[kind]; !named {
			t.Errorf("Kind(%q) is supervised and the reader table does not name it", kind)
		}
	}

	for kind := range readerOfKind {
		if !kind.Supervised() {
			t.Errorf("Kind(%q) is in the reader table and is not supervised", kind)
		}
	}

	both := slices.Concat(ReadKinds(), UnreadKinds())
	slices.Sort(both)

	supervised := slices.Clone(SupervisedKinds())
	slices.Sort(supervised)

	if !slices.Equal(both, supervised) {
		t.Fatalf("ReadKinds() and UnreadKinds() cover %v, want the supervised set %v", both, supervised)
	}
}

// TestAKindOutsideTheSupervisedSetIsReadByNothing pins the answer a caller gets
// for a kind with no instance to read. It has to be false rather than a panic,
// because the refusal path asks this before it asks anything else.
func TestAKindOutsideTheSupervisedSetIsReadByNothing(t *testing.T) {
	t.Parallel()

	for _, kind := range []Kind{KindGCodePreset, KindPrinter, KindNode, Kind("spool")} {
		if kind.Read() {
			t.Errorf("Kind(%q).Read() = true, want false", kind)
		}
	}
}
