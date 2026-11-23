package account

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// avatar asks the avatar route with the query, as PrusaSlicer does: no
// credential.
func avatar(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	Avatar(recorder, httptest.NewRequest(http.MethodGet, PathAvatar+"?"+query, nil))

	return recorder
}

// The me reply carries a relative avatar_small. Resolved against the me
// address, which ends in a slash, it is the avatar route under the same
// mount, with the initials of the name and the colour of the subject.
func TestMeAnswersARelativeAvatarUnderTheMeRoute(t *testing.T) {
	t.Parallel()

	_, body := me(t, "user:alice", staticPeople{{Subject: "user:alice", Name: "Alice (viewer)"}})

	ref, isString := body["avatar_small"].(string)
	if !isString {
		t.Fatalf("avatar_small is %v, want a string", body["avatar_small"])
	}

	base, err := url.Parse("https://pcl.example/api/v1/modules/ns/account/api/v1/me/")
	if err != nil {
		t.Fatal(err)
	}

	relative, err := url.Parse(ref)
	if err != nil {
		t.Fatal(err)
	}

	resolved := base.ResolveReference(relative)
	if resolved.Path != "/api/v1/modules/ns"+PathAvatar {
		t.Fatalf("avatar_small %q resolves to %s, want the avatar route under the mount", ref, resolved)
	}

	query := resolved.Query()
	if query.Get(queryInitials) != "AV" || query.Get(queryColour) != colourOf("user:alice") {
		t.Fatalf("avatar_small %q, want the initials AV and the colour of user:alice", ref)
	}

	if got := avatar(t, resolved.RawQuery); got.Code != http.StatusOK {
		t.Fatalf("the avatar route refused the me reply's own query: %d %s", got.Code, got.Body)
	}
}

func TestInitialsAreTheFirstLettersOfTwoWords(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"Dave":              "D",
		"alice smith jones": "AS",
		"Bob (operator)":    "BO",
		"  žofie   černá ":  "ŽČ",
		"(7) 42":            "",
		"":                  "",
	} {
		if got := initialsOf(name); got != want {
			t.Errorf("initialsOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// A name with no letter gets no avatar_small, and the slicer shows its own.
func TestMeLeavesOutTheAvatarOfANameWithNoLetter(t *testing.T) {
	t.Parallel()

	_, body := me(t, "user:7", staticPeople{{Subject: "user:7", Name: "7"}})
	if _, present := body["avatar_small"]; present {
		t.Fatalf("avatar_small is %v for a name with no letter", body["avatar_small"])
	}
}

// One subject always gets one colour, from the palette.
func TestTheColourIsStableAndFromThePalette(t *testing.T) {
	t.Parallel()

	for _, subject := range []string{"user:alice", "user:bob", "user:7", ""} {
		first := colourOf(subject)
		if first != colourOf(subject) {
			t.Fatalf("%q got two colours", subject)
		}

		if _, _, err := avatarQuery(queryInitials + "=A&" + queryColour + "=" + first); err != nil {
			t.Fatalf("%q got %s, which the route refuses: %v", subject, first, err)
		}
	}
}

// The route answers a PNG of the avatar's size with no credential, and a
// client may keep it as long as it likes.
func TestTheAvatarIsAPNGWithNoCredential(t *testing.T) {
	t.Parallel()

	for _, initials := range []string{"D", "AS", "Žč", "日本"} {
		query := url.Values{queryInitials: {initials}, queryColour: {palette[0]}}.Encode()

		got := avatar(t, query)
		if got.Code != http.StatusOK {
			t.Fatalf("%s: answered %d: %s", initials, got.Code, got.Body)
		}

		if got.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s: Content-Type is %q", initials, got.Header().Get("Content-Type"))
		}

		if got.Header().Get("Cache-Control") != avatarCache {
			t.Errorf("%s: Cache-Control is %q", initials, got.Header().Get("Cache-Control"))
		}

		picture, err := png.Decode(bytes.NewReader(got.Body.Bytes()))
		if err != nil {
			t.Fatalf("%s: not a PNG: %v", initials, err)
		}

		if bounds := picture.Bounds(); bounds.Dx() != avatarSize || bounds.Dy() != avatarSize {
			t.Fatalf("%s: the picture is %v, want %dx%d", initials, bounds, avatarSize, avatarSize)
		}

		// PrusaSlicer rounds the avatar itself and fails on an alpha channel.
		if _, isRGBA := picture.(*image.RGBA); !isRGBA {
			t.Errorf("%s: the picture is %T, want an opaque RGB PNG", initials, picture)
		}

		if _, _, _, alpha := picture.At(0, 0).RGBA(); alpha != 0xffff {
			t.Errorf("%s: the corner is not opaque", initials)
		}
	}
}

func TestABadAvatarQueryIsRefused(t *testing.T) {
	t.Parallel()

	colour := queryColour + "=" + palette[0]

	for name, query := range map[string]string{
		"no initials":              colour,
		"empty initials":           queryInitials + "=&" + colour,
		"three letters":            queryInitials + "=ABC&" + colour,
		"a digit":                  queryInitials + "=A1&" + colour,
		"markup":                   queryInitials + "=%3C&" + colour,
		"a combining mark":         queryInitials + "=%CC%81&" + colour,
		"invalid UTF-8":            queryInitials + "=%FF&" + colour,
		"no colour":                queryInitials + "=A",
		"a colour off the palette": queryInitials + "=A&" + queryColour + "=000000",
		"a colour with a hash":     queryInitials + "=A&" + queryColour + "=%23" + palette[0],
		"a key given twice":        queryInitials + "=A&" + queryInitials + "=B&" + colour,
		"an unknown key":           queryInitials + "=A&" + colour + "&size=4096",
		"a broken query":           queryInitials + "=%ZZ&" + colour,
	} {
		if got := avatar(t, query); got.Code != http.StatusBadRequest {
			t.Errorf("%s: answered %d, want 400", name, got.Code)
		}
	}
}
