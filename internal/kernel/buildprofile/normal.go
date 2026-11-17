//go:build !printer

package buildprofile

// Name is the profile this binary was built with.
const Name = "normal"

// The defaults of a normal build.
const (
	// PreviewCacheBytes is how much preview picture the server keeps in
	// memory, across every print file it has read.
	PreviewCacheBytes = 32 << 20

	// DeviceThumbnailDiskBytes is how much device file picture the server
	// keeps on disk, so a restart does not ask every device again.
	DeviceThumbnailDiskBytes = 64 << 20

	// DeviceListingCacheBytes is how much device directory listing the
	// server keeps in memory, across every device.
	DeviceListingCacheBytes = 4 << 20

	// DeviceThumbnailPrefetch is whether the server fetches the pictures of
	// a listed folder's print files before anybody asks, and
	// DeviceThumbnailDisk is whether it keeps those pictures on disk.
	DeviceThumbnailPrefetch = true
	DeviceThumbnailDisk     = true

	// SQLitePageCacheBytes bounds the page cache of one SQLite connection. It
	// is the size SQLite uses when nobody sets one.
	SQLitePageCacheBytes = 2000 << 10

	// PasswordMemoryKiB and PasswordPasses are the argon2id cost of one
	// password hash. 19 MiB with 2 passes is the first setting OWASP lists.
	PasswordMemoryKiB = 19 * 1024
	PasswordPasses    = 2

	// PasswordHashesAtOnce is how many password hashes may run at the same
	// time. Each one holds PasswordMemoryKiB while it runs.
	PasswordHashesAtOnce = 4
)
