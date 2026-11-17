package capability

import (
	"context"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Storages returns the storage devices the printer reported.
//
// It takes the same route a command does: the resolved set decides whether the
// printer may be asked at all, and the driver is reached only if it does.
// A dialect with no file vocabulary and a printer whose resolved set does not
// carry the verb get the same refusal, because both mean this printer cannot
// answer.
func (s *Session) Storages(ctx context.Context) ([]connectorapi.Storage, error) {
	files, err := s.filesystem(connectorapi.VerbSendInfo)
	if err != nil {
		return nil, err
	}

	storages, err := files.Storages(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the printer's storages: %w", err)
	}

	return storages, nil
}

// StorageReport is what a driver whose printer reports its storages only when
// asked adds to Filesystem: a way to ask again, whether the connection held now
// has answered, and a wake-up when an answer moves either. Without the second,
// an empty Storages cannot tell a printer with no drive from one that has not
// said yet.
type StorageReport interface {
	// AskStorages asks the printer and returns once its answer is held.
	AskStorages(ctx context.Context) error
	// StoragesReported reports whether the connection held now has answered.
	StoragesReported() bool
	// StoragesChanged wakes a reader when an answer changes the storages or is
	// the first on a connection. It carries no value and never closes.
	StoragesChanged() <-chan struct{}
}

// FileWatch is what a driver whose printer reports changes of its own files
// adds to Filesystem: an upload through another client, a delete or a new
// folder. A change made on the printer's own screen may go unreported.
type FileWatch interface {
	// FilesChanged carries each change the printer reported. It never closes.
	FilesChanged() <-chan connectorapi.FileChange
}

// FilesChanged carries the file changes the printer reports. It is nil for a
// driver that does not implement FileWatch, whose reader blocks on it forever.
func (s *Session) FilesChanged() <-chan connectorapi.FileChange {
	if watch, ok := As[FileWatch](s.inner); ok {
		return watch.FilesChanged()
	}

	return nil
}

// AskStorages asks the printer what storages it has now. A driver that does not
// implement StorageReport holds nothing to refresh and is not asked.
func (s *Session) AskStorages(ctx context.Context) error {
	if _, err := s.filesystem(connectorapi.VerbSendInfo); err != nil {
		return err
	}

	report, asks := As[StorageReport](s.inner)
	if !asks {
		return nil
	}

	if err := report.AskStorages(ctx); err != nil {
		return fmt.Errorf("ask the printer for its storages: %w", err)
	}

	return nil
}

// StoragesReported reports whether the printer has answered, so that Storages
// is its answer. A driver that does not implement StorageReport holds only a
// list it was given, so its list is its answer.
func (s *Session) StoragesReported() bool {
	if report, asks := As[StorageReport](s.inner); asks {
		return report.StoragesReported()
	}

	return true
}

// StoragesChanged wakes a reader when the storages change. It is nil for a
// driver that does not implement StorageReport, whose reader blocks on it
// forever.
func (s *Session) StoragesChanged() <-chan struct{} {
	if report, asks := As[StorageReport](s.inner); asks {
		return report.StoragesChanged()
	}

	return nil
}

// List returns one directory of the printer's own file tree. path is
// storage-prefixed and relative, and the empty path is the root whose entries
// are the storages.
func (s *Session) List(ctx context.Context, path string) (connectorapi.Listing, error) {
	// Every method here that takes a path refuses a climbing path before the
	// driver is reached. The contract puts the same rule on a driver, but a
	// driver is third-party code and may forget the check. The path would then
	// reach the device.
	if refusal := connectorapi.RefusePathEscape(path); refusal != nil {
		return connectorapi.Listing{}, refusal
	}

	files, err := s.filesystem(connectorapi.VerbSendFileInfo)
	if err != nil {
		return connectorapi.Listing{}, err
	}

	listing, err := files.List(ctx, path)
	if err != nil {
		return connectorapi.Listing{}, fmt.Errorf("list %q on the printer: %w", path, err)
	}

	return listing, nil
}

// CreateFolder makes one folder on the printer's storage.
func (s *Session) CreateFolder(ctx context.Context, path string) error {
	if refusal := connectorapi.RefusePathEscape(path); refusal != nil {
		return refusal
	}

	manager, err := s.manager(connectorapi.VerbCreateFolder)
	if err != nil {
		return err
	}

	if createErr := manager.CreateFolder(ctx, path); createErr != nil {
		return fmt.Errorf("create %q on the printer: %w", path, createErr)
	}

	return nil
}

// DeleteFile removes one file from the printer's storage.
func (s *Session) DeleteFile(ctx context.Context, path string) error {
	if refusal := connectorapi.RefusePathEscape(path); refusal != nil {
		return refusal
	}

	manager, err := s.manager(connectorapi.VerbDeleteFile)
	if err != nil {
		return err
	}

	if deleteErr := manager.DeleteFile(ctx, path); deleteErr != nil {
		return fmt.Errorf("delete the file %q on the printer: %w", path, deleteErr)
	}

	return nil
}

// DeleteFolder removes one folder from the printer's storage.
func (s *Session) DeleteFolder(ctx context.Context, path string) error {
	if refusal := connectorapi.RefusePathEscape(path); refusal != nil {
		return refusal
	}

	manager, err := s.manager(connectorapi.VerbDeleteFolder)
	if err != nil {
		return err
	}

	if deleteErr := manager.DeleteFolder(ctx, path); deleteErr != nil {
		return fmt.Errorf("delete the folder %q on the printer: %w", path, deleteErr)
	}

	return nil
}

// manager is the driver's file-management half, once the resolved set says the
// verb it is spoken through is one this printer has.
//
// Each operation is gated on its own verb and not on the interface. A printer
// can make a folder and not remove one, and one check for the interface could
// not say that.
func (s *Session) manager(verb connectorapi.Capability) (connectorapi.FileManager, error) {
	if !s.current.Load().resolution.Capabilities.Has(verb) {
		return nil, connectorapi.CapabilityNotSupported.
			New("the printer does not have that capability").
			With(fault.StringParam("verb", verb.String()))
	}

	manager, ok := As[connectorapi.FileManager](s.inner)
	if !ok {
		return nil, connectorapi.CapabilityNotSupported.
			New("the printer does not have that capability").
			With(fault.StringParam("verb", verb.String())).
			Wrap(connectorapi.ErrNoFileManager)
	}

	return manager, nil
}

// filesystem is the driver's file half, once the resolved set says the verb it
// is spoken through is one this printer has.
func (s *Session) filesystem(verb connectorapi.Capability) (connectorapi.Filesystem, error) {
	if !s.current.Load().resolution.Capabilities.Has(verb) {
		return nil, connectorapi.CapabilityNotSupported.
			New("the printer does not have that capability").
			With(fault.StringParam("verb", verb.String()))
	}

	files, ok := As[connectorapi.Filesystem](s.inner)
	if !ok {
		return nil, connectorapi.CapabilityNotSupported.
			New("the printer does not have that capability").
			With(fault.StringParam("verb", verb.String())).
			Wrap(connectorapi.ErrNoFilesystem)
	}

	return files, nil
}
