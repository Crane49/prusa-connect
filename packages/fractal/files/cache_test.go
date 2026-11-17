package files_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/files"

	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// fill writes data and returns kept, as a fetch would.
func fill(data string, kept files.Retention) func(files.Sink) (files.Retention, error) {
	return func(sink files.Sink) (files.Retention, error) {
		_, err := io.WriteString(sink, data)

		return kept, err
	}
}

// nameOf is the file name of a copy: the algorithm and the hex digest.
func nameOf(data string) string {
	sum := sha256.Sum256([]byte(data))

	return "sha256-" + hex.EncodeToString(sum[:])
}

// look says, for each label, what the cache holds: the bytes of a copy and
// its path, or "absent".
func look(t *testing.T, cache *files.Cache, ids map[string]string) map[string]string {
	t.Helper()

	out := map[string]string{}

	for label, data := range ids {
		id := sdkfiles.Sum([]byte(data))

		if opened, _, unpin, ok := cache.Open(id); ok {
			held, err := io.ReadAll(opened)
			_ = opened.Close()

			unpin()

			if err != nil {
				t.Fatalf("read a copy: %v", err)
			}

			out[label] = "kept " + string(held) + " at " + cache.Path(id)

			continue
		}

		out[label] = "absent"
	}

	return out
}

func put(t *testing.T, cache *files.Cache, data string, kept files.Retention) {
	t.Helper()

	if err := cache.Put(sdkfiles.Sum([]byte(data)), fill(data, kept)); err != nil {
		t.Fatalf("Put %q: %v", data, err)
	}
}

func openCache(t *testing.T, dir string, maxBytes int64) *files.Cache {
	t.Helper()

	cache, err := files.OpenCache(dir, maxBytes, nil)
	if err != nil {
		t.Fatalf("OpenCache: %v", err)
	}

	return cache
}

func TestCacheKeepsOnlyUnlimited(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := openCache(t, dir, 1<<20)
	refused := func(data string, kept files.Retention) bool {
		return errors.Is(cache.Put(sdkfiles.Sum([]byte(data)), fill(data, kept)), files.ErrRetention)
	}

	put(t, cache, "aaaa", files.Retention{Unlimited: true})

	refusals := []bool{
		refused("bbbb", files.Retention{Until: time.Now().Add(time.Hour)}),
		refused("cccc", files.Retention{}),
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the folder: %v", err)
	}

	got := map[string]any{
		"refused": refusals,
		"held":    look(t, cache, map[string]string{"unlimited": "aaaa", "until": "bbbb", "none": "cccc"}),
		"files":   len(entries),
	}
	want := map[string]any{
		"refused": []bool{true, true},
		"held": map[string]string{
			"unlimited": "kept aaaa at " + nameOf("aaaa"),
			"until":     "absent",
			"none":      "absent",
		},
		"files": 1,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("the cache gave %v, want %v", got, want)
	}
}

func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	cache := openCache(t, t.TempDir(), 8)
	put(t, cache, "aaaa", files.Retention{Unlimited: true})
	put(t, cache, "bbbb", files.Retention{Unlimited: true})

	if opened, _, unpin, ok := cache.Open(sdkfiles.Sum([]byte("aaaa"))); ok {
		_ = opened.Close()

		unpin()
	}

	put(t, cache, "cccc", files.Retention{Unlimited: true})

	got := look(t, cache, map[string]string{"used": "aaaa", "unused": "bbbb", "new": "cccc"})
	want := map[string]string{
		"used": "kept aaaa at " + nameOf("aaaa"), "unused": "absent", "new": "kept cccc at " + nameOf("cccc"),
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("the cache holds %v, want %v", got, want)
	}
}

func TestCacheKeepsAPinnedCopy(t *testing.T) {
	t.Parallel()

	cache := openCache(t, t.TempDir(), 8)
	put(t, cache, "aaaa", files.Retention{Unlimited: true})
	unpin := cache.Pin(sdkfiles.Sum([]byte("aaaa")))
	put(t, cache, "bbbb", files.Retention{Unlimited: true})
	put(t, cache, "cccc", files.Retention{Unlimited: true})

	got := look(t, cache, map[string]string{"pinned": "aaaa", "older": "bbbb", "new": "cccc"})
	want := map[string]string{
		"pinned": "kept aaaa at " + nameOf("aaaa"), "older": "absent", "new": "kept cccc at " + nameOf("cccc"),
	}

	unpin()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("the cache holds %v, want %v", got, want)
	}
}

