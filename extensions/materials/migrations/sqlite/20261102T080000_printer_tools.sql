-- Spools on printer tools.

-- Every spool gets an identity of its own and a note of where it came from,
-- so a spool a printer reports can be matched by the uuid of its tag.
-- consumed_weight_g is the grams its printer tools fed off it in all. A spool
-- taken out of stock without being deleted is archived: archived_at is when,
-- NULL for a spool in stock, and only an archived spool has a reason and a
-- note, both optional.
ALTER TABLE spools ADD COLUMN uuid TEXT NOT NULL DEFAULT '';
ALTER TABLE spools ADD COLUMN source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'printer'));
ALTER TABLE spools ADD COLUMN consumed_weight_g REAL NOT NULL DEFAULT 0;
ALTER TABLE spools ADD COLUMN archived_at TEXT NULL;
ALTER TABLE spools ADD COLUMN archive_reason TEXT NULL
    CHECK (archive_reason IN ('consumed', 'defective', 'lost', 'expired', 'damaged', 'other'));
ALTER TABLE spools ADD COLUMN archive_reason_note TEXT NULL;

-- SQLite has no uuid function. This builds a random version 4 uuid, one for each row.
UPDATE spools SET uuid = lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' ||
    substr(lower(hex(randomblob(2))), 2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) ||
    substr(lower(hex(randomblob(2))), 2) || '-' || lower(hex(randomblob(6)));

CREATE UNIQUE INDEX spools_uuid ON spools (uuid);

-- What is on each printer tool: a material, and a spool of it when one was
-- picked. A person sets it, or a printer reports it. The material fields are
-- copied, as a spool copies them, so a tool keeps its name when the material
-- goes. reported is the last instance the printer reported, as JSON.
-- odometer_e and odometer_id are the last reading of the tool's odometer, so
-- a restart does not lose the filament fed in between.
CREATE TABLE assignments (
    device_id       TEXT NOT NULL,
    tool_id         TEXT NOT NULL,
    material_uuid   TEXT NOT NULL DEFAULT '',
    material_source TEXT NOT NULL DEFAULT '',
    material_name   TEXT NOT NULL DEFAULT '',
    brand_name      TEXT NOT NULL DEFAULT '',
    type            TEXT NOT NULL DEFAULT '',
    abbreviation    TEXT NOT NULL DEFAULT '',
    color_rgba      TEXT NOT NULL DEFAULT '',
    spool_id        INTEGER NULL REFERENCES spools (id),
    assigned_by     TEXT NOT NULL CHECK (assigned_by IN ('person', 'printer')),
    reported        TEXT NULL,
    updated_at      TEXT NOT NULL,
    odometer_e      REAL NULL,
    odometer_id     INTEGER NULL,
    PRIMARY KEY (device_id, tool_id)
);

-- A spool is on one tool at most.
CREATE UNIQUE INDEX assignments_spool ON assignments (spool_id) WHERE spool_id IS NOT NULL;
