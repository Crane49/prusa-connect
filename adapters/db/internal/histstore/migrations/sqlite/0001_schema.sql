-- The history store's tables. Research 43 section 3.1 is the shape and section
-- 3.2 is the measurement behind it.
--
-- This file lives in a database of its own and is never ATTACHed to the host's.
-- SQLite has one writer per WAL file, and sample inserts flooding beside the
-- graph put a small authorization write's p99 at the five second busy timeout
-- (research 43 section 3.5).

-- An instant is Unix microseconds, the resolution sdk/historyapi.Precision
-- states. It is an integer rather than a timestamp because the two engines
-- disagree about what a timestamp column stores, and a row is compared,
-- bucketed, paged and expired by that number on both of them. A millisecond
-- cannot separate two readings of a printer reporting at 20 Hz.
--
-- history_series exists so that history_sample can stay narrow: one row per
-- (ref, metric) pair the fleet has produced, which is 2,400 rows at 400
-- printers reporting six metrics. Repeating the rendered reference on every
-- sample row measured more than twice the row width (research 43 section 3.2).

CREATE TABLE history_series (
  series_id INTEGER PRIMARY KEY,
  ref       TEXT NOT NULL,
  metric    TEXT NOT NULL,
  UNIQUE (ref, metric)
);

-- A reading is a number or a word, and exactly one of the two: a row holding 1
-- and "PRINTING" at once is two answers to what the series read at that
-- instant. text_value and not text, because text is a type name on both
-- engines.
CREATE TABLE history_sample (
  series_id  INTEGER NOT NULL REFERENCES history_series(series_id),
  at_us      INTEGER NOT NULL,
  value      REAL,
  text_value TEXT,
  CHECK ((value IS NULL) <> (text_value IS NULL))
);

-- UNIQUE because two values of one metric at one instant is a contradiction
-- rather than two measurements, and the pair is the ordering key a page resumes
-- from: without uniqueness a page boundary between two rows sharing it drops
-- one of them. It also makes an append idempotent on replay.
CREATE UNIQUE INDEX history_sample_series_at ON history_sample (series_id, at_us);

-- The primary key is the dedup key the envelope defines: sequence is scoped to
-- the source, and epoch changes when the owning node's store is recreated. A
-- replayed event collides on insert, which makes an append idempotent.
--
-- class is the frame's class, kept so a query can exclude audit rows (ADR-0045
-- rule 13). It is not recoverable from the type string for a type this build
-- has never heard of, so it is a column and not a lookup.
CREATE TABLE history_fact (
  ref      TEXT NOT NULL,
  epoch    TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  at_us    INTEGER NOT NULL,
  type     TEXT NOT NULL,
  class    TEXT NOT NULL,
  payload  TEXT NOT NULL CHECK (json_valid(payload)),
  PRIMARY KEY (ref, epoch, sequence)
);

CREATE INDEX history_fact_ref_at ON history_fact (ref, at_us);

-- A window this store did not see whole. The hub drops the oldest events of a
-- reader that cannot keep up and tells it continuity is gone, and a store that
-- wrote nothing down then would answer a later query with a quiet hole instead
-- of an incomplete window. resumed_at is the offset the reader carried on from
-- inside epoch, so a second loss in one generation is a second row and a replay
-- of the same loss is not.
CREATE TABLE history_gap (
  ref        TEXT NOT NULL,
  epoch      TEXT NOT NULL,
  resumed_at INTEGER NOT NULL,
  at_us      INTEGER NOT NULL,
  PRIMARY KEY (ref, epoch, resumed_at)
);

CREATE INDEX history_gap_ref_at ON history_gap (ref, at_us);

-- The mutable command row, which is this adapter's projection and not part of
-- the contract (ADR-0045 rule 14). The core publishes append-only transitions
-- per command and folds nothing; this row is derived from them, keyed by the
-- host-minted command_id and driven by event type.
--
-- The fold is idempotent and order-tolerant: requested fills the left half,
-- settled the right, and neither overwrites the other's columns, so a replay in
-- any order converges. The audit columns, authorized_us, auth_type and
-- auth_id, are the audit transition. A caller without audit sight of the origin
-- is handed the row with them empty (internal/app/historyread.go).
CREATE TABLE history_command (
  command_id      TEXT PRIMARY KEY,
  ref             TEXT NOT NULL,
  verb            TEXT,
  key             TEXT,
  requested_us    INTEGER,
  args            TEXT,
  args_digest     TEXT,
  settled_us      INTEGER,
  status          TEXT,
  ok              INTEGER,
  outcome_unknown INTEGER,
  machine_reason  TEXT,
  fault_code      TEXT,
  elapsed_ms      INTEGER,
  authorized_us   INTEGER,
  auth_type       TEXT,
  auth_id         TEXT
);

CREATE INDEX history_command_ref_at ON history_command (ref, requested_us);

-- The start-up sweep asks which invocations have a beginning and no end, once
-- per process, and a partial index answers it without reading the commands
-- that ended.
CREATE INDEX history_command_open ON history_command (requested_us)
  WHERE settled_us IS NULL;

-- The mutable transfer row, this adapter's projection of the two durable
-- transitions per transfer, keyed by the host-minted transfer_id. The fold is
-- idempotent and order-tolerant as the command fold is: a started frame that
-- lands after the settled one finds ended_ms written and leaves the state
-- alone. The progress frame is not folded: it is telemetry, superseded by the
-- next one.
--
-- started_ms, ended_ms and elapsed_ms are Unix milliseconds.
CREATE TABLE history_transfer (
  transfer_id TEXT PRIMARY KEY,
  ref         TEXT NOT NULL,
  storage     TEXT,
  source      TEXT,
  destination TEXT,
  bytes_total INTEGER,
  bytes_sent  INTEGER,
  state       TEXT,
  reason      TEXT,
  fault_code  TEXT,
  started_ms  INTEGER,
  ended_ms    INTEGER,
  elapsed_ms  INTEGER
);

CREATE INDEX history_transfer_ref_at ON history_transfer (ref, started_ms);
