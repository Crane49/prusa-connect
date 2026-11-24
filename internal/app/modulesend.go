package app

import (
	"context"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// Send lands a file on a printer and starts nothing. It takes what Print takes
// for a file from a storage, and answers when the transfer has started.
func (f moduleFleet) Send(
	ctx context.Context, params deviceapi.SendParams,
) (deviceapi.Sent, error) {
	who, err := f.as(params.Principal, authz.ActionOperate)
	if err != nil {
		return deviceapi.Sent{}, err
	}

	if _, err = f.as(params.Principal, authz.ActionReadStorage); err != nil {
		return deviceapi.Sent{}, err
	}

	if params.Storage == deviceapi.ModuleDataStorage {
		if f.data == "" {
			return deviceapi.Sent{}, fmt.Errorf("%w: %s was granted no data directory, so it has no %s",
				deviceapi.ErrDenied, f.standing.principal, deviceapi.ModuleDataStorage)
		}

		ctx = withModuleData(ctx, f.data, params.DeleteSource)
	}

	result, err := f.fleet.StartSend(ctx, who, params.ID, httpapi.DevicePrint{
		Storage:  params.Storage,
		Source:   params.Source,
		Path:     params.Path,
		KeepBoth: params.KeepBoth,
	})
	if err != nil {
		return deviceapi.Sent{}, f.printRefusal(ctx, err, params.Path)
	}

	sent := deviceapi.Sent{Path: result.Path}
	if result.Transfer != nil {
		sent.TransferID = result.Transfer.TransferID
	}

	return sent, nil
}
