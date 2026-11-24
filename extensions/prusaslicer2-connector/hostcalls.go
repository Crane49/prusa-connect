package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"

	"github.com/prusa3d/prusaslicer2-connector/internal/link"
)

// The adapters below turn the host's wire methods into the small interfaces
// the link package declares, so that package holds no wire type and a test
// fakes them with a struct.

// actionOperate is the action the module asks about before a print, as the
// authorization vocabulary spells it.
const actionOperate = "operate"

// access is link.Access over access.check. The host answers about the person
// whose upload request is open.
type access struct{ client *accessapi.Client }

// MayPrint asks whether principal may start a print on printer. A host that
// does not let the module ask, because the operator did not approve the
// check, leaves the print itself as the check.
func (a access) MayPrint(ctx context.Context, principal, printer string) (bool, error) {
	decision, err := a.client.Check(ctx, accessapi.CheckParams{
		Subject: principal, Action: actionOperate, ObjectType: "device", ObjectID: printer,
	})

	switch {
	case errors.Is(err, accessapi.ErrDenied):
		return false, fmt.Errorf("%w: %w", link.ErrCannotAsk, err)
	case err != nil:
		return false, fmt.Errorf("ask whether %s may print on %s: %w", principal, printer, err)
	}

	return decision.Allowed, nil
}

// fleet is link.Fleet over the host's device door.
type fleet struct{ devices *deviceapi.Client }

func (f fleet) Printer(ctx context.Context, principal, id string) (link.Printer, error) {
	device, err := f.devices.Get(ctx, deviceapi.GetParams{Principal: principal, ID: id})
	if err != nil {
		return link.Printer{}, deviceRefusal(err)
	}

	if device.Retired || device.Kind != "printer" {
		return link.Printer{}, link.ErrNoPrinter
	}

	return link.Printer{ID: device.ID, Name: device.Name, Model: device.Model, State: stateOf(device)}, nil
}

// stateOf is the state the printer last reported, in upper case, such as
// IDLE, and OFFLINE for a printer that is away.
func stateOf(device deviceapi.Device) string {
	if !device.Connected {
		return "OFFLINE"
	}

	var state string
	if err := json.Unmarshal(device.State["/state"], &state); err != nil {
		return "UNKNOWN"
	}

	return strings.ToUpper(state)
}

func (f fleet) Print(ctx context.Context, principal, id, storage, source, target string) error {
	_, err := f.devices.Print(ctx, deviceapi.PrintParams(transfer(principal, id, storage, source, target)))
	if err != nil {
		return deviceRefusal(err)
	}

	return nil
}

// Send lands the file without starting it.
func (f fleet) Send(ctx context.Context, principal, id, storage, source, target string) error {
	_, err := f.devices.Send(ctx, transfer(principal, id, storage, source, target))
	if err != nil {
		return deviceRefusal(err)
	}

	return nil
}

// transfer is one file sent to a printer. A file from the print folder is
// deleted there by the host once it landed, so nothing waits for the sweep.
// A file from a storage of this server stays there.
func transfer(principal, id, storage, source, target string) deviceapi.SendParams {
	params := deviceapi.SendParams{
		Principal: principal,
		ID:        id,
		Storage:   storage,
		Source:    source,
		Path:      target,
		KeepBoth:  true,
	}

	if storage == "" {
		params.Storage = deviceapi.ModuleDataStorage
		params.DeleteSource = true
	}

	return params
}

// deviceRefusal maps what the device door reported to the refusals the link
// package answers.
func deviceRefusal(err error) error {
	switch {
	case errors.Is(err, deviceapi.ErrNoDevice), errors.Is(err, deviceapi.ErrInvalidQuery):
		return fmt.Errorf("%w: %w", link.ErrNoPrinter, err)
	case errors.Is(err, deviceapi.ErrDenied):
		return fmt.Errorf("%w: %w", link.ErrDenied, err)
	case errors.Is(err, deviceapi.ErrNameTaken):
		return fmt.Errorf("%w: %w", link.ErrNameTaken, err)
	case errors.Is(err, deviceapi.ErrNotReady):
		return fmt.Errorf("%w: %w", link.ErrNotReady, err)
	case errors.Is(err, deviceapi.ErrPrintRefused), errors.Is(err, deviceapi.ErrPrintFailed):
		return fmt.Errorf("%w: %w", link.ErrPrintRefused, err)
	default:
		return err
	}
}

// files is link.Files over storage.write.
type files struct{ storages *storageapi.Client }

func (f files) Write(ctx context.Context, principal, storage, file string, size int64, body io.Reader) error {
	_, err := f.storages.Write(ctx, storageapi.WriteParams{
		Principal: principal, Storage: storage, Path: file, Size: size,
	}, body)
	if err != nil {
		return fmt.Errorf("write %s into %s: %w", file, storage, keepRefusal(err))
	}

	return nil
}

func (f files) Exists(ctx context.Context, principal, storage, file string) (bool, error) {
	_, err := f.storages.Stat(ctx, storageapi.StatParams{Principal: principal, Storage: storage, Path: file})

	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, storageapi.ErrNoFile):
		return false, nil
	default:
		return false, fmt.Errorf("look for %s in %s: %w", file, storage, keepRefusal(err))
	}
}

// keepRefusal maps what storage.write reported to the refusals the link
// package answers.
func keepRefusal(err error) error {
	switch {
	case errors.Is(err, storageapi.ErrNoFile):
		return fmt.Errorf("%w: %w", link.ErrNoShelf, err)
	case errors.Is(err, storageapi.ErrDenied):
		return fmt.Errorf("%w: %w", link.ErrCannotKeep, err)
	default:
		return err
	}
}
