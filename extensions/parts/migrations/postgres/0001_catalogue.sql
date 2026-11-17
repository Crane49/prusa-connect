-- The catalogue: folders, parts, their tags, versions, the files of each
-- version, and comments.

CREATE TABLE id_counter (
    name TEXT NOT NULL PRIMARY KEY,
    next BIGINT NOT NULL
);

INSERT INTO id_counter (name, next) VALUES ('folder', 1), ('part', 1), ('comment', 1);

CREATE TABLE folder (
    id          BIGINT NOT NULL PRIMARY KEY,
    parent      BIGINT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created     TIMESTAMPTZ NOT NULL,
    changed     TIMESTAMPTZ NOT NULL,
    changed_by  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE part (
    id           BIGINT NOT NULL PRIMARY KEY,
    folder       BIGINT NOT NULL,
    name         TEXT NOT NULL,
    summary      TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    cover        TEXT NOT NULL DEFAULT '',
    last_version BIGINT NOT NULL DEFAULT 0,
    revision     BIGINT NOT NULL DEFAULT 1,
    created      TIMESTAMPTZ NOT NULL,
    created_by   TEXT NOT NULL DEFAULT '',
    changed      TIMESTAMPTZ NOT NULL,
    changed_by   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE part_tag (
    part_id BIGINT NOT NULL REFERENCES part (id) ON DELETE CASCADE,
    tag     TEXT NOT NULL,
    PRIMARY KEY (part_id, tag)
);

CREATE TABLE part_version (
    part_id    BIGINT NOT NULL REFERENCES part (id) ON DELETE CASCADE,
    number     BIGINT NOT NULL,
    note       TEXT NOT NULL DEFAULT '',
    created    TIMESTAMPTZ NOT NULL,
    created_by TEXT NOT NULL DEFAULT '',
    changed    TIMESTAMPTZ NOT NULL,
    changed_by TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (part_id, number)
);

CREATE TABLE version_file (
    part_id  BIGINT NOT NULL,
    number   BIGINT NOT NULL,
    position BIGINT NOT NULL,
    name     TEXT NOT NULL,
    storage  TEXT NOT NULL,
    path     TEXT NOT NULL,
    size     BIGINT NOT NULL,
    sha256   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (part_id, number, position),
    FOREIGN KEY (part_id, number) REFERENCES part_version (part_id, number) ON DELETE CASCADE
);

CREATE TABLE part_comment (
    id      BIGINT NOT NULL PRIMARY KEY,
    part_id BIGINT NOT NULL REFERENCES part (id) ON DELETE CASCADE,
    body    TEXT NOT NULL,
    author  TEXT NOT NULL,
    created TIMESTAMPTZ NOT NULL,
    edited  TIMESTAMPTZ
);

CREATE INDEX part_comment_part ON part_comment (part_id);

-- One row once the records an earlier release kept in the server's record
-- store were read into the tables above, or once there were none to read.
CREATE TABLE record_import (
    id       INTEGER NOT NULL PRIMARY KEY CHECK (id = 1),
    finished TIMESTAMPTZ NOT NULL,
    moved    BIGINT NOT NULL
);
