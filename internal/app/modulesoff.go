package app

import (
	"context"
	"log/slog"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// modulesRefused is the one check every install, every catalogue read and the
// marketplace switch make first. While the operator has switched modules off
// it answers MODULES_DISABLED, and in safe mode SAFE_MODE, before anything is
// read, downloaded or written.
func modulesRefused(core config.Core) error {
	if core.ModulesOn() {
		return nil
	}

	return httpfault.ModulesHeld(core.SafeMode)
}

// logModulesOff says once at start that installed modules are off, and names
// the modules that therefore do not start. In safe mode the line says so,
// names the declared modules too, and says how to leave it.
//
// The server does not delete them: that cannot be undone and takes their
// records too. The line names each one as `module remove` takes it, and says
// how to start them again.
func logModulesOff(ctx context.Context, logger *slog.Logger, core config.Core, store *resource.Store) {
	if core.ModulesOn() {
		return
	}

	var held []string

	// A module switched off in the overlay would not start with modules on
	// either, so naming it would make the start advice wrong.
	for _, declared := range store.Document().Instances {
		if store.Held(declared) && declared.Spec.Enabled {
			held = append(held, declared.Ref.String())
		}
	}

	const remove = "prusa-connect module remove <kind:name>, or Remove on the Extensions page"

	if core.SafeMode {
		logger.WarnContext(ctx, "the server runs in safe mode, so no module starts",
			"modules", held, "remove", remove, "leave", safeModeLeave)

		return
	}

	const start = "set core.modules_enabled to true, or delete the line, and restart the server"

	if len(held) == 0 {
		logger.InfoContext(ctx, "installed modules are switched off, because core.modules_enabled is false",
			"install", start)

		return
	}

	logger.WarnContext(ctx, "installed modules are not started, because core.modules_enabled is false",
		"modules", held,
		"remove", remove,
		"start", start)
}
