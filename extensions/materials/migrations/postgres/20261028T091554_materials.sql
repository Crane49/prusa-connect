-- The catalogue read from OpenPrintTag, the owner's custom materials and
-- favourites, and the spools in stock.

CREATE TABLE brands (
    uuid TEXT NOT NULL PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE containers (
    uuid           TEXT NOT NULL PRIMARY KEY,
    slug           TEXT NOT NULL UNIQUE,
    brand_slug     TEXT NOT NULL DEFAULT '',
    name           TEXT NOT NULL DEFAULT '',
    empty_weight_g DOUBLE PRECISION NULL
);

CREATE TABLE materials (
    uuid                  TEXT NOT NULL PRIMARY KEY,
    slug                  TEXT NOT NULL UNIQUE,
    brand_slug            TEXT NOT NULL DEFAULT '',
    name                  TEXT NOT NULL DEFAULT '',
    class                 TEXT NOT NULL DEFAULT '',
    type                  TEXT NOT NULL DEFAULT '',
    abbreviation          TEXT NOT NULL DEFAULT '',
    color_rgba            TEXT NOT NULL DEFAULT '',
    color_family         TEXT NOT NULL DEFAULT '',
    density               DOUBLE PRECISION NULL,
    hardness_shore_d      DOUBLE PRECISION NULL,
    min_print_temp        INTEGER NULL,
    max_print_temp        INTEGER NULL,
    preheat_temp          INTEGER NULL,
    min_bed_temp          INTEGER NULL,
    max_bed_temp          INTEGER NULL,
    chamber_temp          INTEGER NULL,
    min_chamber_temp      INTEGER NULL,
    max_chamber_temp      INTEGER NULL,
    drying_temp           INTEGER NULL,
    drying_time_min       INTEGER NULL,
    transmission_distance DOUBLE PRECISION NULL,
    -- OpenPrintTag tags, comma-separated in the catalogue's order. A tag is a
    -- lower-case identifier, so it holds no comma.
    tags                  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX materials_brand ON materials (brand_slug);
CREATE INDEX materials_type ON materials (type);
CREATE INDEX materials_color_family ON materials (color_family);

CREATE TABLE packages (
    uuid           TEXT NOT NULL PRIMARY KEY,
    slug           TEXT NOT NULL UNIQUE,
    class          TEXT NOT NULL DEFAULT '',
    material_slug  TEXT NOT NULL,
    container_slug TEXT NULL,
    net_weight_g   DOUBLE PRECISION NULL,
    diameter_um    INTEGER NULL,
    length_mm      INTEGER NULL,
    gtin           TEXT NULL
);

CREATE INDEX packages_material ON packages (material_slug);

-- An import fills these in short batches, then swaps them into the four
-- tables above in one short transaction, so it never holds the database long.
CREATE TABLE brands_next (
    uuid TEXT NOT NULL PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE containers_next (
    uuid           TEXT NOT NULL PRIMARY KEY,
    slug           TEXT NOT NULL UNIQUE,
    brand_slug     TEXT NOT NULL DEFAULT '',
    name           TEXT NOT NULL DEFAULT '',
    empty_weight_g DOUBLE PRECISION NULL
);

CREATE TABLE materials_next (
    uuid                  TEXT NOT NULL PRIMARY KEY,
    slug                  TEXT NOT NULL UNIQUE,
    brand_slug            TEXT NOT NULL DEFAULT '',
    name                  TEXT NOT NULL DEFAULT '',
    class                 TEXT NOT NULL DEFAULT '',
    type                  TEXT NOT NULL DEFAULT '',
    abbreviation          TEXT NOT NULL DEFAULT '',
    color_rgba            TEXT NOT NULL DEFAULT '',
    color_family         TEXT NOT NULL DEFAULT '',
    density               DOUBLE PRECISION NULL,
    hardness_shore_d      DOUBLE PRECISION NULL,
    min_print_temp        INTEGER NULL,
    max_print_temp        INTEGER NULL,
    preheat_temp          INTEGER NULL,
    min_bed_temp          INTEGER NULL,
    max_bed_temp          INTEGER NULL,
    chamber_temp          INTEGER NULL,
    min_chamber_temp      INTEGER NULL,
    max_chamber_temp      INTEGER NULL,
    drying_temp           INTEGER NULL,
    drying_time_min       INTEGER NULL,
    transmission_distance DOUBLE PRECISION NULL,
    -- OpenPrintTag tags, comma-separated in the catalogue's order. A tag is a
    -- lower-case identifier, so it holds no comma.
    tags                  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE packages_next (
    uuid           TEXT NOT NULL PRIMARY KEY,
    slug           TEXT NOT NULL UNIQUE,
    class          TEXT NOT NULL DEFAULT '',
    material_slug  TEXT NOT NULL,
    container_slug TEXT NULL,
    net_weight_g   DOUBLE PRECISION NULL,
    diameter_um    INTEGER NULL,
    length_mm      INTEGER NULL,
    gtin           TEXT NULL
);

-- One row: the state of the last import.
CREATE TABLE catalogue_meta (
    id            INTEGER NOT NULL PRIMARY KEY CHECK (id = 1),
    data_hash     TEXT NOT NULL,
    last_modified TEXT NOT NULL DEFAULT '',
    brands        INTEGER NOT NULL,
    materials     INTEGER NOT NULL,
    packages      INTEGER NOT NULL,
    containers    INTEGER NOT NULL,
    skipped       INTEGER NOT NULL,
    imported_at   TIMESTAMPTZ NOT NULL
);

CREATE TABLE custom_materials (
    uuid             TEXT NOT NULL PRIMARY KEY,
    owner            TEXT NOT NULL,
    name             TEXT NOT NULL,
    brand_name       TEXT NOT NULL DEFAULT '',
    class            TEXT NOT NULL DEFAULT '',
    type             TEXT NOT NULL DEFAULT '',
    abbreviation     TEXT NOT NULL DEFAULT '',
    color_rgba       TEXT NOT NULL DEFAULT '',
    color_family    TEXT NOT NULL DEFAULT '',
    diameter_um      INTEGER NULL,
    density          DOUBLE PRECISION NULL,
    min_print_temp   INTEGER NULL,
    max_print_temp   INTEGER NULL,
    preheat_temp     INTEGER NULL,
    min_bed_temp     INTEGER NULL,
    max_bed_temp     INTEGER NULL,
    chamber_temp     INTEGER NULL,
    min_chamber_temp INTEGER NULL,
    max_chamber_temp INTEGER NULL,
    drying_temp      INTEGER NULL,
    drying_time_min  INTEGER NULL,
    min_nozzle_diameter   DOUBLE PRECISION NULL,
    transmission_distance DOUBLE PRECISION NULL,
    hardness_shore_a      DOUBLE PRECISION NULL,
    hardness_shore_d      DOUBLE PRECISION NULL,
    ral_code              TEXT NOT NULL DEFAULT '',
    -- Up to five #rrggbb[aa] colours, and lower_snake labels, each list
    -- comma-separated: neither holds a comma.
    secondary_colors      TEXT NOT NULL DEFAULT '',
    certifications        TEXT NOT NULL DEFAULT '',
    tags                  TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL
);

CREATE INDEX custom_materials_owner ON custom_materials (owner);

CREATE TABLE favourites (
    principal     TEXT NOT NULL,
    material_uuid TEXT NOT NULL,
    starred_at    TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (principal, material_uuid)
);

-- Where spools are kept, each place once and shared by everyone, so a place
-- is picked rather than typed again and renamed in one go.
CREATE TABLE locations (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX locations_name ON locations (lower(name));

-- A spool keeps a copy of its material's name, colour and the rest, so it
-- reads the same after a catalogue import drops the material.
CREATE TABLE spools (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    material_uuid     TEXT NOT NULL,
    material_source   TEXT NOT NULL,
    material_name     TEXT NOT NULL DEFAULT '',
    brand_name        TEXT NOT NULL DEFAULT '',
    class             TEXT NOT NULL DEFAULT '',
    type              TEXT NOT NULL DEFAULT '',
    color_rgba        TEXT NOT NULL DEFAULT '',
    color_family     TEXT NOT NULL DEFAULT '',
    density           DOUBLE PRECISION NULL,
    diameter_um       INTEGER NULL,
    package_uuid      TEXT NULL,
    full_weight_g     DOUBLE PRECISION NULL,
    empty_weight_g    DOUBLE PRECISION NULL,
    length_mm         INTEGER NULL,
    remaining_weight_g DOUBLE PRECISION NOT NULL,
    condition         TEXT NOT NULL DEFAULT 'new',
    location_id       BIGINT NULL REFERENCES locations (id),
    notes             TEXT NOT NULL DEFAULT '',
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    deleted_at        TIMESTAMPTZ NULL
);

CREATE INDEX spools_material ON spools (material_uuid) WHERE deleted_at IS NULL;
CREATE INDEX spools_location ON spools (location_id) WHERE deleted_at IS NULL;

-- The filters and order each person last gave the materials list, kept as
-- the JSON object the page sends, so the list opens the way they left it.
CREATE TABLE list_filters (
    principal  TEXT NOT NULL PRIMARY KEY,
    filters    TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
