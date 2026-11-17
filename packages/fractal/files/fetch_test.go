package files_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/files"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// obtained is what one Obtain gave and how often the parent was asked.
type obtained struct {
	Storage string
	Source  string
	Failure string
	Asked   int
}

func copyName(data []byte) string {
	sum := sha256.Sum256(data)

	return "sha256-" + hex.EncodeToString(sum[:])
}

// facade builds Files over a real cache and a scripted parent. A nil parent
// leaves the server with no parent.
func facade(
	t *testing.T, published shelf, store *chunkStore, parent *scripted, attempts int,
) (*files.Files, *files.Cache) {
	t.Helper()

	cache := openCache(t, t.TempDir(), 1<<20)

	return facadeOver(t, cache, published, store, parent, attempts), cache
}

// facadeOver is facade over a cache the test opened.
func facadeOver(
	t *testing.T, cache *files.Cache, published shelf, store *chunkStore, parent *scripted, attempts int,
) *files.Files {
	t.Helper()

	signer, _ := newSigner(t)
	cfg := files.FilesConfig{Publisher: published, Storage: store, Cache: cache, Attempts: attempts}

	if parent != nil {
		cfg.Client = files.NewClient(
			files.ClientConfig{Signer: signer, Anchors: parent.anchors(), Timeout: time.Second},
		)
		cfg.Parent = func() (string, bool) { return parent.srv.URL, true }
	}

	return files.NewFiles(cfg)
}

func TestReachable(t *testing.T) {
	t.Parallel()

	published := []byte("published")
	held := []byte("held")
	elsewhere := []byte("elsewhere")

	alone, cache := facade(
		t, shelf{sdkfiles.Sum(published): {Storage: "library", Path: "p.gcode", Size: 9}}, &chunkStore{}, nil, 1,
	)
	put(t, cache, string(held), files.Retention{Unlimited: true})
	below, _ := facade(t, shelf{}, &chunkStore{}, newScripted(t, status(http.StatusNotFound, nil)), 1)

	got := map[string]bool{
		"published":               alone.Reachable(t.Context(), sdkfiles.Sum(published)),
		"a copy":                  alone.Reachable(t.Context(), sdkfiles.Sum(held)),
		"no way":                  alone.Reachable(t.Context(), sdkfiles.Sum(elsewhere)),
		"anything below a parent": below.Reachable(t.Context(), sdkfiles.Sum(elsewhere)),
	}
	want := map[string]bool{"published": true, "a copy": true, "no way": false, "anything below a parent": true}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Reachable gave %v, want %v", got, want)
	}
}

func TestObtain(t *testing.T) {
	t.Parallel()

	data := []byte("GCDE cube")

	listing, err := sdkfiles.Listing{}.Encode()
	if err != nil {
		t.Fatalf("encode a listing: %v", err)
	}

	for name, tc := range map[string]struct {
		published shelf
		kept      bool
		steps     []http.HandlerFunc
		id        sdkfiles.Identity
		want      obtained
	}{
		"published": {
			published: shelf{sdkfiles.Sum(data): {Storage: "library", Path: "prints/cube.bgcode", Size: 9}},
			id:        sdkfiles.Sum(data),
			want:      obtained{Storage: "library", Source: "prints/cube.bgcode"},
		},
		"a listing": {
			published: shelf{sdkfiles.EmptyListing: {Listing: listing}},
			id:        sdkfiles.EmptyListing,
			want:      obtained{Failure: files.ErrNotAFile.Error()},
		},
		"a kept copy": {
			kept: true, steps: []http.HandlerFunc{status(http.StatusNotFound, nil)}, id: sdkfiles.Sum(data),
			want: obtained{Storage: deviceapi.ModuleDataStorage, Source: copyName(data)},
		},
		"fetched": {
			steps: []http.HandlerFunc{whole(data, keptFor())}, id: sdkfiles.Sum(data),
			want: obtained{Storage: deviceapi.ModuleDataStorage, Source: copyName(data), Asked: 1},
		},
		"not available": {
			steps: []http.HandlerFunc{status(http.StatusNotFound, nil)}, id: sdkfiles.Sum(data),
			want: obtained{Failure: files.ErrNotAvailable.Error(), Asked: 1},
		},
		"no parent": {
			id:   sdkfiles.Sum(data),
			want: obtained{Failure: files.ErrNoParent.Error()},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var parent *scripted
			if tc.steps != nil {
				parent = newScripted(t, tc.steps...)
			}

			published := tc.published
			if published == nil {
				published = shelf{}
			}

			facadeUnderTest, cache := facade(t, published, &chunkStore{}, parent, 1)
			if tc.kept {
				put(t, cache, string(data), files.Retention{Unlimited: true})
			}

			storage, source, release, obtainErr := facadeUnderTest.Obtain(t.Context(), sdkfiles.URL{ID: tc.id})
			if release != nil {
				defer release()
			}

			got := obtained{Storage: storage, Source: source, Failure: knownFailure(obtainErr)}
			if parent != nil {
				got.Asked = len(parent.requests())
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Obtain gave %+v, want %+v", got, tc.want)
			}
		})
	}
}

