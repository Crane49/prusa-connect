package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// The endpoint paths. An endpoint is addressed by name alone: a name is unique
// across the list rather than within one connector, so a machine can be moved
// to another dialect without changing the URL it is reached at.
const (
	connectorEndpointsPath = "/api/v1/connector-endpoints"
	connectorEndpointPath  = connectorEndpointsPath + "/{name}"
)

type listConnectorEndpointsOutput struct {
	ETag string `header:"ETag" doc:"The version of the configuration this listing was read from."`
	Body struct {
		Items []ConnectorEndpoint `json:"items"`
	}
}

// ConnectorEndpointRef is the endpoint a path names. It is exported because
// Huma binds parameters by reflection and cannot write into an unexported
// embedded field.
type ConnectorEndpointRef struct {
	Name string `path:"name" maxLength:"63" doc:"The operator-chosen name." example:"bench-printer"`
}

// connectorEndpointBody is what a PUT accepts: the declaration, plus the
// read-only members a listing returns, so a row read from the listing can be
// sent straight back.
type connectorEndpointBody struct {
	EndpointSpec

	Name    string `json:"name,omitempty"    doc:"Ignored unless it disagrees with the path, which is refused."`
	Version string `json:"version,omitempty" doc:"Read-only. The precondition rides in If-Match, not here."`
}

type writeConnectorEndpointInput struct {
	ConnectorEndpointRef

	IfMatch     string `header:"If-Match"      doc:"The ETag last read, quotes included. A list is accepted; a weak validator is not. * alone does not fence a write."`
	IfNoneMatch string `header:"If-None-Match" doc:"Send * to create only, failing if the endpoint already exists. A tag list is compared weakly and alone does not fence a write."`
	DryRun      bool   `                       doc:"Validate and report the result without writing the file."                                                                       query:"dry_run" example:"true"`

	Body connectorEndpointBody
}

type writeConnectorEndpointOutput struct {
	Status   int
	ETag     string `header:"ETag"     doc:"The version to quote in If-Match on the next write. Absent on a dry run, which wrote nothing to quote."`
	Location string `header:"Location" doc:"Where the created endpoint lives. Sent only with 201."`
	Body     ConnectorEndpoint
}

type deleteConnectorEndpointInput struct {
	ConnectorEndpointRef

	IfMatch     string `header:"If-Match"      doc:"The ETag last read, quotes included. A list is accepted. * alone does not fence a delete."`
	IfNoneMatch string `header:"If-None-Match" doc:"A tag list, compared weakly: the delete applies only if none is current. It alone does not fence a delete."`
	DryRun      bool   `                       doc:"Validate without writing the file."                                                                         query:"dry_run" example:"true"`
}

type deleteConnectorEndpointOutput struct {
	Status int
}

// registerConnectorEndpoints publishes the addresses this server dials: the
// listing, and the two writes that add, change and remove one.
func registerConnectorEndpoints(api huma.API, store EndpointStore) {
	registerListConnectorEndpoints(api, store)
	registerPutConnectorEndpoint(api, store)
	registerDeleteConnectorEndpoint(api, store)
}

