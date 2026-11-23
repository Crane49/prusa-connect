package storageapi

// Moving a file of the module's own print folder into a storage. The host
// reads the file from the module's data directory, so no byte of it travels on
// the wire.

import (
	"context"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MethodMove puts one file of the calling module's print folder into a
// storage, replaces any file of that name there, and deletes the file from the
// print folder. The print folder is print/ in the module's data directory, the
// folder a print from module:data reads.
const MethodMove wirerpc.Method = "storage.move"

// MoveParams is the parameters of MethodMove.
type MoveParams struct {
	// Principal is who the call is made for, and empty for the module's own
	// work.
	Principal string `json:"principal,omitempty"`

	// Source is the file to move, relative to the print folder.
	Source string `json:"source"`

	Storage string `json:"storage"`
	// Path is where the file lands, relative to the storage's root. Missing
	// parent folders are created.
	Path string `json:"path"`
}

// Validate checks one move against the limits of this contract.
func (p MoveParams) Validate() error {
	if err := validTarget(p.Principal, "", p.Storage, p.Path); err != nil {
		return err
	}

	switch {
	case p.Source == "":
		return fmt.Errorf("%w: the call names no file to move", ErrNoFile)
	case len(p.Source) > MaxPathBytes:
		return fmt.Errorf("%w: the source is %d bytes and the limit is %d",
			ErrInvalidPath, len(p.Source), MaxPathBytes)
	}

	return nil
}

// Move puts a file of the module's print folder into a storage and returns
// when the storage has published it. It needs the write_storage permission,
// as Write does, and a data directory. A source that is not in the print
// folder is ErrNoFile. A failed move leaves the source where it was.
func (c *Client) Move(ctx context.Context, params MoveParams) (Written, error) {
	if err := params.Validate(); err != nil {
		return Written{}, fmt.Errorf("storageapi: %s: %w", MethodMove, err)
	}

	var written Written
	if err := c.call(ctx, MethodMove, params, &written); err != nil {
		return Written{}, err
	}

	return written, nil
}
