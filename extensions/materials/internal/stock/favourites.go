package stock

import (
	"context"
	"fmt"
	"time"
)

// Star marks or unmarks a material as a favourite of principal. A catalogue
// material, the principal's own custom material, or a material a live spool
// still holds may be starred: each is a row of principal's list.
func (s *Store) Star(ctx context.Context, principal, materialUUID string, on bool) error {
	if !on {
		_, err := s.db.ExecContext(
			ctx,
			`DELETE FROM favourites WHERE principal=$1 AND material_uuid=$2`,
			principal,
			materialUUID,
		)

		return err
	}

	// Integer counts rather than EXISTS, so both engines scan them alike.
	var catalogue, custom, held int
	if err := s.db.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM materials WHERE uuid=$1),
			(SELECT COUNT(*) FROM custom_materials WHERE uuid=$1 AND owner=$2),
			(SELECT COUNT(*) FROM spools WHERE material_uuid=$1 AND deleted_at IS NULL)`,
		materialUUID, principal).Scan(&catalogue, &custom, &held); err != nil {
		return fmt.Errorf("find material: %w", err)
	}

	if catalogue+custom+held == 0 {
		return ErrNotFound
	}

	_, err := s.db.ExecContext(ctx, `INSERT INTO favourites (principal, material_uuid, starred_at)
		VALUES ($1, $2, $3) ON CONFLICT (principal, material_uuid) DO NOTHING`,
		principal, materialUUID, s.at(time.Now()))

	return err
}