// knownFailure names the sentinel an Obtain error matches.
func knownFailure(err error) string {
	if err == nil {
		return ""
	}

	for _, known := range []error{
		files.ErrNotAFile, files.ErrNotAvailable, files.ErrNoParent, files.ErrRefused, files.ErrDropped,
		files.ErrTooLarge, files.ErrRetention,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}

	return err.Error()
}

// TestObtainRefusesALimitedRetention: an object whose retention is not
// unlimited is not printed, because the printer keeps the file it prints and
// the module cannot discard that copy when the retention ends
// (fractal-files.md, Retention). The fetch is not tried again, and no copy
// stays.
func TestObtainRefusesALimitedRetention(t *testing.T) {
	t.Parallel()

	data := []byte("G28 ; once")
	until := http.Header{
		"Cache-Control": {"private"},
		"Expires":       {time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)},
	}

	type outcome struct {
		Failure string
		Asked   int
		Held    string
	}

	got := map[string]outcome{}

	for name, directives := range map[string]http.Header{"none": nil, "an instant": until} {
		parent := newScripted(t, whole(data, directives))
		facadeUnderTest, cache := facade(t, shelf{}, &chunkStore{}, parent, 3)

		_, _, release, err := facadeUnderTest.Obtain(t.Context(), sdkfiles.URL{ID: sdkfiles.Sum(data)})
		if release != nil {
			defer release()
		}

		got[name] = outcome{
			Failure: knownFailure(err), Asked: len(parent.requests()), Held: cache.Path(sdkfiles.Sum(data)),
		}
	}

	refused := outcome{Failure: files.ErrRetention.Error(), Asked: 1}
	if want := map[string]outcome{"none": refused, "an instant": refused}; !reflect.DeepEqual(got, want) {
		t.Errorf("Obtain gave %+v, want %+v", got, want)
	}
}

// TestListingStopsPastTheLimit: an object larger than a listing may be is
// refused, and nothing of it is kept.
func TestListingStopsPastTheLimit(t *testing.T) {
	t.Parallel()

	big := bytes.Repeat([]byte("G1 X1\n"), 4096)
	parent := newScripted(t, whole(big, keptFor()))
	facadeUnderTest, cache := facade(t, shelf{}, &chunkStore{}, parent, 1)

	_, err := facadeUnderTest.Listing(t.Context(), sdkfiles.Sum(big), 64)

	got := []string{knownFailure(err), cache.Path(sdkfiles.Sum(big))}
	if want := []string{files.ErrTooLarge.Error(), ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("Listing gave %q, want %q", got, want)
	}
}

