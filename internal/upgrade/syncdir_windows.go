//go:build windows

package upgrade

// syncDirectory does nothing on Windows, as atomicfile's syncDir does:
// FlushFileBuffers refuses a directory handle with "Access is denied".
func syncDirectory(string) error { return nil }
