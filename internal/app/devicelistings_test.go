package app

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// A listing read while the printer's tree was dropped, a stick pulled out
// halfway through the read, describes a tree that is gone. It is not kept, or
// the next visit would be answered from it.
func TestAListingReadAcrossADropIsNotKept(t *testing.T) {
	t.Parallel()

	listing := httpapi.DeviceListing{Items: []httpapi.DeviceFile{{Path: "usb/a.bgcode", Name: "a.bgcode"}}}
	cache := newListingCache(1 << 20)

	read, _ := cache.join("p1", "usb", false)
	cache.drop("p1")
	cache.putSince("p1", "usb", listing, read)
	cache.finish("p1", "usb", read)

	_, ok := cache.get("p1", "usb")
	require.False(t, ok, "a listing read across a drop was kept")

	putLanded(cache, "usb", listing)

	_, ok = cache.get("p1", "usb")
	require.True(t, ok, "a listing read after the drop was not kept")
}

// Over its byte bound the cache drops the listing used longest ago, and
// reading a listing counts as using it.
func TestTheListingCacheDropsTheLeastRecentlyUsedOverItsBound(t *testing.T) {
	t.Parallel()

	listing := httpapi.DeviceListing{Items: []httpapi.DeviceFile{{Path: "usb/a.bgcode", Name: "a.bgcode"}}}
	one := listingBytes(listingKey{device: "p1", path: "usb/1"}, listing)

	cache := newListingCache(3 * one)
	cache.put("p1", "usb/1", listing)
	cache.put("p1", "usb/2", listing)
	cache.put("p2", "usb/3", listing)

	_, ok := cache.get("p1", "usb/1")
	require.True(t, ok)

	cache.put("p2", "usb/4", listing)

	_, ok = cache.get("p1", "usb/2")
	require.False(t, ok, "the listing used longest ago was kept over the bound")

	for _, key := range []listingKey{{"p1", "usb/1"}, {"p2", "usb/3"}, {"p2", "usb/4"}} {
		_, ok = cache.get(key.device, key.path)
		require.True(t, ok, "%v was dropped", key)
	}

	require.LessOrEqual(t, cache.bytes, 3*one)

	cache.drop("p2")

	_, ok = cache.get("p2", "usb/3")
	require.False(t, ok, "a dropped printer's listing was kept")
	require.Equal(t, one, cache.bytes)
}

func listingOf(paths ...string) httpapi.DeviceListing {
	listing := httpapi.DeviceListing{Items: []httpapi.DeviceFile{}}
	for _, at := range paths {
		listing.Items = append(listing.Items, httpapi.DeviceFile{Path: at, Name: at[len("usb/"):]})
	}

	return listing
}

func clockedCache(maxBytes int, at *time.Time) *listingCache {
	cache := newListingCache(maxBytes)
	cache.now = func() time.Time { return *at }

	return cache
}

// A listing larger than the whole bound is still held while its page has not
// read it yet, and only for that window.
func TestALandedListingOutlivesTheBoundForItsWindow(t *testing.T) {
	t.Parallel()

	at := time.Unix(1_000, 0)
	cache := clockedCache(1, &at)

	putLanded(cache, "usb", listingOf("usb/x"))

	start := at
	at = start.Add(59 * time.Second)

	_, ok := cache.get("p1", "usb")
	require.True(t, ok, "a landed listing was not held for its window")

	at = start.Add(listingHold + time.Nanosecond)

	_, ok = cache.get("p1", "usb")
	require.False(t, ok, "an oversized listing was held past its window")
	require.Zero(t, cache.bytes)
}

