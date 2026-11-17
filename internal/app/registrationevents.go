package app

// The registration exchange on the event stream.
//
// A page adding a printer has three things to wait for: a code arriving, a
// decision on it, and the machine dialling in. These frames are the first two.
// One says a code is waiting, one says a code was let through and names the
// device it became, and one says a code was turned away. The third is
// com.prusa.connect.device.arrived, on the device the approval named. A fourth
// frame says a machine that called was refused at the connector's wire, so the
// page can say why the machine is not joining.
//
// The origin is the connector instance that holds the table, not a device.
// There is no device to name until an approval mints one, so whoever may
// observe the connector sees the exchange and whoever may not sees none of it.

import (
	"context"
	"encoding/json"

	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// RegistrationAnnounced puts one waiting code on the bus.
//
// It is called by the record store, because that write is where the host first
// learns of a registration: a connector holds the table and mints the codes,
// and the host sees each one when it is stored. When a machine retries its
// handshake the code is written again, and the store calls this only on the
// first write.
func (d *devices) RegistrationAnnounced(ctx context.Context, record admission.Record) {
	d.publishRegistration(ctx, record.Connector, event.TypeDeviceRegistrationAnnounced,
		encoded(event.RegistrationAnnounced{
			Code:        record.Code,
			Connector:   record.Connector,
			Fingerprint: record.Fingerprint,
			Serial:      record.Serial,
			Model:       record.Model,
			Firmware:    record.Firmware,
		}))
}

// registrationDecided puts an approval or a rejection on the bus.
//
// It is called from the surface that decided, after the decision has been
// written and, for an approval, after the device id has been bound. So a page
// reading the approved frame has an id it can open a device page with.
func (d *devices) registrationDecided(
	ctx context.Context, connector string, kind string, data json.RawMessage,
) {
	d.publishRegistration(ctx, connector, kind, data)
}

// approvedOnArrival puts an approval a connector made on its own on the bus.
//
// The operator's approval publishes its frame from the surface that decided
// (registrations.admit). A connector that approves on arrival decides inside
// its own table, so this is its frame, published after the device id is
// bound so the frame names the id the machine arrives under.
func (d *devices) approvedOnArrival(
	ctx context.Context, connector string, registration admission.Registration,
) {
	d.registrationDecided(ctx, connector, event.TypeDeviceRegistrationApproved,
		encoded(event.RegistrationApproved{
			Code:        registration.Code,
			Connector:   connector,
			Fingerprint: registration.Fingerprint,
			DeviceID:    d.identities.boundDevice(registration.Fingerprint),
			Serial:      registration.Serial,
			Model:       registration.Model,
			Firmware:    registration.Firmware,
		}))
}

// registrationRefused puts a machine a connector turned away on the bus.
//
// The connector decides how often it calls this, once a minute per machine for
// xBuddy, so a refused printer that calls every few seconds produces one frame
// a minute. admission is the connector's own admission setting, in the words
// the app settings use, so a page reading the frame knows whether a person
// would have been asked.
func (d *devices) registrationRefused(
	ctx context.Context, connector, admission string, refusal connectorapi.DeviceRefusal,
) {
	d.publishRegistration(ctx, connector, event.TypeDeviceRegistrationRefused,
		encoded(event.RegistrationRefused{
			Connector:         connector,
			Reason:            string(refusal.Reason),
			Admission:         admission,
			FingerprintDigest: refusal.FingerprintDigest,
			Model:             refusal.Model,
			Firmware:          refusal.Firmware,
		}))
}

// publishRegistration resolves the connector instance as an origin and
// publishes.
//
// A registration names its connector by the instance name an operator wrote in
// configuration, and a connector is an instance of the driver resource kind. So
// the origin is that kind and that name. visibility.VisibleOrigins renders the
// same origin for a caller who may observe the connector.
//
// A name this build cannot render is logged and the frame is dropped, because
// nobody can subscribe to such an origin.
func (d *devices) publishRegistration(
	ctx context.Context, connector string, kind string, data json.RawMessage,
) {
	instance, err := ref.New(ref.KindDriver, connector)
	if err != nil {
		d.log.ErrorContext(ctx, "a registration names a connector that is not a resource",
			"connector", connector, "type", kind, "error", err.Error())

		return
	}

	origin, err := event.NewOrigin(event.Kind(instance.Kind()), instance.ID())
	if err != nil {
		d.log.ErrorContext(ctx, "a connector instance is not an origin",
			"connector", connector, "type", kind, "error", err.Error())

		return
	}

	// No conflation key. Each frame is about one code or one refused machine,
	// and none of them supersedes another.
	d.publishAt(ctx, origin, instance.String(), kind, event.ClassState, "", data)
}
