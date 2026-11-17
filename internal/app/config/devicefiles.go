package config

import "github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"

// The defaults of the caches over the files on printers. buildprofile holds
// the normal and the printer build's values.
const (
	// DefaultDeviceListingCacheBytes is how much printer directory listing
	// the server keeps in memory. The printer build keeps less.
	DefaultDeviceListingCacheBytes = buildprofile.DeviceListingCacheBytes
	// DefaultDeviceThumbnailDiskBytes is how much printer file picture the
	// server keeps on disk. The printer build keeps less.
	DefaultDeviceThumbnailDiskBytes = buildprofile.DeviceThumbnailDiskBytes
	// DefaultDeviceThumbnailPrefetch and DefaultDeviceThumbnailDisk turn the
	// background picture fetch and the disk copy on. The printer build turns
	// both off.
	DefaultDeviceThumbnailPrefetch = buildprofile.DeviceThumbnailPrefetch
	DefaultDeviceThumbnailDisk     = buildprofile.DeviceThumbnailDisk
)
