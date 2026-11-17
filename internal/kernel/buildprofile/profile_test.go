package buildprofile_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
)

// Each profile holds its own defaults. A normal build keeps the values it ran
// on before the printer build existed, and the printer build keeps less.
func TestTheProfileHoldsItsOwnDefaults(t *testing.T) {
	t.Parallel()

	want := map[string]struct{ preview, pageCache, memoryKiB, passes, hashes, listings, thumbnailDisk int }{
		"normal":  {32 << 20, 2000 << 10, 19 * 1024, 2, 4, 4 << 20, 64 << 20},
		"printer": {1 << 20, 512 << 10, 7 * 1024, 5, 1, 256 << 10, 4 << 20},
	}

	values, ok := want[buildprofile.Name]
	if !ok {
		t.Fatalf("Name = %q, which is no profile this test knows", buildprofile.Name)
	}

	if buildprofile.PreviewCacheBytes != values.preview {
		t.Errorf("PreviewCacheBytes = %d, want %d", buildprofile.PreviewCacheBytes, values.preview)
	}

	if buildprofile.SQLitePageCacheBytes != values.pageCache {
		t.Errorf("SQLitePageCacheBytes = %d, want %d", buildprofile.SQLitePageCacheBytes, values.pageCache)
	}

	if buildprofile.PasswordMemoryKiB != values.memoryKiB || buildprofile.PasswordPasses != values.passes {
		t.Errorf("the password cost is %d KiB with %d passes, want %d KiB with %d passes",
			buildprofile.PasswordMemoryKiB, buildprofile.PasswordPasses, values.memoryKiB, values.passes)
	}

	if buildprofile.DeviceListingCacheBytes != values.listings {
		t.Errorf("DeviceListingCacheBytes = %d, want %d", buildprofile.DeviceListingCacheBytes, values.listings)
	}

	if buildprofile.DeviceThumbnailDiskBytes != values.thumbnailDisk {
		t.Errorf("DeviceThumbnailDiskBytes = %d, want %d", buildprofile.DeviceThumbnailDiskBytes, values.thumbnailDisk)
	}

	// The printer build fetches no picture nobody asked for and writes none to disk.
	switchesOn := map[string]bool{"normal": true, "printer": false}[buildprofile.Name]

	if buildprofile.DeviceThumbnailPrefetch != switchesOn {
		t.Errorf("DeviceThumbnailPrefetch = %t, want %t", buildprofile.DeviceThumbnailPrefetch, switchesOn)
	}

	if buildprofile.DeviceThumbnailDisk != switchesOn {
		t.Errorf("DeviceThumbnailDisk = %t, want %t", buildprofile.DeviceThumbnailDisk, switchesOn)
	}

	if buildprofile.PasswordHashesAtOnce != values.hashes {
		t.Errorf("PasswordHashesAtOnce = %d, want %d", buildprofile.PasswordHashesAtOnce, values.hashes)
	}
}
