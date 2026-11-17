package app

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// safeModeFile is the marker a restart into safe mode leaves beside the
// overlay. It is read once and deleted.
const safeModeFile = "safe-mode.json"

// safeModeLeave is how an operator leaves safe mode, for every log line that
// names it.
const safeModeLeave = "Restart normally in the web app, or restart without -safe"

// safeMode is how this run entered safe mode. The zero value is a normal run.
type safeMode struct {
	// Reason is "flag" for -safe and "restart" for the marker.
	Reason string
	// Since is when this run took safe mode.
	Since time.Time
}

// on reports whether this run is in safe mode.
func (s safeMode) on() bool { return !s.Since.IsZero() }

// hold lays safe mode over the configuration: the run behaves as if
// core.modules_enabled were false, and SafeMode widens that to the modules
// the operator declared.
func (s safeMode) hold(core config.Core) config.Core {
	if !s.on() {
		return core
	}

	off := false
	core.ModulesEnabled = &off
	core.SafeMode = true

	return core
}

// safeModeRequest is the marker's content. It names no principal: the
// restart is logged with one when it is asked for.
type safeModeRequest struct {
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// safeModeMarker is the marker's path for the overlay at overlay.
func safeModeMarker(overlay string) string {
	return filepath.Join(filepath.Dir(overlay), safeModeFile)
}

// takeSafeMode decides whether this run is in safe mode, from the flag and the
// marker, and deletes the marker so the next start is normal. A marker it
// cannot read or delete still starts safe mode. The operator asked for it,
// and a normal start might leave them with a server they cannot reach.
func takeSafeMode(ctx context.Context, logger *slog.Logger, overlay string, flag bool, now time.Time) safeMode {
	path := safeModeMarker(overlay)

	data, err := os.ReadFile(path)
	marked := !errors.Is(err, fs.ErrNotExist)

	if !marked && !flag {
		return safeMode{}
	}

	mode := safeMode{Reason: "flag", Since: now}

	if marked {
		mode.Reason = "restart"

		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			logger.ErrorContext(ctx, "the safe-mode marker could not be deleted; delete it yourself, "+
				"or the next start is in safe mode too", "path", path, "error", removeErr)
		}

		var request safeModeRequest
		if err == nil {
			err = json.Unmarshal(data, &request)
		}

		if err != nil {
			logger.WarnContext(ctx, "the safe-mode marker could not be read; starting in safe mode anyway",
				"path", path, "error", err)
		} else {
			logger.InfoContext(ctx, "a restart into safe mode was asked for", "at", request.At)
		}
	}

	logger.WarnContext(ctx, "the server starts in safe mode", "reason", mode.Reason, "leave", safeModeLeave)

	return mode
}

// afterServe is what Run returns once serve has, given why the run stopped.
// A restart the server was asked for goes on when the plane did not stop in
// its budget or the listeners did not drain in theirs. Without that, a module
// that hangs on stop would make a restart impossible. The wedged shutdown has
// killed every module process by then, so the new process starts no second
// copy. Run logs the error beside the restart. An upgrade or a restore goes on
// only after a clean stop, because both touch the database and the files.
func afterServe(cause, err error) error {
	if errors.Is(cause, ErrRestart) && restartsPast(err) {
		return ErrRestart
	}

	if err != nil {
		return err
	}

	for _, wanted := range []error{ErrUpgradeApply, ErrBackupRestore} {
		if errors.Is(cause, wanted) {
			return wanted
		}
	}

	return nil
}

// restartsPast reports whether a serve error leaves a requested restart going:
// none, a wedged plane, or a listener drain that ran out of its budget.
func restartsPast(err error) bool {
	return err == nil || errors.Is(err, ErrPlaneWedged) || errors.Is(err, context.DeadlineExceeded)
}
