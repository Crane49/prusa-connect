package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// connectorHealthPath is the rollup. It sits beside the liveness probe and is
// not part of it. An unplugged printer is normal operation, so it is reported
// here and never in the verdict a load balancer routes on.
const connectorHealthPath = "/api/v1/health/connectors"

// ConnectorHealth is one rollup per connector, as this surface reads it. It is
// declared here because this is where it is consumed, and the device plane in
// internal/app satisfies it by rolling up the printers each caller may see.
//
// It takes the principal for the same reason the printer listing takes one. A
// rollup names device identifiers and counts them, and a caller who may see
// twenty printers must not learn from a count that there are four hundred.
type ConnectorHealth interface {
	// Connectors rolls up the devices this caller may view telemetry on, one
	// entry per connector they have at least one device on.
	Connectors(ctx context.Context, principal string) ([]ConnectorRollup, error)
}

// ConnectorRollup is one connector's whole device population in one entry.
//
// It carries the counts and a capped list of the devices worth looking at. One
// entry per healthy device would be a large body nobody reads, and a dashboard
// polling it would load the server.
type ConnectorRollup struct {
	Connector string `json:"connector" doc:"The connector instance these devices arrived through."`
	Status    string `json:"status"    doc:"The verdict over this connector's whole population."   enum:"pass,degraded,fail"`

	// Counts carries every state, including the ones with none, so a client
	// reading a key it expects does not have to guess whether a missing key
	// means zero.
	Counts map[string]int `json:"counts" doc:"How many devices are in each state. Every state this build knows is present, at zero when empty."`

	Degraded          []ConnectorDevice `json:"degraded"           doc:"The devices worth looking at, longest-broken first, capped."`
	DegradedTruncated int               `json:"degraded_truncated" doc:"How many degraded devices are not in the list. A truncated list without this is a lie."`
	Total             int               `json:"total"              doc:"The population size this caller may see."`
}

// ConnectorDevice is one device the rollup asks somebody to look at.
type ConnectorDevice struct {
	ID     string `json:"id"               doc:"The device's identity within its connector."`
	Name   string `json:"name,omitempty"   doc:"What a person calls the printer: the name an operator gave it, or the model and serial it reported."`
	State  string `json:"state"            doc:"Where it was when its connector last reported."                                                      enum:"connected,connecting,disconnected,error,unknown"`
	Reason string `json:"reason,omitempty" doc:"A stable machine-readable token such as AuthRejected. It is not a message and not an error string."`

	// Since and LastSeen are absent, not zero, when nothing recorded them. A
	// zero timestamp on the wire is not a real time, and a client that
	// rendered it would report a device as broken since that date.
	Since    *time.Time `json:"since,omitempty"     doc:"When it entered this state, absent when the connector records no transition time."`
	LastSeen *time.Time `json:"last_seen,omitempty" doc:"When the connector last heard from it, absent when it never has."`

	Attempts int `json:"attempts" doc:"How many times the connector has tried since then."`
}

type connectorHealthOutput struct {
	Body struct {
		Items []ConnectorRollup `json:"items"`
	}
}

// registerConnectorHealth publishes the rollup. A nil source answers an empty
// list and not 404: a deployment with no connector plane has no connectors to
// report, but the path still exists.
func registerConnectorHealth(api huma.API, source ConnectorHealth) {
	register(api, huma.Operation{
		OperationID: "get-connector-health",
		Method:      http.MethodGet,
		Path:        connectorHealthPath,
		Summary:     "Device health, rolled up per connector",
		Description: "One entry per connector the caller has a device on: the counts by state, the " +
			"verdict over the population, and the devices worth looking at, capped and " +
			"longest-broken first. It is deliberately not part of the readiness verdict. A printer " +
			"that is switched off overnight is normal operations, and a probe that failed for it " +
			"would take a healthy process out of rotation. The population is the one the caller may " +
			"view telemetry on, so the counts disclose nothing the printer listing does not.",
		Tags:   []string{"system"},
		Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*connectorHealthOutput, error) {
		out := &connectorHealthOutput{}
		out.Body.Items = []ConnectorRollup{}

		if source == nil {
			return out, nil
		}

		rollups, err := source.Connectors(ctx, principalFrom(ctx))
		if err != nil {
			return nil, err
		}

		if len(rollups) > 0 {
			out.Body.Items = rollups
		}

		return out, nil
	})
}
