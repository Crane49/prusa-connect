package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/adapters/storage/diskstore"
	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// ErrFilesystemConfig reports settings this type will not run on.
var ErrFilesystemConfig = errors.New("filesystem: invalid settings")

// filesystemConfig is the type-private settings block. The core stores and
// returns it unchanged; this is the only code that reads inside it.
type filesystemConfig struct {
	// Root is the directory the instance owns. It is created when it does not
	// exist.
	Root string `json:"root"`
	// AllowedExtensions bounds what an upload may be called. An absent key takes
	// the built-in list, and an explicit empty list permits any extension.
	AllowedExtensions *[]string `json:"allowed_extensions,omitempty"`
}

// newFilesystem constructs one instance. A root that cannot be used is an
// error: the deployment does not have that resource.
//
// The instance is a storageapi.Provider built from adapters/storage/diskstore.
// That package registers no name, so importing it claims none.
func newFilesystem(ctx context.Context, handle host.Handle, spec registry.Spec) (storageapi.Provider, error) {
	settings, err := decodeFilesystem(spec.Config)
	if err != nil {
		return nil, err
	}

	if settings.Root == "" {
		return nil, fmt.Errorf("%w: root is empty", ErrFilesystemConfig)
	}

	provider, err := diskstore.Open(storageapi.ProviderSpec{
		ID:                spec.Ref.ID(),
		Type:              TypeFilesystem,
		Path:              settings.Root,
		AllowedExtensions: allowedExtensions(settings),
	}, spec.Ref.ID())
	if err != nil {
		return nil, fmt.Errorf("%w: root %q: %w", ErrFilesystemConfig, settings.Root, err)
	}

	handle.Logger.InfoContext(ctx, "storage root ready", "root", provider.Root())

	return provider, nil
}

// allowedExtensions reads the allowlist. A nil pointer is a key nobody wrote
// and means the built-in list. An empty list means no restriction.
func allowedExtensions(settings filesystemConfig) []string {
	if settings.AllowedExtensions == nil {
		return storageapi.DefaultAllowedExtensions
	}

	return *settings.AllowedExtensions
}

// decodeFilesystem rejects a key this type does not know. The core cannot judge
// a type-private block, so this type refuses a typo itself.
func decodeFilesystem(raw json.RawMessage) (filesystemConfig, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return filesystemConfig{}, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var settings filesystemConfig
	if err := decoder.Decode(&settings); err != nil {
		return filesystemConfig{}, fmt.Errorf("%w: %w", ErrFilesystemConfig, err)
	}

	return settings, nil
}
