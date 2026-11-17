package files_test

import (
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// The tree of fractal-files.md, Encoding: a.txt, sub/b.txt and Ä.txt, which
// hold aaa, bbbbb and ccccccc.
const (
	subListingHex = "8201818465622e74787401055821015e846c64f2db12266e6b658a8e5b5b" +
		"42cc225419b3ee1fca88acbb181ddfdb52"
	subListingText = "sha256:V9Re6x1ciTvvUevzuaMMMtQYcihwlrppm_NW9migS4o"
	rootListingHex = "8201838465612e74787401035821019834876dcfb05cb167a5c24953eba5" +
		"8c4ac89b1adf57f28f2f9d09af107ee8f0846373756202182f58210157d4" +
		"5eeb1d5c893bef51ebf3b9a30c32d41872287096ba699bf356f668a04b8a" +
		"8466c3842e7478740107582101cd4f3afa6982937e1fcc283c451f1cd9d3" +
		"68e998b554ff7b06eabe6e723a39c2"
	rootListingText = "sha256:ZFCqxq_kPS8JxunJomCzq-cQErV8Ql8blnPlki1IEeg"
)

// treeListings returns the listing of sub and the root listing of the tree,
// with the root's entries in an order Encode must change.
func treeListings(t *testing.T) (sub, root files.Listing) {
	t.Helper()
	sub = files.Listing{Entries: []files.Entry{
		{Name: "b.txt", Kind: files.KindFile, Size: 5, Hash: files.Sum([]byte("bbbbb"))},
	}}
	subBytes, err := sub.Encode()
	if err != nil {
		t.Fatalf("encode sub: %v", err)
	}
	root = files.Listing{Entries: []files.Entry{
		{Name: "Ä.txt", Kind: files.KindFile, Size: 7, Hash: files.Sum([]byte("ccccccc"))},
		{Name: "sub", Kind: files.KindDirectory, Size: uint64(len(subBytes)), Hash: files.Sum(subBytes)},
		{Name: "a.txt", Kind: files.KindFile, Size: 3, Hash: files.Sum([]byte("aaa"))},
	}}
	return sub, root
}

// encoded is a listing as Encode writes it and the identity of those bytes.
type encoded struct {
	Hex  string
	Text string
}

// encode returns l in hex and its identity.
func encode(t *testing.T, l files.Listing) encoded {
	t.Helper()
	b, err := l.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return encoded{Hex: hex.EncodeToString(b), Text: files.Sum(b).Text()}
}

// unhex decodes s, which the test wrote, and ignores spaces in it.
func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad hex in the test: %v", err)
	}
	return b
}

func TestEmptyListing(t *testing.T) {
	t.Parallel()
	got := []encoded{encode(t, files.Listing{}), {Text: files.EmptyListing.Text()}}
	want := []encoded{{Hex: "820180", Text: emptyListingText}, {Text: emptyListingText}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the empty listing is %+v, want %+v", got, want)
	}
}

func TestTreeListing(t *testing.T) {
	t.Parallel()
	sub, root := treeListings(t)
	got := []encoded{encode(t, sub), encode(t, root)}
	want := []encoded{{Hex: subListingHex, Text: subListingText}, {Hex: rootListingHex, Text: rootListingText}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the tree's listings are %+v, want %+v", got, want)
	}
}

func TestDecodeListing(t *testing.T) {
	t.Parallel()
	_, root := treeListings(t)
	got, err := files.DecodeListing(unhex(t, rootListingHex))
	want := files.Listing{Entries: []files.Entry{root.Entries[2], root.Entries[1], root.Entries[0]}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("DecodeListing = %+v, %v, want %+v", got, err, want)
	}
	empty, err := files.DecodeListing([]byte{0x82, 0x01, 0x80})
	if err != nil || !reflect.DeepEqual(empty, files.Listing{}) {
		t.Fatalf("DecodeListing(82 01 80) = %+v, %v, want an empty listing", empty, err)
	}
}

