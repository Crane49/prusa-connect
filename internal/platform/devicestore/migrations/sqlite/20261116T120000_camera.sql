-- A camera a person added: the type it runs and its settings. The device row
-- is its identity and its grants, and the camera goes with it
-- (ADR-20261116-g5x). generation counts accepted changes, and is the version a
-- write is fenced on.
CREATE TABLE camera (
    device_id  TEXT NOT NULL PRIMARY KEY
               REFERENCES authz_device (device_id) ON DELETE CASCADE,
    type       TEXT NOT NULL,
    config     TEXT NOT NULL DEFAULT '{}'
               CHECK (json_valid(config) AND json_type(config) = 'object'),
    generation INTEGER NOT NULL DEFAULT 1
);
