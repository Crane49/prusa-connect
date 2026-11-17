package app

import (
	"container/list"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/prusa3d/prusa-connect/internal/kernel/lane"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// listingHold is how long a listing that just landed is held whatever the
// bound and the LRU say, so the page told it landed finds it. Without it a
// listing evicted before the page read it would be read from the printer
// again, which on a full USB stick takes 12 to 15 s.
const listingHold = 60 * time.Second

// listingCache holds the last directory listing each printer answered, by
// printer and path, so a repeat visit is answered at once. A printer takes
// seconds to list a full USB stick.
//
// It is one cache for every printer, bounded in bytes by
// core.device_listing_cache_bytes. Over the bound the listing used longest ago
// is dropped, unless it landed less than listingHold ago. A nil cache holds
// nothing, and every listing asks the printer.
type listingCache struct {
	mu       sync.Mutex
	now      func() time.Time
	maxBytes int
	bytes    int
	byKey    map[listingKey]*list.Element
	// recent orders the listings from the one used last to the one used
	// longest ago. Each element is a *heldListing.
	recent list.List
	// failed is the error the last read of each folder ended in, held until
	// the next read of it starts.
	failed map[listingKey]error
	// reading is the read each key is waiting on right now. A second caller
	// asking for the same directory waits for that read instead of asking the
	// printer again, so a page that retries a slow listing joins the work the
	// printer is already doing.
	reading map[listingKey]*listingRead
}

// listingRead is one listing read in flight. done is closed when the answer is
// in.
type listingRead struct {
	done    chan struct{}
	listing httpapi.DeviceListing
	err     error
	began   time.Time
	// background marks a read no person waits for, and is nil for a person's.
	background *lane.Background
	// stale marks a read that began before its folder or printer was
	// forgotten. It describes a tree that is gone and is not kept.
	stale bool
	// changes are the writes this server made to the folder while the read was
	// on the wire. The printer may have listed the folder before or after
	// each, so they are applied to the rows it answers.
	changes []func(httpapi.DeviceListing) httpapi.DeviceListing
}

type listingKey struct {
	device string
	path   string
}

type heldListing struct {
	key     listingKey
	listing httpapi.DeviceListing
	bytes   int
	// until is when the listing stops being held over the bound and the LRU.
	until time.Time
}

func newListingCache(maxBytes int) *listingCache {
	return &listingCache{
		now:      time.Now,
		maxBytes: maxBytes,
		byKey:    map[listingKey]*list.Element{},
		failed:   map[listingKey]error{},
		reading:  map[listingKey]*listingRead{},
	}
}

// join returns the read in flight for path, or a new one. mine is true for a
// new one, and the caller then runs it and hands it to finish; starting it
// forgets the failure held for path. background marks a read no person waits
// for. A person joining one promotes it, so the printer answers it ahead of
// other background reads. A nil cache joins nothing, and every read is the
// caller's own.
func (c *listingCache) join(device, path string, background bool) (read *listingRead, mine bool) {
	read = &listingRead{done: make(chan struct{})}
	if background {
		read.background = lane.NewBackground()
	}

	if c == nil {
		read.began = time.Now()

		return read, true
	}

	key := listingKey{device: device, path: path}

	c.mu.Lock()
	defer c.mu.Unlock()

	if waiting, ok := c.reading[key]; ok {
		if !background {
			waiting.background.Promote()
		}

		return waiting, false
	}

	read.began = c.now()
	c.reading[key] = read
	delete(c.failed, key)

	return read, true
}

// inFlight returns the read of path the printer is working on, if any.
func (c *listingCache) inFlight(device, path string) (*listingRead, bool) {
	if c == nil {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	read, ok := c.reading[listingKey{device: device, path: path}]

	return read, ok
}

// finish retires a read join started and wakes everyone waiting on it. The
// caller files the answer with putSince first, so a caller arriving after the
// read is retired finds the listing held.
func (c *listingCache) finish(device, path string, read *listingRead) {
	if c != nil {
		key := listingKey{device: device, path: path}

		c.mu.Lock()
		if c.reading[key] == read {
			delete(c.reading, key)
		}
		c.mu.Unlock()
	}

	close(read.done)
}

// get returns the last listing the printer answered for path.
func (c *listingCache) get(device, path string) (httpapi.DeviceListing, bool) {
	if c == nil {
		return httpapi.DeviceListing{}, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	found, ok := c.byKey[listingKey{device: device, path: path}]
	if !ok {
		return httpapi.DeviceListing{}, false
	}

	held, _ := found.Value.(*heldListing)
	if held.bytes > c.maxBytes && !held.until.After(c.now()) {
		c.removeLocked(found)

		return httpapi.DeviceListing{}, false
	}

	c.recent.MoveToFront(found)
	c.evictLocked()

	return held.listing, true
}

// put keeps what the printer answered for path in place of what was held. A
// listing larger than the whole bound is not kept.
func (c *listingCache) put(device, path string, listing httpapi.DeviceListing) {
	c.keep(device, path, listing, nil)
}

// keep is put and putSince. read is checked under the same lock forget and
// drop take, so neither can land between the check and the write. A listing
// with read is held until listingHold from now, over the bound.
func (c *listingCache) keep(device, path string, listing httpapi.DeviceListing, read *listingRead) {
	if c == nil {
		return
	}

	key := listingKey{device: device, path: path}
	held := &heldListing{key: key, listing: listing, bytes: listingBytes(key, listing)}

	c.mu.Lock()
	defer c.mu.Unlock()

	if read != nil {
		if c.reading[key] != read || read.stale {
			return
		}

		for _, change := range read.changes {
			listing = change(listing)
		}

		read.listing = listing
		held.listing = listing
		held.bytes = listingBytes(key, listing)
		held.until = c.now().Add(listingHold)
		delete(c.failed, key)
	}

	if found, ok := c.byKey[key]; ok {
		c.removeLocked(found)
	}

	if held.bytes > c.maxBytes && read == nil {
		return
	}

	c.byKey[key] = c.recent.PushFront(held)
	c.bytes += held.bytes

	c.evictLocked()
}

// evictLocked drops the listings used longest ago until the held ones fit the
// bound, passing over those still in their hold. The one used last stays, so
// the listing just put is never the one dropped.
func (c *listingCache) evictLocked() {
	now := c.now()

	for found := c.recent.Back(); found != nil && found != c.recent.Front() && c.bytes > c.maxBytes; {
		older := found.Prev()

		if held, _ := found.Value.(*heldListing); !held.until.After(now) {
			c.removeLocked(found)
		}

		found = older
	}
}

// putSince is put for the rows read answered, with the writes made while it
// was on the wire applied to them. A read across a forget or a drop describes a
// tree that was there before it, a stick pulled out halfway through the read,
// and is not kept.
func (c *listingCache) putSince(device, path string, listing httpapi.DeviceListing, read *listingRead) {
	c.keep(device, path, listing, read)
}

// failSince holds err as the answer for path, under the rule putSince keeps.
func (c *listingCache) failSince(device, path string, err error, read *listingRead) {
	if c == nil || err == nil {
		return
	}

	key := listingKey{device: device, path: path}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.reading[key] == read && !read.stale {
		c.failed[key] = err
	}
}

// failure returns the error the last read of path ended in, and nil when none
// is held.
func (c *listingCache) failure(device, path string) error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.failed[listingKey{device: device, path: path}]
}

// patch applies a change the server made to the listing held for folder, and
// reports whether one was held. A read of folder in flight gets the change too,
// applied to its rows when they land.
func (c *listingCache) patch(
	device, folder string, change func(httpapi.DeviceListing) httpapi.DeviceListing,
) bool {
	if c == nil {
		return false
	}

	key := listingKey{device: device, path: folder}

	c.mu.Lock()
	defer c.mu.Unlock()

	if read, ok := c.reading[key]; ok {
		read.changes = append(read.changes, change)
	}

	found, ok := c.byKey[key]
	if !ok {
		return false
	}

	held, _ := found.Value.(*heldListing)
	held.listing = change(held.listing)
	c.bytes -= held.bytes
	held.bytes = listingBytes(key, held.listing)
	c.bytes += held.bytes

	c.evictLocked()

	return true
}

// resolve is the key the listing of at is held under. at is in the printer's
// spelling, whose segments may be short names, and each is looked up among the
// held rows of the folder above it. It reports false when a folder on the way
// is not held.
func (c *listingCache) resolve(device, at string) (string, bool) {
	segments := strings.Split(at, "/")
	key := segments[0]

	for _, segment := range segments[1:] {
		listing, held := c.get(device, key)
		if !held {
			return "", false
		}

		at := slices.IndexFunc(listing.Items, func(item httpapi.DeviceFile) bool {
			return item.Folder && namesFile(
				connectorapi.File{Path: item.Path, Name: item.Name, DisplayName: item.DisplayName}, segment)
		})
		if at < 0 {
			return "", false
		}

		key = listing.Items[at].Path
	}

	return key, true
}

// invalidate forgets what is held for folder and every folder under it, and
// refuses the reads of them in flight, which began before the change.
func (c *listingCache) invalidate(device, folder string) {
	c.forget(device, func(at string) bool {
		return at == folder || strings.HasPrefix(at, folder+"/")
	})
}

// keepStorages forgets every storage of device not reported, with the folders
// under them, and the root when its rows name other storages. A report that
// only moves the free space, as each write does, keeps the root.
func (c *listingCache) keepStorages(device string, storages []httpapi.DeviceStorage) {
	kept := make(map[string]bool, len(storages))
	for _, storage := range storages {
		kept[storage.Name] = true
	}

	root, held := c.get(device, "")
	sameRoot := held && len(root.Items) == len(kept) &&
		!slices.ContainsFunc(root.Items, func(item httpapi.DeviceFile) bool { return !kept[item.Path] })

	c.forget(device, func(at string) bool {
		storage, _, _ := strings.Cut(at, "/")

		return at == "" && !sameRoot || at != "" && !kept[storage]
	})
}

// forget is invalidate and keepStorages: it forgets the listings and failures
// held for each folder of device that matches, and marks the reads of them in
// flight stale.
func (c *listingCache) forget(device string, matches func(path string) bool) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for key, read := range c.reading {
		if key.device == device && matches(key.path) {
			read.stale = true
		}
	}

	for key := range c.failed {
		if key.device == device && matches(key.path) {
			delete(c.failed, key)
		}
	}

	for key, found := range c.byKey {
		if key.device == device && matches(key.path) {
			c.removeLocked(found)
		}
	}
}

// drop forgets every listing of one printer, for a lost link, a retired
// printer or a storage that went away, after which its tree is not known.
func (c *listingCache) drop(device string) {
	c.forget(device, func(string) bool { return true })
}

func (c *listingCache) removeLocked(found *list.Element) {
	held, _ := c.recent.Remove(found).(*heldListing)
	delete(c.byKey, held.key)
	c.bytes -= held.bytes
}

// listingBytes is what one held listing costs: the key, every row's struct and
// the text in it, and the bookkeeping around them.
func listingBytes(key listingKey, listing httpapi.DeviceListing) int {
	held := int(unsafe.Sizeof(heldListing{})+unsafe.Sizeof(list.Element{})) +
		len(key.device) + len(key.path) + fileBytes(listing.Directory)

	for _, file := range listing.Items {
		held += fileBytes(file)
	}

	return held
}

func fileBytes(file httpapi.DeviceFile) int {
	held := int(unsafe.Sizeof(file)) + len(file.Path) + len(file.Name) + len(file.DisplayName)
	if file.Modified != nil {
		held += int(unsafe.Sizeof(*file.Modified))
	}

	return held
}

// heldListingOf is the answer Files gives without waiting: the rows held for
// folder, marked stale, or an empty folder when none are; the failure held; and
// the read in flight, when there is one.
func heldListingOf(
	folder string, rows httpapi.DeviceListing, held bool, failed error, read *listingRead,
) httpapi.DeviceListing {
	if held {
		rows.Stale = true
	} else {
		// path.Base names the root "."; the printer's own listing of it has no name.
		name := ""
		if folder != "" {
			name = path.Base(folder)
		}

		rows = httpapi.DeviceListing{
			Directory: httpapi.DeviceFile{Path: folder, Name: name, Folder: true},
			Items:     []httpapi.DeviceFile{},
		}
	}

	if failed != nil {
		rows.Fault = listingFault(folder, failed)
	}

	if read != nil {
		rows.Reading = true
		rows.ReadingSince = &read.began
	}

	return rows
}

// withFile is listing with file in place of the entry of the same name, or
// added when there is none.
func withFile(listing httpapi.DeviceListing, file httpapi.DeviceFile) httpapi.DeviceListing {
	items := make([]httpapi.DeviceFile, 0, len(listing.Items)+1)
	replaced := false

	for _, item := range listing.Items {
		if !replaced && sameEntry(item, file) {
			item, replaced = file, true
		}

		items = append(items, item)
	}

	if !replaced {
		items = append(items, file)
	}

	listing.Items = items

	return listing
}

// withoutFile is listing without the entry at path.
func withoutFile(listing httpapi.DeviceListing, at string) httpapi.DeviceListing {
	gone := httpapi.DeviceFile{Path: at}
	items := make([]httpapi.DeviceFile, 0, len(listing.Items))

	for _, item := range listing.Items {
		if !sameEntry(item, gone) {
			items = append(items, item)
		}
	}

	listing.Items = items

	return listing
}

// sameEntry reports whether two entries of one folder are one file, under the
// FAT rule namesFile keeps.
func sameEntry(item, file httpapi.DeviceFile) bool {
	if item.Path == file.Path {
		return true
	}

	entry := connectorapi.File{Path: item.Path, Name: item.Name, DisplayName: item.DisplayName}
	for _, name := range []string{file.DisplayName, file.Name, path.Base(file.Path)} {
		if name != "" && namesFile(entry, name) {
			return true
		}
	}

	return false
}
