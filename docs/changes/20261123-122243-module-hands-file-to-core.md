### Added

- [sdk] A module that prints a file of its own print folder can set `DeleteSource` on `deviceapi.PrintParams`, and the server deletes the file once the print has settled. A send that failed keeps the file for another try.
- [sdk] A module can move a file of its own print folder into a storage with `storageapi.Client.Move`, the `storage.move` call, without sending the bytes back to the server.
