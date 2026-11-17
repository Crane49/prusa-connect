//go:build printer

package buildprofile

// Name is the profile this binary was built with.
const Name = "printer"

// The defaults of the printer build.
const (
	// PreviewCacheBytes is how much preview picture the server keeps in
	// memory, across every print file it has read. A folder of pictures does
	// not fit on the printer, so the cache keeps only the most recent ones.
	PreviewCacheBytes = 1 << 20

	// DeviceThumbnailDiskBytes is how much device file picture the server
	// keeps on disk, so a restart does not ask every device again.
	DeviceThumbnailDiskBytes = 4 << 20

	// DeviceListingCacheBytes is how much device directory listing the
	// server keeps in memory, across every device.
	DeviceListingCacheBytes = 256 << 10

	// DeviceThumbnailPrefetch is whether the server fetches the pictures of
	// a listed folder's print files before anybody asks, and
	// DeviceThumbnailDisk is whether it keeps those pictures on disk.
	DeviceThumbnailPrefetch = false
	DeviceThumbnailDisk     = false

	// SQLitePageCacheBytes bounds the page cache of one SQLite connection.
	// Four connections then hold at most 2 MiB.
	SQLitePageCacheBytes = 512 << 10

	// PasswordMemoryKiB and PasswordPasses are the argon2id cost of one
	// password hash. OWASP lists 7 MiB with 5 passes as equal in strength to
	// 19 MiB with 2 passes, and the smaller block fits the printer's memory.
	PasswordMemoryKiB = 7 * 1024
	PasswordPasses    = 5

	// PasswordHashesAtOnce is how many password hashes may run at the same
	// time. The printer runs one, and a second login waits for it.
	PasswordHashesAtOnce = 1
)
