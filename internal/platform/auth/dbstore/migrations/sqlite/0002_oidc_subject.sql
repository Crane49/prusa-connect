-- The people who signed in through the OpenID Connect provider (ADR-20261023-wgv).
--
-- A login does not read this table: the id is derived from the issuer and the
-- subject every time. The table is what turns an id back into the subject and
-- the name a person recognises.
CREATE TABLE auth_oidc_subject (
    -- The bare user id the authorization graph knows the person by.
    principal    TEXT NOT NULL PRIMARY KEY,
    -- The issuer and the sub claim, as the provider spells them.
    issuer       TEXT NOT NULL,
    subject      TEXT NOT NULL,
    -- What the provider last said about the person, empty when it said
    -- nothing.
    display_name TEXT NOT NULL DEFAULT '',
    email        TEXT NOT NULL DEFAULT '',
    first_seen   TIMESTAMP NOT NULL,
    last_seen    TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX auth_oidc_subject_by_issuer ON auth_oidc_subject (issuer, subject);
