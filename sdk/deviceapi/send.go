package deviceapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// SendParams is the parameters of MethodSend. It names a file the way a print
// from a storage does, and the fields mean what they mean on PrintParams:
// Storage and Source are the file, Path is where it lands on the printer,
// KeepBoth sends it under a free name beside a file already there, and
// DeleteSource has the host delete a file of ModuleDataStorage once it landed.
type SendParams PrintParams

// Validate checks one send against the limits of this contract. The client and
// the host both call it. A send moves a file, so it names a storage.
func (p SendParams) Validate() error {
	if err := PrintParams(p).Validate(); err != nil {
		return err
	}

	if p.Storage == "" {
		return fmt.Errorf("%w: a send names the storage the file comes from", ErrInvalidQuery)
	}

	return nil
}

// Sent is the answer of MethodSend. TransferID is the transfer the host opened,
// and empty when the printer already held this file. Path is where the file is
// on the printer, a numbered name with KeepBoth.
type Sent struct {
	TransferID string `json:"transfer_id,omitempty"`
	Path       string `json:"path,omitempty"`
}

// Send copies a file from one of the server's storages onto a printer and does
// not start it. It needs operate on the printer and read_storage.
//
// It returns when the transfer has started. The rest comes on
// device.transfer.settled. The refusals are those of Print.
func (c *Client) Send(ctx context.Context, params SendParams) (Sent, error) {
	if err := params.Validate(); err != nil {
		return Sent{}, fmt.Errorf("deviceapi: %s: %w", MethodSend, err)
	}

	var sent Sent
	if err := c.call(ctx, MethodSend, params, &sent); err != nil {
		return Sent{}, err
	}

	return sent, nil
}

func sendHandler(fleet Fleet) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var params SendParams

		if len(call.Params) == 0 {
			return nil, badParams(MethodSend)
		}

		if err := json.Unmarshal(call.Params, &params); err != nil {
			return nil, badParams(MethodSend)
		}

		if err := params.Validate(); err != nil {
			return nil, refusal(err)
		}

		sent, err := fleet.Send(ctx, params)
		if err != nil {
			return nil, refusal(err)
		}

		return sent, nil
	}
}