// Eviction passes over listings still in their window, and once the window
// has passed the bound applies again.
func TestEvictionSkipsPinnedListingsAndEnds(t *testing.T) {
	t.Parallel()

	listing := listingOf("usb/x")
	one := listingBytes(listingKey{device: "p1", path: "usb/a"}, listing)

	at := time.Unix(1_000, 0)
	cache := clockedCache(2*one, &at)

	for _, folder := range []string{"usb/a", "usb/b", "usb/c"} {
		putLanded(cache, folder, listing)
	}

	for _, folder := range []string{"usb/a", "usb/b", "usb/c"} {
		_, ok := cache.get("p1", folder)
		require.True(t, ok, "%s was evicted inside its window", folder)
	}

	require.Greater(t, cache.bytes, cache.maxBytes)

	at = at.Add(61 * time.Second)
	putLanded(cache, "usb/d", listing)

	require.LessOrEqual(t, cache.bytes, cache.maxBytes)

	_, ok := cache.get("p1", "usb/d")
	require.True(t, ok)
}

// A read on the wire while the server patched the folder may describe it
// without the change, so the change is applied to the rows it lands.
func TestAReadAcrossAPatchKeepsTheChange(t *testing.T) {
	t.Parallel()

	cache := newListingCache(1 << 20)
	putLanded(cache, "usb", listingOf("usb/x"))

	read, _ := cache.join("p1", "usb", true)

	patched := cache.patch("p1", "usb", func(listing httpapi.DeviceListing) httpapi.DeviceListing {
		return withFile(listing, httpapi.DeviceFile{Path: "usb/y", Name: "y"})
	})
	require.True(t, patched)

	cache.putSince("p1", "usb", listingOf("usb/x"), read)
	cache.finish("p1", "usb", read)

	held, ok := cache.get("p1", "usb")
	require.True(t, ok)
	require.Equal(t, listingOf("usb/x", "usb/y").Items, held.Items)
	require.Equal(t, listingBytes(listingKey{device: "p1", path: "usb"}, held), cache.bytes)
}

func TestInvalidatingAFolderForgetsItAndEverythingUnderIt(t *testing.T) {
	t.Parallel()

	cache := newListingCache(1 << 20)
	for _, folder := range []string{"usb", "usb/a", "usb/a/b", "usb/ab", "local"} {
		cache.put("p1", folder, listingOf("usb/x"))
	}

	reading, _ := cache.join("p1", "usb/a/c", true)

	cache.invalidate("p1", "usb/a")

	for _, folder := range []string{"usb", "usb/ab", "local"} {
		_, ok := cache.get("p1", folder)
		require.True(t, ok, "%s was forgotten", folder)
	}

	for _, folder := range []string{"usb/a", "usb/a/b"} {
		_, ok := cache.get("p1", folder)
		require.False(t, ok, "%s was kept", folder)
	}

	cache.putSince("p1", "usb/a/c", listingOf("usb/x"), reading)
	cache.finish("p1", "usb/a/c", reading)

	_, ok := cache.get("p1", "usb/a/c")
	require.False(t, ok, "a read in flight under the invalidated folder was kept")
}

func TestAFailedReadIsHeldUntilTheNextReadStarts(t *testing.T) {
	t.Parallel()

	refused := errors.New("refused")
	cache := newListingCache(1 << 20)

	read, _ := cache.join("p1", "usb", false)
	cache.failSince("p1", "usb", refused, read)
	cache.finish("p1", "usb", read)
	require.ErrorIs(t, cache.failure("p1", "usb"), refused)

	_, mine := cache.join("p1", "usb", false)
	require.True(t, mine)
	require.NoError(t, cache.failure("p1", "usb"), "the failure outlived the next read starting")
}

func TestAPersonJoiningABackgroundReadPromotesIt(t *testing.T) {
	t.Parallel()

	cache := newListingCache(1 << 20)

	read, mine := cache.join("p1", "usb", true)
	require.True(t, mine)
	require.NotNil(t, read.background)

	joined, mine := cache.join("p1", "usb", false)
	require.False(t, mine)
	require.Same(t, read, joined)

	select {
	case <-read.background.Promoted():
	default:
		require.Fail(t, "a person joining a background read did not promote it")
	}

	flying, ok := cache.inFlight("p1", "usb")
	require.True(t, ok)
	require.Same(t, read, flying)
}

