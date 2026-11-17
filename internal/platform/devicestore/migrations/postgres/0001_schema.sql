-- The device records, the prints and the registrations. The records and the
-- prints reference the device directory, authz_device, which internal/authz
-- owns and the host migrates first.

-- What a restart must not forget about a device: the facts admission learned,
-- the latest state, and one JSON document for the rest of the state tree.
--
-- authz_device stays the one answer to which devices exist. device_id is a
-- foreign key onto the directory, so a record can never name a device the
-- directory does not hold, and ON DELETE CASCADE takes the record away with the
-- directory row. An operator-chosen id reused after a replacement then inherits
-- no state, as it inherits no grant. The kind lives on the directory row only.
--
-- The documents are JSONB here and TEXT on SQLite. The CHECKs hold the shape
-- everything that reads these columns unmarshals: an object or an array.
CREATE TABLE device_record (
    device_id      TEXT NOT NULL PRIMARY KEY
                   REFERENCES authz_device (device_id) ON DELETE CASCADE,

    -- Record columns: what admission learned, changed only by another
    -- admission of the same machine.
    driver         TEXT NOT NULL DEFAULT '',
    vendor         TEXT NOT NULL DEFAULT '',
    model          TEXT NOT NULL DEFAULT '',
    serial         TEXT NOT NULL DEFAULT '',
    unit_uid       TEXT NOT NULL DEFAULT '',

    -- Latest state: overwritten by the machine. departure is why the last
    -- session ended, and last_seen is when the machine was last heard from.
    -- Whether it is connected now is never stored: the process that opens this
    -- database holds no socket.
    firmware       TEXT NOT NULL DEFAULT '',
    departure      TEXT NOT NULL DEFAULT '',
    last_seen      TIMESTAMPTZ NOT NULL,

    state_document JSONB NOT NULL DEFAULT '{}'
                   CHECK (jsonb_typeof(state_document) = 'object'),

    -- The machine's own word for what it is doing, and the print it is on, as
    -- printer_job names it. Empty when it is on none.
    state          TEXT NOT NULL DEFAULT '',
    current_job_id TEXT NOT NULL DEFAULT '',

    -- A retired device id is not this machine's any more, and is kept whole
    -- rather than deleted or merged. retired_at is NULL for a device in use.
    retired_reason TEXT NOT NULL DEFAULT '',
    retired_at     TIMESTAMPTZ,

    -- The name an operator gave the device, and the tags an operator put on it.
    name           TEXT NOT NULL DEFAULT '',
    tags           JSONB NOT NULL DEFAULT '[]'
                   CHECK (jsonb_typeof(tags) = 'array')
);

-- One row per print. printer_job_id is a version 7 UUID the record flush mints
-- once per print, so prints sort by when this server first recorded them.
-- device_job_id is the number the machine issued, as text. It is not unique: a
-- machine reuses its numbers after a firmware reset and when its counter wraps.
-- A print belongs to a device in the directory, and goes with it.
CREATE TABLE printer_job (
    printer_job_id TEXT NOT NULL PRIMARY KEY,

    device_id      TEXT NOT NULL
                   REFERENCES authz_device (device_id) ON DELETE CASCADE,

    -- What is being printed. The path is the machine's own, and display_name is
    -- what the firmware puts on its screen for it, which is not always the last
    -- segment of the path.
    path           TEXT NOT NULL DEFAULT '',
    display_name   TEXT NOT NULL DEFAULT '',

    -- The latest reading. state is the printer's word for what the job is
    -- doing, progress is whole percent as the firmware reports it, and
    -- time_remaining is the seconds it thinks are left.
    state          TEXT NOT NULL DEFAULT '',
    progress       DOUBLE PRECISION NOT NULL DEFAULT 0,
    time_remaining BIGINT NOT NULL DEFAULT 0,

    started        TIMESTAMPTZ NOT NULL,
    ended          TIMESTAMPTZ,

    device_job_id  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX printer_job_by_device ON printer_job (device_id, started);

-- Device registrations, so a printer somebody approved is still approved after
-- a restart. The connector is the only writer, through the host's admission
-- store.
CREATE TABLE device_registration (
    connector    TEXT NOT NULL,
    code         TEXT NOT NULL,
    -- The identity the machine keeps across reconnections, and what an
    -- approved allowlist is keyed by.
    fingerprint  TEXT NOT NULL,
    -- The SHA-256 of the token the driver issued, hex encoded, and never the
    -- token. A row an attacker reads must not be a credential they can present,
    -- and the driver only ever needs to answer whether a presented token is the
    -- one it issued. A registration whose plaintext token this process never
    -- held is re-issued a token when the machine next polls its code, which is
    -- the same handshake step the machine already makes.
    token_hash   TEXT NOT NULL DEFAULT '',
    -- What the machine said about itself, each in its own spelling and each
    -- empty for a handshake that does not carry it.
    serial       TEXT NOT NULL DEFAULT '',
    model        TEXT NOT NULL DEFAULT '',
    firmware     TEXT NOT NULL DEFAULT '',
    -- The device kind the connector registers its machines under, so a listing
    -- read before anything dials still says what is waiting.
    kind         TEXT NOT NULL DEFAULT '',
    -- Whether anything has let this device through, and whether a person did.
    -- Auto-approval sets the first and not the second: nobody decided anything
    -- there, and an entry a person endorsed is one the table never drops to
    -- make room.
    approved     BOOLEAN NOT NULL DEFAULT FALSE,
    endorsed     BOOLEAN NOT NULL DEFAULT FALSE,
    -- Whether the machine collected its token. It is the last step of the
    -- handshake and the only evidence the table holds that the caller behind a
    -- row is a machine rather than anything that opened a socket.
    completed    BOOLEAN NOT NULL DEFAULT FALSE,
    -- Who approved it and when, empty and NULL for one nobody has decided. It
    -- is the provenance an operator asks for after the fact: which of us let
    -- this printer onto the farm.
    admitted_by  TEXT NOT NULL DEFAULT '',
    admitted_at  TIMESTAMPTZ,
    -- When the code was issued, and when it stops being usable. Expiry applies
    -- to a registration nobody has approved: a code an operator never typed is
    -- a claim on the table and on the code space, and one left there for ever
    -- is a code somebody may still guess. An approved registration carries no
    -- expiry, because it is the credential the machine dials with.
    issued_at    TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ,
    -- The order the machines registered in, which is what decides who is
    -- dropped when the table is full. It is a counter and not a clock, so two
    -- registrations in one millisecond are still ordered.
    seq          BIGINT NOT NULL,
    PRIMARY KEY (connector, code)
);

-- A machine is one registration per connector, whatever it does with its code.
-- The driver looks a returning machine up by fingerprint and hands it the code
-- it already has, rather than minting a second one an operator would then have
-- to choose between.
CREATE UNIQUE INDEX device_registration_by_fingerprint
    ON device_registration (connector, fingerprint);

-- The lookup the websocket handshake makes on every dial.
CREATE INDEX device_registration_by_token
    ON device_registration (token_hash);
