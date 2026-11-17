package app

import (
	"fmt"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/devicestate"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// placedStates places the state sets the mounted dialects declared, which is
// what GET /api/v1/vocabularies/device-states serves.
//
// It reads what the host installed, the registration of every mounted
// connector, and never a running session. Two connectors of one type share one
// entry that names both instances, because a device names its instance and a
// client joins it onto the entry by that name.
//
// A refused set refuses the start. connectorapi.Register already refuses a
// dialect whose set the grammar rejects, so what is left to refuse here is a
// placement the host itself cannot make, and serving a vocabulary that
// silently dropped one dialect would leave a client with no way to know.
func placedStates(mounted []mountedStates) (devicestate.Installed, error) {
	order := make([]connectorapi.TypeName, 0, len(mounted))
	byType := make(map[connectorapi.TypeName]*devicestate.Declaration, len(mounted))

	for _, one := range mounted {
		qualified := one.registration.Qualified()

		declaration, seen := byType[qualified]
		if !seen {
			declaration = &devicestate.Declaration{
				Namespace: one.registration.Namespace(),
				Kind:      ref.Kind(one.kind.Or(authz.DeviceKindPrinter)),
				Connector: qualified,
				States:    one.registration.Dialect().States,
			}
			byType[qualified] = declaration
			order = append(order, qualified)
		}

		declaration.Instances = append(declaration.Instances, one.instance.String())
	}

	declarations := make([]devicestate.Declaration, 0, len(order))

	for _, qualified := range order {
		declaration := byType[qualified]
		slices.Sort(declaration.Instances)
		declarations = append(declarations, *declaration)
	}

	installed, err := devicestate.Place(declarations...)
	if err != nil {
		return devicestate.Installed{}, fmt.Errorf("place the device states the connectors declared: %w", err)
	}

	return installed, nil
}
