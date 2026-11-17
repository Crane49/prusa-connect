package files_test

import (
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// emptyListingText is the identity of the empty listing, 82 01 80, as
// fractal-files.md, Encoding, gives it.
const emptyListingText = "sha256:yvWzZXKtGDZw7s2x9uya3irUm_l_-Zv-kvojJgtG-_E"

// emptyListingBytes is the same identity in byte form, in hex.
const emptyListingBytes = "01caf5b36572ad183670eecdb1f6ec9ade2ad49bf97ff99bfe92fa23260b46fbf1"

// identityForms is an identity as each of its encodings shows it.
type identityForms struct {
	Text     string
	Bytes    string
	FromText files.Identity
	FromByte files.Identity
}

// formsOf returns id in both encodings and parsed back from each.
func formsOf(t *testing.T, id files.Identity) identityForms {
	t.Helper()
	fromText, err := files.ParseText(id.Text())
	if err != nil {
		t.Fatalf("ParseText(%q): %v", id.Text(), err)
	}
	fromByte, err := files.ParseBytes(id.Bytes())
	if err != nil {
		t.Fatalf("ParseBytes(%x): %v", id.Bytes(), err)
	}
	return identityForms{
		Text: id.Text(), Bytes: hex.EncodeToString(id.Bytes()), FromText: fromText, FromByte: fromByte,
	}
}

func TestIdentityEncodings(t *testing.T) {
	t.Parallel()
	id := files.Sum([]byte{0x82, 0x01, 0x80})
	want := identityForms{Text: emptyListingText, Bytes: emptyListingBytes, FromText: id, FromByte: id}
	if got := formsOf(t, id); got != want {
		t.Fatalf("the empty listing's identity is %+v, want %+v", got, want)
	}
}

func TestParseTextRefuses(t *testing.T) {
	t.Parallel()
	digest := emptyListingText[len("sha256:"):]
	cases := []struct {
		name string
		in   string
		is   error
	}{
		{name: "empty", in: "", is: files.ErrMalformed},
		{name: "no colon", in: "sha256" + digest, is: files.ErrMalformed},
		{name: "upper case name", in: "SHA256:" + digest, is: files.ErrUnsupportedAlgorithm},
		{name: "unknown name", in: "sha512:" + digest, is: files.ErrUnsupportedAlgorithm},
		{name: "short digest", in: "sha256:" + digest[1:], is: files.ErrMalformed},
		{name: "long digest", in: "sha256:" + digest + "A", is: files.ErrMalformed},
		{name: "padding", in: "sha256:" + digest + "=", is: files.ErrMalformed},
		{name: "unused bits set", in: "sha256:" + digest[:len(digest)-1] + "F", is: files.ErrMalformed},
		{
			name: "standard alphabet",
			in:   "sha256:" + "yvWzZXKtGDZw7s2x9uya3irUm/l/+Zv+kvojJgtG+/E",
			is:   files.ErrMalformed,
		},
		{name: "line break", in: "sha256:" + digest[:10] + "\n" + digest[10:], is: files.ErrMalformed},
		{name: "empty digest", in: "sha256:", is: files.ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := files.ParseText(tc.in); !errors.Is(err, tc.is) {
				t.Fatalf("ParseText(%q) = %v, want %v", tc.in, err, tc.is)
			}
		})
	}
}

func TestParseBytesRefuses(t *testing.T) {
	t.Parallel()
	valid, decodeErr := hex.DecodeString(emptyListingBytes)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	cases := []struct {
		name string
		in   []byte
		is   error
	}{
		{name: "empty", in: nil, is: files.ErrMalformed},
		{name: "type only", in: valid[:1], is: files.ErrMalformed},
		{name: "short digest", in: valid[:32], is: files.ErrMalformed},
		{name: "long digest", in: append(append([]byte{}, valid...), 0), is: files.ErrMalformed},
		{name: "unknown type", in: append([]byte{0x02}, valid[1:]...), is: files.ErrUnsupportedAlgorithm},
		{name: "zero type", in: append([]byte{0x00}, valid[1:]...), is: files.ErrUnsupportedAlgorithm},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := files.ParseBytes(tc.in); !errors.Is(err, tc.is) {
				t.Fatalf("ParseBytes(%x) = %v, want %v", tc.in, err, tc.is)
			}
		})
	}
}

func TestHasherMatchesSum(t *testing.T) {
	t.Parallel()
	h := files.NewHasher()
	for _, part := range []string{"bbb", "bb"} {
		if _, err := h.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := h.Identity(), files.Sum([]byte("bbbbb")); got != want {
		t.Fatalf("Hasher gave %s, want %s", got.Text(), want.Text())
	}
}

func TestZeroIdentity(t *testing.T) {
	t.Parallel()
	type state struct {
		IsZero bool
		Text   string
		Bytes  []byte
	}
	var zero files.Identity
	got := state{IsZero: zero.IsZero(), Text: zero.Text(), Bytes: zero.Bytes()}
	if want := (state{IsZero: true}); !reflect.DeepEqual(got, want) {
		t.Fatalf("the zero identity is %+v, want %+v", got, want)
	}
	if files.Sum(nil).IsZero() {
		t.Fatal("the identity of no bytes is the zero value")
	}
}
