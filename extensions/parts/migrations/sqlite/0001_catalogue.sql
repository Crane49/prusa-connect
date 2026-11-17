-- The catalogue: folders, parts, their tags, versions, the files of each
-- version, and comments. A time is an RFC 3339 text in UTC.

CREATE TABLE id_counter (
    name TEXT NOT NULL PRIMARY KEY,
    next INTEGER NOT NULL
);

INSERT INTO id_counter (name, next) VALUES ('folder', 1), ('part', 1), ('comment', 1);

CREATE TABLE folder (
    id          INTEGER NOT NULL PRIMARY KEY,
    parent      INTEGER NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created     TEXT NOT NULL,
    changed     TEXT NOT NULL,
    changed_by  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE part (
    id           INTEGER NOT NULL PRIMARY KEY,
    folder       INTEGER NOT NULL,
    name         TEXT NOT NULL,
    summary      TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    cover        TEXT NOT NULL DEFAULT '',
    last_version INTEGER NOT NULL DEFAULT 0,
    revision     INTEGER NOT NULL DEFAULT 1,
    created      TEXT NOT NULL,
    created_by   TEXT NOT NULL DEFAULT '',
    changed      TEXT NOT NULL,
    changed_by   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE part_tag (
    part_id INTEGER NOT NULL REFERENCES part (id) ON DELETE CASCADE,
    tag     TEXT NOT NULL,
    PRIMARY KEY (part_id, tag)
);

CREATE TABLE part_version (
    part_id    INTEGER NOT NULL REFERENCES part (id) ON DELETE CASCADE,
    number     INTEGER NOT NULL,
    note       TEXT NOT NULL DEFAULT '',
    created    TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT '',
    changed    TEXT NOT NULL,
    changed_by TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (part_id, number)
);

CREATE TABLE version_file (
    part_id  INTEGER NOT NULL,
    number   INTEGER NOT NULL,
    position INTEGER NOT NULL,
    name     TEXT NOT NULL,
    storage  TEXT NOT NULL,
    path     TEXT NOT NULL,
    size     INTEGER NOT NULL,
    sha256   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (part_id, number, position),
    FOREIGN KEY (part_id, number) REFERENCES part_version (part_id, number) ON DELETE CASCADE
);

CREATE TABLE part_comment (
    id      INTEGER NOT NULL PRIMARY KEY,
    part_id INTEGER NOT NULL REFERENCES part (id) ON DELETE CASCADE,
    body    TEXT NOT NULL,
    author  TEXT NOT NULL,
    created TEXT NOT NULL,
    edited  TEXT
);

CREATE INDEX part_comment_part ON part_comment (part_id);

-- One row once the records an earlier release kept in the server's record
-- store were read into the tables above, or once there were none to read.
CREATE TABLE record_import (
    id       INTEGER NOT NULL PRIMARY KEY CHECK (id = 1),
    finished TEXT NOT NULL,
    moved    INTEGER NOT NULL
);
