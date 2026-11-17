package files

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// fileMode is the permission of the files this package writes. Only the
// module's own user reads them.
const fileMode = 0o600

// writeAtomically writes value as JSON to a temporary file beside file and
// renames it over file, so a crash leaves the old content or the new one and
// never a part of either.
func writeAtomically(file string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(file), err)
	}

	temp, err := os.CreateTemp(filepath.Dir(file), filepath.Base(file)+".*")
	if err != nil {
		return fmt.Errorf("create a temporary file for %s: %w", filepath.Base(file), err)
	}

	defer func() { _ = os.Remove(temp.Name()) }() // fails once the rename moved it, which is the aim

	if _, err = temp.Write(data); err == nil {
		err = temp.Sync()
	}

	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}

	if err == nil {
		err = os.Chmod(temp.Name(), fileMode)
	}

	if err == nil {
		err = os.Rename(temp.Name(), file)
	}

	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(file), err)
	}

	return nil
}
