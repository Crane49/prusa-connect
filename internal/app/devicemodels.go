package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// declaredModels gathers the device models the drivers in this binary declared
// and places them beside the core's.
//
// The connector of each row is the registration's own name, so a driver cannot
// declare a model for a dialect other than its own. The icon key is the generic
// printer: the other keys name Prusa families, and a driver that wants its
// machine drawn declares a drawing of it.
//
// A collision refuses the row and never the start, as a refused device kind
// does. A model is a name and a drawing on a page, and refusing the start
// over one would stop every printer the build serves. Each refusal
// is logged once, at the start, with both ends and what to do, and the
// vocabulary serves the same refusals beside its rows.
func declaredModels(ctx context.Context, logger *slog.Logger) (profile.Catalogue, error) {
	types, err := connectorapi.NewRegistry()
	if err != nil {
		return profile.Catalogue{}, fmt.Errorf("build the connector type registry: %w", err)
	}

	return placeModels(ctx, logger, types.Types()...)
}

// placeModels is declaredModels over registrations it is handed, so a test can
// place a declaration without registering it for the whole process.
func placeModels(
	ctx context.Context, logger *slog.Logger, registrations ...connectorapi.Registration,
) (profile.Catalogue, error) {
	var docs []profile.ModelDoc

	for _, registration := range registrations {
		for _, model := range registration.Dialect().Models {
			docs = append(docs, profile.ModelDoc{
				Model:       model.Model,
				DisplayName: model.DisplayName,
				Vendor:      model.Vendor,
				Icon:        profile.IconGeneric,
				Connector:   registration.Name().String(),
				DeclaredBy:  registration.Qualified().String(),
				ToolChanger: model.ToolChanger,
				Artwork:     model.Artwork,
			})
		}
	}

	catalogue, err := profile.Place(docs...)
	if err != nil {
		return profile.Catalogue{}, fmt.Errorf("place the device models the drivers declared: %w", err)
	}

	for _, refusal := range catalogue.Refused() {
		logger.ErrorContext(ctx, "a declared device model was refused",
			"model", refusal.Model,
			"type", refusal.DeclaredBy,
			"collides_with", refusal.CollidesWith(),
			"remedy", refusal.Remedy())
	}

	return catalogue, nil
}
