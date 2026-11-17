//go:build diagnostics

package app

import (
	"context"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// secretValues is what the cameras hold under a credential, for a
// diagnostics bundle to sweep. A server with no cameras built has none.
func (c *cameraSettings) secretValues(ctx context.Context) []string {
	if c == nil {
		return nil
	}

	rows, err := c.rows.Cameras(ctx)
	if err != nil {
		return nil
	}

	var out []string

	for _, row := range rows {
		options, known := c.types.DeclaredOptions(ref.KindCamera,
			resource.DriverType(ref.KindCamera, row.Type, c.plane.CameraTypes()))
		out = append(out, resource.Secrets(options, known, row.Config)...)
	}

	return out
}
