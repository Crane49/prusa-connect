package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
)

// accountFileVersion is the document's shape. A file with another version is
// refused and not skipped. A server that skipped it would report a first run
// to whoever asked, and that reopens the setup path.
const accountFileVersion = 1

// ErrAccountFile reports an account file this build cannot read.
var ErrAccountFile = errors.New("app: the account file cannot be read")

// The accounts are rows in the host's database, beside the grants written for
// them (see internal/platform/auth/dbstore). A deployment that ran an earlier
// build has its accounts in a file, and this code imports that file so its
// operators keep their accounts.
//
// It is an import and not a fallback. The rows are copied into the database
// once, when the database holds none. After that the server never reads the
// file, and an operator who wants it gone deletes it.

// accountDocument is the file's shape.
type accountDocument struct {
	Version  int             `json:"version"`
	Accounts []accountRecord `json:"accounts"`
}

// accountRecord is one row as it was written. The field names are spelled out
// here and not taken from local.Account. The file outlives the struct, and a
// renamed Go field must not silently drop a password.
type accountRecord struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Hash        string    `json:"password_hash"`
	CreatedAt   time.Time `json:"created_at"`
	PasswordAt  time.Time `json:"password_changed_at"`
}

// importAccountFile copies the accounts an earlier build left in a file into
// the store. It does nothing when there is no file or when the store already
// holds an account.
//
// A store that already holds an account is never touched. The file is older
// than the database, and a row from the file that overwrote one from the
// database would bring back a password somebody changed.
//
// A file that is there and unreadable stops the server. Otherwise the server
// would decide on its own that nobody has an account yet.
func importAccountFile(
	ctx context.Context, path string, into local.Store, logger *slog.Logger,
) error {
	if path == "" {
		return nil
	}

	rows, found, err := readAccountFile(path)
	if err != nil || !found {
		return err
	}

	held, err := into.Count(ctx)
	if err != nil {
		return fmt.Errorf("read the accounts before importing %s: %w", path, err)
	}

	if held > 0 {
		logger.InfoContext(ctx, "an account file is present and was not imported: "+
			"this server already holds accounts of its own", "accounts", path, "held", held)

		return nil
	}

	for _, row := range rows {
		if err = into.Create(ctx, row); err != nil {
			return fmt.Errorf("import the account %q from %s: %w", row.Username, path, err)
		}
	}

	logger.InfoContext(ctx, "the accounts in the account file were imported into the database",
		"accounts", path, "imported", len(rows))

	return nil
}

// readAccountFile reads the document at path. found is false when there is no
// file, which is every deployment that never ran the build that wrote one.
func readAccountFile(path string) ([]local.Account, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("%w: %s: %w", ErrAccountFile, path, err)
	}

	var document accountDocument
	if err = json.Unmarshal(raw, &document); err != nil {
		return nil, false, fmt.Errorf("%w: %s: %w", ErrAccountFile, path, err)
	}

	if document.Version != accountFileVersion {
		return nil, false, fmt.Errorf("%w: %s says version %d and this build reads %d",
			ErrAccountFile, path, document.Version, accountFileVersion)
	}

	out := make([]local.Account, 0, len(document.Accounts))

	for _, record := range document.Accounts {
		if record.ID == "" || record.Username == "" {
			return nil, false, fmt.Errorf("%w: %s holds a row with no id or no username",
				ErrAccountFile, path)
		}

		out = append(out, local.Account{
			ID:          record.ID,
			Username:    record.Username,
			DisplayName: record.DisplayName,
			Hash:        record.Hash,
			CreatedAt:   record.CreatedAt,
			PasswordAt:  record.PasswordAt,
		})
	}

	return out, true, nil
}
