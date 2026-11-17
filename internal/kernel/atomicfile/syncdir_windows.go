//go:build windows

package atomicfile

// syncDir does nothing on Windows, so a rename there is not durable.
// FlushFileBuffers refuses a directory handle, and os.Rename calls MoveFileEx
// with MOVEFILE_REPLACE_EXISTING and without MOVEFILE_WRITE_THROUGH, so the
// rename is atomic but returns before it is on disk. Windows is a development
// platform for this project and not a deployment target.
func syncDir(string) error { return nil }
