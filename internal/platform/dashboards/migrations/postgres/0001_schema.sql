-- The dashboards a person lays out on the home page (ADR-0073).
--
-- A dashboard is a small row: a name, who sees it, the grid, and its cards.
-- The cards are one JSON array, because a card's settings belong to the card
-- type and the server checks their shape and size, never their fields
-- (ADR-0073 rule 3). The server keeps no copy of these rows in memory, so a
-- request reads the row it needs (rule 30).
--
-- audience is the visibility as a level: 0 everyone, 1 operators, 2
-- administrators. A listing compares it with the level the caller reaches,
-- inside the query, so its count never includes a dashboard the caller may
-- not see (rule 9).
CREATE TABLE dashboard (
    id          BIGINT NOT NULL PRIMARY KEY,
    name        TEXT NOT NULL,
    audience    INTEGER NOT NULL DEFAULT 0 CHECK (audience BETWEEN 0 AND 2),
    is_default  BOOLEAN NOT NULL DEFAULT FALSE,
    grid_columns INTEGER NOT NULL DEFAULT 12,
    density     TEXT NOT NULL DEFAULT 'comfortable',
    cards       JSONB NOT NULL DEFAULT '[]'
        CHECK (jsonb_typeof(cards) = 'array'),
    revision    INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    created_by  TEXT NOT NULL,
    changed_at  TIMESTAMPTZ NOT NULL,
    changed_by  TEXT NOT NULL
);

-- At most one dashboard is the default (rule 11).
CREATE UNIQUE INDEX dashboard_one_default ON dashboard (is_default) WHERE is_default;

-- The next id a dashboard takes. One row, raised in the transaction that
-- writes the dashboard, so an id is never given out twice, also after the
-- newest dashboard was deleted (rule 4).
CREATE TABLE dashboard_sequence (
    slot    INTEGER NOT NULL PRIMARY KEY CHECK (slot = 1),
    next_id BIGINT NOT NULL
);

INSERT INTO dashboard_sequence (slot, next_id) VALUES (1, 1);