func TestOpenCacheChecksEveryCopy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, data := range map[string]string{
		".node-path":               "hq/brno",
		nameOf("good"):             "good",
		nameOf("bad"):              "not what the name says",
		".part-123":                "half",
		"stray":                    "x",
		filepath.Join("once", "x"): "a folder of an earlier build",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o750); err != nil {
			t.Fatalf("create a folder: %v", err)
		}

		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	cache := openCache(t, dir, 1<<20)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the folder: %v", err)
	}

	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	got := map[string]any{
		"names": names,
		"held":  look(t, cache, map[string]string{"good": "good", "bad": "bad"}),
	}
	want := map[string]any{
		"names": slices.Sorted(slices.Values([]string{".node-path", nameOf("good")})),
		"held":  map[string]string{"good": "kept good at " + nameOf("good"), "bad": "absent"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the start the folder is %v, want %v", got, want)
	}
}

func TestCacheDrop(t *testing.T) {
	t.Parallel()

	cache := openCache(t, t.TempDir(), 1<<20)
	put(t, cache, "aaaa", files.Retention{Unlimited: true})
	put(t, cache, "cccc", files.Retention{Unlimited: true})

	if err := cache.Drop(); err != nil {
		t.Fatalf("Drop: %v", err)
	}

	got := look(t, cache, map[string]string{"a": "aaaa", "c": "cccc"})
	if want := map[string]string{"a": "absent", "c": "absent"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after Drop the cache holds %v, want %v", got, want)
	}
}

func TestCacheFollowsTheNodePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := openCache(t, dir, 1<<20)

	steps := []struct {
		path   string
		reopen bool
	}{{"hq/brno/workshop", false}, {"hq/brno/workshop", true}, {"hq/praha/workshop", false}}

	var got []string

	for _, step := range steps {
		if step.reopen {
			cache = openCache(t, dir, 1<<20)
		}

		if look(t, cache, map[string]string{"a": "aaaa"})["a"] == "absent" {
			put(t, cache, "aaaa", files.Retention{Unlimited: true})
		}

		if err := cache.Follow(fractalPath(step.path)); err != nil {
			t.Fatalf("Follow: %v", err)
		}

		got = append(got, look(t, cache, map[string]string{"a": "aaaa"})["a"])
	}

	want := []string{"absent", "kept aaaa at " + nameOf("aaaa"), "absent"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after each path the copy was %v, want %v", got, want)
	}
}

// A drop of the copies writes one line, so an operator sees that a revocation
// or a new Node Path took them. A drop of nothing writes none.
func TestCacheLogsADrop(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	cache, err := files.OpenCache(t.TempDir(), 1<<20, slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}

			return a
		},
	})))
	if err != nil {
		t.Fatalf("OpenCache: %v", err)
	}

	put(t, cache, "aaaa", files.Retention{Unlimited: true})

	if err = cache.Follow(fractalPath("hq/brno")); err != nil {
		t.Fatalf("Follow: %v", err)
	}

	put(t, cache, "aaaa", files.Retention{Unlimited: true})
	put(t, cache, "cccc", files.Retention{Unlimited: true})

	for range 2 {
		if err = cache.Drop(); err != nil {
			t.Fatalf("Drop: %v", err)
		}
	}

	want := `level=INFO msg="the copies of files from above were dropped for a new node path" path=hq/brno copies=1
level=INFO msg="the copies of files from above were dropped" copies=2
`
	if got := out.String(); got != want {
		t.Errorf("the log is\n%s\nwant\n%s", got, want)
	}
}

func TestCacheFailedFillLeavesNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := openCache(t, dir, 1<<20)
	cut := errors.New("cut")

	err := cache.Put(sdkfiles.Sum([]byte("aaaa")), func(sink files.Sink) (files.Retention, error) {
		_, _ = io.WriteString(sink, "aa")

		return files.Retention{}, cut
	})

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read the folder: %v", readErr)
	}

	got := map[string]any{"error": errors.Is(err, cut), "entries": len(entries)}
	if want := map[string]any{"error": true, "entries": 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("after a failed fill %v, want %v", got, want)
	}
}

func TestCacheIgnoresAStaleUnpin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := openCache(t, dir, 1<<20)
	put(t, cache, "cccc", files.Retention{Unlimited: true})
	unpin := cache.Pin(sdkfiles.Sum([]byte("cccc")))

	if err := cache.Drop(); err != nil {
		t.Fatalf("Drop: %v", err)
	}

	put(t, cache, "cccc", files.Retention{Unlimited: true})
	unpin()

	_, statErr := os.Stat(filepath.Join(dir, nameOf("cccc")))

	got := map[string]any{"held": look(t, cache, map[string]string{"c": "cccc"}), "file": statErr == nil}
	want := map[string]any{"held": map[string]string{"c": "kept cccc at " + nameOf("cccc")}, "file": true}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("after a stale unpin the cache shows %v, want %v", got, want)
	}
}

