// Package instanceid keeps the id this server answers to.
//
// A client finds the server at an address, and the address can change: a new
// lease, a second network, a move to another host. The id does not change. It
// is written once into the host's own database, on the first start, so every
// process that opens that database reads the same id, and a backup of the
// database carries it.
//
// The id is random. It is derived from nothing, so it tells a reader nothing
// about the machine, the network or any secret, and it is safe to send to an
// unauthenticated caller and over mDNS.
package instanceid

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

const (
	// insertSQL writes the row unless one is there. Two processes that start
	// at once against one database both run it, and the second one changes
	// nothing.
	insertSQL = `INSERT INTO server_instance (slot, instance_id, created_at) VALUES (1, ?, ?)
		ON CONFLICT (slot) DO NOTHING`
	selectSQL = `SELECT instance_id FROM server_instance WHERE slot = 1`
)

// Ensure returns the id of the server whose database db is, and writes a new
// one first when the database holds none. The schema is the host's and is
// already migrated.
func Ensure(ctx context.Context, db *sql.DB, engine sqlstore.Engine, now time.Time) (string, error) {
	if db == nil {
		return "", errors.New("instanceid: no database handle")
	}

	if err := engine.Check(); err != nil {
		return "", err
	}

	if _, err := db.ExecContext(ctx, engine.Q(insertSQL), uuid.NewString(), now.UTC()); err != nil {
		return "", fmt.Errorf("write the instance id: %w", err)
	}

	var id string
	if err := db.QueryRowContext(ctx, engine.Q(selectSQL)).Scan(&id); err != nil {
		return "", fmt.Errorf("read the instance id: %w", err)
	}

	return id, nil
}
