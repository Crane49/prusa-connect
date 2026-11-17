package devtools

import (
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// What became of one connector in this process. The column a person reads
// first, so it says what happened rather than carrying a boolean somebody has
// to interpret.
const (
	// StatusMounted is a driver whose declared routes this origin serves.
	StatusMounted = "mounted"
	// StatusUnmounted is a driver that declares routes this origin does not
	// serve, which is a driver a printer cannot reach.
	StatusUnmounted = "not mounted"
	// StatusHostChosen is an inbound driver that can be told where to dial. The
	// host picks its prefix, and nothing in the registry records which, so the
	// route it is on is in Routes at the top of the inventory and not here.
	StatusHostChosen = "mounted under a host-chosen prefix"
	// StatusNoRoutes is an outbound driver. The host dials the device, so there
	// is no path for a device to arrive on.
	StatusNoRoutes = "outbound, no routes"
	// StatusFailed is a type this build registers that could not be built or
	// that names no direction. Failure says which.
	StatusFailed = "failed"
)

// The directions a driver can be reached in, as the view names them.
const (
	directionInbound  = "inbound"
	directionOutbound = "outbound"
	directionBoth     = "inbound and outbound"
)

// Route is one path this origin serves that the OpenAPI document does not
// publish: a frozen device wire, or the frontend at the root. It is the host's
// own declaration, carried through unchanged.
type Route struct {
	Pattern string `json:"pattern" doc:"The pattern the router matches on."`
	Reason  string `json:"reason"  doc:"Why this path is not a published operation."`
}

// Connector is one printer dialect this build carries.
type Connector struct {
	Name      string `json:"name"      doc:"The bare type name, as configuration writes it."`
	Qualified string `json:"qualified" doc:"The namespace-qualified name, which is unique by construction."`
	Namespace string `json:"namespace" doc:"The module the registration came from."`
	Version   string `json:"version"   doc:"The driver's own version."`
	Contract  string `json:"contract"  doc:"The connector contract version it compiled against."`
	Stability string `json:"stability" doc:"What an operator may assume about this type between releases."`
	Summary   string `json:"summary"   doc:"One sentence on the dialect."`

	Direction string `json:"direction" doc:"Whether the host dials the device, the device dials the host, or both."`
	Status    string `json:"status"    doc:"What became of this connector in this process."`

	// Routes are the absolute paths this dialect's firmware has compiled in.
	// Only a driver that declares them has any, and for one that does the join
	// against what this origin serves is exact.
	Routes []string `json:"routes" doc:"The paths this dialect's devices dial, when the dialect declares them itself."`

	// Baseline is what the driver's code can do at all, before any profile or
	// probe narrows it.
	Baseline int `json:"baseline_capabilities" doc:"How many capabilities the driver's baseline declares."`

	// Failure is why this type produced no driver. Empty for every other status.
	Failure string `json:"failure,omitempty" doc:"Why this type could not be built, for a connector that failed."`
}

// Inventory is the device plane of one build: every dialect compiled into it,
// and every path this origin serves that is not an operation.
type Inventory struct {
	Connectors []Connector `json:"connectors" doc:"Every printer dialect this build carries, by qualified name."`
	Routes     []Route     `json:"routes"     doc:"Every path this origin serves that the OpenAPI document does not publish."`
}

// declaresRoutes is the half of an inbound driver whose devices cannot be told
// where to dial: shipping firmware with absolute paths compiled in. It is
// declared here because this is where it is consumed, and it is the one thing
// that makes the join below exact rather than a guess at what prefix a host
// chose.
type declaresRoutes interface {
	Routes() []string
}

// ReadInventory builds one probe instance of every type in the registry and
// reports what this build carries, joined to the routes it was handed.
//
// The probe is a read and not a second mounting. A factory is handed no host
// services, so by contract it cannot reach a device before the host is ready,
// and Baseline is documented pure and called once at registration; the probe is
// never handed Arrivals and never serves a request. It is built from the same
// spec a host builds from, which is the registration's own qualified name and
// no config block, so the two facts read here are the ones the mounted driver
// reports.
//
// A type that fails to build is reported as failed rather than dropped. A
// connector missing from this list would read as a build that does not carry
// the dialect at all, which is a different problem with a different fix.
func ReadInventory(types *connectorapi.Registry, routes []Route) Inventory {
	served := make([]string, 0, len(routes))
	for _, route := range routes {
		served = append(served, route.Pattern)
	}

	registered := types.Types()
	connectors := make([]Connector, 0, len(registered))

	for _, registration := range registered {
		connectors = append(connectors, describe(registration, served))
	}

	return Inventory{Connectors: connectors, Routes: routes}
}

// describe reads one registered type. Every failure lands in the value rather
// than in an error: one broken driver must not cost the view the other rows,
// which are what tell a reader whether the process is otherwise wired.
func describe(registration connectorapi.Registration, served []string) Connector {
	descriptor := registration.Descriptor()
	doc := descriptor.Doc()

	out := Connector{
		Name:      registration.Name().String(),
		Qualified: registration.Qualified().String(),
		Namespace: registration.Namespace().String(),
		Version:   doc.Version.String(),
		Contract:  doc.Contract.String(),
		Stability: doc.Stability.String(),
		Summary:   doc.Summary,
		Status:    StatusFailed,
	}

	driver, err := registration.Build(connectorapi.Spec{
		Ref:  connectorapi.Ref(registration.Name()),
		Type: registration.Qualified(),
	})
	if err != nil {
		out.Failure = err.Error()

		return out
	}

	inbound, outbound, err := connectorapi.Directions(driver)
	if err != nil {
		out.Failure = err.Error()

		return out
	}

	out.Direction = directionOf(inbound != nil, outbound != nil)
	out.Baseline = len(driver.Baseline())
	out.Routes, out.Status = placement(driver, inbound != nil, served)

	return out
}

// placement is where this driver's devices arrive, and whether this origin is
// serving that path.
func placement(driver connectorapi.Driver, inbound bool, served []string) ([]string, string) {
	if !inbound {
		return nil, StatusNoRoutes
	}

	frozen, declares := driver.(declaresRoutes)
	if !declares {
		return nil, StatusHostChosen
	}

	routes := frozen.Routes()
	for _, route := range routes {
		if !slices.Contains(served, route) {
			return routes, StatusUnmounted
		}
	}

	return routes, StatusMounted
}

func directionOf(inbound, outbound bool) string {
	switch {
	case inbound && outbound:
		return directionBoth
	case inbound:
		return directionInbound
	default:
		return directionOutbound
	}
}