func TestEncodeRefuses(t *testing.T) {
	t.Parallel()
	hash := files.Sum([]byte("aaa"))
	cases := []struct {
		name    string
		entries []files.Entry
		is      error
	}{
		{
			name: "duplicate",
			entries: []files.Entry{
				{Name: "a", Kind: files.KindFile, Hash: hash},
				{Name: "a", Kind: files.KindDirectory, Hash: hash},
			},
			is: files.ErrBadName,
		},
		{name: "empty name", entries: []files.Entry{{Kind: files.KindFile, Hash: hash}}, is: files.ErrBadName},
		{name: "slash", entries: []files.Entry{{Name: "a/b", Kind: files.KindFile, Hash: hash}}, is: files.ErrBadName},
		{name: "kind zero", entries: []files.Entry{{Name: "a", Hash: hash}}, is: files.ErrMalformed},
		{name: "kind three", entries: []files.Entry{{Name: "a", Kind: 3, Hash: hash}}, is: files.ErrMalformed},
		{name: "zero hash", entries: []files.Entry{{Name: "a", Kind: files.KindFile}}, is: files.ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := (files.Listing{Entries: tc.entries}).Encode(); !errors.Is(err, tc.is) {
				t.Fatalf("Encode = %v, want %v", err, tc.is)
			}
		})
	}
}

// entryHex is one entry in hex: a name of fewer than 24 bytes, a kind, a size
// of less than 24 and the byte form of the identity of "aaa".
func entryHex(name string, kind byte) string {
	return hex.EncodeToString([]byte{0x84, 0x60 + byte(len(name))}) + hex.EncodeToString([]byte(name)) +
		hex.EncodeToString([]byte{kind, 0x03, 0x58, 0x21}) +
		hex.EncodeToString(files.Sum([]byte("aaa")).Bytes())
}

func TestDecodeListingRefuses(t *testing.T) {
	t.Parallel()
	digest := hex.EncodeToString(files.Sum([]byte("aaa")).Bytes()[1:])
	cases := []struct {
		name string
		hex  string
		is   error
	}{
		{name: "empty", hex: "", is: files.ErrMalformed},
		{name: "version 2", hex: "82 02 80", is: files.ErrUnsupportedVersion},
		{name: "version 2 with three values", hex: "83 02 80 00", is: files.ErrUnsupportedVersion},
		{name: "version 0", hex: "82 00 80", is: files.ErrUnsupportedVersion},
		{name: "three values", hex: "83 01 80 00", is: files.ErrMalformed},
		{name: "no version", hex: "80", is: files.ErrMalformed},
		{name: "a map", hex: "a0", is: files.ErrMalformed},
		{name: "indefinite array", hex: "9f 01 80 ff", is: files.ErrMalformed},
		{name: "indefinite entries", hex: "82 01 9f ff", is: files.ErrMalformed},
		{name: "long version", hex: "82 18 01 80", is: files.ErrMalformed},
		{name: "long entry count", hex: "82 01 98 00", is: files.ErrMalformed},
		{name: "trailing byte", hex: "82 01 80 00", is: files.ErrMalformed},
		{name: "truncated", hex: rootListingHex[:len(rootListingHex)-2], is: files.ErrMalformed},
		{name: "unsorted", hex: "82 01 82" + entryHex("b", 1) + entryHex("a", 1), is: files.ErrMalformed},
		{name: "duplicate", hex: "82 01 82" + entryHex("a", 1) + entryHex("a", 2), is: files.ErrMalformed},
		{name: "kind 3", hex: "82 01 81" + entryHex("a", 3), is: files.ErrMalformed},
		{name: "kind 0", hex: "82 01 81" + entryHex("a", 0), is: files.ErrMalformed},
		{name: "dot name", hex: "82 01 81" + entryHex(".", 1), is: files.ErrBadName},
		{name: "empty name", hex: "82 01 81" + entryHex("", 1), is: files.ErrBadName},
		{name: "invalid UTF-8", hex: "82 01 81" + entryHex("\xff", 1), is: files.ErrBadName},
		{name: "three fields", hex: "82 01 81 83 61 61 01 03", is: files.ErrMalformed},
		{name: "name as bytes", hex: "82 01 81 84 41 61 01 03 5821 01" + digest, is: files.ErrMalformed},
		{name: "indefinite name", hex: "82 01 81 84 7f 61 61 ff 01 03 5821 01" + digest, is: files.ErrMalformed},
		{name: "unknown algorithm", hex: "82 01 81 84 61 61 01 03 5821 02" + digest, is: files.ErrUnsupportedAlgorithm},
		{name: "short hash", hex: "82 01 81 84 61 61 01 03 5820 01" + digest[2:], is: files.ErrMalformed},
		{name: "hash as text", hex: "82 01 81 84 61 61 01 03 7821 01" + digest, is: files.ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := files.DecodeListing(unhex(t, tc.hex)); !errors.Is(err, tc.is) {
				t.Fatalf("DecodeListing(%s) = %v, want %v", tc.hex, err, tc.is)
			}
		})
	}
}
