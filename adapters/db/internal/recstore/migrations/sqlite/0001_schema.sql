-- The key/value store for extensions, which is ADR-0023 rule 3's Records and
-- ADR-0035 rule 2's only door into storage.
--
-- One row is one record in one namespace. The namespace is a column and never
-- an argument a caller passes: sdk/stateapi binds it before an extension runs,
-- and this table is what it is bound against. ADR-0023 rule 3 makes the key
-- order part of the contract: List returns keys in sorted order, by bytes.
--
-- NOT NULL beside PRIMARY KEY is load-bearing on SQLite, which accepts NULL in
-- a primary key column that does not say so. COLLATE BINARY is SQLite's
-- default and is spelled out so both engines state the same order.
CREATE TABLE module_state (
    namespace    TEXT COLLATE BINARY NOT NULL,
    record_key   TEXT COLLATE BINARY NOT NULL,
    record_value BLOB      NOT NULL,
    -- Rises by one on every write and is never reused, so a version a reader
    -- holds names one write and CompareAndPut refuses every other.
    version      INTEGER   NOT NULL,
    -- The key and the value together, which is what the namespace quota counts.
    -- It is stored rather than computed so the quota is one indexed sum and not
    -- a scan that reads every value in the namespace.
    bytes        INTEGER   NOT NULL,
    updated_at   TIMESTAMP NOT NULL,
    PRIMARY KEY (namespace, record_key)
);
