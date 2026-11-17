package httpapi

import (
	"net/http"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func notFoundPrinter(id string) error {
	return fault.PrinterNotFound.New("no connected printer answers to that id").
		With(fault.StringParam("device_id", id))
}

func devPrinter() Device {
	return Device{
		ID:              "pdeadbeef",
		Name:            "COREONE SIM-9",
		Model:           "COREONE",
		Serial:          "SIM-9",
		Firmware:        "6.4.0",
		Driver:          "prusa-xbuddy",
		Connected:       true,
		Capabilities:    []string{"SET_NOZZLE_TEMPERATURE"},
		CapabilityEpoch: 4,
		ProfileMatched:  true,
	}
}

// devMounts is what a host hands in for the paths it serves that are not
// operations, which is what the connector panel joins against.
func devMounts() []Mount {
	return []Mount{{
		Pattern: "/p/ws",
		Handler: http.NotFoundHandler(),
		Reason:  "the prusa-xbuddy connector: a path this dialect's firmware has compiled in",
	}}
}