func TestPrinterPaths(t *testing.T) {
	t.Parallel()

	binary := []byte("GCDE binary")
	text := []byte("G28 ; text")
	short := func(data []byte) string {
		return strings.TrimPrefix(sdkfiles.Sum(data).Text(), "sha256:")[:12]
	}
	stem := func(data []byte) string { return "usb/fractal-" + short(data) }

	store := &chunkStore{files: map[string][]byte{"library/b.bgcode": binary, "library/t.gcode": text}}
	facadeUnderTest, cache := facade(t, shelf{}, store, nil, 1)
	put(t, cache, string(binary), files.Retention{Unlimited: true})

	printerPaths := func(data []byte, name, storage, source string) []string {
		return facadeUnderTest.PrinterPaths(
			t.Context(),
			sdkfiles.URL{ID: sdkfiles.Sum(data), Name: name},
			storage,
			source,
		)
	}

	got := map[string][]string{
		"a name":               printerPaths(text, "part.gcode", "library", "t.gcode"),
		"a name with dots":     printerPaths(text, "my.part.gcode", "library", "t.gcode"),
		"a bad name":           printerPaths(binary, "a/b", "library", "b.bgcode"),
		"a model":              printerPaths(text, "part.stl", "library", "t.gcode"),
		"a backslash":          printerPaths(text, `a\part.gcode`, "library", "t.gcode"),
		"a reserved character": printerPaths(text, "part?.gcode", "library", "t.gcode"),
		"a capital extension":  printerPaths(binary, "PART.BGCODE", "library", "b.bgcode"),
		"a name not in NFC":    printerPaths(text, "cafe\u0301.gcode", "library", "t.gcode"),
		"binary from storage":  printerPaths(binary, "", "library", "b.bgcode"),
		"text from storage":    printerPaths(text, "", "library", "t.gcode"),
		"binary from the copy": printerPaths(binary, "", deviceapi.ModuleDataStorage, cache.Path(sdkfiles.Sum(binary))),
	}
	want := map[string][]string{
		"a name":               {"usb/part.gcode", "usb/part-" + short(text) + ".gcode"},
		"a name with dots":     {"usb/my.part.gcode", "usb/my.part-" + short(text) + ".gcode"},
		"a bad name":           {stem(binary) + ".bgcode"},
		"a model":              {stem(text) + ".gcode"},
		"a backslash":          {stem(text) + ".gcode"},
		"a reserved character": {stem(text) + ".gcode"},
		"a capital extension":  {"usb/PART.BGCODE", "usb/PART-" + short(binary) + ".BGCODE"},
		"a name not in NFC":    {stem(text) + ".gcode"},
		"binary from storage":  {stem(binary) + ".bgcode"},
		"text from storage":    {stem(text) + ".gcode"},
		"binary from the copy": {stem(binary) + ".bgcode"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("PrinterPaths gave %v, want %v", got, want)
	}
}

func TestObtainRetries(t *testing.T) {
	t.Parallel()

	data := []byte("GCDE cube")

	for name, tc := range map[string]struct {
		steps    []http.HandlerFunc
		attempts int
		want     obtained
		atLeast  time.Duration
	}{
		"after the date it was given": {
			steps: []http.HandlerFunc{
				// The date is cut to the second, so it is between 2 and 3 seconds after
				// the request: more than retryFloor. It is made at the request, because
				// a parallel subtest may start long after this table is built.
				func(w http.ResponseWriter, r *http.Request) {
					date := time.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat)
					status(http.StatusServiceUnavailable, http.Header{"Retry-After": {date}})(w, r)
				},
				whole(data, keptFor()),
			},
			attempts: 3,
			want:     obtained{Storage: deviceapi.ModuleDataStorage, Source: copyName(data), Asked: 2},
			atLeast:  2 * time.Second,
		},
		"at most fetch_attempts times": {
			steps:    []http.HandlerFunc{status(http.StatusServiceUnavailable, nil)},
			attempts: 2,
			want:     obtained{Failure: files.ErrRefused.Error(), Asked: 2},
		},
		"never after a 404": {
			steps:    []http.HandlerFunc{status(http.StatusNotFound, nil)},
			attempts: 3,
			want:     obtained{Failure: files.ErrNotAvailable.Error(), Asked: 1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			parent := newScripted(t, tc.steps...)
			facadeUnderTest, _ := facade(t, shelf{}, &chunkStore{}, parent, tc.attempts)

			began := time.Now()
			storage, source, release, err := facadeUnderTest.Obtain(t.Context(), sdkfiles.URL{ID: sdkfiles.Sum(data)})
			took := time.Since(began)

			if release != nil {
				defer release()
			}

			got := obtained{Storage: storage, Source: source, Failure: knownFailure(err), Asked: len(parent.requests())}
			if !reflect.DeepEqual(got, tc.want) || took < tc.atLeast {
				t.Errorf("Obtain gave %+v after %s, want %+v after at least %s", got, took, tc.want, tc.atLeast)
			}
		})
	}
}

