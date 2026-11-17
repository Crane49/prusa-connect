package fault

// The core.storage codes. Each one names a condition sdk/storageapi reports
// as a sentinel error.
var (
	// StorageInvalidPath rejects a path a provider will not accept: empty,
	// absolute, or escaping the storage root.
	StorageInvalidPath = Register(DomainStorage(), "INVALID_PATH", statusBadRequest, Document(
		"The request names a path this storage will not accept",
		"Send a path relative to the storage root, with no .. segment and no drive or leading separator.",
		RetryNever,
		ShowDetail,
		"storage_id", "path",
	))

	// StorageNotFound reports a storage this deployment does not declare, or
	// one that is declared and not running.
	StorageNotFound = Register(DomainStorage(), "NOT_FOUND", statusNotFound, Document(
		"No storage of that name is serving",
		"Read the names from the resource list. A storage that is declared but not ready is not usable yet, "+
			"and its status says why.",
		RetryNever,
		ShowDetail,
		"storage_id",
	))

	// StorageFileNotFound reports a name the storage has no file under.
	StorageFileNotFound = Register(DomainStorage(), "FILE_NOT_FOUND", statusNotFound, Document(
		"The storage holds no file of that name",
		"List the storage and send a name it reports. The name is relative to the storage root.",
		RetryNever,
		ShowDetail,
		"storage_id", "path",
	))

	// StorageNoThumbnail reports a file with no picture stored in it by a
	// slicer. The file exists, so asking again does not help.
	StorageNoThumbnail = Register(DomainStorage(), "NO_THUMBNAIL", statusNotFound, Document(
		"That file carries no picture",
		"Show the file without one. A picture comes from the slicer: a .gcode, .bgcode or .3mf file "+
			"exported with thumbnails enabled has one, and an .stl file never has one.",
		RetryNever,
		ShowDetail,
		"storage_id", "path",
	))

	// StorageNotReadable reports a storage whose type cannot return a file,
	// whatever the name.
	StorageNotReadable = Register(DomainStorage(), "NOT_READABLE", statusConflict, Document(
		"That storage cannot open a file for reading",
		"Use a storage whose type serves files. The types this build carries are listed at startup and "+
			"in the extension reference.",
		RetryNever,
		ShowDetail,
		"storage_id",
	))

	// StorageExtensionNotAllowed rejects an upload whose extension is outside
	// the allow-list the storage was configured with.
	StorageExtensionNotAllowed = Register(DomainStorage(), "EXTENSION_NOT_ALLOWED", statusUnsupportedType, Document(
		"The file extension is not one this storage accepts",
		"Upload a file with an allowed extension. The allow-list is per storage and is part of the server configuration.",
		RetryNever,
		ShowDetail,
		"storage_id",
		"path",
		"extension",
	))

	// StorageNoSpace reports a write that ran out of room. On FAT and USB it
	// can arrive on close, after the last byte was accepted.
	StorageNoSpace = Register(DomainStorage(), "NO_SPACE", statusInsufficientSpace, Document(
		"The storage ran out of space while writing the file",
		"Free space on this storage or upload to another one, then send the file again. Treat the target path as unusable until it is deleted.",
		RetryNever,
		ShowDetail,
		"storage_id",
		"path",
	))

	// StorageUnavailable reports a storage operation that failed for a reason
	// the server does not recognise.
	StorageUnavailable = Register(DomainStorage(), "UNAVAILABLE", statusInternal, Document(
		"The storage could not carry that out",
		"Read the server log for the reason. The request itself may be fine: this is the code for a "+
			"failure the server could not attribute to anything the caller sent.",
		RetryAllowed,
		ShowDetail,
		"storage_id", "path",
	))

	// StorageDeleteIncomplete reports a multiple-entry delete where some
	// entries survived. The operation is not atomic and does not roll back.
	// The path is the one the caller asked to remove. There is no count.
	StorageDeleteIncomplete = Register(DomainStorage(), "DELETE_INCOMPLETE", statusInternal, Document(
		"Some of the entries in the delete request are still there",
		"List the directory and repeat the delete for what remains. The entries that were removed stay removed.",
		RetryAllowed,
		ShowDetail,
		"storage_id", "path",
	))
)
