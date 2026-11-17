package files_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// FuzzDecodeListing tests every input DecodeListing accepts. A listing it
// accepts is in the deterministic form, so Encode must write the same bytes
// back.
func FuzzDecodeListing(f *testing.F) {
	for _, s := range []string{"820180", subListingHex, rootListingHex, "820280", "83018000", "a0", "9f0180ff"} {
		b, err := hex.DecodeString(s)
		if err != nil {
			f.Fatalf("bad hex in the seed %q: %v", s, err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		l, err := files.DecodeListing(b)
		if err != nil {
			return
		}
		again, err := l.Encode()
		if err != nil {
			t.Fatalf("DecodeListing accepted %x, and Encode refused the result: %v", b, err)
		}
		if !bytes.Equal(again, b) {
			t.Fatalf("DecodeListing accepted %x, and Encode wrote %x", b, again)
		}
	})
}

// FuzzParseURL tests every input ParseURL accepts. The URL String writes
// must parse to the same URL, and its identity must have one text.
func FuzzParseURL(f *testing.F) {
	for _, s := range []string{
		"fractal:///" + emptyListingText,
		"FRACTAL:///" + emptyListingText + benchyQuery,
		"fractal:///" + emptyListingText + "?size=3&name=a+b&x",
		"fractal://hq/" + emptyListingText,
		"fractal:///" + emptyListingText + "#x",
		"fractal:///sha256%3A" + emptyListingText[7:],
		"fractal:///" + emptyListingText + "?name=%ZZ",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		u, err := files.ParseURL(s)
		if err != nil {
			return
		}
		again, err := files.ParseURL(u.String())
		if err != nil || again != u {
			t.Fatalf("ParseURL(%q) = %+v, and its String %q parses to %+v, %v", s, u, u.String(), again, err)
		}
		if id, textErr := files.ParseText(u.ID.Text()); textErr != nil || id != u.ID {
			t.Fatalf("the identity of %q does not parse back from %q: %v", s, u.ID.Text(), textErr)
		}
	})
}