func registerListConnectorEndpoints(api huma.API, store EndpointStore) {
	register(api, huma.Operation{
		OperationID: "list-connector-endpoints",
		Method:      http.MethodGet,
		Path:        connectorEndpointsPath,
		Summary:     "The addresses this server dials",
		Description: "Every endpoint the caller may read, which takes view_resource on the driver " +
			"kind. An endpoint is one machine this server reaches out to, named by the connector that " +
			"dials it and the address that connector is handed.\n\n" +
			"A printer that dials in is not here. It appears under /api/v1/devices once it has " +
			"arrived, and nothing declares it. The ETag is the version of the configuration overlay " +
			"these rows are stored in, and it moves whenever any endpoint, preset or resource " +
			"instance is written.",
		Tags:   []string{"resources"},
		Errors: []int{http.StatusUnauthorized, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, _ *struct{}) (*listConnectorEndpointsOutput, error) {
		listing, err := store.ListEndpoints(ctx, principalFrom(ctx))
		if err != nil {
			return nil, err
		}

		out := &listConnectorEndpointsOutput{}
		out.Body.Items = listing.Items

		if listing.Version != "" {
			if out.ETag, err = validatedETag(listing.Version); err != nil {
				return nil, err
			}
		}

		return out, nil
	})
}

func registerPutConnectorEndpoint(api huma.API, store EndpointStore) {
	register(api, huma.Operation{
		OperationID: "put-connector-endpoint",
		Method:      http.MethodPut,
		Path:        connectorEndpointPath,
		Summary:     "Declare an address to dial",
		Description: "Writes the endpoint into the machine-owned configuration overlay and answers with " +
			"the row as it now stands. It takes configure_resource on the driver kind, which is the " +
			"grant that already covers this deployment's connectors.\n\n" +
			"The dial supervisor picks the row up as soon as it is written, so adding a printer is a " +
			"write and never a restart. Nothing is dialled while enabled is false, and a row whose " +
			"connector, address or options changed is dialled again from the new declaration.\n\n" +
			"An If-Match tag list or If-None-Match: * is required, because the overlay has another " +
			"writer. A write that creates answers 201 with a Location; one that replaces answers 200.\n\n" +
			"What is at the address is not checked here. The machine may be switched off, so a 201 " +
			"says the row was stored and never that a printer answered. Watch the event stream, or " +
			"read /api/v1/devices, for the arrival.",
		Tags:          []string{"resources"},
		DefaultStatus: http.StatusOK,
		Errors:        mutationErrors,
	}, func(ctx context.Context, in *writeConnectorEndpointInput) (*writeConnectorEndpointOutput, error) {
		if err := in.Body.checkName(in.Name); err != nil {
			return nil, err
		}

		pre, err := precondition(in.IfMatch, in.IfNoneMatch, in.DryRun)
		if err != nil {
			return nil, err
		}

		applied, err := store.ApplyEndpoint(ctx, principalFrom(ctx), in.Name, in.Body.EndpointSpec, pre)
		if err != nil {
			return nil, err
		}

		return connectorEndpointOutput(applied, pre.DryRun)
	})

	documentCreatedResponse(api, connectorEndpointPath)
}

func registerDeleteConnectorEndpoint(api huma.API, store EndpointStore) {
	register(api, huma.Operation{
		OperationID: "delete-connector-endpoint",
		Method:      http.MethodDelete,
		Path:        connectorEndpointPath,
		Summary:     "Stop dialling an address",
		Description: "Removes the endpoint from the machine-owned configuration overlay. The dial loop " +
			"for it stops, and a printer whose session is open is released. A device record the " +
			"machine already produced stays, carrying the reason its session ended.",
		Tags:          []string{"resources"},
		DefaultStatus: http.StatusNoContent,
		Errors:        mutationErrors,
	}, func(ctx context.Context, in *deleteConnectorEndpointInput) (*deleteConnectorEndpointOutput, error) {
		pre, err := precondition(in.IfMatch, in.IfNoneMatch, in.DryRun)
		if err != nil {
			return nil, err
		}

		if err = store.DeleteEndpoint(ctx, principalFrom(ctx), in.Name, pre); err != nil {
			return nil, err
		}

		return &deleteConnectorEndpointOutput{Status: http.StatusNoContent}, nil
	})
}

// connectorEndpointOutput frames what the store wrote. A dry run carries no
// ETag and no version, for the reason applyOutput drops them: it wrote nothing,
// so a token handed back would name a state that never existed.
func connectorEndpointOutput(applied EndpointResult, dryRun bool) (*writeConnectorEndpointOutput, error) {
	if dryRun {
		applied.Endpoint.Version = ""

		return &writeConnectorEndpointOutput{Status: http.StatusOK, Body: applied.Endpoint}, nil
	}

	etag, err := validatedETag(applied.Endpoint.Version)
	if err != nil {
		return nil, err
	}

	out := &writeConnectorEndpointOutput{Status: http.StatusOK, ETag: etag, Body: applied.Endpoint}
	if applied.Created {
		out.Status = http.StatusCreated
		out.Location = connectorEndpointsPath + "/" + applied.Endpoint.Name
	}

	return out, nil
}

// checkName refuses a body that names a different endpoint from the path, for
// the reason resourceSpecBody.checkNames does: the body is echoed back from a
// listing and a disagreement is a client bug worth reporting.
func (b connectorEndpointBody) checkName(name string) error {
	if b.Name != "" && b.Name != name {
		return httpfault.RequestInvalid("body.name",
			"expected "+name, "the body names a different endpoint from the path")
	}

	return nil
}
