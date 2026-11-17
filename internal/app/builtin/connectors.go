package builtin

import (
	"fmt"

	"github.com/prusa3d/prusa-connect/adapters/connector/echo"
	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The dialects this binary carries. This is the only file under internal/app
// that names one. The core reads what a driver declares and never branches on a
// name, so a name written anywhere else is a feature a driver from outside this
// repository cannot have. internal/arch checks that rule.
//
// The Moonraker dialect is not here. It is a module of its own that a build
// configuration admits.

// defaultDialect is the dialect a deployment that declares no connector mounts.
// Its firmware dials fixed paths, so a configuration file that says nothing
// about connectors keeps serving the printers already in the field.
const defaultDialect = xbuddy.Type

// referenceDialect answers every machine that connects and authenticates
// nobody. It is mounted only where a configuration names it.
const referenceDialect = echo.Type

// ShippingConnector is the dialect a deployment that declares none mounts. It
// is the name alone, for a caller that writes a configuration file and mounts
// nothing.
func ShippingConnector() connectorapi.TypeName { return defaultDialect }

// DefaultConnector is the dialect to mount for a deployment that declares none,
// looked up in the registry this build composed.
//
// The lookup matters: a name this package holds and the binary does not carry
// would leave a server with no connector and no error.
func DefaultConnector(types *connectorapi.Registry) (connectorapi.TypeName, error) {
	if _, err := types.Lookup(defaultDialect); err != nil {
		return "", fmt.Errorf("the shipping dialect %s is not in this build: %w", defaultDialect, err)
	}

	return defaultDialect, nil
}

// ReferenceConnector is the dialect that answers every machine, for a demo and
// for a deployment with nothing to plug in yet.
func ReferenceConnector() connectorapi.TypeName { return referenceDialect }
