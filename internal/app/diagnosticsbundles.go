package app

import (
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/platform/observability"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// diagnosticsBundles reads every source under the requesting admin's identity
// and hands them to diagnostics.Build.
type diagnosticsBundles struct {
	version string
	started time.Time
	core    config.Core
	// setupCode is the first-run code this process drew because the
	// configuration names none. It is in no file, so the sweep is told here.
	setupCode string
	ring      *observability.LogRing
	crashes   string
	devices   *devices
	store     *resource.Store
	cameras   *cameraSettings
	probes    readiness
	accounts  httpapi.Accounts
	// mode is the first run's access decision, which decides whether an
	// install with no sign-in serves the bundle.
	mode *setupMode
	// sources is the running modules that add files to a bundle, read when
	// one is built, and nil for a server that runs none.
	sources func() []resourcehost.DiagnosticsSource
}
