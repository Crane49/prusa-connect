-- The id this server answers to (ADR-20261025-gve).
--
-- One row, written on the first start and never changed. A client that found
-- this server at one address reads the id and knows the server again at
-- another. It is random and says nothing about the machine, the network or a
-- secret. The CHECK keeps the table to the one row.
CREATE TABLE server_instance (
    slot        INTEGER NOT NULL PRIMARY KEY CHECK (slot = 1),
    instance_id TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL
);
