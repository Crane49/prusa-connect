-- The authorization graph and the device directory. The device records, the
-- prints and the registrations are internal/platform/devicestore's.
--
-- NOT NULL beside PRIMARY KEY is not redundant on SQLite: a PRIMARY KEY column
-- other than the rowid alias accepts NULL, which Postgres never does
-- (TestPrimaryKeyColumnsRefuseNull, TestSchemaParity).

-- The vocabulary, as rows rather than as DDL. internal/authz/vocabulary.go is
-- the definition and Reconcile writes it here before anything binds a port, so
-- adding a type or a relation is an INSERT on both engines. A CHECK would put
-- the vocabulary on the most engine-divergent path there is: SQLite cannot alter
-- a CHECK without rebuilding the table, and a Postgres enum cannot drop a value
-- (docs/research/18-schema-and-migrations.md §3.1).
CREATE TABLE authz_type (
    name TEXT NOT NULL PRIMARY KEY
);

CREATE TABLE authz_action (
    name TEXT NOT NULL PRIMARY KEY
);

-- One row per permitted object_type#relation@subject_type#subject_relation.
-- This is relationSpecs projected, not a list of names: the fail-open case is a
-- legal name in an illegal position, so the shape is what the tuple table's
-- foreign key points at. subject_relation is '' for a principal, as it is in a
-- tuple row, so one composite key covers usersets and principals.
CREATE TABLE authz_relation_shape (
    object_type      TEXT NOT NULL REFERENCES authz_type (name),
    relation         TEXT NOT NULL,
    subject_type     TEXT NOT NULL REFERENCES authz_type (name),
    subject_relation TEXT NOT NULL,
    PRIMARY KEY (object_type, relation, subject_type, subject_relation)
);

-- The relation tuples. The primary key is the whole tuple, so a repeated write
-- is a no-op rather than a duplicate edge.
CREATE TABLE authz_tuple (
    object_type      TEXT NOT NULL,
    object_id        TEXT NOT NULL,
    relation         TEXT NOT NULL,
    subject_type     TEXT NOT NULL,
    subject_id       TEXT NOT NULL,
    -- Empty for a principal, a relation name for a userset (Zanzibar 2.1).
    subject_relation TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (object_type, object_id, relation, subject_type, subject_id, subject_relation),
    -- The vocabulary, where the database can hold rows to it. A row that
    -- reached this table without passing NewTuple names a shape that is not in
    -- authz_relation_shape, and the engine refuses it without knowing what an
    -- org or a service account is. TestStoredVocabularyIsEnforced writes every
    -- combination past the constructors and holds this to the ones that are
    -- legal.
    --
    -- SQLite enforces this only when PRAGMA foreign_keys is on, which is off by
    -- default and set per connection. The migrator refuses to run without it.
    FOREIGN KEY (object_type, relation, subject_type, subject_relation)
        REFERENCES authz_relation_shape (object_type, relation, subject_type, subject_relation)
);

CREATE INDEX authz_tuple_by_subject
    ON authz_tuple (subject_type, subject_id, subject_relation);

CREATE INDEX authz_tuple_by_shape
    ON authz_tuple (object_type, relation, subject_type, subject_relation);

-- The device directory: which devices exist, and the object a device grant is
-- written on. The projection, the list and the visibility checks join it.
CREATE TABLE authz_device (
    device_id TEXT NOT NULL PRIMARY KEY,
    -- The name an arrival derives from the machine's model and serial. The name
    -- an operator gives a device is device_record.name, which no arrival
    -- overwrites.
    name      TEXT NOT NULL,
    -- The list orders by this, never by name: SQLite compares TEXT as bytes and
    -- Postgres compares it in the database collation, so ordering by the name
    -- would put different rows on the same page on the two engines. The key is
    -- written case-folded by the application and compared as bytes here.
    -- COLLATE BINARY is SQLite's default and is spelled out so that the parity
    -- gate has a collation to read back: NOCASE or RTRIM here would reorder the
    -- page.
    sort_key  TEXT COLLATE BINARY NOT NULL,
    -- One of the closed set ref.Kind holds (ADR-0031 rule 8). A camera is a row
    -- here beside the printers.
    kind      TEXT NOT NULL DEFAULT 'printer'
);

CREATE INDEX authz_device_by_sort_key ON authz_device (sort_key, device_id);

-- The projection: the actions each subject reaches on each device, compiled
-- from the tuples by a graph walk. since is the epoch the sight began at, which
-- bounds what the event stream replays to a newly granted origin.
CREATE TABLE device_access (
    subject_type TEXT NOT NULL,
    subject_id   TEXT NOT NULL,
    device_id    TEXT NOT NULL,
    actions      INTEGER NOT NULL,
    since        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (subject_type, subject_id, device_id)
);

