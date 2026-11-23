package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"

	"github.com/prusa3d/prusaslicer3-connector/internal/account"
	"github.com/prusa3d/prusaslicer3-connector/internal/connect"
)

// The adapters below turn the host's wire methods into the small interfaces
// the account and connect packages declare, so those packages hold no wire
// type and a test fakes them with a struct.

// actionOperate is the action the module asks about before a print, as the
// authorization vocabulary spells it.
const actionOperate = "operate"

// access is connect.Access over access.check. The host answers about the
// person whose upload request is open.
type access struct{ client *accessapi.Client }

// MayPrint asks whether principal may start a print on printer, while the
// person's upload request is open. A host that does not let the module ask,
// because the operator did not approve the check, leaves the print itself as
// the check.
func (a access) MayPrint(ctx context.Context, principal, printer string) (bool, error) {
	decision, err := a.client.Check(ctx, accessapi.CheckParams{
		Subject: principal, Action: actionOperate, ObjectType: "device", ObjectID: printer,
	})

	switch {
	case errors.Is(err, accessapi.ErrDenied):
		return false, fmt.Errorf("%w: %w", connect.ErrCannotAsk, err)
	case err != nil:
		return false, fmt.Errorf("ask whether %s may print on %s: %w", principal, printer, err)
	}

	return decision.Allowed, nil
}

// people is account.People over access.people.
type people struct{ client *accessapi.Client }

func (p people) People(ctx context.Context) ([]account.Person, error) {
	all, err := p.client.People(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the people: %w", err)
	}

	out := make([]account.Person, 0, len(all))
	for _, person := range all {
		out = append(out, account.Person{Subject: person.Subject, Name: person.Name})
	}

	return out, nil
}

// fleet is connect.Fleet over the host's device door.
type fleet struct{ devices *deviceapi.Client }

// pageSize is how many printers one device.list call asks for.
const pageSize = 200

func (f fleet) Printers(ctx context.Context, principal string) ([]connect.Printer, error) {
	var out []connect.Printer

	for offset := 0; ; offset += pageSize {
		page, err := f.devices.List(ctx, deviceapi.ListParams{Principal: principal, Limit: pageSize, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("list the printers: %w", deviceRefusal(err))
		}

		for _, device := range page.Items {
			if device.Kind == "printer" && !device.Retired {
				out = append(out, printerOf(device))
			}
		}

		if offset+len(page.Items) >= page.Total || len(page.Items) == 0 {
			return out, nil
		}
	}
}

func (f fleet) Printer(ctx context.Context, principal, id string) (connect.Printer, error) {
	device, err := f.devices.Get(ctx, deviceapi.GetParams{Principal: principal, ID: id})
	if err != nil {
		return connect.Printer{}, deviceRefusal(err)
	}

	if device.Retired {
		return connect.Printer{}, connect.ErrNoPrinter
	}

	return printerOf(device), nil
}

// Print keeps the printer's file of the same name and sends this one under a
// free name, as the cloud does when a person sends the same file again. The
// host deletes source once the print has settled.
func (f fleet) Print(ctx context.Context, principal, id, source, target string) (string, error) {
	started, err := f.devices.Print(ctx, deviceapi.PrintParams{
		Principal:    principal,
		ID:           id,
		Storage:      deviceapi.ModuleDataStorage,
		Source:       source,
		Path:         target,
		KeepBoth:     true,
		DeleteSource: true,
	})
	if err != nil {
		return "", deviceRefusal(err)
	}

	return started.Path, nil
}

func printerOf(device deviceapi.Device) connect.Printer {
	return connect.Printer{
		ID: device.ID, Name: device.Name, Model: device.Model,
		Connected: device.Connected, State: device.State, Capabilities: device.Capabilities,
	}
}

// deviceRefusal maps what the device door reported to the refusals the
// connect package answers.
func deviceRefusal(err error) error {
	switch {
	case errors.Is(err, deviceapi.ErrNoDevice):
		return fmt.Errorf("%w: %w", connect.ErrNoPrinter, err)
	case errors.Is(err, deviceapi.ErrDenied):
		return fmt.Errorf("%w: %w", connect.ErrDenied, err)
	case errors.Is(err, deviceapi.ErrNameTaken):
		return fmt.Errorf("%w: %w", connect.ErrNameTaken, err)
	case errors.Is(err, deviceapi.ErrNotReady):
		return fmt.Errorf("%w: %w", connect.ErrNotReady, err)
	case errors.Is(err, deviceapi.ErrPrintRefused), errors.Is(err, deviceapi.ErrPrintFailed):
		return fmt.Errorf("%w: %w", connect.ErrPrintRefused, err)
	default:
		return err
	}
}