// After a copy that does not hash to its identity, every later request of the
// same fetch carries no-cache, also on the next attempt
// (fractal-files-http.md, Caching).
func TestObtainAsksFreshAfterAMismatch(t *testing.T) {
	t.Parallel()

	data := []byte("GCDE cube")
	wrong := []byte("GCDE cubf")
	parent := newScripted(t, whole(wrong, keptFor()), whole(wrong, keptFor()), whole(data, keptFor()))
	facadeUnderTest, _ := facade(t, shelf{}, &chunkStore{}, parent, 2)

	_, _, release, err := facadeUnderTest.Obtain(t.Context(), sdkfiles.URL{ID: sdkfiles.Sum(data)})
	if release != nil {
		defer release()
	}

	var asked []string
	for _, r := range parent.requests() {
		asked = append(asked, r.CacheControl)
	}

	if want := []string{"", "no-cache", "no-cache"}; err != nil || !slices.Equal(asked, want) {
		t.Errorf("Obtain failed with %v and sent Cache-Control %q, want %q", err, asked, want)
	}
}

func TestObtainStopsWhenTheCopiesAreDropped(t *testing.T) {
	t.Parallel()

	for name, stuck := range map[string]bool{"a clean drop": false, "a drop that fails": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data := []byte("G28 ; revoked")
			gate := make(chan struct{})
			parent := newScripted(t, func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-gate:
				case <-r.Context().Done():
				}

				whole(data, keptFor())(w, r)
			})

			dir := t.TempDir()
			cache := openCache(t, dir, 1<<20)
			facadeUnderTest := facadeOver(t, cache, shelf{}, &chunkStore{}, parent, 3)

			if stuck {
				// A folder with a file in it under a copy's name cannot be removed.
				older := []byte("older")
				put(t, cache, string(older), files.Retention{Unlimited: true})

				blocker := filepath.Join(dir, copyName(older))
				if err := os.Remove(blocker); err != nil {
					t.Fatalf("remove the copy: %v", err)
				}

				if err := os.MkdirAll(filepath.Join(blocker, "inside"), 0o750); err != nil {
					t.Fatalf("block the copy: %v", err)
				}
			}

			failure := make(chan error, 1)

			go func() {
				_, _, release, err := facadeUnderTest.Obtain(t.Context(), sdkfiles.URL{ID: sdkfiles.Sum(data)})
				if release != nil {
					release()
				}

				failure <- err
			}()

			awaitTrue(t, "the request", func() bool { return len(parent.requests()) > 0 })

			dropErr := cache.Drop()
			close(gate)

			type result struct {
				DropFailed bool
				Failure    string
				Path       string
			}

			got := result{
				DropFailed: dropErr != nil,
				Failure:    knownFailure(<-failure),
				Path:       cache.Path(sdkfiles.Sum(data)),
			}
			if want := (result{DropFailed: stuck, Failure: files.ErrDropped.Error()}); !reflect.DeepEqual(got, want) {
				t.Errorf("a fetch across Drop gave %+v, want %+v", got, want)
			}
		})
	}
}

// awaitTrue waits until cond holds.
func awaitTrue(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("waited in vain for %s", what)
}
