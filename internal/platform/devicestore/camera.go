package devicestore

import (
	"context"
	"database/sql"
	"encoding/json"
)

const (
	selectCamerasSQL = `SELECT device_id, type, config, generation FROM camera ORDER BY device_id`

	upsertCameraSQL = `INSERT INTO camera (device_id, type, config, generation) VALUES (?, ?, ?, ?)
		ON CONFLICT (device_id) DO UPDATE SET
			type = excluded.type, config = excluded.config, generation = excluded.generation`

	// A camera device an older build declared from a file and no camera row
	// carries (ADR-20261116-g5x).
	selectBareCamerasSQL = `SELECT device_id FROM authz_device
		WHERE kind = 'camera' AND device_id NOT IN (SELECT device_id FROM camera) ORDER BY device_id`
)

// Camera is one camera a person added. Its device row in authz_device is its
// identity and its grants; this is what it runs.
type Camera struct {
	ID         string
	Type       string
	Config     json.RawMessage
	Generation int64
}

// Cameras is every camera, by id.
func (s *Store) Cameras(ctx context.Context) ([]Camera, error) {
	rows, err := s.db.QueryContext(ctx, s.q(selectCamerasSQL))
	if err != nil {
		return nil, failure("list the cameras", err)
	}
	defer rows.Close()

	out := []Camera{}

	for rows.Next() {
		var (
			camera Camera
			config []byte
		)

		if err = rows.Scan(&camera.ID, &camera.Type, &config, &camera.Generation); err != nil {
			return nil, failure("read a camera", err)
		}

		camera.Config = config
		out = append(out, camera)
	}

	if err = rows.Err(); err != nil {
		return nil, failure("list the cameras", err)
	}

	return out, nil
}

// PutCamera writes one camera inside tx, which is the transaction that writes
// its device row: the foreign key needs the device first.
func (s *Store) PutCamera(ctx context.Context, tx *sql.Tx, camera Camera) error {
	config := string(camera.Config)
	if config == "" {
		config = "{}"
	}

	if _, err := tx.ExecContext(ctx, s.q(upsertCameraSQL),
		camera.ID, camera.Type, config, camera.Generation); err != nil {
		return failure("write camera "+camera.ID, err)
	}

	return nil
}

// BareCameras is every camera device with no camera row.
func (s *Store) BareCameras(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, s.q(selectBareCamerasSQL))
	if err != nil {
		return nil, failure("list the camera devices", err)
	}
	defer rows.Close()

	var out []string

	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, failure("read a camera device", err)
		}

		out = append(out, id)
	}

	if err = rows.Err(); err != nil {
		return nil, failure("list the camera devices", err)
	}

	return out, nil
}