func TestOpenCacheDeletesAnUnreadableCopy(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root reads a file of mode 000")
	}

	dir := t.TempDir()
	name := filepath.Join(dir, nameOf("locked"))

	if err := os.WriteFile(name, []byte("locked"), 0o000); err != nil {
		t.Fatalf("write the copy: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, ".node-path"), []byte("hq/brno"), 0o600); err != nil {
		t.Fatalf("record the node path: %v", err)
	}

	cache := openCache(t, dir, 1<<20)
	_, statErr := os.Stat(name)

	got := map[string]any{
		"held":      look(t, cache, map[string]string{"locked": "locked"}),
		"file gone": errors.Is(statErr, os.ErrNotExist),
	}
	want := map[string]any{"held": map[string]string{"locked": "absent"}, "file gone": true}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the start the cache shows %v, want %v", got, want)
	}
}

// block puts a folder that holds a file under the name of a copy, which
// os.Remove cannot delete, as Windows cannot delete a copy a reader holds open.
func block(t *testing.T, path string) {
	t.Helper()

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove the copy: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(path, "inside"), 0o750); err != nil {
		t.Fatalf("block the copy: %v", err)
	}
}

// unblock puts the copy of "aaaa" in dir back as a file, as it is once
// nothing holds it open.
func unblock(t *testing.T, dir string) {
	t.Helper()

	path := filepath.Join(dir, nameOf("aaaa"))
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("remove the block: %v", err)
	}

	if err := os.WriteFile(path, []byte("aaaa"), 0o600); err != nil {
		t.Fatalf("write the copy back: %v", err)
	}
}

// droppedWithABlock is a cache under a Node Path whose Drop could not delete
// the copy of "aaaa".
func droppedWithABlock(t *testing.T, dir string) (*files.Cache, error) {
	t.Helper()

	cache := openCache(t, dir, 1<<20)
	if err := cache.Follow(fractalPath("hq/brno")); err != nil {
		t.Fatalf("Follow: %v", err)
	}

	put(t, cache, "aaaa", files.Retention{Unlimited: true})
	put(t, cache, "bbbb", files.Retention{Unlimited: true})
	put(t, cache, "cccc", files.Retention{Unlimited: true})
	block(t, filepath.Join(dir, nameOf("aaaa")))

	return cache, cache.Drop()
}

func TestCacheDropForgetsWhatItCannotDelete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache, dropErr := droppedWithABlock(t, dir)
	held := look(t, cache, map[string]string{"a": "aaaa", "b": "bbbb", "c": "cccc"})

	unblock(t, dir)
	retryErr := cache.Drop()
	_, statErr := os.Stat(filepath.Join(dir, nameOf("aaaa")))

	got := map[string]any{
		"drop failed":    dropErr != nil,
		"held":           held,
		"retry failed":   retryErr != nil,
		"left file gone": errors.Is(statErr, os.ErrNotExist),
	}
	want := map[string]any{
		"drop failed":    true,
		"held":           map[string]string{"a": "absent", "b": "absent", "c": "absent"},
		"retry failed":   false,
		"left file gone": true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("a Drop with a copy it cannot delete gave %v, want %v", got, want)
	}
}

func TestOpenCacheAdoptsNothingAfterADrop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if _, err := droppedWithABlock(t, dir); err == nil {
		t.Fatal("Drop deleted a blocked copy")
	}

	unblock(t, dir)

	cache := openCache(t, dir, 1<<20)
	_, statErr := os.Stat(filepath.Join(dir, nameOf("aaaa")))

	got := map[string]any{
		"held":      look(t, cache, map[string]string{"a": "aaaa"}),
		"file gone": errors.Is(statErr, os.ErrNotExist),
	}
	want := map[string]any{"held": map[string]string{"a": "absent"}, "file gone": true}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("after a Drop and a restart the cache shows %v, want %v", got, want)
	}
}

func TestCacheFollowsPastACopyItCannotDelete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := openCache(t, dir, 1<<20)
	if err := cache.Follow(fractalPath("hq/brno")); err != nil {
		t.Fatalf("Follow: %v", err)
	}

	put(t, cache, "aaaa", files.Retention{Unlimited: true})
	block(t, filepath.Join(dir, nameOf("aaaa")))

	followErr := cache.Follow(fractalPath("hq/praha"))
	put(t, cache, "bbbb", files.Retention{Unlimited: true})

	reopened := openCache(t, dir, 1<<20)
	againErr := reopened.Follow(fractalPath("hq/praha"))

	got := map[string]any{
		"follow error": followErr,
		"again error":  againErr,
		"held":         look(t, reopened, map[string]string{"a": "aaaa", "b": "bbbb"}),
	}
	want := map[string]any{
		"follow error": nil,
		"again error":  nil,
		"held":         map[string]string{"a": "absent", "b": "kept bbbb at " + nameOf("bbbb")},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("a Follow past a blocked copy gave %v, want %v", got, want)
	}
}

