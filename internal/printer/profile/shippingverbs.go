package profile

import (
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// verbPrefix is the namespace every core command verb sits under.
const verbPrefix = "core.command."

// renamedVerbs holds the shipping firmware command names whose published verb
// is not the name lowercased. A row exists where the vocabulary's naming rule
// and the firmware's spelling disagree. This project cannot change the
// firmware's spelling.
//
// SET_HEATBED_TEMPERATURE is the only row. The heated bed is spelled `bed` on
// every surface this project names: the argument is `bed_temperature` and the
// state paths are `/temperature/bed` and `/temperature/bed_target`. The
// firmware command keeps its own name. So does the MQTT topic segment, which
// is frozen.
//
//nolint:gochecknoglobals // a static table read at start, never written.
var renamedVerbs = map[string]connectorapi.Capability{
	"SET_HEATBED_TEMPERATURE": verbPrefix + "set_bed_temperature",
}

// VerbForShippingCommand returns the published verb one shipping firmware
// command maps to.
//
// The profile data and the driver's bound comparison both convert the recorded
// firmware matrix into capability keys, and a rename that lived in only one of
// them would make the two disagree about the same command.
func VerbForShippingCommand(command string) connectorapi.Capability {
	if verb, ok := renamedVerbs[command]; ok {
		return verb
	}

	return connectorapi.Capability(verbPrefix + strings.ToLower(command))
}
