package compose

import (
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// recoveryFailed names the recovery step that failed. Both steps read the
// configuration file first, so a typo in it fails there. That error goes out
// alone: no restore or upgrade was involved, and naming one would send the
// operator to look for it.
func recoveryFailed(step string, err error) error {
	if errors.Is(err, config.ErrInvalid) {
		return err
	}

	return fmt.Errorf("%s: %w", step, err)
}