CREATE INDEX device_access_by_device
    ON device_access (device_id, subject_type, subject_id);

-- The generation of the graph. Every grant write bumps it in the transaction
-- that changes the tuples, and the stream hub compares it on every tick.
CREATE TABLE authz_epoch (
    id    INT NOT NULL PRIMARY KEY CHECK (id = 1),
    epoch INTEGER NOT NULL
);

INSERT INTO authz_epoch (id, epoch) VALUES (1, 1);

-- The resource surface. A grant is written on a resource kind and never on an
-- instance, so the projection is keyed by kind, and authz_resource is the
-- directory of the instances that exist. Both keys compare as bytes on both
-- engines.
CREATE TABLE authz_resource (
    kind        TEXT COLLATE BINARY NOT NULL,
    resource_id TEXT COLLATE BINARY NOT NULL,
    PRIMARY KEY (kind, resource_id)
);

CREATE TABLE resource_access (
    subject_type TEXT NOT NULL,
    subject_id   TEXT NOT NULL,
    kind         TEXT COLLATE BINARY NOT NULL,
    actions      INTEGER NOT NULL,
    since        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (subject_type, subject_id, kind)
);

CREATE INDEX resource_access_by_kind
    ON resource_access (kind, subject_type, subject_id);

-- The marker that the graph has had its first grant written. It is one row,
-- written in the same transaction as that grant.
CREATE TABLE authz_bootstrap (
    id    INT NOT NULL PRIMARY KEY CHECK (id = 1),
    owner TEXT NOT NULL
);

-- The dangerous tier (ADR-0046 rule 17): a verb no class, role or level
-- confers, reached only by a row naming the subject, the device and the verb.
-- It is the only place a verb name appears in a grant; everywhere else a grant
-- names a class, and a class is a tuple. object_type carries the type, and the
-- refusal for an org, a group or a device group is in Go, because those span
-- device kinds and one verb would name a verb that exists on some of them only.
CREATE TABLE authz_declared_grant (
    object_type      TEXT NOT NULL,
    object_id        TEXT NOT NULL,
    verb             TEXT NOT NULL,
    subject_type     TEXT NOT NULL,
    subject_id       TEXT NOT NULL,
    -- Empty for a principal, a relation name for a userset, exactly as
    -- authz_tuple spells it.
    subject_relation TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (object_type, object_id, verb, subject_type, subject_id, subject_relation)
);

CREATE INDEX authz_declared_grant_by_subject
    ON authz_declared_grant (subject_type, subject_id, subject_relation);

-- First sight (ADR-0046 rule 22): the class and markings a verb was first seen
-- carrying, keyed by the module path that declared it, the kind and the verb.
-- The module path is the half a declaration author does not control after the
-- fact, so renaming the kind does not shed its history, and a kind that comes
-- back from another module path is refused. It is never truncated: a kind a
-- build drops keeps its record, so a kind that comes back meaning something
-- else is held rather than trusted.
CREATE TABLE authz_declared_retired (
    module     TEXT NOT NULL,
    kind       TEXT NOT NULL,
    verb       TEXT NOT NULL,
    class      TEXT NOT NULL,
    -- The markings, comma separated in a stable order. A set of at most two
    -- names is stored as text rather than as two boolean columns so a third
    -- marking is a value and not a migration.
    markings   TEXT NOT NULL DEFAULT '',
    first_seen TIMESTAMP NOT NULL,
    PRIMARY KEY (module, kind, verb)
);

-- What an operator accepted (ADR-0046 rule 23). A lowered class or a removed
-- marking is inert until a row here names it, and a module-path acceptance
-- moves a kind's publisher without moving a pin.
CREATE TABLE authz_declared_acceptance (
    module      TEXT NOT NULL,
    kind        TEXT NOT NULL,
    -- Empty for a module-path acceptance, which names no verb.
    verb        TEXT NOT NULL DEFAULT '',
    accepted_to TEXT NOT NULL,
    accepted_at TIMESTAMP NOT NULL,
    PRIMARY KEY (module, kind, verb)
);

-- The verb projection: who reaches which dangerous verb on which device,
-- compiled from authz_declared_grant as device_access is compiled from the
-- tuples, in the same transaction and by the same graph walk. It is a table and
-- not a mask because the verb set is open and has no bits to assign.
CREATE TABLE device_verb (
    subject_type TEXT NOT NULL,
    subject_id   TEXT NOT NULL,
    device_id    TEXT NOT NULL,
    verb         TEXT NOT NULL,
    PRIMARY KEY (subject_type, subject_id, device_id, verb)
);

CREATE INDEX device_verb_by_device ON device_verb (device_id);