// TestCacheAdoptsNoCopyOfAFormerPath: a copy a Follow could not delete, which
// the core lets go of only after the process ended, is not taken back under
// the new Node Path at the next start (fractal-files.md, The Downward Cache).
func TestCacheAdoptsNoCopyOfAFormerPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := openCache(t, dir, 1<<20)
	if err := cache.Follow(fractalPath("hq/brno")); err != nil {
		t.Fatalf("Follow: %v", err)
	}

	put(t, cache, "aaaa", files.Retention{Unlimited: true})
	block(t, filepath.Join(dir, nameOf("aaaa")))

	if err := cache.Follow(fractalPath("hq/praha")); err != nil {
		t.Fatalf("Follow to another path: %v", err)
	}

	unblock(t, dir)

	reopened := openCache(t, dir, 1<<20)
	againErr := reopened.Follow(fractalPath("hq/praha"))
	_, statErr := os.Stat(filepath.Join(dir, nameOf("aaaa")))

	got := map[string]any{
		"again error": againErr,
		"held":        look(t, reopened, map[string]string{"a": "aaaa"}),
		"file gone":   errors.Is(statErr, os.ErrNotExist),
	}
	want := map[string]any{"again error": nil, "held": map[string]string{"a": "absent"}, "file gone": true}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("a restart after a Follow past a blocked copy gave %v, want %v", got, want)
	}
}

// TestCacheKeepsANewerCopyAfterADrop: the holds of prints from before a Drop
// end without deleting the copy a later print fetched.
func TestCacheKeepsANewerCopyAfterADrop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := openCache(t, dir, 1<<20)
	id := sdkfiles.Sum([]byte("aaaa"))

	hold := func() func() {
		_, unpin, err := cache.PutHeld(id, fill("aaaa", files.Retention{Unlimited: true}))
		if err != nil {
			t.Fatalf("PutHeld: %v", err)
		}

		return unpin
	}

	first := hold()
	second := hold()

	if err := cache.Drop(); err != nil {
		t.Fatalf("Drop: %v", err)
	}

	third := hold()
	defer third()

	first()
	second()

	_, statErr := os.Stat(filepath.Join(dir, nameOf("aaaa")))

	got := map[string]any{"path": cache.Path(id), "file there": statErr == nil}
	if want := map[string]any{"path": nameOf("aaaa"), "file there": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("after the older holds ended the third print's copy was %v, want %v", got, want)
	}
}

func TestCacheKeepsWhatItCannotEvict(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := openCache(t, dir, 8)
	put(t, cache, "aaaa", files.Retention{Unlimited: true})
	block(t, filepath.Join(dir, nameOf("aaaa")))
	put(t, cache, "bbbb", files.Retention{Unlimited: true})
	put(t, cache, "cccc", files.Retention{Unlimited: true})
	unblock(t, dir)

	got := look(t, cache, map[string]string{"blocked": "aaaa", "older": "bbbb", "new": "cccc"})
	want := map[string]string{
		"blocked": "kept aaaa at " + nameOf("aaaa"), "older": "absent", "new": "kept cccc at " + nameOf("cccc"),
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("the cache holds %v, want %v", got, want)
	}
}

// A Put of a copy the cache holds under the same name, which it cannot
// replace, keeps the copy there: its name is the digest of the same bytes.
func TestCachePutKeepsACopyItCannotReplace(t *testing.T) {
	t.Parallel()

	held := func(t *testing.T) (*files.Cache, string) {
		t.Helper()

		dir := t.TempDir()
		cache := openCache(t, dir, 1<<20)
		put(t, cache, "aaaa", files.Retention{Unlimited: true})
		block(t, filepath.Join(dir, nameOf("aaaa")))

		return cache, dir
	}

	dropped := func(t *testing.T) (*files.Cache, string) {
		t.Helper()

		dir := t.TempDir()
		cache, _ := droppedWithABlock(t, dir)

		return cache, dir
	}

	for name, setup := range map[string]func(*testing.T) (*files.Cache, string){
		"a held copy": held, "a dropped copy": dropped,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cache, dir := setup(t)
			putErr := cache.Put(sdkfiles.Sum([]byte("aaaa")), fill("aaaa", files.Retention{Unlimited: true}))
			unblock(t, dir)

			got := map[string]any{"put error": putErr, "held": look(t, cache, map[string]string{"a": "aaaa"})}
			want := map[string]any{"put error": nil, "held": map[string]string{"a": "kept aaaa at " + nameOf("aaaa")}}

			if !reflect.DeepEqual(got, want) {
				t.Errorf("a Put over a copy it cannot replace gave %v, want %v", got, want)
			}
		})
	}
}
