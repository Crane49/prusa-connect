package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/atomicfile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

// Restart records whether the next run is in safe mode, then asks for the
// restart. A normal restart that cannot remove the marker is refused: the
// server would start in safe mode again after the operator asked to leave it.
// A restart is also refused while an upgrade is scheduled or has begun,
// because the restart would interrupt it.
func (m *maintenance) Restart(ctx context.Context, input httpapi.MaintenanceRestart) error {
	if m.requestRestart == nil {
		return errors.New("restart is not available")
	}

	// A server with upgrades off runs no upgrade, so a restart cannot interrupt
	// one, and a journal an earlier run left is not read.
	if !m.upgradesOff() {
		if journal, err := m.store.Load(); err == nil &&
			(journal.State == upgrade.StateScheduled || journal.Interrupted()) {
			return fmt.Errorf("%w: transaction %s is %s", upgrade.ErrUpgradeBusy, journal.ID, journal.State)
		}
	}

	marker := safeModeMarker(m.core.Overlay)

	switch input.Mode {
	case httpapi.RestartSafe:
		data, err := json.Marshal(safeModeRequest{Reason: "restart", At: time.Now().UTC()})
		if err != nil {
			return fmt.Errorf("encode the safe-mode marker: %w", err)
		}
		if err = os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
			return fmt.Errorf("create the safe-mode marker folder: %w", err)
		}
		if err = atomicfile.Write(marker, data, privateFileMode); err != nil {
			return fmt.Errorf("write the safe-mode marker: %w", err)
		}
	case httpapi.RestartNormal:
		if err := os.Remove(marker); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove the safe-mode marker: %w", err)
		}
	default:
		return fmt.Errorf("unknown restart mode %q", input.Mode)
	}

	m.logger.InfoContext(ctx, "the server restarts", "mode", input.Mode, "by", input.Principal)
	m.requestRestart()

	return nil
}