func TestPatchingAFolderNotHeldChangesNothing(t *testing.T) {
	t.Parallel()

	cache := newListingCache(1 << 20)

	patched := cache.patch("p1", "usb", func(listing httpapi.DeviceListing) httpapi.DeviceListing {
		return withFile(listing, httpapi.DeviceFile{Path: "usb/y", Name: "y"})
	})
	require.False(t, patched)

	_, ok := cache.get("p1", "usb")
	require.False(t, ok)
}

// The storage is FAT, where a long and a short name are one entry.
func TestWithFileReplacesTheEntryOfTheSameName(t *testing.T) {
	t.Parallel()

	listing := httpapi.DeviceListing{Items: []httpapi.DeviceFile{
		{Path: "usb/A~1.GCO", Name: "A~1.GCO", DisplayName: "a.gcode", Size: 1},
	}}

	sent := withFile(listing, httpapi.DeviceFile{Path: "usb/a.gcode", Size: 2})
	require.Len(t, sent.Items, 1)
	require.EqualValues(t, 2, sent.Items[0].Size)
	require.EqualValues(t, 1, listing.Items[0].Size, "withFile changed the listing it was given")

	require.Empty(t, withoutFile(sent, "usb/A.GCODE").Items)
}

func TestKeepingStoragesForgetsTheOthersAndTheRoot(t *testing.T) {
	t.Parallel()

	cache := newListingCache(1 << 20)
	for _, folder := range []string{"", "usb", "usb/x", "local"} {
		cache.put("p1", folder, listingOf("usb/x"))
	}

	cache.put("p2", "usb", listingOf("usb/x"))

	cache.keepStorages("p1", []httpapi.DeviceStorage{{Name: "local"}})

	for _, folder := range []string{"", "usb", "usb/x"} {
		_, ok := cache.get("p1", folder)
		require.False(t, ok, "%q was kept", folder)
	}

	_, ok := cache.get("p1", "local")
	require.True(t, ok)

	_, ok = cache.get("p2", "usb")
	require.True(t, ok, "another printer's listing was forgotten")
}

// A patch that grows a held listing is held to the same byte bound as a put.
func TestAPatchKeepsTheListingCacheInsideItsBound(t *testing.T) {
	t.Parallel()

	one := listingBytes(listingKey{device: "p1", path: "usb/1"}, listingOf("usb/a.bgcode"))

	cache := newListingCache(2 * one)
	cache.put("p1", "usb/1", listingOf("usb/a.bgcode"))
	cache.put("p1", "usb/2", listingOf("usb/a.bgcode"))

	cache.patch("p1", "usb/2", func(listing httpapi.DeviceListing) httpapi.DeviceListing {
		listing.Items = append(listing.Items, httpapi.DeviceFile{Path: "usb/b.bgcode", Name: "b.bgcode"})

		return listing
	})

	require.LessOrEqual(t, cache.bytes, 2*one)
}

// putLanded files listing as a read of path on printer p1 that ran with nothing else
// happening to the folder.
func putLanded(cache *listingCache, path string, listing httpapi.DeviceListing) {
	read, _ := cache.join("p1", path, false)
	cache.putSince("p1", path, listing, read)
	cache.finish("p1", path, read)
}

// A send lands in a folder nothing is held for while the first read of it is on
// the wire. The read is kept, with the file in it, so the next visit does not
// read the folder again.
func TestAPatchDuringAColdReadKeepsTheRead(t *testing.T) {
	t.Parallel()

	cache := newListingCache(1 << 20)

	read, mine := cache.join("p1", "usb", false)
	require.True(t, mine)

	cache.patch("p1", "usb", func(listing httpapi.DeviceListing) httpapi.DeviceListing {
		return withFile(listing, httpapi.DeviceFile{Path: "usb/y", Name: "y"})
	})

	cache.putSince("p1", "usb", listingOf("usb/x"), read)
	cache.finish("p1", "usb", read)

	held, ok := cache.get("p1", "usb")
	require.True(t, ok, "a read that landed after the write was thrown away")
	require.Equal(t, []string{"usb/x", "usb/y"}, itemPaths(held))
}
