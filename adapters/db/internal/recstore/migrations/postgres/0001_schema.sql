-- The key/value store for extensions, which is ADR-0023 rule 3's Records and
-- ADR-0035 rule 2's only door into storage.
--
-- One row is one record in one namespace. The namespace is a column and never
-- an argument a caller passes: sdk/stateapi binds it before an extension runs,
-- and this table is what it is bound against. ADR-0023 rule 3 makes the key
-- order part of the contract: List returns keys in sorted order, by bytes.
--
-- COLLATE "C" is byte order, which is what SQLite's BINARY is. It is pinned on
-- the column rather than written into each ORDER BY, so a statement added later
-- cannot forget it, and the prefix LIKE in List matches the same bytes the
-- primary key is built on. A database created with a locale collation would
-- otherwise answer a different order from every SQLite deployment.
CREATE TABLE module_state (
    namespace    TEXT COLLATE "C" NOT NULL,
    record_key   TEXT COLLATE "C" NOT NULL,
    record_value BYTEA      NOT NULL,
    -- Rises by one on every write and is never reused, so a version a reader
    -- holds names one write and CompareAndPut refuses every other.
    version      BIGINT   NOT NULL,
    -- The key and the value together, which is what the namespace quota counts.
    -- It is stored rather than computed so the quota is one indexed sum and not
    -- a scan that reads every value in the namespace.
    bytes        BIGINT   NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (namespace, record_key)
);
