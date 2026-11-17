package app

import (
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// expectedAsks is the expect a request carries, and nil for none.
func expectedAsks(expect *httpapi.ModuleRequests) *install.ModuleAsks {
	if expect == nil {
		return nil
	}

	asked := resource.AsksOf(*expect)

	return &asked
}
