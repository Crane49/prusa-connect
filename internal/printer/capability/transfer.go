package capability

import (
	"context"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Send puts one file on the printer and returns when it is there.
//
// It takes the same route a command does: the resolved set decides whether the
// printer may be asked at all, and the driver is reached only if it does.
// A dialect that cannot transfer a file and a printer whose resolved set does
// not carry the verb get the same refusal, because both mean this printer
// cannot be sent a file.
//
// ctx bounds the whole transfer. The bytes leave while the printer asks for
// them, so a deadline shorter than the file needs cancels a transfer the
// printer would have completed.
func (s *Session) Send(ctx context.Context, up connectorapi.Upload) error {
	// The destination is refused here and not only in the driver, for the
	// reason List gives: a driver may forget the check.
	if refusal := connectorapi.RefusePathEscape(up.Path); refusal != nil {
		return refusal
	}

	transfers, err := s.transfers(connectorapi.VerbFileUpload)
	if err != nil {
		return err
	}

	if sendErr := transfers.Send(ctx, up); sendErr != nil {
		return fmt.Errorf("send %q to the printer: %w", up.Path, sendErr)
	}

	return nil
}

// transfers is the driver's transfer half, once the resolved set says the verb
// it is spoken through is one this printer has.
func (s *Session) transfers(verb connectorapi.Capability) (connectorapi.Transfers, error) {
	if !s.current.Load().resolution.Capabilities.Has(verb) {
		return nil, connectorapi.CapabilityNotSupported.
			New("the printer does not have that capability").
			With(fault.StringParam("verb", verb.String()))
	}

	transfers, ok := As[connectorapi.Transfers](s.inner)
	if !ok {
		return nil, connectorapi.CapabilityNotSupported.
			New("the printer does not have that capability").
			With(fault.StringParam("verb", verb.String())).
			Wrap(connectorapi.ErrNoTransfers)
	}

	return transfers, nil
}
