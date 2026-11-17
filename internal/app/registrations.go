package app

// The operator's part of admission. A dialect whose devices pair by displaying
// a code holds the table. This file publishes it, so an operator decides
// through the API and does not edit configuration and restart.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// registrations is every connector's registration table as one surface.
//
// A decision names only the code, which is what an operator reads off a
// machine. Codes are minted per connector, so a decision walks the tables in
// instance order and the first that holds the code takes it. Two connectors
// minting one code needs two equal draws of ten characters from a 29 character
// alphabet. If it happens, the fixed order gives the same outcome on every
// call.
type registrations struct {
	tables []admissionTable
	// fleet is the device plane, which turns an approval into an identity. A
	// device id is the host's name for a machine and a dialect never sees
	// one.
	//
	// It is the plane itself and not an interface over it, because there is
	// one plane and no second implementation. A test that decides nothing
	// about identity passes nil, and Approve and Reject check for it.
	fleet *devices
}

// newRegistrations puts the tables in instance order, so a listing reads the
// same twice and a decision reaches the same connector twice.
func newRegistrations(tables []admissionTable, fleet *devices) registrations {
	ordered := slices.Clone(tables)

	slices.SortFunc(ordered, func(a, b admissionTable) int {
		return strings.Compare(a.ref.String(), b.ref.String())
	})

	return registrations{tables: ordered, fleet: fleet}
}

// Pending is every device waiting for a decision, over every connector.
func (r registrations) Pending(context.Context) ([]httpapi.Registration, error) {
	var waiting []httpapi.Registration

	for _, table := range r.tables {
		for _, item := range table.driver.Pending() {
			waiting = append(waiting, rowOf(table.ref.String(), item))
		}
	}

	return waiting, nil
}

// Approve lets one device through. A code no table holds is
// admission.ErrUnknownCode, which the transport renders as the same refusal a
// rejected code gets.
func (r registrations) Approve(ctx context.Context, code string) (httpapi.Registration, error) {
	// by is who is deciding, as the graph named them when the request was
	// authenticated. It is recorded against the registration. It is empty on
	// a deployment that authenticates nobody.
	by := httpapi.PrincipalFrom(ctx)

	return r.admit(ctx, code, by, by)
}

// admit is the approval with its two identities apart: who is recorded
// against the registration as having admitted the machine, and the person
// the identity is decided for.
//
// They are one principal on the API, where a person decided. They differ for a
// module admitting a device as itself. The instance is recorded, because it
// decided and an audit reads it. No person is claimed, because a module is not
// a person. A program's word never links a device to a person. So a machine
// this fleet already has keeps its id and its owner, and a new machine is
// bound as a first admission is.
func (r registrations) admit(
	ctx context.Context, code, recorded, claimant string,
) (httpapi.Registration, error) {
	for _, table := range r.tables {
		approved, err := table.driver.Approve(ctx, code, recorded)
		if errors.Is(err, admission.ErrUnknownCode) {
			continue
		}

		if err != nil {
			return httpapi.Registration{}, fmt.Errorf("approve a registration on %s: %w", table.ref, err)
		}

		// The identity is bound here and not at the first handshake, because
		// the printer collects the id from GET /p/auth before it dials the
		// websocket. It is bound after the driver's own write, so an approval
		// the table refused mints nothing.
		//
		// Who decided goes with it. A second approval of a machine this fleet
		// already has is a re-bind by the person who owns it and an ownership
		// transfer by anybody else (approvalKeepsTheIdentity).
		//
		// A failure is returned to the caller. The registration is approved
		// by then and stays approved. The machine is still bound to the
		// device that held it, so a second attempt makes the whole decision
		// again.
		row := rowOf(table.ref.String(), approved)

		if r.fleet != nil {
			if err = r.fleet.registrationApproved(ctx, approved, claimant); err != nil {
				return httpapi.Registration{},
					fmt.Errorf("bind an identity for the registration approved on %s: %w", table.ref, err)
			}

			// After the binding, so the id on the answer and on the frame is
			// the id the machine will arrive under. A page that approved a code
			// opens the device page from either of them.
			row.DeviceID = r.fleet.identities.boundDevice(approved.Fingerprint)

			r.fleet.registrationDecided(ctx, table.ref.String(),
				event.TypeDeviceRegistrationApproved, encoded(event.RegistrationApproved{
					Code:        approved.Code,
					Connector:   table.ref.String(),
					Fingerprint: approved.Fingerprint,
					DeviceID:    row.DeviceID,
					Serial:      approved.Serial,
					Model:       approved.Model,
					Firmware:    approved.Firmware,
				}))
		}

		return row, nil
	}

	return httpapi.Registration{}, fmt.Errorf("%w: %s", admission.ErrUnknownCode, code)
}

// Reject forgets one registration and every credential it held.
func (r registrations) Reject(ctx context.Context, code string) error {
	for _, table := range r.tables {
		// Read before the rejection, because the row is gone afterwards and the
		// frame names the machine the decision was about.
		turned := r.pendingRow(table, code)

		err := table.driver.Reject(ctx, code)
		if errors.Is(err, admission.ErrUnknownCode) {
			continue
		}

		if err != nil {
			return fmt.Errorf("reject a registration on %s: %w", table.ref, err)
		}

		if r.fleet != nil {
			r.fleet.registrationDecided(ctx, table.ref.String(),
				event.TypeDeviceRegistrationRejected, encoded(event.RegistrationRejected{
					Code:        code,
					Connector:   table.ref.String(),
					Fingerprint: turned.Fingerprint,
				}))
		}

		return nil
	}

	return fmt.Errorf("%w: %s", admission.ErrUnknownCode, code)
}

// pendingRow is the registration one table holds under a code, and the zero
// value for a code it does not hold. It walks the table's own listing, which is
// bounded by the codes a connector has minted and is read once per rejection.
func (r registrations) pendingRow(table admissionTable, code string) admission.Registration {
	for _, item := range table.driver.Pending() {
		if item.Code == code {
			return item
		}
	}

	return admission.Registration{}
}

// rowOf is one registration on the wire. The connector is added here, because
// the table knows which instance it belongs to and the driver does not.
func rowOf(connector string, item admission.Registration) httpapi.Registration {
	return httpapi.Registration{
		Code:        item.Code,
		Connector:   connector,
		Fingerprint: item.Fingerprint,
		Serial:      item.Serial,
		Model:       item.Model,
		Firmware:    item.Firmware,
		Approved:    item.Approved,
	}
}

// admissionNames is the instance name of every connector holding a registration
// table. The visibility answer offers these to a caller who may decide an
// admission.
func admissionNames(tables []admissionTable) []string {
	out := make([]string, 0, len(tables))
	for _, table := range tables {
		out = append(out, table.ref.String())
	}

	return out
}
