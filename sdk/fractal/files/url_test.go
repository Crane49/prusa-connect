package files_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// benchyName is the name with punctuation in fractal-files.md, The URL, and
// benchyQuery the query that carries it.
const (
	benchyName  = "Benchy & Co + 3DBenchy #2 (Ä).bgcode"
	benchyQuery = "?name=Benchy%20%26%20Co%20%2B%203DBenchy%20%232%20%28%C3%84%29.bgcode"
)

func TestURLString(t *testing.T) {
	t.Parallel()
	id := files.Sum([]byte{0x82, 0x01, 0x80})
	got := []string{files.URL{ID: id}.String(), files.URL{ID: id, Name: benchyName}.String()}
	want := []string{"fractal:///" + emptyListingText, "fractal:///" + emptyListingText + benchyQuery}
	if !slices.Equal(got, want) {
		t.Fatalf("String gave %q, want %q", got, want)
	}
}

func TestParseURL(t *testing.T) {
	t.Parallel()
	id := files.Sum([]byte{0x82, 0x01, 0x80})
	cases := []struct {
		name string
		in   string
		want files.URL
	}{
		{name: "plain", in: "fractal:///" + emptyListingText, want: files.URL{ID: id}},
		{name: "upper case scheme", in: "FRACTAL:///" + emptyListingText, want: files.URL{ID: id}},
		{name: "mixed case scheme", in: "Fractal:///" + emptyListingText, want: files.URL{ID: id}},
		{name: "name", in: "fractal:///" + emptyListingText + benchyQuery, want: files.URL{ID: id, Name: benchyName}},
		{
			name: "unknown parameter",
			in:   "fractal:///" + emptyListingText + "?size=3&name=a.txt&x",
			want: files.URL{ID: id, Name: "a.txt"},
		},
		{name: "empty query", in: "fractal:///" + emptyListingText + "?", want: files.URL{ID: id}},
		{
			name: "plus is a plus",
			in:   "fractal:///" + emptyListingText + "?name=a+b",
			want: files.URL{ID: id, Name: "a+b"},
		},
		{name: "empty name", in: "fractal:///" + emptyListingText + "?name=", want: files.URL{ID: id}},
		{name: "slash in name", in: "fractal:///" + emptyListingText + "?name=a%2Fb", want: files.URL{ID: id}},
		{name: "dot dot name", in: "fractal:///" + emptyListingText + "?name=..", want: files.URL{ID: id}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := files.ParseURL(tc.in)
			if err != nil || got != tc.want {
				t.Fatalf("ParseURL(%q) = %+v, %v, want %+v", tc.in, got, err, tc.want)
			}
		})
	}
}

func TestURLQueryDoesNotChangeIdentity(t *testing.T) {
	t.Parallel()
	a, errA := files.ParseURL("fractal:///" + emptyListingText + "?name=a.txt")
	b, errB := files.ParseURL("fractal:///" + emptyListingText + "?name=b.txt&v=2")
	if errA != nil || errB != nil || a.ID != b.ID {
		t.Fatalf("the two URLs name %s (%v) and %s (%v), want one object", a.ID.Text(), errA, b.ID.Text(), errB)
	}
}

func TestParseURLRefuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		is   error
	}{
		{name: "empty", in: "", is: files.ErrMalformed},
		{name: "other scheme", in: "https:///" + emptyListingText, is: files.ErrMalformed},
		{name: "no scheme", in: "/" + emptyListingText, is: files.ErrMalformed},
		{name: "authority", in: "fractal://hq/" + emptyListingText, is: files.ErrMalformed},
		{name: "one slash", in: "fractal:/" + emptyListingText, is: files.ErrMalformed},
		{name: "no slash", in: "fractal:" + emptyListingText, is: files.ErrMalformed},
		{name: "fragment", in: "fractal:///" + emptyListingText + "#x", is: files.ErrMalformed},
		{name: "fragment after query", in: "fractal:///" + emptyListingText + "?name=a#x", is: files.ErrMalformed},
		{name: "percent in path", in: "fractal:///sha256%3A" + emptyListingText[7:], is: files.ErrMalformed},
		{name: "segment after identity", in: "fractal:///" + emptyListingText + "/a", is: files.ErrMalformed},
		{name: "unknown algorithm", in: "fractal:///sha1:" + emptyListingText[7:], is: files.ErrUnsupportedAlgorithm},
		{name: "bad escape", in: "fractal:///" + emptyListingText + "?name=%ZZ", is: files.ErrMalformed},
		{name: "two names", in: "fractal:///" + emptyListingText + "?name=a&name=b", is: files.ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := files.ParseURL(tc.in); !errors.Is(err, tc.is) {
				t.Fatalf("ParseURL(%q) = %v, want %v", tc.in, err, tc.is)
			}
		})
	}
}
